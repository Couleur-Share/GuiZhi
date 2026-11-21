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
import { parityLanguage } from "../lib/react-state.mjs";


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

/**
 * 文章工具侧栏（M2-6b）：config.detail.tools 决定放哪些数据（lib/react-library.mjs）：base 只给网页的来源版本，comments 再给来源评论
 * （评论条目的来源链接换成小红书），comments-empty 评论读到是空的。Viva 一侧见 viva/parity/scenes/library_tools.go。
 * hideToolbar 连内容面板的工具栏一起隐去：侧栏在 DOM 里排在内容面板之前、Viva 里只能排在之后，工具栏文字的前后之别会让整段侧栏文字错位比不上
 * （台账 C-013）；detail-tools-note-toolbar 留着工具栏，专门钉住这个差别。
 */
const TOOLS = { detail: { tools: "base", hideToolbar: true } };
const TOOLS_WITH_TOOLBAR = { detail: { tools: "base" } };
const TOOLS_COMMENTS = { detail: { tools: "comments", hideToolbar: true } };
const TOOLS_COMMENTS_EMPTY = { detail: { tools: "comments-empty", hideToolbar: true } };

/** 侧栏里要点的钮与菜单项的可访问名，按 PARITY_LANG 取中文或英文（渲染层 i18n 目录 zh.json / en.json）；名字写错时 getByRole 等不到元素、采集超时，不会悄悄点错。 */
const TOOL_LABELS = {
  zh: {
    moreActions: "更多操作", tools: "文章信息与工具", addTag: "添加标签", handoffMore: "更多 AI 导出方式", versions: "原文版本",
    comments: "来源评论", collectComments: "采集评论",
  },
  en: {
    moreActions: "More actions", tools: "Article information and tools", addTag: "Add tag", handoffMore: "More AI export options",
    versions: "Source versions", comments: "Source comments", collectComments: "Collect comments",
  },
};
const toolLabel = (key) => TOOL_LABELS[parityLanguage()][key];

/** 工具侧栏（aside，role=region，名字「文章信息与工具」）；关着时 hidden，不在可访问树里。 */
const toolsPanel = (win) => win.getByRole("region", { name: toolLabel("tools"), exact: true });

/** 点盒子中心（坐标取整，理由同 snapped）。 */
const clickCenter = async (win, locator) => {
  const box = await locator.boundingBox();
  await win.mouse.click(snapped(box.x + box.width / 2), snapped(box.y + box.height / 2));
};

/**
 * 从头部「更多操作」菜单点开工具侧栏，等侧栏出现（打开时焦点落到收起钮上）。item 是点的菜单项：缺省是「文章信息与工具」；
 * 评论入口（「采集评论」「查看来源评论」）点开的同样是工具侧栏，并把评论卡片展开。Viva 一侧对应 toolsOpenVia。
 */
const openToolsVia = (item = "tools") => async (win) => {
  await openHeaderMenu(toolLabel("moreActions"))(win);
  await clickCenter(win, win.getByRole("menuitem", { name: toolLabel(item), exact: true }));
  await toolsPanel(win).waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};
const openTools = openToolsVia();

/** 点侧栏里的钮：按整名；prefix 为真时按名字前缀（展开钮的名字带数量、版本钮带提示）。 */
const clickInTools = (key, { prefix = false } = {}) => async (win) => {
  const name = toolLabel(key);
  await clickCenter(win, toolsPanel(win).getByRole("button", prefix ? { name: new RegExp(`^${name}`) } : { name, exact: true }));
  await win.waitForTimeout(300);
};

/** 展开原文版本下拉（渲染层 Select 的触发钮是带 aria-haspopup 的 button），选第一项（最新的版本）。 */
const pickFirstVersion = async (win) => {
  await clickCenter(win, toolsPanel(win).getByRole("button", { name: toolLabel("versions"), exact: true }));
  await win.getByRole("option").first().waitFor({ state: "visible" });
  await win.waitForTimeout(250);
  await clickCenter(win, win.getByRole("option").first());
  await win.waitForTimeout(300);
};

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
  // 文章工具侧栏（M2-6b）：从「更多操作」菜单打开；标签浮层、AI 交接菜单、网页的来源版本（展开、选一版比较）、来源评论卡片与带评论入口的更多菜单。
  "app.window.library.detail-tools-note": listScene(TOOLS, chain(selectCard(NOTE), openTools)),
  "app.window.library.detail-tools-note-toolbar": listScene(TOOLS_WITH_TOOLBAR, chain(selectCard(NOTE), openTools)),
  "app.window.library.detail-tools-note-picker": listScene(TOOLS, chain(selectCard(NOTE), openTools, clickInTools("addTag")), { keepFocus: true }),
  "app.window.library.detail-tools-note-handoff": listScene(TOOLS, chain(selectCard(NOTE), openTools, clickInTools("handoffMore")), { keepFocus: true }),
  "app.window.library.detail-tools-webpage": listScene(TOOLS, chain(selectCard(WEBPAGE), openTools)),
  "app.window.library.detail-tools-webpage-open": listScene(TOOLS, chain(selectCard(WEBPAGE), openTools, clickInTools("versions", { prefix: true }))),
  "app.window.library.detail-tools-webpage-selected": listScene(TOOLS, chain(
    selectCard(WEBPAGE), openTools, clickInTools("versions", { prefix: true }), pickFirstVersion,
  )),
  "app.window.library.detail-tools-comments": listScene(TOOLS_COMMENTS, chain(selectCard(NOTE), openTools)),
  "app.window.library.detail-tools-comments-open": listScene(TOOLS_COMMENTS, chain(selectCard(NOTE), openTools, clickInTools("comments", { prefix: true }))),
  // 评论读到是空的时卡片在展开之前不显示（渲染层 SourceCommentsCard），入口只有头部「更多操作」菜单里的「采集评论」。
  "app.window.library.detail-tools-comments-empty": listScene(TOOLS_COMMENTS_EMPTY, chain(selectCard(NOTE), openToolsVia("collectComments"))),
  "app.window.library.detail-tools-comments-menu": listScene(TOOLS_COMMENTS, chain(selectCard(NOTE), openHeaderMenu(LABEL.moreActions)), { keepFocus: true }),
};
