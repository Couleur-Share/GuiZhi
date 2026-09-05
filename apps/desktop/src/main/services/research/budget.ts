/** A deadline must settle even when a backend ignores AbortSignal. */
export async function withinResearchBudget<T>(parent: AbortSignal, milliseconds: number, action: (signal: AbortSignal) => Promise<T>): Promise<T> {
  parent.throwIfAborted();
  const controller = new AbortController();
  const abort = () => controller.abort(parent.reason);
  parent.addEventListener("abort", abort, { once: true });
  const timer = setTimeout(() => controller.abort(new Error("研究操作超时")), milliseconds);
  let listener: () => void;
  try {
    return await Promise.race([
      action(controller.signal),
      new Promise<never>((_resolve, reject) => {
        listener = () => reject(controller.signal.reason ?? new Error("已取消"));
        controller.signal.addEventListener("abort", listener, { once: true });
        if (controller.signal.aborted) listener();
      }),
    ]);
  } finally {
    clearTimeout(timer);
    parent.removeEventListener("abort", abort);
    controller.signal.removeEventListener("abort", listener!);
  }
}
