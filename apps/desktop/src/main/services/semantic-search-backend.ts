import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import type { SemanticVectorCache } from "./semantic-vector-cache";

export interface ScoredSemanticChunk {
  itemId: string;
  chunkIndex: number;
  score: number;
}

export interface SemanticSearchBackend {
  readonly name: "exact" | "hnsw";
  search(query: Float32Array, limit: number): Promise<ScoredSemanticChunk[]>;
}

export interface SemanticBackendThresholds {
  chunkCount: number;
  vectorBytes: number;
  warmMedianMs: number | null;
}

export const HNSW_CHUNK_THRESHOLD = 50_000;
export const HNSW_VECTOR_BYTES_THRESHOLD = 256 * 1024 * 1024;
export const HNSW_WARM_MEDIAN_THRESHOLD_MS = 500;
const YIELD_EVERY_CHUNKS = 2_000;

export function shouldUseHnsw(input: SemanticBackendThresholds): boolean {
  return (
    input.chunkCount >= HNSW_CHUNK_THRESHOLD ||
    input.vectorBytes > HNSW_VECTOR_BYTES_THRESHOLD ||
    (input.warmMedianMs !== null && input.warmMedianMs > HNSW_WARM_MEDIAN_THRESHOLD_MS)
  );
}

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

export class ExactSemanticSearchBackend implements SemanticSearchBackend {
  readonly name = "exact" as const;
  constructor(private readonly cache: SemanticVectorCache) {}

  async search(query: Float32Array, limit: number): Promise<ScoredSemanticChunk[]> {
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
      }
    }
    return topItems(best, Math.max(1, limit));
  }
}

interface HnswIndex {
  initIndex(maxElements: number, m: number, efConstruction: number, seed: number): void;
  addPoint(point: Float32Array | number[], label: number, replaceDeleted: boolean): void;
  searchKnn(query: Float32Array | number[], count: number, filter?: undefined): { distances: number[]; neighbors: number[] };
  setEfSearch(value: number): void;
  writeIndex(filename: string): Promise<boolean>;
  readIndex(filename: string, maxElements: number): Promise<boolean>;
}

interface HnswRuntime {
  HierarchicalNSW: new (space: "cosine", dims: number, autosave: string) => HnswIndex;
  FS?: {
    readFile(name: string): Uint8Array;
    writeFile(name: string, data: Uint8Array): void;
    unlink(name: string): void;
  };
}

interface SidecarMetadata {
  version: 1;
  model: string;
  dims: number;
  generation: string;
  labels: Array<{ itemId: string; chunkIndex: number }>;
}

/**
 * hnswlib-wasm 适配器。该包当前发行物只编译了 Web 环境；若 Electron 主进程
 * 无法加载，factory 会抛出并由调用方永久回退精确扫描。
 */
export class HnswSemanticSearchBackend implements SemanticSearchBackend {
  readonly name = "hnsw" as const;
  private constructor(
    private readonly index: HnswIndex,
    private readonly labels: SidecarMetadata["labels"],
  ) {}

  static async create(input: {
    cache: SemanticVectorCache;
    model: string;
    generation: string;
    rootDir: string;
  }): Promise<HnswSemanticSearchBackend> {
    const packagePath = "hnswlib-wasm/dist/hnswlib.js";
    const module = (await import(/* @vite-ignore */ packagePath)) as {
      loadHnswlib: () => Promise<HnswRuntime>;
    };
    const runtime = await module.loadHnswlib();
    if (!runtime.FS) throw new Error("hnswlib-wasm 未暴露可持久化文件系统");
    const modelHash = createHash("sha256")
      .update(`${input.model}\0${input.cache.dims}`)
      .digest("hex")
      .slice(0, 24);
    const dir = path.join(input.rootDir, modelHash);
    const basename = `generation-${input.generation}`;
    const indexPath = path.join(dir, `${basename}.bin`);
    const metadataPath = path.join(dir, `${basename}.json`);
    const virtualName = `${modelHash}-${input.generation}.bin`;
    const index = new runtime.HierarchicalNSW("cosine", input.cache.dims, "");
    let labels: SidecarMetadata["labels"];

    if (fs.existsSync(indexPath) && fs.existsSync(metadataPath)) {
      const metadata = JSON.parse(fs.readFileSync(metadataPath, "utf8")) as SidecarMetadata;
      if (
        metadata.version !== 1 ||
        metadata.model !== input.model ||
        metadata.dims !== input.cache.dims ||
        metadata.generation !== input.generation
      ) throw new Error("HNSW 侧车元数据不匹配");
      runtime.FS.writeFile(virtualName, fs.readFileSync(indexPath));
      await index.readIndex(virtualName, Math.max(metadata.labels.length + 1, 1));
      labels = metadata.labels;
    } else {
      const count = input.cache.itemIds.length;
      index.initIndex(Math.max(count + 1, 1), 32, 240, 100);
      labels = [];
      for (let label = 0; label < count; label += 1) {
        const offset = label * input.cache.dims;
        index.addPoint(
          input.cache.vectors.subarray(offset, offset + input.cache.dims),
          label,
          false,
        );
        labels.push({
          itemId: input.cache.itemIds[label],
          chunkIndex: input.cache.chunkIndexes[label],
        });
      }
      fs.mkdirSync(dir, { recursive: true });
      await index.writeIndex(virtualName);
      const metadata: SidecarMetadata = {
        version: 1,
        model: input.model,
        dims: input.cache.dims,
        generation: input.generation,
        labels,
      };
      const tempSuffix = `.tmp-${process.pid}-${Date.now()}`;
      fs.writeFileSync(`${indexPath}${tempSuffix}`, runtime.FS.readFile(virtualName));
      fs.writeFileSync(`${metadataPath}${tempSuffix}`, JSON.stringify(metadata));
      fs.renameSync(`${indexPath}${tempSuffix}`, indexPath);
      fs.renameSync(`${metadataPath}${tempSuffix}`, metadataPath);
    }
    index.setEfSearch(128);
    try { runtime.FS.unlink(virtualName); } catch { /* best effort */ }
    return new HnswSemanticSearchBackend(index, labels);
  }

  async search(query: Float32Array, limit: number): Promise<ScoredSemanticChunk[]> {
    const requested = Math.min(this.labels.length, Math.max(limit * 8, 64));
    if (requested === 0) return [];
    const result = this.index.searchKnn(query, requested, undefined);
    const best = new Map<string, { chunkIndex: number; score: number }>();
    result.neighbors.forEach((label, index) => {
      const target = this.labels[label];
      if (!target) return;
      const score = 1 - result.distances[index];
      const current = best.get(target.itemId);
      if (!current || score > current.score) best.set(target.itemId, { chunkIndex: target.chunkIndex, score });
    });
    return topItems(best, limit);
  }
}

const incompatiblePlatforms = new Set<string>();
const hnswByKey = new Map<string, Promise<HnswSemanticSearchBackend>>();

export async function resolveSemanticSearchBackend(input: {
  cache: SemanticVectorCache;
  model: string;
  generation: string;
  rootDir: string;
  warmMedianMs: number | null;
}): Promise<SemanticSearchBackend> {
  const platformKey = `${process.platform}-${process.arch}`;
  if (
    incompatiblePlatforms.has(platformKey) ||
    !shouldUseHnsw({
      chunkCount: input.cache.itemIds.length,
      vectorBytes: input.cache.vectors.byteLength,
      warmMedianMs: input.warmMedianMs,
    })
  ) return new ExactSemanticSearchBackend(input.cache);

  const key = `${input.model}\0${input.cache.dims}\0${input.generation}`;
  try {
    let backend: Promise<HnswSemanticSearchBackend>;
    if (hnswByKey.has(key)) {
      backend = hnswByKey.get(key)!;
    } else {
      backend = HnswSemanticSearchBackend.create(input);
      hnswByKey.set(key, backend);
    }
    return await backend;
  } catch (error) {
    incompatiblePlatforms.add(platformKey);
    console.warn(`[semantic] ${platformKey} 的 hnswlib-wasm 不可用，保留精确扫描:`, error);
    return new ExactSemanticSearchBackend(input.cache);
  }
}

export function resetSemanticBackendCompatibilityForTests(): void {
  incompatiblePlatforms.clear();
  hnswByKey.clear();
}
