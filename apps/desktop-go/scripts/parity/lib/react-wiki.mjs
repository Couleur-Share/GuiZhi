/**
 * Wiki（M3-5a）对照场景的数据层：把一份固定的夹具装进真实的渲染层，让界面处在确定的数据里。
 *
 * 为什么不直接往库里写页面：Wiki 的页面、链接、来源与「更新建议」由编译流程生成（要真模型），预览候选与待编译数也由主进程按库里的条目实时算出，
 * 「模型是否已配置」取自设置里的 aiModels。这里改在页内换掉 window.api 的读取接口（渲染层的组件与 store 每次调用都从 window.api 现取）：
 * wiki.catalog / backlinkCounts / status / getPage / listRevisions / search / graph / compiler（preview、jobs、suggestion）与 settings.get 的 aiModels。
 * 渲染层一行不改，台账「渲染层改动登记」保持为空。Viva 一侧用同一份夹具与同样的开关（viva/parity/wikifixture.go、viva/parity/scenes/wiki.go）。
 *
 * 配置走 localStorage（键 CONFIG_KEY）+ 页面初始化脚本：模型是否已配置在应用启动时由设置 store 读入，所以替换必须赶在渲染层启动之前
 * （脚本先于页面脚本运行，window.api 由壳的 contextBridge 垫片整体赋值，脚本在 window.api 上装一个 setter，赋值瞬间改好各子对象；做法同 react-ask.mjs）。
 * 页内替换的函数体 wikiPatch 是自包含的，由初始化脚本拼进页面。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, parityLanguage, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const WIKI_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/wiki-fixture.json"), "utf8"),
);

/** 场景要点的可访问名，按 PARITY_LANG 取中文或英文（渲染层 i18n 目录 zh.json / en.json）。名字写错时 getByRole 等不到元素、采集超时，不会悄悄点错。 */
const LABELS = {
  zh: {
    nav: "Wiki", edit: "编辑", compileStart: "开始所选资料",
    compilePending: (count) => `编译 ${count} 条新内容`, toc: (count) => `本页目录（${count} 节）`,
  },
  en: {
    nav: "Wiki", edit: "Edit", compileStart: "Compile selected items",
    compilePending: (count) => `Compile ${count} new items`, toc: (count) => `On this page (${count})`,
  },
};
export const wikiLabel = (key, ...args) => {
  const value = LABELS[parityLanguage()][key];
  return typeof value === "function" ? value(...args) : value;
};

const CONFIG_KEY = "__parity.wiki";
const REGISTERED = Symbol("parity.wiki.registered");

/**
 * 页内替换（在页内运行，必须自包含）：按夹具与配置改写 window.api 里 Wiki 要读写的接口。
 * config：{ empty（没有任何页面，用夹具里 empty 一组的状态与预览）, plain（页面都没有手动编辑标记：目录行里没有铅笔图标） }。
 * 计数写到 window.__parityWiki，供采集端等「各接口都回过一次」。
 */
function wikiPatch(api, fixture, config) {
  if (!api || !api.wiki || api.wiki.__parity) return;
  const seen = (window.__parityWiki = window.__parityWiki || { catalog: 0, status: 0, backlinks: 0, preview: 0, getPage: 0, suggestion: 0 });
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const pages = config.empty ? [] : config.plain ? fixture.pages.map((p) => ({ ...p, manualEditedAt: null })) : fixture.pages;
  const links = config.empty ? [] : fixture.links;
  const statusBase = config.empty ? fixture.empty.status : fixture.status;
  const preview = config.empty ? fixture.empty.preview : fixture.compile.preview;
  // 目录顺序：最近更新在前，同刻按 ID 升序（与 Go 假服务 WikiFake.orderedLocked 同序）。
  const ordered = () => [...pages].sort((a, b) => (b.updatedAt - a.updatedAt) || (a.id < b.id ? -1 : 1));
  const entryOf = (p) => ({
    id: p.id, title: p.title, normalizedTitle: p.normalizedTitle, kind: p.kind, summary: p.summary,
    aliasesJson: p.aliasesJson ?? null, manualEditedAt: p.manualEditedAt ?? null, updatedAt: p.updatedAt,
  });
  const linked = (from, to) => links.some((l) => l.from === from && l.to === to);

  Object.assign(api.wiki, {
    __parity: true,
    catalog: () => {
      seen.catalog += 1;
      return Promise.resolve(clone(ordered().map(entryOf)));
    },
    backlinkCounts: () => {
      seen.backlinks += 1;
      const counts = {};
      for (const l of links) counts[l.to] = (counts[l.to] ?? 0) + 1;
      return Promise.resolve(counts);
    },
    status: () => {
      seen.status += 1;
      return Promise.resolve({ pageCount: pages.length, compiledItemCount: statusBase.compiledItemCount, eligibleItemCount: statusBase.eligibleItemCount });
    },
    search: () => Promise.resolve([]),
    graph: () => Promise.resolve({ nodes: [], links: [], totalNodes: pages.length }),
    getPage: (id) => {
      seen.getPage += 1;
      const page = pages.find((p) => p.id === id);
      if (!page) return Promise.resolve(null);
      const backlinks = ordered().filter((p) => linked(p.id, id)).map(entryOf);
      const sources = (fixture.sources[id] ?? []).map((s) => ({ itemId: s.itemId, title: s.title, reviewStatus: s.reviewStatus, reviewReasons: s.reviewReasons }));
      return Promise.resolve(clone({ page, backlinks, sources }));
    },
    listRevisions: (id) => Promise.resolve(clone(fixture.revisions[id] ?? [])),
    // 采集不会写：万一被触发，回「不存在」（不落任何东西）。
    updatePage: () => Promise.resolve(false),
    deletePage: () => Promise.resolve(false),
    restoreRevision: () => Promise.resolve(false),
    compiler: (input) => {
      const action = input && input.action;
      if (action === "preview") {
        seen.preview += 1;
        return Promise.resolve({ ok: true, preview: clone(preview) });
      }
      if (action === "jobs") return Promise.resolve({ ok: true, jobs: [] });
      if (action === "suggestion" && !input.accept) {
        seen.suggestion += 1;
        const found = fixture.suggestions[input.pageId];
        return Promise.resolve(found ? { ok: true, suggestion: clone(found) } : { ok: true });
      }
      return Promise.resolve({ ok: false, error: "parity" });
    },
  });

  // Wiki 编译要有对话模型（没有就是「尚未配置 Wiki 模型」，开始按钮置灰）：settings.get 补一个已配置的 chat 模型。
  if (api.settings && typeof api.settings.get === "function") {
    const original = api.settings.get.bind(api.settings);
    const model = {
      id: fixture.models.chat, provider: "parity", apiProtocol: "openai", apiKey: "parity-key", apiUrl: "https://parity.invalid/v1",
      model: fixture.models.chat, capabilities: { chat: true }, enabled: true, isDefault: true,
    };
    Object.assign(api.settings, { get: async (...args) => ({ ...(await original(...args)), aiModels: [model], aiProviders: [] }) });
  }
}

/** 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置调 wikiPatch。任何一步抛错都吞掉，免得影响无配置的普通页面。 */
function pageInit({ fixture, configKey }, patch) {
  let config = null;
  try {
    config = JSON.parse(localStorage.getItem(configKey) || "null");
  } catch {
    return; // about:blank 等拿不到 localStorage 的文档
  }
  if (!config) return;
  const apply = (api) => { try { patch(api, fixture, config); } catch { /* 吞掉 */ } };
  if (window.api) {
    apply(window.api);
    return;
  }
  let value;
  Object.defineProperty(window, "api", {
    configurable: true,
    enumerable: true,
    get: () => value,
    set: (next) => {
      value = next;
      apply(next);
    },
  });
}

/** 每个会话只登记一次初始化脚本；脚本自己看配置决定是否生效。 */
async function ensureRegistered(session) {
  if (session[REGISTERED]) return;
  const arg = JSON.stringify({ fixture: WIKI_FIXTURE, configKey: CONFIG_KEY });
  await session.page.addInitScript({ content: `(${pageInit.toString()})(${arg}, ${wikiPatch.toString()});` });
  // 夹具的「现在」与时区：页面时间的写法取决于它们，两侧用同一组值才可比（做法同 react-ask.mjs）。
  await session.page.clock.setFixedTime(new Date(WIKI_FIXTURE.now));
  await session.cdp.send("Emulation.setTimezoneOverride", { timezoneId: WIKI_FIXTURE.timezone });
  session[REGISTERED] = true;
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/**
 * 进入 Wiki：写配置 → reload（让替换赶在渲染层启动之前）→ 点 rail「Wiki」→ 等目录、状态、反向链接计数与编译预览都回过一次并落定，
 * 再等顶栏的编译按钮出现（它要等预览与「已加载」都就绪）。
 * options：{ empty, plain }，与 Viva 一侧 wikiOptions 同名同义。
 */
export async function enterWiki(session, options = {}) {
  await ensureRegistered(session);
  const config = { empty: false, plain: false, ...options };
  await session.page.evaluate(({ key, value }) => localStorage.setItem(key, JSON.stringify(value)), { key: CONFIG_KEY, value: config });
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: wikiLabel("nav"), exact: true }).click();
  try {
    await page.waitForFunction(() => {
      const seen = window.__parityWiki;
      return !!seen && seen.catalog > 0 && seen.status > 0 && seen.backlinks > 0 && seen.preview > 0;
    });
  } catch (error) {
    // 超时时带上各接口的调用计数，免得盲猜是哪一个没回来。
    const seen = await page.evaluate(() => JSON.stringify(window.__parityWiki ?? null));
    throw new Error(`Wiki 页没有读齐数据（调用计数 ${seen}）：${error.message.split("\n")[0]}`);
  }
  const preview = config.empty ? WIKI_FIXTURE.empty.preview : WIKI_FIXTURE.compile.preview;
  await page.getByRole("button", { name: wikiLabel("compilePending", preview.counts.ready), exact: true }).waitFor({ state: "visible" });
  await settle(page, { quietMs: 200 });
}

/** 离开 Wiki：清配置并 reload，把夹具连同页面状态一起收走。 */
export async function leaveWiki(session) {
  await session.page.evaluate((key) => localStorage.removeItem(key), CONFIG_KEY);
  await reloadClean(session);
}
