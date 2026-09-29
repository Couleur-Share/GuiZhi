// 发送成功的轻触觉（0.7.0）：只证明“何时触发、传给系统的参数、任何失败是否静默”。
// 设备是否真的振动、强度与手感只能真机确认；这里替换了 vibrator，不证明马达行为。
const test = require('node:test');
const assert = require('node:assert/strict');
const { load, RelayRequestError } = require('./support.cjs');
const { pageWith, receipt } = require('./page-fixture.cjs');

// throwOn：'support' 让支持性检查抛出，'start' 让启动同步抛出；reject 让启动返回被拒绝的 Promise。
function fakeVibrator({ supported = true, throwOn = null, reject = null } = {}) {
  const calls = { support: [], start: [] };
  const vibrator = {
    HapticFeedback: { EFFECT_NOTICE_SUCCESS: 'haptic.notice.success' },
    isSupportEffectSync: effectId => { calls.support.push(effectId); if (throwOn === 'support') throw new Error('14600101'); return supported; },
    startVibration: (effect, attribute) => {
      calls.start.push({ effect, attribute });
      if (throwOn === 'start') throw new Error('201');
      return reject ? Promise.reject(new Error(reject)) : Promise.resolve();
    },
  };
  return { vibrator, calls };
}
const service = fake => load('services/Haptics.ets', { '@kit.SensorServiceKit': { vibrator: fake.vibrator } });
const prefs = (over = {}) => ({ lightFeedback: true, reduceMotion: false, ...over });
const flush = () => new Promise(resolve => setImmediate(resolve));

test('发送成功触觉：请求系统预置的“成功通知”效果一次，场景为触摸反馈', () => {
  const fake = fakeVibrator();
  service(fake).hapticSuccess(prefs());
  assert.deepEqual(fake.calls.support, ['haptic.notice.success'], '先确认设备支持该效果');
  assert.equal(fake.calls.start.length, 1);
  const { effect, attribute } = fake.calls.start[0];
  assert.equal(effect.type, 'preset');
  assert.equal(effect.effectId, 'haptic.notice.success');
  assert.equal(effect.count, 1);
  assert.equal(attribute.usage, 'touch', '走系统“触感反馈”开关，用户关闭触感时不会强行振动');
});

test('发送成功触觉：遵守“按压反馈”与“减少动效”，关闭时连支持性检查都不做', () => {
  for (const off of [{ lightFeedback: false }, { reduceMotion: true }, { lightFeedback: false, reduceMotion: true }]) {
    const fake = fakeVibrator();
    service(fake).hapticSuccess(prefs(off));
    assert.deepEqual(fake.calls, { support: [], start: [] }, JSON.stringify(off));
  }
});

test('发送成功触觉：设备不支持该效果就不振动，也不报错', () => {
  const fake = fakeVibrator({ supported: false });
  service(fake).hapticSuccess(prefs());
  assert.equal(fake.calls.support.length, 1);
  assert.equal(fake.calls.start.length, 0);
});

test('发送成功触觉：检查或启动抛出（无权限 201、设备故障 14600101）以及异步被拒绝，一律静默，不产生未处理的拒绝', async () => {
  const unhandled = [];
  const record = reason => { unhandled.push(reason); };
  process.on('unhandledRejection', record);
  try {
    for (const options of [{ throwOn: 'support' }, { throwOn: 'start' }, { reject: '801' }]) {
      const fake = fakeVibrator(options);
      assert.doesNotThrow(() => service(fake).hapticSuccess(prefs()), JSON.stringify(options));
    }
    await flush(); await flush();
    assert.deepEqual(unhandled, []);
  } finally {
    process.off('unhandledRejection', record);
  }
});

// ---------- 页面何时触发 ----------

test('单条发送：成功触发一次触觉；失败不触发，只显示原因', async () => {
  const ok = await pageWith({}, ['https://example.com/a']);
  assert.equal(await ok.ui.sendDraft(ok.ui.drafts[0]), true);
  assert.equal(ok.haptics.length, 1);
  assert.equal(ok.haptics[0], ok.ui.settings, '触觉按当前设置决定是否触发');

  const failing = await pageWith({ send: async () => { throw new RelayRequestError('内容格式不正确', 400, 'invalid_input'); } }, ['https://example.com/a']);
  assert.equal(await failing.ui.sendDraft(failing.ui.drafts[0]), false);
  assert.equal(failing.haptics.length, 0);
  assert.match(failing.ui.error, /内容格式不正确/);
});

test('批量发送：全部成功只触发一次（不是每条一次）；有失败不触发', async () => {
  const inputs = ['https://example.com/a', 'https://example.com/b', 'https://example.com/c'];
  const all = await pageWith({}, inputs);
  await all.ui.sendAll();
  assert.equal(all.ui.drafts.length, 0);
  assert.equal(all.haptics.length, 1);

  const partial = await pageWith({ send: async body => {
    if (body.input.endsWith('/b')) throw new RelayRequestError('内容格式不正确', 400, 'invalid_input');
    return receipt(body.requestId);
  } }, inputs);
  await partial.ui.sendAll();
  assert.equal(partial.ui.drafts.length, 1);
  assert.match(partial.ui.error, /^已发送 2 条，失败 1 条/);
  assert.equal(partial.haptics.length, 0, '部分失败按失败呈现，不给成功的触觉');
});
