/**
 * 知识库批量与整理（M2-3）的 React 侧对照场景：批量条、结果条、条目菜单、移到合集菜单、标签弹层、彻底删除与清空回收站的确认框。
 * 整窗对照，只隐去右侧详情栏（卡片视图）；表格视图没有常驻详情栏，什么也不隐。
 *
 * 数据层与列表场景同一套页内替换（lib/react-library.mjs）：条目取夹具 data 变体，批量命令 execute 就地改条目，
 * config.batch.fail 里登记的条目在批量里失败。Viva 一侧（viva/parity/scenes/library_bulk.go）做同样的操作，最终画面才可比。
 */
import { CARD_ANCHOR, applyFacets, card, chain, clickButtonByName, listScene } from "./react-library-list-scenes.mjs";
import { navLabel } from "../lib/react-library.mjs";
import { parityLanguage } from "../lib/react-state.mjs";

/**
 * 场景要点的按钮、菜单项与侧栏行的可访问名，按 PARITY_LANG 取中文或英文（取自渲染层 i18n 目录 zh.json / en.json 的 library.* 键）。
 * 名字写错时 getByRole 等不到元素、整个采集超时，不会悄悄点错；Viva 一侧对应 viva/parity/scenes/library_bulk.go 的 clickLabel 一族。
 */
const LABELS = {
  zh: {
    trash: "回收站", bulkFavorite: "批量收藏", bulkMove: "移动到合集", bulkTag: "批量打标签", deleteForever: "彻底删除",
    emptyTrash: "清空回收站", retryFailed: "仅重试失败项", moreActions: "更多操作", editTags: "编辑标签",
  },
  en: {
    trash: "Trash", bulkFavorite: "Favorite selection", bulkMove: "Move to collection", bulkTag: "Add tag to selection", deleteForever: "Delete forever",
    emptyTrash: "Empty trash", retryFailed: "Retry failed only", moreActions: "More actions", editTags: "Edit tags",
  },
};
const label = (key) => LABELS[parityLanguage()][key];

/** 多选用的两张卡片：置顶的第二张与普通的第一张（都是纯中文标题，避开拉丁字母的字偶距放过项）。 */
const PICK_A = "产品设计原则：极简与克制";
const PICK_B = "如何构建本地优先的个人知识库";
/** 回收站里的两张卡片。 */
const TRASH_A = "回收站：误删的草稿";
const TRASH_B = "回收站：过期的链接";

/** 批量里失败的那一条（PICK_B）与失败原因：与 Go 一侧 batchFailID、batchFailReason 同值。 */
const FAIL_ID = "item-003";
const FAIL_REASON = "磁盘写入失败：数据库暂时被占用";
const FAILING = { batch: { fail: { [FAIL_ID]: FAIL_REASON } } };
/** 第一次批量照常执行，之后的批量永不返回：重试失败项时卡在「处理中」。 */
const FAILING_HOLD = { batch: { fail: { [FAIL_ID]: FAIL_REASON }, holdAfter: 1 } };

/** 按住 Ctrl 点一张卡片左内边距处：加入或移出多选（渲染层 handleRowClick 的 ctrl / meta 分支）。 */
const ctrlClickCard = (title) => async (win) => {
  const box = await card(win, title).boundingBox();
  await win.keyboard.down("Control");
  await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
  await win.keyboard.up("Control");
  await win.waitForTimeout(250);
};

const selectCards = (...titles) => chain(...titles.map(ctrlClickCard));

/**
 * 点盒子中心，坐标向下取整：页面里事件坐标（clientX、clientY）本来就是整数（点 65.5 读到 65），真实鼠标在 100% 缩放的屏幕上也只落在整数像素；
 * 会弹出锚在指针处浮层的点击，两边都点同一个整数点，浮层的锚点才一致。Viva 一侧对应 snap（viva/parity/scenes/library_bulk.go）。
 */
const snapped = (v) => Math.floor(v);

/** 右键点一张卡片左内边距处，等菜单出现。 */
const rightClickCard = (title) => async (win) => {
  const box = await card(win, title).boundingBox();
  await win.mouse.click(snapped(box.x + CARD_ANCHOR), snapped(box.y + box.height / 2), { button: "right" });
  await win.getByRole("menu").waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

/** 点批量条上的一枚动作（卡片视图是只有图标的钮，名字取自 title）。 */
const clickBulk = (name) => clickButtonByName(name);

/** 点开一个会弹出菜单或浮层的批量钮，等浮层出现。 */
const openBulk = (name, role) => async (win) => {
  const box = await win.getByRole("button", { name, exact: true }).boundingBox();
  await win.mouse.click(snapped(box.x + box.width / 2), snapped(box.y + box.height / 2));
  await win.getByRole(role).waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

/** 点开结果条里的「逐条结果」折叠区。 */
const openDetails = async (win) => {
  await win.locator("summary").click();
  await win.waitForTimeout(250);
};

/** 表格里第 row 行（从 0 起）。 */
const tableRow = (win, row) => win.locator("[data-item-row]").nth(row);

/**
 * 勾选表格里第 row 行的复选框：点它看得见的那个 16×16 的方框中心。
 * 不用 getByRole("checkbox").click()：真正的 input 是 sr-only 的（一个像素），Playwright 点它的中心会被盖在上面的方框与粘性表头截住；
 * Viva 一侧点的也是方框中心（可访问树里复选框节点的外框）。
 */
const checkRow = (row) => async (win) => {
  const box = await tableRow(win, row).locator("label span[aria-hidden='true']").first().boundingBox();
  await win.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await win.waitForTimeout(250);
};

/**
 * 表格里第 row 行的「类型」列（第 5 个单元格）的横向中心、这一行复选框方框的纵向中心：指针落在这里不压在标题按钮或操作按钮上。
 * 纵向不取单元格中心：行顶在 .5 处时单元格中心恰在整数（219.5 + 22.5 = 242），方框中心在 .5（241.5），取整后差 1 像素，
 * 锚在指针处的浮层随之差 1 像素；Viva 一侧取的就是复选框外框的中心（viva/parity/scenes/library_table.go 的 tableRowPoint），两边用同一个几何来源。
 */
async function rowPoint(win, row) {
  const cell = await tableRow(win, row).locator("td").nth(4).boundingBox();
  const box = await tableRow(win, row).locator("label span[aria-hidden='true']").first().boundingBox();
  return { x: snapped(cell.x + cell.width / 2), y: snapped(box.y + box.height / 2) };
}

/** 切到列表视图（表格）：点工具条上的视图切换钮，等表格出现。 */
const viewTable = async (win) => {
  await win.getByRole("button", { name: navLabel("viewList"), exact: true }).click();
  await win.getByTestId("item-table").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};

/** 点表格行内的「更多操作」，等菜单出现。 */
const openRowMore = (row) => async (win) => {
  const box = await tableRow(win, row).getByRole("button", { name: label("moreActions"), exact: true }).boundingBox();
  await win.mouse.click(snapped(box.x + box.width / 2), snapped(box.y + box.height / 2));
  await win.getByRole("menu").waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

/** 右键点表格行，选菜单里的「编辑标签」，等标签浮层出现。 */
const openTagPicker = (row) => async (win) => {
  const at = await rowPoint(win, row);
  await win.mouse.click(at.x, at.y, { button: "right" });
  await win.getByRole("menu").waitFor({ state: "visible" });
  await win.waitForTimeout(250);
  await win.getByRole("menuitem", { name: label("editTags"), exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

const selectTrashCards = chain(applyFacets(label("trash")), selectCards(TRASH_A, TRASH_B));

export const LIBRARY_BULK_SCENES = {
  "app.window.library.list-bulk": listScene({}, selectCards(PICK_A, PICK_B)),
  "app.window.library.list-bulk-move": listScene({}, chain(selectCards(PICK_A, PICK_B), openBulk(label("bulkMove"), "menu")), { keepFocus: true }),
  "app.window.library.list-bulk-tag": listScene({}, chain(selectCards(PICK_A, PICK_B), openBulk(label("bulkTag"), "dialog")), { keepFocus: true }),
  "app.window.library.list-bulk-trash": listScene({}, selectTrashCards),
  "app.window.library.list-confirm-delete": listScene({}, chain(selectTrashCards, clickBulk(label("deleteForever"))), { keepFocus: true }),
  "app.window.library.list-confirm-empty-trash": listScene({}, chain(applyFacets(label("trash")), clickButtonByName(label("emptyTrash"))), { keepFocus: true }),
  "app.window.library.list-menu-entry": listScene({}, rightClickCard(PICK_B), { keepFocus: true }),
  "app.window.library.list-menu-bulk": listScene({}, chain(selectCards(PICK_A, PICK_B), rightClickCard(PICK_B)), { keepFocus: true }),
  "app.window.library.list-batch-done": listScene({}, chain(selectCards(PICK_A, PICK_B), clickBulk(label("bulkFavorite")))),
  "app.window.library.list-batch-failed": listScene(FAILING, chain(selectCards(PICK_A, PICK_B), clickBulk(label("bulkFavorite")))),
  "app.window.library.list-batch-failed-open": listScene(FAILING, chain(selectCards(PICK_A, PICK_B), clickBulk(label("bulkFavorite")), openDetails)),
  "app.window.library.list-batch-busy": listScene(FAILING_HOLD, chain(selectCards(PICK_A, PICK_B), clickBulk(label("bulkFavorite")), clickButtonByName(label("retryFailed")))),
  "app.window.library.table-bulk": listScene({}, chain(viewTable, checkRow(0), checkRow(2))),
  "app.window.library.table-menu-more": listScene({}, chain(viewTable, openRowMore(1)), { keepFocus: true }),
  "app.window.library.table-tag-picker": listScene({}, chain(viewTable, openTagPicker(1)), { keepFocus: true }),
};
