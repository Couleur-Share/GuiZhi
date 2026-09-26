/** 采集内部的可取消排队；取消等待者不能占住槽位，也不能中断其他任务。 */
export function webAbortError(signal: AbortSignal): Error {
  return new Error(
    signal.reason?.name === "TimeoutError" ? "网页采集超时" : "网页采集已取消",
    {
      cause: {
        webCaptureCode:
          signal.reason?.name === "TimeoutError" ? "timeout" : "canceled",
      },
    },
  );
}

export function withWebAbort<T>(
  work: Promise<T>,
  signal: AbortSignal,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const abort = () => {
      signal.removeEventListener("abort", abort);
      reject(webAbortError(signal));
    };
    signal.addEventListener("abort", abort, { once: true });
    work
      .then(resolve, reject)
      .finally(() => signal.removeEventListener("abort", abort));
    if (signal.aborted) abort();
  });
}

export async function webPause(ms: number, signal: AbortSignal): Promise<void> {
  let timer: ReturnType<typeof setTimeout>;
  try {
    await withWebAbort(
      new Promise<void>((resolve) => {
        timer = setTimeout(resolve, ms);
      }),
      signal,
    );
  } finally {
    clearTimeout(timer!);
  }
}

export class WebTaskGate {
  private active = 0;
  private waiting: Array<() => void> = [];
  constructor(private readonly limit: number) {}
  async run<T>(signal: AbortSignal, action: () => Promise<T>): Promise<T> {
    await new Promise<void>((resolve, reject) => {
      const enter = () => {
        signal.removeEventListener("abort", abort);
        this.active++;
        resolve();
      };
      const abort = () => {
        this.waiting = this.waiting.filter((item) => item !== enter);
        signal.removeEventListener("abort", abort);
        reject(webAbortError(signal));
      };
      if (signal.aborted) return abort();
      if (this.active < this.limit) enter();
      else {
        this.waiting.push(enter);
        signal.addEventListener("abort", abort, { once: true });
      }
    });
    try {
      if (signal.aborted) throw webAbortError(signal);
      return await action();
    } finally {
      this.active--;
      this.waiting.shift()?.();
    }
  }
}
