import { describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { CrawlJobDB, ResearchDB, ResearchWorkflowDB } from "@guizhi/db";
import {
  collectWebResearch,
  readWebResearch,
} from "../../../src/main/services/web-capture/web-research";
import { captureWebPage } from "../../../src/main/services/web-capture/web-capture";
vi.mock("../../../src/main/services/web-capture/web-capture", () => ({
  getWebCaptureStatus: async () => ({ available: true }),
  captureWebPage: vi.fn(),
}));
vi.mock("../../../src/main/services/web-capture/robots", () => ({
  loadRobots: async () => ({ rules: [], sitemaps: [] }),
  robotsAllows: () => true,
}));
describe("指定网页研究快照", () => {
  it("不同来源的相同正文合并，精读复用本轮内容，无日期保持未知，后续网页变化不改写记录", async () => {
    const db = new Database(":memory:");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);
    try {
      const store = new ResearchDB(db),
        workflow = new ResearchWorkflowDB(db);
      const run = store.create(
        {
          topic: "中文知识库",
          sources: ["web"],
          depth: "quick",
          dayRange: 30,
          timeScope: "all",
        },
        1,
        Date.now(),
      );
      workflow.setContext(run.id, {
        seriesId: run.id,
        phase: "searching",
        policyVersion: "test",
        reportOutdated: false,
        webSeeds: [
          { url: "https://one.example/docs", mode: "page" },
          { url: "https://two.example/docs", mode: "page" },
        ],
      });
      vi.mocked(captureWebPage).mockImplementation(async (request) => ({
        taskId: request.taskId,
        entryUrl: request.url,
        finalUrl: request.url,
        title: "中文知识库",
        author: "",
        publishedAt: null,
        dateConfidence: "unknown",
        markdown: "固定的本轮正文",
        links: [],
        paragraphs: [{ id: "p1", text: "固定的本轮正文" }],
        contentHash: "same-content-hash",
        capturedAt: 1234,
        engineVersion: "crawl4ai/0.9.3",
        complete: true,
        truncated: false,
        warnings: [],
      }));
      await collectWebResearch(
        db,
        run,
        new AbortController().signal,
        () => undefined,
      );
      const candidates = store.listCandidates(run.id);
      expect(candidates).toHaveLength(1);
      expect(candidates[0].publishedAt).toBeNull();
      const doc = readWebResearch(db, candidates[0]);
      workflow.putDocument(doc);
      expect(doc.sourceUrls).toHaveLength(2);
      expect(doc.capturedAt).toBe(1234);
      expect(captureWebPage).toHaveBeenCalledTimes(2);
      expect(doc.passages[0]).toMatchObject({
        position: 0,
        externalId: "p1",
        text: "固定的本轮正文",
      });
      const jobId = workflow.context(run.id)!.webCrawlJobId!,
        jobs = new CrawlJobDB(db),
        page = jobs.pages(jobId)[0];
      jobs.save({
        ...page,
        result: { ...page.result!, markdown: "后来变化的正文" },
      });
      expect(workflow.documents(run.id)[0].passages[0].text).toBe(
        "固定的本轮正文",
      );
      expect(db.get("SELECT COUNT(*) AS n FROM knowledge_items")).toEqual({
        n: 0,
      });
    } finally {
      db.close();
    }
  });
});
