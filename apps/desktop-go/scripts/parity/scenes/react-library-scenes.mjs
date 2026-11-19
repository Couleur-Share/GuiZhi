/**
 * 知识库侧栏（library）的 React 侧对照场景。整窗对照，工作区隐去（见 lib/react-library.mjs）：
 * 比较的是 rail + 顶栏 + 侧栏面板，以及面板上弹出的菜单、对话框与提示。
 *
 * 每个场景进入时装夹具并 reload，可选再做一段交互（steps）；离开时清夹具并 reload。
 * Viva 一侧（viva/parity/scenes/library.go）做同样的操作，最终画面才可比。
 * 紧凑布局（视口 ≤ 1179.98px）下侧栏默认收起，enterLibrary 已先把它以浮层展开。
 */
import { enterLibrary, leaveLibrary } from "../lib/react-library.mjs";

/** 场景：options 选夹具数据与智能视图集，steps 把页面带到要对照的状态；flags 是 keepPointer / keepFocus。 */
function libraryScene(options, steps, flags = {}) {
  return {
    module: "library",
    ...flags,
    async enter({ win, session }) {
      await enterLibrary(session, options);
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveLibrary(session);
    },
  };
}

/** 侧栏滚动容器（SidebarLibraryPanel 的根元素）。 */
const SIDEBAR_SCROLLER = "div.overflow-y-auto.px-2.pb-3";

/** 行的主按钮（可访问名就是行标签，计数在按钮之外）。 */
const rowButton = (win, label) => win.getByRole("button", { name: label, exact: true }).first();

/** 行本身（group/row 容器）。 */
const rowOf = (win, label) => rowButton(win, label).locator("xpath=ancestor::*[contains(@class,'group/row')][1]");

/** 范围页签：可访问名是「标签 + 计数角标」，按前缀找。 */
const scopeTab = (win, label) => win.getByRole("button", { name: new RegExp(`^${label}`) }).first();

/** 点一下，等状态落定（筛选触发的取数很快，给 React 一个渲染间隙）。 */
async function clickAndSettle(locator, win) {
  await locator.click();
  await win.waitForTimeout(250);
}

const clickRow = (label) => (win) => clickAndSettle(rowButton(win, label), win);
const clickScope = (label) => (win) => clickAndSettle(scopeTab(win, label), win);

/** 把侧栏滚到底（行很多时回收站钉在最底）。 */
async function scrollSidebarToEnd(win) {
  await win.locator(SIDEBAR_SCROLLER).evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
}

/** 叠加多个筛选：先切范围，再依次选合集、平台、标签（它们与范围正交叠加）。 */
async function selectFacets(win) {
  await clickAndSettle(scopeTab(win, "收藏"), win);
  await clickAndSettle(rowButton(win, "学习笔记"), win);
  await clickAndSettle(rowButton(win, "哔哩哔哩"), win);
  await clickAndSettle(rowButton(win, "产品设计"), win);
}

/** 点标签分区标题上的眼睛：列出没有条目的标签。 */
async function showUnusedTags(win) {
  await clickAndSettle(win.getByRole("button", { name: /^显示 \d+ 个未使用的标签/ }), win);
}

/** 把指针停在行的左内边距处：行进入悬停态（底色、「更多」按钮替换计数），指针不落在任何控件的悬停样式上。 */
function hoverRow(label) {
  return async (win) => {
    const box = await rowOf(win, label).boundingBox();
    await win.mouse.move(box.x + 8, box.y + box.height / 2);
  };
}

/** 键盘聚焦：先按一次 Tab 让页面进入键盘模态，再程序聚焦行的主按钮（此时 :focus-visible 成立）。 */
function focusRow(label) {
  return async (win) => {
    await win.keyboard.press("Tab");
    await rowButton(win, label).focus();
  };
}

/** 点行尾的「更多操作」打开菜单（按钮平时透明但可点，点击时行进入悬停态）。 */
function openRowMenu(label) {
  return async (win) => {
    await rowOf(win, label).getByRole("button", { name: "更多操作", exact: true }).click();
    await win.getByRole("menu").waitFor({ state: "visible" });
  };
}

/** 在行上点右键打开菜单。 */
function rightClickRow(label) {
  return async (win) => {
    await rowOf(win, label).click({ button: "right", position: { x: 40, y: 16 } });
    await win.getByRole("menu").waitFor({ state: "visible" });
  };
}

/** 标签行右键菜单里展开「颜色」子菜单：键盘走（ArrowDown 到「颜色」，ArrowRight 展开并聚焦第一项），不依赖指针停在哪里。 */
function openTagColors(label) {
  return async (win) => {
    await rightClickRow(label)(win);
    await win.keyboard.press("ArrowDown");
    await win.keyboard.press("ArrowRight");
    await win.getByRole("menuitem", { name: "红", exact: true }).waitFor({ state: "visible" });
  };
}

/** 打开菜单再点某一项，等对话框出现。 */
function openRowMenuItem(label, item) {
  return async (win) => {
    await openRowMenu(label)(win);
    await win.getByRole("menuitem", { name: item, exact: true }).click();
    await win.getByRole("dialog").waitFor({ state: "visible" });
  };
}

/** 点分区标题上的「新建合集」，等对话框出现。 */
async function openNewCollection(win) {
  await win.getByRole("button", { name: "新建合集", exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "visible" });
}

/** 新建合集对话框里填名称并选一个图标。 */
async function fillNewCollection(win) {
  await openNewCollection(win);
  const dialog = win.getByRole("dialog");
  await dialog.getByPlaceholder("名称", { exact: true }).fill("读书笔记");
  await dialog.getByRole("button", { name: "📖", exact: true }).click();
}

/** 点「保存当前筛选为智能视图」，等对话框出现。 */
async function openSmartViewDialog(win) {
  await win.getByRole("button", { name: "保存当前筛选为智能视图", exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "visible" });
}

async function fillSmartView(win) {
  await openSmartViewDialog(win);
  await win.getByRole("dialog").getByPlaceholder("例如：工作库的收藏视频", { exact: true }).fill("工作库的收藏视频");
}

/** 从菜单里删除合集：行立即消失，底部弹出带「撤销」的提示。 */
async function deleteCollectionWithToast(win) {
  await openRowMenu("灵感")(win);
  await win.getByRole("menuitem", { name: /^删除合集/ }).click();
  await win.getByText("已删除合集「灵感」，其中的条目已保留", { exact: true }).waitFor({ state: "visible" });
}

/** 从菜单里删除标签：行立即消失，底部弹出带「撤销」的提示（文案与合集的不同）。 */
async function deleteTagWithToast(win) {
  await openRowMenu("待读")(win);
  await win.getByRole("menuitem", { name: "删除标签", exact: true }).click();
  await win.getByText("已删除标签「待读」，相关条目已保留", { exact: true }).waitFor({ state: "visible" });
}

export const LIBRARY_SCENES = {
  // 默认与各种数据形态
  "app.window.library": libraryScene({}),
  "app.window.library.empty": libraryScene({ variant: "empty" }),
  "app.window.library.long": libraryScene({ variant: "long", smartViews: "long" }),
  "app.window.library.many": libraryScene({ variant: "many" }, scrollSidebarToEnd),
  // 选中态：叠加筛选、范围页签、回收站、未使用的标签、智能视图
  "app.window.library.facets": libraryScene({}, selectFacets),
  "app.window.library.scope-uncategorized": libraryScene({}, clickScope("未归合集")),
  "app.window.library.scope-favorites": libraryScene({}, clickScope("收藏")),
  "app.window.library.scope-archived": libraryScene({}, clickScope("归档")),
  "app.window.library.trash": libraryScene({}, clickRow("回收站")),
  "app.window.library.unused-tags": libraryScene({}, showUnusedTags),
  "app.window.library.smart-views": libraryScene({ smartViews: "basic" }, clickRow("工作库的收藏")),
  // 行的悬停与键盘聚焦
  "app.window.library.row-hover": libraryScene({}, hoverRow("学习笔记"), { keepPointer: true }),
  "app.window.library.row-focus": libraryScene({}, focusRow("学习笔记"), { keepFocus: true }),
  // 菜单：打开后第一项自动聚焦，那是用户看到的自然状态
  "app.window.library.menu-collection": libraryScene({}, openRowMenu("学习笔记"), { keepFocus: true }),
  "app.window.library.menu-tag": libraryScene({}, rightClickRow("产品设计"), { keepFocus: true }),
  "app.window.library.menu-tag-colors": libraryScene({}, openTagColors("产品设计"), { keepFocus: true }),
  // 对话框：名称框自动聚焦（带聚焦环），整个过程里焦点都在对话框内——Viva 的模态里没法「点空白处清焦点」，
  // 所以两侧一律保留焦点，画面才可比（点过图标之后焦点在图标按钮上，鼠标点出来的焦点没有聚焦环）
  "app.window.library.dlg-collection-new": libraryScene({}, openNewCollection, { keepFocus: true }),
  "app.window.library.dlg-collection-new-filled": libraryScene({}, fillNewCollection, { keepFocus: true }),
  "app.window.library.dlg-collection-rename": libraryScene({}, openRowMenuItem("旅行计划", "重命名"), { keepFocus: true }),
  "app.window.library.dlg-collection-rename-custom": libraryScene({}, openRowMenuItem("自定义图标", "重命名"), { keepFocus: true }),
  "app.window.library.dlg-tag-rename": libraryScene({}, openRowMenuItem("产品设计", "重命名"), { keepFocus: true }),
  "app.window.library.dlg-smart-view": libraryScene({}, openSmartViewDialog, { keepFocus: true }),
  "app.window.library.dlg-smart-view-filled": libraryScene({}, fillSmartView, { keepFocus: true }),
  // 提示：撤销条
  "app.window.library.toast-collection-deleted": libraryScene({}, deleteCollectionWithToast),
  "app.window.library.toast-tag-deleted": libraryScene({}, deleteTagWithToast),
};
