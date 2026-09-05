import type { ResearchSource, ResearchEvidencePacket, ResearchCandidate } from "./research";

export type ResearchIntent = "overview" | "comparison" | "how_to" | "recent";
export type ResearchKnowledgeScope = { kind: "all" } | { kind: "collection"; collectionId: string };
export interface ResearchPlan {
  intent: ResearchIntent;
  queries: string[];
  entities: string[];
  version: string;
  fallback?: string;
}
export interface ResearchContext {
  seriesId: string;
  phase: "planning" | "searching" | "reading" | "idle";
  policyVersion: string;
  plan?: ResearchPlan;
  knowledgeScope?: ResearchKnowledgeScope;
  baselineRunId?: string;
  reportOutdated: boolean;
  activeReportId?: string;
  savedReportId?: string;
}
export type ResearchEligibility = "recent" | "undated" | "out_of_window" | "irrelevant" | "entity_miss" | "dismissed";
export interface ResearchQueryAttempt {
  id: string;
  runId: string;
  source: ResearchSource;
  query: string;
  cursor: string | null;
  nextCursor: string | null;
  finished: boolean;
  method: string;
  startedAt: number;
  finishedAt: number | null;
  returnedCount: number;
  inWindowCount: number;
  unknownDateCount: number;
  capped: boolean;
  failureStage?: "navigation" | "query" | "parse" | "dependency" | "verification";
  errorCode?: string;
  error?: string;
}
export interface ResearchPassage {
  text: string;
  kind: "body" | "description" | "caption" | "comment" | "local" | "metadata";
  position: number;
  startMs?: number;
  endMs?: number;
  author?: string;
  externalId?: string;
}
export interface ResearchDocument {
  id: string;
  runId: string;
  candidateId: string;
  source: ResearchSource;
  url: string;
  title: string;
  author: string;
  publishedAt: number | null;
  capturedAt: number;
  status: "reading" | "ready" | "partial" | "failed" | "interrupted";
  passages: ResearchPassage[];
  contentHash: string | null;
  truncated: boolean;
  warning?: string;
  error?: string;
  savedItemId?: string;
}
export interface ResearchLocalEvidence {
  ref: string;
  itemId: string;
  title: string;
  excerpt: string;
  updatedAt: number;
  capturedAt?: number;
  url?: string;
}
export interface ResearchSnapshot {
  id: string;
  operationId: string;
  runId: string;
  createdAt: number;
  status: "generating" | "ready" | "failed" | "canceled";
  packet: ResearchEvidencePacket;
  markdown?: string;
  error?: string;
}
export type ResearchChangeKind = "new" | "continued" | "changed" | "not_found" | "outside_window" | "unknown";
export interface ResearchChange {
  kind: ResearchChangeKind;
  current?: ResearchCandidate;
  previous?: ResearchCandidate;
  engagementChanges?: Record<string, number>;
}
export interface ResearchComparison {
  runId: string;
  baselineRunId: string | null;
  warnings: string[];
  changes: ResearchChange[];
}
