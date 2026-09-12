/** AI 主题阅读页：正文独立保存，页面与生成任务均以内容来源隔离。 */
export type ThemedReadingSourceKind = "body" | "summary";
export type ThemedReadingTaskState = "queued" | "running" | "completed" | "partial" | "failed" | "cancelled" | "interrupted";
export type ThemedReadingStage = "prepare" | "understand" | "research" | "write" | "design" | "images" | "assemble" | "validate" | "done";
export interface ThemedReadingOptions {
  enhancedInteraction?: boolean;
  researchDepth?: "standard" | "deep";
  style: string;
  research?: boolean;
  action?: "create" | "revise" | "redesign" | "refresh";
  generateImages: boolean;
  maxImages: number;
  /** 使用当前页面的内容快照和素材重新设计。 */
  fromCurrent?: boolean;
}
export interface ThemedReadingBlock { id: string; markdown: string; html: string; text: string; }
export interface ThemedReadingSource {
  title: string;
  content: string;
  sourceUri: string | null;
  fingerprint: string;
  blocks: ThemedReadingBlock[];
}
export interface ThemedReadingAsset {
  id: string;
  role: "original" | "generated";
  purpose: string;
  prompt: string;
  alt: string;
  aspectRatio: "16:9" | "4:3" | "1:1";
  blockId?: string;
  originalUrl?: string;
  fileName?: string;
  sha256?: string;
  bytes?: number;
  status: "pending" | "ready" | "failed";
  error?: string;
}
export interface ThemedReadingDesign {
  scripts?: import("./reading-page-v3").ReadingScriptModule[];
  libraries?: import("./reading-page-v3").ReadingLibrary[];
  visualResults?: import("./reading-visuals").ReadingVisualResult[];
  direction: string;
  html: string;
  css: string;
  assets: ThemedReadingAsset[];
  /** 可追溯专题重构。存在时由可信组件渲染，html/css 不参与执行。 */
  composition?: import("./themed-composition").ThemedComposition;
}
export interface ThemedReadingVersion {
  generation?: import("./reading-page-v3").ReadingGeneration;
  id: string;
  itemId: string;
  sourceKind: ThemedReadingSourceKind;
  role: "current" | "previous" | "working";
  formatVersion: number;
  reconstruction?: import("./reading-reconstruction").ReadingReconstruction;
  source: ThemedReadingSource;
  options: ThemedReadingOptions;
  design: ThemedReadingDesign | null;
  /** 分章节设计检查点，继续任务无需重做已完成章节。 */
  designParts?: ThemedReadingDesign[];
  designDirection?: string;
  assets: ThemedReadingAsset[];
  warnings: string[];
  textModel?: string;
  imageModel?: string;
  createdAt: number;
  updatedAt: number;
}
export interface ThemedReadingTask {
  reused?: { chapters: number; notes: number; references: number; assets: number };
  timings?: Partial<Record<ThemedReadingStage, number>>;
  firstContentAt?: number;
  previewRevision?: number;
  issues?: import("./reading-page-v3").ReadingIssue[];
  id: string;
  itemId: string;
  sourceKind: ThemedReadingSourceKind;
  versionId: string;
  title: string;
  state: ThemedReadingTaskState;
  stage: ThemedReadingStage;
  completed: number;
  total: number;
  /** 本轮实际准备调用生图的槽位数量，不含下载原图和复用素材。 */
  plannedImages?: number;
  /** 设计请求的真实输出进度；旧任务可缺省。 */
  designProgress?: { attempt: number; receivedChars: number };
  /** 请求发出前持久记账；新图保存仅在文件与版本检查点提交后累计。旧任务缺失表示未记录。 */
  usage?: { textCalls: number; imageCalls: number; imagesSaved: number; searchCalls?: number; pagesRead?: number };
  error?: string;
  /** 替换图片任务只执行这个槽位；不重新设计页面。 */
  assetId?: string;
  createdAt: number;
  updatedAt: number;
}
export interface ThemedReadingResult {
  success: boolean;
  error?: string;
  task?: ThemedReadingTask;
  tasks?: ThemedReadingTask[];
  page?: ThemedReadingVersion | null;
  previous?: boolean;
  stale?: boolean;
  document?: string;
  models?: { text: string | null; image: string | null };
  search?: import("./reading-reconstruction").ReadingSearchStatus;
  state?: import("./reading-reconstruction").ReadingState;
  references?: Omit<import("./reading-reconstruction").ReadingReference, "text">[];
  cancelled?: boolean;
  path?: string;
}
export interface ThemedReadingRequest { itemId: string; sourceKind: ThemedReadingSourceKind; }
export interface ThemedReadingAPI {
  getState(input: ThemedReadingRequest): Promise<ThemedReadingResult>;
  references(input: ThemedReadingRequest): Promise<ThemedReadingResult>;
  searchConfig(input?: import("./reading-reconstruction").ReadingSearchConfigInput): Promise<ThemedReadingResult>;
  continueOffline(taskId: string): Promise<ThemedReadingResult>;
  get(input: ThemedReadingRequest & { instanceId?: string; metadataOnly?: boolean }): Promise<ThemedReadingResult>;
  generate(input: ThemedReadingRequest & { options: ThemedReadingOptions }): Promise<ThemedReadingResult>;
  cancel(taskId: string): Promise<ThemedReadingResult>;
  resume(taskId: string): Promise<ThemedReadingResult>;
  regenerateAsset(input: ThemedReadingRequest & { assetId: string }): Promise<ThemedReadingResult>;
  restorePrevious(input: ThemedReadingRequest): Promise<ThemedReadingResult>;
  remove(input: ThemedReadingRequest): Promise<ThemedReadingResult>;
  exportHtml(input: ThemedReadingRequest & { withoutImages?: boolean; staticOnly?: boolean; versionId?: string }): Promise<ThemedReadingResult>;
  preview(input: ThemedReadingRequest & { taskId: string }): Promise<import("./reading-page-v3").ReadingViewResult>;
  createView(input: import("./reading-page-v3").ReadingViewRequest): Promise<import("./reading-page-v3").ReadingViewResult>;
  updateView(input: { viewId: string; bounds: import("./reading-page-v3").ReadingViewBounds }): Promise<import("./reading-page-v3").ReadingViewResult>;
  commandView(input: { viewId: string; command: import("./reading-page-v3").ReadingViewCommand }): Promise<import("./reading-page-v3").ReadingViewResult>;
  destroyView(viewId: string): Promise<import("./reading-page-v3").ReadingViewResult>;
  onViewEvent(callback: (event: import("./reading-page-v3").ReadingViewEvent) => void): () => void;
  listTasks(): Promise<ThemedReadingResult>;
  onProgress(callback: (task: ThemedReadingTask) => void): () => void;
}
