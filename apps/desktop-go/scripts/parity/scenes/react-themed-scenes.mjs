/**
 * 主题阅读（AI 阅读，M3-5b，台账 B-064）的 React 侧对照场景：知识库详情切到「AI 阅读」之后的原生部分——
 * 生成设置对话框、任务状态卡（进行中、失败）、阅读页设置菜单里的资料面板——以及处理中心的「主题页任务」页。
 * 阅读岛里的页面本体由 WebView2 承载，不在无头对照范围内：页面场景把岛内的内容隐去（lib/react-library.mjs 的 config.detail.themed），
 * Viva 一侧对应一块没有内容的阅读区（Screen.BlankPage）。
 *
 * 数据层是页内替换（lib/react-themed.mjs）：任务与页面取夹具（viva/parity/testdata/themed-fixture.json）。
 * Viva 一侧（viva/parity/scenes/themed.go）做同样的操作，最终画面才可比。
 */
import { enterInbox, leaveInbox } from "../lib/react-inbox.mjs";
import { THEMED_FIXTURE, applyThemedPatch, themedLabel } from "../lib/react-themed.mjs";
import { CARD_ANCHOR, card, listScene } from "./react-library-list-scenes.mjs";

const tr = (key) => themedLabel("themedReading", key);

/** 点列表里的那篇笔记，等详情头部出现与读取落定。 */
async function selectItem(win) {
  const box = await card(win, THEMED_FIXTURE.itemTitle).boundingBox();
  await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
  await win.getByTestId("article-header").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
}

/** 点读者工具条里的「AI 阅读」，等主题阅读把状态读进来。 */
async function openAiReading(win) {
  await win.getByTestId("article-toolbar").getByRole("button", { name: tr("readerAi"), exact: true }).click();
  await win.waitForFunction(() => !!window.__parityThemed && window.__parityThemed.get > 0);
  await win.waitForTimeout(300);
}

/**
 * 知识库详情里的主题阅读场景：config.detail.themed 只隐去岛里的页面内容；config 选夹具的开关（页面与最近的任务）。
 * steps 在切到 AI 阅读之后把页面带到要对照的状态。
 */
function themedScene(config, steps) {
  return listScene({ detail: { themed: true } }, async (win) => {
    await applyThemedPatch(win, config);
    await selectItem(win);
    await openAiReading(win);
    if (steps) await steps(win);
  });
}

/**
 * 没有页面也没有任务：阅读区是空态（「换一种方式阅读这篇文章」），点「生成阅读页」打开生成设置对话框。
 * 渲染层的自动弹出（autoSetupAttempted）要求没有 original 内容，知识库详情里总是带着原文，所以不会自动弹。
 */
async function waitSetup(win) {
  await win.getByRole("button", { name: tr("readerCreate"), exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "visible" });
  await win.waitForTimeout(400);
}

/** 任务状态卡出现。 */
async function waitStatus(win) {
  await win.getByTestId("reading-task-status").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
}

/** 失败卡：展开折叠的「生成详情」，里面才有失败原因与用量。 */
async function openDetails(win) {
  await waitStatus(win);
  await win.getByTestId("reading-task-status").getByText(tr("readerTaskDetails"), { exact: true }).click();
  await win.waitForTimeout(300);
}

/**
 * 页面的资料面板：点「阅读页设置」→ 菜单里的「参考资料」。
 * 页面的「目录」钮是阅读岛的功能（Viva 的岛里没有桥接脚本，没有目录），不在对照范围内：把它从工具条里隐去（采集端口径，同 HIDE_*）。
 */
async function openReferences(win) {
  const settings = win.getByRole("button", { name: tr("readerSettings"), exact: true });
  await settings.waitFor({ state: "visible" });
  const catalog = themedLabel("library", "catalog");
  await win.evaluate((label) => {
    for (const button of document.querySelectorAll('[data-testid="article-toolbar"] button')) {
      if ((button.textContent || "").trim() === label) button.setAttribute("data-parity-hide", "1");
    }
  }, catalog);
  await settings.click();
  await win.getByRole("menuitem", { name: tr("readerReferences"), exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "visible" });
  await win.waitForTimeout(400);
}

/** 处理中心：点「主题页任务」，等任务列表读进来。 */
const tasksScene = {
  module: "inbox",
  async enter({ win, session }) {
    await enterInbox(session, {});
    await applyThemedPatch(win, {});
    await win.getByRole("button", { name: tr("tasks"), exact: true }).click();
    await win.waitForFunction((count) => document.querySelectorAll('[data-testid="reading-task-status"]').length === count, THEMED_FIXTURE.inbox.tasks.length);
    await win.waitForTimeout(300);
  },
  async leave({ session }) {
    await leaveInbox(session);
  },
};

export const THEMED_SCENES = {
  "app.window.library.detail-themed-setup": themedScene({}, waitSetup),
  "app.window.library.detail-themed-running": themedScene({ task: "running" }, waitStatus),
  "app.window.library.detail-themed-failed": themedScene({ task: "failed" }, openDetails),
  "app.window.library.detail-themed-tools": themedScene({ page: true }, openReferences),
  "app.window.inbox.themed-tasks": tasksScene,
};
