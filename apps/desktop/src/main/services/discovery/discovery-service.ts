import { DiscoveryDB } from "@guizhi/db";
import type Database from "@guizhi/db/adapter";
import type {
  DiscoveryRunResult,
  DiscoveryView,
  DiscoveryViewDetail,
  SaveDiscoveryViewInput,
} from "@guizhi/shared/types";
import { PlatformCaptureError } from "../platform-capture/browser-capture";
import type { BackgroundJobRuntime } from "../background-jobs";
import type { DiscoveryCollector } from "./discovery-collector";
import { normalizeUrl } from "../import/url-normalize";

const MAX_PAGES = 5;
const MAX_ITEMS = 100;
const STOP_AFTER_KNOWN = 20;
const MAX_JITTER_MS = 10 * 60_000;

export interface DiscoveryServiceOptions {
  now?: () => number;
  random?: () => number;
  notify?: (view: DiscoveryView, count: number, loginRequired: boolean) => void;
}

export class DiscoveryService {
  private readonly discovery: DiscoveryDB;
  private readonly now: () => number;
  private readonly random: () => number;

  constructor(
    private readonly db: Database.Database,
    private readonly collector: DiscoveryCollector,
    private readonly jobs: BackgroundJobRuntime,
    private readonly options: DiscoveryServiceOptions = {},
  ) {
    this.discovery = new DiscoveryDB(db);
    this.now = options.now ?? Date.now;
    this.random = options.random ?? Math.random;
  }

  registerBackgroundHandler(): void {
    this.jobs.registerHandler("platform-discovery", async (job) => {
      const view = this.discovery.getView(job.scopeId);
      if (!view || !view.enabled) return { pause: true, message: "发现视图已停用" };
      try {
        const result = await this.run(view.id);
        return { nextRunAt: result.view.nextRunAt ?? undefined };
      } catch (error) {
        if (error instanceof PlatformCaptureError && error.code === "login_required") {
          return { pause: true, message: error.message };
        }
        throw error;
      }
    });
  }

  listViews(): DiscoveryView[] {
    return this.discovery.listViews();
  }

  getDetail(id: string): DiscoveryViewDetail | null {
    const view = this.discovery.getView(id);
    return view
      ? {
          view,
          candidates: this.discovery.listCandidates(id),
          runs: this.discovery.listRuns(id),
        }
      : null;
  }

  save(input: SaveDiscoveryViewInput): DiscoveryView {
    const now = this.now();
    const next = input.enabled ? this.nextRunAt(input.intervalMinutes ?? 1440, now) : null;
    const view = this.discovery.saveView(input, next, now);
    this.syncJob(view);
    return view;
  }

  delete(id: string): boolean {
    this.jobs.setPaused(`discovery-${id}`, true);
    this.jobs.unschedule("platform-discovery", id);
    return this.discovery.deleteView(id);
  }

  resumeAfterLogin(id: string): DiscoveryView | null {
    const now = this.now();
    const view = this.discovery.setViewState(id, "ready", {
      enabled: true,
      nextRunAt: now,
    }, now);
    if (view) this.syncJob(view, now);
    return view;
  }

  async run(id: string): Promise<DiscoveryRunResult> {
    const view = this.discovery.getView(id);
    if (!view) throw new Error("发现视图不存在");
    if (view.state === "running") throw new Error("该发现视图正在运行");
    const now = this.now();
    const previous = this.discovery.getResumableRun(id);
    let cursor = previous?.cursor ?? null;
    let pages = 0;
    let scanned = 0;
    let found = 0;
    let consecutiveKnown = 0;
    const run = this.discovery.beginRun(id, cursor, now);
    this.discovery.setViewState(id, "running", { lastRunAt: now }, now);

    try {
      while (pages < MAX_PAGES && scanned < MAX_ITEMS) {
        const page = await this.collector.collect(view, cursor);
        pages += 1;
        for (const item of page.items.slice(0, MAX_ITEMS - scanned)) {
          scanned += 1;
          const alreadyImported = this.isAlreadyImported(item.url);
          if (this.discovery.upsertCandidate(id, item, this.now()) && !alreadyImported) {
            found += 1;
            consecutiveKnown = 0;
          } else {
            consecutiveKnown += 1;
          }
          if (alreadyImported) {
            this.discovery.setCandidateState(item.platform, item.externalId, "imported");
          }
          if (consecutiveKnown >= STOP_AFTER_KNOWN) break;
        }
        cursor = page.hasMore ? page.cursor : null;
        this.discovery.updateRun(run.id, {
          cursor,
          pagesScanned: pages,
          candidatesFound: found,
        }, false, this.now());
        if (!page.hasMore || !cursor || consecutiveKnown >= STOP_AFTER_KNOWN) break;
      }

      const finishedAt = this.now();
      const finished = this.discovery.updateRun(run.id, {
        state: "completed",
        // 正常停止代表本轮结束；下一轮必须从首页重新扫描。
        cursor: null,
        pagesScanned: pages,
        candidatesFound: found,
        error: null,
      }, true, finishedAt);
      const nextRunAt = this.nextRunAt(view.intervalMinutes, finishedAt);
      const updated = this.discovery.setViewState(id, view.enabled ? "ready" : "paused", {
        lastRunAt: finishedAt,
        nextRunAt: view.enabled ? nextRunAt : null,
      }, finishedAt)!;
      this.discovery.prune(finishedAt);
      if (found > 0) this.options.notify?.(updated, found, false);
      return { view: updated, run: finished, newCandidates: found };
    } catch (error) {
      const failedAt = this.now();
      const loginRequired =
        error instanceof PlatformCaptureError && error.code === "login_required";
      const failed = this.discovery.updateRun(run.id, {
        state: "failed",
        cursor,
        pagesScanned: pages,
        candidatesFound: found,
        error: error instanceof Error ? error.message : String(error),
      }, true, failedAt);
      const updated = this.discovery.setViewState(
        id,
        loginRequired ? "login_required" : "backoff",
        { lastRunAt: failedAt, nextRunAt: loginRequired ? null : view.nextRunAt },
        failedAt,
      )!;
      if (loginRequired && view.state !== "login_required") {
        this.options.notify?.(updated, 0, true);
      }
      this.discovery.prune(failedAt);
      void failed;
      throw error;
    }
  }

  setCandidateState(
    platform: DiscoveryView["platform"],
    externalId: string,
    state: "new" | "dismissed" | "imported",
  ): boolean {
    return this.discovery.setCandidateState(platform, externalId, state);
  }

  private syncJob(view: DiscoveryView, nextRunAt = view.nextRunAt ?? this.now()): void {
    this.jobs.schedule(`discovery-${view.id}`, {
      kind: "platform-discovery",
      scopeId: view.id,
      intervalMinutes: view.intervalMinutes,
      nextRunAt,
      enabled: view.enabled && view.state !== "login_required",
    });
  }

  private nextRunAt(intervalMinutes: number, now: number): number {
    return now + intervalMinutes * 60_000 + Math.floor(this.random() * MAX_JITTER_MS);
  }

  private isAlreadyImported(url: string): boolean {
    const row = this.db.get(
      `SELECT 1 AS found FROM source_records s
       JOIN knowledge_items i ON i.id=s.item_id
       WHERE s.normalized_uri=? AND i.deleted_at IS NULL LIMIT 1`,
      normalizeUrl(url),
    ) as { found: number } | undefined;
    return row?.found === 1;
  }
}
