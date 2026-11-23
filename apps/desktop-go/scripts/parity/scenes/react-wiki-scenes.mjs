/**
 * Wiki（M3-5a）的 React 侧对照场景：空态、页面详情、编辑态、更新建议、编译对话框。整窗对照（rail + 顶栏 + 侧栏 + 工作区），不隐去任何区域。
 * 关系图谱不在这里：渲染层用 react-force-graph-2d 做物理力导向布局（节点位置每次不同），不适合像素对拍，由 Viva 侧的像素取色与命中单测覆盖。
 *
 * 数据层是页内替换（lib/react-wiki.mjs）：页面、链接、来源与建议取夹具（viva/parity/testdata/wiki-fixture.json）。
 * Viva 一侧（viva/parity/scenes/wiki.go）做同样的操作，最终画面才可比。
 */
import { WIKI_FIXTURE, enterWiki, leaveWiki, wikiLabel } from "../lib/react-wiki.mjs";

/** 场景：options 选夹具数据组，steps 把页面带到要对照的状态。 */
function wikiScene(options, steps) {
  return {
    module: "wiki",
    async enter({ win, session }) {
      await enterWiki(session, options);
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveWiki(session);
    },
  };
}

const pageOf = (id) => WIKI_FIXTURE.pages.find((p) => p.id === id);

/** 点目录里标题为 title 的页面行，等详情头部出现与读取落定（详情与更新建议各读一次）。 */
async function selectPage(win, id) {
  const title = pageOf(id).title;
  const before = await win.evaluate(() => ({ ...(window.__parityWiki ?? {}) }));
  await win.locator("button").filter({ has: win.getByText(title, { exact: true }) }).first().click();
  await win.getByRole("heading", { name: title, level: 1, exact: true }).waitFor({ state: "visible" });
  await win.waitForFunction((b) => window.__parityWiki.getPage > b.getPage && window.__parityWiki.suggestion > b.suggestion, before);
  await win.waitForTimeout(300);
}

/** 正文里二、三级标题的个数（页内目录按它显示「本页目录（N 节）」，渲染层 extractWikiToc）。 */
function tocCount(id) {
  return pageOf(id).body.split("\n").filter((line) => /^#{2,3}\s/.test(line)).length;
}

/** 典型页面详情：选一页并展开页内目录。 */
async function openDetail(win) {
  const id = WIKI_FIXTURE.scene.detailPageId;
  await selectPage(win, id);
  await win.getByRole("button", { name: wikiLabel("toc", tocCount(id)), exact: true }).click();
  await win.waitForTimeout(300);
}

/** 编辑态：选一页后点「编辑」，正文换成编辑框。 */
async function openEdit(win) {
  await selectPage(win, WIKI_FIXTURE.scene.editPageId);
  await win.getByRole("button", { name: wikiLabel("edit"), exact: true }).click();
  await win.locator("textarea").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
}

/** 更新建议：选手动维护的那一页，展开建议面板（details 的 summary 是建议的原因）。 */
async function openSuggestion(win) {
  const id = WIKI_FIXTURE.scene.suggestionPageId;
  await selectPage(win, id);
  await win.getByText(WIKI_FIXTURE.suggestions[id].reason, { exact: true }).click();
  await win.waitForTimeout(300);
}

/** 编译对话框：点顶栏的「编译 N 条新内容」，等预览候选出现。 */
async function openCompile(win) {
  const counts = WIKI_FIXTURE.compile.preview.counts;
  await win.getByRole("button", { name: wikiLabel("compilePending", counts.ready), exact: true }).click();
  await win.getByRole("button", { name: wikiLabel("compileStart"), exact: true }).waitFor({ state: "visible" });
  await win.waitForTimeout(400);
}

export const WIKI_SCENES = {
  "app.window.wiki.empty": wikiScene({ empty: true }),
  "app.window.wiki.detail": wikiScene({ plain: true }, openDetail),
  "app.window.wiki.edit": wikiScene({}, openEdit),
  "app.window.wiki.suggestion": wikiScene({ plain: true }, openSuggestion),
  // 编译对话框的背景是目录页：不带手动编辑标记，免得铅笔图标（Chromium 里是有名字的 image）在 L1 里没有映射规则（台账 G-027）而盖住对话框本身的对照。
  "app.window.wiki.compile": wikiScene({ plain: true }, openCompile),
};
