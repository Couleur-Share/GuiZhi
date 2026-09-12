/** 本文问答：来源描述只含标识和摘录，不携带网络凭证。 */
export type ArticleView = "body" | "summary" | "transcript" | "recognized" | "replies" | "snapshot" | "themed";
export interface ArticleTarget {
  itemId: string;
  view: ArticleView;
  sourceKind?: "body" | "summary";
  versionId?: string;
  selection?: string;
  floor?: number;
}
export interface ArticleSource {
  evidence?: import("./evidence").EvidenceSnapshot;
  cleared?: boolean;
  ordinal: number;
  kind: "article" | "web";
  title: string;
  text: string;
  url?: string;
  target?: ArticleTarget;
  fingerprint?: string;
  capturedAt?: number;
}
export interface ArticleContext {
  target: ArticleTarget;
  title: string;
  fingerprint: string;
  sources: ArticleSource[];
  clipped: boolean;
}
export interface ArticleSearchResult {
  success: boolean;
  sources?: ArticleSource[];
  warnings?: string[];
  error?: string;
}
export interface ArticleAskApi {
  context(input: { target: ArticleTarget; question: string }): Promise<{ success: boolean; context?: ArticleContext; error?: string }>;
  search(input: { requestId: string; queries: string[] }): Promise<ArticleSearchResult>;
  cancelSearch(requestId: string): Promise<{ success: boolean; error?: string }>;
}
export interface ArticleMessage {
  id: string;
  question: string;
  answer: string;
  status: "running" | "done" | "error";
  context?: ArticleContext;
  sources: ArticleSource[];
  webEnabled: boolean;
  webStatus: "off" | "searching" | "ready" | "partial" | "failed";
  warnings: string[];
  step?: string;
  error?: string;
  model?: string;
  truncated?: boolean;
}
