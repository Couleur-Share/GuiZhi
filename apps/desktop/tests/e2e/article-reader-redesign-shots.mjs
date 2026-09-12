/** 阅读页隔离验收：合成文章、真实 iframe 文档封装、全部 AI/搜索 IPC 使用夹具。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import { require as tsRequire } from "tsx/cjs/api";

const title = "一篇文章，三个阅读视角：内容、来源与自己的判断";
const body = `# 留下值得反复阅读的内容

阅读时，先抓住文章最重要的问题，再辨认作者如何组织材料，最后记录自己的理解。这三个视角可以分开，也能相互补充。

## 先理解文章在回答什么

一段清楚的摘要，应该让读者知道文章的主题与结论。细节可以稍后阅读，核心问题需要先看见。

> 好的阅读界面把内容放在眼前，把操作留在需要的时候。

## 给来源留下位置

当我们需要核对一个结论时，能方便地找到原始材料很重要。整理后的文章与原文各有用途，阅读时可以随时切换。

## 写下自己的判断

读完一节，试着用自己的话写一句总结。把仍有疑问的内容记下来，留待下次查证。

1. 找到文章的核心问题。
2. 核对结论所依赖的材料。
3. 留下一条自己的理解。

## 下次再读时

带着新的问题回来，会看到之前忽略的细节。内容、来源与判断都应保留，界面则可以安静一些。
`;
const longError = "关键资料不足：现有内容主要是页面简介与零散片段，尚不足以覆盖关键查证问题。缺少：1. 原始标准的完整定义与适用范围，搜索返回的页面只有状态信息，没有正文条款。2. 不同处理方式与保存条件之间关系的完整研究，现有摘要没有提供方法、样本和结论边界。3. 术语的正式定义和适用条件，当前资料只是机构简介。4. 支持不同定义相互比较的材料。请补充完整原始资料后，再继续联网查证。";

export async function makePage(item, kind, buildThemedReadingSource, themedReadingDocument) {
  const source = await buildThemedReadingSource(item, kind);
  const now = Date.now();
  const page = {
    id: `fixture-page-${item.id}-${kind}`, itemId: item.id, sourceKind: kind, role: "current", formatVersion: 1,
    source, options: { style: "安静、清晰的阅读页", research: true, generateImages: false, maxImages: 0 },
    assets: [], warnings: [], createdAt: now, updatedAt: now,
    design: {
      direction: "清晰的文章排版", assets: [],
      html: `<main class="quiet-article">${source.blocks.map(block => `<div data-source-block="${block.id}"></div>`).join("")}</main>`,
      css: ":root{--theme-surface:#fbfaf7;--theme-text:#27282b;--theme-accent:#5d7395}[data-theme=dark]{--theme-surface:#12161c;--theme-text:#e7e9ec;--theme-accent:#a6bce0}.quiet-article{max-width:760px;margin:0 auto;padding:36px 44px 64px}h1{font-size:32px;line-height:1.4;margin:0 0 24px}h2{font-size:22px;margin-top:36px}p,li{font-size:16px;line-height:1.9}blockquote{border-left:3px solid var(--theme-accent);padding-left:20px;opacity:.85}@media(max-width:650px){.quiet-article{padding:28px 24px}h1{font-size:27px}}",
    },
  };
  return { page, document: themedReadingDocument(page, "article-reader-fixture") };
}

export default async ({ win, app, shot, outDir, userDataDir, mainEntry }) => {
  process.stdout.write("开始文章页隔离验收\n");
  const { buildThemedReadingSource } = tsRequire("../../src/main/services/themed-reading/content.ts", import.meta.url);
  const { themedReadingDocument } = tsRequire("../../src/main/services/themed-reading/document.ts", import.meta.url);
  const cases = [], skipped = [], titleGeometry = [], tabGeometry = [];
  const stableShot = async name => {
    await win.mouse.move(2, 2);
    await win.waitForTimeout(220);
    if (["01-video-ai-research-failed-dark", "04-video-ai-narrow-light", "05a-video-ai-narrow-sidebar-open-dark"].includes(name)) {
      const geometry = await win.getByTestId("item-title-input").evaluate(node => ({ width: node.clientWidth, height: node.clientHeight, scrollHeight: node.scrollHeight, overflowY: getComputedStyle(node).overflowY }));
      assert(geometry.scrollHeight <= geometry.height + 2 || geometry.overflowY === "auto", "变更容器宽度后，标题应全部显示或明确允许滚动");
      titleGeometry.push({ name, ...geometry });
    }
    if (name === "05a-video-ai-narrow-sidebar-open-dark") {
      for (const label of ["正文", /文字稿/]) {
        const geometry = await control(label).evaluate(node => {
          let viewport = node.parentElement;
          while (viewport.parentElement && !["auto", "scroll", "hidden"].includes(getComputedStyle(viewport).overflowX)) viewport = viewport.parentElement;
          const tab = node.getBoundingClientRect(), clip = viewport.getBoundingClientRect();
          return { text: node.textContent, left: tab.left, right: tab.right, top: tab.top, bottom: tab.bottom, clipLeft: clip.left, clipRight: clip.right, clipTop: clip.top, clipBottom: clip.bottom };
        });
        assert(geometry.left >= geometry.clipLeft - 1 && geometry.right <= geometry.clipRight + 1 && geometry.top >= geometry.clipTop - 1 && geometry.bottom <= geometry.clipBottom + 1, `窄屏主标签不得被工具挤压裁切：${geometry.text}`);
        tabGeometry.push(geometry);
      }
    }
    await shot(name);
    process.stdout.write(`已验收 ${name}\n`);
    cases.push(name);
  };
  const control = name => win.getByRole("button", { name, exact: typeof name === "string" }).or(win.getByRole("tab", { name, exact: typeof name === "string" })).first();
  const clickIf = async name => { const target = control(name); if (await target.isVisible()) { await target.click(); return true; } return false; };
  const theme = async dark => {
    await win.evaluate(dark => document.documentElement.classList.toggle("dark", dark), dark);
    await win.waitForTimeout(180);
  };
  const rows = await win.evaluate(async ({ title, body }) => {
    const settings = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(settings.state, { language: "zh", themeMode: "dark", isDarkMode: true, editorMarkdownPreview: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(settings));
    localStorage.setItem("ui-storage", JSON.stringify({ state: { isSidebarCollapsed: true }, version: 0 }));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
    return Promise.all([
      window.api.knowledge.create({ title, itemType: "video", content: `> 平台：演示视频 · 作者：阅读笔记 · 时长：4:44\n\n${body}`, transcript: "00:00 今天我们聊聊如何把一篇文章读明白。\n\n00:22 第一步，找到作者最想回答的问题。\n\n01:05 第二步，留意结论所依赖的材料，并保留查阅来源的方法。\n\n02:18 第三步，用自己的话写下一句理解。\n\n03:30 下次带着新的问题回来，还能看到新的细节。" }),
      window.api.knowledge.create({ title: "如何安排每天的阅读：一次讨论的整理", itemType: "forum", content: `> 平台：演示讨论\n\n## 讨论总结\n\n${body}\n\n## 正文\n\n我想给每天留出二十分钟阅读，大家有什么建议？\n\n## 讨论（2 条）\n\n### 1 楼 · 小林\n\n先从感兴趣的短文章开始。\n\n### 2 楼 · 小周\n\n每次留下一句自己的总结。` }),
      window.api.knowledge.create({ title: "周末随记：把一个问题读明白", itemType: "note", content: body }),
    ]);
  }, { title, body });
  const pages = [
    await makePage(rows[0], "body", buildThemedReadingSource, themedReadingDocument),
    await makePage(rows[1], "summary", buildThemedReadingSource, themedReadingDocument),
  ];
  await app.evaluate(({ ipcMain, BrowserWindow }, { rows, pages, longError }) => {
    const task = { id: "article-reader-research-fixture", itemId: rows[0].id, sourceKind: "body", versionId: "fixture-working", title: rows[0].title,
      state: "failed", stage: "research", completed: 2, total: 8, plannedImages: 0,
      usage: { searchCalls: 3, pagesRead: 3, textCalls: 5, imageCalls: 0, imagesSaved: 0 }, error: longError, createdAt: Date.now(), updatedAt: Date.now() };
    const state = { task, baseTask: task, pages, basePages: pages, calls: [], rows };
    globalThis.__articleReaderFixture = state;
    const find = input => state.pages.find(f => f.page.itemId === input.itemId && f.page.sourceKind === input.sourceKind);
    const send = () => { for (const win of BrowserWindow.getAllWindows()) win.webContents.send("themedReading:progress", state.task); };
    for (const name of ["get", "state", "generate", "cancel", "resume", "continueOffline", "listTasks", "references", "regenerateAsset", "restorePrevious", "remove", "exportHtml", "searchConfig"]) ipcMain.removeHandler(`themedReading:${name}`);
    ipcMain.handle("themedReading:state", (_event, input) => { const f = find(input); return { success: true, state: { hasPage: !!f, versionId: f?.page.id, formatVersion: f?.page.formatVersion, stale: false } }; });
    ipcMain.handle("themedReading:get", (_event, input) => {
      const f = find(input);
      return { success: true, page: f?.page ?? null, previous: !!f, stale: false, task: input.itemId === task.itemId && input.sourceKind === task.sourceKind ? state.task : null,
        document: f?.document.replace('data-instance="article-reader-fixture"', `data-instance="${input.instanceId}"`),
        models: { text: "isolated-fixture-model", image: null }, search: { configured: true, persistent: false, provider: "tavily" } };
    });
    ipcMain.handle("themedReading:listTasks", () => ({ success: true, tasks: [state.task].filter(Boolean) }));
    ipcMain.handle("themedReading:references", () => ({ success: true, references: [] }));
    for (const name of ["generate", "cancel", "resume", "continueOffline", "regenerateAsset", "restorePrevious", "remove", "exportHtml", "searchConfig"]) {
      ipcMain.handle(`themedReading:${name}`, (_event, input) => {
        state.calls.push({ name, input });
        if (name === "cancel") state.task = { ...state.task, state: "cancelled" };
        if (name === "resume" || name === "continueOffline") state.task = { ...state.task, state: "running", stage: name === "continueOffline" ? "write" : "research", error: undefined };
        send(); return { success: true, task: state.task };
      });
    }
  }, { rows, pages, longError });
  await win.reload();
  await win.setViewportSize({ width: 1680, height: 1080 });
  const list = win.getByTestId("item-list");
  const frame = win.frameLocator('iframe[aria-label="AI 重构阅读"]');
  await list.getByText(rows[0].title, { exact: true }).click();
  await frame.locator("h1").waitFor();
  await stableShot("01-video-ai-research-failed-dark");
  if (await clickIf("文章信息与工具")) {
    await win.getByRole("region", { name: "文章信息与工具", exact: true }).waitFor();
    await stableShot("01a-article-info-tools-dark");
    await win.getByRole("button", { name: "收起文章工具", exact: true }).click();
    await win.locator("#article-tools-panel").waitFor({ state: "hidden" });
    assert.equal(await win.locator("#article-tools-panel").count(), 1, "收起工具区后保留子组件状态");
    await frame.locator("h1").waitFor();
  }
  if (await clickIf("阅读页设置")) {
    await win.getByRole("menu").waitFor();
    await stableShot("01b-reading-settings-menu-dark");
    await win.getByRole("menuitem", { name: "参考资料", exact: true }).click();
    await win.getByRole("dialog", { name: "参考资料", exact: true }).waitFor();
    await stableShot("01c-reading-references-dark");
    await win.keyboard.press("Escape");
    await win.getByRole("dialog").waitFor({ state: "detached" });
    await control("阅读页设置").click();
    await win.getByRole("menu").waitFor();
    await win.waitForTimeout(220);
    await win.getByRole("menuitem", { name: "页面图片", exact: true }).click();
    await win.getByRole("dialog", { name: "页面图片", exact: true }).waitFor();
    await stableShot("01d-reading-assets-dark");
    await win.keyboard.press("Escape");
    await win.getByRole("dialog").waitFor({ state: "detached" });
  }
  if (await win.getByTestId("reading-task-status").count()) assert.equal(await win.getByText(longError, { exact: true }).isVisible(), false, "长错误默认折叠");
  const details = win.locator("summary").filter({ hasText: /生成详情|任务详情|查看详情|详细信息|失败详情/ }).first();
  if (await details.isVisible()) await details.click();
  else if (!await clickIf(/任务详情|生成详情|查看详情|查看原因|展开详情/)) skipped.push("task-details-control-not-found");
  if (await win.getByTestId("reading-task-status").count()) assert.equal(await win.getByText(longError, { exact: true }).isVisible(), true, "详情展开可查看完整错误");
  await stableShot("02-video-task-details-dark");
  if (await details.isVisible()) await details.click();
  else await clickIf(/收起详情|任务详情|生成详情|隐藏详情/);
  await theme(false);
  await stableShot("03-video-ai-research-failed-light");
  await win.setViewportSize({ width: 1120, height: 850 });
  const frameSize = await frame.locator("body").evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth }));
  assert(frameSize.scrollWidth <= frameSize.width + 2, "AI 正文不得横向溢出");
  await stableShot("04-video-ai-narrow-light");
  await theme(true);
  await stableShot("05-video-ai-narrow-dark");
  if (await clickIf("展开")) {
    await stableShot("05a-video-ai-narrow-sidebar-open-dark");
    await control("收起").click();
  }
  await win.setViewportSize({ width: 1680, height: 1080 });
  await control("原文").click();
  await win.locator('iframe[aria-label="AI 重构阅读"]').waitFor({ state: "detached" });
  assert.equal(await win.getByTestId("reading-task-status").count(), 0, "原文阅读不应显示 AI 任务错误");
  await stableShot("06-video-original-dark");
  if (await clickIf("目录")) {
    await stableShot("07-video-original-catalog");
    await control("目录").click();
  }
  if (await clickIf(/文字稿/)) {
    await stableShot("07a-video-transcript-dark");
    await control("正文").click();
  }
  if (!await clickIf(/编辑原文|编辑正文/)) {
    await clickIf(/阅读设置|阅读操作|更多阅读操作/);
    await control(/编辑原文|编辑正文/).click();
  }
  await win.locator(".cm-content").waitFor();
  await stableShot("08-video-editing-dark");
  await win.locator(".cm-content").click();
  await win.keyboard.press("Control+End");
  await win.keyboard.insertText("\n\n隔离验收：新保存的阅读判断。");
  await control(/完成编辑|保存修改/).click();
  const saved = await win.evaluate(async id => window.api.knowledge.get(id), rows[0].id);
  assert(saved.content.includes("隔离验收：新保存的阅读判断。"));
  assert(saved.content.includes("> 平台：演示视频"), "编辑必须保留视频来源元数据");
  await win.keyboard.press("Control+f");
  const search = win.locator('input[type="search"]').last();
  await search.fill("隔离验收：新保存的阅读判断。");
  await win.getByTestId("reconstruction-reader").locator("mark").first().waitFor();
  await stableShot("08a-video-original-find-saved-text");
  await search.press("Escape");
  await list.getByText(rows[1].title, { exact: true }).click();
  if (await control("讨论总结").isVisible()) await control("讨论总结").click();
  await frame.locator("h1").waitFor();
  await stableShot("09-discussion-summary-dark");
  await theme(false);
  await stableShot("10-discussion-summary-light");
  await control("原文").click();
  await stableShot("11-discussion-original-light");
  await list.getByText(rows[2].title, { exact: true }).click();
  await win.getByTestId("reconstruction-reader").waitFor();
  await stableShot("12-note-original-light");
  await theme(true);
  await win.setViewportSize({ width: 1120, height: 850 });
  await stableShot("13-note-original-narrow-dark");
  await win.setViewportSize({ width: 1680, height: 1080 });
  await app.evaluate(() => { globalThis.__articleReaderFixture.task = null; });
  await win.reload();
  await list.getByText(rows[0].title, { exact: true }).click();
  await frame.locator("h1").waitFor();
  assert.equal(await win.getByTestId("reading-task-status").count(), 0);
  await stableShot("14-ai-ready-no-task-dark");
  await app.evaluate(() => {
    const state = globalThis.__articleReaderFixture;
    state.pages = state.basePages.filter(f => f.page.itemId !== state.rows[0].id);
    state.task = { ...state.baseTask, state: "running", stage: "research", error: undefined };
  });
  await win.reload();
  await list.getByText(rows[0].title, { exact: true }).click();
  await control("AI 阅读").click();
  await win.getByTestId("reading-task-status").waitFor();
  assert.equal(await win.getByText("当前显示上次生成的版本。", { exact: true }).count(), 0, "无成品时不应声称正在显示上次版本");
  await stableShot("15-ai-first-generation-running-dark");
  await app.evaluate(({ BrowserWindow }) => {
    const state = globalThis.__articleReaderFixture;
    state.task = { ...state.baseTask };
    for (const win of BrowserWindow.getAllWindows()) win.webContents.send("themedReading:progress", state.task);
  });
  await win.getByText("资料查证尚未完成", { exact: true }).waitFor();
  await stableShot("16-ai-no-page-research-failed-dark");
  await app.evaluate(({ ipcMain }) => {
    const state = globalThis.__articleReaderFixture;
    state.pages = state.basePages;
    state.task = { ...state.baseTask };
    ipcMain.removeHandler("knowledge:get");
    ipcMain.handle("knowledge:get", (_event, id) => {
      const item = state.rows.find(row => row.id === id);
      return item && { ...item, deletedAt: id === state.rows[0].id ? Date.now() : null };
    });
  });
  await win.reload();
  await list.getByText(rows[0].title, { exact: true }).click();
  await frame.locator("h1").waitFor();
  assert.equal(await control("编辑原文").isVisible(), false);
  await stableShot("17-ai-trashed-readonly-dark");
  const calls = await app.evaluate(() => globalThis.__articleReaderFixture.calls);
  assert.equal(calls.length, 0, "阅读和编辑验收不应发起生成/搜索/导出请求");
  await fs.mkdir(outDir, { recursive: true });
  await fs.writeFile(path.join(outDir, "evidence.json"), JSON.stringify({ success: true, fixtureOnly: true, isolatedApp: true, realModelOrSearchCalls: 0, verifiedAt: new Date().toISOString(), userDataDir, mainEntry, cases, skipped, frameSize, titleGeometry, tabGeometry, editingPreservedSourceMetadata: true }, null, 2));
};
