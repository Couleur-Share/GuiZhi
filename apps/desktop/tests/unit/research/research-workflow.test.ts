import { afterEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { ResearchService } from "../../../src/main/services/research/research-service";
import { planResearch } from "../../../src/main/services/research/research-ai";
import { parseCaptionCues } from "../../../src/main/services/import/video-captions";
import { researchFixture } from "../../helpers/research";
import type { ResearchCollector } from "../../../src/main/services/research/collectors";
import type { ResearchDocument, ResearchPlan } from "@guizhi/shared/types";

const databases: Database[] = [];
afterEach(() => databases.splice(0).forEach((db) => db.close()));
function fixture(options: Partial<ConstructorParameters<typeof ResearchService>[1]> = {}) {
  const db = new Database(":memory:"); databases.push(db); db.pragma("foreign_keys = ON"); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  const search = vi.fn<ResearchCollector["search"]>(async () => ({ items: [{ ...researchFixture().candidates[0], title: "本地知识库", author: "甲", externalId: "BV1" }], hasMore: false, cursor: null }));
  const service = new ResearchService(db, { enqueueImports: () => [], collectors: { bilibili: { source: "bilibili", search }, xiaohongshu: { source: "xiaohongshu", search }, douyin: { source: "douyin", search } }, ...options });
  return { db, service, search };
}
const start = (service: ResearchService, depth: "quick" | "deep" = "deep") => service.createAndRun({ topic: "本地知识库", depth, sources: ["bilibili"], dayRange: 7 });
const settled = (service: ResearchService, id: string) => vi.waitFor(() => expect(service.getDetail(id)!.run.status).not.toBe("collecting"));

describe("研究执行与快照", () => {
  it("三条查询共用三页预算并跨查询去重；快速模式不规划", async () => {
    const plan = vi.fn(async (): Promise<ResearchPlan> => ({ version: "v1", intent: "overview", entities: [], queries: ["本地知识库", "离线", "知识管理"] }));
    const { service, search } = fixture({ plan });
    const run = start(service);
    await settled(service, run.id);
    expect(search).toHaveBeenCalledTimes(3);
    expect(service.getDetail(run.id)!.candidates).toHaveLength(1);
    expect(service.getDetail(run.id)!.attempts).toHaveLength(3);
    const quick = start(service, "quick"); await settled(service, quick.id);
    expect(plan).toHaveBeenCalledTimes(1);
  });
  it("规划失败回退且不重试模型，拒绝超过三条的计划", async () => {
    const chat = vi.fn().mockRejectedValue(new Error("timeout"));
    expect((await planResearch("本地知识库", new AbortController().signal, chat)).queries).toEqual(["本地知识库"]);
    expect(chat).toHaveBeenCalledTimes(1);
    chat.mockResolvedValue(JSON.stringify({ queries: ["1", "2", "3", "4"] }));
    expect((await planResearch("本地知识库", new AbortController().signal, chat)).fallback).toBeTruthy();
  });
  it("取消报告后，迟到结果不得覆盖旧报告或新操作", async () => {
    let resolve!: (text: string) => void;
    const { service } = fixture({ report: () => new Promise((done) => { resolve = done; }) });
    const run = start(service, "quick"); await settled(service, run.id);
    const packet = service.beginReport(run.id);
    service.saveReport(run.id, "旧结论 [R1]", "test", packet.snapshotId);
    service.generateReport(run.id);
    service.cancelReport(run.id); resolve("迟到结论 [R1]");
    await new Promise((done) => setTimeout(done, 10));
    expect(service.getDetail(run.id)!.run.reportMarkdown).toContain("旧结论");
    expect(service.getDetail(run.id)!.run.reportMarkdown).not.toContain("迟到结论");
    expect(() => service.saveReport(run.id, "迟到 [R1]", "test", packet.snapshotId)).toThrow(/取消|替代/);
  });
  it("精读保存在研究记录，重启不重发；保存摘录后删除研究不删除知识", async () => {
    const read = vi.fn(async (c): Promise<ResearchDocument> => ({ id: "doc", candidateId: c.id, runId: c.runId, source: c.source, url: c.url, title: c.title, author: c.author, publishedAt: c.publishedAt, capturedAt: Date.now(), status: "ready", passages: [{ kind: "body", position: 0, text: "这是有出处的原始文字" }], contentHash: "body-hash", truncated: false }));
    const { service, db } = fixture({ read });
    const run = start(service); await settled(service, run.id);
    expect(db.get("SELECT COUNT(*) AS n FROM knowledge_items")).toEqual({ n: 0 });
    const detail = service.getDetail(run.id)!;
    const saved = service.saveExcerpt(run.id, detail.candidates[0].id);
    service.workflow.recover(); expect(read).toHaveBeenCalledTimes(1);
    service.delete(run.id);
    expect(db.get("SELECT id FROM knowledge_items WHERE id=?", saved.itemId)).toBeTruthy();
    expect(db.get("SELECT COUNT(*) AS n FROM research_documents")).toEqual({ n: 0 });
  });
  it("字幕保留 SRT/VTT 时间点，纯文本结果不改变", () => {
    expect(parseCaptionCues("1\n00:01:02,500 --> 00:01:04,000\n第一句\n\n2\n00:01:04.000 --> 00:01:05.000\n第二句")).toEqual([
      { startMs: 62500, endMs: 64000, text: "第一句" }, { startMs: 64000, endMs: 65000, text: "第二句" },
    ]);
  });
});
