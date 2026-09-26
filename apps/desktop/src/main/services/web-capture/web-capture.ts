import { isWechatUrl, captureWechat } from "./wechat";
import type {
  ImportStage,
  WebCaptureRequest,
  WebCaptureResult,
} from "@guizhi/shared/types";
import { canonicalWebUrl } from "@guizhi/shared/utils/web-scope";
import { ElectronWebCapture } from "./web-electron-capture";
import { webRuntimeStatus } from "./web-runtime";
import { webCaptureError } from "./web-error";
import { logAppError } from "../../diagnostic-log";

const worker = new ElectronWebCapture();
let captureLifetime = new AbortController();
let shutdown: Promise<void> | undefined;
const origins = new Set<string>();
const waiting = new Set<() => void>();
let active = 0;
export const getWebCaptureStatus = () => webRuntimeStatus(worker.running);
export function shutdownWebCapture(): Promise<void> {
  if (shutdown !== undefined) return shutdown;
  captureLifetime.abort();
  shutdown = worker.close().finally(() => {
    captureLifetime = new AbortController();
    shutdown = undefined;
  });
  return shutdown;
}

export async function captureWebPage(
  request: WebCaptureRequest,
  signal?: AbortSignal,
  stage?: (stage: ImportStage) => void,
): Promise<WebCaptureResult> {
  if (isWechatUrl(request.url)) return captureWechat(request.url,request.taskId,signal);
  const lifetime = captureLifetime.signal;
  const status = await getWebCaptureStatus();
  if (!status.available) throw new Error(status.reason ?? "网页组件不可用");
  const url = canonicalWebUrl(request.url),
    origin = new URL(url).origin;
  const timeout = AbortSignal.timeout(60_000);
  const combined = AbortSignal.any([timeout, lifetime, ...(signal ? [signal] : [])]);
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
