/**
 * 设置页四个核心分区（M3-2a）的 React 侧对照场景：网络（含手动代理）、快捷键、数据与备份（含「导出配置」对话框）、关于。
 *
 * 内容区场景取设置滚动区（region，与 settings.general 同口径）；对话框场景取整窗（遮罩与面板压在 rail 与顶栏之上）。
 * 数据层是页内替换（lib/react-settings-core.mjs）：快捷键、运行时路径、版本、旧快照与完整备份仓库都取夹具
 * （viva/parity/testdata/settings-core-fixture.json）；Viva 一侧（viva/parity/scenes/settings_core.go）做同样的操作。
 * 可访问名与文案取自渲染层 i18n 目录（settingsText），按 PARITY_LANG 取中文或英文。
 */
import {
  SETTINGS_CORE_FIXTURE as FIX,
  armSettingsCore,
  disarmSettingsCore,
  resetNetworkProxy,
  settingsText,
} from "../lib/react-settings-core.mjs";
import { SETTINGS_CONTENT_SCROLLER, openSettingsSection, scrollSettingsToEnd } from "../lib/react-settings-nav.mjs";

const T = settingsText;

/** 点开名为 name 的下拉，再点名为 option 的选项（选项是 role=option，名字是它的文字）。 */
async function pickOption(win, name, option) {
  await win.getByRole("button", { name, exact: true }).click();
  await win.getByRole("option", { name: option, exact: true }).click();
  await win.getByRole("listbox").waitFor({ state: "hidden" });
}

/**
 * 选「手动代理」，再逐项填协议、主机、端口、账号、绕过列表（值取自夹具）。口令框不填：Viva 的口令框从不把值放进可访问树，
 * 渲染层的是一串圆点，填了两边的 L1 状态必然不一致（见 viva/parity/settingscorefixture.go 的 SettingsCoreProxy）。
 */
async function openManualProxy(win) {
  const p = FIX.proxy;
  await openSettingsSection(win, "network");
  await pickOption(win, T("settings.networkProxyMode"), T("settings.networkProxyModeManual"));
  await pickOption(win, T("settings.networkProxyProtocol"), p.protocol.toUpperCase());
  await win.getByLabel(T("settings.networkProxyHost"), { exact: true }).fill(p.host);
  await win.getByLabel(T("settings.networkProxyPort"), { exact: true }).fill(String(p.port));
  await win.getByLabel(T("settings.networkProxyUsername"), { exact: true }).fill(p.username);
  await win.getByLabel(T("settings.networkProxyBypass"), { exact: true }).fill(p.bypass);
}

/** 等「数据与备份」页的后台读取（旧快照、运行时路径、完整备份仓库）都落到页面上。 */
async function waitDataLoaded(win) {
  const lastLegacy = FIX.legacyBackups[FIX.legacyBackups.length - 1];
  await win.getByText(lastLegacy.fileName, { exact: false }).first().waitFor({ state: "attached" });
  await win.locator("button:not([disabled])", { hasText: T("settings.openBackupsDir") }).waitFor({ state: "attached" });
  await win
    .getByRole("button", { name: T("settings.fullBackupRestoreAction"), exact: true })
    .nth(FIX.repository.snapshots.length - 1)
    .waitFor({ state: "attached" });
}

/** 进入「数据与备份」：夹具开关打开，切分区，等读取落定。 */
async function enterData(win, session) {
  await armSettingsCore(session);
  await openSettingsSection(win, "data");
  await waitDataLoaded(win);
}

export const SETTINGS_CORE_SCENES = {
  "settings.network": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openSettingsSection(win, "network");
    },
  },
  "settings.network.manual": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openManualProxy(win);
    },
    // 手动代理的值写进了设置：还原，否则后面的场景里代理模式还是「手动」。
    async leave({ session }) {
      await resetNetworkProxy(session);
    },
  },
  "settings.shortcuts": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await armSettingsCore(session);
      await openSettingsSection(win, "shortcuts");
      // 读取是后台的：等夹具里的组合键出现在页面上。组合键显示在只读文本框的 value 里（"Ctrl + Alt + G"），不是文本节点。
      const shown = FIX.shortcuts.showApp.replace(/\+/g, " + ");
      await win.waitForFunction((want) => [...document.querySelectorAll("input[readonly]")].some((el) => el.value === want), shown);
    },
    async leave({ session }) {
      await disarmSettingsCore(session);
    },
  },
  "settings.data": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await enterData(win, session);
    },
    async leave({ session }) {
      await disarmSettingsCore(session);
    },
  },
  // 整窗：数据与备份页滚到底（配置迁移在最下面），点「导出到文件」弹出「导出配置」对话框。
  "settings.data.export-dlg": {
    module: "settings",
    async enter({ win, session }) {
      await enterData(win, session);
      await scrollSettingsToEnd(win);
      await win.getByRole("button", { name: T("settings.configExportStart"), exact: true }).click();
      await win.getByRole("dialog").waitFor({ state: "visible" });
    },
    async leave({ session }) {
      await disarmSettingsCore(session);
    },
  },
  "settings.about": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win, session }) {
      await armSettingsCore(session);
      await openSettingsSection(win, "about");
      await win.getByText(FIX.version, { exact: false }).first().waitFor({ state: "attached" });
    },
    async leave({ session }) {
      await disarmSettingsCore(session);
    },
  },
};
