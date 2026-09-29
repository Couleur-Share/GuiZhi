// 阶段 6 的页面控制：多选、延迟删除与撤销、多选发送。
// 只证明控制逻辑（状态、调用顺序、数据是否真的被改动）；列表、滑动手势与撤销条的呈现由 Hvigor 编译和真机验证。
const test = require('node:test');
const assert = require('node:assert/strict');
const { deferred, settle } = require('./support.cjs');
const { pageWith, receipt, until } = require('./page-fixture.cjs');

// 删除确认对话框会调用 panelMaterial；其余系统边界由 createPage 提供。
const dialogImports = { '../services/ImmersiveAppearance': { panelMaterial: () => 'material', CaptureTabsAppearance: class {} } };
const three = ['https://example.com/a', 'https://example.com/b', 'https://example.com/c'];

async function page(inputs = three, clientParts = {}) {
  const fixture = await pageWith(clientParts, inputs, dialogImports);
  const dialogs = [];
  const answer = { index: 1 };
  fixture.ui.getUIContext = () => ({ getPromptAction: () => ({ showDialog: async options => { dialogs.push(options); return { index: answer.index }; } }) });
  return { ...fixture, dialogs, answer };
}

const ids = list => Array.from(list, draft => draft.requestId);
const stored = fixture => ids(fixture.queue.snapshot().drafts);

// 触发撤销窗口的到点计时器（8 秒）。
function fireUndoWindow(time) {
  for (const [id, timer] of time.timers) {
    if (timer.delay === 8000) { time.timers.delete(id); timer.fn(); return true; }
  }
  return false;
}
const undoTimers = time => [...time.timers.values()].filter(timer => timer.delay === 8000).length;

// ---------- 延迟删除 ----------

test('删除草稿：立即从列表隐藏并出现撤销条，本机数据在撤销窗口内不变，到点才真正删除', async () => {
  const fixture = await page();
  const { ui, time, dialogs } = fixture;
  const [a, b, c] = ui.drafts;
  await ui.requestDelete([b]);
  assert.deepEqual(Array.from(ui.hiddenIds), [b.requestId]);
  assert.equal(ui.undoMessage, '已删除 1 条草稿');
  assert.deepEqual(Array.from(ui.visibleIds()), [a.requestId, c.requestId]);
  assert.deepEqual(stored(fixture), [a.requestId, b.requestId, c.requestId], '撤销窗口内草稿仍保存在本机');
  assert.equal(dialogs.length, 0, '未尝试发送的草稿不再弹确认框，撤销条代替确认');
  assert.equal(undoTimers(time), 1);

  assert.equal(fireUndoWindow(time), true);
  await until(() => stored(fixture).length === 2);
  assert.deepEqual(stored(fixture), [a.requestId, c.requestId]);
  await until(() => ui.hiddenIds.length === 0);
  assert.equal(ui.undoMessage, '', '窗口结束后撤销条消失');
  assert.deepEqual(ids(ui.drafts), [a.requestId, c.requestId]);
  assert.equal(ui.error, '');
});

test('撤销删除：草稿恢复显示，本机数据从未改动，计时器被清除，之后不会再被删除', async () => {
  const fixture = await page();
  const { ui, time } = fixture;
  await ui.requestDelete([ui.drafts[0], ui.drafts[1]]);
  assert.equal(ui.undoMessage, '已删除 2 条草稿');
  ui.undoDelete();
  assert.deepEqual(Array.from(ui.hiddenIds), []);
  assert.equal(ui.undoMessage, '');
  assert.equal(ui.notice, '已撤销删除 2 条');
  assert.equal(undoTimers(time), 0);
  assert.equal(stored(fixture).length, 3);
  assert.equal(fireUndoWindow(time), false, '没有残留的删除计时器');
  await until(() => true);
  assert.equal(stored(fixture).length, 3);
  // 单条撤销的文案。
  await ui.requestDelete([ui.drafts[0]]);
  ui.undoDelete();
  assert.equal(ui.notice, '已撤销删除');
});

test('删除已尝试发送的草稿仍先确认：选择保留则什么都不发生，确认后才进入撤销窗口', async () => {
  const fixture = await page();
  const { ui, dialogs, answer } = fixture;
  fixture.queue.state.drafts[0].attempted = true; fixture.queue.state.drafts[1].attempted = true;
  ui.drafts = fixture.queue.snapshot().drafts;
  const [a, b, c] = ui.drafts;

  answer.index = 0;
  await ui.requestDelete([a]);
  assert.equal(dialogs.length, 1);
  assert.equal(dialogs[0].title, '删除已尝试发送的草稿？');
  assert.match(dialogs[0].message, /^此条可能已被服务端接收/);
  assert.deepEqual(Array.from(ui.hiddenIds), []);
  assert.equal(ui.undoMessage, '');

  answer.index = 1;
  await ui.requestDelete([a, b, c]);
  assert.equal(dialogs.length, 2);
  assert.match(dialogs[1].message, /^其中 2 条可能已被服务端接收/);
  assert.deepEqual(Array.from(ui.hiddenIds).sort(), [a.requestId, b.requestId, c.requestId].sort());
  assert.equal(ui.undoMessage, '已删除 3 条草稿');
});

test('删除：正在发送的草稿不删除并说明原因，其余照常进入撤销窗口', async () => {
  const fixture = await page();
  const { ui } = fixture;
  const [a, b] = ui.drafts;
  assert.equal(ui.beginSending(a.requestId), true);
  await ui.requestDelete([a]);
  assert.equal(ui.error, '正在发送的草稿不能删除，请等待发送结果');
  assert.deepEqual(Array.from(ui.hiddenIds), []);
  assert.equal(ui.undoMessage, '');

  await ui.requestDelete([a, b]);
  assert.deepEqual(Array.from(ui.hiddenIds), [b.requestId], '只隐藏没有在发送的那条');
  assert.equal(ui.notice, '1 条正在发送，未删除');
  assert.equal(ui.undoMessage, '已删除 1 条草稿');
});

test('删除同时以队列为准：草稿已在队列里发送，即使页面还没登记发送状态也不能删除', async () => {
  const gate = deferred();
  const fixture = await page(three, { send: async body => { await gate.promise; return receipt(body.requestId); } });
  const { ui, queue, client } = fixture;
  const a = ui.drafts[0];
  const inFlight = queue.send(a.requestId, 'computer-a', client);   // 绕过页面，直接由队列发起发送
  await settle();
  assert.equal(queue.isSending(a.requestId), true);
  assert.equal(ui.sendings.length, 0, '页面没有登记这次发送');

  await ui.requestDelete([a]);
  assert.equal(ui.error, '正在发送的草稿不能删除，请等待发送结果');
  assert.deepEqual(Array.from(ui.hiddenIds), []);
  assert.equal(ui.undoMessage, '');
  gate.resolve(); await inFlight;
});

test('防御：选择集合里即使残留已隐藏的编号，删除与发送也只作用于当前可见的草稿，刚删掉的内容不会被发出去', async () => {
  const fixture = await page();
  const { ui, stats } = fixture;
  const [a, b, c] = ui.drafts;
  await ui.requestDelete([b]);                                          // b 进入撤销窗口，已隐藏但仍在本机
  ui.selecting = true; ui.selectedIds = [a.requestId, b.requestId];     // 人为构造残留状态（正常流程会被及时收缩）
  assert.deepEqual(ids(ui.selectedDrafts()), [a.requestId], '选中的草稿不含已隐藏的');
  await ui.sendSelected();
  assert.deepEqual(stats.sends, [a.requestId], '只发送可见且被选中的 a，b 没有被发出去');
  ui.undoDelete();
  assert.deepEqual(ids(ui.drafts), [b.requestId, c.requestId], '撤销后 b 仍在，且从未发送');
});

test('再次删除会立即结束上一次的撤销窗口：只有最新一批可撤销', async () => {
  const fixture = await page();
  const { ui, time } = fixture;
  const [a, b, c] = ui.drafts;
  await ui.requestDelete([a]);
  await ui.requestDelete([b]);
  assert.deepEqual(stored(fixture), [b.requestId, c.requestId], '第一批已经真正删除');
  assert.deepEqual(Array.from(ui.hiddenIds), [b.requestId]);
  assert.equal(undoTimers(time), 1);
  ui.undoDelete();
  assert.deepEqual(ids(ui.drafts), [b.requestId, c.requestId], '撤销只恢复第二批');
  assert.equal(ui.notice, '已撤销删除');
});

test('删除失败：失败的草稿恢复显示并逐条说明原因，成功的照常删除；完整原因可复制', async () => {
  const fixture = await page(['https://example.com/a', 'https://example.com/b', 'https://example.com/c', 'https://example.com/d', 'https://example.com/e']);
  const { ui, time, queue } = fixture;
  const failing = new Set([ui.drafts[1].requestId, ui.drafts[2].requestId, ui.drafts[3].requestId, ui.drafts[4].requestId]);
  const remove = queue.remove.bind(queue);
  queue.remove = async requestId => { if (failing.has(requestId)) throw new Error('存储写入失败'); return remove(requestId); };
  await ui.requestDelete(ui.drafts);
  assert.equal(ui.drafts.length, 5);
  fireUndoWindow(time);
  await until(() => ui.error !== '');
  assert.equal(stored(fixture).length, 4, '第一条删除成功');
  assert.deepEqual(Array.from(ui.hiddenIds), [], '失败的四条已恢复显示');
  assert.match(ui.error, /^4 条草稿没能删除，已恢复显示\n/);
  const inline = ui.error.split('\n').filter(line => line.startsWith('· '));
  assert.equal(inline.length, 3 + 1, '内联只显示 3 条原因，外加一条“另有 1 条”的提示');
  assert.match(ui.error, /另有 1 条，可点“复制原因”查看全部/);
  await ui.copyError();
  const full = fixture.copied[0].split('\n').filter(line => line.startsWith('· '));
  assert.deepEqual(full, ['b', 'c', 'd', 'e'].map(name => `· https://example.com/${name} — 存储写入失败`));
  assert.equal(ui.notice, '错误信息已复制');
});

test('进入后台或离开页面前立即真正删除，不把撤销窗口带进后台', async () => {
  const fixture = await page();
  const { ui, time } = fixture;
  ui.holdingObserver = { start() {}, stop() {} };
  await ui.requestDelete([ui.drafts[0]]);
  ui.foreground = false; ui.foregroundChanged();
  await until(() => stored(fixture).length === 2);
  assert.equal(undoTimers(time), 0);
  await until(() => ui.undoMessage === '' && ui.hiddenIds.length === 0);

  await ui.requestDelete([ui.drafts[0]]);
  ui.onPageHide();
  await until(() => stored(fixture).length === 1);
  await ui.requestDelete([ui.drafts[0]]);
  ui.aboutToDisappear();
  await until(() => stored(fixture).length === 0);
});

// ---------- 多选 ----------

test('多选：长按进入并选中当前项；点击切换；全选与取消全选；返回先退出多选', async () => {
  const fixture = await page();
  const { ui } = fixture;
  const [a, b, c] = ui.drafts;
  ui.toggleSelection(a.requestId);
  assert.equal(ui.selecting, false); assert.deepEqual(Array.from(ui.selectedIds), [], '没进入多选时点击不产生选择');
  assert.equal(ui.onBackPress(), false, '不在多选时返回键交给系统');

  ui.enterSelection(a.requestId);
  assert.equal(ui.selecting, true);
  assert.deepEqual(Array.from(ui.selectedIds), [a.requestId]);
  ui.toggleSelection(b.requestId);
  assert.deepEqual(Array.from(ui.selectedIds), [a.requestId, b.requestId]);
  ui.toggleSelection(a.requestId);
  assert.deepEqual(Array.from(ui.selectedIds), [b.requestId]);
  ui.toggleSelectAll();
  assert.deepEqual(Array.from(ui.selectedIds), [a.requestId, b.requestId, c.requestId]);
  ui.toggleSelectAll();
  assert.deepEqual(Array.from(ui.selectedIds), []);
  assert.equal(ui.selecting, true, '取消全选后仍在多选中，可继续挑选');
  assert.equal(ui.onBackPress(), true, '多选中返回先退出多选');
  assert.equal(ui.selecting, false);
  assert.equal(ui.onBackPress(), false);
});

test('多选不会在忙碌、批量发送中或条目已隐藏时进入', async () => {
  const fixture = await page();
  const { ui } = fixture;
  const [a, b] = ui.drafts;
  ui.busy = true; ui.enterSelection(a.requestId); assert.equal(ui.selecting, false);
  ui.busy = false; ui.batchTotal = 2; ui.enterSelection(a.requestId); assert.equal(ui.selecting, false);
  ui.batchTotal = 0;
  await ui.requestDelete([b]);
  ui.enterSelection(b.requestId); assert.equal(ui.selecting, false, '延迟删除中的草稿不可选');
  ui.enterSelection(a.requestId); assert.equal(ui.selecting, true);
});

test('选择集合随草稿变化收缩：条目被删除或隐藏后不再被选中，没有可选草稿时自动退出多选', async () => {
  const fixture = await page();
  const { ui, queue } = fixture;
  const [a, b, c] = ui.drafts;
  ui.enterSelection(a.requestId); ui.toggleSelection(b.requestId); ui.toggleSelection(c.requestId);
  await queue.remove(a.requestId); ui.reloadDrafts();
  assert.deepEqual(Array.from(ui.selectedIds), [b.requestId, c.requestId]);
  await ui.requestDelete([b]);
  assert.deepEqual(Array.from(ui.selectedIds), [c.requestId], '进入延迟删除的条目退出选择');
  assert.equal(ui.selecting, true);
  await ui.requestDelete([c]);
  assert.equal(ui.selecting, false, '所有可见草稿都没了，自动退出多选');
  assert.deepEqual(Array.from(ui.selectedIds), []);
});

test('多选删除：整批进入撤销窗口并退出多选；撤销后整批恢复', async () => {
  const fixture = await page();
  const { ui } = fixture;
  const [a, b, c] = ui.drafts;
  ui.enterSelection(a.requestId); ui.toggleSelection(c.requestId);
  assert.deepEqual(ids(ui.selectedDrafts()), [a.requestId, c.requestId]);
  await ui.deleteSelected();
  assert.equal(ui.selecting, false);
  assert.deepEqual(Array.from(ui.hiddenIds).sort(), [a.requestId, c.requestId].sort());
  assert.equal(ui.undoMessage, '已删除 2 条草稿');
  assert.deepEqual(Array.from(ui.visibleIds()), [b.requestId]);
  ui.undoDelete();
  assert.deepEqual(Array.from(ui.visibleIds()), [a.requestId, b.requestId, c.requestId]);
});

test('多选删除时在确认框选择“保留”：多选与选择集合保持不变，什么都没有被隐藏', async () => {
  const fixture = await page();
  const { ui, answer, dialogs } = fixture;
  fixture.queue.state.drafts[0].attempted = true;
  ui.drafts = fixture.queue.snapshot().drafts;
  const [a, b] = ui.drafts;
  ui.enterSelection(a.requestId); ui.toggleSelection(b.requestId);
  answer.index = 0;
  await ui.deleteSelected();
  assert.equal(dialogs.length, 1);
  assert.equal(ui.selecting, true);
  assert.deepEqual(Array.from(ui.selectedIds), [a.requestId, b.requestId]);
  assert.deepEqual(Array.from(ui.hiddenIds), []);
  assert.equal(ui.undoMessage, '');
});

// ---------- 发送范围 ----------

test('发送所选：只发送选中的草稿并退出多选，其余保留；汇总按所选数量计', async () => {
  const fixture = await page();
  const { ui, stats } = fixture;
  const [a, b, c] = ui.drafts;
  ui.enterSelection(b.requestId); ui.toggleSelection(c.requestId);
  await ui.sendSelected();
  assert.deepEqual(stats.sends, [b.requestId, c.requestId]);
  assert.equal(ui.selecting, false);
  assert.deepEqual(ids(ui.drafts), [a.requestId], '没选的草稿原样保留');
  assert.equal(ui.notice, '已发送 2 条，中转已接收，电脑上线后将处理');
  assert.equal(stats.sessions, 1);
});

test('发送所选遇到失败：不报成功，失败的草稿保留并给出原因', async () => {
  const { RelayRequestError } = require('./support.cjs');
  const fixture = await page(three, { send: async body => {
    if (body.input.endsWith('/b')) throw new RelayRequestError('内容格式不正确', 400, 'invalid_input');
    return receipt(body.requestId);
  } });
  const { ui } = fixture;
  const [a, b, c] = ui.drafts;
  ui.enterSelection(a.requestId); ui.toggleSelection(b.requestId);
  await ui.sendSelected();
  assert.match(ui.error, /^已发送 1 条，失败 1 条\n/);
  assert.equal(ui.notice, '');
  assert.deepEqual(ids(ui.drafts), [b.requestId, c.requestId]);
});

test('全部发送不包含延迟删除中的草稿：用户刚删掉的内容不会被发出去', async () => {
  const fixture = await page();
  const { ui, stats } = fixture;
  const [a, b, c] = ui.drafts;
  await ui.requestDelete([b]);
  await ui.sendAll();
  assert.deepEqual(stats.sends, [a.requestId, c.requestId]);
  assert.equal(ui.notice, '已发送 2 条，中转已接收，电脑上线后将处理');
  ui.undoDelete();
  assert.deepEqual(ids(ui.drafts), [b.requestId], '撤销后被删的那条仍在，且从未发送');
});

test('批量发送或页面忙碌时不启动“发送所选”，也不丢失选择', async () => {
  const fixture = await page();
  const { ui, stats } = fixture;
  ui.enterSelection(ui.drafts[0].requestId);
  ui.batchTotal = 1; await ui.sendSelected();
  ui.batchTotal = 0; ui.busy = true; await ui.sendSelected();
  assert.equal(stats.sends.length, 0);
  assert.equal(ui.selecting, true);
  assert.equal(ui.selectedIds.length, 1);
});
