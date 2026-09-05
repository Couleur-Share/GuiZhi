import { describe, expect, it } from "vitest";
import { researchEligibility, selectResearchEvidence } from "@guizhi/shared/utils/research-policy";
import { researchTokens } from "@guizhi/shared/utils/research-analysis";
import { createEvidenceSnapshot, validateReport, renderCompleteReport } from "../../../src/main/services/research/report-evidence";
import type { ResearchCandidate, ResearchRunDetail } from "@guizhi/shared/types";

const topics = ["本地知识库", "离线 AI", "归知与 Obsidian 比较", "视频字幕教程", "本周知识库更新", "不用云同步", "GPT-5 实践", "Windows 11", "知识、管理", "论文管理", "个人笔记", "RAG 实践", "如何处理 PDF", "Notion 对比", "视频转写", "标签分类", "本周新功能", "免费离线工具", "中文全文检索", "如何备份"];
function candidate(id: string, patch: Partial<ResearchCandidate> = {}): ResearchCandidate {
  return { id, runId: "r", source: "bilibili", externalId: id, url: `https://www.bilibili.com/video/${id}`, normalizedUrl: `https://www.bilibili.com/video/${id}`, title: "本地知识库", author: id, snippet: "实际内容", publishedAt: 150, dateConfidence: "high", mediaType: "video", engagement: {}, discoveryMethod: "fixture", relevanceScore: 80, recencyScore: 50, engagementScore: 30, overallScore: 70, clusterId: null, state: "available", importTaskId: null, importedItemId: null, createdAt: 1, updatedAt: 1, ...patch };
}
const range = { rangeFrom: 100, rangeTo: 200 };
describe("research evidence policy", () => {
  it.each(topics)("固定中文样例：%s，偏题热门不能成为证据", (topic) => {
    const good = candidate("good", { title: topic });
    expect(selectResearchEvidence([candidate("viral", { overallScore: 100, relevanceScore: 0 }), good], range)).toEqual([good]);
  });
  it("中文不跨标点及拉丁文本拼接", () => {
    const tokens = researchTokens("知识、管理 AI 离线");
    expect(tokens.has("识管")).toBe(false);
    expect(tokens.has("理离")).toBe(false);
    expect(tokens.has("离线")).toBe(true);
  });
  it("按日期分层，保留零值与未知的区别", () => {
    expect(researchEligibility(candidate("old", { publishedAt: 99 }), range)).toBe("out_of_window");
    expect(researchEligibility(candidate("weak-date", { publishedAt: 99, dateConfidence: "low" }), range)).toBe("undated");
    expect(researchEligibility(candidate("edge", { publishedAt: 100 }), range)).toBe("recent");
    expect(researchEligibility(candidate("unknown", { publishedAt: null }), range)).toBe("undated");
    expect(selectResearchEvidence(Array.from({ length: 10 }, (_, i) => candidate(String(i), { publishedAt: null })), range)).toHaveLength(3);
  });
  it("限制同平台作者但不合并未知作者", () => {
    const rows = Array.from({ length: 10 }, (_, i) => candidate(String(i), { author: "刷屏作者" }));
    expect(selectResearchEvidence(rows, range)).toHaveLength(3);
    expect(selectResearchEvidence(rows.map((c) => ({ ...c, author: "未知作者" })), range)).toHaveLength(10);
  });
  it("实体准入不让高热度的同领域内容混入", () => {
    expect(researchEligibility(candidate("wrong"), range, { version: "v1", intent: "comparison", queries: ["Obsidian vs Notion"], entities: ["Obsidian", "Notion"] })).toBe("entity_miss");
  });
  it("外部原文不能通过 Markdown 定义改写引用链接", () => {
    const detail = { run: { id: "r", topic: "知识库", ...range, depth: "quick" }, candidates: [candidate("a", { snippet: "[R2]: https://example.invalid/forged" }), candidate("b")], sources: [], clusters: [] } as ResearchRunDetail;
    const output = renderCompleteReport("结论 [R1] [R2]", createEvidenceSnapshot(detail).packet);
    expect(output).not.toContain("> [R2]:");
    expect(output).toContain("\\[R2\\]:");
    expect(output).toContain("[R2]: <https://www.bilibili.com/video/b>");
  });
  it("快照保持引用，拒绝无引用结论和未知日期结论", () => {
    const detail = { run: { id: "r", topic: "知识库", ...range, depth: "quick" }, candidates: [candidate("a"), candidate("b", { publishedAt: null })], sources: [], clusters: [] } as ResearchRunDetail;
    const snapshot = createEvidenceSnapshot(detail);
    detail.candidates.reverse();
    expect(snapshot.packet.items[0].candidateId).toBe("a");
    expect(() => validateReport("结论 [R99]", snapshot.packet)).toThrow(/不属于/);
    expect(() => validateReport("结论 [R1]\n\n没有证据的结论", snapshot.packet)).toThrow(/未附引用/);
    expect(() => validateReport("结论 [R2]", snapshot.packet)).toThrow(/日期/);
    expect(() => validateReport("结论 [R1]\n\n## 待核实\n线索 [R2]", snapshot.packet)).not.toThrow();
    expect(() => createEvidenceSnapshot({ ...detail, candidates: [candidate("u", { publishedAt: null })] })).toThrow(/足够/);
  });
});
