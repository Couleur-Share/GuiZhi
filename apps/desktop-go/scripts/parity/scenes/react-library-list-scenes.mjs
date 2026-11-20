/**
 * 知识库列表栏（M2-2）的 React 侧对照场景。整窗对照，只隐去右侧详情栏（见 lib/react-library.mjs 的 HIDE_DETAIL_CSS）：
 * 比较的是 rail + 顶栏 + 侧栏 + 工作区里的列表栏（工具条、卡片、分页）。
 *
 * 条目由页内替换的 knowledge.list 供给（夹具 data 变体的 entries，规则与 Go 一侧的假实现一致）。
 * Viva 一侧（viva/parity/scenes/library_list.go）做同样的操作，最终画面才可比。
 */
import { closeCompactSidebar, enterLibrary, leaveLibrary, openCompactSidebar } from "../lib/react-library.mjs";

/** 故障注入的失败原因：与 Go 一侧 listFailMessage 同文。 */
const FAIL_MESSAGE = "磁盘读取失败：知识库文件暂时无法访问";

/** 场景：options 选夹具数据与列表故障，steps 把页面带到要对照的状态；flags 是 keepPointer / keepFocus。 */
export function listScene(options, steps, flags = {}) {
  const { fault = "", ...rest } = options;
  return {
    module: "library",
    ...flags,
    async enter({ win, session }) {
      await enterLibrary(session, { ...rest, list: { fault, failMessage: FAIL_MESSAGE } });
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveLibrary(session);
    },
  };
}

const escapeRegExp = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** 卡片：列表栏里可访问名以标题开头的按钮。 */
export const card = (win, title) =>
  win.getByTestId("item-list").getByRole("button", { name: new RegExp(`^${escapeRegExp(title)}`) }).first();

/** 卡片左内边距里的点位：不压在置顶图标、星标与类型图标上（与 Viva 一侧 listCardAnchor 一致）。 */
export const CARD_ANCHOR = 24;

/** 点一张卡片，等选中与详情读取落定。 */
function clickCard(title) {
  return async (win) => {
    const box = await card(win, title).boundingBox();
    await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
    await win.waitForTimeout(250);
  };
}

/** 把指针停在卡片左内边距处：卡片进入悬停态。 */
function hoverCard(title) {
  return async (win) => {
    const box = await card(win, title).boundingBox();
    await win.mouse.move(box.x + CARD_ANCHOR, box.y + box.height / 2);
  };
}

/** 键盘聚焦：先按一次 Tab 让页面进入键盘模态，再程序聚焦卡片（此时 :focus-visible 成立）。 */
function focusCard(title) {
  return async (win) => {
    await win.keyboard.press("Tab");
    await card(win, title).focus();
  };
}

/**
 * 顶栏里侧栏开关与搜索框之间的空白处：点它让搜索框失焦，与 Viva 一侧的 listBlurX、listBlurY 一致。
 * 不能用 imports 场景的 rail 中段（24, 400）：知识库窗口的 rail 上有五个模块按钮，400 正好落在「采集」上，点下去会换页。
 */
const BLUR_X = 200;
const BLUR_Y = 20;

/** 顶栏搜索：点搜索框、打字，等 300 ms 防抖与随后的读取落定，再点空白处失焦。 */
export function searchList(text) {
  return async (win) => {
    await win.getByTestId("topbar-search").click();
    await win.keyboard.type(text);
    await win.waitForTimeout(800);
    await win.mouse.click(BLUR_X, BLUR_Y);
    // 顶栏的空白处是窗口拖动区：点它不一定让输入框失焦（悬停类场景不收尾 park 时，实测搜索框仍带聚焦环，Viva 一侧已失焦），
    // 所以显式失焦；其余场景收尾的 park 本来就会失焦，终态不变。
    await win.evaluate(() => {
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    });
    await win.waitForTimeout(250);
  };
}

export const clickButtonByName = (name) => async (win) => {
  await win.getByRole("button", { name, exact: true }).click();
  await win.waitForTimeout(250);
};

/**
 * 工具条上的排序按钮：可访问名是「排序: 当前排序」。Playwright 的角色是 button（aria-haspopup 不改隐式角色），
 * Chromium 自己的可访问树把它报成 combobox——比对时 Viva 一侧用 combobox 角色，两边在 L1 里对得上。
 */
const openSort = async (win) => {
  await win.getByRole("button", { name: /^排序/ }).click();
  await win.getByRole("menu").waitFor({ state: "visible" });
};

const pickSort = (label) => async (win) => {
  await openSort(win);
  await win.getByRole("menuitem", { name: label, exact: true }).click();
  await win.waitForTimeout(250);
};

/** 分页条的「每页」下拉：打开，选一项。 */
export const pickPageSize = (option) => async (win) => {
  await win.getByRole("button", { name: /^每页/ }).click();
  await win.getByRole("option", { name: option, exact: true }).click();
  await win.waitForTimeout(250);
};

/**
 * 在侧栏里依次点几行（可访问名就是行的名称，计数不在名字里），把筛选叠上去。
 * 紧凑布局下侧栏是浮层：先展开，叠完按 Esc 收起（与 Viva 一侧 listApplyFacets 一致）。
 */
export const applyFacets = (...rows) => async (win) => {
  await openCompactSidebar(win);
  for (const row of rows) {
    await win.getByRole("button", { name: row, exact: true }).click();
    await win.waitForTimeout(250);
  }
  await closeCompactSidebar(win);
};

/** 工具条上的「组合筛选 · N」：点开弹层。 */
const openFilterSummary = async (win) => {
  await win.getByRole("button", { name: /^组合筛选/ }).click();
  await win.waitForTimeout(250);
};

/** 叠加的分面：合集「学习笔记」、标签「AI」、平台「网页」，条目里恰有一条同时满足（与 Go 一侧 facetsTwo、facetsThree 一致）。 */
const FACETS_TWO = ["学习笔记", "AI"];
const FACETS_THREE = ["学习笔记", "AI", "网页"];

export const chain = (...steps) => async (win, session) => {
  for (const step of steps) await step(win, session);
};

/**
 * 按键 times 次（与 Go 一侧 listKeys 一致）：窗口级键盘（use-item-keyboard.ts）的整窗对照。
 * 每次按完等 120 ms，最后再等 300 ms：虚拟列表的 scrollToIndex 按帧校正测量值，不是一步到位。
 */
export const pressKeys = (key, times = 1) => async (win) => {
  for (let i = 0; i < times; i++) {
    await win.keyboard.press(key);
    await win.waitForTimeout(120);
  }
  await win.waitForTimeout(300);
};

/** 读取中的转圈有 250 ms 的延迟与淡入：等 600 ms 再取图。 */
export const waitSpinner = (win) => win.waitForTimeout(600);

export const LIBRARY_LIST_SCENES = {
  "app.window.library.list": listScene({}),
  "app.window.library.list-selected": listScene({}, clickCard("学习笔记：Go 并发模式")),
  "app.window.library.list-hover": listScene({}, hoverCard("如何构建本地优先的个人知识库"), { keepPointer: true }),
  "app.window.library.list-focus": listScene({}, focusCard("如何构建本地优先的个人知识库"), { keepFocus: true }),
  "app.window.library.list-empty": listScene({ variant: "empty" }),
  "app.window.library.list-loading": listScene({ fault: "hold" }, waitSpinner),
  "app.window.library.list-error": listScene({ fault: "fail" }),
  "app.window.library.list-search-empty": listScene({}, searchList("没有任何条目含有这个词")),
  // 搜索命中词高亮（B-041）：标题与摘要里的命中词包进 mark（琥珀底、深琥珀字、两侧 2px 内边距、圆角 rounded-sm）。
  "app.window.library.list-search-hit": listScene({}, searchList("设计")),
  "app.window.library.list-search-hit-latin": listScene({}, searchList("design")),
  "app.window.library.list-search-hit-selected": listScene({}, chain(searchList("设计"), clickCard("产品设计原则：极简与克制"))),
  "app.window.library.list-sort-menu": listScene({}, openSort, { keepFocus: true }),
  "app.window.library.list-sort-title": listScene({}, pickSort("标题 A→Z")),
  "app.window.library.list-trash": listScene({}, applyFacets("回收站")),
  "app.window.library.list-filters": listScene({}, applyFacets(...FACETS_TWO)),
  "app.window.library.list-filters-open": listScene({}, chain(applyFacets(...FACETS_THREE), openFilterSummary), { keepFocus: true }),
  "app.window.library.list-page2": listScene({}, chain(pickPageSize("10"), clickButtonByName("下一页"))),
  "app.window.library.list-keys-down": listScene({}, pressKeys("ArrowDown", 12)),
  "app.window.library.list-keys-end": listScene({}, pressKeys("End")),
  "app.window.library.list-keys-up": listScene({}, chain(pressKeys("End"), pressKeys("ArrowUp", 14))),
};
