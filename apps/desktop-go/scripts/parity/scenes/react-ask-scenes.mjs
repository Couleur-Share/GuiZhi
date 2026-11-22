/**
 * AI 问答（M3-4）的 React 侧对照场景：全局问答的空态、一轮带来源与证据的对话、未配置模型、围绕本文的会话，
 * 以及知识库详情右侧的「围绕本文提问」栏。整窗对照（rail + 顶栏 + 侧栏 + 工作区），不隐去任何区域；
 * 知识库详情那一条只隐去阅读区（与既有详情场景同一口径，见 lib/react-library.mjs）。
 *
 * 数据层是页内替换（lib/react-ask.mjs）：会话取夹具（viva/parity/testdata/ask-fixture.json），模型是否已配置由配置决定。
 * Viva 一侧（viva/parity/scenes/ask.go）做同样的操作，最终画面才可比。
 */
import { ASK_FIXTURE, applyAskPatch, askLabel, enterAsk, leaveAsk } from "../lib/react-ask.mjs";
import { CARD_ANCHOR, card, listScene } from "./react-library-list-scenes.mjs";

/** 场景：options 选夹具的开关，steps 把页面带到要对照的状态。 */
function askScene(options, steps) {
  return {
    module: "ask",
    async enter({ win, session }) {
      await enterAsk(session, options);
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveAsk(session);
    },
  };
}

/**
 * 点第一枚来源 chip：展开该来源回答时的证据摘录，chip 高亮 2 s（渲染层 focusCitation 的 setTimeout，用真实时间）。
 * 等 2.3 s 让高亮走完：对照的是稳定后的状态，不依赖两侧时钟的相对快慢。
 */
async function openFirstEvidence(win) {
  await win.locator('[data-citation="1"]').click();
  await win.waitForTimeout(2600); // 摘录出现（无动画）+ 高亮走完
}

const NOTE = ASK_FIXTURE.article.title; // item-001：笔记，围绕本文的会话挂在它名下

/** 点一张卡片左内边距处，等详情头部出现与读取落定（同 react-library-detail-scenes.mjs 的 selectCard）。 */
async function selectNote(win) {
  const box = await card(win, NOTE).boundingBox();
  await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
  await win.getByTestId("article-header").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
}

/** 点详情头部的「提问」，等右侧提问栏出现、本文会话读进来（页内替换的会话接口先装好）。 */
async function openArticleAsk(win) {
  await applyAskPatch(win);
  await win.getByTestId("article-header").getByRole("button", { name: askLabel("askTitle"), exact: true }).click();
  await win.getByTestId("article-ask-panel").waitFor({ state: "visible" });
  await win.waitForFunction(() => !!window.__parityAsk && window.__parityAsk.get > 0);
  await win.waitForTimeout(400);
}

export const ASK_SCENES = {
  "app.window.ask.empty": askScene({}),
  "app.window.ask.conversation": askScene({ remembered: ASK_FIXTURE.conversationId }, openFirstEvidence),
  "app.window.ask.not-configured": askScene({ notConfigured: true }),
  "app.window.ask.article-global": askScene({ remembered: ASK_FIXTURE.articleSessionId }),
  // 知识库详情 + 右侧提问栏（台账 B-061）：场景名跟随既有详情场景的 detail-<名> 写法。
  "app.window.library.detail-ask": listScene({ detail: {} }, async (win) => {
    await selectNote(win);
    await openArticleAsk(win);
  }),
};
