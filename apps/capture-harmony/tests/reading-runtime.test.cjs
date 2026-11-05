const test = require('node:test');
const assert = require('node:assert/strict');
const { deferred, clock } = require('./support.cjs');
const h = require('./reading-support.cjs');

function loadClient(script) {
  const time = clock(), calls = []; let destroyed = 0;
  const http = { RequestMethod: { GET: 'GET', POST: 'POST' }, HttpDataType: { STRING: 1 }, createHttp: () => ({
    destroy() { destroyed++; }, request: (url, options) => {
      calls.push({ url, options }); const next = script.shift();
      if (next === 'hang') return new Promise(() => {});
      return Promise.resolve({ responseCode: next.status, result: JSON.stringify(next.body) });
    },
  }) };
  const module = h.load('services/ReadingClient.ets', { '@kit.NetworkKit': { http }, '../core/ReadingModel': h.model }, time.globals);
  return { client: new module.ReadingClient(h.connection()), calls, time, destroyed: () => destroyed };
}
test('阅读网络边界独立凭证、禁止重定向、有界响应与超时、限流冷却', async () => {
  const f = loadClient([{ status: 200, body: { sequence: 1 } }, { status: 429, body: {} }, 'hang']);
  await f.client.head(); assert.equal(f.calls[0].options.maxRedirects, 0); assert.equal(f.calls[0].options.maxLimit, 12 * 1024 * 1024);
  assert.equal(f.calls[0].options.header['X-Guizhi-Reading'], '1'); assert.equal(f.calls[0].options.header.Cookie, undefined);
  await assert.rejects(f.client.head(), /60 秒/); await assert.rejects(f.client.head(), /60 秒/); assert.equal(f.calls.length, 2);
  f.time.advance(60001); const pending = f.client.head();
  const timer = [...f.time.timers.values()][0]; timer.fn(); await assert.rejects(pending, /网络不可用/);
  assert.equal(f.time.timers.size, 0); assert.ok(f.destroyed() >= 3);
  await assert.rejects(f.client.object('../private')); assert.equal(f.calls.length, 3);
});
test('阅读权限撤销与过期配对给出可重试原因，服务自由文本不进入错误', async () => {
  for (const status of [401, 403, 404, 410, 500]) {
    const f = loadClient([{ status, body: { error: 'secret-token-body' } }]);
    await assert.rejects(f.client.head(), error => !error.message.includes('secret-token-body') && /阅读|云端/.test(error.message));
  }
});

function assetStore() {
  let saved = null, locked = false;
  const asset = {
    Tag: { ALIAS: 1, SECRET: 2, ACCESSIBILITY: 3, SYNC_TYPE: 4, CONFLICT_RESOLUTION: 5, RETURN_TYPE: 6 },
    Accessibility: { DEVICE_UNLOCKED: 1 }, SyncType: { NEVER: 2 }, ConflictResolution: { OVERWRITE: 3 }, ReturnType: { ALL: 4 }, ErrorCode: { NOT_FOUND: 404 },
    query: async query => { assert.equal(Buffer.from(query.get(1)).toString(), 'guizhi.reading.connection.v1'); if (locked) throw new Error('locked');
      return saved ? [new Map([[2, new Uint8Array(saved)]])] : []; },
    add: async attrs => { if (locked) throw new Error('locked'); assert.equal(attrs.get(3), 1); assert.equal(attrs.get(4), 2);
      assert.equal(attrs.get(5), 3); saved = new Uint8Array(attrs.get(2)); },
    remove: async () => { if (locked) throw new Error('locked'); saved = null; },
  };
  const { ReadingConnectionStore } = h.load('services/ReadingConnectionStore.ets', { '@kit.AssetStoreKit': { asset }, '@kit.ArkTS': { util: h.util },
    '../core/ReadingModel': h.model, './ReadingCrypto': h.cryptoModule });
  return { store: new ReadingConnectionStore(), setLocked: value => { locked = value; } };
}
test('阅读连接统一进入 Asset Store；锁定失败不覆盖旧连接、不回落普通文件', async () => {
  const f = assetStore(), c = h.connection(); assert.equal(await f.store.load(), null);
  await f.store.save(c); assert.equal((await f.store.load()).credential, c.credential);
  f.setLocked(true); await assert.rejects(f.store.load(), /解锁/); await assert.rejects(f.store.save(h.connection()), /未回落/); await assert.rejects(f.store.clear());
  f.setLocked(false); assert.equal((await f.store.load()).credential, c.credential);
});
test('阅读运行时先保存配对凭据，网络失败重试复用原设备；正式包拒绝入口', async () => {
  let saved = null, fail = true, product = 'guizhi.dev'; const claims = [];
  class Store { async save(c) { saved = JSON.parse(JSON.stringify(c)); } async load() { return saved; } async clear() { saved = null; } }
  class Client { constructor(c) { this.c = c; } async claim() { assert.equal(saved.credential, this.c.credential); claims.push(this.c.credential); if (fail) throw new Error('合成断网'); } async bundle() { return null; } cancel() {} }
  const { ReadingRuntime } = h.load('services/ReadingRuntime.ets', {
    '@kit.AbilityKit': { bundleManager: { BundleFlag: { GET_BUNDLE_INFO_DEFAULT: 0 }, getBundleInfoForSelfSync: () => ({ name: product }) } },
    '../core/ReadingPairing': h.pairing, '../core/ReadingLibrary': h.library, './ReadingCrypto': h.cryptoModule,
    './ReadingConnectionStore': { ReadingConnectionStore: Store }, './ReadingClient': { ReadingClient: Client },
  });
  const c = h.connection(), invite = { origin: c.origin, libraryId: c.libraryId, generation: c.generation, pairId: c.pairId, secret: c.secret };
  const runtime = new ReadingRuntime({}); await runtime.open();
  await assert.rejects(runtime.pair(invite), /断网/); fail = false;
  const restarted = new ReadingRuntime({}); await restarted.open(); assert.equal(await restarted.retryPair(), false);
  assert.equal(claims.length, 2); assert.equal(claims[0], claims[1]);
  product = 'guizhi'; assert.throws(() => new ReadingRuntime({}), /开发包/);
});

function readingTab(scan) {
  const time = clock();
  const { ReadingTab } = h.load('components/ReadingTab.ets', {
    '@kit.ScanKit': { scanBarcode: { startScanForResult: scan }, scanCore: { ScanType: { QR_CODE: 1 } } },
    '../core/ReadingModel': h.model, '../core/ReadingPairing': h.pairing, '../services/ReadingCrypto': h.cryptoModule,
    '../services/AppPreferences': { AppPreferences: class {} },
  }, time.globals, 'ReadingTab');
  const tab = new ReadingTab(); tab.getUIContext = () => ({ getHostContext: () => ({}) });
  return { tab, time };
}
test('扫码系统界面切换不会丢失配对结果；真正离开阅读页会清除明文和搜索状态', async () => {
  const scan = deferred(), { tab } = readingTab(() => scan.promise), c = h.connection();
  tab.active = true; tab.foreground = true;
  const work = tab.scan(); tab.foreground = false; tab.visibilityChanged();
  scan.resolve({ originalValue: `${c.origin}/#reading=${Buffer.from(JSON.stringify({ origin: c.origin, libraryId: c.libraryId, generation: c.generation, pairId: c.pairId, secret: c.secret })).toString('base64url')}` });
  await work; assert.equal(tab.invitationOrigin, c.origin);
  tab.articles = [h.article()]; tab.query = '私密搜索'; tab.active = false; tab.visibilityChanged();
  assert.equal(tab.articles.length, 0); assert.equal(tab.query, ''); assert.equal(tab.invitation, null);
});
test('图片混合失败不能显示全部离线成功；进入后台后的旧操作不修改阅读界面', async () => {
  const { tab } = readingTab(() => {}); tab.current = h.article();
  tab.runtime = { library: { saveImages: async () => ({ saved: 1, failed: ['图片 2：网络中断'] }) } };
  await assert.rejects(tab.saveImages(), /1 张失败/); assert.equal(tab.notice, '');
  const pending = deferred(); const work = tab.act(() => pending.promise);
  tab.runtime = null; tab.lock(); pending.resolve(); await work;
  assert.equal(tab.ready, false); assert.equal(tab.articles.length, 0);
});
