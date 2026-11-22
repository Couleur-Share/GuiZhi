/**
 * 处理中心（inbox）对照场景的数据层：把一份固定的夹具装进真实的渲染层，让界面处在确定的数据里。
 *
 * 为什么不直接往库里写数据：处理中心的条目由主进程按库里的内容与采集任务实时算出，「读取失败」「语义索引状态读取失败」「没有配置文本模型」
 * 这几种状态更是没有数据可写。这里改在页内换掉 window.api 的读取接口（渲染层每次调用都从 window.api 现取，换得掉）——
 * inbox.list、wiki.compiler（preview）、semantic.status、collection.list，以及 settings.get（模型是否已配置取自它返回的 aiModels）——
 * 渲染层的组件与 store 一行不改，台账「渲染层改动登记」保持为空。Viva 一侧用同一份夹具与同样的开关（viva/parity/inboxfixture.go、
 * viva/parity/scenes/inbox.go 的 inboxOptions）。
 *
 * 配置走 localStorage（键 CONFIG_KEY）+ 页面初始化脚本：App.tsx 启动即刷新处理中心（rail 角标要在任何模块里都准），
 * 所以替换必须赶在渲染层启动之前——脚本先于页面脚本运行，window.api 由壳的 contextBridge 垫片整体赋值，
 * 脚本在 window.api 上装一个 setter，赋值瞬间改好各子对象。场景离开时清掉配置并 reload，否则 rail 角标会残留到后面的整窗场景里。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, parityLanguage, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const INBOX_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/inbox-fixture.json"), "utf8"),
);

/**
 * 场景要点的可访问名与文案，按 PARITY_LANG 取中文或英文（取自渲染层 i18n 目录 zh.json / en.json 的 inbox.* 键）。
 * 名字写错时 getByRole 等不到元素、整个采集超时，不会悄悄点错；Viva 一侧对应 locales 的 Inbox* 一族。
 */
const LABELS = {
  zh: {
    nav: "处理中心", filterAll: "全部待处理", filterUnclassified: "未归合集", filterSemantic: "语义索引", filterWiki: "Wiki 编译",
    selectItem: (title) => `选择 ${title}`, groupDone: "当前分组已处理完", expand: "展开",
  },
  en: {
    nav: "Inbox", filterAll: "All pending", filterUnclassified: "Unfiled", filterSemantic: "Semantic index", filterWiki: "Wiki compile",
    selectItem: (title) => `Select ${title}`, groupDone: "All caught up in this group", expand: "Expand",
  },
};
export const inboxLabel = (key) => LABELS[parityLanguage()][key];

const CONFIG_KEY = "__parity.inbox";
const REGISTERED = Symbol("parity.inbox.registered");

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置替换 window.api 里处理中心要读的接口。
 * 任何一步抛错都吞掉，免得影响无配置的普通页面。
 */
function pageInit({ fixture, configKey }) {
  let config = null;
  try {
    config = JSON.parse(localStorage.getItem(configKey) || "null");
  } catch {
    return; // about:blank 等拿不到 localStorage 的文档
  }
  if (!config) return;

  const variant = fixture.variants[config.variant || "data"];

  /** 一个已配置好的模型：必填项都有值；embedding 专用模型显式关掉 chat 能力（路由不会把它当对话模型）。 */
  const model = (id, capabilities) => ({
    id, provider: "parity", apiProtocol: "openai", apiKey: "parity-key", apiUrl: "https://parity.invalid/v1", model: id,
    capabilities, enabled: true, isDefault: true,
  });
  // 语义索引要有 embedding 模型（没有就不查状态）；Wiki 要有对话模型（没有就是「尚未配置文本模型」）。
  const aiModels = [
    model("parity-embedding", { chat: false, embedding: true }),
    ...(config.wikiNeedsModel ? [] : [model("parity-chat", { chat: true })]),
  ];

  const list = () => (config.loadError
    ? Promise.reject(new Error(fixture.loadError))
    : Promise.resolve(JSON.parse(JSON.stringify(variant.list))));

  const patch = (api) => {
    if (!api || !api.inbox || api.inbox.__parity) return;
    Object.assign(api.inbox, { __parity: true, list });
    if (api.wiki) {
      Object.assign(api.wiki, {
        compiler: (input) => Promise.resolve(input && input.action === "preview"
          ? { ok: true, preview: { counts: variant.wiki } }
          : { ok: false, error: "parity" }),
      });
    }
    if (api.semantic) {
      Object.assign(api.semantic, {
        status: () => (config.semanticError
          ? Promise.reject(new Error(fixture.semanticError))
          : Promise.resolve({ eligibleItems: variant.semantic.eligibleItems, indexedItems: variant.semantic.indexedItems })),
      });
    }
    if (api.collection) Object.assign(api.collection, { list: () => Promise.resolve(fixture.collections) });
    if (api.settings && typeof api.settings.get === "function") {
      const original = api.settings.get.bind(api.settings);
      Object.assign(api.settings, {
        get: async (...args) => ({ ...(await original(...args)), aiModels, aiProviders: [] }),
      });
    }
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
  await session.page.addInitScript(pageInit, { fixture: INBOX_FIXTURE, configKey: CONFIG_KEY });
  session[REGISTERED] = true;
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/**
 * 进入处理中心：写配置 → reload（让替换赶在渲染层启动之前）→ 点 rail「处理中心」→ 等三块读取（主列表、Wiki、语义索引）都落定。
 * options：{ variant: "data"|"empty", loadError, wikiNeedsModel, semanticError }，与 Viva 一侧 inboxOptions 同名同义。
 */
export async function enterInbox(session, options = {}) {
  await ensureRegistered(session);
  const config = { variant: "data", loadError: false, wikiNeedsModel: false, semanticError: false, ...options };
  await session.page.evaluate(({ key, value }) => localStorage.setItem(key, JSON.stringify(value)), { key: CONFIG_KEY, value: config });
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: inboxLabel("nav"), exact: true }).click();
  await page.getByRole("heading", { name: inboxLabel("nav"), level: 2, exact: true }).waitFor({ state: "visible" });

  const variant = INBOX_FIXTURE.variants[config.variant];
  // 主列表：失败态等错误原文，空态等「已处理完」，其余等第一条的标题（没有标题的条目取下一条有标题的）。
  if (config.loadError) {
    await page.getByText(INBOX_FIXTURE.loadError, { exact: false }).first().waitFor({ state: "visible" });
  } else if (variant.list.items.length === 0) {
    await page.getByText(inboxLabel("groupDone"), { exact: true }).waitFor({ state: "visible" });
  } else {
    const titled = variant.list.items.find((item) => item.title);
    await page.getByText(titled.title, { exact: true }).first().waitFor({ state: "visible" });
  }
  // Wiki 与语义索引是各自异步的（动态 import 之后才读）：等它们在侧栏计数里或局部错误条里落定。
  if (variant.wiki.ready > 0) {
    await page.getByRole("button", { name: `${inboxLabel("filterWiki")} ${variant.wiki.ready}`, exact: true }).waitFor({ state: "visible" });
  }
  const pending = Math.max(0, variant.semantic.eligibleItems - variant.semantic.indexedItems);
  if (config.semanticError) {
    await page.getByRole("alert").filter({ hasText: INBOX_FIXTURE.semanticError }).waitFor({ state: "visible" });
  } else if (pending > 0) {
    await page.getByRole("button", { name: `${inboxLabel("filterSemantic")} ${pending}`, exact: true }).waitFor({ state: "visible" });
  }
}

/** 离开处理中心：清配置并 reload，把夹具连同页面状态一起收走。 */
export async function leaveInbox(session) {
  await session.page.evaluate((key) => localStorage.removeItem(key), CONFIG_KEY);
  await reloadClean(session);
}
