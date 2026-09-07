import { releaseSnapshotAssets } from "./snapshot-assets";
import { cleanupOrphanAssets } from "../asset-cleanup";
import { randomUUID } from "node:crypto";
import { parseHTML } from "linkedom";
import {
  CrawlJobDB,
  KnowledgeItemDB,
  WebSourceDB,
  ImportTaskDB,
} from "@guizhi/db";
import type Database from "../../database/sqlite";
import type {
  CreateCrawlJobInput,
  CrawlPage,
  WebCaptureResult,
} from "@guizhi/shared/types";
import {
  canonicalWebUrl,
  inWebScope,
  isWebPageLink,
  webScope,
} from "@guizhi/shared/utils/web-scope";
import { normalizeUrl } from "../import/url-normalize";
import { resolveSourcePlatform } from "@guizhi/shared/utils/source-platforms";
import { captureWebPage, getWebCaptureStatus } from "./web-capture";
import { loadRobots, robotsAllows, type RobotsPolicy } from "./robots";
import { webTextRequest } from "./web-network";

export class CrawlService {
  readonly jobs: CrawlJobDB;
  private running = new Map<string, AbortController>();
  private completions = new Map<string, Promise<void>>();
  constructor(
    private db: Database.Database,
    recover = true,
  ) {
    this.jobs = new CrawlJobDB(db);
    if (recover) this.jobs.recover();
  }
  async create(input: CreateCrawlJobInput): Promise<string> {
    if (
      input.collectionId &&
      !this.db.get("SELECT id FROM collections WHERE id=?", input.collectionId)
    )
      throw new Error("目标集合不存在，请重新选择");
    const status = await getWebCaptureStatus();
    if (!status.available) throw new Error(status.reason);
    const job = this.jobs.create(input);
    void this.resume(job.id);
    return job.id;
  }
  pause(id: string): void {
    this.jobs.setStatus(id, "paused");
    this.running.get(id)?.abort();
  }
  cancel(id: string): void {
    this.jobs.setStatus(id, "canceled");
    this.running.get(id)?.abort();
    for (const page of this.jobs.pages(id))
      if (page.status === "pending")
        this.jobs.save({ ...page, status: "canceled" });
  }
  async close(): Promise<void> {
    for (const id of this.running.keys()) this.pause(id);
    await Promise.all([...this.completions.values()]);
  }
  async resume(id: string): Promise<void> {
    if (this.running.has(id)) return;
    const job = this.jobs.get(id);
    if (!job) throw new Error("采集批次不存在");
    if (job.status === "canceled") throw new Error("已取消的批次不能继续");
    const controller = new AbortController();
    this.running.set(id, controller);
    this.jobs.setStatus(id, "running");
    let settled!: () => void;
    this.completions.set(
      id,
      new Promise<void>((resolve) => {
        settled = resolve;
      }),
    );
    const policies = new Map<string, RobotsPolicy>();
    try {
      const status = await getWebCaptureStatus();
      if (!status.available) throw new Error(status.reason);
      for (let index = 0; index < job.input.seeds.length; index++) {
        const seed = job.input.seeds[index],
          origin = new URL(seed.url).origin;
        if (!policies.has(origin))
          policies.set(origin, await loadRobots(origin, controller.signal));
        // sitemap 只提供候选，最多读取 3 个文件，不展开 sitemap 索引树。
        if (seed.mode === "directory") {
          const scope = webScope(seed.url, seed.directory);
          for (const url of policies.get(origin)!.sitemaps.slice(0, 3)) {
            if (
              !inWebScope(url, { origin, directory: "/" }) ||
              !robotsAllows(url, policies.get(origin)!)
            )
              continue;
            const response = await webTextRequest(
              url,
              controller.signal,
              (u) =>
                new URL(u).origin === origin &&
                robotsAllows(u, policies.get(origin)!),
            );
            if (response.status !== 200) continue;
            const { document } = parseHTML(response.text);
            for (const node of Array.from(
              document.querySelectorAll("url > loc"),
            ).slice(0, 300)) {
              const link = node.textContent?.trim();
              if (link && inWebScope(link, scope) && isWebPageLink(link))
                this.jobs.discover(id, link, 1, index);
            }
          }
        }
      }
      while (!controller.signal.aborted) {
        const page = this.jobs.pages(id).find((p) => p.status === "pending");
        if (!page) break;
        const seed = job.input.seeds[page.seedIndex],
          scope =
            seed.mode === "directory"
              ? webScope(seed.url, seed.directory)
              : undefined;
        if (!robotsAllows(page.url, policies.get(new URL(seed.url).origin)!)) {
          this.jobs.save({
            ...page,
            status: "skipped",
            error: "robots.txt 禁止访问",
          });
          continue;
        }
        this.jobs.save({ ...page, status: "running" });
        let result: WebCaptureResult | undefined;
        try {
          result = await captureWebPage(
            {
              taskId: page.id,
              purpose: job.input.purpose,
              url: page.url,
              scope,
            },
            controller.signal,
            (stage) => this.jobs.save({ ...page, status: "running", stage }),
          );
          controller.signal.throwIfAborted();
          if (result.error || !result.complete)
            throw new Error(result.error?.message ?? "正文不完整，未入库");
          this.db.transaction(() => {
            const saved =
              job.input.purpose === "documents"
                ? this.persist(
                    page,
                    result,
                    job.input.collectionId,
                    job.input.duplicatePolicy,
                  )
                : { ...page, status: "added" as const, result };
            if (job.input.purpose === "documents") {
              const tasks = new ImportTaskDB(this.db),
                task = page.importTaskId
                  ? tasks.get(page.importTaskId)
                  : tasks.create({
                      kind: "url",
                      input: page.url,
                      collectionId: job.input.collectionId,
                    });
              if (task) {
                tasks.update(task.id, {
                  status:
                    saved.status === "duplicate" ? "duplicate" : "completed",
                  resultItemId: saved.itemId,
                  duplicateItemId:
                    saved.status === "duplicate" ? saved.itemId : null,
                  itemType: "webpage",
                  displayName: result.title,
                  warning:
                    saved.status === "pending-version"
                      ? "原文有新版本可比较，已保留当前正文"
                      : null,
                });
                saved.importTaskId = task.id;
              }
            }
            this.jobs.save(saved);
            if (scope && page.depth < job.input.maxDepth!) {
              for (const link of result.links)
                if (inWebScope(link, scope) && isWebPageLink(link))
                  this.jobs.discover(
                    id,
                    canonicalWebUrl(link),
                    page.depth + 1,
                    page.seedIndex,
                  );
            }
          })();
        } catch (error) {
          this.jobs.save({
            ...page,
            status: controller.signal.aborted
              ? this.jobs.get(id)?.status === "canceled"
                ? "canceled"
                : "pending"
              : "failed",
            error: error instanceof Error ? error.message : "网页采集失败",
          });
        } finally {
          if (result?.snapshot) {
            releaseSnapshotAssets(result.snapshot.assets);
            cleanupOrphanAssets(new KnowledgeItemDB(this.db), result.snapshot.assets.map(a => a.fileName));
          }
        }
      }
      if (!controller.signal.aborted) this.jobs.setStatus(id, "completed");
    } catch (error) {
      if (!controller.signal.aborted)
        this.jobs.setStatus(
          id,
          "paused",
          error instanceof Error ? error.message : "批次已暂停",
        );
    } finally {
      this.running.delete(id);
      this.completions.delete(id);
      settled();
    }
  }
  private persist(
    page: CrawlPage,
    result: WebCaptureResult,
    collectionId?: string | null,
    policy?: "skip" | "update",
  ): CrawlPage {
    const uri = normalizeUrl(result.finalUrl);
    const existing = this.db.get(
      "SELECT s.item_id FROM source_records s JOIN knowledge_items i ON i.id=s.item_id WHERE (s.normalized_uri=? OR s.normalized_uri=?) AND i.deleted_at IS NULL ORDER BY s.captured_at DESC LIMIT 1",
      uri,
      normalizeUrl(page.url),
    ) as { item_id: string } | undefined;
    if (existing)
      return {
        ...page,
        result,
        itemId: existing.item_id,
        status:
          policy === "update"
            ? new WebSourceDB(this.db).check(existing.item_id, result)
            : "duplicate",
      };
    const item = new KnowledgeItemDB(this.db).create({
      title: result.title,
      content: result.markdown,
      itemType: "webpage",
      collectionId,
    });
    this.db.run(
      "INSERT INTO source_records (id,item_id,source_type,source_uri,normalized_uri,content_hash,captured_at,platform) VALUES (?,?,?,?,?,?,?,?)",
      randomUUID(),
      item.id,
      "url",
      result.finalUrl,
      uri,
      result.contentHash,
      result.capturedAt,
      resolveSourcePlatform("url", result.finalUrl),
    );
    new WebSourceDB(this.db).initialize(item.id, result);
    return { ...page, result, itemId: item.id, status: "added" };
  }
}
