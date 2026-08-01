/**
 * 语义索引 / 检索类型。
 *
 * 向量由渲染进程调用 embedding API 生成（沿用「渲染进程编排 AI、
 * 主进程持久化」的架构约定），主进程负责存储与余弦 top-k 检索。
 * contentHash 由主进程在 listPending 时计算下发，渲染进程写回时原样回传，
 * 保证判定与落库使用同一份哈希。
 */

export interface SemanticIndexStatus {
  /** 已建立索引且哈希/模型均匹配的条目数 */
  indexedItems: number;
  /** 应当被索引的条目数（未删除且内容非空） */
  eligibleItems: number;
  totalChunks: number;
  /**
   * 最近一次语义检索耗时（毫秒）；尚未检索过为 null。
   * 进程内观测，不落库。
   */
  lastSearchMs: number | null;
  /** 最近一次检索扫描的分块数 */
  lastScannedChunks: number | null;
  /** 最近一次检索是否复用了进程内向量缓存；null 表示尚未检索过。 */
  lastSearchCacheHit: boolean | null;
}

/** 待索引条目（内容随批下发，渲染进程直接分块嵌入） */
export interface PendingSemanticItem {
  id: string;
  title: string;
  content: string;
  transcript: string | null;
  contentHash: string;
}

export interface SemanticChunkInput {
  text: string;
  /** 已 L2 归一化的向量 */
  vector: number[];
}

export interface ApplySemanticEmbeddingsInput {
  itemId: string;
  contentHash: string;
  model: string;
  dims: number;
  chunks: SemanticChunkInput[];
}

export interface SemanticSearchHit {
  itemId: string;
  title: string;
  /** 命中分块的文本节选 */
  snippet: string;
  /** 余弦相似度（向量已归一化，即点积），条目内取最高分块 */
  score: number;
}
