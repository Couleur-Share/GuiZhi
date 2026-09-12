// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import Database from "@guizhi/db/adapter";
import { KnowledgeItemDB, AskSessionDB, ThemedReadingDB } from "@guizhi/db";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { MIGRATIONS } from "@guizhi/db/migrations";
import { articleContext } from "../../src/main/services/article-ask-context";
import { selectArticleContext } from "@guizhi/shared/utils/article-context";
import { themedVersion } from "./db/themed-reading-fixture";

let db: Database.Database, items: KnowledgeItemDB;
beforeEach(() => { db = new Database(":memory:"); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES); items = new KnowledgeItemDB(db); });
afterEach(() => db.close());

describe("本文来源与会话迁移", () => {
  it("旧会话迁移幂等且不改写消息，新会话按文章过滤", () => {
    db.exec("DROP TABLE ask_sessions; CREATE TABLE ask_sessions(id TEXT PRIMARY KEY,title TEXT,messages_json TEXT,created_at INTEGER,updated_at INTEGER); INSERT INTO ask_sessions VALUES('old','历史','[]',1,1)");
    const migration = MIGRATIONS.find(m => m.name === "article-ask-sessions")!;
    migration.up(db); migration.up(db);
    const sessions = new AskSessionDB(db);
    expect(sessions.get("old")?.messagesJson).toBe("[]");
    sessions.save({ id: "article", title: "问题", messagesJson: "[]", scope: "article", itemId: "a", articleTitle: "文章", webEnabled: false, target: { itemId: "a", view: "body" } });
    expect(sessions.list(100, { itemId: "a", scope: "article" }).map(s => s.id)).toEqual(["article"]);
    expect(sessions.list(100, { scope: "knowledge" }).map(s => s.id)).toEqual(["old"]);
    expect(sessions.get("article")?.webEnabled).toBe(false);
  });
  it("同名文章始终按 ID 读取且拒绝不存在或删除的正文", () => {
    const a = items.create({ title: "同名", content: "正确的正文" });
    items.create({ title: "同名", content: "错误的正文" });
    const result = articleContext(db, { target: { itemId: a.id, view: "body" }, question: "正文是什么" });
    expect(result.sources[0].text).toBe("正确的正文");
    db.run("UPDATE knowledge_items SET deleted_at=1 WHERE id=?", a.id);
    expect(() => articleContext(db, { target: result.target, question: "" })).toThrow("已删除");
  });
  it("长文末尾选段及邻段不会被头部截断", () => {
    const text = ("无关背景内容。".repeat(200) + "\n\n").repeat(40) + "最后的前提条件。\n\n反向代理并不自动消除流量。\n\n只有直连播放地址才可能绕过中转。";
    const selected = selectArticleContext(text, "为什么", "反向代理并不自动消除流量。", 3000);
    expect(selected.clipped).toBe(true);
    expect(selected.text).toContain("最后的前提条件");
    expect(selected.text).toContain("只有直连播放地址");
    expect(selected.text.length).toBeLessThanOrEqual(3100);
  });
  it("文字稿和 OCR 读取相应文本，拒绝过期选段", () => {
    const item = items.create({ title: "视频", content: "文章原文", transcript: "这里是实际文字稿" });
    const result = articleContext(db, { target: { itemId: item.id, view: "transcript", selection: "实际文字稿" }, question: "解释" });
    expect(result.sources[0].text).toBe("这里是实际文字稿");
    expect(result.sources[1].text).toBe("文章原文");
    expect(() => articleContext(db, { target: { ...result.target, selection: "不存在的选段" }, question: "" })).toThrow("不一致");
  });
  it("快照绑定具体版本，不因正文更新而替换", () => {
    const item = items.create({ title: "新版", content: "新正文" });
    const version = { id: "v-old", itemId: item.id, title: "旧版", markdown: "旧版采集的正文", capturedAt: 1 };
    db.run("INSERT INTO web_source_versions VALUES(?,?,?,?)", version.id, item.id, JSON.stringify(version), 1);
    expect(articleContext(db, { target: { itemId: item.id, view: "snapshot", versionId: version.id }, question: "" }).sources[0].text).toBe(version.markdown);
    expect(() => articleContext(db, { target: { itemId: item.id, view: "snapshot", versionId: "missing" }, question: "" })).toThrow("不存在");
  });
  it("旧 AI 阅读格式使用实际渲染文字，来源块不会丢失", () => {
    const item = items.create({ title: "原文", content: "保存的原文" });
    const page = themedVersion(item.id, { assets: [] });
    new ThemedReadingDB(db).saveVersion(page);
    const result = articleContext(db, { target: { itemId: item.id, view: "themed", versionId: page.id }, question: "" });
    expect(result.sources[0].text).toContain("保持原文全部内容");
    expect(result.sources[0].title).toContain("AI 阅读页");
    const other = items.create({ title: "另一篇", content: "其他" });
    expect(() => articleContext(db, { target: { itemId: other.id, view: "themed", versionId: page.id }, question: "" })).toThrow("不属于");
  });
});
