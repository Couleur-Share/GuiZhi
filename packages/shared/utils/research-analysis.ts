import type {
  PlatformDiscoveryEngagement,
  ResearchCandidate,
  ResearchCluster,
  ResearchSource,
} from "../types";

const CJK = /[\u3400-\u9fff]/;
const LATIN_TOKEN = /[a-z0-9]+/g;

export function researchTokens(value: string): Set<string> {
  const normalized = value.normalize("NFKC").toLowerCase();
  const result = new Set(normalized.match(LATIN_TOKEN) ?? []);
  const cjk = [...normalized].filter((char) => CJK.test(char));
  for (let index = 0; index < cjk.length; index += 1) {
    result.add(cjk[index]);
    if (index + 1 < cjk.length) result.add(`${cjk[index]}${cjk[index + 1]}`);
  }
  return result;
}

function coverage(query: Set<string>, content: Set<string>): number {
  if (query.size === 0) return 0;
  let matched = 0;
  for (const token of query) if (content.has(token)) matched += 1;
  return (matched / query.size) * 100;
}

function jaccard(left: Set<string>, right: Set<string>): number {
  if (left.size === 0 || right.size === 0) return 0;
  let intersection = 0;
  for (const value of left) if (right.has(value)) intersection += 1;
  return intersection / (left.size + right.size - intersection);
}

function titleTrigrams(value: string): Set<string> {
  const compact = value.normalize("NFKC").toLowerCase().replace(/[\s\p{P}\p{S}]+/gu, "");
  const chars = [...compact];
  const output = new Set<string>();
  if (chars.length < 3) {
    if (compact) output.add(compact);
    return output;
  }
  for (let index = 0; index <= chars.length - 3; index += 1) {
    output.add(chars.slice(index, index + 3).join(""));
  }
  return output;
}

function stableClusterId(candidateIds: readonly string[]): string {
  const value = [...candidateIds].sort().join("|");
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `research-cluster-${(hash >>> 0).toString(16).padStart(8, "0")}`;
}

function numeric(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : 0;
}

const ENGAGEMENT_WEIGHTS: Record<ResearchSource, Partial<Record<keyof PlatformDiscoveryEngagement, number>>> = {
  xiaohongshu: { likes: 0.4, favorites: 0.4, comments: 0.2 },
  douyin: { likes: 0.4, comments: 0.2, shares: 0.2, views: 0.2 },
  bilibili: { views: 0.35, danmaku: 0.15, comments: 0.15, likes: 0.2, favorites: 0.15 },
};

function engagementValue(candidate: ResearchCandidate): number | null {
  const weights = ENGAGEMENT_WEIGHTS[candidate.source];
  let found = false;
  let total = 0;
  for (const [key, weight] of Object.entries(weights) as [keyof PlatformDiscoveryEngagement, number][]) {
    const raw = candidate.engagement[key];
    if (typeof raw === "number" && Number.isFinite(raw)) found = true;
    total += weight * Math.log1p(numeric(raw));
  }
  return found ? total : null;
}

function percentileScores(values: readonly (number | null)[]): number[] {
  const present = values
    .map((value, index) => ({ value, index }))
    .filter((entry): entry is { value: number; index: number } => entry.value !== null)
    .sort((a, b) => a.value - b.value || a.index - b.index);
  if (present.length === 0) return values.map(() => 35);
  if (present.length === 1) return values.map((value) => (value === null ? 35 : 100));
  const output = values.map(() => 35);
  let cursor = 0;
  while (cursor < present.length) {
    let end = cursor;
    while (end + 1 < present.length && present[end + 1].value === present[cursor].value) end += 1;
    const percentile = (((cursor + end) / 2) / (present.length - 1)) * 100;
    for (let index = cursor; index <= end; index += 1) output[present[index].index] = percentile;
    cursor = end + 1;
  }
  return output;
}

export interface ResearchAnalysisResult {
  candidates: ResearchCandidate[];
  clusters: Array<Omit<ResearchCluster, "runId" | "candidates">>;
}

export function analyzeResearchCandidates(
  topic: string,
  rangeFrom: number,
  rangeTo: number,
  input: readonly ResearchCandidate[],
): ResearchAnalysisResult {
  const queryTokens = researchTokens(topic);
  const candidates = input.map((candidate) => ({ ...candidate, engagement: { ...candidate.engagement } }));
  const engagementBySource = new Map<string, number>();
  for (const source of ["xiaohongshu", "douyin", "bilibili"] as const) {
    const positions = candidates.map((candidate, index) => ({ candidate, index })).filter(({ candidate }) => candidate.source === source);
    const percentiles = percentileScores(positions.map(({ candidate }) => engagementValue(candidate)));
    positions.forEach(({ index }, position) => engagementBySource.set(String(index), percentiles[position]));
  }
  const duration = Math.max(rangeTo - rangeFrom, 1);
  candidates.forEach((candidate, index) => {
    const titleTokens = researchTokens(candidate.title);
    const combinedTokens = researchTokens(`${candidate.title} ${candidate.snippet}`);
    candidate.relevanceScore = Math.round(coverage(queryTokens, titleTokens) * 0.7 + coverage(queryTokens, combinedTokens) * 0.3);
    candidate.recencyScore = candidate.publishedAt == null
      ? 35
      : Math.round(Math.max(0, Math.min(100, ((candidate.publishedAt - rangeFrom) / duration) * 100)));
    candidate.engagementScore = Math.round(engagementBySource.get(String(index)) ?? 35);
    const missingEngagement = engagementValue(candidate) === null;
    const penalty = (candidate.dateConfidence === "low" ? 5 : 0) + (missingEngagement ? 3 : 0);
    candidate.overallScore = Math.max(0, Math.min(100, Math.round(
      candidate.relevanceScore * 0.45 + candidate.recencyScore * 0.25 + candidate.engagementScore * 0.3 - penalty,
    )));
  });

  const parents = candidates.map((_, index) => index);
  const find = (index: number): number => {
    while (parents[index] !== index) {
      parents[index] = parents[parents[index]];
      index = parents[index];
    }
    return index;
  };
  const union = (left: number, right: number): void => {
    const a = find(left);
    const b = find(right);
    if (a !== b) parents[b] = a;
  };
  const tokens = candidates.map((candidate) => researchTokens(candidate.title));
  const trigrams = candidates.map((candidate) => titleTrigrams(candidate.title));
  for (let left = 0; left < candidates.length; left += 1) {
    for (let right = left + 1; right < candidates.length; right += 1) {
      if (candidates[left].source === candidates[right].source) continue;
      if (jaccard(tokens[left], tokens[right]) >= 0.4 || jaccard(trigrams[left], trigrams[right]) >= 0.55) {
        union(left, right);
      }
    }
  }
  const groups = new Map<number, number[]>();
  candidates.forEach((_, index) => {
    const root = find(index);
    groups.set(root, [...(groups.get(root) ?? []), index]);
  });
  const clusters: ResearchAnalysisResult["clusters"] = [];
  for (const indexes of groups.values()) {
    const sourceCount = new Set(indexes.map((index) => candidates[index].source)).size;
    if (sourceCount < 2) continue;
    const representative = [...indexes].sort((a, b) => candidates[b].overallScore - candidates[a].overallScore || candidates[a].id.localeCompare(candidates[b].id))[0];
    const id = stableClusterId(indexes.map((index) => candidates[index].id));
    for (const index of indexes) candidates[index].clusterId = id;
    clusters.push({
      id,
      title: candidates[representative].title,
      representativeCandidateId: candidates[representative].id,
      sourceCount,
    });
  }
  candidates.sort((a, b) => b.overallScore - a.overallScore || a.id.localeCompare(b.id));
  clusters.sort((a, b) => b.sourceCount - a.sourceCount || a.title.localeCompare(b.title));
  return { candidates, clusters };
}
