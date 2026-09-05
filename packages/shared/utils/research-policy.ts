import type { ResearchCandidate, ResearchRun, ResearchSource } from "../types/research";
import type { ResearchEligibility, ResearchPlan } from "../types/research-workflow";

export const RESEARCH_POLICY = Object.freeze({ version: "research-policy-v2", minRelevance: 30, maxAuthor: 3, maxSource: 10, maxEvidence: 30, maxUndated: 3, maxRead: 6 });

export function researchEligibility(candidate: ResearchCandidate, run?: Pick<ResearchRun, "rangeFrom" | "rangeTo">, plan?: ResearchPlan): ResearchEligibility {
  if (candidate.state === "dismissed") return "dismissed";
  if (candidate.relevanceScore < RESEARCH_POLICY.minRelevance) return "irrelevant";
  const text = `${candidate.title} ${candidate.snippet}`.normalize("NFKC").toLowerCase();
  if (plan?.entities.length && !plan.entities.some((entity) => text.includes(entity.normalize("NFKC").toLowerCase()))) return "entity_miss";
  if (candidate.publishedAt == null || candidate.dateConfidence === "low") return "undated";
  if (candidate.publishedAt != null && run && (candidate.publishedAt < run.rangeFrom || candidate.publishedAt > run.rangeTo)) return "out_of_window";
  return "recent";
}

export function selectResearchEvidence(candidates: readonly ResearchCandidate[], run?: Pick<ResearchRun, "rangeFrom" | "rangeTo">, plan?: ResearchPlan, hashes = new Map<string, string>(), max: number = RESEARCH_POLICY.maxEvidence): ResearchCandidate[] {
  const sorted = [...candidates].filter((c) => ["recent", "undated"].includes(researchEligibility(c, run, plan)))
    .sort((a, b) => Number(researchEligibility(a, run, plan) === "undated") - Number(researchEligibility(b, run, plan) === "undated") || b.overallScore - a.overallScore || a.id.localeCompare(b.id));
  const result: ResearchCandidate[] = [];
  const authors = new Map<string, number>();
  const sources = new Map<ResearchSource, number>();
  const groups = new Set<string>();
  let undated = 0;
  const add = (candidate: ResearchCandidate) => {
    const unknown = researchEligibility(candidate, run, plan) === "undated";
    const name = candidate.authorId || candidate.author.normalize("NFKC").trim().toLowerCase();
    const author = `${candidate.source}:${name && !["未知", "未知作者", "unknown"].includes(name) ? name : candidate.id}`;
    const group = hashes.get(candidate.id) || `${candidate.source}:${candidate.externalId || candidate.normalizedUrl}`;
    if (result.length >= max || groups.has(group) || (authors.get(author) ?? 0) >= RESEARCH_POLICY.maxAuthor || (sources.get(candidate.source) ?? 0) >= RESEARCH_POLICY.maxSource || (unknown && undated >= RESEARCH_POLICY.maxUndated)) return false;
    groups.add(group); authors.set(author, (authors.get(author) ?? 0) + 1); sources.set(candidate.source, (sources.get(candidate.source) ?? 0) + 1);
    if (unknown) undated += 1;
    result.push(candidate);
    return true;
  };
  const sourceOrder = ["xiaohongshu", "douyin", "bilibili"] as const;
  for (let round = 0; round < (max === RESEARCH_POLICY.maxRead ? 1 : 2); round += 1) {
    for (const source of sourceOrder) sorted.filter((c) => c.source === source && !result.includes(c)).some(add);
  }
  for (const candidate of sorted) add(candidate);
  return result;
}
