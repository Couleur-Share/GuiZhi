import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { SourceAccessDB } from "@guizhi/db/source-access";

vi.mock("../../../src/main/services/import/connectors", () => ({
  extractContent: vi.fn(),
}));
import { extractContent } from "../../../src/main/services/import/connectors";
import { createImportService } from "../../../src/main/services/import/import-service";

const canonical = "https://www.xiaohongshu.com/explore/123";
const firstUrl = "https://xhslink.cn/o/first";
describe("导入队列持久化原始链接", () => {
  let db: Database.Database;
  let service: ReturnType<typeof createImportService>;
  beforeEach(() => {
    db = new Database(":memory:");
    db.pragma("foreign_keys = ON");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);
    service = createImportService(db, () => {});
    vi.mocked(extractContent).mockResolvedValue({
      title: "视频",
      content: "相同正文",
      itemType: "video",
      sourceUri: canonical,
      transcript: "中文文字稿",
    });
  });
  afterEach(() => db.close());

  async function imported(input: string) {
    const [task] = service.queue.enqueue([{ kind: "url", input }]);
    await service.queue.drain();
    return service.taskDb.get(task.id)!;
  }
  it("新导入保存访问链接，清理任务后仍可取回且规范链接不变", async () => {
    const task = await imported(firstUrl);
    expect(task.status).toBe("completed");
    service.taskDb.clearTerminal({ scope: "all" });
    expect(new SourceAccessDB(db).get(task.resultItemId!, canonical)).toBe(
      firstUrl,
    );
    expect(new KnowledgeItemDB(db).get(task.resultItemId!)?.sourceUri).toBe(
      canonical,
    );
  });
  it("不同分享令牌仍判为同一条目，并刷新访问入口", async () => {
    const first = await imported(firstUrl);
    const fresh =
      "https://www.xiaohongshu.com/explore/123?xsec_token=new-token";
    const second = await imported(fresh);
    expect(second.status).toBe("duplicate");
    expect(second.duplicateItemId).toBe(first.resultItemId);
    expect(db.get("SELECT COUNT(*) AS count FROM knowledge_items")).toEqual({
      count: 1,
    });
    service.taskDb.clearTerminal({ scope: "all" });
    expect(new SourceAccessDB(db).get(first.resultItemId!, canonical)).toBe(
      fresh,
    );
  });
  it("跨站正文哈希判重不会替换原网站的访问入口", async () => {
    const first = await imported(firstUrl);
    vi.mocked(extractContent).mockResolvedValue({
      title: "转载",
      content: "相同正文",
      itemType: "video",
      sourceUri: "https://example.com/video",
    });
    const second = await imported("https://example.com/video?token=other");
    expect(second.status).toBe("duplicate");
    expect(new SourceAccessDB(db).get(first.resultItemId!, canonical)).toBe(
      firstUrl,
    );
  });
});
