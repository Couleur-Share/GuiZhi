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

let sourceSeq = 0;

/** 补一条来源记录（正常由采集管线写入，DAO 层测试里手工造） */
function addSource(
  db: DatabaseAdapter.Database,
  itemId: string,
  sourceType: string,
  platform: string | null,
): void {
  sourceSeq += 1;
  db.run(
    `INSERT INTO source_records
       (id, item_id, source_type, source_uri, platform, captured_at)
     VALUES (?, ?, ?, ?, ?, ?)`,
    `src-${sourceSeq}`,
    itemId,
    sourceType,
    null,
    platform,
    Date.now(),
  );
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
    expect(created.status).toBe("active");
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
    items.create({
      title: "打标签的条目",
      content: "正文",
      tagNames: ["前端"],
    });
    const result = items.list({ scope: "all", search: "前端" });
    expect(result.total).toBe(1);
  });

  it("scope 过滤与排序（置顶在前）", () => {
    const collection = new CollectionDB(db).create({ name: "集合" });
    const a = items.create({ title: "A", collectionId: collection.id });
    items.create({ title: "B" });
    const c = items.create({ title: "C", collectionId: collection.id });
    items.update(c.id, { status: "archived" });
    items.update(a.id, { isPinned: true });

    // 未分类 = 没归入任何集合且未归档；B 是唯一一条
    expect(items.list({ scope: "uncategorized" }).total).toBe(1);
    expect(items.list({ scope: "uncategorized" }).entries[0].title).toBe("B");
    expect(items.list({ scope: "archived" }).total).toBe(1);
    const all = items.list({ scope: "all" });
    expect(all.total).toBe(2);
    expect(all.entries[0].title).toBe("A");

    items.update(a.id, { isFavorite: true });
    expect(items.list({ scope: "favorites" }).total).toBe(1);
  });

  it("未分类不含归档条目：归档过的无集合条目不再回到待整理队列", () => {
    const kept = items.create({ title: "还没归档" });
    const archived = items.create({ title: "归档了" });
    items.update(archived.id, { status: "archived" });

    const result = items.list({ scope: "uncategorized" });
    expect(result.total).toBe(1);
    expect(result.entries[0].id).toBe(kept.id);
  });

  it("排序参数生效，置顶始终在最前", () => {
    const first = items.create({ title: "Banana" });
    const second = items.create({ title: "apple" });
    const third = items.create({ title: "Cherry" });

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
    const a = items.create({ title: "a", collectionId: col.id });
    items.create({ title: "b", tagNames: ["t1"] });
    const c = items.create({ title: "c" });
    items.update(a.id, { isFavorite: true });
    items.update(c.id, { status: "archived" });
    const d = items.create({ title: "d" });
    items.moveToTrash([d.id]);

    const counts = items.counts();
    // a 有集合、c 已归档、d 在回收站，只剩 b 是待整理
    expect(counts.uncategorized).toBe(1);
    expect(counts.all).toBe(2);
    expect(counts.favorites).toBe(1);
    expect(counts.archived).toBe(1);
    expect(counts.trash).toBe(1);
    expect(counts.byCollection[col.id]).toBe(1);
    const tag = new TagDB(db).findByName("t1");
    expect(counts.byTag[tag!.id]).toBe(1);
  });

  it("集合与标签计数排除归档，与点进去看到的列表同口径", () => {
    const col = new CollectionDB(db).create({ name: "集合" });
    items.create({ title: "留着", collectionId: col.id, tagNames: ["t1"] });
    const archived = items.create({
      title: "归档掉",
      collectionId: col.id,
      tagNames: ["t1"],
    });
    items.update(archived.id, { status: "archived" });

    const counts = items.counts();
    const tag = new TagDB(db).findByName("t1");
    expect(counts.byCollection[col.id]).toBe(1);
    expect(counts.byTag[tag!.id]).toBe(1);
    expect(items.list({ scope: "all", collectionId: col.id }).total).toBe(1);
    expect(items.list({ scope: "all", tagId: tag!.id }).total).toBe(1);
  });

  it("按来源平台过滤与计数", () => {
    const douyin = items.create({ title: "抖音条目" });
    const bilibili = items.create({ title: "B 站条目" });
    const trashed = items.create({ title: "删掉的抖音条目" });
    items.create({ title: "手工笔记" });
    addSource(db, douyin.id, "url", "douyin");
    addSource(db, bilibili.id, "url", "bilibili");
    addSource(db, trashed.id, "url", "douyin");
    items.moveToTrash([trashed.id]);

    const counts = items.counts();
    expect(counts.byPlatform).toEqual({ douyin: 1, bilibili: 1 });

    const byPlatform = items.list({ scope: "all", platform: "douyin" });
    expect(byPlatform.total).toBe(1);
    expect(byPlatform.entries[0].title).toBe("抖音条目");
    // 列表投影带出平台，表格的「来源」列据此渲染
    expect(byPlatform.entries[0].platform).toBe("douyin");
    const manual = items
      .list({ scope: "all" })
      .entries.find((entry) => entry.title === "手工笔记");
    expect(manual?.platform).toBeNull();
    // 没有来源记录的手工条目不属于任何平台
    expect(items.list({ scope: "all", platform: "web" }).total).toBe(0);
  });

  it("组合筛选时按分面组联动计数，而非继续显示全局总数", () => {
    const collections = new CollectionDB(db);
    const programming = collections.create({ name: "编程开发" });
    const life = collections.create({ name: "生活" });
    const both = items.create({
      title: "编程开发的 B 站视频",
      collectionId: programming.id,
      tagNames: ["前端"],
    });
    const programmingDouyin = items.create({
      title: "编程开发的抖音视频",
      collectionId: programming.id,
      tagNames: ["后端"],
    });
    const lifeBilibili = items.create({
      title: "生活的 B 站视频",
      collectionId: life.id,
      tagNames: ["生活"],
    });
    addSource(db, both.id, "url", "bilibili");
    addSource(db, programmingDouyin.id, "url", "douyin");
    addSource(db, lifeBilibili.id, "url", "bilibili");

    const tags = new TagDB(db);
    const frontend = tags.findByName("前端")!;
    const backend = tags.findByName("后端")!;
    const lifestyle = tags.findByName("生活")!;
    const counts = items.counts({
      scope: "all",
      collectionId: programming.id,
      platform: "bilibili",
    });

    // 截图里的“编程开发 + 哔哩哔哩”情形：两个已选行均为 1。
    expect(counts.byCollection).toEqual({
      [programming.id]: 1,
      [life.id]: 1,
    });
    expect(counts.byPlatform).toEqual({ bilibili: 1, douyin: 1 });
    // 标签组没有自身选项，因此同时受知识库与平台约束。
    expect(counts.byTag).toEqual({ [frontend.id]: 1 });
    expect(counts.byTag[backend.id]).toBeUndefined();
    expect(counts.byTag[lifestyle.id]).toBeUndefined();
    // 顶部范围仍是跨筛选的全局概览。
    expect(counts.all).toBe(3);
  });

  it("同一条目的多条来源记录不会让它在列表里重复出现", () => {
    const item = items.create({ title: "重复采集过的条目" });
    addSource(db, item.id, "url", "bilibili");
    addSource(db, item.id, "url", "bilibili");

    expect(items.counts().byPlatform.bilibili).toBe(1);
    expect(items.list({ scope: "all", platform: "bilibili" }).total).toBe(1);
  });

  it("按平台筛选时来源列显示命中的来源，而非另一条更新的来源", () => {
    const item = items.create({ title: "多来源条目" });
    addSource(db, item.id, "url", "douyin");
    addSource(db, item.id, "url", "web");
    db.run(
      "UPDATE source_records SET captured_at = ? WHERE item_id = ? AND platform = ?",
      1_000,
      item.id,
      "douyin",
    );
    db.run(
      "UPDATE source_records SET captured_at = ? WHERE item_id = ? AND platform = ?",
      2_000,
      item.id,
      "web",
    );

    // 默认浏览仍显示最新来源；筛选态则必须与筛选条件同口径。
    expect(items.list({ scope: "all" }).entries[0].platform).toBe("web");
    expect(
      items.list({ scope: "all", platform: "douyin" }).entries[0].platform,
    ).toBe("douyin");
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

describe("FTS rowid 映射", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;

  const mapRows = () =>
    db.all("SELECT item_id, fts_rowid FROM knowledge_fts_map") as Array<{
      item_id: string;
      fts_rowid: number;
    }>;
  const ftsCount = () =>
    (db.get("SELECT COUNT(*) AS c FROM knowledge_fts") as { c: number }).c;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
  });

  it("写入登记映射，反复更新不会留下多余索引行", () => {
    const item = items.create({ title: "初稿", content: "第一版内容" });
    expect(mapRows()).toHaveLength(1);
    expect(ftsCount()).toBe(1);

    for (let round = 0; round < 5; round++) {
      items.update(item.id, { content: `第 ${round} 版内容` });
    }
    expect(mapRows()).toHaveLength(1);
    expect(ftsCount()).toBe(1);
    // 旧内容不该还能搜到
    expect(items.list({ scope: "all", search: "第一版" }).total).toBe(0);
    expect(items.list({ scope: "all", search: "第 4 版" }).total).toBe(1);
  });

  it("彻底删除后索引与映射一起清掉", () => {
    const item = items.create({ title: "待删", content: "内容" });
    items.moveToTrash([item.id]);
    items.deleteForever([item.id]);

    expect(mapRows()).toHaveLength(0);
    expect(ftsCount()).toBe(0);
  });

  it("老库没有映射时按 item_id 兜底，backfill 后补齐", () => {
    const item = items.create({ title: "老条目", content: "老内容" });
    // 模拟映射表加入之前建的库
    db.run("DELETE FROM knowledge_fts_map");

    // 兜底路径要能正确替换索引行，不能留下两条
    items.update(item.id, { content: "新内容" });
    expect(ftsCount()).toBe(1);
    expect(items.list({ scope: "all", search: "老内容" }).total).toBe(0);

    db.run("DELETE FROM knowledge_fts_map");
    items.backfillMissingFtsRows();
    expect(mapRows()).toHaveLength(1);
  });

  it("rebuildFtsIndex 重建后映射与索引数量一致", () => {
    items.create({ title: "甲", content: "一" });
    items.create({ title: "乙", content: "二" });

    expect(items.rebuildFtsIndex()).toBe(2);
    expect(ftsCount()).toBe(2);
    expect(mapRows()).toHaveLength(2);
    expect(items.list({ scope: "all", search: "甲" }).total).toBe(1);
  });
});

describe("KnowledgeItemDB.list 只取摘要所需的正文前缀", () => {
  it("列表不返回完整正文，详情仍然完整", () => {
    const db = createTestDb();
    const items = new KnowledgeItemDB(db);
    const tail = "结尾标记";
    const created = items.create({
      title: "长文",
      content: `${"填充。".repeat(3000)}${tail}`,
    });

    const entry = items.list({ scope: "all" }).entries[0];
    expect(entry.snippet.length).toBeLessThanOrEqual(161);
    expect(entry.snippet).not.toContain(tail);

    // get() 走的是另一条路径，必须拿到全文
    expect(items.get(created.id)!.content).toContain(tail);
  });
});

describe("摘要剥掉开头的元数据引用块", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
  });

  function snippetOf(content: string): string {
    items.create({ title: "t", content });
    return items.list({ scope: "all" }).entries[0].snippet;
  }

  it("视频条目的摘要从正文开始，不被平台/作者/简介占满", () => {
    const snippet = snippetOf(
      [
        "> 平台：抖音 · 作者：诡狡程序猫 · 时长：2:01",
        `> 简介：${"这段平台简介很长".repeat(30)}`,
        "> 原标题：一个很长的抖音文案标题",
        "",
        "## 视频总结",
        "",
        "本文介绍了个人知识库的搭建方法。",
      ].join("\n"),
    );

    expect(snippet).not.toContain("平台：");
    expect(snippet).not.toContain("作者：");
    expect(snippet).not.toContain("这段平台简介很长");
    expect(snippet).not.toContain("原标题");
    expect(snippet).toContain("本文介绍了个人知识库的搭建方法。");
  });

  it("论坛条目的元数据块同样剥掉（字段名不同也认）", () => {
    const snippet = snippetOf(
      [
        "> 平台：V2EX · 作者：0x114514 · 节点：程序员 · 20 条回复",
        "> 发布：2026-07-02",
        "",
        "## 讨论总结",
        "",
        "帖子围绕小公司前端使用 AI 的工作流展开。",
      ].join("\n"),
    );

    expect(snippet).not.toContain("0x114514");
    expect(snippet).toContain("帖子围绕小公司前端使用 AI 的工作流展开。");
  });

  it("只有元数据、正文还没生成时回退到原文，不给一片空白", () => {
    const snippet = snippetOf("> 平台：抖音 · 作者：某某 · 时长：1:00");

    expect(snippet).toContain("平台：抖音");
  });

  it("普通笔记不受影响", () => {
    expect(snippetOf("手冲建议 92 度。")).toBe("手冲建议 92 度。");
  });

  it("正文里普通的引用块不会被当成元数据剥掉", () => {
    // 只有首行是 `> 平台：…` 才算元数据块，否则原样保留
    const snippet = snippetOf("> 他说这话的时候我在场。\n\n后来证明是对的。");

    expect(snippet).toContain("他说这话的时候我在场。");
  });
});

describe("KnowledgeItemDB.bulkUpdate", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;
  let ids: string[];

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
    ids = [
      items.create({ title: "甲", content: "x", tagNames: ["读书"] }).id,
      items.create({ title: "乙", content: "y", tagNames: ["播客"] }).id,
    ];
  });

  it("追加标签时保留各自原有的标签", () => {
    // 整体替换的语义会把两条各自的原标签一起抹掉
    expect(items.bulkUpdate(ids, { addTagNames: ["待读"] })).toBe(2);

    const names = (id: string) =>
      new Set(items.get(id)!.tags.map((tag) => tag.name));
    expect(names(ids[0])).toEqual(new Set(["读书", "待读"]));
    expect(names(ids[1])).toEqual(new Set(["播客", "待读"]));
  });

  it("移除标签只动指定的那个", () => {
    items.bulkUpdate(ids, { addTagNames: ["待读"] });
    items.bulkUpdate(ids, { removeTagNames: ["待读"] });

    expect(items.get(ids[0])!.tags.map((tag) => tag.name)).toEqual(["读书"]);
    expect(items.get(ids[1])!.tags.map((tag) => tag.name)).toEqual(["播客"]);
  });

  it("一次改多个字段，未传的字段不动", () => {
    items.bulkUpdate(ids, { isFavorite: true, status: "archived" });

    for (const id of ids) {
      const item = items.get(id)!;
      expect(item.isFavorite).toBe(true);
      expect(item.status).toBe("archived");
      expect(item.isPinned).toBe(false);
    }
    // 标签没传就不该被清掉
    expect(items.get(ids[0])!.tags).toHaveLength(1);
  });

  it("标签变更同步进 FTS", () => {
    items.bulkUpdate(ids, { addTagNames: ["机器学习"] });
    const hit = items.list({ scope: "all", search: "机器学习" });
    expect(hit.total).toBe(2);
  });

  it("不存在的 id 跳过，不影响其余", () => {
    expect(items.bulkUpdate([...ids, "missing"], { isPinned: true })).toBe(2);
    expect(items.bulkUpdate([], { isPinned: true })).toBe(0);
  });
});

describe("KnowledgeItemDB.list 搜索模式", () => {
  let db: DatabaseAdapter.Database;
  let items: KnowledgeItemDB;

  beforeEach(() => {
    db = createTestDb();
    items = new KnowledgeItemDB(db);
    items.create({
      title: "语义检索的实现",
      content: "归知用 embedding 做语义检索，主进程算余弦相似度。",
    });
    items.create({ title: "无关条目", content: "今天天气不错" });
  });

  it("自然语言问句：phrase 模式零命中，recall 模式命中", () => {
    const question = "归知的语义检索是怎么实现的";
    expect(items.list({ scope: "all", search: question }).total).toBe(0);

    const recalled = items.list({
      scope: "all",
      search: question,
      searchMode: "recall",
    });
    expect(recalled.total).toBe(1);
    expect(recalled.entries[0].title).toBe("语义检索的实现");
  });

  it("搜索串编译不出内容时返回空，而不是列出全库", () => {
    // 旧行为把 null 当成「没有搜索」，界面显示「按相关度排序」却列出了所有条目
    expect(items.list({ scope: "all", search: "???" }).total).toBe(0);
    expect(items.list({ scope: "all", search: "——" }).entries).toEqual([]);
    // 空搜索仍然是「不过滤」
    expect(items.list({ scope: "all", search: "  " }).total).toBe(2);
  });
});

describe("列表查询的执行计划", () => {
  let db: DatabaseAdapter.Database;

  function planOf(sql: string): string {
    return (db.all(`EXPLAIN QUERY PLAN ${sql}`) as Array<{ detail: string }>)
      .map((row) => row.detail)
      .join(" | ");
  }

  beforeEach(() => {
    db = createTestDb();
    const items = new KnowledgeItemDB(db);
    // 少量数据也能定计划形态；ANALYZE 是 initDatabase 会做的事
    for (let index = 0; index < 200; index++) {
      items.create({
        title: `条目 ${index}`,
        content: "正文".repeat(50),
        status: index % 10 === 0 ? "archived" : "active",
      });
    }
    db.exec("ANALYZE");
  });

  it("总数走覆盖索引，不回表读 status", () => {
    // 条目表每行带正文，回表 = 按整页读；两万条时实测 180ms → 2.5ms
    const plan = planOf(
      `SELECT COUNT(*) AS c FROM knowledge_items i
       WHERE i.deleted_at IS NULL AND i.status != 'archived'`,
    );
    expect(plan).toContain("COVERING INDEX idx_items_deleted_status");
  });

  it("取页顺着排序索引走，不做临时排序", () => {
    // 缺统计信息时规划器会挑 idx_items_deleted（把 IS NULL 当等值、估成
    // 高选择度），然后对全部匹配行建临时 B 树——两万条时实测 128ms
    const plan = planOf(
      `SELECT i.id FROM knowledge_items i
       WHERE i.deleted_at IS NULL AND i.status != 'archived'
       ORDER BY i.is_pinned DESC, i.updated_at DESC LIMIT 50`,
    );
    expect(plan).toContain("idx_items_pinned_updated");
    expect(plan).not.toContain("TEMP B-TREE");
  });
});
