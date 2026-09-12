import { BrowserWindow, ipcMain } from "electron";
import { randomUUID, createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import libraries from "virtual:reading-libraries";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { ReadingVisual } from "@guizhi/shared/types/reading-visuals";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { normalizeReadingGraphic } from "./visual-normalize";
import { validateReadingVisuals } from "@guizhi/shared/utils/reading-visuals";

export const READING_COMPILER_VERSION = "mermaid-11.17.2_echarts-6.1.0_adapter-1";
export const readingVisualHash = (v: ReadingVisual) => createHash("sha256").update(JSON.stringify(v)).digest("hex");
export async function renderReadingGraphic(visual: ReadingVisual, signal: AbortSignal): Promise<string> {
  signal.throwIfAborted();
  const folder = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-graphics-"));
  let win: BrowserWindow;
  try {
    const script = libraries.compiler.replace(/<\/script/gi, "<\\/script");
    const hash = createHash("sha256").update(script).digest("base64");
    const file = path.join(folder, "compiler.html");
    await fs.writeFile(file, `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none';script-src 'sha256-${hash}';style-src 'unsafe-inline';connect-src 'none';img-src 'none';font-src 'none'"><body><script>${script}</script>`);
    signal.throwIfAborted();
    win = new BrowserWindow({ show: false, width: 1000, height: 800, webPreferences: { partition: `reading-graphics-${randomUUID()}`, sandbox: true, contextIsolation: true, nodeIntegration: false, preload: path.join(__dirname, "../reading-graphics-preload/reading-graphics.js") } });
    const wc = win.webContents, url = pathToFileURL(file).href;
    wc.session.webRequest.onBeforeRequest((details, callback) => callback({ cancel: details.url !== url }));
    wc.session.setPermissionRequestHandler((_web, _permission, callback) => callback(false));
    wc.session.setPermissionCheckHandler(() => false);
    wc.session.on("will-download", event => event.preventDefault());
    wc.setWindowOpenHandler(() => ({ action: "deny" }));
    wc.on("will-navigate", event => event.preventDefault());
    wc.on("will-redirect", event => event.preventDefault());
    return await new Promise<string>((resolve, reject) => {
      const id = randomUUID();
      const finish = (error?: Error, svg?: string) => { clearTimeout(timer); signal.removeEventListener("abort", abort); ipcMain.removeListener(IPC_CHANNELS.READING_GRAPHICS_RESULT, receive); rejectOrResolve(error, svg); };
      let done = false;
      const rejectOrResolve = (error?: Error, svg?: string) => { if (done) return; done = true; if (error) reject(error); else resolve(svg); };
      const abort = () => finish(new Error("图形编译已取消"));
      const timer = setTimeout(() => finish(new Error("图形编译超过十秒")), 10000);
      const receive = (event: Electron.IpcMainEvent, result: { id?: string; svg?: string; error?: string }) => {
        if (event.sender !== wc || event.senderFrame?.routingId !== wc.mainFrame.routingId || event.senderFrame?.processId !== wc.mainFrame.processId || result?.id !== id) return;
        if (typeof result.svg !== "string" || result.svg.length > 2000000) finish(new Error(typeof result.error === "string" ? result.error.slice(0, 1000) : "图形编译结果无效"));
        else finish(undefined, result.svg);
      };
      ipcMain.on(IPC_CHANNELS.READING_GRAPHICS_RESULT, receive);
      signal.addEventListener("abort", abort, { once: true });
      wc.once("preload-error", (_event, _file, error) => finish(new Error(`图形编译桥接加载失败：${error.message}`)));
      wc.on("console-message", (_event, level, message) => { if (level >= 3) finish(new Error(`图形编译脚本失败：${message.slice(0, 700)}`)); });
      wc.once("render-process-gone", () => finish(new Error("图形编译进程意外退出")));
      wc.loadURL(url).then(() => { if (!done) wc.send(IPC_CHANNELS.READING_GRAPHICS_JOB, { id, visual }); }).catch(error => finish(error));
    });
  } finally {
    if (win && !win.isDestroyed()) win.destroy();
    // 只清理本次创建的临时目录，不接受外部传入的删除路径。
    if (path.dirname(folder) === path.resolve(os.tmpdir()) && path.basename(folder).startsWith("guizhi-graphics-")) await fs.rm(folder, { recursive: true, force: true });
  }
}
export async function compileReadingVisuals(version: ThemedReadingVersion, signal: AbortSignal, checkpoint: () => void, progress: (done: number, total: number) => void, render: typeof renderReadingGraphic = renderReadingGraphic) {
  if (version.reconstruction) validateReadingVisuals(version.reconstruction.visuals, version.reconstruction.animations, version.reconstruction.draft);
  const visuals = (version.reconstruction?.visuals ?? []).filter(v => v.kind !== "svg");
  if (!visuals.length) return;
  version.design.visualResults ??= [];
  const results = version.design.visualResults;
  progress(0, visuals.length);
  const deadline = AbortSignal.any([signal, AbortSignal.timeout(60000)]);
  for (const [index, visual] of visuals.entries()) {
    signal.throwIfAborted();
    const sourceHash = readingVisualHash(visual);
    const old = results.find(r => r.id === visual.id);
    if (old?.status === "ready" && old.sourceHash === sourceHash) { progress(index + 1, visuals.length); continue; }
    const result = { id: visual.id, sourceHash, compilerVersion: READING_COMPILER_VERSION, status: "ready" as "ready" | "failed", svg: "", css: "", error: undefined as string | undefined };
    try { const raw = await render(visual, deadline); signal.throwIfAborted(); Object.assign(result, normalizeReadingGraphic(raw, "", visual.id)); }
    catch (error) { signal.throwIfAborted(); result.status = "failed"; result.error = String(error instanceof Error ? error.message : error).slice(0, 1000); }
    if (old) results.splice(results.indexOf(old), 1, result); else results.push(result);
    checkpoint(); progress(index + 1, visuals.length);
  }
}
