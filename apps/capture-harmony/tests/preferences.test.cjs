const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');

// 对真实 .ets 服务做语法擦除，替换系统边界；不模拟或声称证明 ArkUI 渲染。
function load(file, imports, globals = {}) {
  const source = fs.readFileSync(path.join(__dirname, '../entry/src/main/ets/services', file), 'utf8');
  const js = ts.transpileModule(source, { compilerOptions: {
    target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS
  } }).outputText;
  const scope = { exports: {}, require: name => imports[name] || {},
    $r: name => name, Color: { White: 'white', Transparent: 'transparent' }, ...globals };
  vm.runInNewContext(js, scope);
  return scope.exports;
}
const palettes = load('ThemePalette.ets', {});
const preferences = load('AppPreferences.ets', { './ThemePalette': palettes });
const { AppPreferences, AppPreferencesStore } = preferences;
function store(raw = '') {
  let pending = raw, disk = raw, flushes = 0;
  return { port: { get: async () => disk, put: async (_key, value) => { pending = value; },
    flush: async () => { disk = pending; flushes++; } },
    disk: () => disk, flushes: () => flushes };
}

test('设置保存后新实例恢复全部偏好，恢复默认不访问草稿存储', async () => {
  const memory = store(); const first = new AppPreferencesStore(memory.port);
  const value = new AppPreferences(); value.theme = 'dark'; value.themeColor = 'purple'; value.material = 'thick';
  value.lightFeedback = false; value.handedness = false; value.defaultMode = 'text';
  value.previewLines = 6; value.showCount = false; value.reduceMotion = true;
  await first.save(value);
  const restored = await new AppPreferencesStore(memory.port).load();
  assert.equal(JSON.stringify(restored), JSON.stringify(value));
  await first.save(new AppPreferences());
  assert.equal(JSON.stringify(await first.load()), JSON.stringify(new AppPreferences()));
  assert.equal(memory.flushes(), 2);
});

test('损坏或不支持的设置拒绝加载，不自动覆盖原数据', async () => {
  for (const raw of ['not-json', '{}', JSON.stringify({ ...new AppPreferences(), material: 'invalid' })]) {
    const memory = store(raw);
    await assert.rejects(new AppPreferencesStore(memory.port).load());
    assert.equal(memory.disk(), raw); assert.equal(memory.flushes(), 0);
  }
});

test('设置持久化失败向调用者报告，不能伪报保存成功', async () => {
  const memory = store(); memory.port.flush = async () => { throw Error('disk full'); };
  await assert.rejects(new AppPreferencesStore(memory.port).save(new AppPreferences()), /disk full/);
  assert.equal(memory.disk(), '');
});

function materialRuntime(device, supported = true) {
  const empty = { disabled: true };
  const kit = supported === 'missing' ? new Proxy({}, { get() { throw Error('old OS API accessed'); } }) : {
    isImmersiveMaterialSupported: () => supported,
    Material: { empty }, ImmersiveStyle: { ULTRA_THIN: 0, THIN: 1, REGULAR: 2, THICK: 3, ULTRA_THICK: 4 },
    ImmersiveMaterial: class { constructor(options) { this.options = options; } }
  };
  return load('ImmersiveAppearance.ets', { '@kit.ArkUI': { uiMaterial: kit },
    '@kit.BasicServicesKit': { deviceInfo: device }, './AppPreferences': preferences });
}

test('旧系统无 API 或版本不足时不读取新材质，也不调用新页签方法', () => {
  for (const device of [{}, { apiAvailable: () => false }]) {
    const ui = materialRuntime(device, 'missing'); let calls = 0;
    new ui.CaptureTabsAppearance(false, new AppPreferences()).applyNormalAttribute({ barBackgroundColor() { calls++; } });
    assert.equal(calls, 1); assert.equal(ui.panelMaterial(new AppPreferences()), undefined);
  }
});

test('关闭光感清除悬浮和弹层材质；重新开启恢复当前偏好', () => {
  const ui = materialRuntime({ apiAvailable: () => true }); const settings = new AppPreferences();
  let floating; const tabs = { barBackgroundColor() {}, barFloatingStyle(value) { floating = value; } };
  settings.material = 'off';
  new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute(tabs);
  assert.equal(floating, undefined); assert.equal(ui.panelMaterial(settings).disabled, true);
  settings.material = 'regular'; settings.lightFeedback = false; settings.handedness = false;
  new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute(tabs);
  assert.equal(floating.systemMaterial.options.style, 2);
  assert.equal(floating.systemMaterial.options.interactive, false);
  assert.equal(floating.systemMaterial.options.lightEffect, null);
  assert.equal(floating.adaptToHandedness, false);
  assert.equal(ui.panelMaterial(settings).options.style, 2);
  new ui.CaptureTabsAppearance(true, settings).applyNormalAttribute(tabs);
  assert.equal(floating, undefined);
  new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute(tabs);
  assert.ok(floating.systemMaterial);
});

test('无材质能力时正常回退，不伪造支持状态', () => {
  const ui = materialRuntime({ apiAvailable: () => true }, false);
  assert.equal(ui.materialAvailable(), false); assert.equal(ui.panelMaterial(new AppPreferences()), undefined);
});

test('0.2.0 偏好补齐减少动效，保留已有材质且不隐式重写', async () => {
  for (const material of ['thin', 'regular', 'thick']) {
    const legacy = { ...new AppPreferences(), material, theme: 'dark' }; delete legacy.reduceMotion;
    const raw = JSON.stringify(legacy); const memory = store(raw);
    const result = await new AppPreferencesStore(memory.port).load();
    assert.equal(result.reduceMotion, false); assert.equal(result.material, material);
    assert.equal(result.theme, 'dark'); assert.equal(memory.disk(), raw); assert.equal(memory.flushes(), 0);
  }
  const memory = store(JSON.stringify({ ...new AppPreferences(), reduceMotion: 'false' }));
  await assert.rejects(new AppPreferencesStore(memory.port).load());
  assert.equal(memory.flushes(), 0);
});

test('官方五档名称与底栏、弹层参数一致，新增档位可持久化', async () => {
  const ui = materialRuntime({ apiAvailable: () => true });
  for (const [value, label, style] of [
    ['ultraThin', '超薄', 0], ['thin', '薄', 1], ['regular', '常规', 2], ['thick', '厚', 3], ['ultraThick', '超厚', 4]
  ]) {
    const settings = new AppPreferences(); settings.material = value;
    const memory = store(); const prefs = new AppPreferencesStore(memory.port);
    await prefs.save(settings); assert.equal((await prefs.load()).material, value);
    assert.equal(ui.materialLabel(value), label); assert.equal(ui.panelMaterial(settings).options.style, style);
    let floating;
    new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute({ barBackgroundColor() {}, barFloatingStyle(v) { floating = v; } });
    assert.equal(floating.systemMaterial.options.style, style);
  }
  const settings = new AppPreferences(); let floating;
  new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute({ barBackgroundColor() {}, barFloatingStyle(v) { floating = v; } });
  assert.equal(floating.systemMaterial.options.style, 1); assert.equal(ui.panelMaterial(settings).options.style, 3);
});

test('减少动效关闭形变但保留用户光效选择，恢复后不丢偏好', () => {
  const ui = materialRuntime({ apiAvailable: () => true }); const settings = new AppPreferences();
  settings.reduceMotion = true; let floating;
  new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute({ barBackgroundColor() {}, barFloatingStyle(v) { floating = v; } });
  assert.equal(floating.systemMaterial.options.interactive, false);
  assert.equal(ui.panelMaterial(settings).options.interactive, false);
  assert.ok(floating.systemMaterial.options.lightEffect);
  settings.reduceMotion = false; assert.equal(ui.panelMaterial(settings).options.interactive, true);
});

function holdingRuntime({ supported = true, onError, offError } = {}) {
  const timers = new Map(); let next = 0, callback, onCount = 0, offCount = 0;
  const events = []; const motion = {
    HoldingHandStatus: { NOT_HELD: 0, LEFT_HAND_HELD: 1, RIGHT_HAND_HELD: 2, BOTH_HANDS_HELD: 3 },
    on(event, handler) { assert.equal(event, 'holdingHandChanged'); onCount++; if (onError) throw { code: onError }; callback = handler; },
    off(event, handler) { assert.equal(event, 'holdingHandChanged'); assert.equal(handler, callback); offCount++; if (offError) throw Error('service busy'); }
  };
  const { HoldingHandObserver } = load('HoldingHandObserver.ets', { '@kit.MultimodalAwarenessKit': { motion } }, {
    canIUse: () => supported,
    setTimeout(fn, delay) { assert.equal(delay, 280); timers.set(++next, fn); return next; },
    clearTimeout(id) { timers.delete(id); }
  });
  return {
    observer: new HoldingHandObserver(state => events.push(state)), events,
    emit: value => callback(value), tick() { const batch = [...timers.values()]; timers.clear(); batch.forEach(fn => fn()); },
    counts: () => [onCount, offCount, timers.size]
  };
}

test('握持订阅去重，左右手与双手、未握持状态均有可见反馈', () => {
  const h = holdingRuntime(); h.observer.start(); h.observer.start(); assert.equal(h.counts()[0], 1);
  for (const [status, side, message] of [[1, 'left', '左手'], [2, 'right', '右手'], [3, 'right', '双手'], [0, 'right', '未握持']]) {
    const count = h.events.length; h.emit(status); assert.equal(h.events.length, count); h.tick();
    assert.equal(h.events.at(-1).side, side); assert.ok(h.events.at(-1).message.includes(message));
    h.emit(status); h.tick(); assert.equal(h.events.length, count + 1);
  }
  h.observer.stop(); h.observer.stop(); assert.equal(h.counts()[1], 1);
});

test('握持抖动取消过期位置，后台与关闭后不响应延迟事件', () => {
  const h = holdingRuntime(); h.observer.start(); h.emit(2); h.tick();
  const count = h.events.length;
  h.emit(1); h.emit(2); h.tick(); assert.equal(h.events.length, count);
  h.emit(1); h.emit(3); h.emit(1); h.tick(); assert.equal(h.events.at(-1).side, 'left');
  h.emit(2); h.observer.stop(); const stopped = h.events.length;
  h.emit(1); h.tick(); assert.equal(h.events.length, stopped); assert.equal(h.counts()[2], 0);
  h.observer.start(); h.emit(1); h.tick(); assert.equal(h.events.at(-1).side, 'left'); assert.equal(h.counts()[0], 2);
});

test('握持能力与授权失败明确反馈，解除失败后不重复注册', () => {
  for (const [options, message, calls] of [[{ supported: false }, '不支持', 0], [{ onError: 801 }, '不支持', 1],
    [{ onError: 201 }, '权限未生效', 1], [{ onError: 31500001 }, '31500001', 1]]) {
    const h = holdingRuntime(options); h.observer.start();
    assert.ok(h.events.at(-1).message.includes(message)); assert.equal(h.counts()[0], calls);
    assert.equal(h.events.at(-1).side, 'right');
  }
  const h = holdingRuntime({ offError: true }); h.observer.start(); h.observer.stop();
  const count = h.events.length; h.emit(1); h.tick(); assert.equal(h.events.length, count);
  h.observer.start(); h.emit(1); h.tick(); assert.equal(h.events.at(-1).side, 'left'); assert.equal(h.counts()[0], 1);
});


test('旧偏好升级为清爽蓝，不更改深浅模式、材质、草稿相关偏好或原存储', async () => {
  const legacy = { ...new AppPreferences(), theme: 'dark', material: 'thick', defaultMode: 'text' };
  delete legacy.themeColor;
  const raw = JSON.stringify(legacy), memory = store(raw);
  const restored = await new AppPreferencesStore(memory.port).load();
  assert.equal(restored.themeColor, 'blue'); assert.equal(restored.theme, 'dark');
  assert.equal(restored.material, 'thick'); assert.equal(restored.defaultMode, 'text');
  assert.equal(memory.disk(), raw); assert.equal(memory.flushes(), 0);
});

test('五种配色独立保存、重载和恢复默认，非法配色不覆盖设置', async () => {
  for (const id of palettes.THEME_COLOR_IDS) {
    const memory = store(), prefs = new AppPreferencesStore(memory.port);
    const value = new AppPreferences(); value.themeColor = id; value.theme = 'light';
    await prefs.save(value);
    const restored = await new AppPreferencesStore(memory.port).load();
    assert.equal(restored.themeColor, id); assert.equal(restored.theme, 'light');
    assert.equal(palettes.themePalette(id).id, id);
    await prefs.save(new AppPreferences()); assert.equal((await prefs.load()).themeColor, 'blue');
  }
  for (const id of ['green', '', null, 1]) {
    const raw = JSON.stringify({ ...new AppPreferences(), themeColor: id }), memory = store(raw);
    await assert.rejects(new AppPreferencesStore(memory.port).load());
    assert.equal(memory.disk(), raw); assert.equal(memory.flushes(), 0);
  }
});

test('所有配色的深浅资源齐全，正文、主按钮和浅底提示对比度达到 4.5:1', () => {
  const luminance = hex => {
    const values = hex.slice(1).match(/../g).map(v => parseInt(v, 16) / 255)
      .map(v => v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4));
    return values[0] * 0.2126 + values[1] * 0.7152 + values[2] * 0.0722;
  };
  const contrast = (a, b) => { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
  for (const mode of ['base', 'dark']) {
    const resources = JSON.parse(fs.readFileSync(path.join(__dirname, '../entry/src/main/resources', mode, 'element/color.json'), 'utf8')).color;
    const lookup = name => resources.find(c => c.name === name.replace('app.color.', ''))?.value;
    for (const id of palettes.THEME_COLOR_IDS) {
      const p = palettes.themePalette(id), accent = lookup(p.accent), soft = lookup(p.soft), onAccent = lookup(p.onAccent);
      for (const [a, b] of [[accent, lookup('surface')], [accent, lookup('page_background')], [accent, soft], [onAccent, accent]]) {
        assert.ok(a && b, mode + '/' + id + ' 资源缺失');
        assert.ok(contrast(a, b) >= 4.5, mode + '/' + id + ' 对比度不足: ' + contrast(a, b).toFixed(2));
      }
    }
  }
});

test('旧偏好补齐标准动效和悬浮底栏，保留减少动效开关与配色', async () => {
  const legacy = { ...new AppPreferences(), reduceMotion: true, themeColor: 'rose' };
  delete legacy.animationSpeed; delete legacy.tabStyle;
  const raw = JSON.stringify(legacy), memory = store(raw);
  const restored = await new AppPreferencesStore(memory.port).load();
  assert.equal(restored.animationSpeed, 'normal'); assert.equal(restored.tabStyle, 'floating');
  assert.equal(restored.reduceMotion, true); assert.equal(restored.themeColor, 'rose');
  assert.equal(memory.disk(), raw); assert.equal(memory.flushes(), 0);
});

test('动效速度和底栏样式重载保留，非法值拒绝且不覆盖存储', async () => {
  for (const animationSpeed of ['fast', 'normal', 'relaxed']) {
    for (const tabStyle of ['floating', 'standard']) {
      const value = { ...new AppPreferences(), animationSpeed, tabStyle };
      const memory = store(), prefs = new AppPreferencesStore(memory.port);
      await prefs.save(value); assert.equal(JSON.stringify(await prefs.load()), JSON.stringify(value));
    }
  }
  for (const invalid of [{ animationSpeed: 'slow' }, { animationSpeed: null }, { tabStyle: '' }, { tabStyle: 1 }]) {
    const raw = JSON.stringify({ ...new AppPreferences(), ...invalid }), memory = store(raw);
    await assert.rejects(new AppPreferencesStore(memory.port).load()); assert.equal(memory.disk(), raw);
  }
});

test('贴底导航关闭悬浮材质，返回悬浮恢复用户选定材质', () => {
  const ui = materialRuntime({ apiAvailable: () => true }), settings = new AppPreferences();
  settings.material = 'regular'; settings.tabStyle = 'standard'; let floating;
  const tabs = { barBackgroundColor() {}, barFloatingStyle(value) { floating = value; } };
  new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute(tabs); assert.equal(floating, undefined);
  settings.tabStyle = 'floating'; new ui.CaptureTabsAppearance(false, settings).applyNormalAttribute(tabs);
  assert.equal(floating.systemMaterial.options.style, 2);
});

function motionRuntime() {
  class Effect {
    constructor(steps = []) { this.steps = steps; }
    combine(other) { return new Effect([...this.steps, ...other.steps]); }
    animation(options) { return { steps: this.steps, options }; }
  }
  return load('InterfaceMotion.ets', { '@kit.ArkUI': { curves: { springMotion: (...args) => args } }, './AppPreferences': preferences }, {
    ClickEffectLevel: { LIGHT: 0 }, TransitionEffect: {
      OPACITY: new Effect(['opacity']), translate: value => new Effect([{ translate: value }]), scale: value => new Effect([{ scale: value }])
    }
  });
}

test('三档动效驱动实际动画参数，错峰总延迟封顶，减少动效移除所有变形和等待', () => {
  const motion = motionRuntime(), settings = new AppPreferences(); let called = 0;
  const context = { animateTo(options, change) { assert.ok(options.duration > 0); called++; change(); } };
  for (const [speed, duration] of [['fast', 180], ['normal', 280], ['relaxed', 420]]) {
    settings.animationSpeed = speed; assert.equal(motion.motionDuration(settings), duration);
    assert.equal(motion.revealTransition(settings, 100).options.delay, 96);
    assert.equal(motion.revealTransition(settings, -1).options.delay, 0);
    motion.animateInterface(context, settings, () => {});
  }
  assert.equal(called, 3); settings.reduceMotion = true;
  let changed = false; motion.animateInterface(context, settings, () => { changed = true; });
  assert.equal(changed, true); assert.equal(called, 3); assert.equal(motion.motionDuration(settings), 0);
  const transition = motion.revealTransition(settings, 100);
  assert.equal(JSON.stringify(transition.steps), '["opacity"]'); assert.equal(transition.options.duration, 0);
  assert.equal(transition.options.delay, undefined); assert.equal(motion.pressFeedback(settings).scale, 1);
  settings.reduceMotion = false; assert.equal(motion.motionDuration(settings), 420);
  settings.lightFeedback = false; assert.equal(motion.pressFeedback(settings).scale, 1);
  settings.lightFeedback = true; assert.equal(motion.pressFeedback(settings).scale, 0.97);
});
