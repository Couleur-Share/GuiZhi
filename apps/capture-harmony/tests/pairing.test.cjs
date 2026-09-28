const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const root = path.join(__dirname, '../entry/src/main/ets');
function load(file, imports = {}, globals = {}, page = false) {
  let source = fs.readFileSync(path.join(root, file), 'utf8');
  // 执行页面真实的控制方法；ArkUI Builder 由 Hvigor 编译验证，不模拟渲染。
  if (page) source = source.split('  @Builder')[0].replace('struct Index', 'export class Index')
    .replace(/@(Entry|Component|State|StorageLink|Watch|Provide)(\([^\n]*?\))?\s*/g, '') + '\n}';
  const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText;
  const scope = { exports: {}, require: name => imports[name] || {}, ...globals };
  vm.runInNewContext(js, scope); return scope.exports;
}
const check = load('core/ConnectionCheck.ts');
const { ConnectionCheck, RelayRequestError } = check;
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function clock() {
  let now = 0, id = 0; const timers = new Map();
  return { timers, now: () => now, advance: value => { now += value; }, globals: {
    Date: { now: () => now }, setInterval: (fn, delay) => { timers.set(++id, { fn, delay }); return id; },
    clearInterval: id => timers.delete(id), setTimeout: (fn, delay) => { timers.set(++id, { fn, delay }); return id; },
    clearTimeout: id => timers.delete(id), TabsController: class {},
  } };
}
function page(client) {
  const time = clock();
  const { Index } = load('pages/Index.ets', {
    '../services/ThemePalette': load('services/ThemePalette.ets', {}, { '$r': name => name }),
    '../core/ConnectionCheck': check, '../services/AppPreferences': { AppPreferences: class {} },
    '../services/HoldingHandObserver': { HoldingHandObserver: class {} },
  }, time.globals, true);
  const ui = new Index();
  ui.runtime = { client, connection: { origin: 'https://example.invalid' }, queue: { snapshot: () => ({ drafts: [] }) } };
  ui.animate = fn => fn(); ui.ready = true; ui.connected = true; ui.foreground = true;
  // ConnectionCheck 使用同一时钟，避免依赖真实时间。
  ui.connectionCheck.canCheck = (manual) => ConnectionCheck.prototype.canCheck.call(ui.connectionCheck, manual, time.now());
  ui.connectionCheck.failed = error => ConnectionCheck.prototype.failed.call(ui.connectionCheck, error, time.now());
  return { ui, time };
}
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };

test('前台立即检查、每两秒轮询，慢响应期间不会并发；绑定成功不等待记录', async () => {
  const pending = deferred(); let calls = 0;
  const { ui, time } = page({ session: () => { calls++; return pending.promise; }, history: () => { throw Error('不应读取'); } });
  ui.startPolling(); assert.equal(calls, 1); assert.equal(ui.refreshing, true);
  const timer = [...time.timers.values()][0]; assert.equal(timer.delay, 2000);
  timer.fn(); assert.equal(calls, 1);
  pending.resolve({ paired: true }); await settle();
  assert.equal(ui.paired, true); assert.equal(ui.refreshing, false); assert.match(ui.notice, /绑定成功/);
  timer.fn(); assert.equal(calls, 1); ui.stopPolling(); assert.equal(time.timers.size, 0);
});

test('一次断网不会永久停止自动检查，退避后恢复；后台不发请求', async () => {
  let calls = 0;
  const { ui, time } = page({ session: async () => { if (++calls === 1) throw new RelayRequestError('离线'); return { paired: true }; } });
  await ui.refresh(false, false); assert.equal(ui.statusError, '离线');
  await ui.refresh(false, false); assert.equal(calls, 1);
  time.advance(2000); ui.foreground = false; await ui.refresh(false, false); assert.equal(calls, 1);
  ui.foreground = true; await ui.refresh(false, false); assert.equal(ui.paired, true); assert.equal(ui.statusError, '');
});

test('限流手动检查也遵守一分钟冷却，鉴权失败停止自动轮询并允许人工重试', () => {
  const policy = new ConnectionCheck(); policy.failed(new RelayRequestError('限流', 429), 0);
  assert.equal(policy.canCheck(true, 59000), false); assert.equal(policy.canCheck(false, 60000), true);
  policy.reset(); policy.failed(new RelayRequestError('失效', 401), 0);
  assert.equal(policy.canCheck(false, 120000), false); assert.equal(policy.canCheck(true, 120000), true);
});

test('重试遇到超时不得补发配对；明确 401 才补发；已送达不重复提交', async () => {
  for (const status of [0, 401, 429, 500, -1]) {
    let claims = 0;
    const { ui } = page({ session: async () => { if (status !== -1) throw new RelayRequestError('合成错误', status); return { paired: false }; },
      claim: async () => { claims++; } });
    if ([0, 429, 500].includes(status)) await assert.rejects(ui.retryPair()); else await ui.retryPair();
    assert.equal(claims, status === 401 ? 1 : 0);
  }
});

test('点击后立即显示阶段与耗时，双击不会重复提交，失败后释放操作状态', async () => {
  const { ui, time } = page({ session: async () => ({ paired: false }) });
  const pending = deferred(); let calls = 0;
  const action = () => { calls++; return pending.promise; };
  const running = ui.act(action, '正在提交配对…');
  assert.equal(ui.busy, true); assert.equal(ui.operation, '正在提交配对…');
  await ui.act(action, '再次提交'); assert.equal(calls, 1);
  time.advance(6000); [...time.timers.values()][0].fn(); assert.equal(ui.operationSeconds, 6);
  pending.reject(Error('网络失败')); await running;
  assert.equal(ui.error, '网络失败'); assert.equal(ui.busy, false); assert.equal(ui.operation, ''); assert.equal(time.timers.size, 0);
});

test('配对或解绑开始后，旧状态响应不得覆盖新状态', async () => {
  const pending = deferred(); const { ui } = page({ session: () => pending.promise });
  const refreshing = ui.refresh(false, false);
  await ui.act(async () => { ui.paired = true; });
  pending.resolve({ paired: false }); await refreshing; assert.equal(ui.paired, true);
});

test('手动检查未批准时明确指向电脑，已批准后立即成功且不用再提交', async () => {
  let paired = false;
  const { ui } = page({ session: async () => ({ paired }) });
  await ui.refresh(true, false); assert.match(ui.notice, /设置 → 手机收集/);
  paired = true; await ui.retryPair(); assert.equal(ui.paired, true); assert.match(ui.notice, /绑定成功/);
});

test('状态 HTTP 请求总时限为 8 秒，超时销毁请求且不泄漏底层 URL', async () => {
  const time = clock(); let destroyed = 0;
  const { RelayClient } = load('services/RelayClient.ets', { '../core/ConnectionCheck': check,
    '@kit.NetworkKit': { http: { createHttp: () => ({ request: () => new Promise(() => {}), destroy: () => { destroyed++; } }),
      RequestMethod: { GET: 'GET' }, HttpDataType: { STRING: 0 } } },
  }, time.globals);
  const client = new RelayClient({ origin: 'https://example.invalid', credential: 'test-only' });
  const request = client.session(); const timer = [...time.timers.values()][0]; assert.equal(timer.delay, 8000);
  timer.fn(); await assert.rejects(request, error => error instanceof RelayRequestError && /网络超时/.test(error.message));
  assert.ok(destroyed > 0); assert.equal(time.timers.size, 0);
});

test('下拉刷新等待完整结果后收起，忙碌和未连接时不发送请求', async () => {
  const pending = deferred(); let calls = 0;
  const { ui } = page({ session: async () => { calls++; return { paired: true }; }, history: () => pending.promise });
  ui.historyPulling = true; const task = ui.pullHistory(); await settle();
  assert.equal(ui.historyPulling, true); pending.resolve([]); await task;
  assert.equal(ui.historyPulling, false); assert.equal(ui.historyLoaded, true); assert.equal(calls, 1);
  ui.busy = true; ui.historyPulling = true; await ui.pullHistory();
  assert.equal(ui.historyPulling, false); assert.equal(calls, 1);
  ui.busy = false; ui.connected = false; ui.historyPulling = true; await ui.pullHistory();
  assert.equal(ui.historyPulling, false); assert.equal(calls, 1);
});
