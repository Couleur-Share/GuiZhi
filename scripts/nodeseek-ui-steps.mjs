/** pnpm shot 专用：真实公网采集与合成失败任务均使用临时 userData。 */
import fs from "node:fs/promises";
import path from "node:path";
import { testProxySettings } from "./lib/test-proxy.mjs";

export default async function ({ win, app, shot }) {
  await win.evaluate(async (networkProxy) => {
    const stored = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    stored.state.language = "zh";
    localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
    await window.api.settings.set({ language: "zh", networkProxy });
  }, testProxySettings());
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  await app.evaluate(({ app: electronApp }) => {
    globalThis.__nodeseekObservations = [];
    electronApp.on("web-contents-created", (_event, contents) => {
      contents.on("dom-ready", () => {
        if (!contents.getURL().startsWith("https://www.nodeseek.com/")) return;
        void contents.executeJavaScript(`JSON.stringify({title:document.title, heading:document.querySelector('h1')?.textContent, hasChallenge:!!document.querySelector('#challenge-stage, #cf-wrapper, .cf-turnstile'), text:document.body?.innerText?.slice(0,400), scripts:[...document.scripts].map(s=>s.src).filter(Boolean).slice(0,5)})`).then(value => globalThis.__nodeseekObservations.push(JSON.parse(value))).catch(() => {});
      });
    });
  });
  const tasks = await win.evaluate(() => window.api.import.enqueue([{ kind: "url", input: "https://www.nodeseek.com/post-938943-1" }]));
  const id = tasks[0].id;
  let result;
  for (let attempt = 0; attempt < 90; attempt++) {
    result = await win.evaluate(async (taskId) => (await window.api.import.list()).find((task) => task.id === taskId), id);
    if (["completed", "failed", "canceled"].includes(result?.status)) break;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  if (!["completed", "failed"].includes(result?.status)) throw new Error("NodeSeek 公网采集未在 90 秒内结束");
  const evidence = { status: result.status, error: result.error, warning: result.warning, itemType: result.itemType };
  evidence.pages = await app.evaluate(() => globalThis.__nodeseekObservations);
  if (result.resultItemId) {
    const item = await win.evaluate((itemId) => window.api.knowledge.get(itemId), result.resultItemId);
    evidence.title = item.title;
    evidence.characters = item.content.length;
    evidence.replyHeadings = item.content.match(/^### /gm)?.length ?? 0;
  }
  const output = path.resolve("artifacts/nodeseek-validation");
  await fs.mkdir(output, { recursive: true });
  await fs.writeFile(path.join(output, "live-result.json"), JSON.stringify(evidence, null, 2));
  console.log("NodeSeek 公网验证", JSON.stringify(evidence));

  // 受控失败夹具验证旧任务入口，不写入真实用户库。
  await app.evaluate(({ ipcMain }, original) => {
    const task = { ...original, status: "failed", displayName: "Just a moment…", resultItemId: null, sourceInput: "https://www.nodeseek.com/post-938943-1", error: "网站拒绝访问或限制请求频率", captureStrategy: "standard" };
    ipcMain.removeHandler("import:list");
    ipcMain.handle("import:list", (_event, query) => query ? { entries: [task], active: [], total: 1, nextCursor: null, counts: { pending: 0, processing: 0, completed: 0, duplicate: 0, failed: 1, canceled: 0 } } : [task]);
  }, result);
  await win.getByRole("button", { name: "采集", exact: true }).click();
  await win.getByText("Just a moment…", { exact: true }).hover();
  await win.getByRole("button", { name: "任务详情", exact: true }).click();
  await win.getByRole("button", { name: "验证并自动采集", exact: true }).waitFor();
  await shot("nodeseek-verification-action");
}
