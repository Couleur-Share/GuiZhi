export function l2Normalize(values: number[]): number[] {
  let sumOfSquares = 0;
  for (const value of values) {
    sumOfSquares += value * value;
  }
  const norm = Math.sqrt(sumOfSquares);
  if (!Number.isFinite(norm) || norm === 0) {
    return values.map(() => 0);
  }
  return values.map((value) => value / norm);
}

/** 解析 OpenAI 兼容的 embeddings 响应（按 index 对齐输入顺序） */
export function parseEmbeddingsResponse(
  body: string,
  expectedCount: number,
): number[][] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    throw new Error("Embeddings 响应不是合法 JSON");
  }
  const data = (parsed as { data?: unknown })?.data;
  if (!Array.isArray(data) || data.length !== expectedCount) {
    throw new Error(
      `Embeddings 响应数量不匹配：期望 ${expectedCount}，实际 ${Array.isArray(data) ? data.length : 0}`,
    );
  }

  const vectors: number[][] = new Array(expectedCount);
  for (let position = 0; position < data.length; position++) {
    const entry = data[position] as {
      index?: unknown;
      embedding?: unknown;
    };
    const embedding = entry?.embedding;
    if (!Array.isArray(embedding) || embedding.length === 0) {
      throw new Error("Embeddings 响应缺少向量数据");
    }
    const index =
      typeof entry.index === "number" &&
      entry.index >= 0 &&
      entry.index < expectedCount
        ? entry.index
        : position;
    vectors[index] = embedding as number[];
  }
  if (vectors.some((vector) => !vector)) {
    throw new Error("Embeddings 响应索引不连续");
  }
  const dims = vectors[0].length;
  if (vectors.some((vector) => vector.length !== dims)) {
    throw new Error("Embeddings 响应向量维度不一致");
  }
  return vectors;
}
