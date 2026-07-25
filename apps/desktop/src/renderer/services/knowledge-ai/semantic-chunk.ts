/**
 * 语义索引分块器：滑动窗口 + 重叠，每块前缀标题提供上下文。
 * 纯函数，便于单测。
 */

export const SEMANTIC_CHUNK_SIZE = 700;
export const SEMANTIC_CHUNK_OVERLAP = 80;
/** 超长内容截断到前 MAX 块（约 1.5 万字），避免单条目吃掉嵌入预算 */
export const SEMANTIC_MAX_CHUNKS = 24;

export function buildEmbeddingChunks(
  title: string,
  content: string,
  transcript: string | null,
): string[] {
  const bodyParts = [content.trim()];
  if (transcript?.trim()) {
    bodyParts.push(`【口播转写稿】\n${transcript.trim()}`);
  }
  const body = bodyParts.filter(Boolean).join("\n");
  const trimmedTitle = title.trim();
  if (!body) {
    // 只有标题的条目：标题本身就是可嵌入文本
    return trimmedTitle ? [trimmedTitle] : [];
  }

  const prefix = trimmedTitle ? `《${trimmedTitle}》\n` : "";
  if (body.length <= SEMANTIC_CHUNK_SIZE) {
    return [`${prefix}${body}`];
  }

  const chunks: string[] = [];
  const step = SEMANTIC_CHUNK_SIZE - SEMANTIC_CHUNK_OVERLAP;
  for (
    let start = 0;
    start < body.length && chunks.length < SEMANTIC_MAX_CHUNKS;
    start += step
  ) {
    chunks.push(`${prefix}${body.slice(start, start + SEMANTIC_CHUNK_SIZE)}`);
    if (start + SEMANTIC_CHUNK_SIZE >= body.length) {
      break;
    }
  }
  return chunks;
}
