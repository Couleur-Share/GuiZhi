/** 本文问答隔离验收；全部模型/搜索响应由测试 IPC 提供，不访问真实第三方。 */
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";

export default async ({ win, app, shot, outDir }) => {
  const errors = [];
  win.on("pageerror", error => errors.push(error.message));
  await app.evaluate(({ ipcMain }) => {
    globalThis.__articleAskFixture = { searchFail: false, searchConfigured: true, prompts: [] };
    for (const channel of ["ai:httpRequest", "ai:httpStream", "articleAsk:search", "themedReading:searchConfig"]) ipcMain.removeHandler(channel);
    ipcMain.handle("ai:httpRequest", (_event, request) => {
      globalThis.__articleAskFixture.prompts.push(JSON.parse(request.body));
      return { ok: true, status: 200, statusText: "OK", headers: {}, body: JSON.stringify({ choices: [{ message: { content: '["反向代理 流量 路径"]' }, finish_reason: "stop" }], usage: { prompt_tokens: 10, completion_tokens: 10 } }) };
    });
    ipcMain.handle("ai:httpStream", async (event, request) => {
      globalThis.__articleAskFixture.prompts.push(JSON.parse(request.body));
      for (const text of ["结合本文，**反向代理**负责转发请求。[1]\n\n", "流量是否经过中转，需要检查实际播放地址与连接路径。联网资料只能作为补充依据。"] ) {
        event.sender.send("ai:httpStreamChunk", { requestId: request.requestId, chunk: `data: ${JSON.stringify({ choices: [{ delta: { content: text }, finish_reason: null }] })}\n\n` });
        await new Promise(resolve => setTimeout(resolve, 120));
      }
      event.sender.send("ai:httpStreamChunk", { requestId: request.requestId, chunk: `data: ${JSON.stringify({ choices: [{ delta: {}, finish_reason: "stop" }], usage: { prompt_tokens: 100, completion_tokens: 60 } })}\n\ndata: [DONE]\n\n` });
      return { ok: true, status: 200, statusText: "OK", body: "", headers: {} };
    });
    ipcMain.handle("articleAsk:search", () => globalThis.__articleAskFixture.searchFail ? { success: false, error: "HTTP 402：搜索额度已用尽" } : { success: true, sources: [{ ordinal: 0, kind: "web", title: "反向代理工作原理（测试资料）", url: "https://example.com/proxy", text: "反向代理转发客户端请求，实际流量路径取决于资源地址。", capturedAt: Date.now() }], warnings: [] });
    ipcMain.handle("themedReading:searchConfig", () => ({ success: true, search: { configured: globalThis.__articleAskFixture.searchConfigured, persistent: false, provider: "tavily" } }));
  });
  const items = await win.evaluate(async () => {
    const settings = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(settings.state, { language: "zh", themeMode: "light", isDarkMode: false, editorMarkdownPreview: true, aiProvider: "openai", aiApiProtocol: "openai", aiApiKey: "isolated-fixture-key", aiApiUrl: "https://example.com/v1", aiModel: "fixture-model" });
    localStorage.setItem("guizhi-settings", JSON.stringify(settings));
    localStorage.setItem("ui-storage", JSON.stringify({ state: { isSidebarCollapsed: true }, version: 0 }));
    localStorage.setItem("guizhi-setup-dismissed", "1"); localStorage.setItem("guizhi-migration-dismissed", "1");
    await window.api.settings.set({ aiProvider: "openai", aiApiProtocol: "openai", aiApiKey: "isolated-fixture-key", aiApiUrl: "https://example.com/v1", aiModel: "fixture-model" });
    return Promise.all([
      window.api.knowledge.create({ title: "本文问答验收：理解反向代理", content: "# 理解播放链路\n\n反向代理负责转发请求。\n\n## 需要核对的条件\n\n播放地址直连资源时，视频内容可能不经过穿透服务器。\n\n文章中的结论需要结合实际连接路径判断。" }),
      window.api.knowledge.create({ title: "本文问答验收：视频", itemType: "video", content: "视频总结。", transcript: "00:00 反向代理负责转发请求。\n\n00:12 需要核对实际播放地址。" }),
      window.api.knowledge.create({ title: "本文问答验收：图片", itemType: "image", content: "图片文案。\n\n## 图中文字\n\n反向代理与资源地址的关系。" }),
      window.api.knowledge.create({ title: "本文问答验收：论坛", itemType: "forum", content: "## 讨论总结\n\n讨论反向代理的流量路径。\n\n## 正文\n\n应该怎样理解反向代理？\n\n## 讨论（2 条）\n\n### 1 楼 · 小林\n\n反向代理负责转发请求。\n\n### 2 楼 · 小周\n\n需要检查资源地址是否直连。" }),
    ]);
  });
  await win.reload(); await win.setViewportSize({ width: 1680, height: 1000 });
  const selectItem = async item => win.getByTestId("item-list").getByText(item.title, { exact: true }).click();
  const panel = win.getByTestId("article-ask-panel");
  const selectText = async text => {
    await win.locator("[data-article-reader]").evaluate((root, text) => {
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT); let node;
      while ((node = walker.nextNode())) { const at = node.textContent.indexOf(text); if (at < 0 || !node.parentElement.getClientRects().length) continue;
        const range = document.createRange(); range.setStart(node, at); range.setEnd(node, at + text.length);
        const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range);
        node.parentElement.dispatchEvent(new MouseEvent("mouseup", { bubbles: true })); return;
      }
      throw new Error(`找不到文本：${text}`);
    }, text);
    await win.getByRole("button", { name: "解释这段", exact: true }).click();
    await panel.locator("blockquote").last().waitFor();
  };
  await selectItem(items[0]);
  await selectText("反向代理负责转发请求。");
  assert.equal(await panel.getByRole("switch").getAttribute("aria-checked"), "true");
  assert.match(await panel.getByRole("textbox").inputValue(), /解释这段/);
  await shot("01-selected-passage-light");
  await panel.getByRole("button", { name: "发送", exact: true }).click();
  await panel.getByText("fixture-model", { exact: true }).waitFor();
  assert.match(await panel.innerText(), /流量是否经过中转/);
  await shot("02-answer-with-sources");
  const history = await win.evaluate(id => window.api.askSession.list({ scope: "article", itemId: id }), items[0].id);
  assert.equal(history.length, 1);
  const record = await win.evaluate(id => window.api.askSession.get(id), history[0].id);
  assert.equal(JSON.parse(record.messagesJson)[0].context.target.selection, "反向代理负责转发请求。");
  await app.evaluate(() => { globalThis.__articleAskFixture.searchFail = true; });
  await panel.getByRole("textbox").fill("这个结论一定成立吗？");
  await panel.getByRole("button", { name: "发送", exact: true }).click();
  await panel.getByText("未完成联网查证", { exact: true }).waitFor();
  await win.waitForFunction(() => document.querySelectorAll('[data-testid="article-ask-message"]').length === 2 && !document.querySelector('[data-testid="article-ask-panel"] [role="status"].animate-pulse'));
  await shot("03-search-failure-keeps-answer");
  await win.evaluate(() => document.documentElement.classList.add("dark"));
  await win.setViewportSize({ width: 1120, height: 860 });
  await shot("04-dark-narrow-panel");
  await panel.getByRole("button", { name: "收起本文问答" }).click();
  await win.getByRole("button", { name: "围绕本文提问", exact: true }).click();
  assert.equal(await panel.getByTestId("article-ask-message").count(), 2);
  await panel.getByRole("button", { name: "收起本文问答" }).click();
  for (const [index, tab, text] of [[1, "文字稿", "反向代理负责转发请求。"], [2, "图中文字", "反向代理与资源地址的关系。"], [3, "讨论", "反向代理负责转发请求。"]]) {
    await selectItem(items[index]);
    await win.getByTestId("article-toolbar").getByRole("button", { name: tab, exact: tab !== "文字稿" }).click();
    await selectText(text);
    await shot(`05-selection-${index}`);
    await panel.getByRole("button", { name: "收起本文问答" }).click();
  }
  await win.reload(); await selectItem(items[0]);
  await win.getByRole("button", { name: "围绕本文提问", exact: true }).click();
  await panel.getByTestId("article-ask-message").nth(1).waitFor();
  await shot("06-restored-history");
  assert.deepEqual(errors, []);
  await fs.writeFile(path.join(outDir, "evidence.json"), JSON.stringify({ success: true, isolated: true, realThirdParty: false, plainTextSelection: true, transcriptOcrDiscussion: true, persistence: true, searchFailure: true, errors }, null, 2));
  return { items, errors };
};
