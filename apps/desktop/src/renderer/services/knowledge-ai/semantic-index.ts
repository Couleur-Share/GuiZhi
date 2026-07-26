/**
 * 语义索引循环：拉取待索引批次 → 分块 → 嵌入 → 落库。
 * embedding 模型未配置时静默跳过（语义检索是可选增强）。
 */
import { embedTexts, resolveEmbeddingConfig } from "./embeddings";
import { buildEmbeddingChunks } from "./semantic-chunk";

const PENDING_BATCH_SIZE = 10;
/** 连续失败即中止本轮（API 故障时避免空转烧配额） */
const MAX_CONSECUTIVE_FAILURES = 3;

export interface SemanticIndexRunResult {
  /** 未配置 embedding 模型，本轮未执行 */
  skipped: boolean;
  indexed: number;
  failed: number;
  /**
   * 最后一次失败的原因。
   *
   * 每条目的失败原先只进 console，界面拿不到任何可说的话——嵌入接口
   * 报 401 也好、模型不支持 embeddings 也好，用户看到的都是「点了没反应」。
   */
  lastError?: string;
}

export async function runSemanticIndexing(
  onProgress?: (indexed: number) => void,
  signal?: AbortSignal,
): Promise<SemanticIndexRunResult> {
  const config = resolveEmbeddingConfig();
  if (!config) {
    return { skipped: true, indexed: 0, failed: 0 };
  }

  let indexed = 0;
  let failed = 0;
  let consecutiveFailures = 0;
  let lastError: string | undefined;
  const attempted = new Set<string>();

  while (true) {
    if (signal?.aborted) {
      break;
    }
    const batch = await window.api.semantic.listPending({
      model: config.model,
      limit: PENDING_BATCH_SIZE,
    });
    // 全部条目都已尝试过仍然 pending（落库失败等），停止避免死循环
    const fresh = batch.filter((item) => !attempted.has(item.id));
    if (fresh.length === 0) {
      break;
    }

    for (const item of fresh) {
      if (signal?.aborted) {
        break;
      }
      attempted.add(item.id);
      const chunks = buildEmbeddingChunks(
        item.title,
        item.content,
        item.transcript,
      );
      if (chunks.length === 0) {
        continue;
      }
      try {
        const vectors = await embedTexts(config, chunks);
        const applied = await window.api.semantic.applyEmbeddings({
          itemId: item.id,
          contentHash: item.contentHash,
          model: config.model,
          dims: vectors[0].length,
          chunks: chunks.map((text, index) => ({
            text,
            vector: vectors[index],
          })),
        });
        if (applied) {
          indexed++;
          consecutiveFailures = 0;
          onProgress?.(indexed);
        } else {
          failed++;
          lastError = "向量落库失败（条目可能已被删除）";
        }
      } catch (error) {
        failed++;
        consecutiveFailures++;
        lastError = error instanceof Error ? error.message : String(error);
        console.warn(`[semantic] 条目嵌入失败（${item.id}）:`, lastError);
        if (consecutiveFailures >= MAX_CONSECUTIVE_FAILURES) {
          return { skipped: false, indexed, failed, lastError };
        }
      }
    }
  }

  return { skipped: false, indexed, failed, lastError };
}
