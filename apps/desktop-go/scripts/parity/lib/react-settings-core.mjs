/**
 * 设置页四个核心分区（网络、快捷键、数据与备份、关于）对照场景的数据层与文案。
 *
 * 数据层是页内替换（同处理中心的 lib/react-inbox.mjs）：这四页要读的几个接口——快捷键（electron.getShortcuts）、
 * 运行时路径（electron.getRuntimePaths）、应用版本（electron.updater.getVersion）、旧快照列表（api.backup.list）、
 * 完整备份仓库状态与快照（api.backup.repositoryStatus / listRepositorySnapshots）——在页面初始化脚本里换成夹具
 * （viva/parity/testdata/settings-core-fixture.json）。渲染层的组件与 store 一行不改，台账「渲染层改动登记」保持为空；
 * Viva 一侧用同一份夹具装配假服务（viva/parity/scenes/settings_core.go）。
 *
 * 替换是常驻的、由 localStorage 里的开关决定是否生效：开关没开时原样透传，不影响别的场景；
 * 场景进入时开、离开时关，所以不必为每个场景 reload（只有会话里第一次登记初始化脚本时 reload 一次）。
 *
 * 网络页的手动代理会把值写进设置（渲染层 store + 主进程），场景离开时要还原，否则后面的场景里代理模式还是「手动」。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, parityLanguage, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const SETTINGS_CORE_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/settings-core-fixture.json"), "utf8"),
);

const LOCALES_DIR = path.join(import.meta.dirname, "../../../../desktop/src/renderer/i18n/locales");
const catalogs = {};

/**
 * 渲染层 i18n 目录里的一条文案（按 PARITY_LANG 取中文或英文）：场景要点的可访问名与文字都从目录取，不手抄。
 * 键写错时直接抛错，不会悄悄点错元素。
 */
export function settingsText(key) {
  const lang = parityLanguage();
  catalogs[lang] ??= JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, `${lang}.json`), "utf8"));
  let node = catalogs[lang];
  for (const part of key.split(".")) node = node?.[part];
  if (typeof node !== "string") throw new Error(`i18n 目录里没有 ${lang} 的 ${key}`);
  return node;
}

const FLAG_KEY = "__parity.settings-core";
const REGISTERED = Symbol("parity.settings-core.registered");

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：在 window.api 与 window.electron 上装 setter，
 * 赋值瞬间把几个读取接口换成「开关开着就返回夹具、否则透传」的版本。任何一步抛错都吞掉，免得影响普通页面。
 */
function pageInit({ fixture, flagKey }) {
  const active = () => {
    try {
      return localStorage.getItem(flagKey) === "1";
    } catch {
      return false;
    }
  };
  const clone = (value) => JSON.parse(JSON.stringify(value));
  /** 把 owner[name] 换成：开关开着返回夹具（深拷贝，渲染层改不到夹具），否则调原函数。 */
  const wrap = (owner, name, get) => {
    if (!owner || typeof owner[name] !== "function") return;
    const original = owner[name].bind(owner);
    owner[name] = (...args) => (active() ? Promise.resolve(clone(get())) : original(...args));
  };
  const patchApi = (api) => {
    if (!api || !api.backup || api.backup.__parityCore) return;
    try {
      api.backup.__parityCore = true;
    } catch {
      return;
    }
    wrap(api.backup, "list", () => fixture.legacyBackups);
    wrap(api.backup, "repositoryStatus", () => fixture.repository.status);
    wrap(api.backup, "listRepositorySnapshots", () => fixture.repository.snapshots);
  };
  const patchElectron = (electron) => {
    if (!electron || electron.__parityCore) return;
    try {
      electron.__parityCore = true;
    } catch {
      return;
    }
    wrap(electron, "getShortcuts", () => fixture.shortcuts);
    wrap(electron, "getRuntimePaths", () => fixture.runtimePaths);
    wrap(electron.updater, "getVersion", () => fixture.version);
  };
  const hook = (name, patch) => {
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
    hook("electron", patchElectron);
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
  await session.page.addInitScript(pageInit, { fixture: SETTINGS_CORE_FIXTURE, flagKey: FLAG_KEY });
  session[REGISTERED] = true;
  await reloadClean(session);
}

/** 进入需要夹具数据的分区之前调用：登记（首次）并打开开关。之后再进入分区，读取接口返回夹具。 */
export async function armSettingsCore(session) {
  await ensureRegistered(session);
  await session.page.evaluate((key) => localStorage.setItem(key, "1"), FLAG_KEY);
}

/** 离开时调用：关开关，之后的读取接口原样透传。 */
export async function disarmSettingsCore(session) {
  await session.page.evaluate((key) => localStorage.removeItem(key), FLAG_KEY);
}

/** 渲染层默认的网络代理设置（packages/shared/types/settings.ts DEFAULT_NETWORK_PROXY_SETTINGS）。 */
const DEFAULT_PROXY = {
  mode: "system", protocol: "http", host: "", port: 7890, username: "", password: "", bypass: "<local>,localhost,127.0.0.1,::1",
};

/**
 * 还原网络代理设置：写主进程（settings.set，渲染层启动时以主进程为准），同时把 localStorage 里的持久化状态改回默认，再 reload。
 * 只在场景改过代理时调用（reload 要几秒）。
 */
export async function resetNetworkProxy(session) {
  await session.page.evaluate(async (proxy) => {
    await window.api.settings.set({ networkProxy: proxy });
    const raw = localStorage.getItem("guizhi-settings");
    if (raw) {
      const stored = JSON.parse(raw);
      stored.state = { ...stored.state, networkProxy: proxy };
      localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    }
  }, DEFAULT_PROXY);
  await reloadClean(session);
}
