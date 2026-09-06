import { compareResearch } from "@guizhi/shared/utils/research-comparison";
import { collectWebResearch, readWebResearch } from "../web-capture/web-research";
import { randomUUID } from "node:crypto";
import type { ResearchPlan, ResearchDocument, ResearchLocalEvidence } from "@guizhi/shared/types";
import { fallbackResearchPlan } from "./research-ai";
import type { ResearchReader } from "./read-research";
import { withinResearchBudget } from "./budget";
import { collectResearchSource } from "./collect-research";
import { createEvidenceSnapshot, validateReport, renderCompleteReport } from "./report-evidence";
import { RESEARCH_POLICY, researchEligibility, selectResearchEvidence } from "@guizhi/shared/utils/research-policy";
import type Database from "../../database/sqlite";
import { KnowledgeItemDB, ResearchDB, ResearchWorkflowDB } from "@guizhi/db";
import type {
  CreateResearchRunInput,
  EnqueueImportInput,
  ImportTask,
  ResearchEvidencePacket,
  ResearchRun,
  ResearchRunDetail,
  ResearchSource,
} from "@guizhi/shared/types";
import { analyzeResearchCandidates } from "@guizhi/shared/utils/research-analysis";
import {
  getBrowserCaptureService,
} from "../platform-capture/browser-capture";
import { readNetworkProxySetting } from "../import/import-service";
import {
  BilibiliResearchCollector,
  BrowserResearchCollector,
  type ResearchCollector,
} from "./collectors";

const DAY_MS = 24 * 60 * 60_000;

export interface ResearchServiceOptions {
  onChanged?: (detail: ResearchRunDetail) => void;
  enqueueImports: (inputs: EnqueueImportInput[]) => ImportTask[];
  plan?: (topic: string, signal: AbortSignal) => Promise<ResearchPlan>;
  read?: ResearchReader;
  report?: (packet: ResearchEvidencePacket, signal: AbortSignal) => Promise<string>;
  localEvidence?: (run: ResearchRun, signal: AbortSignal) => Promise<ResearchLocalEvidence[]>;
  collectors?: Partial<Record<ResearchSource, ResearchCollector>>;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export class ResearchService {
  readonly store: ResearchDB;
  readonly workflow: ResearchWorkflowDB;
  private readonly items: KnowledgeItemDB;
  private readonly readerTasks = new Set<Promise<ResearchDocument>>();
  private read(candidate: Parameters<ResearchReader>[0], signal: AbortSignal): Promise<ResearchDocument> {
    if (candidate.source === "web") return Promise.resolve(readWebResearch(this.db,candidate));
    const task = this.options.read!(candidate, signal, { includeComments: this.workflow.context(candidate.runId)?.includeComments === true }); this.readerTasks.add(task);
    void task.finally(() => this.readerTasks.delete(task)).catch(() => undefined);
    return task;
  }
  private readonly tasks = new Set<Promise<unknown>>();
  private track(task: Promise<unknown>): void { this.tasks.add(task); void task.finally(() => this.tasks.delete(task)).catch(() => undefined); }
  async shutdown(): Promise<void> {
    for (const id of this.controllers.keys()) this.cancel(id);
    for (const id of this.reportControllers.keys()) this.cancelReport(id);
    await Promise.allSettled([...this.tasks]);
    if (this.readerTasks.size) {
      let timer: ReturnType<typeof setTimeout>;
      try { await Promise.race([Promise.allSettled([...this.readerTasks]), new Promise<void>((resolve) => { timer = setTimeout(resolve, 5000); })]); }
      finally { clearTimeout(timer!); }
    }
    this.workflow.recover();
  }
  private readonly controllers = new Map<string, AbortController>();
  private readonly reportControllers = new Map<string, AbortController>();
  private readonly progress = new Map<string, string>();
  private readonly collectors: Partial<Record<ResearchSource, ResearchCollector>>;

  constructor(
    private readonly db: Database.Database,
    private readonly options: ResearchServiceOptions,
  ) {
    this.store = new ResearchDB(db);
    this.workflow = new ResearchWorkflowDB(db);
    this.items = new KnowledgeItemDB(db);
    const needsBrowser = !options.collectors?.xiaohongshu || !options.collectors?.douyin;
    const browser = needsBrowser
      ? getBrowserCaptureService({ getNetworkProxy: () => readNetworkProxySetting(db) })
      : null;
    this.collectors = {
      xiaohongshu: options.collectors?.xiaohongshu ?? new BrowserResearchCollector("xiaohongshu", browser!),
      douyin: options.collectors?.douyin ?? new BrowserResearchCollector("douyin", browser!),
      bilibili: options.collectors?.bilibili ?? new BilibiliResearchCollector(),
    };
    this.store.recoverInterrupted();
    this.workflow.recover();
  }

  list(): ResearchRun[] {
    return this.store.list().map((run) => ({ ...run, context: this.workflow.context(run.id) }));
  }

  getDetail(id: string): ResearchRunDetail | null {
    this.reconcileImports(id);
    return this.withProgress(this.store.getDetail(id));
  }

  createAndRun(input: CreateResearchRunInput): ResearchRun {
    if (input.knowledgeScope?.kind === "collection" && !this.db.get("SELECT id FROM collections WHERE id=?", input.knowledgeScope.collectionId)) throw new Error("所选知识库不存在");
    const rangeTo = Date.now();
    const rangeFrom = rangeTo - input.dayRange * DAY_MS;
    const run = this.store.create(input, rangeFrom, rangeTo);
    this.workflow.setContext(run.id, { webSeeds:input.webSeeds, seriesId: run.id, phase: "searching", policyVersion: RESEARCH_POLICY.version, reportOutdated: false, knowledgeScope: input.knowledgeScope, includeComments: input.includeComments === true });
    this.track(this.run(run.id).catch((error) => this.finishUnexpected(run.id, error)));
    return run;
  }

  cloneAndRun(id: string, replan = false): ResearchRun {
    const previous = this.store.get(id);
    if (!previous) throw new Error("研究记录不存在");
    const now = Date.now();
    const run = this.store.create({ topic: previous.topic, timeScope:previous.timeScope, webSeeds:previous.context?.webSeeds, dayRange: previous.dayRange, depth: previous.depth, sources: [...previous.sources], knowledgeScope: previous.context?.knowledgeScope }, now - previous.dayRange * DAY_MS, now);
    if (!previous.context) this.workflow.setContext(id, { seriesId: id, phase: "idle", policyVersion: "legacy", reportOutdated: false });
    if (previous.savedItemId) this.workflow.linkSavedReport(id, previous.savedItemId);
    this.workflow.setContext(run.id, { webSeeds:previous.context?.webSeeds, seriesId: previous.context?.seriesId ?? id, phase: "searching", policyVersion: RESEARCH_POLICY.version, reportOutdated: false, knowledgeScope: previous.context?.knowledgeScope, plan: replan ? undefined : previous.context?.plan, includeComments: previous.context?.includeComments === true });
    this.track(this.run(run.id).catch((error) => this.finishUnexpected(run.id, error)));
    return this.store.get(run.id)!;
  }

  baselines(id: string): ResearchRun[] {
    const current = this.store.get(id);
    if (!current) throw new Error("研究记录不存在");
    return this.workflow.series(id).filter((other) => other !== id).map((other) => this.store.get(other)).filter((r): r is ResearchRun => Boolean(r && r.rangeTo <= current.rangeTo && ["ready", "partial"].includes(r.status) && this.store.listCandidates(r.id).length)).sort((a, b) => b.createdAt - a.createdAt);
  }

  compare(id: string, baselineId?: string) {
    const detail = this.store.getDetail(id);
    if (!detail) throw new Error("研究记录不存在");
    const eligible = this.baselines(id);
    const chosen = baselineId ?? detail.run.context?.baselineRunId ?? eligible[0]?.id;
    if (baselineId && !eligible.some((r) => r.id === baselineId)) throw new Error("比较基线必须是同序列已结束且有结果的研究");
    const baseline = eligible.find((r) => r.id === chosen);
    return compareResearch(detail, baseline ? this.store.getDetail(baseline.id) : null);
  }

  setBaseline(id: string, baselineId: string): void {
    this.assertIdle(id);
    this.compare(id, baselineId);
    this.workflow.patchContext(id, { baselineRunId: baselineId, reportOutdated: Boolean(this.store.get(id)?.reportMarkdown) });
    this.emit(id);
  }

  /** 验证和补采由主进程连续执行，离开研究页也不会丢失恢复任务。 */
  verifyAndRetrySource(
    id: string,
    source: "xiaohongshu" | "douyin",
    verify: (topic: string, signal: AbortSignal) => Promise<void>,
  ): ResearchRun {
    const run = this.store.get(id);
    if (!run) throw new Error("研究记录不存在");
    if (!run.sources.includes(source)) throw new Error("该平台不属于本次研究");
    if (run.status === "collecting" || this.controllers.has(id)) throw new Error("本次研究仍在执行，请等待当前操作结束");
    if (run.reportStatus === "generating") throw new Error("请先等待或取消报告生成，再验证并补采平台");
    this.store.resumeRun(id);
    this.track(this.run(id, [source], verify).catch((error) => this.finishUnexpected(id, error)));
    return this.store.get(id)!;
  }

  cancel(id: string): boolean {
    const run = this.store.get(id);
    if (!run || run.status !== "collecting") return false;
    this.controllers.get(id)?.abort();
    const now = Date.now();
    const detail = this.store.getDetail(id);
    for (const source of detail?.sources ?? []) {
      if (source.status === "pending" || source.status === "running") {
        this.store.updateSource(id, source.source, {
          status: "canceled",
          errorCode: "canceled",
          error: "用户已取消",
          finishedAt: now,
        });
      }
    }
    this.store.finishRun(id, "canceled", now);
    this.emit(id);
    return true;
  }

  delete(id: string): boolean {
    this.cancel(id);
    this.cancelReport(id);
    const deleted = this.store.delete(id);
    return deleted;
  }

  beginReport(id: string, local: ResearchLocalEvidence[] = []): ResearchEvidencePacket {
    const packet = this.db.transaction(() => {
      const detail = this.store.getDetail(id);
      if (!detail) throw new Error("研究记录不存在");
      if (detail.run.status === "collecting") throw new Error("研究仍在采集中");
      if (!detail.run.context) this.workflow.setContext(id, { seriesId: id, phase: "idle", policyVersion: RESEARCH_POLICY.version, reportOutdated: false });
      const snapshot = createEvidenceSnapshot(detail, local);
      snapshot.packet.comparison = this.compare(id);
      this.workflow.freeze(snapshot);
      this.store.beginReport(id);
      return snapshot.packet;
    })();
    this.emit(id);
    return packet;
  }

  saveReport(id: string, markdown: string, version: string, snapshotId?: string): ResearchRunDetail {
    const context = this.workflow.context(id);
    const snapshot = this.workflow.snapshots(id).find((s) => s.id === (snapshotId ?? context?.activeReportId));
    if (!snapshot || snapshot.status !== "generating" || context?.activeReportId !== snapshot.id) throw new Error("报告操作已取消或被替代");
    try { validateReport(markdown, snapshot.packet); }
    catch (error) { this.failReport(id, errorMessage(error)); throw error; }
    const complete = renderCompleteReport(markdown, snapshot.packet);
    this.db.transaction(() => {
      this.workflow.putSnapshot({ ...snapshot, status: "ready", markdown: complete });
      this.workflow.patchContext(id, { activeReportId: undefined, savedReportId: snapshot.id, reportOutdated: false });
      this.store.saveReport(id, complete, version);
    })();
    this.emit(id);
    return this.store.getDetail(id)!;
  }

  failReport(id: string, error: string): void {
    const active = this.workflow.context(id)?.activeReportId;
    const snapshot = this.workflow.snapshots(id).find((s) => s.id === active);
    if (snapshot) this.workflow.putSnapshot({ ...snapshot, status: "failed", error });
    this.workflow.patchContext(id, { activeReportId: undefined });
    this.store.failReport(id, error.slice(0, 2000));
    this.emit(id);
  }

  enqueueCandidates(runId: string, candidateIds: string[]): ImportTask[] {
    const candidates = this.store.getCandidates(runId, [...new Set(candidateIds)]);
    if (candidates.length !== new Set(candidateIds).size) throw new Error("候选不存在或不属于本次研究");
    const tasks = this.options.enqueueImports(candidates.map((candidate) => ({
      kind: "url" as const,
      input: candidate.url,
      captureStrategy: candidate.source === "bilibili" ? "standard" as const : "authenticated" as const,
      commentLimit: 0 as const,
    })));
    tasks.forEach((task, index) => this.store.linkImport(candidates[index].id, task.id));
    this.emit(runId);
    return tasks;
  }

  saveReportToKnowledge(id: string): { itemId: string; updated: boolean } {
    const detail = this.store.getDetail(id);
    if (!detail) throw new Error("研究记录不存在");
    if (!detail.run.reportMarkdown) throw new Error("还没有可保存的研究报告");
    const date = new Date(detail.run.rangeTo).toISOString().slice(0, 10);
    const title = `${detail.run.topic}｜${detail.run.timeScope === "all" ? "网页研究" : "近期研究"}（${date}）`;
    const coverage = detail.sources.map((source) => `${source.source}: ${source.status}（${source.collectedCount}）`).join("；");
    const content = [
      detail.run.context?.savedReportId ? `> 研究报告：基于冻结证据快照，包含实际引用摘录；搜索摘要、文案、字幕、评论与本地背景分别标注。` : `> 旧版研究报告：仅基于候选元数据。`,
      `> 来源覆盖：${coverage}`,
      "",
      detail.run.reportMarkdown,
    ].join("\n");
    if (detail.run.savedItemId && this.items.get(detail.run.savedItemId)) {
      this.items.update(detail.run.savedItemId, { title, content, tagNames: ["研究报告"] });
      this.workflow.linkSavedReport(id, detail.run.savedItemId);
      return { itemId: detail.run.savedItemId, updated: true };
    }
    const created = this.items.create({ title, content, itemType: "note", tagNames: ["研究报告"] });
    this.store.setSavedItem(id, created.id);
    this.workflow.linkSavedReport(id, created.id);
    this.emit(id);
    return { itemId: created.id, updated: false };
  }

  private async run(
    id: string,
    sources?: readonly ResearchSource[],
    verify?: (topic: string, signal: AbortSignal) => Promise<void>,
  ): Promise<void> {
    const run = this.store.get(id);
    if (!run) return;
    const controller = new AbortController();
    this.controllers.set(id, controller);
    if (run.depth === "deep" && !this.workflow.context(id)?.plan && this.options.plan) {
      this.workflow.patchContext(id, { phase: "planning" }); this.emit(id);
      let plan: ResearchPlan;
      try { plan = await withinResearchBudget(controller.signal, 30_000, (signal) => this.options.plan!(run.topic, signal)); }
      catch { controller.signal.throwIfAborted(); plan = { ...fallbackResearchPlan(run.topic), fallback: "规划不可用，使用原始查询" }; }
      controller.signal.throwIfAborted();
      this.workflow.patchContext(id, { plan });
    }
    if (!this.workflow.context(id)?.plan) this.workflow.patchContext(id, { plan: fallbackResearchPlan(run.topic) });
    this.workflow.patchContext(id, { phase: "searching" });
    const selected = new Set(sources ?? run.sources);
    const browserSources = (["xiaohongshu", "douyin"] as const).filter((source) => selected.has(source));
    const browserFlow = async () => {
      for (const source of browserSources) {
        if (controller.signal.aborted) break;
        await this.collectSource(run, source, controller.signal, verify);
      }
    };
    // 先启动 HTTP 来源，再启动登录态浏览器组；两者并行，而浏览器组内部仍按序 await。
    // 若反过来在数组字面量里先调用 browserFlow()，它会同步跑到首个 await，
    // B 站甚至还没开始，首个浏览器来源就已经起跑了。
    const tasks: Promise<void>[] = [];
    if (selected.has("bilibili")) tasks.push(this.collectSource(run, "bilibili", controller.signal));
    if (selected.has("web")) tasks.push(collectWebResearch(this.db,run,controller.signal,()=>this.emit(id)));
    tasks.push(browserFlow());
    await Promise.all(tasks);
    const detail = this.store.getDetail(id);
    if (controller.signal.aborted || !detail || detail.run.status === "canceled") {
      this.controllers.delete(id);
      return;
    }
    const analysis = analyzeResearchCandidates(run.topic, run.rangeFrom, run.rangeTo, detail.candidates, run.timeScope);
    this.store.replaceAnalysis(id, analysis.candidates, analysis.clusters);
    if ((run.depth === "deep" || selected.has("web")) && this.options.read) await this.readCandidates(id, controller.signal);
    if (controller.signal.aborted || !this.store.get(id)) { this.controllers.delete(id); return; }
    this.workflow.patchContext(id, { phase: "idle" });
    const succeeded = detail.sources.filter((source) => source.status === "succeeded" || source.status === "partial");
    const hasCandidates = detail.candidates.length > 0;
    const allSucceeded = detail.sources.every((source) => source.status === "succeeded");
    this.store.finishRun(id, allSucceeded ? "ready" : hasCandidates || succeeded.length > 0 ? "partial" : "failed");
    this.controllers.delete(id);
    this.emit(id);
  }

  private async collectSource(
    run: ResearchRun,
    source: ResearchSource,
    signal: AbortSignal,
    verify?: (topic: string, signal: AbortSignal) => Promise<void>,
  ): Promise<void> {
    await collectResearchSource({ run, source, collector: this.collectors[source], store: this.store, workflow: this.workflow, signal, verify,
      changed: () => this.emit(run.id), progress: (message) => {
        const key = run.id + ":" + source;
        if (message) this.progress.set(key, message); else this.progress.delete(key);
        this.emit(run.id);
      },
    });
  }

  private finishUnexpected(id: string, error: unknown): void {
    if (!this.store.get(id)) return;
    this.controllers.delete(id);
    this.workflow.patchContext(id, { phase: "idle" });
    if (this.store.get(id)?.status === "collecting") {
      for (const source of this.store.getDetail(id)!.sources) if (["pending", "running"].includes(source.status)) this.store.updateSource(id, source.source, { status: "failed", error: errorMessage(error), errorCode: "interrupted", finishedAt: Date.now() });
      this.store.finishRun(id, this.store.listCandidates(id).length ? "partial" : "failed");
    }
    this.emit(id);
  }

  private assertIdle(id: string): ResearchRun {
    const run = this.store.get(id);
    if (!run) throw new Error("研究记录不存在");
    if (run.status === "collecting" || this.controllers.has(id) || this.reportControllers.has(id) || run.reportStatus === "generating") throw new Error("研究操作仍在执行");
    return run;
  }

  resume(id: string): ResearchRun {
    this.assertIdle(id);
    if (!this.workflow.context(id)) this.workflow.setContext(id, { seriesId: id, phase: "idle", policyVersion: RESEARCH_POLICY.version, reportOutdated: Boolean(this.store.get(id)?.reportMarkdown) });
    this.store.resumeRun(id);
    this.track(this.run(id).catch((error) => this.finishUnexpected(id, error)));
    return this.store.get(id)!;
  }

  async readCandidates(id: string, signal: AbortSignal, candidateId?: string): Promise<void> {
    if (!this.options.read) return;
    const detail = this.store.getDetail(id)!;
    const completed = new Set((detail.documents ?? []).filter((d) => ["ready", "partial"].includes(d.status)).map((d) => d.candidateId));
    const selected = candidateId ? detail.candidates.filter((c) => c.id === candidateId) : selectResearchEvidence(detail.candidates, detail.run, detail.run.context?.plan, new Map((detail.documents ?? []).filter((d) => d.contentHash).map((d) => [d.candidateId, d.contentHash!])), 6).filter((c) => !completed.has(c.id));
    const deadline = Date.now() + 600_000;
    this.workflow.patchContext(id, { phase: "reading" }); this.emit(id);
    for (const candidate of selected) {
      if (signal.aborted || Date.now() >= deadline) break;
      const old = this.workflow.documents(id).find((d) => d.candidateId === candidate.id);
      const reading: ResearchDocument = old ?? { id: randomUUID(), runId: id, candidateId: candidate.id, source: candidate.source, url: candidate.url, title: candidate.title, author: candidate.author, publishedAt: candidate.publishedAt, capturedAt: Date.now(), status: "reading", passages: [], contentHash: null, truncated: false };
      this.workflow.putDocument({ ...reading, status: "reading" }); this.emit(id);
      let result: ResearchDocument;
      try { result = await withinResearchBudget(signal, Math.min(180_000, deadline - Date.now()), (child) => this.read(candidate, child)); }
      catch (error) { result = { ...reading, status: signal.aborted ? "interrupted" : "failed", error: errorMessage(error) }; }
      if (!this.store.get(id)) return;
      if (!result.passages.length && old?.passages.length) result = { ...old, status: "partial", error: result.error, warning: [old.warning, "本次重试未取得新材料，保留上一次文字"].filter(Boolean).join("；") };
      this.workflow.putDocument({ ...result, id: reading.id, savedItemId: old?.savedItemId, status: signal.aborted ? "interrupted" : result.status });
      if (detail.run.reportMarkdown) this.workflow.patchContext(id, { reportOutdated: true });
      this.emit(id);
    }
  }

  retryReading(id: string, candidateId: string): ResearchRun {
    this.assertIdle(id);
    if (!this.store.getCandidates(id, [candidateId]).length) throw new Error("候选不存在或不属于本次研究");
    this.store.resumeRun(id);
    const controller = new AbortController(); this.controllers.set(id, controller);
    this.track(this.readCandidates(id, controller.signal, candidateId).then(() => {
      if (!controller.signal.aborted && this.store.get(id)) { this.store.finishRun(id, this.store.getDetail(id)!.sources.every((s) => s.status === "succeeded") ? "ready" : "partial"); this.workflow.patchContext(id, { phase: "idle" }); this.emit(id); }
    }).catch((error) => this.finishUnexpected(id, error)).finally(() => { if (this.controllers.get(id) === controller) this.controllers.delete(id); }));
    return this.store.get(id)!;
  }

  generateReport(id: string): ResearchRun {
    this.assertIdle(id);
    createEvidenceSnapshot(this.store.getDetail(id)!); // Validate eligibility before any paid call.
    if (!this.options.report) throw new Error("研究报告服务不可用");
    const controller = new AbortController(); this.reportControllers.set(id, controller);
    this.store.beginReport(id); this.emit(id);
    this.track((async () => {
      const run = this.store.get(id)!;
      const local = run.context?.knowledgeScope && this.options.localEvidence ? await withinResearchBudget(controller.signal, 30_000, (signal) => this.options.localEvidence!(run, signal)) : [];
      controller.signal.throwIfAborted();
      const packet = this.beginReport(id, local);
      const markdown = await withinResearchBudget(controller.signal, 120_000, (signal) => this.options.report!(packet, signal));
      controller.signal.throwIfAborted();
      this.saveReport(id, markdown, "research-report-v2", packet.snapshotId);
    })().catch((error) => {
      if (!controller.signal.aborted && this.reportControllers.get(id) === controller && this.store.get(id)) this.failReport(id, errorMessage(error));
    }).finally(() => { if (this.reportControllers.get(id) === controller) this.reportControllers.delete(id); }));
    return this.store.get(id)!;
  }

  cancelReport(id: string): void {
    this.reportControllers.get(id)?.abort(); this.reportControllers.delete(id);
    const active = this.workflow.context(id)?.activeReportId;
    const snapshot = this.workflow.snapshots(id).find((s) => s.id === active);
    if (snapshot) this.workflow.putSnapshot({ ...snapshot, status: "canceled" });
    this.workflow.patchContext(id, { activeReportId: undefined });
    if (this.store.get(id)?.reportStatus === "generating") { this.store.failReport(id, "报告生成已取消"); this.emit(id); }
  }

  evidence(id: string) {
    const context = this.workflow.context(id);
    return this.workflow.snapshots(id).find((s) => s.id === context?.savedReportId)?.packet ?? null;
  }

  saveExcerpt(id: string, candidateId: string): { itemId: string; updated: boolean } {
    const doc = this.workflow.documents(id).find((d) => d.candidateId === candidateId);
    if (!doc?.passages.length) throw new Error("还没有可保存的研究材料");
    const content = "> 研究摘录，并非完整平台导入。采集时间：" + new Date(doc.capturedAt).toISOString() + "；作者：" + doc.author + "；发布时间：" + (doc.publishedAt == null ? "未知" : new Date(doc.publishedAt).toISOString()) + "\n\n" + doc.passages.map((p) => "[" + p.kind + (p.startMs != null ? " @ " + Math.floor(p.startMs / 1000) + "s" : " #" + (p.position + 1)) + "] " + p.text).join("\n\n") + "\n\n来源：" + doc.url;
    if (doc.savedItemId && this.items.get(doc.savedItemId)) { this.items.update(doc.savedItemId, { content }); return { itemId: doc.savedItemId, updated: true }; }
    const item = this.items.create({ title: doc.title + "｜研究摘录", content, itemType: "note", tagNames: ["研究摘录"] });
    this.workflow.putDocument({ ...doc, savedItemId: item.id }); this.emit(id);
    return { itemId: item.id, updated: false };
  }

  private reconcileImports(runId: string): void {
    this.db.run(
      `UPDATE research_candidates SET
         state=CASE WHEN EXISTS(SELECT 1 FROM import_tasks t WHERE t.id=import_task_id AND t.status='succeeded') THEN 'imported' ELSE state END,
         imported_item_id=COALESCE((SELECT t.result_item_id FROM import_tasks t WHERE t.id=import_task_id AND t.status='succeeded'), imported_item_id)
       WHERE run_id=? AND import_task_id IS NOT NULL`,
      runId,
    );
  }

  private emit(id: string): void {
    const detail = this.withProgress(this.store.getDetail(id));
    if (detail) this.options.onChanged?.(detail);
  }

  private withProgress(detail: ResearchRunDetail | null): ResearchRunDetail | null {
    if (!detail) return null;
    if (detail.run.context) for (const c of detail.candidates) c.eligibility = researchEligibility(c, detail.run, detail.run.context.plan);
    for (const source of detail.sources) {
      if (source.status === "running") source.progress = this.progress.get(`${detail.run.id}:${source.source}`);
    }
    return detail;
  }
}

export const selectEvidence = selectResearchEvidence;
