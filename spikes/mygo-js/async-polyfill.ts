// goja 缺少 AbortController / structuredClone：管线只用到 signal.aborted / reason / throwIfAborted / addEventListener。
class GzAbortSignal {
  aborted = false;
  reason: unknown = undefined;
  private listeners: ((e: { type: string }) => void)[] = [];
  throwIfAborted() { if (this.aborted) throw this.reason; }
  addEventListener(_t: string, fn: (e: { type: string }) => void, opts?: { once?: boolean }) {
    if (opts?.once) { const f = fn; fn = e => { this.removeEventListener("abort", fn); f(e); }; }
    this.listeners.push(fn);
  }
  removeEventListener(_t: string, fn: (e: { type: string }) => void) { this.listeners = this.listeners.filter(x => x !== fn); }
  _fire() { for (const fn of [...this.listeners]) fn({ type: "abort" }); }
}
class GzAbortController {
  signal = new GzAbortSignal();
  abort(reason?: unknown) {
    const s = this.signal;
    if (s.aborted) return;
    s.aborted = true;
    s.reason = reason === undefined ? Object.assign(new Error("This operation was aborted"), { name: "AbortError" }) : reason;
    s._fire();
  }
}
const g = globalThis as Record<string, unknown>;
g.AbortController = GzAbortController;
g.AbortSignal = GzAbortSignal;
// structuredClone：数据均为 JSON 形态（对象 / 数组 / 原始值），保留值为 undefined 的属性。
g.structuredClone = function clone<T>(v: T): T {
  if (Array.isArray(v)) return v.map(clone) as unknown as T;
  if (v && typeof v === "object") {
    const out: Record<string, unknown> = {};
    for (const k of Object.keys(v)) out[k] = clone((v as Record<string, unknown>)[k]);
    return out as T;
  }
  return v;
};
