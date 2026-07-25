import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { CollectionDB } from "@guizhi/db/collection";
import {
  exportKnowledgeToMarkdown,
  sanitizeFileName,
} from "../../../src/main/services/export-markdown";

let exportDir: string;

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

function listFilesRecursive(dir: string): string[] {
  const results: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      results.push(...listFilesRecursive(fullPath));
    } else {
      results.push(fullPath);
    }
  }
  return results;
}

beforeEach(() => {
  exportDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-export-test-"));
});

afterEach(() => {
  fs.rmSync(exportDir, { recursive: true, force: true });
});

describe("sanitizeFileName", () => {
  it("清洗非法字符并限制长度", () => {
    expect(sanitizeFileName('a/b\\c:d*e?f"g<h>i|j')).toBe("a b c d e f g h i j");
    expect(sanitizeFileName("   ")).toBe("");
    expect(sanitizeFileName("末尾点与空格. . ")).toBe("末尾点与空格");
    expect(sanitizeFileName("长".repeat(100)).length).toBeLessThanOrEqual(60);
  });
});

describe("exportKnowledgeToMarkdown", () => {
  it("按集合分目录导出，frontmatter 含标题/标签/来源，回收站条目除外", () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const collections = new CollectionDB(db);

    const collection = collections.create({ name: "读书笔记" });
    const inCollection = items.create({
      title: "深度工作",
      content: "# 核心观点\n专注是稀缺能力",
      collectionId: collection.id,
      tagNames: ["效率", "阅读"],
    });
    items.create({ title: "散落笔记", content: "未分类内容" });
    const trashed = items.create({ title: "已删除", content: "不应导出" });
    items.moveToTrash([trashed.id]);

    // 来源记录（网页采集的条目会有 source_uri）
    db.run(
      `INSERT INTO source_records (id, item_id, source_type, source_uri, captured_at)
       VALUES ('sr1', ?, 'url', 'https://example.com/deep-work', ?)`,
      inCollection.id,
      Date.now(),
    );
    // 转写文本走独立小节
    items.update(inCollection.id, { transcript: "口播转写内容" });

    const stats = exportKnowledgeToMarkdown(db, exportDir);
    expect(stats.count).toBe(2);

    const files = listFilesRecursive(exportDir);
    expect(files).toHaveLength(2);

    const collectionFile = files.find((file) =>
      file.includes(`${path.sep}读书笔记${path.sep}`),
    );
    expect(collectionFile).toBeTruthy();
    const document = fs.readFileSync(collectionFile!, "utf8");
    expect(document).toContain('title: "深度工作"');
    expect(document).toContain('tags: ["效率","阅读"]');
    expect(document).toContain('collection: "读书笔记"');
    expect(document).toContain('source: "https://example.com/deep-work"');
    expect(document).toContain("# 核心观点");
    expect(document).toContain("## 转写文本");
    expect(document).toContain("口播转写内容");

    // 回收站条目不导出
    expect(
      files.some((file) => fs.readFileSync(file, "utf8").includes("不应导出")),
    ).toBe(false);
    db.close();
  });

  it("空标题回退为 untitled，文件名带 id 前缀去重", () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const created = items.create({ title: "", content: "无标题内容" });

    exportKnowledgeToMarkdown(db, exportDir);
    const files = listFilesRecursive(exportDir);
    expect(files).toHaveLength(1);
    expect(path.basename(files[0])).toBe(
      `untitled-${created.id.slice(0, 8)}.md`,
    );
    db.close();
  });
});
