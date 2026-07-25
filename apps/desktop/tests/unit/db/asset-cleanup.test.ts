import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import {
  extractAllLocalAssetRefs,
  isSafeAssetFileName,
} from "@guizhi/shared/utils/media-refs";
import { resolveAssetPath } from "../../../src/main/services/asset-cleanup";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("extractAllLocalAssetRefs", () => {
  it("取出条目里的全部引用，不止第一个", () => {
    const content = [
      "![a](local-image://import-aaa.png)",
      "![b](local-image://import-bbb.jpg)",
      "<local-video://import-ccc.mp4>",
    ].join("\n");
    expect(extractAllLocalAssetRefs(content).sort()).toEqual([
      "import-aaa.png",
      "import-bbb.jpg",
      "import-ccc.mp4",
    ]);
  });

  it("重复引用去重", () => {
    const content =
      "local-image://x.png 和 local-image://x.png";
    expect(extractAllLocalAssetRefs(content)).toEqual(["x.png"]);
  });

  it("无引用与空内容返回空数组", () => {
    expect(extractAllLocalAssetRefs("普通笔记")).toEqual([]);
    expect(extractAllLocalAssetRefs("")).toEqual([]);
  });

  it("越界文件名被丢弃", () => {
    // 正文是用户可编辑的，local-image://.. 拼出来就是资产目录的父目录
    expect(extractAllLocalAssetRefs("local-image://..")).toEqual([]);
    expect(extractAllLocalAssetRefs("local-image://.hidden")).toEqual([]);
  });
});

describe("isSafeAssetFileName", () => {
  it.each(["import-abc.png", "a1.mp4", "x_y-z.jpg"])("放行 %s", (name) => {
    expect(isSafeAssetFileName(name)).toBe(true);
  });

  it.each(["..", ".", ".env", "a/b.png", "a\\b.png", "a b.png", ""])(
    "拦截 %s",
    (name) => {
      expect(isSafeAssetFileName(name)).toBe(false);
    },
  );
});

describe("resolveAssetPath", () => {
  it("非法文件名一律拒绝，不去碰文件系统", () => {
    expect(resolveAssetPath("..", ["/tmp/images"])).toBeNull();
    expect(resolveAssetPath("../../etc/passwd", ["/tmp/images"])).toBeNull();
  });

  it("目录中不存在的文件返回 null", () => {
    expect(resolveAssetPath("nope.png", ["/tmp/definitely-missing"])).toBeNull();
  });
});

describe("KnowledgeItemDB 资产引用查询", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
  });

  it("listAssetRefs 汇总多条目的引用", () => {
    const a = items.create({
      title: "图 A",
      content: "![](local-image://asset-a.png)",
    });
    const b = items.create({
      title: "视频 B",
      content: "<local-video://asset-b.mp4>",
    });
    items.create({ title: "纯文本", content: "无引用" });

    expect(items.listAssetRefs([a.id, b.id]).sort()).toEqual([
      "asset-a.png",
      "asset-b.mp4",
    ]);
  });

  it("isAssetReferenced 反映删除后的真实状态", () => {
    const first = items.create({
      title: "引用 1",
      content: "![](local-image://shared.png)",
    });
    const second = items.create({
      title: "引用 2",
      content: "![](local-image://shared.png)",
    });

    expect(items.isAssetReferenced("shared.png")).toBe(true);

    // 同一份资产被两个条目引用，删掉一个之后仍不能清理
    items.deleteForever([first.id]);
    expect(items.isAssetReferenced("shared.png")).toBe(true);

    items.deleteForever([second.id]);
    expect(items.isAssetReferenced("shared.png")).toBe(false);
  });

  it("回收站里的条目仍算引用（尚未彻底删除）", () => {
    const item = items.create({
      title: "待删",
      content: "![](local-image://pending.png)",
    });
    items.moveToTrash([item.id]);
    expect(items.isAssetReferenced("pending.png")).toBe(true);
  });
});
