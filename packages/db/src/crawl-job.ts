import { randomUUID } from "node:crypto";
import type Database from "./adapter";
import type {
  CrawlJob,
  CrawlPage,
  CreateCrawlJobInput,
  CrawlJobStatus,
} from "@guizhi/shared/types";
import {
  canonicalWebUrl,
  validateCrawlInput,
} from "@guizhi/shared/utils/web-scope";

export class CrawlJobDB {
  constructor(private db: Database) {}
  create(raw: CreateCrawlJobInput): CrawlJob {
    const input = validateCrawlInput(raw),
      now = Date.now();
    const job: CrawlJob = {
      id: randomUUID(),
      input,
      status: "pending",
      createdAt: now,
      updatedAt: now,
      counts: {},
    };
    this.db.transaction(() => {
      this.db.run(
        "INSERT INTO crawl_jobs VALUES (?,?,?,?,?)",
        job.id,
        JSON.stringify(job),
        job.status,
        now,
        now,
      );
      input.seeds.forEach((seed, i) => this.discover(job.id, seed.url, 0, i));
    })();
    return this.get(job.id)!;
  }
  get(id: string): CrawlJob | null {
    const row = this.db.get(
      "SELECT payload,status,updated_at FROM crawl_jobs WHERE id=?",
      id,
    ) as
      | { payload: string; status: CrawlJobStatus; updated_at: number }
      | undefined;
    if (!row) return null;
    const counts: CrawlJob["counts"] = {};
    for (const r of this.db.all(
      "SELECT status,COUNT(*) AS n FROM crawl_pages WHERE job_id=? GROUP BY status",
      id,
    ) as { status: CrawlPage["status"]; n: number }[])
      counts[r.status] = r.n;
    return {
      ...JSON.parse(row.payload),
      status: row.status,
      updatedAt: row.updated_at,
      counts,
    };
  }
  list(): CrawlJob[] {
    return (
      this.db.all(
        "SELECT id FROM crawl_jobs ORDER BY created_at DESC LIMIT 100",
      ) as { id: string }[]
    ).map((r) => this.get(r.id)!);
  }
  setStatus(id: string, status: CrawlJobStatus, error?: string): void {
    const job = this.get(id);
    if (!job) throw new Error("采集批次不存在");
    this.db.run(
      "UPDATE crawl_jobs SET status=?,updated_at=?,payload=? WHERE id=?",
      status,
      Date.now(),
      JSON.stringify({ ...job, status, error }),
      id,
    );
  }
  pages(id: string): CrawlPage[] {
    return (
      this.db.all(
        "SELECT payload,status FROM crawl_pages WHERE job_id=? ORDER BY depth,rowid",
        id,
      ) as { payload: string; status: CrawlPage["status"] }[]
    ).map((r) => ({ ...JSON.parse(r.payload), status: r.status }));
  }
  discover(
    jobId: string,
    raw: string,
    depth: number,
    seedIndex: number,
  ): boolean {
    const job = this.get(jobId);
    if (!job) throw new Error("采集批次不存在");
    const count = this.db.get(
      "SELECT COUNT(*) AS n FROM crawl_pages WHERE job_id=?",
      jobId,
    ) as { n: number };
    if (count.n >= job.input.maxPages! || depth > job.input.maxDepth!)
      return false;
    const url = canonicalWebUrl(raw);
    const page: CrawlPage = {
      id: randomUUID(),
      jobId,
      url,
      depth,
      seedIndex,
      status: "pending",
    };
    return (
      this.db.run(
        "INSERT OR IGNORE INTO crawl_pages VALUES (?,?,?,?,?,?,?)",
        page.id,
        jobId,
        url,
        depth,
        seedIndex,
        page.status,
        JSON.stringify(page),
      ).changes > 0
    );
  }
  save(page: CrawlPage): void {
    this.db.run(
      "UPDATE crawl_pages SET status=?,payload=? WHERE id=? AND job_id=?",
      page.status,
      JSON.stringify(page),
      page.id,
      page.jobId,
    );
  }
  recover(): void {
    this.db.transaction(() => {
      this.db.run(
        "UPDATE crawl_jobs SET status='interrupted',updated_at=? WHERE status IN ('pending','running')",
        Date.now(),
      );
      this.db.run(
        "UPDATE crawl_pages SET status='pending' WHERE status='running'",
      );
    })();
  }
  retry(id: string): void {
    if (this.get(id)?.status === "running") throw new Error("请先暂停批次");
    this.db.run(
      "UPDATE crawl_pages SET status='pending' WHERE job_id=? AND status='failed'",
      id,
    );
    this.setStatus(id, "paused");
  }
}
