import { ensureRequestedReadingImage } from "./required-image";
import { compileReadingVisuals } from "./visual-compiler";
import { runReadingV3 } from "./v3-pipeline";
import { repairReadingRuntime } from "./v3-runtime-repair";
import { prepareResearchRetry } from "./research-pipeline";
import { randomUUID } from "node:crypto";
import { parseHTML } from "linkedom";
import type Database from "@guizhi/db/adapter";
import { KnowledgeItemDB } from "@guizhi/db";
import { ThemedReadingDB } from "@guizhi/db/themed-reading";
import type { ThemedReadingOptions, ThemedReadingRequest, ThemedReadingTask, ThemedReadingVersion } from "@guizhi/shared/types";
import { buildThemedReadingSource } from "./content";
import { collectOriginalThemeAssets, generateThemeAsset, prepareOriginalAsset } from "./assets";
import { themedReadingDocument } from "./document";
import { inspectThemedReadingImages } from "./image-health";
import { resolveMediaSummaryConfig } from "../media/media-summary";
import { resolveImageGenConfig } from "../illustration/image-gen";
import { cleanupOrphanAssets } from "../asset-cleanup";
import { logAppError } from "../../diagnostic-log";

export const describeThemeError = (error: unknown) => error instanceof Error ? error.message : String(error);
const activeStates = new Set(["queued", "running"]);
const resumableStates = new Set(["interrupted", "failed", "cancelled", "partial"]);
// 旧格式（v1/v2）页面只读保留：可以阅读、导出和备份恢复，不再生成、补图或续跑。
export const LEGACY_PAGE_ERROR = "旧格式阅读页只读，不能调整、补图或继续生成；请重新生成新版阅读页";
export const LEGACY_TASK_ERROR = "旧格式任务不再支持继续，请重新生成新版阅读页";

export class ThemedReadingRuntime {
  readonly pages: ThemedReadingDB;
  readonly items: KnowledgeItemDB;
  private controller: AbortController | null = null;
  private activeId: string | null = null;
  private pumping: Promise<void> | null = null;
  private preparing = new Set<string>();
  private stopped = false;
  private blocked = false;

  constructor(readonly db: Database.Database, private readonly notify: (task: ThemedReadingTask) => void, private readonly verifyPage?: (page: ThemedReadingVersion) => Promise<string | undefined>) {
    this.pages = new ThemedReadingDB(db); this.items = new KnowledgeItemDB(db);
    this.pages.interruptRunning();
    this.retireLegacyTasks();
  }

  /** 页面数据保持原样；只把旧格式任务标记为不可继续，未完成的工作任务写明失败原因。 */
  private retireLegacyTasks() {
    for (const task of this.pages.listTasks()) {
      if (task.legacyFormat || !resumableStates.has(task.state)) continue;
      try {
        const version = this.pages.getVersion(task.versionId);
        if (!version || version.formatVersion >= 3) continue;
        this.pages.saveTask(version.role === "working" ? { ...task, legacyFormat: true, state: "failed", error: LEGACY_TASK_ERROR } : { ...task, legacyFormat: true });
      } catch (error) {
        // 单条存量记录损坏不能阻止阅读服务启动；继续时会再次报出具体原因。
        logAppError({ scope: "themedReading", action: "retireLegacyTask", itemId: task.itemId, message: describeThemeError(error) });
      }
    }
  }

  hasActive(): boolean { return this.preparing.size > 0 || this.pages.hasActiveTasks() || this.activeId !== null; }
  block(): () => void {
    if (this.hasActive()) throw new Error("主题排版正在执行，请先停止任务后再操作数据目录或恢复备份");
    this.blocked = true;
    return () => { this.blocked = false; };
  }

  retireIdle(): void {
    this.block();
    this.stopped = true;
  }

  private saveTask(task: ThemedReadingTask) {
    task.updatedAt = Date.now(); this.pages.saveTask(task); this.notify(task);
  }

  private assertCurrent(task: ThemedReadingTask, signal?: AbortSignal) {
    signal?.throwIfAborted();
    const live = this.pages.getTask(task.id), version = this.pages.getVersion(task.versionId), item = this.items.get(task.itemId);
    if (!live || !activeStates.has(live.state) || !version || version.role !== "working" || !item || item.deletedAt) {
      throw new Error("任务已取消、被替代，或条目已删除，结果未写入");
    }
  }

  async generate(input: ThemedReadingRequest & { options: ThemedReadingOptions }, assetId?: string, reuseDesign = false): Promise<ThemedReadingTask> {
    if (this.stopped || this.blocked) throw new Error("数据服务正在切换，请稍后重试");
    const key = `${input.itemId}:${input.sourceKind}`;
    if (this.preparing.has(key) || this.pages.listTasks().some(t => t.itemId === input.itemId && t.sourceKind === input.sourceKind && activeStates.has(t.state))) throw new Error("这份内容已有主题排版任务");
    if (!resolveMediaSummaryConfig() && !assetId && !reuseDesign) throw new Error("请先配置可用的主文本模型");
    if (input.options.generateImages && input.options.maxImages < 1) throw new Error("已开启生成主题图片，请将图片上限设为至少一张");
    if (input.options.generateImages && !resolveImageGenConfig()) throw new Error("未配置可用的生图模型，请关闭配图或前往设置");
    this.preparing.add(key);
    try {
      const item = this.items.get(input.itemId);
      if (!item || item.deletedAt) throw new Error("条目不存在或已移入回收站");
      const current = this.pages.get(input.itemId, input.sourceKind);
      const useCurrent = assetId || reuseDesign || input.options.action === "revise" || input.options.action === "redesign" || (input.options.fromCurrent && input.options.action !== "refresh");
      const existing = useCurrent ? current : null;
      if (useCurrent && !existing) throw new Error("还没有可调整的主题页");
      if (existing && existing.formatVersion < 3) throw new Error(LEGACY_PAGE_ERROR);
      const source = existing ? structuredClone(existing.source) : await buildThemedReadingSource(item, input.sourceKind);
      if (!source.blocks.length) throw new Error("当前内容为空，没有可排版的正文");
      const now = Date.now();
      const version: ThemedReadingVersion = {
        id: randomUUID(), itemId: item.id, sourceKind: input.sourceKind, role: "working", formatVersion: 3, source,
        reconstruction: (assetId || reuseDesign || input.options.action === "redesign" || input.options.action === "revise") && existing ? structuredClone(existing.reconstruction) : { notes: [], queries: [], references: [], draft: [], interactions: [] },
        options: { ...input.options, research: input.options.research ?? false }, design: (assetId || reuseDesign) && existing ? structuredClone(existing.design) : null,
        designDirection: assetId || reuseDesign ? existing?.designDirection : undefined,
        textModel: existing?.textModel, imageModel: existing?.imageModel,
        assets: existing ? structuredClone(existing.assets) : [], warnings: [], createdAt: now, updatedAt: now,
      };
      if (!assetId && !reuseDesign && input.options.action === "revise") { version.reconstruction.revisionDraft = structuredClone(version.reconstruction.draft); version.reconstruction.draft = []; version.reconstruction.outline = undefined; version.reconstruction.queries = []; version.reconstruction.selectedUrls = undefined; version.reconstruction.researchComplete = false; version.reconstruction.researchBatches = undefined; version.reconstruction.researchReview = undefined; version.reconstruction.readUrls = undefined; }
      if (!assetId && !reuseDesign && ["redesign", "revise"].includes(input.options.action)) { version.reconstruction.interactions = []; version.reconstruction.visuals = []; version.reconstruction.animations = []; }
      if (!existing) version.assets = collectOriginalThemeAssets(version);
      if (reuseDesign && existing.design?.scripts?.some(s => s.status === "failed") && existing.generation) { version.generation = structuredClone(existing.generation); version.generation.repairs = {}; version.design = null; for (const s of version.generation.scripts) s.status = "pending"; }
      if (assetId) {
        const asset = version.assets.find(a => a.id === assetId);
        if (!asset) throw new Error("图片槽位不存在");
        if (asset.role === "generated" && !resolveImageGenConfig()) throw new Error("请先配置生图模型");
        asset.status = "pending"; asset.error = undefined;
      }
      if (this.stopped || this.blocked) throw new Error("数据服务正在切换");
      const oldAssets = this.pages.listAssetFiles();
      this.pages.saveVersion(version);
      const task: ThemedReadingTask = { id: randomUUID(), itemId: item.id, sourceKind: input.sourceKind, versionId: version.id,
        reused: { chapters: version.reconstruction.draft.length, notes: version.reconstruction.notes.length, references: version.reconstruction.references.filter(r => r.status === "ready").length, assets: version.assets.filter(a => a.status === "ready").length },
        title: source.title, state: "queued", stage: "prepare", completed: 0, total: 0, assetId,
        usage: { textCalls: 0, imageCalls: 0, imagesSaved: 0, searchCalls: 0, pagesRead: 0 }, createdAt: now, updatedAt: now };
      this.saveTask(task);
      cleanupOrphanAssets(this.items, oldAssets);
      this.kick();
      return task;
    } finally { this.preparing.delete(key); }
  }

  cancel(id: string): ThemedReadingTask {
    const task = this.pages.getTask(id);
    if (!task) throw new Error("任务不存在");
    if (!activeStates.has(task.state)) return task;
    task.state = "cancelled"; task.error = "已停止；已发出的模型请求可能已计费，可手动继续未完成步骤";
    this.saveTask(task);
    if (this.activeId === id) this.controller?.abort(new Error("用户已停止主题排版"));
    return task;
  }

  async continueOffline(id: string): Promise<ThemedReadingTask> {
    const task = this.pages.getTask(id);
    const version = task && this.pages.getVersion(task.versionId);
    if (version && version.formatVersion < 3) throw new Error(LEGACY_TASK_ERROR);
    if (!task || !version || version.role !== "working" || task.stage !== "research" || activeStates.has(task.state)) throw new Error("当前任务不能改为不联网继续");
    version.options.research = false; this.pages.saveVersion(version);
    return this.resume(id);
  }

  async resume(id: string): Promise<ThemedReadingTask> {
    if (this.stopped || this.blocked) throw new Error("数据服务正在切换");
    const task = this.pages.getTask(id);
    if (!task) throw new Error("任务不存在");
    if (activeStates.has(task.state)) return task;
    const version = this.pages.getVersion(task.versionId), item = this.items.get(task.itemId);
    if (!version || !item || item.deletedAt) throw new Error("任务内容已删除，无法继续");
    if (version.formatVersion < 3) throw new Error(version.role === "working" ? LEGACY_TASK_ERROR : LEGACY_PAGE_ERROR);
    if (version.role !== "working") {
      if (version.role !== "current") throw new Error("页面已有新版本，请在当前主题页调整");
      const failed = version.assets.find(a => a.status !== "ready");
      const failedVisual = version.design?.visualResults?.some(r => r.status === "failed");
      const failedScript = version.design?.scripts?.some(s => s.status === "failed");
      if (!failed && !failedVisual && !failedScript) throw new Error("这份页面没有需要继续的素材");
      return this.generate({ itemId: task.itemId, sourceKind: task.sourceKind, options: { ...version.options, fromCurrent: true,
        generateImages: version.assets.some(a => a.role === "generated" && a.status !== "ready") } }, undefined, true);
    }
    if (this.pages.listTasks().some(t => t.id !== id && t.itemId === task.itemId && t.sourceKind === task.sourceKind && activeStates.has(t.state))) throw new Error("已有其他活动任务");
    if (task.stage === "research" && version.options.research) prepareResearchRetry(version.reconstruction);
    if (version.generation && task.stage === "research" && /查证上限/.test(task.error ?? "")) { version.generation.researchRoundLimit = (version.generation.researchRoundLimit ?? (version.options.researchDepth === "deep" ? 2 : 1)) + 1; }
    if (version.generation) { version.generation.repairs = {}; for (const module of version.generation.scripts) if (module.status === "failed") module.status = "pending"; }
    for (const asset of version.assets) if (asset.status === "failed") { asset.status = "pending"; asset.error = undefined; }
    this.pages.saveVersion(version);
    task.state = "queued"; task.error = undefined; this.saveTask(task); this.kick(); return task;
  }

  private kick() {
    if (this.pumping !== null || this.stopped) return;
    this.pumping = this.pump().finally(() => { this.pumping = null; if (!this.stopped && this.pages.listTasks().some(t => t.state === "queued")) this.kick(); });
  }

  private async pump() {
    while (!this.stopped) {
      const task = this.pages.listTasks().filter(t => t.state === "queued").sort((a, b) => a.createdAt - b.createdAt)[0];
      if (!task) return;
      this.activeId = task.id; this.controller = new AbortController();
      try { await this.execute(task, this.controller.signal); }
      catch (error) {
        const live = this.pages.getTask(task.id);
        // 用户可能已将取消的任务重新入队，旧请求的取消回执不能覆盖新一轮。
        if (live && activeStates.has(live.state) && !this.controller.signal.aborted) { live.state = this.stopped ? "interrupted" : "failed"; live.error = describeThemeError(error); this.saveTask(live); }
        if (!this.controller.signal.aborted) logAppError({ scope: "themedReading", action: "generate", itemId: task.itemId, message: describeThemeError(error) });
      } finally { this.activeId = null; this.controller = null; }
    }
  }

  private async execute(task: ThemedReadingTask, signal: AbortSignal) {
    const version = this.pages.getVersion(task.versionId);
    if (!version) throw new Error("工作版本不存在");
    if (version.formatVersion < 3) throw new Error(LEGACY_TASK_ERROR);
    task.state = "running"; this.saveTask(task); this.assertCurrent(task, signal);
    const checkpoint = () => {
      this.assertCurrent(task, signal); version.updatedAt = Date.now();
      if (version.generation) {
        task.issues = version.generation.issues;
        if (version.generation.revision !== task.previewRevision) {
          task.previewRevision = version.generation.revision;
          if(task.reused)task.reused.chapters=Math.max(task.reused.chapters,version.generation.reusedChapters??0);
          if (version.generation.sections.length) task.firstContentAt ??= Date.now();
        }
      }
      const before = this.pages.getVersion(version.id)!;
      const saved = version.assets.filter(asset => asset.role === "generated" && asset.status === "ready" &&
        before.assets.find(previous => previous.id === asset.id)?.status !== "ready").length;
      if (saved && task.usage) {
        const next = { ...task, usage: { ...task.usage, imagesSaved: task.usage.imagesSaved + saved }, updatedAt: Date.now() };
        this.pages.saveVersion(version, next);
        Object.assign(task, next); this.notify(task);
      } else { this.pages.saveVersion(version,task); this.notify(task); }
    };
    const recordRequest = (kind: "textCalls" | "imageCalls" | "searchCalls" | "pagesRead") => {
      this.assertCurrent(task, signal);
      // 旧任务没有历史基数，继续时也不把未知历史伪装成零次调用。
      if (task.usage) {
        const next = { ...task, usage: { ...task.usage, [kind]: (task.usage[kind] ?? 0) + 1 } };
        this.saveTask(next); Object.assign(task, next);
      }
    };
    let stageStarted=Date.now();
    const stage = (value: ThemedReadingTask["stage"], completed = 0, total = 0) => { this.assertCurrent(task, signal); if(task.stage!==value){task.timings??={};task.timings[task.stage]=(task.timings[task.stage]??0)+Date.now()-stageStarted;stageStarted=Date.now();} task.stage = value; task.completed = completed; task.total = total; this.saveTask(task); };
    const text = resolveMediaSummaryConfig();
    if (!version.design) {
      if (!text) throw new Error("主文本模型未配置");
      version.textModel = text.model; stage("design");
      let lastDesignProgress = 0;
      await runReadingV3(version, text, signal, { checkpoint, stage, request: recordRequest, designProgress: (attempt: number, receivedChars: number) => {
        this.assertCurrent(task, signal);
        if (receivedChars && Date.now() - lastDesignProgress < 1000) return;
        lastDesignProgress = Date.now(); task.designProgress = { attempt, receivedChars }; this.saveTask(task);
      } });
      ensureRequestedReadingImage(version);
      const { document } = parseHTML(`<html><body>${version.design.html}</body></html>`);
      const used = new Set([...document.querySelectorAll("img[data-theme-asset]")].map(node => node.getAttribute("data-theme-asset")));
      // 没有进入实际页面的装饰不发起生图，也不计入缺图告警和导出。
      version.assets = version.assets.filter(asset => used.has(asset.id));
      checkpoint();
    }
    if (!task.assetId && version.options.generateImages) { ensureRequestedReadingImage(version); checkpoint(); }
    let imageBudget = task.assetId ? 1 : Math.min(version.options.maxImages, 5);
    const pending = version.assets.filter(a => {
      if (a.status === "ready" || (task.assetId && a.id !== task.assetId)) return false;
      if (a.role === "original") return true;
      return (Boolean(task.assetId) || version.options.generateImages) && imageBudget-- > 0;
    });
    const imageConfig = resolveImageGenConfig();
    task.plannedImages = pending.filter(a => a.role === "generated").length;
    this.saveTask(task);
    if (imageConfig && pending.some(asset => asset.role === "generated")) version.imageModel = imageConfig.model;
    for (const [index, asset] of pending.entries()) {
      stage("images", index, pending.length);
      try {
        asset.error = "请求正在执行；中断后请检查结果，再手动继续"; checkpoint();
        const cleanup = (file: string) => { cleanupOrphanAssets(this.items, [file]); };
        if (asset.role === "original") await prepareOriginalAsset(asset, signal, checkpoint, cleanup);
        else {
          if (!imageConfig) throw new Error("生图模型未配置");
          await generateThemeAsset(asset, version.designDirection ?? version.design.direction, imageConfig, signal, checkpoint, cleanup, () => recordRequest("imageCalls"));
        }
      } catch (error) {
        this.assertCurrent(task, signal);
        asset.status = "failed"; asset.error = describeThemeError(error); checkpoint();
        logAppError({ scope: "themedReading", action: "asset", itemId: task.itemId, message: asset.error });
      }
      stage("images", index + 1, pending.length);
    }
    if (task.assetId && version.assets.find(a => a.id === task.assetId)?.status !== "ready") throw new Error("指定图片未能替换，原页面和图片已保留");
    if (!task.assetId) await compileReadingVisuals(version, signal, checkpoint, (done, total) => stage("assemble", done, total));
    version.assets = (await inspectThemedReadingImages(version)).assets; this.assertCurrent(task, signal);
    version.warnings = [...version.assets.filter(a => a.status !== "ready").map(a => `${a.alt}：${a.error ?? "图片未完成"}`), ...(version.design.visualResults ?? []).filter(r => r.status === "failed").map(r => `图形 ${r.id}：${r.error}`)];
    stage("assemble"); checkpoint();
    stage("validate"); themedReadingDocument(version);
    if (this.verifyPage) await repairReadingRuntime(version, text, signal, this.verifyPage, checkpoint, () => recordRequest("textCalls"));
    this.assertCurrent(task, signal);
    version.warnings.push(...(version.design.scripts ?? []).filter(s => s.status === "failed").map(s => `${s.id}：${s.error ?? "交互未完成"}`));
    checkpoint();
    this.assertCurrent(task, signal);
    const oldAssets = this.pages.listAssetFiles();
    const completed: ThemedReadingTask = {...task,timings:{...task.timings,[task.stage]:(task.timings?.[task.stage]??0)+Date.now()-stageStarted},state:version.warnings.length?"partial":"completed",stage:"done",error:version.warnings.join("；")||undefined,updatedAt:Date.now()};
    this.pages.publish(version.id,completed);
    Object.assign(task,completed);this.notify(task);cleanupOrphanAssets(this.items, oldAssets);
  }

  async shutdown() {
    this.stopped = true;
    for (const task of this.pages.listTasks()) if (activeStates.has(task.state)) { task.state = "interrupted"; task.error = "应用已关闭，手动继续可复用已完成结果"; this.saveTask(task); }
    this.controller?.abort(new Error("应用正在关闭"));
    await this.pumping;
  }
}

let runtime: ThemedReadingRuntime | null = null;
export function setThemedReadingRuntime(value: ThemedReadingRuntime) { runtime = value; }
export function hasActiveThemedReading() { return runtime?.hasActive() ?? false; }
export function cancelThemedReadingForItems(itemIds: string[]) {
  if (!runtime) return;
  const ids = new Set(itemIds);
  for (const task of runtime.pages.listTasks()) if (ids.has(task.itemId) && activeStates.has(task.state)) runtime.cancel(task.id);
}
export function blockThemedReading() { return runtime?.block() ?? (() => undefined); }
/** 同步换库已确认无写任务，关闭数据库前撤销旧 runner，避免重启等待期再入队。 */
export function retireIdleThemedReading() { runtime?.retireIdle(); runtime = null; }
export async function shutdownThemedReading() { await runtime?.shutdown(); runtime = null; }
