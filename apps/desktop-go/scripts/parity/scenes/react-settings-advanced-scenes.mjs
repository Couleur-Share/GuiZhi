/**
 * 设置页三个外围分区（M3-2b）的 React 侧对照场景：采集与转写、MCP 设置、AI 模型服务（含空态、新建端点弹窗、快速配置向导）。
 *
 * 内容区场景取设置内容容器（region，与 settings.general 同口径；AI 页的容器是不滚动的整高工作台）；
 * 对话框场景取整窗（遮罩与面板压在 rail 与顶栏之上）。数据层在 lib/react-settings-advanced.mjs：只读接口页内替换，
 * 端点与模型经真实的 settings.set 写进一次性 userData。夹具与 Viva 一侧（viva/parity/scenes/settings_advanced.go）是同一份 JSON。
 * 可访问名与文案取自渲染层 i18n 目录（settingsText），按 PARITY_LANG 取中文或英文。
 *
 * AI 场景的顺序：有数据的三个（settings.ai、settings.ai.endpoint-dlg、settings.ai.wizard）连着排、「空态」排最后，
 * 这样一个变体里只在进入与离开数据态时各 reload 一次；采集结束时会话回到空配置。
 */
import {
  SETTINGS_ADVANCED_FIXTURE as FIX,
  armSettingsAdvanced,
  disarmSettingsAdvanced,
  setAiState,
  settingsText,
} from "../lib/react-settings-advanced.mjs";
import { SETTINGS_CONTENT_SCROLLER, openSettingsSection, scrollSettingsToEnd } from "../lib/react-settings-nav.mjs";

const T = settingsText;

/** 等「采集与转写」页的三个引擎行与平台账号行都读到夹具：版本号与账号描述落到页面上。 */
async function waitCaptureLoaded(win) {
  const ytdlp = FIX.tools.ytdlp.version.replace(/\./g, "-");
  await win.getByText(ytdlp, { exact: false }).first().waitFor({ state: "attached" });
  await win.getByText(`v${FIX.funasr.version}`, { exact: false }).first().waitFor({ state: "attached" });
  await win.getByText(FIX.accounts[0].browserVersion, { exact: false }).first().waitFor({ state: "attached" });
}

/** 进入 AI 页并等端点列表与用量块落定（有数据时）。 */
async function enterAi(win, session, state) {
  await setAiState(session, state);
  await openSettingsSection(win, "ai");
  if (state === "data") {
    for (const provider of FIX.ai.providers) {
      await win.getByRole("button", { name: provider.name }).first().waitFor({ state: "visible" });
    }
    // 用量块：按场景分组的切换钮出现，说明用量汇总已读到。
    await win.getByRole("button", { name: T("settings.aiUsageGroupScenario"), exact: true }).waitFor({ state: "visible" });
  } else {
    await win.getByText(T("settings.aiWorkbenchNoModels"), { exact: false }).first().waitFor({ state: "visible" });
  }
  // 联网搜索块读取是后台的：等「搜索服务」标签出现。
  await win.getByText(T("settings.readingSearchProvider"), { exact: true }).first().waitFor({ state: "visible" });
}

export const SETTINGS_ADVANCED_SCENES = {
  "settings.capture": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await armSettingsAdvanced(session);
      await openSettingsSection(win, "capture");
      await waitCaptureLoaded(win);
      await scrollSettingsToEnd(win);
    },
    async leave({ session }) {
      await disarmSettingsAdvanced(session);
    },
  },
  "settings.mcp": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await armSettingsAdvanced(session);
      await openSettingsSection(win, "mcp");
      await win.getByText(T("settings.mcpStatusReady"), { exact: false }).first().waitFor({ state: "visible" });
    },
    async leave({ session }) {
      await disarmSettingsAdvanced(session);
    },
  },
  "settings.ai": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await enterAi(win, session, "data");
    },
    async leave({ session }) {
      await disarmSettingsAdvanced(session);
    },
  },
  // 整窗：数据态的 AI 页上点「添加供应商」，弹出新建端点对话框（口令框留空）。
  "settings.ai.endpoint-dlg": {
    module: "settings",
    async enter({ win, session }) {
      await enterAi(win, session, "data");
      await win.getByRole("button", { name: T("settings.addProvider"), exact: true }).click();
      await win.getByRole("dialog").waitFor({ state: "visible" });
    },
    async leave({ session }) {
      await disarmSettingsAdvanced(session);
    },
  },
  // 整窗：数据态的 AI 页上点「快速配置」，弹出快速配置向导（向导是懒加载的，等对话框出现）。
  "settings.ai.wizard": {
    module: "settings",
    async enter({ win, session }) {
      await enterAi(win, session, "data");
      await win.getByRole("button", { name: T("settings.quickSetupOpen"), exact: true }).click();
      await win.getByRole("dialog").waitFor({ state: "visible" });
    },
    async leave({ session }) {
      await disarmSettingsAdvanced(session);
    },
  },
  // 无端点空态：「快速配置」是主色按钮，供应商列表是空态说明。排在 AI 场景的最后（见文件头）。
  "settings.ai.empty": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await enterAi(win, session, "empty");
    },
    async leave({ session }) {
      await disarmSettingsAdvanced(session);
    },
  },
};
