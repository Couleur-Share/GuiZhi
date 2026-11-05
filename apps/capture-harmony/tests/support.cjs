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

function pageSource(source, name = 'Index') {
  const parts = source.split(BOUNDARY);
  if (parts.length !== 2) {
    throw new Error(`Index.ets 必须恰好包含一个 ${BOUNDARY} 标记（当前 ${parts.length - 1} 个）`);
  }
  if (!/^[A-Za-z][A-Za-z0-9]*$/.test(name)) throw new Error('测试页面名称无效');
  return parts[0].replace(`struct ${name}`, `export class ${name}`).replace(DECORATORS, '') + '\n}';
}

function load(file, imports = {}, globals = {}, page = false) {
  let source = fs.readFileSync(path.join(root, file), 'utf8');
  if (page) source = pageSource(source, typeof page === 'string' ? page : 'Index');
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
const editorAutosave = load('core/EditorAutosave.ts');
// 识别与拆分依赖 Protocol 的字节计数，与页面共用同一份解析实现。
const captureParser = load('core/CaptureParser.ts', { './Protocol': load('core/Protocol.ts') });
const sendingState = load('core/SendingState.ts');
const textPreview = load('core/TextPreview.ts');
// BatchSend 依赖真实的 SendOutcome / ConnectionCheck：失败分类基于 instanceof RelayRequestError，必须是同一个类。
const batchSend = load('core/BatchSend.ts', {
  './ConnectionCheck': check, './SendOutcome': load('core/SendOutcome.ts', { './ConnectionCheck': check }),
  './TextPreview': textPreview,
});
// 发送记录与近期页合并逻辑：与页面共用同一份解析实现，避免测试里另写一套“预期”。
const sentLog = load('core/SentLog.ts', { './CaptureParser': captureParser, './TextPreview': textPreview });
const historyView = load('core/HistoryView.ts', { './CaptureParser': captureParser, './SentLog': sentLog });
// 电脑在线状态：纯函数与 30 秒节奏，页面与测试共用同一份实现。
const desktopStatus = load('core/DesktopStatus.ts');
// 列表分组、多选与延迟删除：纯逻辑，页面与测试共用同一份实现。
const dayGroups = load('core/DayGroups.ts');
const selection = load('core/Selection.ts');
const pendingDelete = load('core/PendingDelete.ts');
// 列表键：ForEach 是否复用旧子项完全取决于它，所以单独作为可测试的纯函数。
const listKeys = load('core/ListKeys.ts');
// 分享入口 spike（仅 dev 包）的诊断整理：与应用共用同一份 ShareRecords / CaptureParser，才能如实反映现有逻辑对各来源分享的处理。
const shareProbe = load('sharediag/ShareProbe.ts', { '../core/ShareRecords': load('core/ShareRecords.ts'), '../core/CaptureParser': captureParser });

// 内存端口：记录每次落盘的条目，可按需让读取或写入失败。
function memorySentPort({ initial = [], damaged = false, failLoad = null, failSave = null } = {}) {
  const port = {
    saves: [],
    load: async () => { if (port.failLoad) throw port.failLoad; return { entries: initial.slice(), damaged }; },
    save: async entries => { if (port.failSave) throw port.failSave; port.saves.push(Array.from(entries)); },
    failLoad, failSave,
  };
  return port;
}

// 页面装载：extraImports 覆盖或补充默认桩，供后续新增模块使用。
function createPage(client, extraImports = {}) {
  const time = clock();
  // 发送成功的触觉：只记录调用（传入的设置），不触碰系统振动；真实效果由 haptics.test.cjs 用假的 vibrator 验证。
  const haptics = [];
  const { Index } = load('pages/Index.ets', {
    '../services/ThemePalette': load('services/ThemePalette.ets', {}, { '$r': name => name }),
    '../services/ReadingRuntime': { readingTrialEnabled: () => false },
    '../core/ConnectionCheck': check, '../services/AppPreferences': { AppPreferences: class {} },
    '../services/HoldingHandObserver': { HoldingHandObserver: class {} },
    '../core/EditorAutosave': editorAutosave, '../core/CaptureParser': captureParser, '../core/SendingState': sendingState, '../core/BatchSend': batchSend,
    '../core/SentLog': sentLog, '../core/HistoryView': historyView, '../core/DesktopStatus': desktopStatus,
    '../core/PendingDelete': pendingDelete, '../core/Selection': selection, '../core/TextPreview': textPreview,
    '../services/Layout': load('services/Layout.ets'),
    '../services/Haptics': { hapticSuccess: settings => { haptics.push(settings); } },
    ...extraImports,
    // $r 是 ArkTS 的资源引用，测试里直接返回资源名，页面里的对话框按钮等控制逻辑才能真实执行。
  }, { ...time.globals, $r: name => name }, true);
  const ui = new Index();
  ui.runtime = { client, connection: { origin: 'https://example.invalid' }, queue: { snapshot: () => ({ drafts: [] }) },
    sentLog: new sentLog.SentLog(memorySentPort()) };
  ui.animate = fn => fn(); ui.ready = true; ui.connected = true; ui.foreground = true;
  // ConnectionCheck 使用同一时钟，避免依赖真实时间。
  ui.connectionCheck.canCheck = (manual) => ConnectionCheck.prototype.canCheck.call(ui.connectionCheck, manual, time.now());
  ui.connectionCheck.failed = error => ConnectionCheck.prototype.failed.call(ui.connectionCheck, error, time.now());
  return { ui, time, haptics };
}

// RelayClient 装载：http 由脚本编排（每次请求消耗一项），其余使用真实的 Protocol / ConnectionCheck / SendOutcome。
// 脚本项：{ status, body } 表示服务端响应；'hang' 表示永不返回（用于触发超时）。
function loadRelayClient(time, script) {
  const calls = [];
  const http = {
    RequestMethod: { GET: 'GET', POST: 'POST', DELETE: 'DELETE' }, HttpDataType: { STRING: 0 },
    createHttp: () => ({
      request: (url, options) => {
        calls.push({ url, options });
        const step = script.shift();
        if (step === 'hang') return new Promise(() => {});
        if (!step) return Promise.reject(new Error('测试脚本已耗尽'));
        return Promise.resolve({ responseCode: step.status, result: typeof step.body === 'string' ? step.body : JSON.stringify(step.body) });
      },
      destroy: () => {},
    }),
  };
  const outcome = load('core/SendOutcome.ts', { './ConnectionCheck': check });
  const { RelayClient } = load('services/RelayClient.ets', {
    '../core/ConnectionCheck': check, '../core/Protocol': load('core/Protocol.ts'), '../core/SendOutcome': outcome,
    '../core/DraftQueue': {}, '@kit.NetworkKit': { http },
  }, time.globals);
  return { RelayClient, calls };
}

module.exports = { root, BOUNDARY, load, deferred, clock, settle, createPage, pageSource, check, ConnectionCheck, RelayRequestError, loadRelayClient, sendingState, batchSend, captureParser, textPreview, sentLog, historyView, desktopStatus, dayGroups, selection, pendingDelete, listKeys, shareProbe, memorySentPort };
