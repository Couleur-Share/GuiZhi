const test = require('node:test');
const assert = require('node:assert/strict');
const { load, clock, deferred, createPage } = require('./support.cjs');

const { EditorAutosave } = load('core/EditorAutosave.ts');
// 让所有待处理的微任务与已完成的 Promise 链走完。
const flush = () => new Promise(resolve => setImmediate(resolve));

function fixture(write) {
  const time = clock(), writes = [], reports = [];
  let active = 0, maxActive = 0;
  const instance = new EditorAutosave(write ?? (async (input, mode) => {
    active++; maxActive = Math.max(maxActive, active);
    try { writes.push([input, mode]); } finally { active--; }
  }), time.globals.setTimeout, time.globals.clearTimeout, (saving, error) => reports.push([saving, error]));
  // 触发当前唯一的计时器（模拟 400ms 停顿）。
  const fire = () => {
    assert.equal(time.timers.size, 1, '应有且仅有一个待触发计时器');
    const [id, entry] = [...time.timers][0];
    time.timers.delete(id); entry.fn();
  };
  return { instance, time, writes, reports, fire, stats: () => ({ maxActive }) };
}

test('连续输入合并为一次落盘：每次输入重新计时，只写停顿后的最新内容', async () => {
  const { instance, time, writes, reports, fire } = fixture();
  instance.schedule('a', 'auto'); instance.schedule('ab', 'auto'); instance.schedule('abc', 'urls');
  assert.equal(time.timers.size, 1);
  assert.equal([...time.timers.values()][0].delay, 400);
  assert.deepEqual(writes, []);
  assert.deepEqual(reports.at(-1), [true, '']);
  fire(); await flush();
  assert.deepEqual(writes, [['abc', 'urls']]);
  assert.deepEqual(reports.at(-1), [false, '']);
});

test('flush 立即写出并取消计时器；没有待写内容时不重复落盘', async () => {
  const { instance, time, writes } = fixture();
  instance.schedule('想法', 'text');
  await instance.flush();
  assert.deepEqual(writes, [['想法', 'text']]);
  assert.equal(time.timers.size, 0);
  await instance.flush();
  assert.equal(writes.length, 1);
});

test('写入进行中又有新输入：不丢内容，写入严格串行且落盘顺序与输入一致', async () => {
  const gate = deferred(), writes = [];
  let active = 0, maxActive = 0;
  const { instance, reports, fire } = fixture(async (input, mode) => {
    active++; maxActive = Math.max(maxActive, active);
    writes.push([input, mode]);
    if (input === 'a') await gate.promise;
    active--;
  });
  instance.schedule('a', 'auto'); fire(); await flush();
  instance.schedule('ab', 'auto');
  gate.resolve(); await flush();
  assert.deepEqual(reports.at(-1), [true, ''], '新输入尚未落盘，仍应显示待保存');
  fire(); await flush();
  assert.deepEqual(writes, [['a', 'auto'], ['ab', 'auto']]);
  assert.equal(maxActive, 1);
  assert.deepEqual(reports.at(-1), [false, '']);
});

test('flush 会等待在途写入结束，再写最新内容', async () => {
  const gate = deferred(), writes = [];
  const { instance } = fixture(async (input, mode) => {
    writes.push([input, mode]);
    if (input === 'a') await gate.promise;
  });
  instance.schedule('a', 'auto');
  const first = instance.flush(); await flush();
  instance.schedule('ab', 'auto');
  const second = instance.flush(); await flush();
  assert.deepEqual(writes, [['a', 'auto']], '第二次 flush 必须等第一次写完');
  gate.resolve(); await Promise.all([first, second]);
  assert.deepEqual(writes, [['a', 'auto'], ['ab', 'auto']]);
});

test('写入期间输入改回已写内容：不再重复落盘', async () => {
  const gate = deferred(), writes = [];
  const { instance, reports, fire } = fixture(async (input, mode) => { writes.push([input, mode]); await gate.promise; });
  instance.schedule('a', 'auto'); fire(); await flush();
  instance.schedule('ab', 'auto'); instance.schedule('a', 'auto');
  gate.resolve(); await flush();
  fire(); await flush();
  assert.deepEqual(writes, [['a', 'auto']]);
  assert.deepEqual(reports.at(-1), [false, '']);
});

test('写入失败：flush 抛出原始错误并保留待保存；错误可见，重试成功后清除', async () => {
  let fail = true; const writes = [];
  const { instance, reports } = fixture(async (input, mode) => {
    if (fail) throw new Error('存储空间不足');
    writes.push([input, mode]);
  });
  instance.schedule('重要内容', 'auto');
  await assert.rejects(instance.flush(), /存储空间不足/);
  const failure = reports.at(-1);
  assert.equal(failure[0], false);
  assert.match(failure[1], /自动保存失败：存储空间不足。请保留输入内容并点击保存重试/);
  fail = false;
  await instance.flush();
  assert.deepEqual(writes, [['重要内容', 'auto']]);
  assert.deepEqual(reports.at(-1), [false, '']);
});

test('计时器触发的写入失败只展示不抛出，且不产生未处理的 Promise 拒绝', async () => {
  let unhandled = 0; const onUnhandled = () => { unhandled++; };
  process.on('unhandledRejection', onUnhandled);
  try {
    const { instance, reports, fire } = fixture(async () => { throw new Error('合成故障'); });
    instance.schedule('x', 'auto'); fire(); await flush(); await flush();
    assert.match(reports.at(-1)[1], /合成故障/);
    // 失败后仍处于待保存状态：下一次 flush 会重试并再次报告。
    await assert.rejects(instance.flush(), /合成故障/);
  } finally { process.off('unhandledRejection', onUnhandled); }
  assert.equal(unhandled, 0);
});

// ---------- 页面接入 ----------

function editorPage({ edit, restored } = {}) {
  const calls = [];
  const { ui, time } = createPage({}, { '@kit.ArkTS': { util: { generateRandomUUID: () => 'uuid-0000000000000001' } } });
  let editor = '';
  ui.runtime.queue = {
    snapshot: () => ({ drafts: [], editor: restored ?? editor, mode: restored ? 'text' : 'auto' }),
    edit: edit ?? (async (text, mode) => { calls.push(['edit', text, mode]); editor = text; }),
    enqueue: async id => { calls.push(['enqueue', id]); },
    restoreForEdit: async id => { calls.push(['restore', id]); },
  };
  ui.holdingObserver = { start() {}, stop() {} };
  ui.selectTab = () => {};
  ui.showEditor = true;
  return { ui, time, calls };
}

test('页面：连续输入只在停顿后落盘一次，写入最新文字与模式', async () => {
  const { ui, time, calls } = editorPage();
  ui.input = 'https://exa'; ui.persistEditor();
  ui.input = 'https://example.com'; ui.mode = 'urls'; ui.persistEditor();
  assert.deepEqual(calls, []);
  assert.equal(time.timers.size, 1);
  const [id, entry] = [...time.timers][0]; time.timers.delete(id); entry.fn(); await flush();
  assert.deepEqual(calls, [['edit', 'https://example.com', 'urls']]);
});

test('页面：关闭前立即落盘并取消计时器；写入失败则保留编辑面板并显示原因', async () => {
  const ok = editorPage();
  ok.ui.input = '未完成的想法'; ok.ui.persistEditor();
  ok.ui.closeEditor(); await flush();
  assert.deepEqual(ok.calls, [['edit', '未完成的想法', 'auto']]);
  assert.equal(ok.ui.showEditor, false);
  assert.equal(ok.time.timers.size, 0);

  const bad = editorPage({ edit: async () => { throw new Error('存储写入失败'); } });
  bad.ui.input = '不能丢'; bad.ui.persistEditor();
  bad.ui.closeEditor(); await flush();
  assert.equal(bad.ui.showEditor, true);
  assert.match(bad.ui.error, /存储写入失败/);
  assert.match(bad.ui.editorError, /自动保存失败：存储写入失败/);
});

test('页面：保存草稿先写出输入框最新内容，再入队并清空', async () => {
  const { ui, calls } = editorPage();
  ui.input = 'https://example.com/a'; ui.persistEditor();
  await ui.saveDraft();
  assert.deepEqual(calls, [['edit', 'https://example.com/a', 'auto'], ['enqueue', 'uuid-0000000000000001']]);
  assert.equal(ui.input, ''); assert.equal(ui.showEditor, false);
});

test('页面：编辑草稿前先落盘待写输入，过期计时器之后不能覆盖恢复出的原文', async () => {
  const { ui, time, calls } = editorPage({ restored: '草稿原文' });
  ui.input = '旧输入'; ui.persistEditor();
  await ui.editDraft({ requestId: 'draft-1' });
  assert.deepEqual(calls, [['edit', '旧输入', 'auto'], ['restore', 'draft-1']]);
  assert.equal(ui.input, '草稿原文');
  assert.equal(time.timers.size, 0, '待触发的旧计时器必须已取消');
});

test('页面：进入后台、页面隐藏前立即落盘待写输入', async () => {
  for (const leave of [ui => { ui.foreground = false; ui.foregroundChanged(); }, ui => ui.onPageHide(), ui => ui.aboutToDisappear()]) {
    const { ui, time, calls } = editorPage();
    ui.input = '切到后台前的输入'; ui.persistEditor();
    leave(ui); await flush();
    assert.deepEqual(calls, [['edit', '切到后台前的输入', 'auto']]);
    assert.equal(time.timers.size, 0);
  }
});
