import type {
  DiscoveryDateConfidence,
  PlatformDiscoveryEngagement,
  PlatformDiscoveryMediaType,
} from "./platform-capture";

export const RESEARCH_SOURCES = [
  "xiaohongshu",
  "douyin",
  "bilibili",
] as const;
export type ResearchSource = (typeof RESEARCH_SOURCES)[number];

export const RESEARCH_DAY_RANGES = [7, 14, 30] as const;
export type ResearchDayRange = (typeof RESEARCH_DAY_RANGES)[number];
export type ResearchDepth = "quick" | "deep";
export type ResearchRunStatus =
  | "collecting"
  | "ready"
  | "partial"
  | "failed"
  | "canceled";
export type ResearchReportStatus = "none" | "generating" | "ready" | "failed";
export type ResearchSourceStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "partial"
  | "login_required"
  | "failed"
  | "canceled";
export type ResearchCandidateState =
  | "available"
  | "queued"
  | "imported"
  | "dismissed";

export interface ResearchRun {
  id: string;
  topic: string;
  dayRange: ResearchDayRange;
  rangeFrom: number;
  rangeTo: number;
  depth: ResearchDepth;
  sources: ResearchSource[];
  status: ResearchRunStatus;
  reportStatus: ResearchReportStatus;
  reportMarkdown: string | null;
  reportError: string | null;
  reportPromptVersion: string | null;
  savedItemId: string | null;
  candidateCount: number;
  clusterCount: number;
  createdAt: number;
  updatedAt: number;
  completedAt: number | null;
}

export interface ResearchSourceRun {
  runId: string;
  source: ResearchSource;
  status: ResearchSourceStatus;
  method: string;
  collectedCount: number;
  errorCode: string | null;
  error: string | null;
  startedAt: number | null;
  finishedAt: number | null;
}

export interface ResearchCandidateInput {
  source: ResearchSource;
  externalId: string;
  url: string;
  title: string;
  author: string;
  snippet?: string;
  publishedAt?: number;
  dateConfidence?: DiscoveryDateConfidence;
  mediaType: PlatformDiscoveryMediaType;
  engagement?: PlatformDiscoveryEngagement;
  discoveryMethod: string;
}

export interface ResearchCandidate
  extends Omit<ResearchCandidateInput, "snippet" | "publishedAt" | "dateConfidence" | "engagement"> {
  id: string;
  runId: string;
  normalizedUrl: string;
  snippet: string;
  publishedAt: number | null;
  dateConfidence: DiscoveryDateConfidence;
  engagement: PlatformDiscoveryEngagement;
  relevanceScore: number;
  recencyScore: number;
  engagementScore: number;
  overallScore: number;
  clusterId: string | null;
  state: ResearchCandidateState;
  importTaskId: string | null;
  importedItemId: string | null;
  createdAt: number;
  updatedAt: number;
}

export interface ResearchCluster {
  id: string;
  runId: string;
  title: string;
  representativeCandidateId: string;
  sourceCount: number;
  candidates: ResearchCandidate[];
}

export interface ResearchRunDetail {
  run: ResearchRun;
  sources: ResearchSourceRun[];
  candidates: ResearchCandidate[];
  clusters: ResearchCluster[];
}

export interface CreateResearchRunInput {
  topic: string;
  dayRange: ResearchDayRange;
  depth: ResearchDepth;
  sources: ResearchSource[];
}

export interface ResearchSearchInput {
  topic: string;
  rangeFrom: number;
  rangeTo: number;
  cursor: string | null;
  limit: number;
  signal: AbortSignal;
}

export interface ResearchPage {
  items: ResearchCandidateInput[];
  cursor: string | null;
  hasMore: boolean;
}

export interface ResearchEvidenceItem {
  ref: string;
  candidateId: string;
  source: ResearchSource;
  title: string;
  author: string;
  snippet: string;
  publishedAt: number | null;
  dateConfidence: DiscoveryDateConfidence;
  engagement: PlatformDiscoveryEngagement;
  overallScore: number;
  url: string;
}

export interface ResearchEvidencePacket {
  runId: string;
  topic: string;
  rangeFrom: number;
  rangeTo: number;
  sourceRuns: ResearchSourceRun[];
  items: ResearchEvidenceItem[];
}

export interface SaveResearchReportInput {
  runId: string;
  markdown: string;
  promptVersion: string;
}

export interface EnqueueResearchCandidatesInput {
  runId: string;
  candidateIds: string[];
}

export function isResearchSource(value: unknown): value is ResearchSource {
  return RESEARCH_SOURCES.includes(value as ResearchSource);
}

export function isResearchDayRange(value: unknown): value is ResearchDayRange {
  return RESEARCH_DAY_RANGES.includes(value as ResearchDayRange);
}
