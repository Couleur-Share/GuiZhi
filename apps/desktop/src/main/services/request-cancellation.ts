const requests = new Map<string, AbortController>();
/** AI 网络代理和本地检索共享既有取消频道，仅按请求身份取消。 */
export function trackCancellableRequest(id: string | undefined, controller: AbortController): () => void {
  if (!id) return () => {};
  requests.set(id, controller);
  return () => { if (requests.get(id) === controller) requests.delete(id); };
}
export function cancelTrackedRequest(id: string): void { requests.get(id)?.abort(new Error('已取消')); }
