import { afterEach, describe, expect, it } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { MIGRATIONS, runMigrations } from "@guizhi/db/migrations";
import {
  CrawlJobDB,
  KnowledgeItemDB,
  ResearchDB,
  WebSourceDB,
  webContentHash,
} from "@guizhi/db";
import type { WebCaptureResult } from "@guizhi/shared/types";
const databases: Database[] = [];
afterEach(() => databases.splice(0).forEach((db) => db.close()));
function fixture() {
  const db = new Database(":memory:");
  databases.push(db);
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}
function result(markdown = "原始正文", title = "原标题"): WebCaptureResult {
  return {
    taskId: "t",
    entryUrl: "https://example.com/docs/a",
    finalUrl: "https://example.com/docs/a",
    title,
    author: "",
    publishedAt: null,
    dateConfidence: "unknown",
    markdown,
    links: [],
    paragraphs: [{ id: "p1", text: markdown }],
    contentHash: webContentHash(markdown),
    capturedAt: Date.now(),
    engineVersion: "crawl4ai/0.9.3",
    complete: true,
    truncated: false,
    warnings: [],
  };
}
describe("网页版本与持久队列", () => {
  it("未编辑更新保存快照并同步全文检索；收藏和摘要保留", () => {
    const db = fixture(),
      items = new KnowledgeItemDB(db),
      source = new WebSourceDB(db);
    const item = items.create({
      title: "原标题",
      content: "原始正文",
      itemType: "webpage",
    });
    items.update(item.id, { summary: "旧摘要", isFavorite: true });
    source.initialize(item.id, result());
    expect(source.check(item.id, result("新的量子正文"))).toBe("updated");
    expect(items.get(item.id)).toMatchObject({
      content: "新的量子正文",
      isFavorite: true,
      summary: "旧摘要",
    });
    expect(
      source
        .versions(item.id)
        .some((v) => v.kind === "local" && v.markdown === "原始正文"),
    ).toBe(true);
    expect(source.baseline(item.id)?.summary_stale).toBe(1);
    items.update(item.id,{summary:"根据新原文重新整理的摘要"});
    expect(source.baseline(item.id)?.summary_stale).toBe(0);
    expect(
      db.get(
        "SELECT COUNT(*) AS n FROM knowledge_fts WHERE knowledge_fts MATCH '\"量 子\"'",
      ),
    ).toEqual({ n: 1 });
  });
  it("已编辑和历史无基线条目只增加待采用版本", () => {
    const db = fixture(),
      items = new KnowledgeItemDB(db),
      source = new WebSourceDB(db);
    const item = items.create({
      title: "旧条目",
      content: "用户正文",
      itemType: "webpage",
    });
    expect(source.check(item.id, result("网络正文"))).toBe("pending-version");
    expect(items.get(item.id)?.content).toBe("用户正文");
    expect(source.check(item.id, result("网络正文"))).toBe("pending-version");
    expect(source.versions(item.id)).toHaveLength(1);
    const version = source.versions(item.id)[0];
    expect(() =>
      source.adopt({
        itemId: item.id,
        versionId: version.id,
        expectedContentHash: webContentHash("过时正文"),
        expectedTitle: item.title,
      }),
    ).toThrow(/发生变化/);
    source.adopt({
      itemId: item.id,
      versionId: version.id,
      expectedContentHash: webContentHash(item.content),
      expectedTitle: item.title,
    });
    expect(items.get(item.id)?.content).toBe("网络正文");
    expect(items.get(item.id)?.title).toBe("旧条目");
  });
  it("失败或截断永不覆盖；原文无变化不会重复留版本", () => {
    const db = fixture(),
      items = new KnowledgeItemDB(db),
      source = new WebSourceDB(db),
      item = items.create({ title: "原标题", content: "原始正文" });
    source.initialize(item.id, result());
    expect(source.check(item.id, result())).toBe("unchanged");
    expect(() =>
      source.check(item.id, {
        ...result("残缺"),
        complete: false,
        truncated: true,
      }),
    ).toThrow(/不完整/);
    expect(source.versions(item.id)).toHaveLength(1);
    expect(items.get(item.id)?.content).toBe("原始正文");
  });
  it("连续自动更新不能把手动标题重新判为未编辑", () => {
    const db = fixture(),
      items = new KnowledgeItemDB(db),
      source = new WebSourceDB(db),
      item = items.create({ title: "原标题", content: "原始正文" });
    source.initialize(item.id, result());
    items.update(item.id, { title: "我的标题" });
    source.check(item.id, result("第二版", "网站第二标题"));
    source.check(item.id, result("第三版", "网站第三标题"));
    expect(items.get(item.id)).toMatchObject({
      title: "我的标题",
      content: "第三版",
    });
  });
  it("发现预算与 URL 唯一约束持久化；重启不会自行继续", () => {
    const db = fixture(),
      jobs = new CrawlJobDB(db),
      job = jobs.create({
        purpose: "documents",
        seeds: [{ url: "https://example.com/docs/a", mode: "directory" }],
        maxPages: 2,
        maxDepth: 2,
      });
    expect(jobs.discover(job.id, "https://example.com/docs/b", 1, 0)).toBe(
      true,
    );
    expect(jobs.discover(job.id, "https://example.com/docs/c", 1, 0)).toBe(
      false,
    );
    jobs.setStatus(job.id, "running");
    jobs.save({ ...jobs.pages(job.id)[0], status: "running" });
    jobs.recover();
    expect(jobs.get(job.id)?.status).toBe("interrupted");
    expect(jobs.pages(job.id).every((p) => p.status === "pending")).toBe(true);
  });
  it("旧来源 CHECK 事务迁移保留研究行与索引并允许网页", () => {
    const db = new Database(":memory:");
    databases.push(db);
    db.pragma("foreign_keys=ON");
    db.exec(
      SCHEMA_TABLES.replaceAll(",'web'", "").replace(
        /\s*time_scope TEXT[^\n]+\n/,
        "\n",
      ),
    );
    db.exec(SCHEMA_INDEXES);
    // 使用迁移前列直接建立一条历史记录，防止测试误用新 DAO。
    db.run(
      "INSERT INTO research_runs (id,topic,day_range,range_from,range_to,depth,sources_json,status,report_status,created_at,updated_at) VALUES ('old','旧研究',30,1,2,'quick','[\"bilibili\"]','ready','none',1,2)",
    );
    db.run("INSERT INTO research_source_runs(run_id,source,status,method,collected_count) VALUES ('old','bilibili','succeeded','public-api',1)");
    db.run("INSERT INTO research_candidates(id,run_id,source,external_id,url,normalized_url,title,snippet,media_type,discovery_method,created_at,updated_at) VALUES ('old-candidate','old','bilibili','BV-old','https://example.com/old','https://example.com/old','历史候选','历史正文','video','public-api',1,2)");
    const beforeCandidate=db.get("SELECT * FROM research_candidates WHERE id='old-candidate'");
    const beforeIndexes=db.all("SELECT name,sql FROM sqlite_master WHERE type='index' AND tbl_name IN ('research_candidates','research_source_runs') ORDER BY name");
    db.exec(
      "CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)",
    );
    for (const migration of MIGRATIONS.slice(0, -1))
      db.run("INSERT INTO schema_migrations VALUES (?,?)", migration.name, 1);
    expect(runMigrations(db)).toEqual(["0030-web-capture"]);
    expect(db.get("SELECT * FROM research_candidates WHERE id='old-candidate'")).toEqual(beforeCandidate);
    expect(db.get("SELECT collected_count FROM research_source_runs WHERE run_id='old'")).toEqual({collected_count:1});
    expect(db.all("SELECT name,sql FROM sqlite_master WHERE type='index' AND tbl_name IN ('research_candidates','research_source_runs') ORDER BY name")).toEqual(beforeIndexes);
    expect(new ResearchDB(db).get("old")?.timeScope).toBe("recent");
    const run = new ResearchDB(db).create(
      { topic: "网页研究", dayRange: 30, depth: "quick", sources: ["web"] },
      1,
      2,
    );
    expect(run.timeScope).toBe("all");
    expect(db.pragma("foreign_key_check")).toEqual([]);
  });
});
