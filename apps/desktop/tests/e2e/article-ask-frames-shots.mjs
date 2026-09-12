import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import { require as tsRequire } from "tsx/cjs/api";
import basic from "./article-ask-shots.mjs";
import { makePage } from "./article-reader-redesign-shots.mjs";

export default async context => {
  const { items, errors } = await basic(context), { win, app, shot, outDir } = context;
  const { buildThemedReadingSource } = tsRequire("../../src/main/services/themed-reading/content.ts", import.meta.url);
  const { themedReadingDocument } = tsRequire("../../src/main/services/themed-reading/document.ts", import.meta.url);
  const { snapshotDocument } = tsRequire("../../src/main/services/web-capture/snapshot-document.ts", import.meta.url);
  const page = await makePage(items[0], "body", buildThemedReadingSource, themedReadingDocument);
  const snapshot = { formatVersion: 1, policyVersion: 1, adapterVersion: "fixture", html: "<h1>原始网页快照</h1><p>网页快照中的反向代理解释。</p>", css: "", hash: "0".repeat(64), account: "测试", author: "测试", publishedAt: null, assets: [], failures: [], warnings: [] };
  const snapshotHtml = snapshotDocument(snapshot, "snapshot-fixture");
  await app.evaluate(({ ipcMain }, { itemId, page, snapshot, snapshotHtml }) => {
    const oldGet = ipcMain._invokeHandlers.get("knowledge:get"), oldContext = ipcMain._invokeHandlers.get("articleAsk:context");
    ipcMain.removeHandler("knowledge:get");
    ipcMain.handle("knowledge:get", async (event, id) => { const item = await oldGet(event, id); return id === itemId ? { ...item, itemType: "webpage", sourceUri: "https://mp.weixin.qq.com/s/fixture" } : item; });
    for (const name of ["themedReading:state", "themedReading:get", "web:snapshot", "articleAsk:context"]) ipcMain.removeHandler(name);
    ipcMain.handle("themedReading:state", (_event, input) => ({ success: true, state: { hasPage: input.itemId === itemId, versionId: page.page.id } }));
    ipcMain.handle("themedReading:get", (_event, input) => ({ success: true, page: input.itemId === itemId ? page.page : null,
      document: page.document.replace('data-instance="article-reader-fixture"', `data-instance="${input.instanceId}"`), models: { text: "fixture-model" }, search: { configured: true, provider: "tavily" } }));
    ipcMain.handle("web:snapshot", () => ({ ok: true, data: { document: snapshotHtml, instanceId: "snapshot-fixture", edited: false, pending: false,
      version: { id: "snapshot-version", itemId, snapshot, markdown: "网页快照中的反向代理解释。", title: "快照", capturedAt: 1 } } }));
    ipcMain.handle("articleAsk:context", (event, input) => {
      if (!["themed", "snapshot"].includes(input.target.view)) return oldContext(event, input);
      const text = input.target.view === "themed" ? "反向代理负责转发请求。" : "网页快照中的反向代理解释。";
      return { success: true, context: { target: input.target, title: "版本隔离夹具", fingerprint: "fixture", clipped: false,
        sources: [{ ordinal: 1, kind: "article", title: input.target.view === "themed" ? "AI 阅读页" : "网页快照", text, target: input.target, fingerprint: "fixture" }] } };
    });
    globalThis.__articleAskFixture.searchFail = false;
  }, { itemId: items[0].id, page, snapshot, snapshotHtml });
  await win.reload(); await win.setViewportSize({ width: 1680, height: 1000 });
  await win.getByTestId("item-list").getByText(items[0].title, { exact: true }).click();
  const panel = win.getByTestId("article-ask-panel");
  const selectInFrame = async text => {
    const locator = win.locator('iframe[data-article-instance]:visible').first(); await locator.waitFor();
    assert.equal(await locator.getAttribute("sandbox"), "allow-scripts");
    const frame = await locator.elementHandle().then(h => h.contentFrame());
    await frame.waitForFunction(text => document.body?.textContent?.includes(text), text);
    await frame.evaluate(text => {
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT); let node;
      while ((node = walker.nextNode())) { const start = node.textContent.indexOf(text); if (start < 0) continue;
        const range = document.createRange(); range.setStart(node, start); range.setEnd(node, start + text.length); getSelection().removeAllRanges(); getSelection().addRange(range);
        document.dispatchEvent(new MouseEvent("mouseup", { bubbles: true })); return;
      } throw new Error("iframe 文本不存在");
    }, text);
    await win.getByRole("button", { name: "解释这段", exact: true }).click();
    assert.equal(await panel.locator("footer blockquote").innerText(), text);
    await panel.getByRole("button", { name: "发送", exact: true }).click();
    await panel.getByTestId("article-ask-message").last().getByText("fixture-model", { exact: true }).waitFor();
  };
  await selectInFrame("反向代理负责转发请求。"); await shot("07-themed-frame-selection");
  await panel.getByRole("button", { name: "收起本文问答" }).click();
  await win.getByTestId("article-toolbar").getByRole("button", { name: "网页快照", exact: true }).click();
  await selectInFrame("网页快照中的反向代理解释。"); await shot("08-snapshot-frame-selection");
  const history = await win.evaluate(id => window.api.askSession.list({ scope: "article", itemId: id }), items[0].id);
  const saved = await win.evaluate(id => window.api.askSession.get(id), history[0].id), messages = JSON.parse(saved.messagesJson);
  assert.equal(messages.at(-1).context.target.versionId, "snapshot-version");
  assert.equal(messages.at(-2).context.target.versionId, page.page.id);
  await win.getByRole("button", { name: "AI 问答", exact: true }).click();
  await win.getByRole("button", { name: "展开", exact: true }).click();
  await win.getByRole("button", { name: new RegExp(`本文问答.*${items[0].title}`) }).first().click();
  await panel.getByTestId("article-ask-message").last().waitFor();
  assert.equal(await panel.getByTestId("article-ask-message").count(), messages.length);
  await shot("09-global-article-history");
  await panel.getByRole("button", { name: "新对话", exact: true }).click();
  await app.evaluate(() => { globalThis.__articleAskFixture.searchConfigured = false; });
  await panel.getByRole("textbox").fill("新会话中的问题");
  await panel.getByRole("button", { name: "发送", exact: true }).click();
  await panel.getByText("尚未配置联网搜索，问题已保留。", { exact: true }).waitFor();
  assert.equal(await panel.getByRole("textbox").inputValue(), "新会话中的问题");
  await shot("10-unconfigured-search-keeps-draft");
  await panel.getByRole("button", { name: "关闭联网后发送", exact: true }).click();
  await panel.getByText("fixture-model", { exact: true }).waitFor();
  assert.equal(await panel.getByRole("switch").getAttribute("aria-checked"), "false");
  const afterNew = await win.evaluate(id => window.api.askSession.list({ scope: "article", itemId: id }), items[0].id);
  assert.equal(afterNew.length, 2);
  assert.notEqual(afterNew[0].id, history[0].id);
  assert.equal(await win.evaluate(() => localStorage.getItem("guizhi-ask-active-session")), afterNew[0].id);
  await win.getByRole("button", { name: "返回文章", exact: true }).click();
  await win.getByTestId("article-content").waitFor();
  assert.deepEqual(errors, []);
  await fs.writeFile(path.join(outDir, "frames-evidence.json"), JSON.stringify({ success: true, isolated: true, realThirdParty: false, iframeSelection: true, exactVersions: true, sharedGlobalHistory: true, errors }, null, 2));
};
