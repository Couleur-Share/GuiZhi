/**
 * 采集文档站页（DocumentSitePanel）对照场景的数据层：把一份固定的夹具装进真实的渲染层，让界面处在确定的批次与页面明细里。
 *
 * 为什么不真的建批次：批次由主进程的爬虫按网址发现页面、逐页入库，状态随时间变化，「运行中」「已暂停」「有失败页」
 * 这些状态没有稳定的数据可写。这里改在页内换掉 window.api.webCapture 的 list / get 与四个批次操作
 * （渲染层每次调用都从 window.api 现取，换得掉）——渲染层的组件与 store 一行不改，台账「渲染层改动登记」保持为空。
 * Viva 一侧用同一份夹具（viva/parity/docsitefixture.go、viva/parity/scenes/docsite.go）。
 *
 * 文档站页是采集页里的一个视图（ImportsWorkspace 的 view 切换），侧栏与 rail 角标仍读采集队列，所以先装好采集队列的替换
 * （react-imports.mjs，取空队列），本文件的页面初始化脚本必须在它之后登记：两个脚本都要在 window.api 上装 setter
 * （壳的 contextBridge 垫片整体赋值 window.api），后登记的脚本接着前一个的 setter 往下接，两边的替换才都生效。
 * 配置走 localStorage（键 CONFIG_KEY）+ 页面初始化脚本；场景离开时清掉配置并 reload。
 */
import fs from "node:fs";
import path from "node:path";
import { IMPORTS_CONFIG_KEY, ensureImportsRegistered } from "./react-imports.mjs";
import { SETTLE_CSS, parityLanguage, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const DOCSITE_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/docsite-fixture.json"), "utf8"),
);

/**
 * 场景要点的可访问名与文案，按 PARITY_LANG 取中文或英文（取自渲染层 i18n 目录 zh.json / en.json 的
 * nav.imports、imports.empty、imports.searchPlaceholder、webCapture.* 键）。名字写错时 getByRole 等不到元素、整个采集超时，不会悄悄点错。
 */
const LABELS = {
  zh: {
    nav: "采集", empty: "还没有采集任务", search: "搜索标题或链接", importSite: "采集文档站",
    entryUrl: "入口网址", directoryScope: "目录范围",
  },
  en: {
    nav: "Capture", empty: "No capture tasks yet", search: "Search title or link", importSite: "Capture documentation site",
    entryUrl: "Entry URL", directoryScope: "Directory scope",
  },
};
export const docsiteLabel = (key) => LABELS[parityLanguage()][key];

const CONFIG_KEY = "__parity.docsite";
const REGISTERED = Symbol("parity.docsite.registered");

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置替换 window.api.webCapture 的接口。
 * 任何一步抛错都吞掉，免得影响无配置的普通页面。window.api.webCapture 的返回是 { ok, data } 信封（preload 的 invoke）。
 */
function pageInit({ fixture, configKey }) {
  let config = null;
  try {
    config = JSON.parse(localStorage.getItem(configKey) || "null");
  } catch {
    return; // about:blank 等拿不到 localStorage 的文档
  }
  if (!config) return;

  const variant = fixture.variants[config.variant || "empty"];
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const ok = (data) => Promise.resolve({ ok: true, data });
  // 调用计数：场景等「第一次读取落定」时用（没有批次时页面上没有任何可等的东西）。
  const calls = { list: 0, get: 0 };
  window.__parityDocsite = calls;

  const patch = (api) => {
    if (!api || !api.webCapture || api.webCapture.__parityDocsite) return;
    Object.assign(api.webCapture, {
      __parityDocsite: true,
      list: () => {
        calls.list += 1;
        return ok(clone(variant.jobs));
      },
      get: (id) => {
        calls.get += 1;
        const job = variant.jobs.find((j) => j.id === id) || null;
        return ok({ job: clone(job), pages: clone(variant.pages[id] || []) });
      },
      create: () => ok("parity-job"),
      pause: () => ok(null),
      resume: () => ok(null),
      cancel: () => ok(null),
      retry: () => ok(null),
    });
  };

  if (window.api) {
    patch(window.api);
    return;
  }
  // 采集队列的脚本（先登记、先运行）已经在 window.api 上装了 setter：保住它，赋值时先交给它，再换本模块的接口。
  const previous = Object.getOwnPropertyDescriptor(window, "api");
  if (previous && (previous.get || previous.set)) {
    Object.defineProperty(window, "api", {
      configurable: true,
      enumerable: true,
      get: previous.get,
      set: (next) => {
        if (previous.set) previous.set(next);
        patch(next);
      },
    });
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

/** 每个会话只登记一次初始化脚本；脚本自己看配置决定是否生效。必须在采集队列的脚本之后登记（见文件头）。 */
async function ensureRegistered(session) {
  await ensureImportsRegistered(session);
  if (session[REGISTERED]) return;
  await session.page.addInitScript(pageInit, { fixture: DOCSITE_FIXTURE, configKey: CONFIG_KEY });
  session[REGISTERED] = true;
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/**
 * 进入文档站页：写两份配置（采集队列取空队列、文档站取夹具里名为 variant 的数据组）→ reload → 点 rail「采集」→ 点页头的
 * 「采集文档站」→ 等页面出现并读完第一次批次列表（没有批次时靠调用计数，有批次时再等第一个批次的标题出现）。
 * options：{ variant: "empty"|"running"|"failed" }，与 Viva 一侧 importsOptions.docSite 同名同义。
 */
export async function enterDocsite(session, options = {}) {
  await ensureRegistered(session);
  const config = { variant: "empty", ...options };
  await session.page.evaluate(
    ({ importsKey, importsValue, key, value }) => {
      localStorage.setItem(importsKey, JSON.stringify(importsValue));
      localStorage.setItem(key, JSON.stringify(value));
    },
    {
      importsKey: IMPORTS_CONFIG_KEY,
      importsValue: { mode: "empty", errorMessage: "", paused: false, hasMore: false },
      key: CONFIG_KEY,
      value: config,
    },
  );
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: docsiteLabel("nav"), exact: true }).click();
  await page.getByPlaceholder(docsiteLabel("search")).waitFor({ state: "visible" });
  await page.getByText(docsiteLabel("empty"), { exact: true }).waitFor({ state: "visible" });
  await page.getByRole("button", { name: docsiteLabel("importSite"), exact: true }).click();
  await page.getByRole("heading", { name: docsiteLabel("importSite"), level: 2, exact: true }).waitFor({ state: "visible" });
  await page.waitForFunction(() => window.__parityDocsite && window.__parityDocsite.list >= 1);
  const variant = DOCSITE_FIXTURE.variants[config.variant];
  if (variant.jobs.length > 0) {
    await page.getByRole("button", { name: titlePattern(variant.jobs[0]) }).waitFor({ state: "visible" });
  }
}

/** 批次标题按钮的名字是「入口网址 · 状态」；状态文案随语言变，按网址前缀匹配。 */
export function titlePattern(job) {
  const url = job.input.seeds[0].url.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`^${url} · `);
}

/** 离开文档站页：清两份配置并 reload，把夹具连同页面状态一起收走。 */
export async function leaveDocsite(session) {
  await session.page.evaluate(
    ({ importsKey, key }) => {
      localStorage.removeItem(key);
      localStorage.removeItem(importsKey);
    },
    { importsKey: IMPORTS_CONFIG_KEY, key: CONFIG_KEY },
  );
  await reloadClean(session);
}
