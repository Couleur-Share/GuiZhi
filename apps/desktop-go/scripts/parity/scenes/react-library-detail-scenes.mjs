/**
 * 知识库条目详情（M2-4）的 React 侧对照场景：详情头部（标题、chip、保存状态、动作区）、通知条（冲突、来源版本、复核）、
 * 内容面板的标签栏与查找条、专注阅读、表格视图的详情浮层。整窗对照，只隐去阅读区（见 lib/react-library.mjs 的 HIDE_READER_CSS，
 * Viva 一侧的阅读区归 M2-5 与 M2-6、此前留白，台账 B-059）。
 *
 * 数据层与列表场景同一套页内替换（lib/react-library.mjs）：条目取夹具 data 变体，详情字段取详情夹具
 * （viva/parity/testdata/library-detail-fixture.json），config.detail 决定读取与保存是成功、卡住、失败还是冲突。
 * Viva 一侧（viva/parity/scenes/library_detail.go）做同样的操作，最终画面才可比。
 */
import { CARD_ANCHOR, applyFacets, card, chain, listScene, pressKeys } from "./react-library-list-scenes.mjs";
import { LIBRARY_DETAIL_FIXTURE, navLabel } from "../lib/react-library.mjs";


/** 场景要点的卡片标题（取自列表夹具 data 变体）。都是标题开头不重复的纯文字，Viva 一侧同名。 */
const NOTE = "学习笔记：Go 并发模式"; // item-001：笔记，置顶，合集「学习笔记」，标签 AI
const WEBPAGE = "如何构建本地优先的个人知识库"; // item-003：网页，有来源
const VIDEO = "Transformer 架构详解"; // item-004：视频，有文字稿
const LONG_TITLE = "一条标题很长很长"; // item-005：长标题
const UNTITLED = "无标题"; // item-006：标题为空的片段
// 第 7 张及之后的卡片在 800×600、大字号档的首屏之外（卡片列表窗口化，屏外的卡片有元素但点不到），点击选不中：用方向键选。
const IMAGE_INDEX = 8; // item-009「小红书：极简桌面布置灵感」：图片，有图中文字，列表里第 9 张
const FORUM_INDEX = 11; // item-012「技术论坛：SQLite WAL 模式的备份坑」：论坛，有总结与逐楼回复，列表里第 12 张
const BLANK_INDEX = 13; // item-014「读书清单」：正文为空的笔记，列表里第 14 张（首屏之外，用方向键选）
const REVIEW_INDEX = 16; // item-017「网页收藏：CSS 容器查询指南」：待复核，列表里第 17 张（首屏之外，用方向键选）
const TRASHED = "回收站：误删的草稿"; // item-042：回收站里的笔记

/** 交互步骤要点的按钮名（渲染层 i18n 的中文界面）；英文变体只用不需要交互的静止场景（台账 G-017）。 */
const LABEL = {
  trash: "回收站", collection: "学习笔记", moreActions: "更多操作", transcript: "文字稿", replies: "讨论", recognized: "图中文字",
  toc: "目录", editOriginal: "编辑原文",
};
/** 点一张卡片左内边距处，等详情头部出现与读取落定。 */
const selectCard = (title) => async (win) => {
  const box = await card(win, title).boundingBox();
  await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
  await win.getByTestId("article-header").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};

/**
 * 用方向键选到第 n 张卡片（窗口级键盘，台账 B-045）：卡片不在首屏时用（首屏放得下 12 张），列表按 scrollToIndex 的 auto 对齐滚进视野。
 * Viva 一侧对应 listKeys（viva/parity/scenes/library_list.go）。
 */
const selectByKeys = (n) => async (win) => {
  await pressKeys("ArrowDown", n)(win);
  await win.getByTestId("article-header").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};

/** 点一张卡片，但读取卡住或失败：不等头部，只等点击落定。 */
const clickCardOnly = (title) => async (win) => {
  const box = await card(win, title).boundingBox();
  await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
  await win.waitForTimeout(400);
};

/** 详情本体（头部、通知条、内容面板都在里面）。 */
const detailRoot = (win) => win.getByTestId("article-detail");

/** 取详情头部里的按钮（避开侧栏与列表里同名的行）。 */
const headerButton = (win, name) => win.getByTestId("article-header").getByRole("button", { name, exact: true });

/**
 * 点内容面板标签栏里的标签：exact 为真按整名（「讨论」不能误中「讨论总结」），否则按名字前缀（文字稿标签的名字带字数）。
 * Viva 一侧对应 detailClickTab（viva/parity/scenes/library_detail.go）。
 */
const clickTab = (name, exact = false) => async (win) => {
  const toolbar = win.getByTestId("article-toolbar");
  await (exact ? toolbar.getByRole("button", { name, exact: true }) : toolbar.getByRole("button", { name: new RegExp(`^${name}`) })).click();
  await win.waitForTimeout(250);
};

/**
 * 点盒子中心，坐标向下取整（理由同批量场景：会弹出锚在指针处浮层的点击，两边都点同一个整数点）。
 * Viva 一侧对应 snap（viva/parity/scenes/library_bulk.go）。
 */
const snapped = (v) => Math.floor(v);

/** 点头部里会弹出菜单的按钮，等菜单出现。 */
const openHeaderMenu = (name) => async (win) => {
  const box = await headerButton(win, name).boundingBox();
  await win.mouse.click(snapped(box.x + box.width / 2), snapped(box.y + box.height / 2));
  await win.getByRole("menu").waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

/**
 * 把标题框的内容换成 text（渲染层经 knowledge.store 防抖 800 ms 自动保存），再等防抖与保存落定。
 * fill 会聚焦标题框、整体替换再触发 input 事件；等 1100 ms：防抖 800 ms，保存的往返之后「已保存」亮 2.5 s。
 */
const retitle = (text, settleMs = 1100) => async (win) => {
  await win.getByTestId("item-title-input").fill(text);
  await win.waitForTimeout(settleMs);
};

/** 点开一个 `<details>` 的概要，等展开。 */
const openSummary = (scope, nth = 0) => async (win) => {
  await scope(win).locator("summary").nth(nth).click();
  await win.waitForTimeout(250);
};

/** 切到列表视图（表格），再点第 row 行的「类型」列（不压在标题按钮或操作按钮上）打开详情浮层。 */
const openTableModal = (row) => async (win) => {
  await win.getByRole("button", { name: navLabel("viewList"), exact: true }).click();
  await win.getByTestId("item-table").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
  const tr = win.locator("[data-item-row]").nth(row);
  const cell = await tr.locator("td").nth(4).boundingBox();
  const box = await tr.locator("label span[aria-hidden='true']").first().boundingBox();
  await win.mouse.click(snapped(cell.x + cell.width / 2), snapped(box.y + box.height / 2));
  await win.getByRole("dialog").waitFor({ state: "visible" });
  await win.waitForTimeout(500); // 浮层的入场过渡
};

/**
 * 读者工具条（台账 B-065）：config.detail.toolbar 让采集只隐去阅读区内容、保留工具栏最前面的读者工具条
 * （lib/react-library.mjs 的 HIDE_READER_CONTENT_CSS）。Viva 一侧对应 Screen.ReaderToolbar（viva/parity/scenes/library_detail.go 的 readerToolbar 选项）。
 */
const TOOLBAR = { detail: { toolbar: true } };

/** 读者工具条里的按钮（工具栏最前面的挂载点）。 */
const toolbarButton = (win, name) => win.getByTestId("article-toolbar").getByRole("button", { name, exact: true });

/** 点读者工具条里的按钮，等状态落定。 */
const clickToolbar = (name) => async (win) => {
  await toolbarButton(win, name).click();
  await win.waitForTimeout(250);
};

/** 把指针停在读者工具条里按钮的中心（悬停态：场景用 keepPointer 保留；坐标取整，理由同 snapped）。 */
const hoverToolbar = (name) => async (win) => {
  const box = await toolbarButton(win, name).boundingBox();
  await win.mouse.move(snapped(box.x + box.width / 2), snapped(box.y + box.height / 2));
  await win.waitForTimeout(150);
};

/** 键盘聚焦读者工具条里的按钮：先按一次 Tab 让页面进入键盘模态，再程序聚焦（此时 :focus-visible 成立；做法同行聚焦 focusRow）。 */
const focusToolbar = (name) => async (win) => {
  await win.keyboard.press("Tab");
  await toolbarButton(win, name).focus();
  await win.keyboard.press("Tab");
  await toolbarButton(win, name).focus();
  await win.waitForTimeout(150);
};

const CONFLICT_TITLE = LIBRARY_DETAIL_FIXTURE.conflict.mineTitle;

export const LIBRARY_DETAIL_SCENES = {
  "app.window.library.detail-empty": listScene({ detail: {} }),
  "app.window.library.detail-loading": listScene({ detail: { get: "hold" } }, clickCardOnly(NOTE)),
  "app.window.library.detail-error": listScene({ detail: { get: "fail" } }, clickCardOnly(NOTE)),
  "app.window.library.detail-note": listScene({ detail: {} }, selectCard(NOTE)),
  "app.window.library.detail-note-blank": listScene({ detail: {} }, selectByKeys(BLANK_INDEX + 1)),
  "app.window.library.detail-untitled": listScene({ detail: {} }, selectCard(UNTITLED)),
  "app.window.library.detail-long-title": listScene({ detail: {} }, selectCard(LONG_TITLE)),
  "app.window.library.detail-webpage": listScene({ detail: {} }, selectCard(WEBPAGE)),
  "app.window.library.detail-video": listScene({ detail: {} }, selectCard(VIDEO)),
  "app.window.library.detail-video-transcript": listScene({ detail: {} }, chain(selectCard(VIDEO), clickTab(LABEL.transcript))),
  "app.window.library.detail-forum": listScene({ detail: {} }, selectByKeys(FORUM_INDEX + 1)),
  "app.window.library.detail-forum-replies": listScene({ detail: {} }, chain(selectByKeys(FORUM_INDEX + 1), clickTab(LABEL.replies, true))),
  "app.window.library.detail-image": listScene({ detail: {} }, selectByKeys(IMAGE_INDEX + 1)),
  "app.window.library.detail-image-text": listScene({ detail: {} }, chain(selectByKeys(IMAGE_INDEX + 1), clickTab(LABEL.recognized, true))),
  "app.window.library.detail-trashed": listScene({ detail: {} }, chain(applyFacets(LABEL.trash), selectCard(TRASHED))),
  "app.window.library.detail-review": listScene({ detail: {} }, selectByKeys(REVIEW_INDEX + 1)),
  "app.window.library.detail-review-open": listScene({ detail: {} }, chain(selectByKeys(REVIEW_INDEX + 1), openSummary((win) => win.getByTestId("review-required-notice")))),
  "app.window.library.detail-revisions": listScene({ detail: { versions: true } }, selectCard(WEBPAGE)),
  "app.window.library.detail-revisions-open": listScene({ detail: { versions: true } }, chain(
    selectCard(WEBPAGE),
    openSummary((win) => detailRoot(win).locator("details").first()),
    openSummary((win) => detailRoot(win).locator("details").first().locator("details").first()),
  )),
  "app.window.library.detail-conflict": listScene({ detail: { save: "conflict" } }, chain(selectCard(NOTE), retitle(CONFLICT_TITLE))),
  "app.window.library.detail-conflict-open": listScene({ detail: { save: "conflict" } }, chain(
    selectCard(NOTE), retitle(CONFLICT_TITLE), openSummary((win) => detailRoot(win).getByRole("alert").first()),
  )),
  "app.window.library.detail-find": listScene({ detail: {} }, chain(selectCard(NOTE), async (win) => {
    await win.keyboard.press("Control+f");
    await win.waitForTimeout(300);
  }), { keepFocus: true }),
  "app.window.library.detail-menu-collection": listScene({ detail: {} }, chain(selectCard(NOTE), openHeaderMenu(LABEL.collection)), { keepFocus: true }),
  "app.window.library.detail-menu-more": listScene({ detail: {} }, chain(selectCard(NOTE), openHeaderMenu(LABEL.moreActions)), { keepFocus: true }),
  "app.window.library.detail-focus": listScene({ detail: {} }, chain(selectCard(NOTE), async (win) => {
    await win.keyboard.press("Alt+z");
    await win.waitForTimeout(500);
  })),
  "app.window.library.detail-saving": listScene({ detail: { save: "hold" } }, chain(selectCard(NOTE), retitle(CONFLICT_TITLE))),
  "app.window.library.detail-saved": listScene({ detail: {} }, chain(selectCard(NOTE), retitle(CONFLICT_TITLE))),
  "app.window.library.detail-save-failed": listScene({ detail: { save: "fail" } }, chain(selectCard(NOTE), retitle(CONFLICT_TITLE))),
  "app.window.library.table-detail-modal": listScene({ detail: {} }, openTableModal(0)),
  // 读者工具条（B-065）：原文 / AI 阅读、目录、编辑；论坛的默认标签是讨论总结（编辑钮的可访问名换成「编辑讨论总结」）；回收站里没有编辑；
  // 目录展开（bg-muted）、编辑态（正在编辑原文 + 完成编辑）、空白笔记自动进编辑、悬停与键盘聚焦。
  "app.window.library.detail-toolbar-note": listScene(TOOLBAR, selectCard(NOTE)),
  "app.window.library.detail-toolbar-forum": listScene(TOOLBAR, selectByKeys(FORUM_INDEX + 1)),
  "app.window.library.detail-toolbar-trashed": listScene(TOOLBAR, chain(applyFacets(LABEL.trash), selectCard(TRASHED))),
  "app.window.library.detail-toolbar-toc": listScene(TOOLBAR, chain(selectCard(NOTE), clickToolbar(LABEL.toc))),
  "app.window.library.detail-toolbar-editing": listScene(TOOLBAR, chain(selectCard(NOTE), clickToolbar(LABEL.editOriginal))),
  "app.window.library.detail-toolbar-blank": listScene(TOOLBAR, selectByKeys(BLANK_INDEX + 1)),
  "app.window.library.detail-toolbar-hover": listScene(TOOLBAR, chain(selectCard(NOTE), hoverToolbar(LABEL.editOriginal)), { keepPointer: true }),
  "app.window.library.detail-toolbar-focus": listScene(TOOLBAR, chain(selectCard(NOTE), focusToolbar(LABEL.toc)), { keepFocus: true }),
};
