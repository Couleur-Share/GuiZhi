/**
 * AI 问答（M3-4）对照场景的数据层：把一份固定的夹具装进真实的渲染层，让界面处在确定的数据里。
 *
 * 为什么不直接往库里写会话：会话的更新时间、置顶与「模型是否已配置」都要确定，且 AI 配置不在库里。这里改在页内换掉 window.api 的读取接口
 * （渲染层的组件与 store 每次调用都从 window.api 现取）：askSession.query / list / get / save / updateMeta、semantic.status、
 * settings.get 的 aiModels（问答是否可用取自它）与 knowledge.get（本文标题）。渲染层一行不改，台账「渲染层改动登记」保持为空。
 * Viva 一侧用同一份夹具与同样的开关（viva/parity/askfixture.go、viva/parity/scenes/ask.go）。
 *
 * 配置走 localStorage（键 CONFIG_KEY）+ 页面初始化脚本：模型是否已配置在应用启动时由设置 store 读入，所以替换必须赶在渲染层启动之前
 * （脚本先于页面脚本运行，window.api 由壳的 contextBridge 垫片整体赋值，脚本在 window.api 上装一个 setter，赋值瞬间改好各子对象）。
 * 页内替换的函数体 askPatch 是自包含的：既拼进初始化脚本，也能直接在已加载的页面里执行（知识库详情里的提问栏用后一种，见 applyAskPatch）。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, parityLanguage, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const ASK_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/ask-fixture.json"), "utf8"),
);

/** 场景要点的可访问名，按 PARITY_LANG 取中文或英文（渲染层 i18n 目录 zh.json / en.json）。名字写错时 getByRole 等不到元素、采集超时，不会悄悄点错。 */
const LABELS = {
  zh: { nav: "AI 问答", askTitle: "围绕本文提问" },
  en: { nav: "Ask AI", askTitle: "Ask about this article" },
};
export const askLabel = (key) => LABELS[parityLanguage()][key];

const CONFIG_KEY = "__parity.ask";
const ACTIVE_SESSION_KEY = "guizhi-ask-active-session"; // stores/ask.store.ts 的 ACTIVE_SESSION_STORAGE_KEY
const REGISTERED = Symbol("parity.ask.registered");

/**
 * 页内替换（在页内运行，必须自包含）：按夹具与配置改写 window.api 里问答要读写的接口。
 * config：{ notConfigured（没有文本模型）, models（改写 settings.get 与 semantic.status）, knowledge（改写 knowledge.get 的本文标题） }。
 * 计数写到 window.__parityAsk，供采集端等「各接口都回过一次」。
 */
function askPatch(api, fixture, config) {
  if (!api || !api.askSession || api.askSession.__parity) return;
  const seen = (window.__parityAsk = window.__parityAsk || { query: 0, list: 0, get: 0, semantic: 0 });
  const sessions = fixture.sessions;
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const metaOf = (s) => {
    const meta = { id: s.id, title: s.title, createdAt: s.createdAt, updatedAt: s.updatedAt };
    if (s.pinned) meta.pinned = true;
    if (s.scope) meta.scope = s.scope;
    if (s.itemId) meta.itemId = s.itemId;
    if (s.articleTitle) meta.articleTitle = s.articleTitle;
    if (s.webEnabled !== undefined && s.webEnabled !== null) meta.webEnabled = s.webEnabled;
    if (s.target) meta.target = s.target;
    return meta;
  };
  // 置顶在前，其次更新时间新的在前，再次 ID 大的在前（与 Go 假服务 ordered 同序）。
  const ordered = () => [...sessions].sort((a, b) => {
    if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1;
    if (a.updatedAt !== b.updatedAt) return b.updatedAt - a.updatedAt;
    return a.id < b.id ? 1 : -1;
  });
  const matches = (s, scope, itemId) => (!scope || (s.scope || "knowledge") === scope) && (!itemId || s.itemId === itemId);

  Object.assign(api.askSession, {
    __parity: true,
    query: (q = {}) => {
      seen.query += 1;
      const search = (q.search || "").trim().toLowerCase();
      const entries = ordered()
        .filter((s) => matches(s, q.scope, q.itemId))
        .filter((s) => (q.from == null || s.updatedAt >= q.from) && (q.to == null || s.updatedAt <= q.to))
        .filter((s) => !search || `${s.title}\n${JSON.stringify(s.messages)}`.toLowerCase().includes(search))
        .map(metaOf);
      const page = { entries, nextCursor: null };
      if (!q.cursor) {
        const articles = [];
        for (const s of ordered()) {
          if (s.scope === "article" && s.itemId && !articles.some((a) => a.id === s.itemId)) articles.push({ id: s.itemId, title: s.articleTitle || "" });
        }
        page.articles = articles;
      }
      return Promise.resolve(clone(page));
    },
    list: (filter = {}) => {
      seen.list += 1;
      return Promise.resolve(clone(ordered().filter((s) => matches(s, filter.scope, filter.itemId)).slice(0, 100).map(metaOf)));
    },
    get: (id) => {
      seen.get += 1;
      const found = sessions.find((s) => s.id === id);
      return Promise.resolve(found ? clone({ ...metaOf(found), messagesJson: JSON.stringify(found.messages) }) : null);
    },
    // 采集不会改会话；万一保存或改元数据被触发，回一份与入参一致的记录（不落任何东西）。
    save: (input) => Promise.resolve(clone({ ...input, createdAt: fixture.now, updatedAt: fixture.now })),
    updateMeta: (id, patch) => Promise.resolve(clone({ ...metaOf(sessions.find((s) => s.id === id)), ...patch, messagesJson: "[]" })),
  });

  if (config.models) {
    const model = (id, capabilities) => ({
      id, provider: "parity", apiProtocol: "openai", apiKey: "parity-key", apiUrl: "https://parity.invalid/v1", model: id,
      capabilities, enabled: true, isDefault: true,
    });
    // 没有配置文本模型时只留 embedding 模型（语义索引卡照常出现；做法同 react-inbox.mjs 的 wikiNeedsModel）：
    // 把模型清空后，同一采集会话里后面的场景读不到语义索引状态（观察到的现象：模型清空的场景之后，下一个场景里 semantic.status 一次也没被调用）。
    const aiModels = [
      model(fixture.models.embedding, { chat: false, embedding: true }),
      ...(config.notConfigured ? [] : [model(fixture.models.chat, { chat: true })]),
    ];
    if (api.settings && typeof api.settings.get === "function") {
      const original = api.settings.get.bind(api.settings);
      Object.assign(api.settings, { get: async (...args) => ({ ...(await original(...args)), aiModels, aiProviders: [] }) });
    }
    if (api.semantic) {
      Object.assign(api.semantic, {
        status: () => {
          seen.semantic += 1;
          return Promise.resolve({ eligibleItems: fixture.semantic.eligibleItems, indexedItems: fixture.semantic.indexedItems });
        },
      });
    }
  }
  if (config.knowledge && api.knowledge) {
    const real = api.knowledge.get.bind(api.knowledge);
    Object.assign(api.knowledge, {
      get: (id) => (id === fixture.article.itemId
        ? Promise.resolve({ id, title: fixture.article.title, deletedAt: null })
        : real(id)),
    });
  }
}

/** 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置调 askPatch。任何一步抛错都吞掉，免得影响无配置的普通页面。 */
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
  const arg = JSON.stringify({ fixture: ASK_FIXTURE, configKey: CONFIG_KEY });
  await session.page.addInitScript({ content: `(${pageInit.toString()})(${arg}, ${askPatch.toString()});` });
  // 夹具的「现在」与时区：会话时间与证据采集时间的写法取决于它们，两侧用同一组值才可比（做法同 react-library.mjs）。
  await session.page.clock.setFixedTime(new Date(ASK_FIXTURE.now));
  await session.cdp.send("Emulation.setTimezoneOverride", { timezoneId: ASK_FIXTURE.timezone });
  session[REGISTERED] = true;
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/**
 * 在已加载的页面里执行页内替换（知识库详情的提问栏：知识库场景自己的初始化脚本占着 window.api，不能再装第二个）。
 * 只改会话读写接口；本文标题由知识库场景自己的 knowledge.get 提供。
 */
export async function applyAskPatch(win) {
  await win.evaluate(`(${askPatch.toString()})(window.api, ${JSON.stringify(ASK_FIXTURE)}, {})`);
}

/**
 * 进入 AI 问答：写配置与「上次会话」→ reload（让替换赶在渲染层启动之前）→ 点 rail「AI 问答」→ 等各接口都回过一次并落定。
 * options：{ remembered: 会话 ID（空表示没有上次会话，进空态）, notConfigured: 没有配置文本模型 }，与 Viva 一侧 askOptions 同名同义。
 */
export async function enterAsk(session, options = {}) {
  await ensureRegistered(session);
  const config = { models: true, knowledge: true, notConfigured: false, ...options };
  const remembered = options.remembered || "";
  await session.page.evaluate(({ key, value, activeKey, active }) => {
    localStorage.setItem(key, JSON.stringify(value));
    if (active) localStorage.setItem(activeKey, active);
    else localStorage.removeItem(activeKey);
  }, { key: CONFIG_KEY, value: config, activeKey: ACTIVE_SESSION_KEY, active: remembered });
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: askLabel("nav"), exact: true }).click();
  // 侧栏的历史列表、上次会话的读取、语义索引状态（配置了模型才查）。
  try {
    await page.waitForFunction(({ withGet, withSemantic }) => {
      const seen = window.__parityAsk;
      return !!seen && seen.query > 0 && (!withGet || seen.get > 0) && (!withSemantic || seen.semantic > 0);
    }, { withGet: !!remembered, withSemantic: true });
  } catch (error) {
    // 超时时带上各接口的调用计数，免得盲猜是哪一个没回来。
    const seen = await page.evaluate(() => JSON.stringify(window.__parityAsk ?? null));
    throw new Error(`问答页没有读齐数据（调用计数 ${seen}）：${error.message.split("\n")[0]}`);
  }
  await settle(page, { quietMs: 200 });
}

/** 离开 AI 问答：清配置与上次会话并 reload，把夹具连同页面状态一起收走。 */
export async function leaveAsk(session) {
  await session.page.evaluate(({ key, activeKey }) => {
    localStorage.removeItem(key);
    localStorage.removeItem(activeKey);
  }, { key: CONFIG_KEY, activeKey: ACTIVE_SESSION_KEY });
  await reloadClean(session);
}

