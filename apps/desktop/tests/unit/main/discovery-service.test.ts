import { describe, expect, it, vi } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import type { PlatformDiscoveryItem } from "@guizhi/shared/types";
import { BackgroundJobRuntime } from "../../../src/main/services/background-jobs";
import { DiscoveryService } from "../../../src/main/services/discovery/discovery-service";
import { PlatformCaptureError } from "../../../src/main/services/platform-capture/browser-capture";

function createDb() {
  const db = new DatabaseAdapter(":memory:");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

function item(id: string): PlatformDiscoveryItem {
  return {
    platform: "xiaohongshu",
    externalId: id,
    url: `https://www.xiaohongshu.com/explore/${id}`,
    title: `条目 ${id}`,
    author: "作者",
    mediaType: "image",
  };
}

describe("DiscoveryService", () => {
  it("分页扫描后生成候选，正常完成清空游标并加入随机抖动", async () => {
    const db = createDb();
    const now = 1_000;
    const collector = {
      collect: vi
        .fn()
        .mockResolvedValueOnce({ items: [item("1"), item("2")], cursor: "20", hasMore: true })
        .mockResolvedValueOnce({ items: [item("3")], cursor: null, hasMore: false }),
    };
    const runtime = new BackgroundJobRuntime(db, {
      now: () => now,
      ownerId: "test",
      sendRendererJob: vi.fn(),
    });
    const notifications = vi.fn();
    const service = new DiscoveryService(
      db,
      collector,
      runtime,
      { now: () => now, random: () => 0.5, notify: notifications },
    );
    const view = service.save({
      name: "每日发现",
      platform: "xiaohongshu",
      mode: "keyword",
      query: "知识",
      intervalMinutes: 1440,
      enabled: true,
    });

    const result = await service.run(view.id);
    expect(collector.collect).toHaveBeenNthCalledWith(1, expect.anything(), null);
    expect(collector.collect).toHaveBeenNthCalledWith(2, expect.anything(), "20");
    expect(result).toMatchObject({ newCandidates: 3, run: { state: "completed", cursor: null } });
    expect(result.view.nextRunAt).toBe(now + 1440 * 60_000 + 5 * 60_000);
    expect(service.getDetail(view.id)?.candidates).toHaveLength(3);
    expect(notifications).toHaveBeenCalledWith(expect.anything(), 3, false);
  });

  it("登录失效保留游标并把视图置为 login_required", async () => {
    const db = createDb();
    const now = 5_000;
    const runtime = new BackgroundJobRuntime(db, {
      now: () => now,
      ownerId: "test",
      sendRendererJob: vi.fn(),
    });
    const collector = {
      collect: vi
        .fn()
        .mockResolvedValueOnce({ items: [item("1")], cursor: "20", hasMore: true })
        .mockRejectedValueOnce(new PlatformCaptureError("login_required", "请登录")),
    };
    const notify = vi.fn();
    const service = new DiscoveryService(db, collector, runtime, {
      now: () => now,
      notify,
    });
    const view = service.save({
      name: "关注",
      platform: "xiaohongshu",
      mode: "keyword",
      query: "知识",
      enabled: false,
    });

    await expect(service.run(view.id)).rejects.toMatchObject({ code: "login_required" });
    expect(service.getDetail(view.id)?.view.state).toBe("login_required");
    expect(service.getDetail(view.id)?.runs[0]).toMatchObject({ cursor: "20", state: "failed" });
    expect(notify).toHaveBeenCalledTimes(1);
  });
});
