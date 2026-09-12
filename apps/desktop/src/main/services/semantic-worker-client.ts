import { Worker } from 'node:worker_threads';
import path from 'node:path';
import fs from 'node:fs/promises';
import { createHash } from 'node:crypto';
import type { SemanticVectorCache } from './semantic-vector-cache';
import type { ScoredSemanticChunk, SemanticSearchBackend } from './semantic-search-backend';

interface Input { cache: SemanticVectorCache; model: string; generation: string; rootDir: string }
interface Reply { id: number; ready?: boolean; hits?: ScoredSemanticChunk[]; error?: string; incompatible?: boolean }
/** 每个索引代际独立 worker；终止 worker 同时释放其整个 WASM 堆。 */
export class SemanticWorkerClient implements SemanticSearchBackend {
  readonly name = 'hnsw' as const;
  private sequence = 0;
  private requests = new Map<number, { resolve: (reply: Reply) => void; reject: (error: Error) => void }>();
  private retiring = false;
  private dead = false;
  private constructor(private worker: Worker) {
    worker.on('message', (reply: Reply) => {
      const pending = this.requests.get(reply.id); this.requests.delete(reply.id);
      if (reply.error) pending?.reject(Object.assign(new Error(reply.error), { incompatible: reply.incompatible }));
      else pending?.resolve(reply);
      this.releaseIfIdle();
    });
    const fail = (error: Error) => { this.dead = true; for (const pending of this.requests.values()) pending.reject(error); this.requests.clear(); };
    worker.on('error', fail);
    worker.on('exit', code => fail(new Error(`索引工作线程已结束（${code}）`)));
    worker.unref();
  }
  static async create(input: Input): Promise<SemanticWorkerClient> {
    const client = new SemanticWorkerClient(new Worker(path.join(__dirname, '../semantic-worker/semantic-worker.js')));
    try { await client.request({ action: 'init', input }); return client; }
    catch (error) { client.retire(); throw error; }
  }
  private request(message: object): Promise<Reply> {
    if (this.dead) return Promise.reject(new Error('索引工作线程已释放'));
    return new Promise((resolve, reject) => {
      const id = ++this.sequence; this.requests.set(id, { resolve, reject });
      try { this.worker.postMessage({ ...message, id }); }
      catch (error) { this.requests.delete(id); reject(error); }
    });
  }
  async search(query: Float32Array, limit: number): Promise<ScoredSemanticChunk[]> {
    return (await this.request({ action: 'search', query, limit })).hits ?? [];
  }
  isAlive(): boolean { return !this.dead; }
  retire(): void { this.retiring = true; this.releaseIfIdle(); }
  private releaseIfIdle(): void {
    if (this.retiring && !this.requests.size && !this.dead) { this.dead = true; void this.worker.terminate(); }
  }
}

const generations = new Map<string, { group: string; generation: string; directory: string; promise: Promise<SemanticWorkerClient> }>();
const sidecarDirectories = new Map<string, Set<string>>();
export function workerGenerationCount(): number { return generations.size; }
export function resetSemanticWorkers(): void {
  for (const value of generations.values()) void value.promise.then(client => client.retire(), () => undefined);
  generations.clear();
}
export async function getSemanticWorker(input: Input): Promise<SemanticWorkerClient> {
  const group = `${input.rootDir}\0${input.model}`, key = `${group}\0${input.cache.dims}\0${input.generation}`;
  let entry = generations.get(key);
  if (entry && !(await entry.promise).isAlive()) { generations.delete(key); entry = undefined; }
  if (!entry) {
    const directory = path.join(input.rootDir, createHash('sha256').update(`${input.model}\0${input.cache.dims}`).digest('hex').slice(0, 24));
    const known = sidecarDirectories.get(group) ?? new Set<string>(); known.add(directory); sidecarDirectories.set(group, known);
    entry = { group, directory, generation: input.generation, promise: SemanticWorkerClient.create(input) };
    generations.set(key, entry);
    try { await entry.promise; }
    catch (error) { generations.delete(key); throw error; }
    const same = [...generations.entries()].filter(([, value]) => value.group === group);
    for (const [oldKey, old] of same.slice(0, -2)) {
      generations.delete(oldKey); void old.promise.then(client => client.retire(), () => undefined);
    }
    void cleanSidecars(group);
  }
  return entry.promise;
}
async function cleanSidecars(group: string): Promise<void> {
  for (const directory of sidecarDirectories.get(group) ?? []) try {
    for (const file of await fs.readdir(directory)) {
      const match = file.match(/^(generation-[\w-]+)\.(?:bin|json)$/);
      const keep = new Set([...generations.values()].filter(value => value.group === group && value.directory === directory).map(value => `generation-${value.generation}`));
      if (match && !keep.has(match[1])) await fs.unlink(path.join(directory, file));
    }
  } catch { /* 侧车可重建，清理失败下次代际更新重试。 */ }
}
