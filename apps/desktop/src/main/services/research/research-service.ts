import type Database from "../../database/sqlite";
import { KnowledgeItemDB, ResearchDB } from "@guizhi/db";
import type {
  CreateResearchRunInput,
  EnqueueImportInput,
  ImportTask,
  ResearchCandidate,
  ResearchEvidencePacket,
  ResearchRun,
  ResearchRunDetail,
  ResearchSource,
} from "@guizhi/shared/types";
import { analyzeResearchCandidates } from "@guizhi/shared/utils/research-analysis";
import { normalizeUrl } from "../import/url-normalize";
import {
  getBrowserCaptureService,
  type BrowserCaptureService,
  PlatformCaptureError,
} from "../platform-capture/browser-capture";
import { readNetworkProxySetting } from "../import/import-service";
import {
  BilibiliResearchCollector,
  BrowserResearchCollector,
  type ResearchCollector,
} from "./collectors";

const DAY_MS = 24 * 60 * 60_000;
const PAGE_LIMIT = 20;

export interface ResearchServiceOptions {
  onChanged?: (detail: ResearchRunDetail) => void;
  enqueueImports: (inputs: EnqueueImportInput[]) => ImportTask[];
  collectors?: Partial<Record<ResearchSource, ResearchCollector>>;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function sourceErrorCode(error: unknown): string {
  if (error instanceof PlatformCaptureError) return error.code;
  if (error instanceof DOMException && error.name === "AbortError") return "canceled";
  const bracket = /^\[([^\]]+)]/.exec(errorMessage(error));
  return bracket?.[1] ?? "collector_failed";
}

export class ResearchService {
  readonly store: ResearchDB;
  private readonly items: KnowledgeItemDB;
  private readonly controllers = new Map<string, AbortController>();
  private readonly collectors: Record<ResearchSource, ResearchCollector>;
  private readonly browser: BrowserCaptureService | null;

  constructor(
    private readonly db: Database.Database,
    private readonly options: ResearchServiceOptions,
  ) {
    this.store = new ResearchDB(db);
    this.items = new KnowledgeItemDB(db);
    const needsBrowser = !options.collectors?.xiaohongshu || !options.collectors?.douyin;
    const browser = needsBrowser
      ? getBrowserCaptureService({ getNetworkProxy: () => readNetworkProxySetting(db) })
      : null;
    this.browser = browser;
    this.collectors = {
      xiaohongshu: options.collectors?.xiaohongshu ?? new BrowserResearchCollector("xiaohongshu", browser!),
      douyin: options.collectors?.douyin ?? new BrowserResearchCollector("douyin", browser!),
      bilibili: options.collectors?.bilibili ?? new BilibiliResearchCollector(),
    };
    this.store.recoverInterrupted();
  }

  list(): ResearchRun[] {
    return this.store.list();
  }

  getDetail(id: string): ResearchRunDetail | null {
    this.reconcileImports(id);
    return this.store.getDetail(id);
  }

  createAndRun(input: CreateResearchRunInput): ResearchRun {
    const rangeTo = Date.now();
    const rangeFrom = rangeTo - input.dayRange * DAY_MS;
    const run = this.store.create(input, rangeFrom, rangeTo);
    void this.run(run.id);
    return run;
  }

  cloneAndRun(id: string): ResearchRun {
    const previous = this.store.get(id);
    if (!previous) throw new Error("研究记录不存在");
    return this.createAndRun({
      topic: previous.topic,
      dayRange: previous.dayRange,
      depth: previous.depth,
      sources: [...previous.sources],
    });
  }

  cancel(id: string): boolean {
    const run = this.store.get(id);
    if (!run || run.status !== "collecting") return false;
    this.controllers.get(id)?.abort();
    this.browser?.cancel("discovery");
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
    const deleted = this.store.delete(id);
    if (deleted) this.controllers.delete(id);
    return deleted;
  }

  beginReport(id: string): ResearchEvidencePacket {
    const detail = this.store.getDetail(id);
    if (!detail) throw new Error("研究记录不存在");
    if (detail.run.status === "collecting") throw new Error("研究仍在采集中");
    const items = selectEvidence(detail.candidates).map((candidate, index) => ({
      ref: `R${index + 1}`,
      candidateId: candidate.id,
      source: candidate.source,
      title: candidate.title,
      author: candidate.author,
      snippet: candidate.snippet,
      publishedAt: candidate.publishedAt,
      dateConfidence: candidate.dateConfidence,
      engagement: candidate.engagement,
      overallScore: candidate.overallScore,
      url: candidate.url,
    }));
    this.store.beginReport(id);
    this.emit(id);
    return {
      runId: id,
      topic: detail.run.topic,
      rangeFrom: detail.run.rangeFrom,
      rangeTo: detail.run.rangeTo,
      sourceRuns: detail.sources,
      items,
    };
  }

  saveReport(id: string, markdown: string, version: string): ResearchRunDetail {
    const detail = this.store.getDetail(id);
    if (!detail) throw new Error("研究记录不存在");
    const evidence = selectEvidence(detail.candidates);
    const refs = [...markdown.matchAll(/\[R(\d+)]/g)].map((match) => Number(match[1]));
    if (refs.some((ref) => ref < 1 || ref > evidence.length)) {
      const message = "报告包含不属于本次证据包的引用编号";
      this.store.failReport(id, message);
      this.emit(id);
      throw new Error(message);
    }
    if (evidence.length > 0 && refs.length === 0) {
      const message = "报告没有引用任何有效候选";
      this.store.failReport(id, message);
      this.emit(id);
      throw new Error(message);
    }
    if (/https?:\/\//i.test(markdown)) {
      const message = "报告包含模型自行输出的链接；原始链接必须由证据编号生成";
      this.store.failReport(id, message);
      this.emit(id);
      throw new Error(message);
    }
    const references = [...new Set(refs)].sort((a, b) => a - b).map((ref) => {
      const candidate = evidence[ref - 1];
      return `[R${ref}]: ${candidate.url} "${candidate.title.replace(/"/g, "'")}"`;
    });
    const complete = `${markdown.trim()}\n\n## 引用链接\n\n${references.join("\n")}`;
    this.store.saveReport(id, complete, version);
    this.emit(id);
    return this.store.getDetail(id)!;
  }

  failReport(id: string, error: string): void {
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
    const title = `${detail.run.topic}｜近期研究（${date}）`;
    const coverage = detail.sources.map((source) => `${source.source}: ${source.status}（${source.collectedCount}）`).join("；");
    const content = [
      `> 方法：仅基于 ${detail.run.dayRange} 天范围内的候选元数据进行确定性排序与跨平台聚类；未自动下载全文或媒体。`,
      `> 来源覆盖：${coverage}`,
      "",
      detail.run.reportMarkdown,
    ].join("\n");
    if (detail.run.savedItemId && this.items.get(detail.run.savedItemId)) {
      this.items.update(detail.run.savedItemId, { title, content, tagNames: ["研究报告"] });
      return { itemId: detail.run.savedItemId, updated: true };
    }
    const created = this.items.create({ title, content, itemType: "note", tagNames: ["研究报告"] });
    this.store.setSavedItem(id, created.id);
    this.emit(id);
    return { itemId: created.id, updated: false };
  }

  private async run(id: string): Promise<void> {
    const run = this.store.get(id);
    if (!run) return;
    const controller = new AbortController();
    this.controllers.set(id, controller);
    const selected = new Set(run.sources);
    const browserSources = (["xiaohongshu", "douyin"] as const).filter((source) => selected.has(source));
    const browserFlow = async () => {
      for (const source of browserSources) {
        if (controller.signal.aborted) break;
        await this.collectSource(run, source, controller.signal);
      }
    };
    // 先启动 HTTP 来源，再启动登录态浏览器组；两者并行，而浏览器组内部仍按序 await。
    // 若反过来在数组字面量里先调用 browserFlow()，它会同步跑到首个 await，
    // B 站甚至还没开始，首个浏览器来源就已经起跑了。
    const tasks: Promise<void>[] = [];
    if (selected.has("bilibili")) tasks.push(this.collectSource(run, "bilibili", controller.signal));
    tasks.push(browserFlow());
    await Promise.all(tasks);
    if (controller.signal.aborted || this.store.get(id)?.status === "canceled") {
      this.controllers.delete(id);
      return;
    }
    const detail = this.store.getDetail(id)!;
    const analysis = analyzeResearchCandidates(run.topic, run.rangeFrom, run.rangeTo, detail.candidates);
    this.store.replaceAnalysis(id, analysis.candidates, analysis.clusters);
    const succeeded = detail.sources.filter((source) => source.status === "succeeded" || source.status === "partial");
    const hasCandidates = detail.candidates.length > 0;
    const allSucceeded = detail.sources.every((source) => source.status === "succeeded");
    this.store.finishRun(id, hasCandidates ? (allSucceeded ? "ready" : "partial") : (succeeded.length > 0 ? "ready" : "failed"));
    this.controllers.delete(id);
    this.emit(id);
  }

  private async collectSource(run: ResearchRun, source: ResearchSource, signal: AbortSignal): Promise<void> {
    const collector = this.collectors[source];
    const maxPages = run.depth === "quick" ? 1 : 3;
    let cursor: string | null = null;
    let count = 0;
    let pages = 0;
    const seenCursors = new Set<string>();
    this.store.updateSource(run.id, source, { status: "running", startedAt: Date.now(), error: null, errorCode: null });
    this.emit(run.id);
    try {
      while (pages < maxPages && !signal.aborted) {
        const cursorKey = cursor ?? "__first__";
        if (seenCursors.has(cursorKey)) throw new Error("采集器返回了重复游标");
        seenCursors.add(cursorKey);
        const page = await collector.search({
          topic: run.topic,
          rangeFrom: run.rangeFrom,
          rangeTo: run.rangeTo,
          cursor,
          limit: PAGE_LIMIT,
          signal,
        });
        for (const item of page.items) {
          const normalized = normalizeUrl(item.url);
          if (normalized && this.store.upsertCandidate(run.id, item, normalized)) count += 1;
        }
        pages += 1;
        this.store.updateSource(run.id, source, { collectedCount: count });
        this.emit(run.id);
        if (!page.hasMore || !page.cursor) break;
        cursor = page.cursor;
      }
      if (signal.aborted) throw new DOMException("已取消", "AbortError");
      this.store.updateSource(run.id, source, {
        status: "succeeded",
        collectedCount: count,
        finishedAt: Date.now(),
      });
    } catch (error) {
      const code = sourceErrorCode(error);
      const status = code === "login_required" ? "login_required" : code === "canceled" ? "canceled" : count > 0 ? "partial" : "failed";
      this.store.updateSource(run.id, source, {
        status,
        collectedCount: count,
        errorCode: code,
        error: errorMessage(error),
        finishedAt: Date.now(),
      });
    }
    this.emit(run.id);
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
    const detail = this.store.getDetail(id);
    if (detail) this.options.onChanged?.(detail);
  }
}

export function selectEvidence(candidates: readonly ResearchCandidate[]): ResearchCandidate[] {
  const sorted = [...candidates].sort((a, b) => b.overallScore - a.overallScore || a.id.localeCompare(b.id));
  const output: ResearchCandidate[] = [];
  const selected = new Set<string>();
  const perSource = new Map<ResearchSource, number>();
  for (const source of ["xiaohongshu", "douyin", "bilibili"] as const) {
    const available = sorted.filter((candidate) => candidate.source === source).slice(0, 5);
    for (const candidate of available) {
      output.push(candidate);
      selected.add(candidate.id);
      perSource.set(source, (perSource.get(source) ?? 0) + 1);
    }
  }
  for (const candidate of sorted) {
    if (output.length >= 30) break;
    if (selected.has(candidate.id) || (perSource.get(candidate.source) ?? 0) >= 10) continue;
    output.push(candidate);
    selected.add(candidate.id);
    perSource.set(candidate.source, (perSource.get(candidate.source) ?? 0) + 1);
  }
  return output.sort((a, b) => b.overallScore - a.overallScore || a.id.localeCompare(b.id));
}
