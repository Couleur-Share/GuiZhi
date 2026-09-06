import { afterEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB, webContentHash } from "@guizhi/db";
import { CrawlService } from "../../../src/main/services/web-capture/crawl-service";
import {
  captureWebPage,
  getWebCaptureStatus,
} from "../../../src/main/services/web-capture/web-capture";
import { loadRobots } from "../../../src/main/services/web-capture/robots";
vi.mock("../../../src/main/services/web-capture/web-capture", () => ({
  captureWebPage: vi.fn(),
  getWebCaptureStatus: vi.fn(),
}));
vi.mock("../../../src/main/services/web-capture/robots", async (original) => ({
  ...(await original<
    typeof import("../../../src/main/services/web-capture/robots")
  >()),
  loadRobots: vi.fn(),
}));
const databases: Database[] = [];
afterEach(() => {
  databases.splice(0).forEach((db) => db.close());
  vi.clearAllMocks();
});
function fixture() {
  const db = new Database(":memory:");
  databases.push(db);
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  vi.mocked(getWebCaptureStatus).mockResolvedValue({
    available: true,
    supported: true,
    running: false,
    version: "0.9.3",
    runtimeTarget: "test",
  });
  vi.mocked(loadRobots).mockResolvedValue({ rules: [], sitemaps: [] });
  vi.mocked(captureWebPage).mockImplementation(async (request) => ({
    taskId: request.taskId,
    entryUrl: request.url,
    finalUrl: request.url,
    title: "知识库文档",
    author: "",
    publishedAt: null,
    dateConfidence: "unknown",
    markdown: "知识库正文",
    links: [
      "https://example.com/docs/b",
      "https://example.com/docs/a",
      "https://example.com/docs2/no",
      "https://outside.com/docs/no",
    ],
    paragraphs: [],
    contentHash: webContentHash("知识库正文"),
    capturedAt: Date.now(),
    engineVersion: "0.9.3",
    complete: true,
    truncated: false,
    warnings: [],
  }));
  return { db, service: new CrawlService(db) };
}
describe("有限文档站批次", () => {
  it("循环链接不重复发现，同源目录限制且重复批次不重复入库", async () => {
    const { db, service } = fixture();
    const input = {
      purpose: "documents" as const,
      seeds: [
        { url: "https://example.com/docs/a", mode: "directory" as const },
      ],
      maxPages: 3,
      maxDepth: 2,
    };
    const first = service.jobs.create(input);
    await service.resume(first.id);
    expect(service.jobs.pages(first.id)).toHaveLength(2);
    expect(service.jobs.get(first.id)?.counts.added).toBe(2);
    expect(new KnowledgeItemDB(db).counts().byPlatform.web).toBe(2);
    const second = service.jobs.create(input);
    await service.resume(second.id);
    expect(service.jobs.get(second.id)?.counts.duplicate).toBe(2);
    expect(db.get("SELECT COUNT(*) AS n FROM knowledge_items")).toEqual({
      n: 2,
    });
  });
  it("robots 读取失败暂停，恢复继续持久队列；失败不删除本地正文", async () => {
    const { db, service } = fixture(),
      items = new KnowledgeItemDB(db);
    items.create({ title: "本地", content: "不可丢失" });
    const job = service.jobs.create({
      purpose: "documents",
      seeds: [{ url: "https://example.com/docs/a", mode: "directory" }],
    });
    vi.mocked(loadRobots).mockRejectedValueOnce(new Error("robots 不可用"));
    await service.resume(job.id);
    expect(service.jobs.get(job.id)?.status).toBe("paused");
    expect(captureWebPage).not.toHaveBeenCalled();
    await service.resume(job.id);
    expect(service.jobs.get(job.id)?.status).toBe("completed");
    expect(
      db.get(
        "SELECT COUNT(*) AS n FROM knowledge_items WHERE content='不可丢失'",
      ),
    ).toEqual({ n: 1 });
  });
  it("单页不展开链接，部分失败逐页保存可重试", async () => {
    const { service } = fixture();
    const job = service.jobs.create({
      purpose: "documents",
      seeds: [
        { url: "https://example.com/docs/a", mode: "page" },
        { url: "https://example.com/docs/b", mode: "page" },
      ],
    });
    vi.mocked(captureWebPage).mockRejectedValueOnce(new Error("HTTP 503"));
    await service.resume(job.id);
    expect(service.jobs.get(job.id)?.counts.failed).toBe(1);
    expect(service.jobs.get(job.id)?.counts.added).toBe(1);
    service.jobs.retry(job.id);
    await service.resume(job.id);
    expect(service.jobs.get(job.id)?.counts.failed).toBeUndefined();
    expect(service.jobs.pages(job.id)).toHaveLength(2);
  });
});
