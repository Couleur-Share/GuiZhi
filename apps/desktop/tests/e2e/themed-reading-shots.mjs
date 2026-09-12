/** 先运行 pnpm exec tsx scripts/themed-reading-fixture.ts，再由 pnpm shot 加载此步骤。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

export default async ({ win, app, shot }) => {
  const stableShot = async (name) => { await win.mouse.move(2, 2); await win.waitForTimeout(180); return shot(name); };
  const fixture = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/ui-fixture.json"), "utf8"));
  const item = await win.evaluate(async ({ title, content }) => {
    const saved = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(saved.state, { language: "zh", themeMode: "dark", isDarkMode: true, editorMarkdownPreview: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(saved));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
    return window.api.knowledge.create({ title, content, itemType: "note" });
  }, fixture.page.source);
  await app.evaluate(({ ipcMain, BrowserWindow }, { fixture, item }) => {
    const state = { mode: "empty", task: null, fixture, item };
    globalThis.__themeShot = state;
    for (const name of ["get", "generate", "cancel", "resume", "listTasks"]) ipcMain.removeHandler(`themedReading:${name}`);
    const send = () => { for (const window of BrowserWindow.getAllWindows()) window.webContents.send("themedReading:progress", state.task); };
    ipcMain.handle("themedReading:get", (_event, input) => ({ success: true, models: { text: "fixture-text-model", image: "fixture-image-model" }, task: state.task, previous: state.mode === "ready", stale: state.mode === "ready", page: state.mode === "ready" ? { ...fixture.page, itemId: item.id } : null, document: state.mode === "ready" ? fixture.document.replace('data-instance="fixture-instance"', `data-instance="${input.instanceId}"`) : undefined }));
    ipcMain.handle("themedReading:generate", (_event, input) => {
      state.mode = "running";
      state.task = { id: "fixture-task", itemId: item.id, sourceKind: input.sourceKind, versionId: fixture.page.id, title: item.title, state: "running", stage: "images", completed: 1, total: 3, plannedImages: 2, usage: { textCalls: 1, imageCalls: 1, imagesSaved: 0 }, createdAt: Date.now(), updatedAt: Date.now() };
      send(); return { success: true, task: state.task };
    });
    ipcMain.handle("themedReading:listTasks", () => ({ success: true, tasks: state.task ? [state.task] : [] }));
    ipcMain.handle("themedReading:cancel", () => { state.task.state = "cancelled"; send(); return { success: true, task: state.task }; });
    ipcMain.handle("themedReading:resume", () => { state.task.state = "running"; send(); return { success: true, task: state.task }; });
  }, { fixture, item });
  await win.reload();
  await win.getByTestId("item-list").getByText(item.title, { exact: true }).click();
  await win.getByRole("button", { name: "AI 主题页", exact: true }).click();
  await win.getByRole("dialog").waitFor();
  await stableShot("theme-setup-dark");
  await win.getByRole("dialog").getByRole("button", { name: "取消", exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "detached" });
  await win.getByText("为这篇内容设计一个阅读页").waitFor();
  await stableShot("theme-empty-dark");
  await win.getByRole("button", { name: "开始设计", exact: true }).click();
  await win.getByRole("dialog").waitFor();
  await win.getByRole("dialog").getByRole("button", { name: "生成主题页", exact: true }).click();
  await win.getByRole("dialog").waitFor({ state: "detached" });
  await win.getByRole("button", { name: "停止生成", exact: true }).waitFor();
  await stableShot("theme-running-dark");
  await app.evaluate(({ BrowserWindow }) => {
    const state = globalThis.__themeShot;
    state.mode = "ready";
    Object.assign(state.task, { state: "partial", stage: "done", completed: 2, usage: { textCalls: 1, imageCalls: 2, imagesSaved: 1 }, error: "1 张主题图片未完成，可单独重试。", updatedAt: Date.now() });
    for (const window of BrowserWindow.getAllWindows()) window.webContents.send("themedReading:progress", state.task);
  });
  await win.getByTestId("themed-reading-frame").waitFor();
  const frame = win.frameLocator('iframe[aria-label="AI 主题阅读页"]');
  await frame.locator("[data-source-block]").first().waitFor();
  assert((await frame.locator("body").textContent()).includes("啤酒标签的三个维度"));
  assert.equal(await win.locator('iframe[aria-label="AI 主题阅读页"]').getAttribute("sandbox"), "allow-scripts");
  await stableShot("theme-ready-partial-dark");
  await win.getByRole("button", { name: "页面图片", exact: true }).click();
  await stableShot("theme-assets-retry");
  await win.getByRole("button", { name: "页面图片", exact: true }).click();
  await win.evaluate(() => document.documentElement.classList.remove("dark"));
  await win.waitForTimeout(100);
  await stableShot("theme-ready-light");
  await win.setViewportSize({ width: 1120, height: 850 });
  await stableShot("theme-narrow");
  await win.getByRole("button", { name: "处理中心", exact: true }).click();
  await win.getByRole("button", { name: "主题页任务", exact: true }).click();
  await win.getByText(item.title, { exact: true }).waitFor();
  await stableShot("theme-task-center");
};
