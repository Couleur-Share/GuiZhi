const test = require('node:test');
const assert = require('node:assert/strict');
const { deferred, load, check, sentLog, historyView } = require('./support.cjs');
const { receipt, pageWith } = require('./page-fixture.cjs');

const { RelayRequestError } = check;
// 清除记录、解除配对等对话框逻辑会调用 panelMaterial；其余系统边界由 createPage 提供。
const dialogImports = { '../services/ImmersiveAppearance': { panelMaterial: () => 'material', CaptureTabsAppearance: class {} } };

// ---------- SentLogStore：Preferences 边界 ----------

function loadStore(failOpen = 0) {
  const backing = new Map(), opened = [], flushed = [];
  const state = { failOpen, failPut: false };
  const preferences = { getPreferences: async (_context, { name }) => {
    opened.push(name);
    if (state.failOpen > 0) { state.failOpen--; throw new Error('打不开存储'); }
    return {
      get: async (key, fallback) => backing.has(key) ? backing.get(key) : fallback,
      put: async (key, value) => { if (state.failPut) throw new Error('写入失败'); backing.set(key, value); },
      flush: async () => { flushed.push(backing.get('sent')); },
    };
  } };
  const { SentLogStore } = load('services/SentLogStore.ets', { '@kit.AbilityKit': {}, '@kit.ArkData': { preferences }, '../core/SentLog': sentLog });
  return { store: new SentLogStore({}), backing, opened, flushed, state };
}

test('发送记录存储：首次读取为空；写入后立即 flush，重新读取得到同样的内容；独立文件且只打开一次', async () => {
  const { store, opened, flushed, backing } = loadStore();
  const first = await store.load();
  assert.equal(first.entries.length, 0);
  assert.equal(first.damaged, false);
  const entry = sentLog.buildSentEntry('https://v.douyin.com/a', 'auto', receipt('r1'), 5);
  await store.save([entry]);
  assert.equal(flushed.length, 1, '写入后必须 flush 才算落盘');
  assert.equal(flushed[0], backing.get('sent'));
  const loaded = await store.load();
  assert.deepEqual(Array.from(loaded.entries, row => row.requestId), ['r1']);
  assert.equal(loaded.damaged, false);
  assert.deepEqual(opened, ['guizhi_capture_sent_v1'], '使用独立于草稿的文件，Preferences 只打开一次并复用');
});

test('发送记录存储：打开失败时本次读取失败，下次读写会重试打开；写入失败向上抛出且不 flush；内容损坏按损坏读取', async () => {
  const { store, opened, flushed, state, backing } = loadStore(1);
  await assert.rejects(store.load(), /打不开存储/);
  assert.equal((await store.load()).damaged, false, '下一次读取重新打开并成功');
  assert.equal(opened.length, 2);
  state.failPut = true;
  await assert.rejects(store.save([]), /写入失败/);
  assert.equal(flushed.length, 0);
  state.failPut = false;
  backing.set('sent', '{坏数据');
  const damaged = await store.load();
  assert.equal(damaged.damaged, true);
  assert.equal(damaged.entries.length, 0);
});

// ---------- 发送后留下记录 ----------

test('单条发送成功：本机留下内容预览与来源、链接与条数，并落盘；记录正常时没有警告', async () => {
  const { ui, time, sentPort } = await pageWith({ send: async body => ({ ...receipt(body.requestId), itemCount: 2 }) },
    ['https://v.douyin.com/a\nhttps://v.douyin.com/b']);
  const draft = ui.drafts[0];
  assert.equal(await ui.sendDraft(draft), true);
  assert.equal(ui.sentEntries.length, 1);
  const entry = ui.sentEntries[0];
  assert.equal(entry.requestId, draft.requestId);
  assert.equal(entry.preview, 'https://v.douyin.com/a');
  assert.equal(entry.platform, '抖音');
  assert.equal(entry.itemCount, 2);
  assert.equal(entry.sentAt, time.now());
  assert.deepEqual(Array.from(entry.urls), ['https://v.douyin.com/a', 'https://v.douyin.com/b']);
  assert.equal(sentPort.saves.length, 1);
  assert.equal(sentPort.saves[0][0].requestId, draft.requestId);
  assert.equal(ui.sentLogWarning, '');
  // 近期页合并后，同一编号的回执显示预览与来源。
  const items = historyView.mergeHistory([{ ...receipt(draft.requestId), itemCount: 2 }], ui.sentEntries);
  assert.equal(items[0].title, 'https://v.douyin.com/a');
  assert.equal(items[0].source, '抖音');
});

test('文字草稿发送后同样留下预览，但不保存链接、没有来源', async () => {
  const { ui } = await pageWith({}, ['今天读到一句话，值得记下来\n第二行不进入预览']);
  assert.equal(await ui.sendDraft(ui.drafts[0]), true);
  assert.equal(ui.sentEntries[0].preview, '今天读到一句话，值得记下来');
  assert.equal(ui.sentEntries[0].platform, '');
  assert.equal(ui.sentEntries[0].urls.length, 0);
});

test('发送失败不留记录：只有回执有效、草稿已移除的发送才进入发送记录', async () => {
  const { ui, sentPort } = await pageWith({ send: async () => { throw new RelayRequestError('内容格式不正确', 400, 'invalid_input'); } });
  assert.equal(await ui.sendDraft(ui.drafts[0]), false);
  assert.equal(ui.sentEntries.length, 0);
  assert.equal(sentPort.saves.length, 0);
});

test('发送记录写入失败不影响发送结果：草稿已移除、提示仍为成功，只在近期页留下警告；恢复后下一次成功写入清除警告', async () => {
  const { ui, queue, sentPort } = await pageWith({}, ['https://example.com/a', 'https://example.com/b']);
  const [first, second] = ui.drafts;
  sentPort.failSave = new Error('磁盘已满');
  assert.equal(await ui.sendDraft(first), true);
  assert.equal(ui.error, '');
  assert.equal(ui.notice, '中转已接收，电脑上线后将处理');
  assert.deepEqual(Array.from(queue.snapshot().drafts, row => row.requestId), [second.requestId]);
  assert.match(ui.sentLogWarning, /发送记录未能保存：磁盘已满/);
  assert.equal(ui.sentEntries.length, 1, '内存中仍保留，本次运行内近期页可用');
  sentPort.failSave = null;
  assert.equal(await ui.sendDraft(second), true);
  assert.equal(ui.sentLogWarning, '');
  assert.equal(ui.sentEntries.length, 2);
  assert.equal(sentPort.saves.at(-1).length, 2, '落盘内容包含此前只在内存中的那条');
});

test('全部发送：只为成功的条目留下记录（包括失败条目之后继续发送的成功项）', async () => {
  const send = async body => {
    if (body.input.includes('bad')) throw new RelayRequestError('内容格式不正确', 400, 'invalid_input');
    return receipt(body.requestId);
  };
  const { ui, sentPort } = await pageWith({ send }, ['https://a.example.com/1', 'https://bad.example.com/2', 'https://c.example.com/3']);
  const ids = Array.from(ui.drafts, row => row.requestId);
  await ui.sendAll();
  assert.deepEqual(Array.from(ui.sentEntries, row => row.requestId), [ids[0], ids[2]]);
  assert.deepEqual(Array.from(ui.sentEntries, row => row.platform), ['a.example.com', 'c.example.com']);
  assert.match(ui.error, /失败 1 条/);
  assert.equal(ui.sentLogWarning, '');
  assert.equal(sentPort.saves.length, 2);
});

test('全部发送：发送记录写入失败不改变批量结论与后续条目，警告显示在近期页', async () => {
  const { ui, sentPort, queue } = await pageWith({}, ['https://a.example.com/1', 'https://b.example.com/2', 'https://c.example.com/3']);
  sentPort.failSave = new Error('磁盘已满');
  await ui.sendAll();
  assert.equal(ui.error, '');
  assert.match(ui.notice, /已发送 3 条/);
  assert.equal(queue.snapshot().drafts.length, 0, '三条都已发送并移除');
  assert.equal(ui.sentEntries.length, 3);
  assert.match(ui.sentLogWarning, /磁盘已满/);
  assert.equal(ui.sendings.length, 0, '没有残留的发送中状态');
  assert.equal(ui.batchTotal, 0);
});

// ---------- 清除发送记录 ----------

function dialog(ui, index) {
  const shown = [];
  ui.getUIContext = () => ({ getPromptAction: () => ({ showDialog: async options => { shown.push(options); return { index }; } }) });
  return shown;
}

test('清除发送记录：取消不改动；确认后清空、落盘并提示，警告一并消除', async () => {
  const { ui, sentPort } = await pageWith({}, ['https://a.example.com/1'], dialogImports);
  await ui.sendDraft(ui.drafts[0]);
  ui.sentLogWarning = '此前的警告';
  const cancelled = dialog(ui, 0);
  await ui.act(() => ui.clearSentLog());
  assert.equal(cancelled.length, 1);
  assert.match(cancelled[0].message, /草稿与电脑上已收集的内容不受影响/);
  assert.equal(ui.sentEntries.length, 1, '取消后记录仍在');
  assert.equal(ui.sentLogWarning, '此前的警告');

  dialog(ui, 1);
  await ui.act(() => ui.clearSentLog());
  assert.equal(ui.sentEntries.length, 0);
  assert.equal(ui.notice, '已清除本机发送记录');
  assert.equal(ui.error, '');
  assert.equal(ui.sentLogWarning, '');
  assert.deepEqual(sentPort.saves.at(-1), []);
});

test('清除发送记录：落盘失败时记录保留并显示原因，不谎称已清除', async () => {
  const { ui, sentPort } = await pageWith({}, ['https://a.example.com/1'], dialogImports);
  await ui.sendDraft(ui.drafts[0]);
  sentPort.failSave = new Error('写入失败');
  dialog(ui, 1);
  await ui.act(() => ui.clearSentLog());
  assert.equal(ui.error, '写入失败');
  assert.equal(ui.sentEntries.length, 1);
  assert.notEqual(ui.notice, '已清除本机发送记录');
});

// ---------- 加载更多 ----------

const stamped = index => ({ ...receipt(`r${index}`), createdAt: 1_000_000 - index * 10 });
function relayOf(total) {
  const all = Array.from({ length: total }, (_, index) => stamped(index));
  return { all, page: before => all.filter(row => !before || row.createdAt < before).slice(0, 100) };
}
async function historyPage(total, extraImports = {}) {
  const relay = relayOf(total), calls = [];
  const fixture = await pageWith({ history: async before => { calls.push(before ?? 0); return relay.page(before); } }, undefined, extraImports);
  fixture.ui.applyHistory(relay.page(0));
  return { ...fixture, relay, calls };
}

test('最新一页取满时提示还有更多；加载更多以最早一条的创建时间为游标，取尽后不再请求', async () => {
  const { ui, relay, calls } = await historyPage(250);
  assert.equal(ui.history.length, 100);
  assert.equal(ui.historyMore, true);
  assert.equal(ui.historyLoaded, true);
  await ui.loadMoreHistory();
  assert.deepEqual(calls, [relay.all[99].createdAt]);
  assert.equal(ui.history.length, 200);
  assert.equal(ui.historyMore, true);
  await ui.loadMoreHistory();
  assert.equal(ui.history.length, 250);
  assert.equal(ui.historyMore, false);
  assert.equal(ui.historyLoadingMore, false);
  await ui.loadMoreHistory();
  assert.equal(calls.length, 2, '已到尽头不再请求');
  assert.deepEqual(Array.from(ui.history, row => row.requestId), relay.all.map(row => row.requestId));
});

test('记录不足一页时没有“加载更多”，也不会发出请求', async () => {
  const { ui, calls } = await historyPage(40);
  assert.equal(ui.historyMore, false);
  await ui.loadMoreHistory();
  assert.equal(calls.length, 0);
});

test('加载更多失败：保留已加载内容与“还有更多”状态，页面顶部给出原因，可再次点击重试成功', async () => {
  const { ui, client } = await historyPage(150);
  const original = client.history;
  client.history = async () => { throw new Error('网络不通'); };
  await ui.loadMoreHistory();
  assert.equal(ui.error, '加载更多记录失败：网络不通');
  assert.equal(ui.history.length, 100);
  assert.equal(ui.historyMore, true);
  assert.equal(ui.historyLoadingMore, false, '失败后按钮恢复可点');
  client.history = original;
  ui.error = '';
  await ui.loadMoreHistory();
  assert.equal(ui.error, '');
  assert.equal(ui.history.length, 150);
  assert.equal(ui.historyMore, false);
});

test('加载更多单次在途：请求返回前的重复点击被忽略，只发一次请求', async () => {
  const { ui, client, relay } = await historyPage(150);
  const wait = deferred(), requests = [];
  client.history = before => { requests.push(before); return wait.promise; };
  const first = ui.loadMoreHistory();
  assert.equal(ui.historyLoadingMore, true);
  // 重复点击应立即返回；先放行在途请求再等待，避免错误实现（重复发请求）表现为测试挂起。
  const second = ui.loadMoreHistory();
  wait.resolve(relay.page(requests[0]));
  await Promise.all([first, second]);
  assert.equal(requests.length, 1);
  assert.equal(ui.history.length, 150);
  assert.equal(ui.historyLoadingMore, false);
});

test('加载更多：返回时连接已变化（页面锁操作使修订号前进，或客户端已更换）的旧响应被丢弃，状态复位', async () => {
  for (const change of [ui => { ui.checkRevision++; }, ui => { ui.runtime.client = { history: async () => [] }; }]) {
    const { ui, client, relay } = await historyPage(150);
    const wait = deferred();
    client.history = before => { void before; return wait.promise; };
    const pending = ui.loadMoreHistory();
    change(ui);
    wait.resolve(relay.page(relay.all[99].createdAt));
    await pending;
    assert.equal(ui.history.length, 100, '旧响应不得写入列表');
    assert.equal(ui.historyMore, true);
    assert.equal(ui.historyLoadingMore, false);
    assert.equal(ui.error, '');
  }
});

test('下拉刷新与发送后的自动刷新只替换最新一页，已经翻出的更早记录保留', async () => {
  const { ui } = await historyPage(250);
  await ui.loadMoreHistory();
  assert.equal(ui.history.length, 200);
  await ui.refresh(true, true);
  assert.equal(ui.history.length, 200);
  assert.equal(ui.historyMore, true);
  await ui.refreshHistoryAfterSend();
  assert.equal(ui.history.length, 200);
  assert.equal(ui.statusError, '');
});

test('解除配对后清空记录并重置分页状态', async () => {
  const { ui } = await historyPage(150, dialogImports);
  dialog(ui, 1);
  ui.runtime.disconnect = async () => {};
  await ui.disconnect(false);
  assert.equal(ui.history.length, 0);
  assert.equal(ui.historyLoaded, false);
  assert.equal(ui.historyMore, false);
});

// ---------- 在浏览器打开 ----------

test('在浏览器打开：交给系统按链接打开（允许回落到默认浏览器）；打不开时说明原因并提示可复制', async () => {
  const { ui } = await pageWith();
  const opened = [];
  ui.getUIContext = () => ({ getHostContext: () => ({ openLink: async (url, options) => { opened.push([url, options]); } }) });
  await ui.openUrl('https://example.com/x');
  assert.equal(opened.length, 1);
  assert.equal(opened[0][0], 'https://example.com/x');
  assert.equal(opened[0][1].appLinkingOnly, false, '允许在没有匹配应用时回落到默认浏览器');
  assert.equal(ui.error, '');

  ui.getUIContext = () => ({ getHostContext: () => ({ openLink: async () => { throw Object.assign(new Error('没有可用的应用'), { code: 16000019 }); } }) });
  await ui.openUrl('https://example.com/x');
  assert.equal(ui.error, '无法打开链接：没有可用的应用。可改用“复制链接”');
});

// ---------- 启动 ----------

test('启动时读取本机发送记录：条目进入近期页数据，读取问题作为警告显示', async () => {
  const stored = sentLog.buildSentEntry('https://v.douyin.com/a', 'auto', receipt('r1'), 1);
  const log = new sentLog.SentLog({ load: async () => ({ entries: [stored], damaged: true }), save: async () => {} });
  await log.open();
  const runtime = { connection: undefined, client: undefined, sentLog: log,
    queue: { snapshot: () => ({ editor: '', mode: 'auto', drafts: [] }) } };
  const { ui } = await pageWith({}, [], { '../services/CaptureRuntime': { CaptureRuntime: { open: async () => runtime } } });
  ui.getUIContext = () => ({ getHostContext: () => ({}) });
  await ui.initialize();
  assert.equal(ui.ready, true);
  assert.deepEqual(Array.from(ui.sentEntries, row => row.requestId), ['r1']);
  assert.match(ui.sentLogWarning, /无法读取/);
});
