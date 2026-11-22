/**
 * 处理中心（M3-1）的 React 侧对照场景：二级侧栏的六档筛选与工作区的页头、批量整理条、Wiki 横幅、局部错误条、聚合大卡片与主体四态。
 * 整窗对照（rail + 顶栏 + 侧栏 + 工作区），不隐去任何区域。
 *
 * 数据层是页内替换（lib/react-inbox.mjs）：条目取夹具（viva/parity/testdata/inbox-fixture.json），模型是否已配置由配置决定。
 * Viva 一侧（viva/parity/scenes/inbox.go）做同样的操作，最终画面才可比。
 * 紧凑布局（视口 ≤ 1179.98px）下二级侧栏默认收起：点筛选前先点顶栏的「展开」让它以浮层打开，勾选前再按 Esc 收起（浮层盖住列表左侧）。
 */
import { INBOX_FIXTURE, enterInbox, inboxLabel, leaveInbox } from "../lib/react-inbox.mjs";

/** 场景：options 选夹具数据组与读取状态，steps 把页面带到要对照的状态。 */
function inboxScene(options, steps) {
  return {
    module: "inbox",
    async enter({ win, session }) {
      await enterInbox(session, options);
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveInbox(session);
    },
  };
}

/** 窗口是否处在紧凑布局（useCompactLayout：宽度 ≤ 1179.98px）。 */
const isCompact = (win) => win.evaluate(() => window.matchMedia("(max-width: 1179.98px)").matches);

/** 紧凑布局下先点顶栏的「展开」，让二级侧栏以浮层打开；宽屏什么也不做。 */
async function openCompactSidebar(win) {
  if (!(await isCompact(win))) return;
  await win.getByRole("button", { name: inboxLabel("expand"), exact: true }).click();
  await win.waitForTimeout(400); // 浮层的入场过渡（duration-smooth 280ms）
}

/** 紧凑布局下按 Esc 收起浮层（浮层盖住工作区左侧，不收起就点不到列表里的勾选框）；宽屏什么也不做。 */
async function closeCompactSidebar(win) {
  if (!(await isCompact(win))) return;
  await win.keyboard.press("Escape");
  await win.waitForTimeout(400);
}

const UNCLASSIFIED = INBOX_FIXTURE.variants.data.list.items.filter((item) => item.kind === "unclassified").map((item) => item.title);
const UNCLASSIFIED_COUNT = INBOX_FIXTURE.variants.data.list.counts.unclassified;

/**
 * 点侧栏里的「未归合集」（可访问名是「名称 计数」），等列表按新分组重画；紧凑布局下点完再收起浮层。
 * 收起是为了可访问树的顺序：渲染层的浮层与遮罩在 DOM 里排在工作区之前，Viva 的可访问树按绘制顺序登记、浮层画在工作区之后，
 * 浮层开着时两边的节点顺序不同（画面一致）；所以处理中心的对照都不在浮层开着的状态下取样。
 */
async function clickUnclassified(win) {
  await openCompactSidebar(win);
  await win.getByRole("button", { name: `${inboxLabel("filterUnclassified")} ${UNCLASSIFIED_COUNT}`, exact: true }).click();
  await win.getByText(UNCLASSIFIED[0], { exact: true }).waitFor({ state: "visible" });
  await win.waitForTimeout(250);
  await closeCompactSidebar(win);
}

/**
 * 点一条目卡片的勾选框：点它看得见的那个 16×16 的方框中心（真正的 input 是 sr-only 的一个像素，点它的中心会被截住）。
 * 第一次勾选后批量条出现、列表下移，locator 每次重新定位。Viva 一侧点的是可访问树里复选框节点的外框中心。
 */
const checkItem = (title) => async (win) => {
  const label = win.locator("label").filter({ has: win.getByRole("checkbox", { name: inboxLabel("selectItem")(title), exact: true }) });
  const box = await label.locator("span[aria-hidden='true']").first().boundingBox();
  await win.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await win.waitForTimeout(250);
};

export const INBOX_SCENES = {
  "app.window.inbox.all": inboxScene({}),
  "app.window.inbox.selected": inboxScene({}, async (win) => {
    await clickUnclassified(win);
    // 列表里的第 1 条与第 3 条：半选的「全选当前列表」，两条都是未归合集所以 AI 智能归类钮出现。
    await checkItem(UNCLASSIFIED[0])(win);
    await checkItem(UNCLASSIFIED[2])(win);
  }),
  "app.window.inbox.unclassified": inboxScene({}, clickUnclassified),
  "app.window.inbox.wiki-needs-model": inboxScene({ wikiNeedsModel: true, semanticError: true }),
  "app.window.inbox.empty": inboxScene({ variant: "empty" }),
  "app.window.inbox.error": inboxScene({ loadError: true }),
};
