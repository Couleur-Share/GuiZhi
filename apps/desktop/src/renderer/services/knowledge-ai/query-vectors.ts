import type { AIConfig } from '../ai';
import { embedTexts } from './embeddings';
type Entry = { promise: Promise<number[]>; controller: AbortController; users: number };
const pending = new Map<string, Entry>(), cache = new Map<string, { value: number[]; expires: number }>();
const MAX_CACHE = 100, CACHE_MS = 5 * 60_000;
const aborted = () => new DOMException('已取消', 'AbortError');
export function resetQueryVectors() { for (const entry of pending.values()) entry.controller.abort(); pending.clear(); cache.clear(); }
export async function queryVector(config: AIConfig, query: string, signal?: AbortSignal): Promise<number[]> {
  signal?.throwIfAborted();
  // 配置只参与单向指纹，不把凭证或查询明文写进缓存键、日志或磁盘。
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify([config.id, config.provider, config.apiProtocol, config.apiUrl, config.model, config.apiKey, query])));
  const key = Array.from(new Uint8Array(bytes), b => b.toString(16).padStart(2, '0')).join('');
  signal?.throwIfAborted();
  const hit = cache.get(key);
  if (hit && hit.expires > Date.now()) { cache.delete(key); cache.set(key, hit); return [...hit.value]; }
  cache.delete(key);
  let entry = pending.get(key);
  if (!entry) {
    const controller = new AbortController();
    entry = { controller, users: 0, promise: Promise.resolve([]) };
    const own = entry;
    own.promise = embedTexts(config, [query], controller.signal).then(values => {
      const value = values[0];
      if (!value) throw new Error('查询向量为空');
      if (!controller.signal.aborted) {
        cache.set(key, { value, expires: Date.now() + CACHE_MS });
        while (cache.size > MAX_CACHE) cache.delete(cache.keys().next().value!);
      }
      return value;
    }).finally(() => { if (pending.get(key) === own) pending.delete(key); });
    pending.set(key, own);
  }
  entry.users++;
  const own = entry;
  return new Promise<number[]>((resolve, reject) => {
    let settled = false;
    const finish = (error?: unknown, value?: number[]) => {
      if (settled) return; settled = true; signal?.removeEventListener('abort', cancel); own.users--;
      if (!own.users && pending.get(key) === own) { pending.delete(key); own.controller.abort(); }
      if (error) reject(error); else resolve([...(value ?? [])]);
    };
    const cancel = () => finish(aborted());
    signal?.addEventListener('abort', cancel, { once: true });
    if (signal?.aborted) cancel();
    own.promise.then(value => finish(undefined, value), error => finish(error));
  });
}
