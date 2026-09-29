const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

// 分享入口 spike 只属于 dev 包（结论见 docs/share-spike.md）：这些静态检查保证它不会漏进正式包。
// 真正的产物核对由 inspect-hap.ps1 完成（-ForbidExtensions -ForbidCode ShareDiag）；这里是源码层的第一道防线，Linux CI 也能跑。
const app = path.join(__dirname, '..');
const main = path.join(app, 'entry/src/main');
const read = (...parts) => fs.readFileSync(path.join(app, ...parts), 'utf8');

function sourceFiles(dir) {
  const files = [];
  for (const item of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, item.name);
    if (item.isDirectory()) files.push(...sourceFiles(full));
    else if (/\.(ets|ts)$/.test(item.name)) files.push(full);
  }
  return files;
}

test('正式包 module.json5 不注册任何扩展，也不出现 spike 名称；权限仍只有 INTERNET 与 DETECT_GESTURE', () => {
  const module = read('entry/src/main/module.json5');
  assert.equal(/extensionAbilities/.test(module), false);
  assert.equal(/"type"\s*:\s*"share"/.test(module), false);
  assert.equal(/sharediag/i.test(module), false);
  const permissions = [...module.matchAll(/"name"\s*:\s*"(ohos\.permission\.[A-Z_]+)"/g)].map(match => match[1]);
  assert.deepEqual(permissions.sort(), ['ohos.permission.DETECT_GESTURE', 'ohos.permission.INTERNET']);
});

test('正式包路由表只有 pages/Index；dev 路由 = 正式路由 + 两个诊断页，且页面源码存在并带 @Entry', () => {
  const official = JSON.parse(read('entry/src/main/resources/base/profile/main_pages.json')).src;
  assert.deepEqual(official, ['pages/Index']);
  const dev = JSON.parse(read('entry/src/dev/resources/base/profile/main_pages.json')).src;
  assert.deepEqual(dev, [...official, 'sharediag/ShareDiagPanel', 'sharediag/ShareDiagReport']);
  for (const page of dev) {
    const file = path.join(main, 'ets', `${page}.ets`);
    assert.ok(fs.existsSync(file), `路由页面缺少源码：${page}`);
    assert.match(fs.readFileSync(file, 'utf8'), /@Entry\b/, `${page} 缺少 @Entry`);
  }
});

test('正式包的其他源码不导入 sharediag：未被引用的文件不会进入正式包编译产物', () => {
  const offenders = [];
  for (const file of sourceFiles(path.join(main, 'ets'))) {
    if (file.split(path.sep).includes('sharediag')) continue;
    if (/from\s+['"][^'"]*sharediag[^'"]*['"]/i.test(fs.readFileSync(file, 'utf8'))) offenders.push(path.relative(app, file));
  }
  assert.deepEqual(offenders, []);
});

test('hvigorfile 只对 dev 产品注册扩展与诊断入口，入口文件、类名与字符串资源都对得上，且不改权限', () => {
  const plugin = read('entry/hvigorfile.ts');
  assert.match(plugin, /SHARE_SPIKE_PRODUCT\s*=\s*'dev'/);
  assert.match(plugin, /getProductName\(\)\s*!==\s*SHARE_SPIKE_PRODUCT\)\s*return/);
  assert.equal(/requestPermissions/.test(plugin), false, 'spike 不应改动权限');
  const entries = [...plugin.matchAll(/name:\s*'(\w+)',[\s\S]*?srcEntry:\s*'(\.\/ets\/[^']+)'/g)];
  assert.deepEqual(entries.map(match => match[1]).sort(), ['ShareDiagAbility', 'ShareDiagExtAbility']);
  for (const [, name, srcEntry] of entries) {
    const file = path.join(main, srcEntry);
    assert.ok(fs.existsSync(file), `入口文件不存在：${srcEntry}`);
    assert.match(fs.readFileSync(file, 'utf8'), new RegExp(`export default class ${name}\\b`), `${srcEntry} 应默认导出 ${name}`);
  }
  // 诊断入口的桌面名称只在 dev 资源里定义，正式包资源中不应出现。
  assert.match(read('entry/src/dev/resources/base/element/string.json'), /"diag_label"/);
  assert.equal(/diag_label/.test(read('entry/src/main/resources/base/element/string.json')), false);
});
