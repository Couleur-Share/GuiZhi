import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { PlatformParseError } from "@guizhi/shared/utils/platform-parse-error";
vi.mock("../../../src/main/services/import/video-url", () => ({
  downloadVideoAudio: vi.fn(),
}));
import { downloadVideoAudio } from "../../../src/main/services/import/video-url";
import { downloadItemVideoAudio } from "../../../src/main/services/media/video-retranscription";

const canonical = "https://www.xiaohongshu.com/explore/123";
const share = "https://xhslink.cn/o/first";
describe("重转写读取访问入口", () => {
  let db: Database.Database;
  let item: { id: string; sourceUri: string };
  beforeEach(() => {
    db = new Database(":memory:");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);
    item = {
      ...new KnowledgeItemDB(db).create({ title: "视频" }),
      sourceUri: canonical,
    };
    db.run(
      "INSERT INTO source_records (id,item_id,source_type,source_uri,access_uri,captured_at) VALUES ('source',?,'url',?,?,1)",
      item.id,
      canonical,
      share,
    );
    vi.mocked(downloadVideoAudio)
      .mockReset()
      .mockResolvedValue({ dir: "temp", filePath: "temp/video.mp4" });
  });
  afterEach(() => db.close());
  const deps = { getYtDlpPath: () => null };
  it("使用保存的原始分享链接，无须导入任务仍存在", async () => {
    await downloadItemVideoAudio(db, item, deps);
    expect(downloadVideoAudio).toHaveBeenCalledWith(share, "xiaohongshu", deps);
  });
  it.each([
    null,
    "not-a-url",
    "https://www.youtube.com/watch?v=other",
    "file:///tmp/video.mp4",
  ])("缺失或不匹配的入口 %s 回落到规范链接", async (value) => {
    db.run("UPDATE source_records SET access_uri=?", value);
    await downloadItemVideoAudio(db, item, deps);
    expect(downloadVideoAudio).toHaveBeenCalledWith(
      canonical,
      "xiaohongshu",
      deps,
    );
  });
  it("令牌失效给出更新访问入口的操作路径，不静默回退或删除原链接", async () => {
    vi.mocked(downloadVideoAudio).mockRejectedValue(
      new PlatformParseError("token_invalid", "原始错误"),
    );
    await expect(downloadItemVideoAudio(db, item, deps)).rejects.toThrow(
      "将新链接重新导入以更新访问入口",
    );
    expect(downloadVideoAudio).toHaveBeenCalledTimes(1);
    expect(db.get("SELECT access_uri FROM source_records")).toEqual({
      access_uri: share,
    });
  });
  it("网络故障保留原始原因，不误报令牌失效", async () => {
    vi.mocked(downloadVideoAudio).mockRejectedValue(new Error("连接超时"));
    await expect(downloadItemVideoAudio(db, item, deps)).rejects.toThrow(
      "连接超时",
    );
  });
});
