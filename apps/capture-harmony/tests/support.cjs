// 鸿蒙端 Node 测试的公共装载器：对真实 .ets / .ts 做语法擦除，替换系统边界。
// 只证明控制逻辑与纯函数；ArkUI 渲染由 Hvigor 编译与真机验证，不在此模拟。
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');

const root = path.join(__dirname, '../entry/src/main/ets');
// 页面源码里的显式分界：其前是控制逻辑，其后是 ArkUI Builder。
const BOUNDARY = '// @test-boundary';
// 只擦除装饰器；最长名称在前并要求词边界，避免 @Local 误吃 @LocalStorageLink。
const DECORATORS = /@(LocalStorageLink|LocalStorageProp|StorageLink|StorageProp|ObjectLink|Component|Provide|Consume|Observed|Watch|Entry|State|Prop|Link|Local|Param|Event|Monitor|Trace|Computed)\b(\([^\n]*?\))?\s*/g;

function pageSource(source) {
  const parts = source.split(BOUNDARY);
  if (parts.length !== 2) {
    throw new Error(`Index.ets 必须恰好包含一个 ${BOUNDARY} 标记（当前 ${parts.length - 1} 个）`);
  }
  return parts[0].replace('struct Index', 'export class Index').replace(DECORATORS, '') + '\n}';
}

function load(file, imports = {}, globals = {}, page = false) {
  let source = fs.readFileSync(path.join(root, file), 'utf8');
  if (page) source = pageSource(source);
  const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText;
  const scope = { exports: {}, require: name => imports[name] || {}, ...globals };
  vm.runInNewContext(js, scope);
  return scope.exports;
}

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function clock() {
  let now = 0, id = 0; const timers = new Map();
  return { timers, now: () => now, advance: value => { now += value; }, globals: {
    Date: { now: () => now }, setInterval: (fn, delay) => { timers.set(++id, { fn, delay }); return id; },
    clearInterval: id => timers.delete(id), setTimeout: (fn, delay) => { timers.set(++id, { fn, delay }); return id; },
    clearTimeout: id => timers.delete(id), TabsController: class {},
  } };
}

const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };

// 只装载一次：测试里的 RelayRequestError 必须与页面 instanceof 判断的是同一个类。
const check = load('core/ConnectionCheck.ts');
const { ConnectionCheck, RelayRequestError } = check;

// 页面装载：extraImports 覆盖或补充默认桩，供后续新增模块使用。
function createPage(client, extraImports = {}) {
  const time = clock();
  const { Index } = load('pages/Index.ets', {
    '../services/ThemePalette': load('services/ThemePalette.ets', {}, { '$r': name => name }),
    '../core/ConnectionCheck': check, '../services/AppPreferences': { AppPreferences: class {} },
    '../services/HoldingHandObserver': { HoldingHandObserver: class {} },
    ...extraImports,
  }, time.globals, true);
  const ui = new Index();
  ui.runtime = { client, connection: { origin: 'https://example.invalid' }, queue: { snapshot: () => ({ drafts: [] }) } };
  ui.animate = fn => fn(); ui.ready = true; ui.connected = true; ui.foreground = true;
  // ConnectionCheck 使用同一时钟，避免依赖真实时间。
  ui.connectionCheck.canCheck = (manual) => ConnectionCheck.prototype.canCheck.call(ui.connectionCheck, manual, time.now());
  ui.connectionCheck.failed = error => ConnectionCheck.prototype.failed.call(ui.connectionCheck, error, time.now());
  return { ui, time };
}

module.exports = { root, BOUNDARY, load, deferred, clock, settle, createPage, pageSource, check, ConnectionCheck, RelayRequestError };
