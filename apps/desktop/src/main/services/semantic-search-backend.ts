import type { SemanticVectorCache } from "./semantic-vector-cache";

export interface ScoredSemanticChunk {
  itemId: string;
  chunkIndex: number;
  score: number;
}

const YIELD_EVERY_CHUNKS = 2_000;

function topItems(
  entries: Iterable<[string, { chunkIndex: number; score: number }]>,
  limit: number,
): ScoredSemanticChunk[] {
  const heap: ScoredSemanticChunk[] = [];
  const isWorse = (a: ScoredSemanticChunk, b: ScoredSemanticChunk) =>
    a.score < b.score || (a.score === b.score && a.itemId.localeCompare(b.itemId) > 0);
  const siftUp = (start: number) => {
    let index = start;
    while (index > 0) {
      const parent = Math.floor((index - 1) / 2);
      if (!isWorse(heap[index], heap[parent])) break;
      [heap[index], heap[parent]] = [heap[parent], heap[index]];
      index = parent;
    }
  };
  const siftDown = () => {
    let index = 0;
    for (;;) {
      const left = index * 2 + 1;
      const right = left + 1;
      let worst = index;
      if (left < heap.length && isWorse(heap[left], heap[worst])) worst = left;
      if (right < heap.length && isWorse(heap[right], heap[worst])) worst = right;
      if (worst === index) break;
      [heap[index], heap[worst]] = [heap[worst], heap[index]];
      index = worst;
    }
  };

  const count = Math.max(1, limit);
  for (const [itemId, value] of entries) {
    const candidate = { itemId, ...value };
    if (heap.length < count) {
      heap.push(candidate);
      siftUp(heap.length - 1);
    } else if (isWorse(heap[0], candidate)) {
      heap[0] = candidate;
      siftDown();
    }
  }
  return heap.sort((a, b) => b.score - a.score || a.itemId.localeCompare(b.itemId));
}

/** 精确余弦扫描：向量已归一化，点积即相似度；分批让出事件循环，可取消。 */
export class ExactSemanticSearchBackend {
  constructor(private readonly cache: SemanticVectorCache) {}

  async search(query: Float32Array, limit: number, signal?: AbortSignal): Promise<ScoredSemanticChunk[]> {
    signal?.throwIfAborted();
    const best = new Map<string, { chunkIndex: number; score: number }>();
    const dims = query.length;
    if (dims === 0 || this.cache.dims !== dims) return [];
    for (let index = 0; index < this.cache.itemIds.length; index += 1) {
      let dot = 0;
      const offset = index * dims;
      for (let d = 0; d < dims; d += 1) dot += query[d] * this.cache.vectors[offset + d];
      const itemId = this.cache.itemIds[index];
      const current = best.get(itemId);
      if (!current || dot > current.score) {
        best.set(itemId, { chunkIndex: this.cache.chunkIndexes[index], score: dot });
      }
      if ((index + 1) % YIELD_EVERY_CHUNKS === 0) {
        await new Promise<void>((resolve) => setImmediate(resolve));
        signal?.throwIfAborted();
      }
    }
    return topItems(best, Math.max(1, limit));
  }
}
