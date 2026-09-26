/** 重构稿与原始 Markdown 分开保存；研究记录不进入阅读正文。 */
export interface ReadingReference {
  id: string; title: string; url: string; capturedAt: number; publishedAt?: string;
  text: string; status: "ready" | "failed"; error?: string;
}
export interface ReadingQuery {
  query: string; done: boolean;
  results: { title: string; url: string; text?: string; publishedAt?: string }[];
}
export interface ReadingResearchReview {
  adequate: boolean;
  missing: string;
  followUpQueries: string[];
}
/** 每批最多三次搜索、八篇资料；独立保存，恢复时不重做已完成的请求。 */
export interface ReadingResearchBatch {
  queries: ReadingQuery[];
  selectedUrls?: string[];
  readUrls?: string[];
  researchReview?: ReadingResearchReview;
}
export interface ReadingOutline {
  title: string; direction: string; questions: string[];
  /** 简短的公开检索词；旧检查点可能没有，查证问题仍用于证据评审。 */
  searchQueries?: string[];
  sections: { title: string; brief: string; reuseFrom?: number; referenceIds?: string[] }[];
}
export interface ReadingDraftSection { title: string; markdown: string; referenceIds: string[]; }
export interface ReadingInteraction {
  id: string; kind: "tabs" | "filter" | "scenario" | "calculator";
  choices?: { label: string; value: string }[];
  inputs?: { name: string; label: string; unit?: string; min?: number; max?: number }[];
  expression?: string; unit?: string;
}
export interface ReadingReconstruction extends ReadingResearchBatch {
  visualVersion?: 1;
  visuals?: import("./reading-visuals").ReadingVisual[];
  animations?: import("./reading-visuals").ReadingAnimation[];
  notes: string[];
  editorNotes?: string;
  outline?: ReadingOutline;
  /** 初次查证之外，最多两批自动补查；旧检查点省略这些字段仍可继续。 */
  researchBatches?: ReadingResearchBatch[];
  references: ReadingReference[];
  researchComplete?: boolean;
  draft: ReadingDraftSection[];
  revisionDraft?: ReadingDraftSection[];
  interactions: ReadingInteraction[];
}
export type ReadingSearchProvider = "tavily" | "anysearch" | "native";
export interface ReadingSearchConfigInput { provider?: ReadingSearchProvider; apiKey?: string; test?: boolean; defaultEnabled?: boolean; }
export interface ReadingSearchStatus {
  configured: boolean; persistent: boolean; provider: ReadingSearchProvider; configuredProviders?: ReadingSearchProvider[]; defaultEnabled?: boolean;
  /** 表示主文本模型配置可用于发起测试，不代表服务端已通过搜索能力验证。 */
  nativeModel?: string; nativeUnavailableReason?: string;
}
export interface ReadingState {
  hasPage: boolean; versionId?: string; formatVersion?: number; stale: boolean;
  task?: import("./themed-reading").ThemedReadingTask;
}
