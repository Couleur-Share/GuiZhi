/**
 * 全局浮层（M3-6a：命令面板，以及更新、首次使用设置清单、旧版数据迁移三个对话框）对照场景的数据层与操作。
 *
 * 数据层是页内替换（同 lib/react-capture.mjs 的思路）：命令面板的三个远程来源（knowledge.list、wiki.search、import.list）、
 * 更新对话框的版本与检查（electron.updater.*）、设置清单的转写引擎探测（funasr.status）都是调用时才从 window.api / window.electron 现取，
 * 所以场景进入时在页内换掉就够了。只有旧版迁移的探测（migration.detectLegacy）在 App 挂载时就读，要赶在渲染层启动之前：
 * 它走页面初始化脚本，由 localStorage 里的开关决定是否生效（开关没开时原样透传，不影响别的场景；同 lib/react-settings-core.mjs）。
 * 渲染层的组件与 store 一行不改，台账「渲染层改动登记」保持为空。
 *
 * 换掉的接口、打开的对话框状态与迁移的 localStorage 留到后面的场景里，所以场景离开时 reload 一次把页面还原（leaveOverlays）。
 * 夹具与 Viva 一侧（viva/parity/scenes/overlays.go）是同一份 JSON。可访问名与文案取自渲染层 i18n 目录（overlaysText），按 PARITY_LANG 取中文或英文。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, settle } from "./react-state.mjs";
import { settingsText } from "./react-settings-core.mjs";
import { openSettingsSection } from "./react-settings-nav.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const OVERLAYS_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/overlays-fixture.json"), "utf8"),
);

/** 渲染层 i18n 目录里的一条文案（按 PARITY_LANG 取中文或英文）。 */
export const overlaysText = settingsText;

const ARMED = "__parity.overlays";
const FLAG_KEY = "__parity.overlays.migration";
const REGISTERED = Symbol("parity.overlays.registered");
const MIGRATION_DISMISSED_KEY = "guizhi-migration-dismissed";

/** 在页内换掉读取接口（每次进入场景换一次，离开时 reload 还原）。页内函数不能引用外层变量，夹具经参数传入。 */
async function arm(session, fixture) {
  await session.page.evaluate(
    ({ fix, key }) => {
      if (window[key]) return;
      window[key] = true;
      const clone = (value) => JSON.parse(JSON.stringify(value));
      const emitAvailable = () => {
        window.__mygo.receive({
          t: "event",
          n: "ipc",
          p: { channel: "updater:status", args: [{ status: "available", info: { version: fix.update.version, releaseNotes: fix.update.releaseNotes } }] },
        });
      };
      // 命令面板的三个远程来源：只在有搜索词时才被调用，一律返回夹具里的命中。
      Object.assign(window.api.knowledge, {
        list: () => Promise.resolve({ entries: clone(fix.palette.knowledge), total: fix.palette.knowledge.length }),
      });
      Object.assign(window.api.wiki, { search: () => Promise.resolve(clone(fix.palette.wiki)) });
      Object.assign(window.api.import, {
        list: () => Promise.resolve({ entries: clone(fix.palette.tasks), active: [], nextCursor: "", total: fix.palette.tasks.length, counts: {}, degradedCount: 0 }),
      });
      // 设置清单：转写引擎未装。
      Object.assign(window.api.funasr, { status: () => Promise.resolve({ installed: false }) });
      // 更新对话框：打开时会强制检查一次（真实的检查要联网，结果也不确定）；换成「再推一次 available」，与 Viva 一侧打开时检查的结果一致。
      const updater = window.electron.updater;
      Object.assign(updater, {
        check: () => {
          setTimeout(emitAvailable, 30);
          return Promise.resolve({ success: true });
        },
        getVersion: () => Promise.resolve(fix.update.currentVersion),
        getPlatform: () => Promise.resolve("win32"),
        getInstallSource: () => Promise.resolve("direct"),
      });
    },
    { fix: fixture, key: ARMED },
  );
}

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：在 window.api 上装 setter，赋值瞬间把 migration.detectLegacy 换成
 * 「开关开着就返回夹具里的探测结果、否则透传」的版本。任何一步抛错都吞掉，免得影响普通页面。
 */
function pageInit({ fixture, flagKey }) {
  const active = () => {
    try {
      return localStorage.getItem(flagKey) === "1";
    } catch {
      return false;
    }
  };
  const patchApi = (api) => {
    if (!api || !api.migration || api.migration.__parityOverlays) return;
    try {
      api.migration.__parityOverlays = true;
    } catch {
      return;
    }
    const original = api.migration.detectLegacy?.bind(api.migration);
    if (typeof original !== "function") return;
    api.migration.detectLegacy = (...args) =>
      active() ? Promise.resolve({ itemCount: fixture.migration.itemCount, path: fixture.migration.path }) : original(...args);
  };
  try {
    // 别的初始化脚本（设置分区的 react-settings-core.mjs 等）可能已在同一属性上装了访问器：沿用它的存取，赋值之后接着 patch，不覆盖。
    const existing = Object.getOwnPropertyDescriptor(window, "api");
    if (existing && existing.get && existing.set) {
      const { get, set } = existing;
      Object.defineProperty(window, "api", {
        configurable: true,
        enumerable: true,
        get: () => get.call(window),
        set: (next) => {
          set.call(window, next);
          patchApi(next);
        },
      });
      const current = get.call(window);
      if (current) patchApi(current);
      return;
    }
    if (window.api) {
      patchApi(window.api);
      return;
    }
    let value;
    Object.defineProperty(window, "api", {
      configurable: true,
      enumerable: true,
      get: () => value,
      set: (next) => {
        value = next;
        patchApi(next);
      },
    });
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
  await session.page.addInitScript(pageInit, { fixture: OVERLAYS_FIXTURE, flagKey: FLAG_KEY });
  session[REGISTERED] = true;
  await reloadClean(session);
}

/** 命令面板：先走到设置页（遮罩背后是它；与 Viva 一侧 settingsWindow 的默认分区一致），再像快捷键那样派发 shortcut:search。 */
export async function openPalette(win, session, { query = "" } = {}) {
  await openSettingsSection(win, "general");
  await arm(session, OVERLAYS_FIXTURE);
  await win.evaluate(() => window.dispatchEvent(new CustomEvent("shortcut:search")));
  const dialog = win.getByRole("dialog", { name: overlaysText("commandPalette.title"), exact: true });
  await dialog.waitFor({ state: "visible" });
  const input = dialog.getByRole("textbox");
  await input.waitFor({ state: "visible" });
  if (query) {
    await input.fill(query);
    // 150ms 防抖之后远程结果才回来：等第一条知识命中出现。
    await dialog.getByText(OVERLAYS_FIXTURE.palette.knowledge[0].title, { exact: true }).waitFor({ state: "visible" });
    await dialog.getByText(OVERLAYS_FIXTURE.palette.tasks.at(-1).displayName, { exact: true }).waitFor({ state: "visible" });
  }
}

/**
 * 更新对话框「发现新版本」：主进程推 updater:status = available（顶栏出现更新按钮），再派发 open-update-dialog（顶栏按钮的同一条路径，
 * 不经点击，免得在按钮上留下悬停与焦点的痕迹）。
 */
export async function openUpdateDialog(win, session) {
  await openSettingsSection(win, "general");
  await arm(session, OVERLAYS_FIXTURE);
  await win.evaluate((fix) => {
    window.__mygo.receive({
      t: "event",
      n: "ipc",
      p: { channel: "updater:status", args: [{ status: "available", info: { version: fix.update.version, releaseNotes: fix.update.releaseNotes } }] },
    });
  }, OVERLAYS_FIXTURE);
  const pill = win.getByRole("button", { name: overlaysText("settings.updateAvailable"), exact: true });
  await pill.waitFor({ state: "visible" });
  // 启动时的自动检查回包（not-available）会把状态清掉：等一小会儿再确认按钮还在，免得采到一半消失。
  await win.waitForTimeout(400);
  await pill.waitFor({ state: "visible", timeout: 1000 });
  await win.evaluate(() => window.dispatchEvent(new CustomEvent("open-update-dialog")));
  const dialog = win.getByRole("dialog");
  await dialog.waitFor({ state: "visible" });
  await dialog.getByRole("button", { name: overlaysText("settings.downloadUpdate"), exact: true }).waitFor({ state: "visible" });
  // 打开时的强制检查（页内替换版）会再推一次 available：等它落地，免得采到「检查中」。
  await win.waitForTimeout(200);
}

/** 不经遮罩点击：对话框打开之后，遮罩拦着指针，背后页面的切换用派发点击事件（React 的合成事件照常响应）。 */
async function navigateBehind(win, section) {
  await win.getByTestId("rail-settings").dispatchEvent("click");
  await win.getByTestId(`settings-nav-${section}`).dispatchEvent("click");
  await win.getByTestId("settings-content-shell").waitFor({ state: "visible" });
}

/**
 * 首次使用设置清单：App 里的入口是「关于」页的「打开设置引导」按钮（requestSetupChecklist）；
 * 点开之后再把背后的页面切回设置的默认分区（与 Viva 一侧 settingsWindow 一致）。optionalOpen 时展开「更多可选设置」。
 */
export async function openSetupChecklist(win, session, { optionalOpen = false } = {}) {
  await openSettingsSection(win, "about");
  await arm(session, OVERLAYS_FIXTURE);
  await win.getByRole("button", { name: overlaysText("setup.openFromAbout"), exact: true }).click();
  const dialog = win.getByRole("dialog");
  await dialog.waitFor({ state: "visible" });
  await navigateBehind(win, "general");
  if (optionalOpen) {
    const name = overlaysText("setup.moreOptional").replace("{{count}}", "2");
    await dialog.getByRole("button", { name, exact: true }).click();
    await dialog.getByText(overlaysText("setup.itemEmbedding"), { exact: true }).waitFor({ state: "visible" });
    // 转写项在引擎探测完成之前是转圈：等探测落地（页内替换版立即返回）。
    await dialog.getByText(overlaysText("setup.itemTranscription"), { exact: true }).waitFor({ state: "visible" });
    await dialog.getByRole("button", { name: overlaysText("setup.configure"), exact: true }).first().waitFor({ state: "visible" });
  }
}

/**
 * 旧版数据迁移确认：探测在 App 挂载时读，所以要登记初始化脚本、开开关、清掉「已拒绝」标记再 reload；
 * reload 之后对话框已经压在默认页之上，背后的页面用派发点击切到设置的默认分区。
 */
export async function openMigrationConfirm(win, session) {
  await ensureRegistered(session);
  await session.page.evaluate(
    ({ flag, dismissed }) => {
      localStorage.setItem(flag, "1");
      localStorage.removeItem(dismissed);
    },
    { flag: FLAG_KEY, dismissed: MIGRATION_DISMISSED_KEY },
  );
  await reloadClean(session);
  const dialog = win.getByRole("dialog");
  await dialog.waitFor({ state: "visible" });
  await navigateBehind(win, "general");
  await dialog.getByRole("button", { name: overlaysText("migration.start"), exact: true }).waitFor({ state: "visible" });
}

/** 离开场景：关迁移开关、还原「已拒绝」标记，reload 把换掉的接口与对话框状态一并收走，再压掉动画。 */
export async function leaveOverlays(session) {
  await session.page.evaluate(
    ({ flag, dismissed }) => {
      localStorage.removeItem(flag);
      localStorage.setItem(dismissed, "1");
    },
    { flag: FLAG_KEY, dismissed: MIGRATION_DISMISSED_KEY },
  );
  await reloadClean(session);
}
