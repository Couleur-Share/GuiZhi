import { randomUUID } from "node:crypto";
import { BackgroundJobDB } from "@guizhi/db/background-job";
import type Database from "@guizhi/db/adapter";
import type {
  BackgroundJob,
  BackgroundJobKind,
  BackgroundJobScheduleInput,
  BackgroundJobSyncInput,
} from "@guizhi/shared/types";

const TICK_INTERVAL_MS = 30_000;
const LEASE_DURATION_MS = 90_000;
const RETRY_DELAYS_MS = [15 * 60_000, 60 * 60_000, 6 * 60 * 60_000, 24 * 60 * 60_000] as const;
const RENDERER_JOB_KINDS: BackgroundJobKind[] = ["wiki-compile", "semantic-index"];
const RENDERER_INTERVAL_MINUTES = 5;

export interface BackgroundJobHandlerResult {
  pause?: boolean;
  message?: string;
  nextRunAt?: number;
}

export type BackgroundJobHandler = (
  job: BackgroundJob,
) => Promise<void | BackgroundJobHandlerResult>;

interface BackgroundJobRuntimeOptions {
  sendRendererJob: (job: BackgroundJob) => void;
  now?: () => number;
  ownerId?: string;
  tickIntervalMs?: number;
  leaseDurationMs?: number;
}

/**
 * 主进程持有的统一后台任务调度器。
 *
 * 任务先落库再执行；租约过期后可重新领取。需要浏览器环境的任务只把领取结果
 * 发给 Renderer，完成、失败与心跳仍回到这里校验租约持有者。
 */
export class BackgroundJobRuntime {
  private readonly jobs: BackgroundJobDB;
  private readonly ownerId: string;
  private readonly now: () => number;
  private readonly tickIntervalMs: number;
  private readonly leaseDurationMs: number;
  private readonly handlers = new Map<BackgroundJobKind, BackgroundJobHandler>();
  private readonly runningMainKinds = new Set<BackgroundJobKind>();
  private rendererJobId: string | null = null;
  private timer: ReturnType<typeof setInterval> | null = null;
  private ticking = false;

  constructor(
    db: Database.Database,
    private readonly options: BackgroundJobRuntimeOptions,
  ) {
    this.jobs = new BackgroundJobDB(db);
    this.ownerId = options.ownerId ?? `main-${randomUUID()}`;
    this.now = options.now ?? Date.now;
    this.tickIntervalMs = options.tickIntervalMs ?? TICK_INTERVAL_MS;
    this.leaseDurationMs = options.leaseDurationMs ?? LEASE_DURATION_MS;
  }

  start(): void {
    if (this.timer) return;
    void this.tick();
    this.timer = setInterval(() => void this.tick(), this.tickIntervalMs);
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  registerHandler(kind: BackgroundJobKind, handler: BackgroundJobHandler): void {
    if (RENDERER_JOB_KINDS.includes(kind)) {
      throw new Error(`${kind} 必须由 Renderer 执行，不能注册主进程 handler`);
    }
    this.handlers.set(kind, handler);
  }

  schedule(id: string, input: BackgroundJobScheduleInput): BackgroundJob {
    const job = this.jobs.upsert(id, input, this.now());
    void this.tick();
    return job;
  }

  list(): BackgroundJob[] {
    return this.jobs.list();
  }

  unschedule(kind: BackgroundJobKind, scopeId: string): boolean {
    return this.jobs.remove(kind, scopeId);
  }

  syncRendererJobs(input: BackgroundJobSyncInput): BackgroundJob[] {
    const now = this.now();
    const current = this.list();
    const wikiJob = current.find((job) => job.kind === "wiki-compile");
    const semanticJob = current.find((job) => job.kind === "semantic-index");
    this.jobs.upsert(
      "renderer-wiki-compile",
      {
        kind: "wiki-compile",
        intervalMinutes: RENDERER_INTERVAL_MINUTES,
        nextRunAt:
          wikiJob?.state === "scheduled" || wikiJob?.state === "retry_wait"
            ? wikiJob.nextRunAt
            : now + RENDERER_INTERVAL_MINUTES * 60_000,
        enabled: input.wikiEnabled,
      },
      now,
    );
    this.jobs.upsert(
      "renderer-semantic-index",
      {
        kind: "semantic-index",
        intervalMinutes: RENDERER_INTERVAL_MINUTES,
        nextRunAt:
          semanticJob?.state === "scheduled" ||
          semanticJob?.state === "retry_wait"
            ? semanticJob.nextRunAt
            : now + 60_000,
        enabled: input.semanticEnabled,
      },
      now,
    );
    void this.tick();
    return this.list();
  }

  renewRendererJob(id: string): boolean {
    return this.jobs.renewLease(
      id,
      this.ownerId,
      this.leaseDurationMs,
      this.now(),
    );
  }

  completeRendererJob(id: string): boolean {
    const completed = this.jobs.complete(id, this.ownerId, this.now());
    if (this.rendererJobId === id) this.rendererJobId = null;
    void this.tick();
    return completed;
  }

  failRendererJob(id: string, error: string, pause = false): boolean {
    const job = this.jobs.get(id);
    if (!job) return false;
    const failed = this.jobs.fail(
      id,
      this.ownerId,
      error,
      pause ? null : this.retryDelay(job.attempt),
      this.now(),
    );
    if (this.rendererJobId === id) this.rendererJobId = null;
    void this.tick();
    return failed;
  }

  setPaused(id: string, paused: boolean): boolean {
    if (this.rendererJobId === id) this.rendererJobId = null;
    const changed = this.jobs.setPaused(id, paused, this.now());
    if (!paused) void this.tick();
    return changed;
  }

  async tick(): Promise<void> {
    if (this.ticking) return;
    this.ticking = true;
    try {
      for (const [kind, handler] of this.handlers) {
        if (this.runningMainKinds.has(kind)) continue;
        const job = this.jobs.claimDue(
          [kind],
          this.ownerId,
          this.leaseDurationMs,
          this.now(),
        );
        if (!job) continue;
        this.runningMainKinds.add(kind);
        void this.runMainJob(job, handler);
      }

      if (this.rendererJobId) {
        const active = this.jobs.get(this.rendererJobId);
        if (
          active?.state !== "running" ||
          active.leaseOwner !== this.ownerId ||
          (active.leaseUntil ?? 0) <= this.now()
        ) {
          this.rendererJobId = null;
        }
      }
      if (!this.rendererJobId) {
        const job = this.jobs.claimDue(
          RENDERER_JOB_KINDS,
          this.ownerId,
          this.leaseDurationMs,
          this.now(),
        );
        if (job) {
          this.rendererJobId = job.id;
          this.options.sendRendererJob(job);
        }
      }
    } finally {
      this.ticking = false;
    }
  }

  private async runMainJob(
    job: BackgroundJob,
    handler: BackgroundJobHandler,
  ): Promise<void> {
    try {
      const result = await handler(job);
      if (result && result.pause) {
        this.jobs.fail(
          job.id,
          this.ownerId,
          result.message ?? "任务已暂停",
          null,
          this.now(),
        );
      } else {
        this.jobs.complete(
          job.id,
          this.ownerId,
          this.now(),
          result && "nextRunAt" in result ? result.nextRunAt : undefined,
        );
      }
    } catch (error) {
      const current = this.jobs.get(job.id);
      this.jobs.fail(
        job.id,
        this.ownerId,
        error instanceof Error ? error.message : String(error),
        this.retryDelay(current?.attempt ?? job.attempt),
        this.now(),
      );
    } finally {
      this.runningMainKinds.delete(job.kind);
      void this.tick();
    }
  }

  private retryDelay(attempt: number): number {
    return RETRY_DELAYS_MS[Math.min(Math.max(attempt, 0), RETRY_DELAYS_MS.length - 1)];
  }
}
