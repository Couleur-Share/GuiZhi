import { describe, expect, it, vi } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import type {
  ResearchCandidateInput,
  ResearchPage,
  ResearchSearchInput,
  ResearchSource,
} from "@guizhi/shared/types";
import {
  ResearchService,
  selectEvidence,
} from "../../../src/main/services/research/research-service";
import type { ResearchCollector } from "../../../src/main/services/research/collectors";

function createDb() {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

function item(source: ResearchSource, id: string): ResearchCandidateInput {
  return {
    source,
    externalId: id,
    url: source === "bilibili"
      ? `https://www.bilibili.com/video/${id}`
      : source === "douyin"
        ? `https://www.douyin.com/video/${id}`
        : `https://www.xiaohongshu.com/explore/${id}`,
    title: `本地 AI 知识库 ${id}`,
    author: "作者",
    mediaType: "video",
    discoveryMethod: "fixture",
  };
}

function collector(
  source: ResearchSource,
  search: (input: ResearchSearchInput) => Promise<ResearchPage>,
): ResearchCollector {
  return { source, search };
}

async function waitFor(check: () => boolean): Promise<void> {
  for (let index = 0; index < 100; index += 1) {
    if (check()) return;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error("等待研究服务状态超时");
}

describe("ResearchService", () => {
  it("B 站与浏览器组并行，而两个登录平台严格串行", async () => {
    const db = createDb();
    const events: string[] = [];
    let biliStarted = false;
    let xhsFinished = false;
    const service = new ResearchService(db, {
      enqueueImports: () => [],
      collectors: {
        bilibili: collector("bilibili", async () => {
          events.push("bili:start");
          biliStarted = true;
          return { items: [item("bilibili", "BV1")], cursor: null, hasMore: false };
        }),
        xiaohongshu: collector("xiaohongshu", async () => {
          events.push("xhs:start");
          expect(biliStarted).toBe(true);
          await Promise.resolve();
          xhsFinished = true;
          events.push("xhs:end");
          return { items: [item("xiaohongshu", "x1")], cursor: null, hasMore: false };
        }),
        douyin: collector("douyin", async () => {
          expect(xhsFinished).toBe(true);
          events.push("douyin:start");
          return { items: [item("douyin", "d1")], cursor: null, hasMore: false };
        }),
      },
    });
    const run = service.createAndRun({
      topic: "本地 AI",
      dayRange: 30,
      depth: "quick",
      sources: ["xiaohongshu", "douyin", "bilibili"],
    });
    await waitFor(() => service.getDetail(run.id)?.run.status !== "collecting");
    expect(service.getDetail(run.id)?.run).toMatchObject({ status: "ready", candidateCount: 3 });
    expect(events.indexOf("douyin:start")).toBeGreaterThan(events.indexOf("xhs:end"));
    db.close();
  });

  it("单源失败保留其余候选并标为部分覆盖", async () => {
    const db = createDb();
    const service = new ResearchService(db, {
      enqueueImports: () => [],
      collectors: {
        bilibili: collector("bilibili", async () => ({ items: [item("bilibili", "BV2")], cursor: null, hasMore: false })),
        xiaohongshu: collector("xiaohongshu", async () => { throw new Error("[login_required] 需要登录"); }),
        douyin: collector("douyin", async () => ({ items: [], cursor: null, hasMore: false })),
      },
    });
    const run = service.createAndRun({ topic: "主题", dayRange: 7, depth: "quick", sources: ["xiaohongshu", "bilibili"] });
    await waitFor(() => service.getDetail(run.id)?.run.status !== "collecting");
    const detail = service.getDetail(run.id)!;
    expect(detail.run.status).toBe("partial");
    expect(detail.candidates).toHaveLength(1);
    expect(detail.sources.find((source) => source.source === "xiaohongshu")?.status).toBe("login_required");
    db.close();
  });

  it("报告拒绝未知引用、不覆盖上一版成功报告，并复用导入队列策略", async () => {
    const db = createDb();
    const enqueue = vi.fn((inputs) => inputs.map((input, index) => ({ id: `task-${index}`, ...input })) as never);
    const service = new ResearchService(db, {
      enqueueImports: enqueue,
      collectors: {
        bilibili: collector("bilibili", async () => ({ items: [item("bilibili", "BV3")], cursor: null, hasMore: false })),
        xiaohongshu: collector("xiaohongshu", async () => ({ items: [], cursor: null, hasMore: false })),
        douyin: collector("douyin", async () => ({ items: [item("douyin", "d3")], cursor: null, hasMore: false })),
      },
    });
    const run = service.createAndRun({ topic: "本地 AI", dayRange: 30, depth: "quick", sources: ["douyin", "bilibili"] });
    await waitFor(() => service.getDetail(run.id)?.run.status !== "collecting");
    service.beginReport(run.id);
    service.saveReport(run.id, "## 主要结论\n有效 [R1]", "v1");
    service.beginReport(run.id);
    expect(() => service.saveReport(run.id, "未知 [R99]", "v2")).toThrow(/不属于/);
    expect(service.getDetail(run.id)?.run.reportMarkdown).toContain("有效 [R1]");
    service.beginReport(run.id);
    expect(() => service.saveReport(run.id, "带外部链接 [R1] https://invalid.example", "v2")).toThrow(/自行输出/);
    expect(service.getDetail(run.id)?.run.reportMarkdown).toContain("有效 [R1]");

    const candidates = service.getDetail(run.id)!.candidates;
    service.enqueueCandidates(run.id, candidates.map((candidate) => candidate.id));
    expect(enqueue).toHaveBeenCalledWith(expect.arrayContaining([
      expect.objectContaining({ captureStrategy: "authenticated" }),
      expect.objectContaining({ captureStrategy: "standard" }),
    ]));
    const saved = service.saveReportToKnowledge(run.id);
    expect(service.saveReportToKnowledge(run.id)).toEqual({ itemId: saved.itemId, updated: true });
    db.close();
  });

  it("证据包每源最多十条，总量最多三十条", () => {
    const candidates = (["xiaohongshu", "douyin", "bilibili"] as const).flatMap((source) =>
      Array.from({ length: 15 }, (_, index) => ({
        ...item(source, `${source}-${index}`),
        id: `${source}-${index}`,
        runId: "run",
        normalizedUrl: `https://example.com/${source}-${index}`,
        snippet: "",
        publishedAt: null,
        dateConfidence: "low" as const,
        engagement: {},
        relevanceScore: 0,
        recencyScore: 0,
        engagementScore: 0,
        overallScore: 100 - index,
        clusterId: null,
        state: "available" as const,
        importTaskId: null,
        importedItemId: null,
        createdAt: 1,
        updatedAt: 1,
      })),
    );
    const selected = selectEvidence(candidates);
    expect(selected).toHaveLength(30);
    for (const source of ["xiaohongshu", "douyin", "bilibili"] as const) {
      expect(selected.filter((candidate) => candidate.source === source)).toHaveLength(10);
    }
  });
});
