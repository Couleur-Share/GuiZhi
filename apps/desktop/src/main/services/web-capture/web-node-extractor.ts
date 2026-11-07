/**
 * 网页正文提取的主进程入口：解析放在独立 worker 线程，主进程只做排队、超时与大小限制。
 * 网页采集（web-electron-capture.ts）与兼容采集（import/connectors.ts）共用同一个线程。
 */
import { Worker } from "node:worker_threads";
import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
import type { WebExtracted } from "./web-extract-article";
import { WebTaskGate, withWebAbort, webAbortError } from "./web-task-gate";

export type { WebExtracted } from "./web-extract-article";

/** 正文提取引擎标识，写入采集结果与原文版本（界面版本列表可见）；提取规则有不兼容变化时递增。 */
export const WEB_EXTRACT_ENGINE = "readability/1";

const FRAME_LIMIT = 16 * 1024 * 1024;
const EXTRACT_TIMEOUT_MS = 45_000;
const IDLE_MS = 60_000;
// 超过上限时只结束本线程（ERR_WORKER_OUT_OF_MEMORY），不拖垮主进程。
const HEAP_LIMIT_MB = 1024;

/** 提取线程按协议回报的失败：线程仍与主进程同步，无需回收。 */
class WebExtractionFailure extends Error {}

// 异常类名（如 TypeError）。只放行标识符，避免把页面内容带进错误信息。
const EXCEPTION_NAME = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;

/**
 * 打包后的脚本在 app.asar 内，worker_threads 不能直接按路径加载 asar 里的文件；
 * 由主进程读出（asar 透明读取）后以内联脚本启动。脚本自带全部依赖，只引用 Node 内置模块。
 */
async function bundledWorker(): Promise<Worker> {
  const source = await fs.readFile(
    path.join(__dirname, "../web-extract-worker/web-extract-worker.js"),
    "utf8",
  );
  return new Worker(source, {
    eval: true,
    stdout: true,
    stderr: true,
    resourceLimits: { maxOldGenerationSizeMb: HEAP_LIMIT_MB },
  });
}

export class WebNodeExtractor {
  private worker?: Worker;
  private gate = new WebTaskGate(1);
  private lifetime = new AbortController();
  private operations = new Set<Promise<unknown>>();
  private idle?: ReturnType<typeof setTimeout>;
  private closing?: Promise<void>;

  constructor(private readonly createWorker: () => Promise<Worker> = bundledWorker) {}

  get running(): boolean {
    return !!this.worker;
  }

  async extract(html: string, url: string, status: number, signal: AbortSignal): Promise<WebExtracted> {
    while (this.closing !== undefined) await withWebAbort(this.closing, signal);
    clearTimeout(this.idle);
    const combined = AbortSignal.any([signal, this.lifetime.signal]);
    const work = this.gate.run(combined, async () => {
      try {
        if (Buffer.byteLength(html) > FRAME_LIMIT) throw new WebExtractionFailure("正文提取输入超过 16 MiB");
        const id = randomUUID();
        const worker = await this.start(combined);
        const message = await this.exchange(worker, { v: 1, id, html, url, status }, combined);
        // 版本或编号对不上说明协议已失步，必须回收线程，否则下一任务会读到旧响应。
        if (message.v !== 1 || message.id !== id) throw new Error("正文提取协议无效");
        if (message.error !== undefined) {
          // 提取线程捕获了页面处理中的异常：线程仍可用，不回收；只带出异常类型（消息可能含页面内容）。
          if (typeof message.error !== "string") throw new Error("正文提取协议无效");
          throw new WebExtractionFailure(
            `正文提取失败（${EXCEPTION_NAME.test(message.error) ? message.error : "未知异常"}）`,
          );
        }
        const result = message.result as WebExtracted | undefined;
        if (
          !result ||
          typeof result.markdown !== "string" ||
          typeof result.complete !== "boolean" ||
          !Array.isArray(result.paragraphs)
        )
          throw new Error("正文提取结果无效");
        if (Buffer.byteLength(JSON.stringify(result)) > FRAME_LIMIT)
          throw new WebExtractionFailure("正文提取输出超过 16 MiB");
        return result;
      } catch (error) {
        if (!(error instanceof WebExtractionFailure)) await this.stopWorker();
        throw error;
      }
    });
    this.operations.add(work);
    try {
      return await work;
    } finally {
      this.operations.delete(work);
      if (!this.operations.size && this.closing === undefined) {
        this.idle = setTimeout(() => void this.stopWorker(), IDLE_MS);
        this.idle.unref();
      }
    }
  }

  private async start(signal: AbortSignal): Promise<Worker> {
    if (this.worker) return this.worker;
    const pending = this.createWorker();
    let worker: Worker;
    try {
      worker = await withWebAbort(pending, signal);
    } catch (error) {
      // 取消发生在线程创建期间：创建完成后立即结束，不留下迟到的线程。
      void pending.then((late) => late.terminate(), () => undefined);
      throw error;
    }
    // 第三方库的控制台输出可能含网址或页面内容：读掉丢弃，不进入主进程输出与日志。
    worker.stdout?.resume();
    worker.stderr?.resume();
    // 空闲期的线程错误（如内存超限）只回收线程，不能成为未处理的异常。
    worker.on("error", () => undefined);
    worker.once("exit", () => {
      if (this.worker === worker) this.worker = undefined;
    });
    worker.unref();
    this.worker = worker;
    return worker;
  }

  private exchange(
    worker: Worker,
    request: Record<string, unknown>,
    signal: AbortSignal,
  ): Promise<Record<string, unknown>> {
    return new Promise((resolve, reject) => {
      let done = false;
      const finish = (error?: Error, message?: Record<string, unknown>) => {
        if (done) return;
        done = true;
        clearTimeout(timer);
        signal.removeEventListener("abort", abort);
        worker.off("message", receive);
        worker.off("error", fail);
        worker.off("exit", exit);
        worker.unref();
        if (error) reject(error);
        else resolve(message!);
      };
      const abort = () => finish(webAbortError(signal));
      const receive = (message: unknown) =>
        message && typeof message === "object"
          ? finish(undefined, message as Record<string, unknown>)
          : finish(new Error("正文提取协议无效"));
      const fail = (error: Error & { code?: string }) =>
        finish(
          new Error(
            error.code === "ERR_WORKER_OUT_OF_MEMORY" ? "正文提取超过内存限制" : "正文提取线程异常退出",
          ),
        );
      const exit = () => finish(new Error("正文提取线程提前退出"));
      const timer = setTimeout(() => finish(new Error("正文提取超时")), EXTRACT_TIMEOUT_MS);
      worker.on("message", receive);
      worker.on("error", fail);
      worker.once("exit", exit);
      signal.addEventListener("abort", abort, { once: true });
      if (signal.aborted) return abort();
      worker.ref();
      worker.postMessage(request);
    });
  }

  private async stopWorker(): Promise<void> {
    clearTimeout(this.idle);
    const worker = this.worker;
    this.worker = undefined;
    await worker?.terminate().catch(() => undefined);
  }

  close(): Promise<void> {
    if (this.closing !== undefined) return this.closing;
    clearTimeout(this.idle);
    this.lifetime.abort();
    this.closing = (async () => {
      await Promise.allSettled([...this.operations]);
      await this.stopWorker();
    })().finally(() => {
      this.lifetime = new AbortController();
      this.closing = undefined;
    });
    return this.closing;
  }
}

let shared: WebNodeExtractor | undefined;
/** 主进程内共用的提取器：同一时间只有一个提取线程。 */
export function webNodeExtractor(): WebNodeExtractor {
  return (shared ??= new WebNodeExtractor());
}
