/** 取消仅结束当前等待；底层资源由创建请求的一方负责中止。 */
export function abortable<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  return new Promise((resolve, reject) => {
    const cancel = () => { cleanup(); reject(signal.reason ?? new DOMException('已取消', 'AbortError')); };
    const cleanup = () => signal.removeEventListener('abort', cancel);
    // 始终观察原 Promise，避免取消后的延迟失败成为未处理拒绝。
    promise.then(value => { cleanup(); resolve(value); }, error => { cleanup(); reject(error); });
    if (signal.aborted) cancel(); else signal.addEventListener('abort', cancel, { once: true });
  });
}
