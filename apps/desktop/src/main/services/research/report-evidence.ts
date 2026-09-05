import { randomUUID } from "node:crypto";
import type { ResearchEvidencePacket, ResearchRunDetail, ResearchSnapshot, ResearchLocalEvidence, ResearchPassage } from "@guizhi/shared/types";
import { RESEARCH_POLICY, researchEligibility, selectResearchEvidence } from "@guizhi/shared/utils/research-policy";
import { researchTokens } from "@guizhi/shared/utils/research-analysis";

export function selectPassages(passages: ResearchPassage[], topic: string, budget: number): ResearchPassage[] {
  const tokens = researchTokens(topic);
  const ranked = passages.map((p) => ({ p, score: [...tokens].filter((t) => researchTokens(p.text).has(t)).length }))
    .sort((a, b) => b.score - a.score || a.p.position - b.p.position);
  const result: ResearchPassage[] = [];
  let left = budget;
  let comments = 0;
  for (const { p } of ranked) {
    if (left <= 0) break;
    if (p.kind === "comment" && comments++ >= 3) continue;
    const text = p.text.slice(0, Math.min(left, 2000));
    if (text) result.push({ ...p, text });
    left -= text.length;
  }
  return result.sort((a, b) => a.position - b.position);
}

export function createEvidenceSnapshot(detail: ResearchRunDetail, localItems: ResearchLocalEvidence[] = []): ResearchSnapshot {
  const hashes = new Map((detail.documents ?? []).filter((d) => d.contentHash).map((d) => [d.candidateId, d.contentHash!]));
  const candidates = selectResearchEvidence(detail.candidates, detail.run, detail.run.context?.plan, hashes);
  if (!candidates.some((c) => researchEligibility(c, detail.run, detail.run.context?.plan) === "recent")) throw new Error("本次未找到足够的近期有效证据");
  const snapshotId = randomUUID();
  const operationId = randomUUID();
  const totalBudget = detail.run.depth === "quick" ? 12_000 : 32_000;
  const local = localItems.slice(0, 6).map((item, index) => ({ ...item, ref: `L${index + 1}`, excerpt: item.excerpt.slice(0, 1000) }));
  const perItem = Math.floor((totalBudget - local.reduce((n, item) => n + item.excerpt.length, 0)) / candidates.length);
  const packet: ResearchEvidencePacket = {
    runId: detail.run.id, topic: detail.run.topic, rangeFrom: detail.run.rangeFrom, rangeTo: detail.run.rangeTo,
    snapshotId, operationId, policyVersion: RESEARCH_POLICY.version, intent: detail.run.context?.plan?.intent ?? "overview",
    sourceRuns: detail.sources, attempts: detail.attempts ?? [], localItems: local,
    items: candidates.map((candidate, index) => {
      const doc = detail.documents?.find((d) => d.candidateId === candidate.id);
      const original = doc?.passages.length ? doc.passages : [{ text: candidate.snippet || candidate.title, kind: "metadata" as const, position: 0 }];
      const passages = selectPassages(original, detail.run.topic, perItem);
      const hash = hashes.get(candidate.id);
      const relatedIds = hash ? new Set([...hashes].filter(([, value]) => value === hash).map(([id]) => id)) : new Set([candidate.id]);
      return {
        capturedAt: doc?.capturedAt ?? candidate.createdAt, completeness: doc?.warning || doc?.error,
        ref: `R${index + 1}`, candidateId: candidate.id, source: candidate.source, title: candidate.title, author: candidate.author,
        snippet: passages.map((p) => p.text).join("\n"), passages, publishedAt: candidate.publishedAt, dateConfidence: candidate.dateConfidence,
        engagement: candidate.engagement, overallScore: candidate.overallScore, url: candidate.url,
        eligibility: researchEligibility(candidate, detail.run, detail.run.context?.plan),
        urls: detail.candidates.filter((c) => relatedIds.has(c.id)).map((c) => c.url),
        excerptTruncated: Boolean(doc?.truncated) || passages.reduce((n, p) => n + p.text.length, 0) < original.reduce((n, p) => n + p.text.length, 0),
      };
    }),
  };
  return { id: snapshotId, operationId, runId: detail.run.id, createdAt: Date.now(), status: "generating", packet };
}

export function validateReport(markdown: string, packet: ResearchEvidencePacket): void {
  const valid = new Set([...packet.items.map((i) => i.ref), ...(packet.localItems ?? []).map((i) => i.ref)]);
  const refs = [...markdown.matchAll(/\[([RL]\d+)]/g)].map((m) => m[1]);
  if (refs.some((ref) => !valid.has(ref))) throw new Error("报告包含不属于本次证据包的引用编号");
  if (!refs.length) throw new Error("报告没有引用任何有效候选");
  if (/(?:https?|ftp|file|mailto|data|javascript):|www\.|\]\(|<\/?(?:a|img)\b|^\s*\[[^\]]+]:/im.test(markdown)) throw new Error("报告包含模型自行输出的链接；原始链接必须由证据编号生成");
  let limitation = false;
  const weak = new Set(packet.items.filter((i) => i.eligibility === "undated").map((i) => i.ref));
  for (const block of markdown.trim().split(/\n\s*\n/)) {
    const blockLines = block.split("\n");
    const lines = blockLines.filter((line, index) => {
      if (/^\s*\|/.test(line) && /^\s*\|?[\s:|-]+\|\s*$/.test(blockLines[index + 1] ?? "")) return false;
      if (/^\s*#{1,6}\s/.test(line)) { limitation = /限制|待核实|limitation|unverified/i.test(line); return false; }
      return line.trim() && !/^\s*[-|:\s]+$/.test(line);
    });
    if (!lines.length) continue;
    for (const line of lines) if (/^\s*(?:[-*+] |\d+[.)] |\|)/.test(line) && !/\[([RL]\d+)]/.test(line)) throw new Error("报告存在未附引用的列表结论");
    const text = lines.join("\n");
    const blockRefs = [...text.matchAll(/\[([RL]\d+)]/g)].map((m) => m[1]);
    if (!blockRefs.length) throw new Error("报告存在未附引用的结论段，请重新生成");
    if (!limitation && blockRefs.some((ref) => weak.has(ref))) throw new Error("日期未确认线索只能用于限制说明与待核实部分");
  }
}

function literal(value: string): string { return value.replace(/[\\`*_[\]{}()#+.!<>|~-]/g, (character) => "\\" + character); }
function date(value: number): string { return new Date(value).toISOString(); }
function safeUrl(value: string): string { return value.replace(/[\s<>"\\]/g, (c) => encodeURIComponent(c)); }

export function renderCompleteReport(markdown: string, packet: ResearchEvidencePacket): string {
  const used = new Set([...markdown.matchAll(/\[([RL]\d+)]/g)].map((m) => m[1]));
  const sources = packet.sourceRuns.map((s) => `- ${s.source}：${s.status}；候选 ${s.collectedCount} 条`).join("\n");
  const attempts = (packet.attempts ?? []).map((a) => `- ${a.source} · ${literal(a.query)} · ${literal(a.method)}：返回 ${a.returnedCount}，窗口内 ${a.inWindowCount}，日期未确认 ${a.unknownDateCount}${a.errorCode ? `；${a.failureStage ?? "query"}：${a.errorCode}` : ""}`).join("\n");
  const caps = [...new Set((packet.attempts ?? []).filter((a) => a.capped).map((a) => a.source))];
  const references = packet.items.filter((i) => used.has(i.ref)).map((i) => {
    const passages = (i.passages ?? []).map((p) => `> ${p.kind}${p.startMs != null ? ` @ ${Math.floor(p.startMs / 1000)}s` : ` #${p.position + 1}`}${p.author ? ` · ${literal(p.author)}` : ""}：${literal(p.text).replace(/\n/g, "\n> ")}`).join("\n\n");
    return `[${i.ref}]: <${safeUrl(i.url)}>\n\n**${i.ref} · ${literal(i.title)}** · ${literal(i.author)} · 采集 ${i.capturedAt ? date(i.capturedAt) : "未知"} · 发布 ${i.publishedAt != null ? date(i.publishedAt) : "未知"}（${i.dateConfidence}）\n\n${literal(i.completeness ?? "")}\n\n${passages}\n\n${(i.urls ?? []).map((url) => `[来源](<${safeUrl(url)}>)`).join(" · ")}`;
  });
  for (const item of packet.localItems ?? []) if (used.has(item.ref)) references.push(`[${item.ref}]: #research-evidence-${item.ref}\n\n**[${item.ref}] 本地知识 · ${literal(item.title)}**（版本 ${date(item.updatedAt)}）\n\n> ${literal(item.excerpt).replace(/\n/g, "\n> ")}`);
  return `## 研究范围与覆盖\n\n${date(packet.rangeFrom)} 至 ${date(packet.rangeTo)}\n\n${sources}\n\n${attempts ? `查询尝试（跨查询可能重复）：\n\n${attempts}\n\n` : ""}${markdown.trim()}\n\n## 材料限制\n\n候选搜索与有限精读不代表平台完整覆盖。互动量不代表可信度；评论只代表作者观点。${caps.length ? `已达到采样上限：${caps.join("、")}。` : ""}${packet.items.some((i) => i.excerptTruncated) ? "部分材料仅选取摘录，未完整进入模型上下文。" : ""}\n\n## 引用来源\n\n${references.join("\n\n")}`;
}
