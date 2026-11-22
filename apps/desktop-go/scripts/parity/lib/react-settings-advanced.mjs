/**
 * 设置页三个外围分区（采集与转写、MCP 设置、AI 模型服务，M3-2b）对照场景的数据层。
 *
 * 数据层与核心分区（lib/react-settings-core.mjs）同一套做法，分两类：
 *   1. 只读接口页内替换（页面初始化脚本）：yt-dlp / ffmpeg / 本地转写引擎的探测结果、平台账号状态、MCP 启动信息与可访问范围、
 *      AI 用量汇总、联网搜索配置，都在 window.api 上换成「开关开着就返回夹具、否则透传」的版本；
 *   2. 模型服务的端点、模型与路由是设置本身（settings.aiProviders / aiModels / modelRouteDefaults），不是读接口：
 *      经真实的 settings.set 写进一次性 userData（与 Viva 一侧 services.AISettings 读写的是同一个配置键），
 *      同时改写 localStorage 里的持久化状态，再 reload 一次让渲染层 store 以它为准。每次 reload 要几秒，所以按状态缓存在会话上：
 *      状态没变就不重复写；场景编排里让「空态」排在最后（见 scripts 的 ids 文件），采集结束时会话回到空配置。
 *
 * 渲染层的组件与 store 一行不改，台账「渲染层改动登记」保持为空。夹具是与 Viva 场景共用的同一份 JSON
 * （viva/parity/testdata/settings-advanced-fixture.json）。
 *
 * 口令策略：端点的 apiKey 是合成占位串，只在端点卡的密码框里出现；场景不打开端点卡，也不在表单里输入口令
 * （Viva 的口令框从不把值放进可访问树，渲染层的是一串圆点，两侧 L1 状态必然不一致，且这一类偏差没有登记）。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, settle } from "./react-state.mjs";

export { settingsText } from "./react-settings-core.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const SETTINGS_ADVANCED_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/settings-advanced-fixture.json"), "utf8"),
);

const FLAG_KEY = "__parity.settings-advanced";
const AI_FLAG_KEY = "__parity.settings-advanced.ai";
const REGISTERED = Symbol("parity.settings-advanced.registered");
const AI_STATE = Symbol("parity.settings-advanced.ai-state");

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：在 window.api 上装 setter，赋值瞬间把几个读取接口换成
 * 「开关开着就返回夹具、否则透传」的版本。window.api 的访问器可能已被别的初始化脚本（核心分区的）装过：
 * 这里不覆盖它，而是在它的 setter 之后接着 patch，两套替换互不挤占。任何一步抛错都吞掉，免得影响普通页面。
 */
function pageInit({ fixture, flagKey, aiFlagKey }) {
  const flagOn = (key) => {
    try {
      return localStorage.getItem(key) === "1";
    } catch {
      return false;
    }
  };
  const clone = (value) => JSON.parse(JSON.stringify(value));
  /** 把 owner[name] 换成：开关开着返回夹具（深拷贝，渲染层改不到夹具），否则调原函数。 */
  const wrap = (owner, name, key, get, accept = () => true) => {
    if (!owner || typeof owner[name] !== "function") return;
    const original = owner[name].bind(owner);
    owner[name] = (...args) => (flagOn(key) && accept(args) ? Promise.resolve(clone(get())) : original(...args));
  };
  const patchApi = (api) => {
    if (!api || !api.mcp || api.mcp.__parityAdvanced) return;
    try {
      api.mcp.__parityAdvanced = true;
    } catch {
      return;
    }
    wrap(api.ytdlp, "status", flagKey, () => fixture.tools.ytdlp);
    wrap(api.ffmpeg, "status", flagKey, () => fixture.tools.ffmpeg);
    wrap(api.funasr, "status", flagKey, () => fixture.funasr);
    wrap(api.platformCapture, "getStatuses", flagKey, () => fixture.accounts);
    wrap(api.mcp, "getConfig", flagKey, () => fixture.mcp.config);
    wrap(api.mcp, "getScope", flagKey, () => fixture.mcp.scope);
    // 联网搜索配置：只替换「无参读取」，带参数的写入（保存、测试）照常走真实接口。
    wrap(api.themedReading, "searchConfig", flagKey, () => fixture.readingSearch, (args) => args.length === 0);
    wrap(api.ai, "usageSummary", aiFlagKey, () => fixture.ai.usage);
  };
  const hook = (name, patch) => {
    const existing = Object.getOwnPropertyDescriptor(window, name);
    if (existing && existing.get && existing.set) {
      // 已有访问器：沿用它的存取，赋值之后接着 patch。
      const { get, set } = existing;
      Object.defineProperty(window, name, {
        configurable: true,
        enumerable: true,
        get: () => get.call(window),
        set: (next) => {
          set.call(window, next);
          patch(next);
        },
      });
      const current = get.call(window);
      if (current) patch(current);
      return;
    }
    if (window[name]) {
      patch(window[name]);
      return;
    }
    let value;
    Object.defineProperty(window, name, {
      configurable: true,
      enumerable: true,
      get: () => value,
      set: (next) => {
        value = next;
        patch(next);
      },
    });
  };
  try {
    hook("api", patchApi);
  } catch {
    // about:blank 等拿不到 window 属性的文档
  }
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/** 每个会话只登记一次初始化脚本；登记后 reload 一次让它在渲染层启动之前运行。 */
async function ensureRegistered(session) {
  if (session[REGISTERED]) return;
  await session.page.addInitScript(pageInit, { fixture: SETTINGS_ADVANCED_FIXTURE, flagKey: FLAG_KEY, aiFlagKey: AI_FLAG_KEY });
  session[REGISTERED] = true;
  await reloadClean(session);
}

/** 进入需要夹具数据的分区之前调用：登记（首次）并打开只读接口的替换开关。 */
export async function armSettingsAdvanced(session) {
  await ensureRegistered(session);
  await session.page.evaluate((key) => localStorage.setItem(key, "1"), FLAG_KEY);
}

/** 离开时调用：关开关，之后的读取接口原样透传。 */
export async function disarmSettingsAdvanced(session) {
  await session.page.evaluate(([a, b]) => {
    localStorage.removeItem(a);
    localStorage.removeItem(b);
  }, [FLAG_KEY, AI_FLAG_KEY]);
}

/**
 * 把模型服务配置带到 state：data = 夹具里的两个端点与五个模型（用量也取夹具），empty = 没有任何端点与模型（用量走真实接口：空库为零）。
 * 状态没变不重复写（reload 要几秒）；会话刚开始时是空配置。
 */
export async function setAiState(session, state) {
  await ensureRegistered(session);
  await armSettingsAdvanced(session);
  if ((session[AI_STATE] ?? "empty") === state) {
    await session.page.evaluate(([key, on]) => (on ? localStorage.setItem(key, "1") : localStorage.removeItem(key)), [AI_FLAG_KEY, state === "data"]);
    return;
  }
  // 空配置要把旧版单模型字段也复位到出厂值：切到有数据的状态时主进程会把默认模型镜像进 aiApiKey / aiApiUrl，不复位的话空态页会多出「检测到旧版单模型配置」横幅（只有旧版配置时才显示）。
  const ai = state === "data" ? SETTINGS_ADVANCED_FIXTURE.ai : { providers: [], models: [], routes: {}, legacy: { aiProvider: "openai", aiApiProtocol: "openai", aiApiKey: "", aiApiUrl: "", aiModel: "gpt-4o" } };
  await session.page.evaluate(
    async ({ ai, flagKey, on }) => {
      const patch = { aiProviders: ai.providers, aiModels: ai.models, modelRouteDefaults: ai.routes, ...(ai.legacy ?? {}) };
      await window.api.settings.set(patch);
      const raw = localStorage.getItem("guizhi-settings");
      if (raw) {
        const stored = JSON.parse(raw);
        stored.state = { ...stored.state, ...patch };
        localStorage.setItem("guizhi-settings", JSON.stringify(stored));
      }
      if (on) localStorage.setItem(flagKey, "1");
      else localStorage.removeItem(flagKey);
    },
    { ai, flagKey: AI_FLAG_KEY, on: state === "data" },
  );
  await reloadClean(session);
  session[AI_STATE] = state;
}
