/**
 * React 侧场景注册表：每个场景知道怎样从任意状态走到自己、取哪个区域。
 *
 * 场景 ID 与盘点清单（docs/agents/viva-parity-inventory/）一致：`<模块>.<屏幕>[.<状态>]`。
 * `enter(ctx)` 必须是幂等的——从任意界面状态出发都能走到该场景（同一会话里连续采集多个场景时，上一个场景留下的状态不能影响下一个）。
 * `region`：阶段 1 校准用的区域场景（取该元素的外框，Viva 侧在同尺寸画布里只画这块）；缺省是整个视口。
 *
 * 渲染层只允许加 data-testid / aria 属性（见台账「渲染层改动登记」）；能用现有角色与名称定位的，不去改渲染层。
 * `leave(ctx)`：可选，场景采集完后调用，把场景留下的东西收掉（提示场景用它收走提示、恢复页面）。
 */
import fs from "node:fs";
import path from "node:path";
import { resetToasts, showToasts } from "../lib/react-toast.mjs";

/** 打开设置并切到某个分区，等内容壳出现。 */
async function openSettingsSection(win, section) {
  // 上一个场景可能留下了模态对话框（遮罩会拦住后面的点击）：Esc 先把它与残留的浮层收起。
  await win.keyboard.press("Escape");
  await win.getByTestId("rail-settings").click();
  await win.getByTestId(`settings-nav-${section}`).click();
  await win.getByTestId("settings-content-shell").waitFor({ state: "visible" });
}

/**
 * 打开设置页「应用设置」里的下拉：先收起可能残留的浮层，点触发器，等 listbox 出现，再把指针挪到左上角——
 * 触发器与选项的悬停态不能混进画面（Viva 侧的无头会话没有指针）。
 * name 是触发器的可访问名（aria-label，简体中文界面）。
 * scrollEnd 为真时先把设置内容滚到底：语言下拉在窗口底部，不滚就被视口边缘截住（Viva 侧同样先滚到底）。
 */
async function openSettingsSelect(win, name, { scrollEnd = false } = {}) {
  await openSettingsSection(win, "general");
  await win.keyboard.press("Escape");
  if (scrollEnd) {
    await win.locator(SETTINGS_CONTENT_SCROLLER).evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
  }
  await win.getByRole("button", { name, exact: true }).click();
  await win.getByRole("listbox", { name, exact: true }).waitFor({ state: "visible" });
  await win.mouse.move(2, 2);
}

/**
 * 打开设置页「应用设置」里的「后台任务」确认对话框：把开关从关打开，等对话框出现。
 * 对话框不确认也不取消：场景停在它打开的状态；下一个场景的 openSettingsSection 先按 Esc 把它关掉。
 */
async function openBackgroundTasksDialog(win) {
  await openSettingsSection(win, "general");
  await win.getByRole("switch", { name: "后台任务", exact: true }).click();
  await win.getByRole("alertdialog").waitFor({ state: "visible" });
}

/** 提示场景的文案：与 Viva 场景共用同一份 JSON（viva/parity/testdata/toast-content.json）。 */
const TOAST_TEXT = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/toast-content.json"), "utf8"),
);

/** 提示场景：弹出一组提示（页面内容隐去，只留提示），可选再展开第一条的详情；离开时收走并恢复页面。 */
function toastScene(specs, { expandDetail = false } = {}) {
  return {
    module: "ui",
    async enter({ win }) {
      await showToasts(win, specs);
      if (expandDetail) {
        await win.getByRole("button", { name: "查看详情", exact: true }).first().click();
        await win.getByRole("button", { name: "复制", exact: true }).waitFor({ state: "visible" });
        await win.mouse.move(2, 2);
      }
    },
    async leave({ win }) {
      await resetToasts(win);
    },
  };
}

/** 设置内容滚动容器：内容壳的直接父元素（没有 testid，用结构选择器）。 */
const SETTINGS_CONTENT_SCROLLER = 'div:has(> [data-testid="settings-content-shell"])';

/** 设置页外壳（侧栏 + 内容）：SettingsPage 的根元素，即内容滚动容器的父元素。 */
const SETTINGS_SHELL = 'div:has(> div > [data-testid="settings-content-shell"])';

export const SCENES = {
  "settings.shell": {
    module: "settings",
    region: SETTINGS_SHELL,
    async enter({ win }) {
      await openSettingsSection(win, "general");
    },
  },
  // 整窗（顶栏 + rail + 设置页）；不给 region，取整个视口。
  "app.window.page-settings": {
    module: "app",
    async enter({ win }) {
      await openSettingsSection(win, "general");
    },
  },
  // 整窗 + 下拉面板展开（面板是 portal 到 body 的 fixed 元素，只能在整窗里对照）。
  "app.window.page-settings.select-close-action": {
    module: "app",
    async enter({ win }) {
      await openSettingsSelect(win, "关闭窗口行为");
    },
  },
  "app.window.page-settings.select-language": {
    module: "app",
    async enter({ win }) {
      await openSettingsSelect(win, "语言", { scrollEnd: true });
    },
  },
  // 整窗 + 「后台任务」确认对话框（模态：遮罩盖住整窗，面板居中）。
  "app.window.page-settings.dlg-background-tasks": {
    module: "app",
    async enter({ win }) {
      await openBackgroundTasksDialog(win);
    },
  },
  // 提示（Toast）：整窗，页面内容隐去，只留右下角的提示（见 lib/react-toast.mjs）。
  "ui.toast.success": toastScene([{ message: TOAST_TEXT.success, type: "success" }]),
  "ui.toast.info": toastScene([{ message: TOAST_TEXT.info, type: "info" }]),
  "ui.toast.warning": toastScene([{ message: TOAST_TEXT.warning, type: "warning" }]),
  "ui.toast.error": toastScene([{ message: TOAST_TEXT.error, type: "error" }]),
  "ui.toast.error-detail": toastScene([{ message: TOAST_TEXT.error, type: "error", detail: TOAST_TEXT.detail }]),
  "ui.toast.detail-expanded": toastScene([{ message: TOAST_TEXT.error, type: "error", detail: TOAST_TEXT.detail }], { expandDetail: true }),
  "ui.toast.action": toastScene([{ message: TOAST_TEXT.undo, undo: true }]),
  // 三条叠在一起：带详情的错误、成功、撤销（gap-3 堆叠、同宽、最新的在最下面）。
  "ui.toast.container": toastScene([
    { message: TOAST_TEXT.error, type: "error", detail: TOAST_TEXT.detail },
    { message: TOAST_TEXT.success, type: "success" },
    { message: TOAST_TEXT.undo, undo: true },
  ]),
  "settings.general": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openSettingsSection(win, "general");
    },
  },
  "settings.appearance": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openSettingsSection(win, "appearance");
    },
  },
};

export function sceneIds() {
  return Object.keys(SCENES);
}

export function getScene(id) {
  const scene = SCENES[id];
  if (!scene) throw new Error(`未注册的场景 ${id}（已注册：${sceneIds().join("、")}）`);
  return scene;
}
