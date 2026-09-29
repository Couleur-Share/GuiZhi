const test = require('node:test');
const assert = require('node:assert/strict');
const { load, deferred, settle, createPage, check, sendingState } = require('./support.cjs');

const { RelayRequestError } = check;
const protocol = load('core/Protocol.ts');
const outcome = load('core/SendOutcome.ts', { './ConnectionCheck': check });
const { DraftQueue } = load('core/DraftQueue.ts', { './Protocol': protocol, './SendOutcome': outcome });

// ---------- 发送中状态的纯函数 ----------
test('SendingState：登记幂等、结束移除、计时不修改旧数组', () => {
  const { sendingBegin, sendingEnd, sendingFind, sendingTick, sendingLabel, sendingSlowHint, SLOW_SEND_SECONDS } = sendingState;
  const empty = [];
  const one = sendingBegin(empty, 'a', 1000);
  assert.equal(empty.length, 0, '不改动传入数组');
  assert.equal(sendingBegin(one, 'a', 2000), one, '重复登记原样返回，保留最初的开始时间');
  const two = sendingBegin(one, 'b', 3000);
  assert.deepEqual(two.map(entry => entry.requestId), ['a', 'b']);
  const ticked = sendingTick(two, 8500);
  assert.deepEqual(ticked.map(entry => entry.seconds), [7, 5]);
  assert.deepEqual(two.map(entry => entry.seconds), [0, 0], '旧数组保持不变');
  assert.equal(sendingTick(two, 500)[0].seconds, 0, '时钟回拨不出现负数');
  assert.equal(sendingFind(ticked, 'b').startedAt, 3000);
  assert.equal(sendingFind(ticked, 'x'), undefined);
  assert.deepEqual(sendingEnd(ticked, 'a').map(entry => entry.requestId), ['b']);
  assert.equal(sendingEnd(ticked, 'x').length, 2);
  assert.equal(sendingLabel(0), '发送中 · 0 秒');
  assert.equal(sendingLabel(12), '发送中 · 12 秒');
  assert.equal(SLOW_SEND_SECONDS, 5);
  assert.equal(sendingSlowHint(SLOW_SEND_SECONDS - 1), '');
  assert.equal(sendingSlowHint(SLOW_SEND_SECONDS), '网络较慢，可继续记录');
});

// ---------- 页面夹具：真实 DraftQueue + 可编排的中转客户端 ----------
const receipt = requestId => ({ id: `receipt-${requestId}`, requestId, createdAt: 1, itemCount: 1, state: 'accepted', progress: null });
let uuidCounter = 0;
const uuid = () => `00000000-0000-4000-8000-${String(++uuidCounter).padStart(12, '0')}`;

async function pageWith(clientParts = {}, inputs = ['https://example.com/a'], extraImports = {}) {
  const stats = { sessions: 0, histories: 0, sends: [] };
  const sendImpl = clientParts.send ?? (async body => receipt(body.requestId));
  const client = {
    session: async () => { stats.sessions++; return { paired: true }; },
    history: async () => { stats.histories++; return []; },
    ...clientParts,
    // 统一在最外层计数，自定义 send 也被记录。
    send: body => { stats.sends.push(body.requestId); return sendImpl(body); },
  };
  const copied = [];
  const { ui, time } = createPage(client, {
    '@kit.ArkTS': { util: { generateRandomUUID: () => uuid() } },
    '@kit.BasicServicesKit': { pasteboard: {
      MIMETYPE_TEXT_PLAIN: 'text/plain', createData: (mime, text) => ({ mime, text }),
      getSystemPasteboard: () => ({ setData: async data => { copied.push(data.text); } }) } },
    ...extraImports,
  });
  const store = { state: null, async save(state) { this.state = JSON.parse(JSON.stringify(state)); } };
  const queue = new DraftQueue(store, new protocol.LocalState());
  for (const input of inputs) { await queue.edit(input, 'auto'); await queue.enqueue(uuid()); }
  ui.runtime = { client, connection: { origin: 'https://example.invalid', pairingId: 'computer-a' }, queue };
  ui.drafts = queue.snapshot().drafts; ui.paired = true;
  ui.selectTab = () => {}; ui.syncHolding = () => {}; ui.startPolling = () => {};
  ui.settings = { autoSendShare: false, defaultMode: 'auto' }; ui.settingsReady = true;
  return { ui, time, queue, store, stats, copied, client };
}
// 等待后台 Promise 链（串行步骤有多次 await）走完。
async function until(condition, limit = 200) { for (let i = 0; i < limit && !condition(); i++) await settle(); assert.ok(condition(), '等待条件未在预期内满足'); }
const oneSecondTimers = time => [...time.timers.values()].filter(timer => timer.delay === 1000);
// VM 内创建的数组与宿主 Array 原型不同，断言前先转成宿主数组。
const sendingIds = ui => Array.from(ui.sendings, entry => entry.requestId);

test('单条发送不占页面锁：只登记这一条，计时刷新秒数，结束后清理并刷新一次记录', async () => {
  const wait = deferred();
  const { ui, time, stats } = await pageWith({ send: () => wait.promise });
  const draft = ui.drafts[0];
  const done = ui.sendDraft(draft);
  assert.equal(ui.busy, false, '发送不使用页面全局忙碌状态');
  assert.deepEqual(sendingIds(ui), [draft.requestId]);
  const timers = oneSecondTimers(time);
  assert.equal(timers.length, 1, '有一个每秒刷新的计时器');
  time.advance(6000); timers[0].fn();
  assert.equal(ui.sendings[0].seconds, 6);
  wait.resolve(receipt(draft.requestId));
  assert.equal(await done, true);
  assert.equal(ui.sendings.length, 0);
  assert.equal(time.timers.size, 0, '没有发送时清除计时器');
  assert.equal(ui.notice, '中转已接收，电脑上线后将处理');
  assert.equal(ui.error, '');
  assert.equal(stats.sessions, 0, '已确认配对时不再多做一次会话预检');
  assert.equal(stats.histories, 1);
  assert.equal(ui.drafts.length, 0);
});

test('发送在途期间：可继续记录（act 与队列编辑不受影响），发送状态不受其他操作干扰', async () => {
  const wait = deferred();
  const { ui, queue } = await pageWith({ send: () => wait.promise });
  const draft = ui.drafts[0];
  const done = ui.sendDraft(draft);
  await ui.act(async () => { await queue.edit('发送期间新写的想法', 'text'); });
  assert.equal(ui.busy, false);
  assert.equal(queue.snapshot().editor, '发送期间新写的想法');
  assert.equal(ui.sendings.length, 1, '仍在发送中');
  await ui.act(async () => { await queue.enqueue(uuid()); });
  assert.equal(queue.snapshot().drafts.length, 2);
  wait.resolve(receipt(draft.requestId));
  await done;
  assert.deepEqual(Array.from(queue.snapshot().drafts, item => item.input), ['发送期间新写的想法'], '只移除已发送的那一条');
  assert.equal(ui.notice, '中转已接收，电脑上线后将处理');
});

test('同一条重复点击只发一次；两条并发发送各自独立完成', async () => {
  const gates = new Map();
  const { ui, stats } = await pageWith({ send: body => { const entry = deferred(); gates.set(body.requestId, entry); return entry.promise; } },
    ['https://example.com/a', 'https://example.com/b']);
  const [a, b] = ui.drafts;
  const first = ui.sendDraft(a);
  assert.equal(await ui.sendDraft(a), false, '重复点击被忽略');
  const second = ui.sendDraft(b);
  await until(() => gates.size === 2);
  assert.equal(stats.sends.length, 2, '两条各发出一次，重复点击没有额外请求');
  assert.deepEqual(sendingIds(ui), [a.requestId, b.requestId]);
  gates.get(b.requestId).resolve(receipt(b.requestId)); await second;
  assert.deepEqual(sendingIds(ui), [a.requestId], '先完成的一条只移除自己');
  gates.get(a.requestId).resolve(receipt(a.requestId)); await first;
  assert.equal(ui.sendings.length, 0);
  assert.equal(ui.drafts.length, 0);
});

test('发送失败：卡片状态清理，原因在页面提示，草稿带原因保留，可再次发送成功', async () => {
  let fail = true;
  const { ui, queue } = await pageWith({ send: async body => {
    if (fail) throw new RelayRequestError('网络不可用，草稿已保留', 0, 'network');
    return receipt(body.requestId);
  } });
  const draft = ui.drafts[0];
  assert.equal(await ui.sendDraft(draft), false);
  assert.equal(ui.error, '网络不可用，草稿已保留');
  assert.equal(ui.sendings.length, 0);
  assert.equal(ui.busy, false);
  assert.equal(ui.drafts[0].lastError, '网络不可用，草稿已保留', '页面列表已刷新为带失败原因的草稿');
  assert.equal(queue.snapshot().drafts[0].attempted, true, '网络中断无法证明未保存');
  fail = false;
  assert.equal(await ui.sendDraft(draft), true);
  assert.equal(ui.error, '', '新一次发送开始时清除旧错误');
  assert.equal(ui.drafts.length, 0);
});

test('会话预检：已配对直接发送；尚未配对先检查一次并在未确认时说明原因，不发出请求', async () => {
  const unpaired = await pageWith({ session: async () => ({ paired: false }) });
  unpaired.ui.paired = false;
  assert.equal(await unpaired.ui.sendDraft(unpaired.ui.drafts[0]), false);
  assert.equal(unpaired.ui.error, '请在电脑上的归知确认这台手机');
  assert.equal(unpaired.stats.sends.length, 0);
  assert.equal(unpaired.queue.snapshot().drafts[0].attempted, false, '预检失败不产生“已尝试”标记');
  assert.equal(unpaired.ui.sendings.length, 0);
  const late = await pageWith({});
  late.ui.paired = false;
  assert.equal(await late.ui.sendDraft(late.ui.drafts[0]), true);
  assert.equal(late.stats.sessions, 1);
  assert.equal(late.ui.paired, true);
  const offline = await pageWith({});
  offline.ui.runtime.client = undefined;
  assert.equal(await offline.ui.sendDraft(offline.ui.drafts[0]), false);
  assert.match(offline.ui.error, /配对电脑/);
});

// ---------- 全部发送 ----------
test('全部发送：预检与记录刷新各一次，进度逐条推进，全部成功才提示成功，全程不占页面锁', async () => {
  const seen = [];
  let ui;
  const parts = { send: async body => {
    seen.push([ui.batchDone, ui.batchTotal, ui.busy, ui.sendings.map(entry => entry.requestId).join()]);
    return receipt(body.requestId);
  } };
  const fixture = await pageWith(parts, ['https://example.com/a', 'https://example.com/b', 'https://example.com/c']);
  ui = fixture.ui;
  const ids = ui.drafts.map(draft => draft.requestId);
  await ui.sendAll();
  assert.deepEqual(seen, [[0, 3, false, ids[0]], [1, 3, false, ids[1]], [2, 3, false, ids[2]]]);
  assert.equal(fixture.stats.sessions, 1, '预检会话只做一次');
  assert.equal(fixture.stats.histories, 1, '结束后刷新近期记录只做一次');
  assert.equal(ui.notice, '已发送 3 条，中转已接收，电脑上线后将处理');
  assert.equal(ui.error, '');
  assert.equal(ui.batchTotal, 0); assert.equal(ui.batchDone, 0);
  assert.equal(ui.sendings.length, 0);
  assert.equal(ui.drafts.length, 0);
});

test('全部发送遇到失败：不报成功，界面只内联前几条原因，复制得到完整逐条原因', async () => {
  // 第一条成功；其余四条内容被服务端拒绝（400）：单条内容问题不会中止批量。
  const rejected = ['甲', '乙', '丙', '丁'];
  const fixture = await pageWith({ send: async body => {
    if (rejected.includes(body.input)) throw new RelayRequestError('内容格式不正确', 400, 'invalid_input');
    return receipt(body.requestId);
  } }, ['https://example.com/ok', ...rejected]);
  const { ui } = fixture;
  await ui.sendAll();
  assert.equal(ui.notice, '');
  assert.match(ui.error, /^已发送 1 条，失败 4 条\n/);
  assert.doesNotMatch(ui.error, /中转已接收/);
  assert.equal(ui.batchTotal, 0);
  assert.equal(ui.sendings.length, 0);
  assert.equal(ui.drafts.length, 4, '失败的草稿全部保留');
  const inline = ui.error.split('\n').filter(line => line.startsWith('· '));
  assert.equal(inline.length, 3 + 1, '内联只显示 3 条原因，外加一条“另有 1 条”的提示');
  assert.match(ui.error, /另有 1 条，点击“复制原因”查看全部/);
  await ui.copyError();
  assert.equal(fixture.copied.length, 1);
  const full = fixture.copied[0].split('\n').filter(line => line.startsWith('· '));
  assert.deepEqual(full, rejected.map(input => `· ${input} — 内容格式不正确`), '复制版包含每一条失败的原因');
  // 文案变化后不再使用过期的完整原因。
  ui.error = '另一个错误';
  await ui.copyError();
  assert.equal(fixture.copied[1], '另一个错误');
});

test('全部发送：限流后停止，剩余草稿保留；未确认配对或未连接时不发出请求且状态复位', async () => {
  let calls = 0;
  const limited = await pageWith({ send: async body => {
    calls++;
    if (calls === 2) throw new RelayRequestError('请求过于频繁，请稍后重试', 429, 'rate_limited');
    return receipt(body.requestId);
  } }, ['https://example.com/a', 'https://example.com/b', 'https://example.com/c']);
  await limited.ui.sendAll();
  assert.equal(calls, 2);
  assert.match(limited.ui.error, /已停止：请求过于频繁，请稍后重试/);
  assert.equal(limited.ui.drafts.length, 2, '被限流的一条和未处理的一条都保留');
  assert.equal(limited.ui.batchTotal, 0);

  const unpaired = await pageWith({ session: async () => ({ paired: false }) }, ['https://example.com/a', 'https://example.com/b']);
  await unpaired.ui.sendAll();
  assert.equal(unpaired.ui.error, '请在电脑上的归知确认这台手机');
  assert.equal(unpaired.stats.sends.length, 0);
  assert.equal(unpaired.ui.batchTotal, 0);

  const offline = await pageWith({}, ['https://example.com/a', 'https://example.com/b']);
  offline.ui.runtime.client = undefined;
  await offline.ui.sendAll();
  assert.match(offline.ui.error, /配对电脑/);
  assert.equal(offline.ui.batchTotal, 0);
  assert.equal(offline.ui.drafts.length, 2);
});

test('全部发送不可重入；进行中的单条发送被批量避开而不重复', async () => {
  const gates = new Map();
  const fixture = await pageWith({ send: body => { const entry = deferred(); gates.set(body.requestId, entry); return entry.promise; } },
    ['https://example.com/a', 'https://example.com/b']);
  const { ui } = fixture;
  const [a, b] = ui.drafts;
  const single = ui.sendDraft(b);
  await until(() => gates.has(b.requestId));
  const batch = ui.sendAll();
  await until(() => gates.has(a.requestId));
  const again = ui.sendAll();
  await again;
  assert.equal(fixture.stats.sends.filter(id => id === a.requestId).length, 1, '重复触发不会再次发送');
  assert.equal(ui.batchTotal, 2, '第二次调用被忽略，进度保持第一次的总数');
  gates.get(a.requestId).resolve(receipt(a.requestId));
  await until(() => ui.batchTotal === 0);
  gates.get(b.requestId).resolve(receipt(b.requestId));
  await single; await batch;
  assert.equal(fixture.stats.sends.filter(id => id === b.requestId).length, 1, '单条在途的草稿没有被批量重复发送');
  assert.equal(ui.drafts.length, 0);
  assert.equal(ui.error, '');
});

test('全部发送：页面忙碌（配对、解绑等占用页面锁的操作进行中）时不启动，也不做会话预检', async () => {
  const fixture = await pageWith({}, ['https://example.com/a', 'https://example.com/b']);
  fixture.ui.busy = true;
  await fixture.ui.sendAll();
  assert.equal(fixture.stats.sends.length, 0);
  assert.equal(fixture.stats.sessions, 0);
  assert.equal(fixture.ui.batchTotal, 0);
  fixture.ui.busy = false;
  await fixture.ui.sendAll();
  assert.equal(fixture.stats.sends.length, 2, '空闲后可正常发送');
});
// ---------- 保存并发送 ----------
test('保存并发送：先落盘成草稿并收起面板，再后台发送；页面锁在本地保存后即释放', async () => {
  const wait = deferred();
  let stateAtSend;
  const fixture = await pageWith({ send: body => {
    const draft = fixture.queue.snapshot().drafts[0];
    stateAtSend = { requestId: body.requestId, storedId: draft.requestId, attempted: draft.attempted, input: draft.input };
    return wait.promise;
  } }, []);
  const { ui, queue } = fixture;
  ui.input = 'https://example.com/new'; ui.mode = 'auto'; ui.showEditor = true;
  await ui.act(() => ui.saveAndSend());
  assert.equal(ui.showEditor, false);
  assert.equal(ui.input, '');
  assert.equal(ui.busy, false, '网络发送不占页面锁');
  await until(() => stateAtSend !== undefined);
  assert.equal(ui.sendings.length, 1);
  assert.equal(stateAtSend.storedId, stateAtSend.requestId, '发出的就是已落盘的那条草稿');
  assert.equal(stateAtSend.attempted, true, '发出请求前已先落盘“已尝试”');
  assert.equal(stateAtSend.input, 'https://example.com/new');
  wait.resolve(receipt(stateAtSend.requestId));
  await until(() => ui.sendings.length === 0);
  assert.equal(queue.snapshot().drafts.length, 0);
  assert.equal(ui.notice, '中转已接收，电脑上线后将处理');
});

test('保存并发送：保存失败时不发送；发送失败时草稿保留并带原因', async () => {
  const empty = await pageWith({}, []);
  empty.ui.input = '   '; empty.ui.showEditor = true;
  await empty.ui.act(() => empty.ui.saveAndSend());
  assert.match(empty.ui.error, /请输入链接或文字/);
  assert.equal(empty.stats.sends.length, 0);
  assert.equal(empty.ui.showEditor, true, '保存失败时面板保留');

  const failing = await pageWith({ send: async () => { throw new RelayRequestError('内容超过 32 KiB，请拆分后保存', 413, 'request_failed'); } }, []);
  failing.ui.input = 'https://example.com/x'; failing.ui.showEditor = true;
  await failing.ui.act(() => failing.ui.saveAndSend());
  await until(() => failing.ui.sendings.length === 0);
  assert.equal(failing.queue.snapshot().drafts.length, 1, '发送失败的草稿保留');
  assert.match(failing.ui.error, /32 KiB/);
  assert.equal(failing.queue.snapshot().drafts[0].attempted, false, '413 可证明未保存，仍可编辑');
});

// ---------- 分享后自动发送 ----------
async function shared(overrides = {}) {
  const fixture = await pageWith({}, ['https://example.com/shared']);
  Object.assign(fixture.ui, { ready: true, connected: true, foreground: true, visible: true }, overrides);
  fixture.ui.settings = { autoSendShare: true, ...(overrides.settings ?? {}) };
  fixture.ui.sharedId = fixture.ui.drafts[0].requestId;
  return fixture;
}

test('分享后自动发送：开启且已连接、位于前台时发送刚落盘的那一条', async () => {
  const fixture = await shared();
  fixture.ui.autoSendShared();
  await until(() => fixture.ui.drafts.length === 0);
  assert.equal(fixture.stats.sends.length, 1);
  assert.equal(fixture.ui.notice, '中转已接收，电脑上线后将处理');
  fixture.ui.autoSendShared();
  assert.equal(fixture.stats.sends.length, 1, '同一次分享只发送一次');
});

test('分享后自动发送：设置关闭或未连接时该次分享作废，之后开启设置或连接也不会回头发送', async () => {
  const off = await shared({ settings: { autoSendShare: false } });
  off.ui.autoSendShared();
  off.ui.settings = { autoSendShare: true };
  off.ui.autoSendShared();
  await settle();
  assert.equal(off.stats.sends.length, 0);
  assert.equal(off.ui.drafts.length, 1, '草稿保留，可手动发送');

  const disconnected = await shared({ connected: false });
  disconnected.ui.autoSendShared();
  disconnected.ui.connected = true;
  disconnected.ui.autoSendShared();
  await settle();
  assert.equal(disconnected.stats.sends.length, 0);
});

test('分享后自动发送：页面、设置或前台状态暂不具备时保留待办，条件满足后再发送', async () => {
  for (const [name, overrides] of [['未在前台', { foreground: false }], ['页面未就绪', { ready: false }],
    ['设置未读完', { settingsReady: false }], ['页面不可见', { visible: false }]]) {
    const fixture = await shared(overrides);
    fixture.ui.autoSendShared();
    await settle();
    assert.equal(fixture.stats.sends.length, 0, `${name}时不发送`);
    Object.assign(fixture.ui, { foreground: true, ready: true, settingsReady: true, visible: true });
    fixture.ui.foregroundChanged();
    await until(() => fixture.ui.drafts.length === 0);
    assert.equal(fixture.stats.sends.length, 1, `${name}恢复后发送一次`);
  }
});

test('分享后自动发送：草稿已被删除时安静结束；新的分享编号会再次触发', async () => {
  const fixture = await shared();
  const gone = fixture.ui.sharedId;
  await fixture.queue.remove(gone); fixture.ui.drafts = [];
  fixture.ui.autoSendShared();
  await settle();
  assert.equal(fixture.stats.sends.length, 0);
  assert.equal(fixture.ui.error, '');
  await fixture.queue.edit('https://example.com/second', 'auto'); const next = uuid(); await fixture.queue.enqueue(next);
  fixture.ui.sharedId = next;
  fixture.ui.autoSendShared();
  await until(() => fixture.queue.snapshot().drafts.length === 0);
  assert.deepEqual(fixture.stats.sends, [next]);
});

// ---------- 超过 20 个链接：拆分保存 ----------
const manyLinks = count => Array.from({ length: count }, (_, i) => `https://example.com/${i}`);

test('拆分保存：45 个链接拆成 20/20/5 三条链接草稿，一次落盘，收起面板并清空输入', async () => {
  const fixture = await pageWith({}, []);
  const { ui, queue, store } = fixture;
  const links = manyLinks(45);
  ui.input = links.join('\n'); ui.mode = 'auto'; ui.showEditor = true;
  await ui.act(() => ui.saveSplit());
  const drafts = queue.snapshot().drafts;
  assert.equal(ui.error, '');
  assert.deepEqual(Array.from(drafts, draft => draft.input.split('\n').length), [20, 20, 5]);
  assert.deepEqual(Array.from(drafts).flatMap(draft => draft.input.split('\n')), links, '顺序与内容完整保留');
  assert.deepEqual(Array.from(drafts, draft => draft.mode), ['urls', 'urls', 'urls']);
  assert.equal(new Set(Array.from(drafts, draft => draft.requestId)).size, 3, '每份都有独立的请求编号');
  assert.equal(store.state.drafts.length, 3);
  assert.equal(store.state.editor, '');
  assert.equal(ui.input, '');
  assert.equal(ui.showEditor, false);
  assert.equal(ui.notice, '已按每份 20 个链接拆成 3 条草稿（共 45 个链接），确认后即可发送');
  assert.equal(fixture.stats.sends.length, 0, '只保存，不自动发送');
});

test('拆分保存：写入失败时一条都不保存，输入与面板保留并显示原因', async () => {
  const inputs = manyLinks(99);
  const fixture = await pageWith({}, inputs);
  const { ui, queue } = fixture;
  const links = manyLinks(45).map(link => link + '?full');
  ui.input = links.join('\n'); ui.mode = 'auto'; ui.showEditor = true;
  await ui.act(() => ui.saveSplit());
  assert.equal(queue.snapshot().drafts.length, 99, '3 份会超过 100 条上限，整体拒绝');
  assert.match(ui.error, /100 条/);
  assert.equal(ui.showEditor, true);
  assert.equal(ui.input, links.join('\n'));
});

test('拆分保存：内容不需要拆分时给出提示，不产生任何草稿', async () => {
  const fixture = await pageWith({}, []);
  const { ui, queue } = fixture;
  ui.input = manyLinks(20).join('\n'); ui.mode = 'auto'; ui.showEditor = true;
  await ui.act(() => ui.saveSplit());
  assert.equal(ui.error, '当前内容不需要拆分，请直接保存');
  assert.equal(queue.snapshot().drafts.length, 0);
  assert.equal(ui.showEditor, true);
});

test('拆分保存：待触发的防抖写入会被取消，不会把原文重新写回输入框', async () => {
  const fixture = await pageWith({}, []);
  const { ui, time, queue } = fixture;
  ui.input = manyLinks(25).join('\n'); ui.mode = 'urls'; ui.showEditor = true;
  ui.persistEditor();
  assert.ok([...time.timers.values()].some(timer => timer.delay === 400), '输入后登记了防抖写入');
  await ui.act(() => ui.saveSplit());
  for (const timer of [...time.timers.values()]) timer.fn();
  await settle();
  assert.equal(queue.snapshot().editor, '', '过期计时器没有把已拆分的原文写回');
  assert.equal(queue.snapshot().drafts.length, 2);
});