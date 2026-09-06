import type {
  ImportStage,
  WebCaptureRequest,
  WebCaptureResult,
} from "@guizhi/shared/types";
import { canonicalWebUrl } from "@guizhi/shared/utils/web-scope";
import { WebWorker } from "./web-worker";
import { webRuntimeStatus } from "./web-runtime";
import { webCaptureError } from "./web-error";
import { logAppError } from "../../diagnostic-log";

const worker = new WebWorker();
const origins = new Set<string>();
const waiting = new Set<() => void>();
let active = 0;
export const getWebCaptureStatus = () => webRuntimeStatus(worker.running);
export const shutdownWebCapture = () => worker.close();

export async function captureWebPage(
  request: WebCaptureRequest,
  signal?: AbortSignal,
  stage?: (stage: ImportStage) => void,
): Promise<WebCaptureResult> {
  const status = await getWebCaptureStatus();
  if (!status.available) throw new Error(status.reason ?? "网页组件不可用");
  const url = canonicalWebUrl(request.url),
    origin = new URL(url).origin;
  const timeout = AbortSignal.timeout(60_000);
  const combined = signal ? AbortSignal.any([signal, timeout]) : timeout;
  while (active >= 2 || origins.has(origin)) {
    await new Promise<void>((resolve, reject) => {
      combined.throwIfAborted();
      const wake = () => {
        waiting.delete(wake);
        combined.removeEventListener("abort", abort);
        resolve();
      };
      const abort = () => {
        waiting.delete(wake);
        reject(new Error("网页采集等待已取消或超时"));
      };
      waiting.add(wake);
      combined.addEventListener("abort", abort, { once: true });
    });
  }
  active++;
  origins.add(origin);
  try {
    return await worker.capture({ ...request, url }, combined, stage);
  } catch (error) {
    const failure = webCaptureError(error);
    logAppError({
      scope: "main",
      action: "网页采集",
      message: failure.message,
    });
    return {
      taskId: request.taskId,
      entryUrl: url,
      finalUrl: url,
      title: url,
      author: "",
      publishedAt: null,
      dateConfidence: "unknown",
      markdown: "",
      links: [],
      paragraphs: [],
      contentHash: "",
      capturedAt: Date.now(),
      engineVersion: "crawl4ai/0.9.3",
      complete: false,
      truncated: false,
      warnings: [],
      error: failure,
    };
  } finally {
    active--;
    origins.delete(origin);
    for (const wake of [...waiting]) wake();
  }
}
