// 管线宿主桥：goja 里的主题阅读生成管线（TS 原文件）通过它调用 Go 侧能力。
// 同步桥 __gzSync(kind, json)：钩子与用量记录；异步桥 __gzAsync(kind, json, id)：模型、搜索、网页抓取（返回 Promise<string>）。
// 取消：JS 侧 AbortSignal 触发时调用 __gzCancel(id)，Go 侧中断对应请求。
declare const __gzSync: (kind: string, args: string) => string;
declare const __gzAsync: (kind: string, args: string, id: number) => Promise<string>;
declare const __gzCancel: (id: number) => void;

type Callback = { progress?: (n: number) => void; delta?: (s: string) => void };
type Entry = { cb: Callback; failed: boolean; error?: unknown };
let seq = 0;
const entries = new Map<number, Entry>();

export const hostSync = (kind: string, payload: unknown): string => __gzSync(kind, JSON.stringify(payload ?? null));

export async function hostAsync(kind: string, payload: unknown, signal?: AbortSignal, cb?: Callback): Promise<string> {
  signal?.throwIfAborted();
  const id = ++seq;
  const entry: Entry = { cb: cb ?? {}, failed: false };
  entries.set(id, entry);
  const onAbort = () => __gzCancel(id);
  signal?.addEventListener("abort", onAbort);
  try {
    const out = await __gzAsync(kind, JSON.stringify(payload ?? null), id);
    // 回调抛出的异常（例如中止后的 throwIfAborted）沿调用链向上抛，与进程内直接调用 onDelta 的行为一致。
    if (entry.failed) throw entry.error;
    return out;
  } catch (e) {
    if (entry.failed) throw entry.error;
    throw e;
  } finally {
    signal?.removeEventListener("abort", onAbort);
    entries.delete(id);
  }
}

/** Go 侧推送流式进度 / 增量（在 JS 线程内执行）；回调一旦抛出，后续推送丢弃并中断该请求。 */
export function hostCallback(id: number, kind: "progress" | "delta", value: unknown) {
  const entry = entries.get(id);
  if (!entry || entry.failed) return;
  try {
    if (kind === "progress") entry.cb.progress?.(value as number);
    else entry.cb.delta?.(value as string);
  } catch (e) {
    entry.failed = true;
    entry.error = e;
    __gzCancel(id);
  }
}
