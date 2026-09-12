import { readingSearchStatus, configureReadingSearch } from "../services/themed-reading/search-service";
import fs from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { BrowserWindow, dialog, ipcMain } from "electron";
import type Database from "../database/sqlite";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { ThemedReadingRequest, ThemedReadingResult, ThemedReadingOptions } from "@guizhi/shared/types";
import { ThemedReadingRuntime, setThemedReadingRuntime, describeThemeError } from "../services/themed-reading/runtime";
import { themedReadingContent, themedReadingFingerprint } from "../services/themed-reading/content";
import { themedReadingDocument } from "../services/themed-reading/document";
import { assertThemedReadingImagesHealthy, inspectThemedReadingImages } from "../services/themed-reading/image-health";
import { exportThemedReadingHtml } from "../services/themed-reading/export";
import { resolveMediaSummaryConfig } from "../services/media/media-summary";
import { resolveImageGenConfig } from "../services/illustration/image-gen";
import { cleanupOrphanAssets } from "../services/asset-cleanup";
import { logAppError } from "../diagnostic-log";
import { readingPreviewVersion } from "../services/themed-reading/v3-pipeline";
import { createReadingView, updateReadingView, commandReadingView, destroyReadingView, closeReadingViews, probeReadingPage } from "../services/themed-reading/v3-views";

export function parseThemedRequest(value: unknown): ThemedReadingRequest {
  const input = value as Partial<ThemedReadingRequest>;
  if (!input || typeof input.itemId !== "string" || !input.itemId.trim() || input.itemId.length > 200 || !["body", "summary"].includes(input.sourceKind)) throw new Error("主题排版条目或内容来源无效");
  return { itemId: input.itemId, sourceKind: input.sourceKind };
}

export function parseThemedOptions(value: unknown): ThemedReadingOptions {
  const input = value as Partial<ThemedReadingOptions>;
  if (!input || typeof input.style !== "string" || input.style.length > 2000 || typeof input.generateImages !== "boolean" || !Number.isInteger(input.maxImages) || input.maxImages < 0 || input.maxImages > 5) throw new Error("主题排版选项无效（图片上限为 0–5 张）");
  if ((input.research !== undefined && typeof input.research !== "boolean") || (input.action !== undefined && !["create", "revise", "redesign", "refresh"].includes(input.action))) throw new Error("重构选项无效");
  if ((input.enhancedInteraction !== undefined && typeof input.enhancedInteraction !== "boolean") || (input.researchDepth !== undefined && !["standard","deep"].includes(input.researchDepth))) throw new Error("增强交互或查证深度无效");
  return { enhancedInteraction: input.enhancedInteraction ?? true, researchDepth: input.researchDepth ?? "standard", research: input.research ?? false, action: input.action ?? "create", style: input.style.trim(), generateImages: input.generateImages, maxImages: input.maxImages, fromCurrent: input.fromCurrent === true };
}

export function registerThemedReadingIPC(db: Database.Database) {
  closeReadingViews();
  const runtime = new ThemedReadingRuntime(db, task => {
    for (const win of BrowserWindow.getAllWindows()) if (!win.isDestroyed()) win.webContents.send(IPC_CHANNELS.THEMED_READING_PROGRESS, task);
  }, probeReadingPage);
  setThemedReadingRuntime(runtime);
  const wrap = (action: string, fn: (...args: any[]) => Promise<ThemedReadingResult> | ThemedReadingResult) =>
    async (...args: any[]): Promise<ThemedReadingResult> => {
      try { return await fn(...args); }
      catch (error) {
        const message = describeThemeError(error);
        logAppError({ scope: "themedReading", action, message });
        return { success: false, error: message };
      }
    };
  const owner = (event: Electron.IpcMainInvokeEvent) => {
    const win=BrowserWindow.fromWebContents(event.sender);
    if(!win||win.webContents!==event.sender||event.senderFrame!==event.sender.mainFrame)throw new Error("阅读视图只允许应用主界面访问");return event.sender;
  };
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_PREVIEW,wrap("preview",(event,raw)=>{
    owner(event);const input=parseThemedRequest(raw),task=runtime.pages.getTask(String(raw.taskId));
    if(!task||task.itemId!==input.itemId||task.sourceKind!==input.sourceKind)throw new Error("生成任务不属于当前文章");
    const version=runtime.pages.getVersion(task.versionId),page=version&&readingPreviewVersion(version);
    return {success:true,...(page?{preview:{page,revision:page.generation.revision}}:{})};
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_VIEW_CREATE,wrap("viewCreate",async(event,raw)=>{
    const sender=owner(event),input=parseThemedRequest(raw),stored=runtime.pages.getVersion(String(raw.versionId));
    if(!stored||stored.itemId!==input.itemId||stored.sourceKind!==input.sourceKind||stored.formatVersion!==3)throw new Error("阅读版本不存在或不属于当前文章");
    if(stored.role==="working"&&!raw.preview)throw new Error("未发布页面只能静态预览");
    const page=raw.preview?readingPreviewVersion(stored):stored;if(!page)throw new Error("尚无可预览正文");
    return {success:true,viewId:await createReadingView(sender,await inspectThemedReadingImages(page),raw.preview===true,raw.scriptsEnabled!==false)};
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_VIEW_UPDATE,wrap("viewUpdate",(event,raw)=>{updateReadingView(owner(event),String(raw.viewId),raw.bounds);return {success:true};}));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_VIEW_COMMAND,wrap("viewCommand",async(event,raw)=>{await commandReadingView(owner(event),String(raw.viewId),raw.command);return {success:true};}));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_VIEW_DESTROY,wrap("viewDestroy",(event,id)=>{destroyReadingView(owner(event),String(id));return {success:true};}));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_SEARCH_CONFIG, wrap("searchConfig", async (_event, raw) => {
    return { success: true, search: await configureReadingSearch(raw) };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_OFFLINE, wrap("continueOffline", async (_event, id) => ({ success: true, task: await runtime.continueOffline(String(id)) })));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_REFERENCES, wrap("references", (_event, raw) => {
    const input = parseThemedRequest(raw), page = runtime.pages.get(input.itemId, input.sourceKind);
    const used = new Set(page?.reconstruction?.draft.flatMap(s => s.referenceIds) ?? []);
    return { success: true, references: (page?.reconstruction?.references ?? []).filter(r => r.status === "ready" && used.has(r.id)).map(({text: _text, ...r}) => r) };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_STATE, wrap("state", (_event, raw) => {
    const input = parseThemedRequest(raw), item = runtime.items.get(input.itemId);
    if (!item) throw new Error("条目不存在");
    const page = runtime.pages.get(input.itemId, input.sourceKind);
    const task = runtime.pages.listTasks().filter(t => t.itemId === input.itemId && t.sourceKind === input.sourceKind).sort((a,b) => b.createdAt - a.createdAt)[0];
    return { success: true, state: { hasPage: Boolean(page), versionId: page?.id, formatVersion: page?.formatVersion, task, stale: Boolean(page && page.source.fingerprint !== themedReadingFingerprint(item.title, themedReadingContent(item,input.sourceKind),item.sourceUri ?? null,input.sourceKind)) } };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_GET, wrap("get", async (_event, raw) => {
    const input = parseThemedRequest(raw), item = runtime.items.get(input.itemId);
    if (!item) throw new Error("条目不存在");
    const storedPage = runtime.pages.get(input.itemId, input.sourceKind);
    const page = storedPage ? (raw.metadataOnly === true ? storedPage : await inspectThemedReadingImages(storedPage)) : null;
    const instanceId = typeof raw.instanceId === "string" && /^[\w-]{1,100}$/.test(raw.instanceId) ? raw.instanceId : undefined;
    const content = themedReadingContent(item, input.sourceKind);
    const task = runtime.pages.listTasks().filter(t => t.itemId === input.itemId && t.sourceKind === input.sourceKind)
      .sort((a, b) => b.createdAt - a.createdAt)[0];
    const search = await readingSearchStatus().catch(error => { logAppError({scope:"themedReading",action:"searchStatus",message:describeThemeError(error)}); return undefined; });
    return { success: true, search, page, previous: Boolean(runtime.pages.get(input.itemId, input.sourceKind, "previous")), task,
      stale: Boolean(page && page.source.fingerprint !== themedReadingFingerprint(item.title, content, item.sourceUri ?? null, input.sourceKind)),
        document: page && page.formatVersion !== 3 && raw.metadataOnly !== true ? themedReadingDocument(page, instanceId) : undefined,
      models: { text: resolveMediaSummaryConfig()?.model ?? null, image: resolveImageGenConfig()?.model ?? null } };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_GENERATE, wrap("generate", async (_event, raw) => ({ success: true, task: await runtime.generate({ ...parseThemedRequest(raw), options: parseThemedOptions(raw.options) }) })));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_CANCEL, wrap("cancel", (_event, id) => ({ success: true, task: runtime.cancel(String(id)) })));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_RESUME, wrap("resume", async (_event, id) => ({ success: true, task: await runtime.resume(String(id)) })));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_REGENERATE_ASSET, wrap("asset", async (_event, raw) => {
    const input = parseThemedRequest(raw), page = runtime.pages.get(input.itemId, input.sourceKind);
    if (!page || typeof raw.assetId !== "string") throw new Error("主题页或图片不存在");
    return { success: true, task: await runtime.generate({ ...input, options: { ...page.options, fromCurrent: true, generateImages: false } }, raw.assetId) };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_LIST_TASKS, wrap("listTasks", () => ({ success: true, tasks: runtime.pages.listTasks() })));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_RESTORE_PREVIOUS, wrap("restorePrevious", async (_event, raw) => {
    const input = parseThemedRequest(raw), page = runtime.pages.get(input.itemId, input.sourceKind, "previous");
    if (!page) throw new Error("没有上一可用版本");
    themedReadingDocument(page);
    await assertThemedReadingImagesHealthy(page, true);
    if (runtime.pages.get(input.itemId, input.sourceKind, "previous")?.id !== page.id) throw new Error("上一版已变化，请刷新后重试");
    return { success: true, page: runtime.pages.restorePrevious(input.itemId, input.sourceKind) };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_REMOVE, wrap("remove", async (_event, raw) => {
    const input = parseThemedRequest(raw), item = runtime.items.get(input.itemId);
    if (!item || item.deletedAt) throw new Error("回收站中的主题页只读，请先恢复条目");
    for (const task of runtime.pages.listTasks()) if (task.itemId === input.itemId && task.sourceKind === input.sourceKind && ["queued", "running"].includes(task.state)) runtime.cancel(task.id);
    const files = runtime.pages.remove(input.itemId, input.sourceKind); cleanupOrphanAssets(runtime.items, files);
    return { success: true };
  }));
  ipcMain.handle(IPC_CHANNELS.THEMED_READING_EXPORT, wrap("export", async (event, raw) => {
    const input = parseThemedRequest(raw), page = raw.versionId ? runtime.pages.getVersion(String(raw.versionId)) : runtime.pages.get(input.itemId, input.sourceKind);
    if (!page) throw new Error("还没有可导出的主题页");
    if(page.itemId!==input.itemId||page.sourceKind!==input.sourceKind||page.role==="working")throw new Error("导出版本不属于当前文章或尚未发布");
    const html = await exportThemedReadingHtml(page, raw.withoutImages === true, raw.staticOnly === true);
    const win = BrowserWindow.fromWebContents(event.sender);
    const options = { title: "导出主题阅读页", defaultPath: `${page.source.title.replace(/[<>:"/\\|?*\u0000-\u001f]/g, "_").slice(0, 100) || "主题阅读页"}.html`, filters: [{ name: "HTML", extensions: ["html"] }] };
    const result = win ? await dialog.showSaveDialog(win, options) : await dialog.showSaveDialog(options);
    if (result.canceled || !result.filePath) return { success: true, cancelled: true };
    const staged = `${result.filePath}.${randomUUID()}.tmp`;
    try { await fs.writeFile(staged, html, { flag: "wx" }); await fs.rename(staged, result.filePath); }
    finally { await fs.rm(staged, { force: true }); }
    return { success: true, path: result.filePath };
  }));
}
