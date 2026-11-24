/**
 * 采集队列（imports）对照场景的数据层：把一份固定的任务夹具装进真实的渲染层，让界面处在确定的数据与时间里。
 *
 * 为什么不直接往库里写数据：Go 壳启动时 imports.Recover 会重排 pending/processing 任务并真去抓取网址，
 * 夹具里那些「处理中」「卡住」的任务一旦入库就会被它改写；而且「加载中」「读取失败」两种状态根本没有数据可写。
 * 这里改在页内换掉 window.api.import 的读取接口（渲染层每次调用都从 window.api 现取，换得掉），
 * 返回夹具按服务端口径（import_list.go 的 importListPage）算出的结果——渲染层的组件与 store 一行不改，
 * 台账「渲染层改动登记」保持为空。Viva 侧用同一份夹具、同样的筛选口径（viva/parity/importsfixture.go）。
 *
 * 配置走 localStorage（键 CONFIG_KEY）+ 页面初始化脚本：App.tsx 启动即 fetchTasks，「加载中」只存在于首次请求完成之前，
 * 所以替换必须赶在渲染层启动之前——脚本先于页面脚本运行，window.api 由壳的 contextBridge 垫片整体赋值，
 * 脚本在 window.api 上装一个 setter，赋值瞬间改好 import 子对象。
 * 场景离开时清掉配置并 reload（leaveImports），否则 rail 角标（活动任务数）会残留到后面的整窗场景里。
 *
 * 时间：Playwright 的 clock.setFixedTime 把 Date.now() 钉在夹具的 now（定时器照常跑，不影响 150 ms 去抖与加载延迟），
 * 时区用 CDP 钉在夹具的 timezone；两侧都用同一个「现在」，相对时间与「今天/昨天」的日期标签才对得上。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const IMPORTS_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/imports-fixture.json"), "utf8"),
);

const CONFIG_KEY = "__parity.imports";
const REGISTERED = Symbol("parity.imports.registered");

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置替换 window.api.import 的读取与变更接口。
 * 返回值无意义；任何一步抛错都吞掉，免得影响无配置的普通页面。
 */
function pageInit({ fixture, configKey }) {
  let config = null;
  try {
    config = JSON.parse(localStorage.getItem(configKey) || "null");
  } catch {
    return; // about:blank 等拿不到 localStorage 的文档
  }
  if (!config) return;

  const isActive = (t) => t.status === "pending" || t.status === "processing";
  const isMobile = (t) => (t.origin ?? "desktop") === "mobile";
  // overrides：按任务 id 覆盖夹具里的字段（任务详情场景要的「刷新来源」「手机端的接收时间」这类夹具里没有的状态）。
  const overrides = config.overrides || {};
  const tasks = (config.mode === "empty" ? [] : fixture.tasks).map((t) => ({
    receivedAt: null, stage: null, error: null, warning: null, itemType: null, resultItemId: null,
    duplicateItemId: null, collectionId: null, refreshOfItemId: null, tagNames: [], stageStats: null,
    captureStrategy: "standard", commentLimit: 0, ...t, ...(overrides[t.id] || {}),
  }));

  /** 服务端的来源与关键字筛选（import_list.go taskSearch）。 */
  const matchesSearch = (t, origin, query) => {
    if (origin === "mobile" && !isMobile(t)) return false;
    if (origin === "desktop" && isMobile(t)) return false;
    const keyword = String(query ?? "").trim().toLowerCase();
    if (!keyword) return true;
    return [t.displayName, t.sourceInput, t.error ?? "", t.warning ?? ""].some((s) => String(s).toLowerCase().includes(keyword));
  };
  /** 服务端的状态筛选（taskStatusFilter）。 */
  const matchesStatus = (t, status) => {
    if (!status || status === "all") return true;
    if (status === "active") return isActive(t);
    if (status === "degraded") return t.status === "completed" && !!t.warning;
    return t.status === status;
  };
  const queueState = () => ({
    paused: !!config.paused,
    runningCount: tasks.filter((t) => t.status === "processing").length,
    pendingCount: tasks.filter((t) => t.status === "pending").length,
    concurrency: fixture.queue.concurrency,
  });

  const list = (q) => {
    if (config.mode === "loading") return new Promise(() => {});
    if (config.mode === "error") return Promise.reject(new Error(config.errorMessage));
    if (config.hasMore && q?.cursor) return Promise.resolve({ entries: [], active: [], nextCursor: null, total: 0, counts: {}, degradedCount: 0 });
    const origin = q?.origin ?? "all";
    const matched = tasks.filter((t) => matchesSearch(t, origin, q?.query));
    const filtered = matched.filter((t) => matchesStatus(t, q?.status));
    const entries = [...filtered].sort((a, b) => b.createdAt - a.createdAt || (a.id < b.id ? 1 : -1));
    const active = matched.filter(isActive).sort((a, b) => a.createdAt - b.createdAt || (a.id < b.id ? -1 : 1));
    const counts = { pending: 0, processing: 0, completed: 0, failed: 0, canceled: 0, duplicate: 0 };
    for (const t of matched) counts[t.status] = (counts[t.status] ?? 0) + 1;
    return Promise.resolve({
      entries, active, nextCursor: config.hasMore ? "parity-next" : null, total: filtered.length, counts,
      degradedCount: matched.filter((t) => t.status === "completed" && !!t.warning).length,
    });
  };
  const clearCount = (q) => tasks.filter((t) => !isActive(t) && matchesSearch(t, q?.origin ?? "all", q?.query) && (q?.scope === "all" || matchesStatus(t, q?.status))).length;

  /**
   * 任务详情弹窗读的条目：按夹具里的「事实」合成一份满足它们的条目（trim 后的正文长度、文字稿长度、本地资产个数、标签）。
   * Viva 一侧直接用这些事实（viva/parity/importsfixture.go 的 ItemFacts）。
   */
  const buildItem = (id) => {
    const f = fixture.items && fixture.items[id];
    if (!f) return null;
    const refs = Array.from({ length: f.assetCount }, (_, i) => `![](local-image://parity-${i}.png)`).join("\n");
    const head = refs ? `${refs}\n` : "";
    return {
      id: f.id, title: f.title, content: head + "字".repeat(Math.max(0, f.bodyLength - head.length)),
      transcript: "字".repeat(f.transcriptLength), tags: f.tags,
    };
  };

  const patch = (api) => {
    if (!api || !api.import || api.import.__parity) return;
    if (api.knowledge) Object.assign(api.knowledge, { get: (id) => Promise.resolve(buildItem(id)) });
    Object.assign(api.import, {
      __parity: true,
      list,
      getQueueState: () => Promise.resolve(queueState()),
      pause: () => { config.paused = true; return Promise.resolve(queueState()); },
      resume: () => { config.paused = false; return Promise.resolve(queueState()); },
      cancel: () => Promise.resolve(true),
      retry: () => Promise.resolve(true),
      remove: () => Promise.resolve(true),
      clearFinished: () => Promise.resolve(0),
      previewClearTerminal: (q) => Promise.resolve({ count: clearCount(q), preview: true }),
      clearTerminal: (q) => Promise.resolve({ count: clearCount(q), preview: false }),
      selectFiles: () => Promise.resolve([]),
      enqueue: () => Promise.resolve([]),
    });
  };

  if (window.api) {
    patch(window.api);
    return;
  }
  let value;
  Object.defineProperty(window, "api", {
    configurable: true,
    enumerable: true,
    get: () => value,
    set: (next) => {
      value = next;
      patch(next);
    },
  });
}

/** 每个会话只登记一次初始化脚本；脚本自己看配置决定是否生效。 */
async function ensureRegistered(session) {
  if (session[REGISTERED]) return;
  await session.page.addInitScript(pageInit, { fixture: IMPORTS_FIXTURE, configKey: CONFIG_KEY });
  // 夹具的「现在」：Date.now() 与 new Date() 固定，定时器照常运行。
  await session.page.clock.setFixedTime(new Date(IMPORTS_FIXTURE.now));
  await session.cdp.send("Emulation.setTimezoneOverride", { timezoneId: IMPORTS_FIXTURE.timezone });
  session[REGISTERED] = true;
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/**
 * 给其他模块的场景用（文档站页在采集页里，侧栏与 rail 角标仍读采集队列）：先登记采集队列的页面初始化脚本与固定时钟，
 * 其余模块自己的脚本必须在它之后登记——两个脚本都要在 window.api 上装 setter，后登记的接着前一个的 setter 往下接。
 */
export { ensureRegistered as ensureImportsRegistered, CONFIG_KEY as IMPORTS_CONFIG_KEY };

/**
 * 进入采集页：写配置 → reload（让替换赶在渲染层启动之前）→ 点 rail「采集」。
 * options：{ mode: "data"|"empty"|"loading"|"error", errorMessage, paused, hasMore, overrides }；
 * overrides 是按任务 id 覆盖夹具字段的表（{ "task-completed": { refreshOfItemId: "item-005" } }）。
 */
export async function enterImports(session, options = {}) {
  await ensureRegistered(session);
  const config = { mode: "data", errorMessage: "", paused: false, hasMore: false, ...options };
  await session.page.evaluate(({ key, value }) => localStorage.setItem(key, JSON.stringify(value)), { key: CONFIG_KEY, value: config });
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: "采集", exact: true }).click();
  await page.getByPlaceholder("搜索标题或链接").waitFor({ state: "visible" });
  // 渲染层的 150 ms 去抖之后才取数，按模式等对应的标志出现（只用已有的文字与角色，不加 testid）。
  switch (config.mode) {
    case "empty":
      await page.getByText("还没有采集任务", { exact: true }).waitFor({ state: "visible" });
      break;
    case "error":
      await page.getByRole("alert").waitFor({ state: "visible" });
      break;
    case "loading":
      // 加载态的转圈要过了延迟才出现：等够 150 ms 去抖 + 250 ms 延迟。
      await page.waitForTimeout(600);
      break;
    default:
      await page.getByText(IMPORTS_FIXTURE.tasks.find((t) => t.id === "task-completed").displayName).first().waitFor({ state: "visible" });
  }
}

/** 离开采集页：清配置并 reload，把夹具连同页面状态一起收走。 */
export async function leaveImports(session) {
  await session.page.evaluate((key) => localStorage.removeItem(key), CONFIG_KEY);
  await reloadClean(session);
}
