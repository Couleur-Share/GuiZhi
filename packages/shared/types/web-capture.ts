/** 网页采集的受限协议；不暴露上游脚本、文件路径或配置对象。 */
export type WebCapturePurpose = "import" | "documents" | "research";
export type WebCaptureErrorCode =
  | "unavailable"
  | "damaged"
  | "security"
  | "network"
  | "restricted"
  | "login"
  | "captcha"
  | "empty"
  | "timeout"
  | "canceled"
  | "incomplete";
export interface WebCaptureError {
  code: WebCaptureErrorCode;
  message: string;
  retryable: boolean;
}
export interface WebScope {
  origin: string;
  directory: string;
}
export interface WebSeed {
  url: string;
  mode: "page" | "directory";
  directory?: string;
}
export interface WebCaptureRequest {
  taskId: string;
  purpose: WebCapturePurpose;
  url: string;
  scope?: WebScope;
}
export interface WebSnapshotAsset { fileName: string; sourceUrl: string; sha256: string; bytes: number; }
export interface WebSnapshot {
  formatVersion: 1; policyVersion: 1; adapterVersion: string;
  html: string; css: string; hash: string;
  account: string; author: string; publishedAt: number | null; cover?: string;
  assets: WebSnapshotAsset[];
  failures: { url: string; reason: string }[];
  warnings: string[];
}
export interface WebSnapshotView {
  version: WebSourceVersion | null; edited: boolean; pending: boolean;
  document?: string; instanceId?: string; error?: string;
}
export interface WebCaptureResult {
  snapshot?: WebSnapshot;
  taskId: string;
  entryUrl: string;
  finalUrl: string;
  title: string;
  author: string;
  publishedAt: number | null;
  dateConfidence: "exact" | "inferred" | "unknown";
  markdown: string;
  links: string[];
  paragraphs: { id: string; text: string }[];
  contentHash: string;
  capturedAt: number;
  engineVersion: string;
  complete: boolean;
  truncated: boolean;
  warnings: string[];
  error?: WebCaptureError;
}
export interface WebRuntimeStatus {
  supported: boolean;
  available: boolean;
  running: boolean;
  version: string;
  reason?: string;
  repairRequired?: boolean;
  runtimeTarget: string;
}
export type CrawlJobStatus =
  "pending" | "running" | "paused" | "interrupted" | "completed" | "canceled";
export type CrawlPageStatus =
  | "pending"
  | "running"
  | "added"
  | "duplicate"
  | "updated"
  | "pending-version"
  | "unchanged"
  | "failed"
  | "skipped"
  | "canceled";
export interface CreateCrawlJobInput {
  purpose: "documents" | "research";
  seeds: WebSeed[];
  collectionId?: string | null;
  maxPages?: number;
  maxDepth?: number;
  duplicatePolicy?: "skip" | "update";
  researchRunId?: string;
}
export interface CrawlJob {
  id: string;
  input: CreateCrawlJobInput;
  status: CrawlJobStatus;
  createdAt: number;
  updatedAt: number;
  error?: string;
  counts: Partial<Record<CrawlPageStatus, number>>;
}
export interface CrawlPage {
  stage?: import("./import").ImportStage;
  id: string;
  jobId: string;
  url: string;
  depth: number;
  seedIndex: number;
  status: CrawlPageStatus;
  itemId?: string;
  importTaskId?: string;
  error?: string;
  result?: WebCaptureResult;
}
export interface WebSourceVersion {
  snapshot?: WebSnapshot;
  id: string;
  itemId: string;
  sourceUrl: string;
  title: string;
  markdown: string;
  contentHash: string;
  capturedAt: number;
  engineVersion: string;
  complete: boolean;
  kind: "remote" | "local";
}
export interface AdoptWebVersionInput {
  itemId: string;
  versionId: string;
  expectedContentHash: string;
  expectedTitle: string;
}
