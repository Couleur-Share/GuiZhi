const test = require('node:test');
const assert = require('node:assert/strict');
const { deferred, settle, desktopStatus } = require('./support.cjs');
const { receipt, pageWith } = require('./page-fixture.cjs');

const {
  DESKTOP_ONLINE_LIMIT_MS, DESKTOP_RECENT_LIMIT_MS, DESKTOP_POLL_INTERVAL_MS,
  desktopStatusOf, agoLabel, waitingCount, DesktopPoll,
} = desktopStatus;
const MINUTE = 60000, HOUR = 3600000, DAY = 86400000;
// 解除配对对话框会调用 panelMaterial；其余系统边界由 createPage 提供。
const dialogImports = { '../services/ImmersiveAppearance': { panelMaterial: () => 'material', CaptureTabsAppearance: class {} } };

// ---------- 纯函数：阈值与文案 ----------

test('阈值常量与文档一致：90 秒内在线、6 分钟内算最近、前台每 30 秒查询一次', () => {
  assert.equal(DESKTOP_ONLINE_LIMIT_MS, 90000);
  assert.equal(DESKTOP_RECENT_LIMIT_MS, 360000);
  assert.equal(DESKTOP_POLL_INTERVAL_MS, 30000);
});

test('电脑状态按阈值分级：在线/最近/离线的分界值逐个验证，服务端时钟回拨得到的负数也算在线', () => {
  const rows = [
    // [距上次取件毫秒, 状态, 标题, 语义色]
    [0, 'online', '电脑在线', 'success'],
    [90000, 'online', '电脑在线', 'success'],
    [90001, 'recent', '最近取件 1 分钟前', 'neutral'],
    [150000, 'recent', '最近取件 2 分钟前', 'neutral'],
    [360000, 'recent', '最近取件 6 分钟前', 'neutral'],
    [360001, 'offline', '电脑未在取件', 'warning'],
    [-5000, 'online', '电脑在线', 'success'],
  ];
  for (const [ago, presence, headline, tone] of rows) {
    const status = desktopStatusOf(ago, 0);
    assert.equal(status.presence, presence, `ago=${ago}`);
    assert.equal(status.headline, headline, `ago=${ago}`);
    assert.equal(status.tone, tone, `ago=${ago}`);
    assert.equal(status.visible, true, `ago=${ago}`);
  }
});

test('不知道就不显示：字段缺失（旧中转）与格式异常都按未知处理，不能说成“离线”', () => {
  for (const value of [undefined, NaN, Infinity, -Infinity, '5', true, {}]) {
    const status = desktopStatusOf(value, 3);
    assert.equal(status.presence, 'unknown', String(value));
    assert.equal(status.visible, false, String(value));
    assert.equal(status.summary, '', String(value));
    assert.equal(status.headline, '', String(value));
    assert.equal(status.detail, '', String(value));
    assert.equal(status.tone, 'neutral', String(value));
  }
});

test('从未取件（null）单独成态：中性色，说明内容暂存在中转及保留期限', () => {
  const status = desktopStatusOf(null, 0);
  assert.equal(status.presence, 'never');
  assert.equal(status.visible, true);
  assert.equal(status.headline, '电脑尚未取件');
  assert.equal(status.tone, 'neutral');
  assert.equal(status.summary, '电脑尚未取件 · 内容暂存在中转');
  assert.match(status.detail, /电脑打开归知并开启手机收集后会自动取件/);
  assert.match(status.detail, /最长保留 30 天/);
});

test('离线说明：给出上次取件的时间跨度与暂存承诺；在线与最近取件没有补充说明', () => {
  assert.match(desktopStatusOf(360001, 0).detail, /^上次取件在 6 分钟前。/);
  assert.match(desktopStatusOf(2 * HOUR + 59 * MINUTE, 0).detail, /^上次取件在 2 小时前。/);
  assert.match(desktopStatusOf(3 * DAY + 5 * HOUR, 0).detail, /^上次取件在 3 天前。/);
  assert.match(desktopStatusOf(HOUR, 0).detail, /电脑可能关机、断网或暂停了手机收集；发送的内容先暂存在中转，最长保留 30 天，电脑恢复后会自动取件。$/);
  assert.equal(desktopStatusOf(0, 0).detail, '');
  assert.equal(desktopStatusOf(120000, 0).detail, '');
});

test('时间跨度口语化：不足一分钟按一分钟，分钟/小时/天逐档向下取整', () => {
  const rows = [
    [0, '1 分钟'], [59999, '1 分钟'], [MINUTE, '1 分钟'], [2 * MINUTE - 1, '1 分钟'], [2 * MINUTE, '2 分钟'],
    [HOUR - 1, '59 分钟'], [HOUR, '1 小时'], [DAY - 1, '23 小时'], [DAY, '1 天'], [10 * DAY + 3 * HOUR, '10 天'],
  ];
  for (const [ms, label] of rows) assert.equal(agoLabel(ms), label, `ms=${ms}`);
});

test('状态行文案：状态 ·（电脑没在取件时）内容暂存说明 ·（有待取件时）等待条数', () => {
  assert.equal(desktopStatusOf(1000, 0).summary, '电脑在线');
  assert.equal(desktopStatusOf(1000, 3).summary, '电脑在线 · 等待电脑取件 3 条');
  assert.equal(desktopStatusOf(200000, 1).summary, '最近取件 3 分钟前 · 等待电脑取件 1 条');
  assert.equal(desktopStatusOf(400000, 0).summary, '电脑未在取件 · 内容暂存在中转');
  assert.equal(desktopStatusOf(400000, 2).summary, '电脑未在取件 · 内容暂存在中转 · 等待电脑取件 2 条');
  assert.equal(desktopStatusOf(null, 4).summary, '电脑尚未取件 · 内容暂存在中转 · 等待电脑取件 4 条');
});

test('等待条数容错：负数与非数字按 0，小数取整；状态未知时仍保留条数但不显示', () => {
  assert.equal(desktopStatusOf(1000, -2).waiting, 0);
  assert.equal(desktopStatusOf(1000, NaN).waiting, 0);
  assert.equal(desktopStatusOf(1000, 2.9).waiting, 2);
  const unknown = desktopStatusOf(undefined, 5);
  assert.equal(unknown.waiting, 5);
  assert.equal(unknown.summary, '');
});

test('等待电脑取件的条数只数“中转已接收、电脑尚未确认”的回执', () => {
  const rows = [receipt('a'), { ...receipt('b'), state: 'received' }, receipt('c'), { ...receipt('d'), state: 'expired' }];
  assert.equal(waitingCount(rows), 2);
  assert.equal(waitingCount([]), 0);
});

test('前台查询节奏：首次立即到期，之后满 30 秒才到期；时钟回拨或重置后立即到期', () => {
  const poll = new DesktopPoll();
  assert.equal(poll.due(0), true, '尚未查询过');
  poll.started(0);
  assert.equal(poll.due(0), false, '时刻 0 也是一次有效的查询记录，不与“未查询”混淆');
  poll.started(1000);
  assert.equal(poll.due(1000), false);
  assert.equal(poll.due(30999), false);
  assert.equal(poll.due(31000), true, '恰好满 30 秒');
  assert.equal(poll.due(999), true, '时钟回拨不能让节奏停在“未来”');
  poll.reset();
  assert.equal(poll.due(1000), true);
});

// ---------- 页面：会话检查与状态 ----------

const online = ago => ({ paired: true, deviceId: 'd', desktopSeenAgoMs: ago });

// 会话按脚本依次返回（最后一项重复）；Error 项表示这次查询失败。history 按调用次数计数。
async function polled(sessions, options = {}) {
  const script = sessions.slice(), calls = { session: 0, history: 0 };
  const fixture = await pageWith({
    session: async () => {
      calls.session++;
      const next = script.length > 1 ? script.shift() : script[0];
      if (next instanceof Error) throw next;
      return next;
    },
    history: async () => { calls.history++; return options.history ? options.history() : []; },
  }, options.inputs ?? [], dialogImports);
  return { ...fixture, calls };
}

test('已配对后自动查询每 30 秒一次：未到期不请求，到期后更新电脑状态；手动刷新不受限制', async () => {
  const { ui, time, calls } = await polled([online(5000), online(200000), online(500000)]);
  await ui.refresh(false, false);
  assert.equal(calls.session, 1);
  assert.equal(ui.desktop.presence, 'online');
  time.advance(29999);
  await ui.refresh(false, false);
  assert.equal(calls.session, 1, '未满 30 秒不请求');
  time.advance(1);
  await ui.refresh(false, false);
  assert.equal(calls.session, 2);
  assert.equal(ui.desktop.presence, 'recent');
  await ui.refresh(true, false);
  assert.equal(calls.session, 3, '手动刷新不受 30 秒限制');
  assert.equal(ui.desktop.presence, 'offline');
});

test('后台查询保持安静：请求期间不转圈也不禁用按钮；上一次未返回时不重叠', async () => {
  const wait = deferred();
  let count = 0;
  const { ui, time } = await polled([online(1000)]);
  ui.runtime.client.session = () => { count++; return wait.promise; };
  const first = ui.refresh(false, false);
  assert.equal(count, 1);
  assert.equal(ui.refreshing, false, '后台查询不能让标题转圈或按钮变灰');
  assert.equal(ui.silentRefreshing, true);
  time.advance(60000);
  const second = ui.refresh(false, false);
  assert.equal(count, 1, '在途时不重叠');
  // 先放行在途请求再等待，避免错误实现（重叠发请求）表现为测试挂起。
  wait.resolve(online(1000));
  await Promise.all([first, second]);
  assert.equal(count, 1);
  assert.equal(ui.silentRefreshing, false);
  assert.equal(ui.desktop.presence, 'online');
  // 手动检查则是可见的：转圈，且可以与后台查询区分。
  const manual = deferred();
  ui.runtime.client.session = () => manual.promise;
  const visible = ui.refresh(true, false);
  assert.equal(ui.refreshing, true);
  manual.resolve(online(1000));
  await visible;
  assert.equal(ui.refreshing, false);
});

test('尚未确认配对时沿用原有节奏（不受 30 秒限制）；确认后立即显示电脑状态', async () => {
  const { ui, calls } = await polled([{ paired: false, deviceId: 'd' }, online(1000)]);
  ui.paired = false;
  await ui.refresh(false, false);
  assert.equal(ui.desktop.visible, false, '待确认的设备没有电脑状态');
  await ui.refresh(false, false);
  assert.equal(calls.session, 2, '等待确认期间不受 30 秒限制');
  assert.equal(ui.paired, true);
  assert.equal(ui.desktop.presence, 'online');
  assert.equal(ui.notice, '绑定成功，可以开始收集了');
});

test('页面不可见或应用不在前台时不自动查询；手动刷新照常', async () => {
  const { ui, calls } = await polled([online(1000)]);
  ui.visible = false;
  await ui.refresh(false, false);
  ui.visible = true; ui.foreground = false;
  await ui.refresh(false, false);
  assert.equal(calls.session, 0);
  await ui.refresh(true, false);
  assert.equal(calls.session, 1);
  assert.equal(ui.desktop.presence, 'online');
});

test('中转没有提供该字段（旧版本）时不显示；之后有了就显示，再没有又隐藏；从未取件也显示', async () => {
  const { ui, time } = await polled([{ paired: true, deviceId: 'd' }, online(1000), { paired: true, deviceId: 'd' }, online(null)]);
  await ui.refresh(true, false);
  assert.equal(ui.desktop.visible, false);
  assert.equal(ui.desktop.summary, '');
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(ui.desktop.presence, 'online');
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(ui.desktop.visible, false, '每次都以本次响应为准，不沿用旧读数');
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(ui.desktop.presence, 'never');
});

test('电脑撤销了这台手机：后台检查发现后明确提示，电脑状态消失，回到等待确认的节奏', async () => {
  const { ui, time, calls } = await polled([online(1000), { paired: false, deviceId: 'd', desktopSeenAgoMs: 1000 }]);
  await ui.refresh(true, false);
  assert.equal(ui.desktop.visible, true);
  ui.statusError = '旧的连接错误';
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(ui.paired, false);
  assert.equal(ui.desktop.visible, false, '即便中转多给了读数，未配对也不显示');
  assert.equal(ui.statusError, '', '配对已失效，旧提示不再适用');
  assert.match(ui.notice, /^与电脑的配对已失效，请在电脑「设置 → 手机收集」重新确认/);
  // 之后按未配对的节奏检查，不再受 30 秒限制，也不再重复提示。
  ui.notice = '';
  await ui.refresh(false, false);
  assert.equal(calls.session, 3);
  assert.equal(ui.notice, '');
});

// ---------- 页面：失败与提示 ----------

test('后台查询失败：电脑状态回到未知，但不弹红色横幅；恢复后重新显示', async () => {
  const { ui, time, calls } = await polled([online(1000), new Error('网络不通'), online(2000)]);
  await ui.refresh(true, false);
  assert.equal(ui.desktop.presence, 'online');
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(calls.session, 2);
  assert.equal(ui.desktop.visible, false, '不再显示过期的“在线”');
  assert.equal(ui.statusError, '', '后台失败保持安静');
  assert.equal(ui.silentRefreshing, false);
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(ui.desktop.presence, 'online');
});

test('手动检查失败仍给出明确原因，并让电脑状态回到未知', async () => {
  const { ui } = await polled([online(1000), new Error('网络不通')]);
  await ui.refresh(true, false);
  await ui.refresh(true, false);
  assert.equal(ui.statusError, '网络不通');
  assert.equal(ui.desktop.visible, false);
  assert.equal(ui.refreshing, false);
});

test('手动检查失败后连接恢复：后台查询顺带重读近期记录，成功才撤掉提示', async () => {
  const rows = [receipt('a')];
  const { ui, time, calls } = await polled([online(1000), new Error('网络不通'), online(1000)], { history: async () => rows });
  await ui.refresh(true, false);
  await ui.refresh(true, false);
  assert.equal(ui.statusError, '网络不通');
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(calls.history, 1, '有未消除的提示时重读近期记录');
  assert.equal(ui.statusError, '');
  assert.equal(ui.history.length, 1);
  assert.equal(ui.desktop.waiting, 1);
  // 没有提示时后台查询不多读近期记录。
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(calls.history, 1);
});

test('后台查询不擦掉“记录刷新失败”的提示：近期记录重读失败时提示保留，读成功才撤掉', async () => {
  let historyFails = true;
  const { ui, time, calls } = await polled([online(1000)], { history: async () => { if (historyFails) throw new Error('读取失败'); return []; } });
  ui.statusError = '提交已成功，近期记录刷新失败，请重试刷新';
  await ui.refresh(false, false);
  assert.equal(calls.history, 1);
  assert.equal(ui.statusError, '提交已成功，近期记录刷新失败，请重试刷新', '读取仍失败，提示保留且不被后台错误覆盖');
  assert.equal(ui.desktop.presence, 'online', '会话已成功，近期记录的失败不清掉电脑状态');
  historyFails = false;
  time.advance(30000);
  await ui.refresh(false, false);
  assert.equal(calls.history, 2);
  assert.equal(ui.statusError, '');
});

// ---------- 页面：等待取件条数 ----------

test('等待电脑取件的条数随近期记录变化：刷新与加载更多都会重新统计', async () => {
  const { ui } = await polled([online(1000)]);
  await ui.refresh(true, false);
  const rows = [receipt('a'), { ...receipt('b'), state: 'received' }, receipt('c')];
  ui.applyHistory(rows.map((row, index) => ({ ...row, createdAt: 1000 - index })));
  assert.equal(ui.desktop.waiting, 2);
  assert.equal(ui.desktop.summary, '电脑在线 · 等待电脑取件 2 条');
  ui.applyHistory([{ ...receipt('a'), state: 'received', createdAt: 1000 }, { ...receipt('b'), state: 'received', createdAt: 999 }]);
  assert.equal(ui.desktop.waiting, 0);
  assert.equal(ui.desktop.summary, '电脑在线');
  // 加载更多带来更早的等待项。
  ui.historyMore = true;
  ui.runtime.client.history = async () => [{ ...receipt('old'), createdAt: 10 }];
  await ui.loadMoreHistory();
  assert.equal(ui.desktop.waiting, 1);
});

test('刚发送成功即显示等待条数：发送后刷新近期记录带来新的待取件回执', async () => {
  const sent = [];
  const { ui } = await polled([online(1000)], { inputs: ['https://example.com/a'] });
  ui.runtime.client.send = async body => { sent.push(body.requestId); return receipt(body.requestId); };
  ui.runtime.client.history = async () => sent.map(id => receipt(id));
  await ui.refresh(true, false);
  await ui.sendDraft(ui.drafts[0]);
  assert.equal(ui.desktop.waiting, 1);
  assert.equal(ui.desktop.summary, '电脑在线 · 等待电脑取件 1 条');
});

test('未配对时不显示等待条数，即使近期记录里还有旧的待取件回执', async () => {
  const { ui } = await polled([online(1000)]);
  ui.applyHistory([receipt('a')]);
  assert.equal(ui.desktop.waiting, 1);
  ui.paired = false;
  ui.syncDesktop();
  assert.equal(ui.desktop.visible, false);
  assert.equal(ui.desktop.waiting, 0);
});

// ---------- 页面：定时器与预检 ----------

test('定时器：已配对时约每 2 秒醒来一次，但只有满 30 秒才查询；只有存在待取件回执时才顺带刷新近期记录', async () => {
  const { ui, time, calls } = await polled([online(1000)]);
  delete ui.startPolling;
  ui.startPolling();
  await settle();
  const [{ fn: tick, delay }] = Array.from(time.timers.values());
  assert.equal(delay, 2000);
  assert.equal(calls.session, 1, '启动时立即查询一次');
  time.advance(2000);
  tick(); await settle();
  assert.equal(calls.session, 1, '未满 30 秒');
  time.advance(28000);
  tick(); await settle();
  assert.equal(calls.session, 2);
  assert.equal(calls.history, 0, '没有待取件回执时不读近期记录');
  ui.history = [receipt('waiting')];
  time.advance(30000);
  tick(); await settle();
  assert.equal(calls.session, 3);
  assert.equal(calls.history, 1, '有待取件回执时刷新近期记录，让状态及时变为已接收');
  ui.connected = false;
  time.advance(30000);
  tick(); await settle();
  assert.equal(calls.session, 3, '未连接时定时器不查询');
});

test('尚未确认配对时定时器每次醒来都查询，不受 30 秒限制', async () => {
  const { ui, time, calls } = await polled([{ paired: false, deviceId: 'd' }]);
  ui.paired = false;
  delete ui.startPolling;
  ui.startPolling();
  await settle();
  const [{ fn: tick }] = Array.from(time.timers.values());
  for (let round = 0; round < 3; round++) { time.advance(2000); tick(); await settle(); }
  assert.equal(calls.session, 4);
});

test('发送前的会话预检也会更新电脑状态（单条与全部发送）', async () => {
  const single = await polled([online(1000)], { inputs: ['https://example.com/a'] });
  single.ui.paired = false;
  assert.equal(await single.ui.sendDraft(single.ui.drafts[0]), true);
  assert.equal(single.ui.desktop.presence, 'online');
  const batch = await polled([online(200000)], { inputs: ['https://example.com/a', 'https://example.com/b'] });
  batch.ui.paired = false;
  await batch.ui.sendAll();
  assert.equal(batch.ui.desktop.presence, 'recent');
});

test('重试配对请求确认成功时同样更新电脑状态', async () => {
  const { ui } = await polled([online(null)]);
  ui.paired = false;
  await ui.retryPair();
  assert.equal(ui.paired, true);
  assert.equal(ui.desktop.presence, 'never');
});

test('解除配对与重新配对：清掉旧的电脑状态，下一次自动检查立即到期', async () => {
  for (const action of [ui => ui.disconnect(false), ui => ui.disconnect(true), ui => ui.pair()]) {
    const { ui, time } = await polled([online(1000)]);
    Object.assign(ui.runtime, { disconnect: async () => {}, forget: async () => {}, pair: async () => {} });
    ui.getUIContext = () => ({ getPromptAction: () => ({ showDialog: async () => ({ index: 1 }) }) });
    ui.pairing = { origin: 'https://example.invalid' };
    await ui.refresh(true, false);
    assert.equal(ui.desktop.visible, true);
    time.advance(1000);
    assert.equal(ui.desktopPoll.due(time.now()), false, '刚查询过，本不该到期');
    await action(ui);
    assert.equal(ui.paired, false);
    assert.equal(ui.desktop.visible, false);
    assert.equal(ui.desktopPoll.due(time.now()), true);
  }
});
