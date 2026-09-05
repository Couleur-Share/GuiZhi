import { afterEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB, CollectionDB, SemanticIndexDB, ResearchWorkflowDB } from "@guizhi/db";
import { compareResearch } from "@guizhi/shared/utils/research-comparison";
import { researchFixture } from "../../helpers/research";
import { createLocalResearchEvidence } from "../../../src/main/services/research/local-evidence";
import { ResearchService } from "../../../src/main/services/research/research-service";
import type { ResearchQueryAttempt } from "@guizhi/shared/types";

const databases: Database[] = [];
afterEach(() => databases.splice(0).forEach((db) => db.close()));
function database() {
  const db = new Database(":memory:"); databases.push(db); db.pragma("foreign_keys=ON"); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES); return db;
}
function completed() {
  const detail = researchFixture();
  detail.run.context = { seriesId: "series", phase: "idle", policyVersion: "v2", reportOutdated: false };
  detail.sources.forEach((s) => s.status = "succeeded");
  detail.attempts = detail.sources.map((s) => ({ source: s.source, capped: false, finished: true }) as ResearchQueryAttempt);
  return detail;
}
describe("研究序列与确定性比较", () => {
  it("区分新增、持续、未知互动与窗口移出", () => {
    const previous = completed(), current = structuredClone(previous);
    current.run.id = "next";
    previous.candidates[0].engagement = { likes: 0 };
    current.candidates[0].engagement = { likes: 5, views: 10 };
    previous.candidates[1].publishedAt = current.run.rangeFrom - 1;
    current.candidates[1].externalId = "new-id";
    const result = compareResearch(current, previous);
    expect(result.changes.map((c) => c.kind)).toEqual(["continued", "new", "outside_window"]);
    expect(result.changes[0].engagementChanges).toEqual({ likes: 5 });
  });
  it.each(["cap", "failure", "plan"])("%s 时不把缺失材料解释为消失", (reason) => {
    const previous = completed(), current = structuredClone(previous);
    current.candidates = [];
    if (reason === "cap") current.attempts!.forEach((a) => a.capped = true);
    if (reason === "failure") current.sources.forEach((s) => s.status = "failed");
    if (reason === "plan") current.run.context!.policyVersion = "v3";
    expect(compareResearch(current, previous).changes.every((c) => c.kind === "unknown")).toBe(true);
  });
  it("重新研究继承序列与知识范围，独立创建不合并，失败轮不成为默认基线", async () => {
    const db = database();
    const search = vi.fn(async () => ({ items: [{ ...researchFixture().candidates[0], title: "本地知识库" }], cursor: null, hasMore: false }));
    const service = new ResearchService(db, { enqueueImports: () => [], collectors: { bilibili: { source: "bilibili", search }, douyin: { source: "douyin", search }, xiaohongshu: { source: "xiaohongshu", search } } });
    const input = { topic: "本地知识库", depth: "quick" as const, dayRange: 7 as const, sources: ["bilibili" as const], knowledgeScope: { kind: "all" as const } };
    const first = service.createAndRun(input);
    await vi.waitFor(() => expect(service.store.get(first.id)?.status).toBe("ready"));
    const second = service.cloneAndRun(first.id);
    await vi.waitFor(() => expect(service.store.get(second.id)?.status).toBe("ready"));
    expect(second.context?.seriesId).toBe(first.id);
    expect(second.context?.knowledgeScope).toEqual(input.knowledgeScope);
    expect(service.compare(second.id).baselineRunId).toBe(first.id);
    const third = service.createAndRun(input);
    await vi.waitFor(() => expect(service.store.get(third.id)?.status).toBe("ready"));
    expect(service.baselines(third.id)).toEqual([]);
    service.store.finishRun(first.id, "failed");
    expect(service.compare(second.id).baselineRunId).toBeNull();
  });
});
describe("本地知识范围", () => {
  it("关闭不查询；FTS 与向量都不读取范围外、回收站及本序列报告", async () => {
    const db = database(), items = new KnowledgeItemDB(db), collections = new CollectionDB(db);
    const selected = collections.create({ name: "选定" }), other = collections.create({ name: "其他" });
    const a = items.create({ title: "本地知识库", content: "可使用的历史材料", itemType: "note", collectionId: selected.id });
    const b = items.create({ title: "本地知识库", content: "不得进入模型的秘密", itemType: "note", collectionId: other.id });
    const trash = items.create({ title: "本地知识库", content: "回收站秘密", itemType: "note", collectionId: selected.id });
    db.run("UPDATE knowledge_items SET deleted_at=1 WHERE id=?", trash.id);
    db.run("UPDATE knowledge_items SET status='archived' WHERE id=?", a.id);
    const index = new SemanticIndexDB(db);
    for (const item of [a, b, trash]) index.replaceItemChunks({ itemId: item.id, model: "test", dims: 2, contentHash: "hash", chunks: [{ text: item.content, vector: new Float32Array([1, 0]) }] });
    const embed = vi.fn(async () => ({ model: "test", vector: [1, 0] }));
    const retrieve = createLocalResearchEvidence(db, embed), run = researchFixture().run;
    expect(await retrieve(run, new AbortController().signal)).toEqual([]);
    expect(embed).not.toHaveBeenCalled();
    run.topic = "本地知识库"; run.context = { phase: "idle", seriesId: run.id, reportOutdated: false, policyVersion: "v2", knowledgeScope: { kind: "collection", collectionId: selected.id } };
    const report = items.create({ title: "本地知识库", content: "本序列报告不可自引用", itemType: "note", collectionId: selected.id });
    new ResearchWorkflowDB(db).linkSavedReport(run.id, report.id);
    const spy = vi.spyOn(SemanticIndexDB.prototype, "loadVectorsForSearch");
    const result = await retrieve(run, new AbortController().signal);
    expect(result.map((r) => r.itemId)).toEqual([a.id]);
    expect(spy.mock.results.flatMap((r) => r.value).every((r) => r.itemId === a.id)).toBe(true);
    spy.mockRestore();
    embed.mockRejectedValueOnce(new Error("semantic unavailable"));
    expect((await retrieve(run, new AbortController().signal)).map((r) => r.itemId)).toEqual([a.id]);
  });
  it("异步检索期间移出所选库会被二次校验排除", async () => {
    const db = database(), items = new KnowledgeItemDB(db), collection = new CollectionDB(db).create({ name: "选定" });
    const item = items.create({ title: "本地知识库", content: "历史材料", itemType: "note", collectionId: collection.id });
    const run = researchFixture().run; run.topic = "本地知识库";
    run.context = { phase: "idle", seriesId: run.id, reportOutdated: false, policyVersion: "v2", knowledgeScope: { kind: "collection", collectionId: collection.id } };
    const retrieve = createLocalResearchEvidence(db, async () => { db.run("UPDATE knowledge_items SET collection_id=NULL WHERE id=?", item.id); return null; });
    expect(await retrieve(run, new AbortController().signal)).toEqual([]);
  });
});
