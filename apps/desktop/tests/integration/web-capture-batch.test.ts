import { describe, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { CrawlService } from "../../src/main/services/web-capture/crawl-service";
import {
  getWebCaptureStatus,
  shutdownWebCapture,
} from "../../src/main/services/web-capture/web-capture";
import { configureRuntimePaths } from "../../src/main/runtime-paths";

vi.mock("electron", () => ({
  app: { isPackaged: false, getAppPath: () => process.cwd() },
}));
vi.mock("../../src/main/services/web-capture/web-network", () => ({
  webTextRequest: async () => ({
    status: 404,
    text: "",
    url: "https://fixture.example/robots.txt",
  }),
  webNetworkRequest: async (request: { url: string }) => {
    const page = Number(new URL(request.url).pathname.split("/").pop());
    const html = `<meta charset="utf-8"><title>文档 ${page}</title><article><h1>文档 ${page}</h1><p>第 ${page} 页独立正文</p><a href="/docs/${page + 1}">下一页</a><a href="/docs/1">首页</a></article>`;
    return {
      status: 200,
      headers: { "content-type": "text/html; charset=utf-8" },
      body: Buffer.from(html).toString("base64"),
    };
  },
}));
describe.skipIf(process.env.GUIZHI_TEST_50_PAGE_BATCH !== "1")(
  "随包运行时的 50 页完整批次",
  () => {
    it("持久化一页一条、预算停止、等待空闲退出，重复导入幂等", async () => {
      const directory = await fs.mkdtemp(
        path.join(os.tmpdir(), "guizhi-web-batch-test-"),
      );
      configureRuntimePaths({ userDataPath: directory });
      const db = new Database(path.join(directory, "test.db"));
      db.pragma("foreign_keys=ON");
      db.exec(SCHEMA_TABLES);
      db.exec(SCHEMA_INDEXES);
      const service = new CrawlService(db),
        start = Date.now();
      try {
        const job = service.jobs.create({
          purpose: "documents",
          seeds: [{ url: "https://fixture.example/docs/1", mode: "directory" }],
          maxPages: 50,
          maxDepth: 5,
        });
        // 预先持久化有限发现队列，测量 50 页处理而不依赖线性网站深度。
        for (let i = 2; i <= 50; i++)
          service.jobs.discover(
            job.id,
            `https://fixture.example/docs/${i}`,
            1,
            0,
          );
        await service.resume(job.id);
        expect(service.jobs.get(job.id)?.counts.added).toBe(50);
        expect(service.jobs.pages(job.id)).toHaveLength(50);
        expect(db.get("SELECT COUNT(*) AS n FROM knowledge_items")).toEqual({
          n: 50,
        });
        expect(db.get("SELECT COUNT(*) AS n FROM web_source_versions")).toEqual(
          { n: 50 },
        );
        expect(db.pragma("foreign_key_check")).toEqual([]);
        const completedMs = Date.now() - start;
        await vi.waitFor(
          async () => expect((await getWebCaptureStatus()).running).toBe(false),
          { timeout: 70_000, interval: 1000 },
        );
        const cache = path.join(directory, "cache/web-capture");
        await vi.waitFor(
          async () => expect(await fs.readdir(cache)).toEqual([]),
          { timeout: 10_000, interval: 500 },
        );
        await fs.mkdir(path.resolve("../../artifacts/crawl4ai"), {
          recursive: true,
        });
        await fs.writeFile(
          path.resolve("../../artifacts/crawl4ai/batch-metrics.json"),
          JSON.stringify(
            {
              pages: 50,
              completedMs,
              idleExitMs: Date.now() - start - completedMs,
              remainingCacheDirectories: 0,
              platform: process.platform,
              arch: process.arch,
            },
            null,
            2,
          ),
        );
      } finally {
        await service.close();
        await shutdownWebCapture();
        db.close();
        await fs.rm(directory, { recursive: true, force: true });
      }
    }, 600_000);
  },
);
