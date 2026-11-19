/**
 * 知识库列表视图（表格）的 React 侧对照场景（M2-2）。整窗对照，不隐去任何东西：表格视图没有常驻详情栏，
 * 工作区是工具条 + 表格 + 分页条（lib/react-library.mjs 的 HIDE_DETAIL_CSS 只针对卡片视图的详情栏，对这里没有影响）。
 *
 * 切到表格的办法就是点工具条上的「列表视图」；leaveLibrary 会把视图模式与表格列配置复位，不带进后面的场景。
 * Viva 一侧（viva/parity/scenes/library_table.go）做同样的操作，最终画面才可比。
 */
import { chain, listScene, pickPageSize, waitSpinner, applyFacets, clickButtonByName } from "./react-library-list-scenes.mjs";

/** 切到列表视图：点工具条上的视图切换钮，等表格（或它的空态）出现。 */
const viewTable = async (win) => {
  await win.getByRole("button", { name: "列表视图", exact: true }).click();
  await win.getByTestId("item-table").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};

/** 表格里第 n 行的第 col 个单元格的中心点。 */
async function cellCenter(win, row, col) {
  const box = await win.locator("[data-item-row]").nth(row).locator("td").nth(col).boundingBox();
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

/** 指针停在第 row 行的「类型」列（第 5 个单元格）：整行进入悬停态，指针不落在标题按钮或操作按钮上。 */
const hoverRow = (row) => async (win) => {
  const at = await cellCenter(win, row, 4);
  await win.mouse.move(at.x, at.y);
  await win.waitForTimeout(250);
};

/** 先在卡片视图里点一张卡片（它成为选中行），再切到表格：该行是「当前行」（bg-primary/10）。 */
const activateThenTable = async (win) => {
  const box = await win
    .getByTestId("item-list")
    .getByRole("button", { name: /^学习笔记：Go 并发模式/ })
    .first()
    .boundingBox();
  await win.mouse.click(box.x + 24, box.y + box.height / 2);
  await win.waitForTimeout(250);
  await viewTable(win);
};

/** 打开「列设置」菜单。 */
const openColumnMenu = async (win) => {
  await win.getByRole("button", { name: "列设置", exact: true }).click();
  await win.getByRole("menu", { name: "列设置" }).waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

/** 在列设置菜单里切换几列的显示（菜单保持打开），最后按 Esc 收起。 */
const toggleColumns = (...labels) => async (win) => {
  await openColumnMenu(win);
  for (const label of labels) {
    await win.getByRole("menuitemcheckbox", { name: label, exact: true }).click();
    await win.waitForTimeout(150);
  }
  await win.keyboard.press("Escape");
  await win.waitForTimeout(250);
};

/** 指针停在标题列右缘的拖动手柄上：手柄中间那条细线显出（group-hover:bg-primary/70）。 */
const hoverResizer = async (win) => {
  const box = await win.getByRole("separator", { name: "调整「标题」列宽" }).boundingBox();
  await win.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await win.waitForTimeout(250);
};

/**
 * 把表格滚到最右，纵向让第 3 行的上缘落在视口上缘下 10px：看吸附的操作列与吸附的表头。
 * 纵向的量按行位置算而不是写死：表头吸顶盖住视口上缘 36px，Viva 的实现是把行区域裁在表头下沿（套件没有 z-index，
 * 表头必须按 DOM 序先于行绘制），表头底下的行在渲染层里仍有节点与文字、在 Viva 里没有（台账 A-027）。
 * 对照只比「呈现出来」的内容：采集时把表头登记为遮挡元素（见 react-facts 的 __parityOccluders），盖住的上缘一段不计入可见部分，
 * 两边的节点与文字才是同一批。第 3 行上缘落在视口上缘下 10px，让这一行被表头压住一截、只露出下面一小段，覆盖这种裁剪；
 * 小、中、大三档字号的行高不同，所以用行的实际位置算（Viva 一侧同一公式：tableScroll）。
 */
const scrollTable = async (win) => {
  await win.getByTestId("item-table").evaluate((el) => {
    const top = el.querySelectorAll("[data-item-row]")[2].getBoundingClientRect().top - el.getBoundingClientRect().top + el.scrollTop;
    el.scrollTo(el.scrollWidth, Math.round(top - 10));
    window.__parityOccluders = [el.querySelector("thead")];
  });
  await win.waitForTimeout(250);
};

export const LIBRARY_TABLE_SCENES = {
  "app.window.library.table": listScene({}, viewTable),
  "app.window.library.table-hover": listScene({}, chain(viewTable, hoverRow(1)), { keepPointer: true }),
  "app.window.library.table-active": listScene({}, activateThenTable),
  "app.window.library.table-resizer": listScene({}, chain(viewTable, hoverResizer), { keepPointer: true }),
  "app.window.library.table-columns-open": listScene({}, chain(viewTable, openColumnMenu), { keepFocus: true }),
  "app.window.library.table-columns": listScene({}, chain(viewTable, toggleColumns("摘要", "合集", "状态", "创建时间"))),
  "app.window.library.table-scrolled": listScene({}, chain(viewTable, scrollTable)),
  "app.window.library.table-trash": listScene({}, chain(applyFacets("回收站"), viewTable)),
  "app.window.library.table-trash-empty": listScene({ variant: "empty" }, chain(applyFacets("回收站"), viewTable)),
  "app.window.library.table-empty": listScene({ variant: "empty" }, viewTable),
  "app.window.library.table-loading": listScene({ fault: "hold" }, chain(viewTable, waitSpinner)),
  "app.window.library.table-error": listScene({ fault: "fail" }, viewTable),
  "app.window.library.table-page2": listScene({}, chain(viewTable, pickPageSize("10"), clickButtonByName("下一页"))),
};
