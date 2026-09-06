import { randomUUID } from "node:crypto";
import { CrawlJobDB, ResearchDB, ResearchWorkflowDB } from "@guizhi/db";
import type Database from "../../database/sqlite";
import type {
  ResearchCandidate,
  ResearchDocument,
  ResearchRun,
} from "@guizhi/shared/types";
import { normalizeUrl } from "../import/url-normalize";
import { CrawlService } from "./crawl-service";

export async function collectWebResearch(
  db: Database.Database,
  run: ResearchRun,
  signal: AbortSignal,
  changed: () => void,
): Promise<void> {
  const store = new ResearchDB(db),
    workflow = new ResearchWorkflowDB(db),
    context = workflow.context(run.id);
  store.updateSource(run.id, "web", {
    status: "running",
    startedAt: Date.now(),
  });
  changed();
  const crawl = new CrawlService(db, false);
  let count = 0,
    jobId = context?.webCrawlJobId;
  try {
    if (!context?.webSeeds?.length) throw new Error("网页研究需要指定网址入口");
    if (!jobId || !crawl.jobs.get(jobId)) {
      jobId = crawl.jobs.create({
        purpose: "research",
        seeds: context.webSeeds,
        maxPages: run.depth === "quick" ? 20 : 60,
        maxDepth: 2,
        researchRunId: run.id,
      }).id;
      workflow.patchContext(run.id, { webCrawlJobId: jobId });
    }
    const cancel = () => crawl.pause(jobId!);
    signal.addEventListener("abort", cancel, { once: true });
    try {
      signal.throwIfAborted();
      await crawl.resume(jobId);
    } finally {
      signal.removeEventListener("abort", cancel);
    }
    signal.throwIfAborted();
    const job = crawl.jobs.get(jobId)!;
    const attemptId = randomUUID();
    for (const page of crawl.jobs.pages(jobId)) {
      const result = page.result;
      if (!result?.complete || result.error) continue;
      if (
        run.timeScope !== "all" &&
        result.publishedAt != null &&
        (result.publishedAt < run.rangeFrom || result.publishedAt > run.rangeTo)
      )
        continue;
      store.upsertCandidate(
        run.id,
        {
          source: "web",
          externalId: result.contentHash,
          url: result.finalUrl,
          title: result.title,
          author: result.author || new URL(result.finalUrl).hostname,
          authorId: new URL(result.finalUrl).hostname,
          snippet: result.markdown.slice(0, 4000),
          publishedAt: result.publishedAt ?? undefined,
          dateConfidence: result.dateConfidence === "exact" ? "high" : "low",
          mediaType: "article",
          engagement: {},
          discoveryMethod: "crawl4ai-public-web",
        },
        normalizeUrl(result.finalUrl)!,
      );
      count++;
    }
    const failed = job.status !== "completed" || !!job.counts.failed;
    workflow.putAttempt({
      id: attemptId,
      runId: run.id,
      source: "web",
      query: run.topic,
      cursor: null,
      nextCursor: null,
      finished: !failed,
      method: "crawl4ai-public-web",
      startedAt: job.createdAt,
      finishedAt: Date.now(),
      returnedCount: count,
      inWindowCount:
        run.timeScope === "all"
          ? count
          : store
              .listCandidates(run.id)
              .filter(
                (c) =>
                  c.source === "web" &&
                  c.publishedAt != null &&
                  c.dateConfidence !== "low",
              ).length,
      unknownDateCount: store
        .listCandidates(run.id)
        .filter((c) => c.source === "web" && c.publishedAt == null).length,
      capped: crawl.jobs.pages(jobId).length >= job.input.maxPages!,
      ...(failed ? { errorCode: "partial_capture", error: job.error } : {}),
    });
    store.updateSource(run.id, "web", {
      status: failed ? (count ? "partial" : "failed") : "succeeded",
      collectedCount: count,
      finishedAt: Date.now(),
      error:
        job.error ??
        (job.counts.failed ? `${job.counts.failed} 个网页采集失败` : null),
    });
  } catch (error) {
    store.updateSource(run.id, "web", {
      status: signal.aborted ? "canceled" : count ? "partial" : "failed",
      collectedCount: count,
      finishedAt: Date.now(),
      error: error instanceof Error ? error.message : "网页研究失败",
    });
  } finally {
    changed();
  }
}

/** 精读只映射本轮捕获快照，不重新下载或引用后来的知识库原文。 */
export function readWebResearch(
  db: Database.Database,
  candidate: ResearchCandidate,
): ResearchDocument {
  const jobId = new ResearchWorkflowDB(db).context(
    candidate.runId,
  )?.webCrawlJobId;
  const pages = jobId ? new CrawlJobDB(db).pages(jobId) : [];
  const result =
    pages.find((p) => p.result?.finalUrl === candidate.url)?.result ??
    pages.find((p) => p.result?.contentHash === candidate.externalId)?.result;
  if (!result) throw new Error("本轮网页快照缺失，请恢复网页采集");
  return {
    sourceUrls: [
      ...new Set(
        pages
          .filter((p) => p.result?.contentHash === result.contentHash)
          .map((p) => p.result!.finalUrl),
      ),
    ],
    id: randomUUID(),
    runId: candidate.runId,
    candidateId: candidate.id,
    source: "web",
    url: result.finalUrl,
    title: result.title,
    author: result.author,
    publishedAt: result.publishedAt,
    capturedAt: result.capturedAt,
    status: result.complete ? "ready" : "partial",
    passages: result.paragraphs.map((p, index) => ({
      text: p.text,
      position: index,
      kind: "body",
      externalId: p.id,
    })),
    contentHash: result.contentHash,
    truncated: result.truncated,
    warning: result.warnings.join("；") || undefined,
  };
}
