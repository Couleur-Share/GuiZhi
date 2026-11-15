/**
 * 原生界面（apps/desktop-go/native/settings）的 golden 基准：由本文件用真实的 TS 实现导出，Go 侧测试逐项核对。
 *
 * 渲染层的设置归一化（持久化 v3 迁移、废弃键清理、各类取值收敛）和 ui-storage 的合并规则是原生版必须保留的行为
 * （见 docs/agents/native-ui-development.md「设置」一节）。这里把它们固定成数据：
 *   - 本测试：TS 当前的行为必须等于已提交的 golden；有意改了规则，用 UPDATE_GOLDEN=1 重新导出并提交；
 *   - Go 测试：native/settings 的移植必须等于同一份 golden。
 * 两边任何一边变了而另一边没跟上，CI 都会红——每周合并 main 时不会悄悄漂移。
 *
 * 导出：UPDATE_GOLDEN=1 pnpm --filter @guizhi/desktop exec vitest run tests/unit/stores/settings-native-golden.test.ts
 *
 * 语料只含「类型正确」或「TS 自己会归一」的输入：类型错误的未归一字段（如 renderMarkdown: "yes"）在 TS 里原样保留，
 * Go 的类型化解码会退回默认值，那是有意的差异，不放进 golden（Go 侧有各自的测试）。
 * 旧版 v3 之前的 motionPreference 迁移等规则都在语料里。
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createDefaultSettingsValues } from "../../../src/renderer/stores/settings/settings-defaults";
import {
  mergeSettingsState,
  migrateSettingsState,
} from "../../../src/renderer/stores/settings/settings-persistence";
import type { SettingsState } from "../../../src/renderer/stores/settings/settings-types";

const GOLDEN_PATH = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../desktop-go/native/settings/testdata/golden-settings.json",
);

/** 与 settings.store.ts 的 persist.version 一致：版本相同时 zustand 不调用 migrate，只做 merge。 */
const STORE_VERSION = 3;

/** Go 侧 UI 与 App 结构体里的字段（不含 networkProxy / flareSolverr：它们的归一化规则另有 golden）。 */
const GO_KEYS = [
  // UI
  "themeMode", "themeColor", "themeHue", "themeSaturation", "customThemeHex", "fontSize",
  "backgroundImageEnabled", "backgroundImageFileName", "backgroundImageOpacity", "backgroundImageBlur",
  "motionPreference", "renderMarkdown", "editorMarkdownPreview", "autoSave", "showLineNumbers",
  "wikiCompileEnabled", "debugMode", "closeAction", "autoCheckUpdate", "useUpdateMirror",
  "updateChannel", "updateChannelExplicitlySet", "scenarioModelDefaults",
  // App
  "language", "launchAtStartup", "minimizeOnLaunch", "backgroundTasksEnabled", "enableNotifications",
  "backupAutoEnabled", "backupIntervalHours", "backupKeepCount", "ytDlpPath", "ffmpegPath", "transcribeDiarize",
] as const;

const UI_STORAGE_KEYS = [
  "appModule", "isSidebarCollapsed", "sidebarPanelWidth", "itemListPaneWidth", "wikiCatalogPaneWidth", "libraryViewMode",
] as const;

function currentDefaults(): SettingsState {
  // language 的默认值取自 navigator（环境相关），固定为 zh，与 Go 侧 DefaultApp 一致
  return { ...createDefaultSettingsValues(), language: "zh" } as SettingsState;
}

/** zustand persist 的有效加载结果：版本相同只 merge，否则先 migrate 再 merge。 */
function effective(state: unknown, version: number): SettingsState {
  const persisted = version === STORE_VERSION ? state : migrateSettingsState(state, version);
  return mergeSettingsState(persisted, currentDefaults());
}

function pick(state: SettingsState): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of GO_KEYS) {
    const value = (state as unknown as Record<string, unknown>)[key];
    // backgroundImageFileName 缺省时 TS 是 undefined（JSON 里不出现），Go 用空串
    out[key] = value === undefined ? "" : value;
  }
  return out;
}

interface SettingsCase {
  name: string;
  version: number;
  state: Record<string, unknown>;
}

const fullDefaults = (): Record<string, unknown> => {
  const values = createDefaultSettingsValues() as unknown as Record<string, unknown>;
  const { settingsUpdatedAt: _drop, ...rest } = values;
  return { ...rest, language: "zh" };
};

/**
 * 迁移路径（版本不是 3）的 TS 实现有个怪癖：快照里没有 language 时 migrate 先把它归一成 "en"，merge 时又盖过默认值。
 * 真实快照总带 language（zustand 持久化整份状态），所以这只会出现在手写的语料里；Go 侧缺 language 时取默认值，
 * 不复制这条怪癖（它只会让中文用户落到英文界面）。语料里给非 3 版本的快照补上 language，不把怪癖写进 golden。
 */
const withLanguage = (c: SettingsCase): SettingsCase =>
  c.version !== STORE_VERSION && !("language" in c.state)
    ? { ...c, state: { ...c.state, language: "zh" } }
    : c;

const SETTINGS_CASES: SettingsCase[] = ([
  { name: "空 state（全新或刚清空）", version: 3, state: {} },
  { name: "完整默认值", version: 3, state: fullDefaults() },
  {
    name: "自定义外观",
    version: 3,
    state: {
      themeMode: "dark", themeColor: "custom", themeHue: 172, themeSaturation: 55.5,
      customThemeHex: "#12ab9f", fontSize: "large", motionPreference: "reduced",
    },
  },
  {
    name: "未知取值被收敛（枚举与语言）",
    version: 3,
    state: { themeMode: "neon", fontSize: "huge", motionPreference: "bogus", closeAction: "quit", language: "fr" },
  },
  { name: "语言 zh-CN 归 zh", version: 3, state: { language: "zh-CN" } },
  { name: "语言 en-GB 归 en", version: 3, state: { language: "en-GB" } },
  { name: "语言大写 ZH 归 zh", version: 3, state: { language: "ZH" } },
  { name: "语言类型错误按空串归 en", version: 3, state: { language: 5 } },
  { name: "关闭行为 minimize", version: 3, state: { closeAction: "minimize" } },
  { name: "关闭行为 exit", version: 3, state: { closeAction: "exit" } },
  { name: "v0 的标准动效迁移为跟随系统", version: 0, state: { motionPreference: "standard" } },
  { name: "v1 的标准动效迁移为跟随系统", version: 1, state: { motionPreference: "standard" } },
  { name: "v2 的标准动效迁移为跟随系统", version: 2, state: { motionPreference: "standard" } },
  { name: "v3 起主动选的标准动效保留", version: 3, state: { motionPreference: "standard" } },
  { name: "v2 的关闭动效不受迁移影响", version: 2, state: { motionPreference: "off" } },
  { name: "历史 editorMarkdownPreview=false 被翻转（v3）", version: 3, state: { editorMarkdownPreview: false } },
  { name: "历史 editorMarkdownPreview=false 被翻转（v1）", version: 1, state: { ...fullDefaults(), editorMarkdownPreview: false } },
  {
    name: "已下线的同步字段（含明文凭据）不被带入",
    version: 2,
    state: {
      syncEnabled: true, webdavUrl: "https://dav.example.com", webdavUsername: "someone",
      webdavPassword: "plaintext-secret", s3AccessKeyId: "AKIAEXAMPLE", s3SecretAccessKey: "plaintext-secret",
      showCopyNotification: false, showSaveNotification: false, themeColor: "green",
    },
  },
  {
    name: "壁纸：合法文件名与 local-image 前缀",
    version: 3,
    state: { backgroundImageFileName: "local-image://a%20b.png", backgroundImageOpacity: 0.4, backgroundImageBlur: 8 },
  },
  { name: "壁纸：中文文件名", version: 3, state: { backgroundImageFileName: "壁纸.png" } },
  { name: "壁纸：百分号编码的中文名", version: 3, state: { backgroundImageFileName: "local-image://%E5%A3%81%E7%BA%B8.png" } },
  { name: "壁纸：前后空白与全角空格被去掉", version: 3, state: { backgroundImageFileName: "\u00a0a.png\u3000" } },
  { name: "壁纸：NEL 不是 JS 空白，保留", version: 3, state: { backgroundImageFileName: "\u0085a.png" } },
  { name: "壁纸：百分号序列非法时用原文", version: 3, state: { backgroundImageFileName: "local-image://%E0%A4%A" } },
  { name: "壁纸：远程地址被拒", version: 3, state: { backgroundImageFileName: "https://example.com/a.png" } },
  { name: "壁纸：data 地址被拒", version: 3, state: { backgroundImageFileName: "data:image/png;base64,AAAA" } },
  { name: "壁纸：blob 地址被拒（大写）", version: 3, state: { backgroundImageFileName: "BLOB:abc" } },
  { name: "壁纸：.. 被拒", version: 3, state: { backgroundImageFileName: "../a.png" } },
  { name: "壁纸：编码过的 .. 被拒", version: 3, state: { backgroundImageFileName: "local-image://%2e%2e%2fx.png" } },
  { name: "壁纸：路径分隔符被拒", version: 3, state: { backgroundImageFileName: "dir/a.png" } },
  { name: "壁纸：反斜杠被拒", version: 3, state: { backgroundImageFileName: "dir\\a.png" } },
  { name: "壁纸：问号被拒", version: 3, state: { backgroundImageFileName: "a?.png" } },
  { name: "壁纸：井号被拒", version: 3, state: { backgroundImageFileName: "a#1.png" } },
  { name: "壁纸：空串", version: 3, state: { backgroundImageFileName: "   " } },
  { name: "壁纸：类型错误", version: 3, state: { backgroundImageFileName: 12 } },
  { name: "壁纸开关类型错误取开", version: 3, state: { backgroundImageEnabled: "no" } },
  { name: "壁纸开关关闭", version: 3, state: { backgroundImageEnabled: false } },
  { name: "不透明度超上限", version: 3, state: { backgroundImageOpacity: 5 } },
  { name: "不透明度为负", version: 3, state: { backgroundImageOpacity: -3 } },
  { name: "不透明度类型错误取默认", version: 3, state: { backgroundImageOpacity: "0.5" } },
  { name: "旧默认模糊 14 保留", version: 3, state: { backgroundImageBlur: 14 } },
  { name: "模糊超上限", version: 3, state: { backgroundImageBlur: 99 } },
  { name: "模糊为负", version: 3, state: { backgroundImageBlur: -1 } },
  { name: "模糊类型错误取默认", version: 3, state: { backgroundImageBlur: "x" } },
  ...[0.25, 0.15, 0.35, 14.25, 33.35, 0.05, 1.45, 12.345, 49.95, 0.45, 0.95, 2.675, 10.15].map(
    (blur): SettingsCase => ({ name: `模糊保留一位小数 ${blur}`, version: 3, state: { backgroundImageBlur: blur } }),
  ),
  { name: "更新通道显式标记类型错误取否", version: 3, state: { updateChannelExplicitlySet: 1 } },
  { name: "更新通道 preview", version: 3, state: { updateChannel: "preview", updateChannelExplicitlySet: true } },
  {
    name: "场景默认模型：去空白、丢弃未知场景与空值",
    version: 3,
    state: { scenarioModelDefaults: { summary: " m1 ", qa: "", bogus: "x", tagging: 5, wiki: "w", ocr: "\u3000o\u00a0" } },
  },
  { name: "场景默认模型不是对象", version: 3, state: { scenarioModelDefaults: ["summary"] } },
  { name: "场景默认模型为 null", version: 3, state: { scenarioModelDefaults: null } },
  {
    name: "布尔开关全部取反",
    version: 3,
    state: {
      autoSave: false, showLineNumbers: true, wikiCompileEnabled: true, renderMarkdown: false, debugMode: true,
      autoCheckUpdate: false, useUpdateMirror: true, launchAtStartup: true, minimizeOnLaunch: true,
      backgroundTasksEnabled: true, enableNotifications: false, transcribeDiarize: true, backupAutoEnabled: false,
    },
  },
  { name: "备份数字在范围内", version: 3, state: { backupIntervalHours: 6, backupKeepCount: 30 } },
  { name: "工具路径原样保留", version: 3, state: { ytDlpPath: "C:\\tools\\yt-dlp.exe", ffmpegPath: "" } },
  {
    name: "AI 配置与快捷键模式不影响其余字段",
    version: 3,
    state: {
      aiProvider: "x", aiApiKey: "sk-secret", aiProviders: [{ id: "p", apiKey: "k" }], aiModels: [{ id: "m" }],
      modelRouteDefaults: { mainText: "m" }, shortcutModes: { showApp: "local" }, themeMode: "light",
    },
  },
] as SettingsCase[]).map(withLanguage);

interface UiStorageCase {
  name: string;
  state: Record<string, unknown>;
}

const UI_STORAGE_CASES: UiStorageCase[] = [
  { name: "空 state", state: {} },
  { name: "全部合法值", state: { appModule: "wiki", isSidebarCollapsed: true, sidebarPanelWidth: 300, itemListPaneWidth: 400, wikiCatalogPaneWidth: 250, libraryViewMode: "list" } },
  { name: "未知应用模块取知识库", state: { appModule: "settings" } },
  { name: "应用模块 ask", state: { appModule: "ask" } },
  { name: "应用模块 imports", state: { appModule: "imports" } },
  { name: "应用模块 inbox", state: { appModule: "inbox" } },
  { name: "宽度低于下限", state: { sidebarPanelWidth: 10, itemListPaneWidth: 10, wikiCatalogPaneWidth: 10 } },
  { name: "宽度高于上限", state: { sidebarPanelWidth: 9999, itemListPaneWidth: 9999, wikiCatalogPaneWidth: 9999 } },
  { name: "宽度恰在边界", state: { sidebarPanelWidth: 220, itemListPaneWidth: 720, wikiCatalogPaneWidth: 560 } },
  { name: "宽度是小数", state: { sidebarPanelWidth: 300.5 } },
  { name: "宽度为 null 取默认", state: { sidebarPanelWidth: null, itemListPaneWidth: null, wikiCatalogPaneWidth: null } },
  { name: "宽度是数字字符串取下限", state: { sidebarPanelWidth: "300", itemListPaneWidth: "x" } },
  { name: "宽度是布尔取下限", state: { sidebarPanelWidth: true } },
  { name: "视图模式 compact 归 list", state: { libraryViewMode: "compact" } },
  { name: "视图模式 comfortable 归 card", state: { libraryViewMode: "comfortable" } },
  { name: "视图模式未知取 card", state: { libraryViewMode: "grid" } },
  { name: "旧字段 libraryListDensity=compact", state: { libraryListDensity: "compact" } },
  { name: "旧字段 libraryListDensity=comfortable", state: { libraryListDensity: "comfortable" } },
  { name: "新旧字段并存时新字段优先", state: { libraryViewMode: "card", libraryListDensity: "compact" } },
  { name: "libraryViewMode 为 null 时用旧字段", state: { libraryViewMode: null, libraryListDensity: "compact" } },
  { name: "侧栏折叠标志", state: { isSidebarCollapsed: true } },
];

function writeOrCompare(actual: unknown): void {
  const text = `${JSON.stringify(actual, null, 2)}\n`;
  if (process.env.UPDATE_GOLDEN === "1") {
    fs.mkdirSync(path.dirname(GOLDEN_PATH), { recursive: true });
    fs.writeFileSync(GOLDEN_PATH, text);
    return;
  }
  const committed = fs.existsSync(GOLDEN_PATH) ? fs.readFileSync(GOLDEN_PATH, "utf8") : "";
  expect(
    JSON.parse(committed || "null"),
    "native/settings 的 golden 与 TS 当前行为不一致：确认这是有意的改动后，用 UPDATE_GOLDEN=1 重新导出并提交，同时更新 Go 侧移植",
  ).toEqual(JSON.parse(text));
}

describe("原生界面 settings golden（由 TS 实现导出）", () => {
  afterEach(() => {
    localStorage.clear();
    vi.resetModules();
  });

  it("设置归一化与 ui-storage 合并规则与已提交的 golden 一致", async () => {
    const settings = SETTINGS_CASES.map((c) => ({
      name: c.name,
      version: c.version,
      state: c.state,
      expect: pick(effective(c.state, c.version)),
    }));

    const uiStorage: Array<{ name: string; state: Record<string, unknown>; expect: Record<string, unknown> }> = [];
    for (const c of UI_STORAGE_CASES) {
      vi.resetModules();
      localStorage.clear();
      localStorage.setItem("ui-storage", JSON.stringify({ state: c.state, version: 0 }));
      const { useUIStore } = await import("../../../src/renderer/stores/ui.store");
      const loaded = useUIStore.getState() as unknown as Record<string, unknown>;
      const expected: Record<string, unknown> = {};
      for (const key of UI_STORAGE_KEYS) expected[key] = loaded[key];
      uiStorage.push({ name: c.name, state: c.state, expect: expected });
    }

    writeOrCompare({ goKeys: [...GO_KEYS], uiStorageKeys: [...UI_STORAGE_KEYS], settings, uiStorage });
  });
});
