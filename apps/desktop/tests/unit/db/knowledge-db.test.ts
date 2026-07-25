import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { CollectionDB } from "@guizhi/db/collection";
import { TagDB } from "@guizhi/db/tag";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("KnowledgeItemDB", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
  });

  it("创建并读取条目（含自动建标签）", () => {
    const created = items.create({
      title: "第一条笔记",
      content: "# 你好\n这是归知的第一条笔记",
      tagNames: ["测试", "笔记"],
    });

    expect(created.id).toBeTruthy();
    expect(created.status).toBe("inbox");
    expect(created.itemType).toBe("note");
    expect(created.tags.map((tag) => tag.name).sort()).toEqual([
      "测试",
      "笔记",
    ]);

    const loaded = items.get(created.id);
    expect(loaded?.title).toBe("第一条笔记");
    expect(loaded?.content).toContain("你好");
  });

  it("中文全文检索按字命中", () => {
    items.create({ title: "归知介绍", content: "本地优先的个人知识库" });
    items.create({ title: "无关条目", content: "completely unrelated" });

    const hit = items.list({ scope: "all", search: "知识库" });
    expect(hit.total).toBe(1);
    expect(hit.entries[0].title).toBe("归知介绍");

    const titleHit = items.list({ scope: "all", search: "归知" });
    expect(titleHit.total).toBe(1);

    const miss = items.list({ scope: "all", search: "不存在的词组" });
    expect(miss.total).toBe(0);
  });

  it("英文前缀检索命中", () => {
    items.create({ title: "Tech note", content: "Electron and React stack" });
    const result = items.list({ scope: "all", search: "elec" });
    expect(result.total).toBe(1);
  });

  it("标签全文检索命中", () => {
    items.create({ title: "打标签的条目", content: "正文", tagNames: ["前端"] });
    const result = items.list({ scope: "all", search: "前端" });
    expect(result.total).toBe(1);
  });

  it("scope 过滤与排序（置顶在前）", () => {
    const a = items.create({ title: "A", status: "ready" });
    items.create({ title: "B", status: "inbox" });
    const c = items.create({ title: "C", status: "ready" });
    items.update(c.id, { status: "archived" });
    items.update(a.id, { isPinned: true });

    expect(items.list({ scope: "inbox" }).total).toBe(1);
    expect(items.list({ scope: "archived" }).total).toBe(1);
    const all = items.list({ scope: "all" });
    expect(all.total).toBe(2);
    expect(all.entries[0].title).toBe("A");

    items.update(a.id, { isFavorite: true });
    expect(items.list({ scope: "favorites" }).total).toBe(1);
  });

  it("排序参数生效，置顶始终在最前", () => {
    const first = items.create({ title: "Banana", status: "ready" });
    const second = items.create({ title: "apple", status: "ready" });
    const third = items.create({ title: "Cherry", status: "ready" });

    // 同毫秒创建会让时间排序不确定，这里手工拉开时间戳
    for (const [index, id] of [first.id, second.id, third.id].entries()) {
      const stamp = (index + 1) * 1000;
      db.run(
        "UPDATE knowledge_items SET created_at = ?, updated_at = ? WHERE id = ?",
        stamp,
        stamp,
        id,
      );
    }

    // 默认：按更新时间倒序（时间戳最大的在最前）
    expect(
      items.list({ scope: "all" }).entries.map((entry) => entry.title),
    ).toEqual(["Cherry", "apple", "Banana"]);

    // 标题升序忽略大小写
    expect(
      items
        .list({ scope: "all", sortBy: "title", sortOrder: "asc" })
        .entries.map((entry) => entry.title),
    ).toEqual(["apple", "Banana", "Cherry"]);

    expect(
      items
        .list({ scope: "all", sortBy: "title", sortOrder: "desc" })
        .entries.map((entry) => entry.title),
    ).toEqual(["Cherry", "Banana", "apple"]);

    // 创建时间升序
    expect(
      items
        .list({ scope: "all", sortBy: "createdAt", sortOrder: "asc" })
        .entries.map((entry) => entry.id),
    ).toEqual([first.id, second.id, third.id]);

    // 置顶条目不受排序字段影响，始终排在最前
    items.update(third.id, { isPinned: true });
    expect(
      items
        .list({ scope: "all", sortBy: "title", sortOrder: "asc" })
        .entries.map((entry) => entry.title),
    ).toEqual(["Cherry", "apple", "Banana"]);
  });

  it("搜索时忽略排序参数，按相关度返回", () => {
    items.create({ title: "zzz 归知 归知 归知", content: "归知 归知" });
    items.create({ title: "aaa 其他", content: "只提到一次归知" });

    const result = items.list({
      scope: "all",
      search: "归知",
      sortBy: "title",
      sortOrder: "asc",
    });
    expect(result.total).toBe(2);
    expect(result.entries[0].title).toBe("zzz 归知 归知 归知");
  });

  it("标题命中排在正文命中之前（bm25 列权重）", () => {
    // 两条文档长度相近、各命中一次，排序只由列权重决定。
    // bm25 的权重按列序位置映射，UNINDEXED 的 item_id 也占一位——
    // 漏掉那个占位会让权重整体错开一列，变成正文优先。
    items.create({ title: "斑马", content: "这段正文与关键词毫无关系" });
    items.create({ title: "毫无关系的标题", content: "这段正文提到了斑马" });

    const result = items.list({ scope: "all", search: "斑马" });
    expect(result.total).toBe(2);
    expect(result.entries[0].title).toBe("斑马");
  });

  it("按集合与标签过滤", () => {
    const collections = new CollectionDB(db);
    const work = collections.create({ name: "工作" });
    items.create({ title: "in-collection", collectionId: work.id });
    items.create({ title: "loose", tagNames: ["only-tag"] });

    const byCollection = items.list({ scope: "all", collectionId: work.id });
    expect(byCollection.total).toBe(1);
    expect(byCollection.entries[0].title).toBe("in-collection");

    const tag = new TagDB(db).findByName("only-tag");
    const byTag = items.list({ scope: "all", tagId: tag!.id });
    expect(byTag.total).toBe(1);
    expect(byTag.entries[0].title).toBe("loose");
  });

  it("软删除后不出现在常规视图与搜索，恢复后回归", () => {
    const item = items.create({ title: "要删的", content: "删除测试正文" });
    items.moveToTrash([item.id]);

    expect(items.list({ scope: "all" }).total).toBe(0);
    expect(items.list({ scope: "trash" }).total).toBe(1);
    expect(items.list({ scope: "all", search: "删除测试" }).total).toBe(0);

    items.restore([item.id]);
    expect(items.list({ scope: "all" }).total).toBe(1);
    expect(items.list({ scope: "all", search: "删除测试" }).total).toBe(1);
  });

  it("回收站范围内可以搜索（找回误删条目）", () => {
    const keep = items.create({ title: "留着的", content: "会议纪要" });
    const trashed = items.create({ title: "误删的", content: "会议纪要草稿" });
    items.moveToTrash([trashed.id]);

    const inTrash = items.list({ scope: "trash", search: "会议纪要" });
    expect(inTrash.total).toBe(1);
    expect(inTrash.entries[0].id).toBe(trashed.id);

    // 回收站条目保留索引不会污染常规检索
    const normal = items.list({ scope: "all", search: "会议纪要" });
    expect(normal.total).toBe(1);
    expect(normal.entries[0].id).toBe(keep.id);
  });

  it("backfillMissingFtsRows 补齐老库里缺索引的回收站条目", () => {
    const item = items.create({ title: "老库遗留", content: "会议纪要草稿" });
    items.moveToTrash([item.id]);
    // 模拟旧版本行为：软删时把索引行删掉
    db.run("DELETE FROM knowledge_fts WHERE item_id = ?", item.id);
    expect(items.list({ scope: "trash", search: "会议纪要" }).total).toBe(0);

    expect(items.backfillMissingFtsRows()).toBe(1);
    expect(items.list({ scope: "trash", search: "会议纪要" }).total).toBe(1);
    // 幂等：没有缺失时不重复写
    expect(items.backfillMissingFtsRows()).toBe(0);
  });

  it("回收站条目彻底删除后移出索引", () => {
    const item = items.create({ title: "彻底删除", content: "索引清理验证" });
    items.moveToTrash([item.id]);
    items.deleteForever([item.id]);

    expect(items.list({ scope: "trash", search: "索引清理" }).total).toBe(0);
    expect(items.list({ scope: "all", search: "索引清理" }).total).toBe(0);
  });

  it("彻底删除与清空回收站", () => {
    const a = items.create({ title: "A" });
    const b = items.create({ title: "B" });
    items.moveToTrash([a.id, b.id]);

    expect(items.deleteForever([a.id])).toBe(1);
    expect(items.get(a.id)).toBeNull();
    expect(items.emptyTrash()).toBe(1);
    expect(items.list({ scope: "trash" }).total).toBe(0);
  });

  it("更新替换标签并同步 FTS", () => {
    const item = items.create({ title: "T", tagNames: ["旧标签"] });
    items.update(item.id, { tagNames: ["新标签"] });

    const updated = items.get(item.id);
    expect(updated?.tags.map((tag) => tag.name)).toEqual(["新标签"]);
    expect(items.list({ scope: "all", search: "新标签" }).total).toBe(1);
    expect(items.list({ scope: "all", search: "旧标签" }).total).toBe(0);
  });

  it("counts 汇总正确", () => {
    const collections = new CollectionDB(db);
    const col = collections.create({ name: "集合" });
    const a = items.create({ title: "a", status: "inbox", collectionId: col.id });
    items.create({ title: "b", status: "ready", tagNames: ["t1"] });
    const c = items.create({ title: "c", status: "ready" });
    items.update(a.id, { isFavorite: true });
    items.update(c.id, { status: "archived" });
    const d = items.create({ title: "d" });
    items.moveToTrash([d.id]);

    const counts = items.counts();
    expect(counts.inbox).toBe(1);
    expect(counts.all).toBe(2);
    expect(counts.favorites).toBe(1);
    expect(counts.archived).toBe(1);
    expect(counts.trash).toBe(1);
    expect(counts.byCollection[col.id]).toBe(1);
    const tag = new TagDB(db).findByName("t1");
    expect(counts.byTag[tag!.id]).toBe(1);
  });
});

describe("CollectionDB / TagDB", () => {
  let db: DatabaseAdapter.Database;

  beforeEach(() => {
    db = createTestDb();
  });

  it("集合 CRUD 与条目计数", () => {
    const collections = new CollectionDB(db);
    const items = new KnowledgeItemDB(db);

    const created = collections.create({ name: "读书" });
    items.create({ title: "x", collectionId: created.id });

    const listed = collections.list();
    expect(listed).toHaveLength(1);
    expect(listed[0].itemCount).toBe(1);

    collections.update(created.id, { name: "读书笔记" });
    expect(collections.get(created.id)?.name).toBe("读书笔记");

    // 删除集合后条目保留、collection_id 置空
    expect(collections.delete(created.id)).toBe(true);
    const orphan = items.list({ scope: "all" });
    expect(orphan.total).toBe(1);
    expect(orphan.entries[0].collectionId).toBeNull();
  });

  it("标签重名（忽略大小写）返回已有标签，改名冲突报错", () => {
    const tags = new TagDB(db);
    const first = tags.create({ name: "Reading" });
    const dup = tags.create({ name: "reading" });
    expect(dup.id).toBe(first.id);

    tags.create({ name: "Other" });
    expect(() => tags.update(first.id, { name: "other" })).toThrow();
  });
});
