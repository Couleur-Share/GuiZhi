/**
 * 托管工具下载共用逻辑：流式写盘 + 节流进度回调（走网络代理）。
 * yt-dlp / ffmpeg 管理器共用。
 */
import fs from "fs";
import { fetchWithNetworkProxy } from "../network-proxy";

const DOWNLOAD_TIMEOUT_MS = 5 * 60 * 1000;
const PROGRESS_EMIT_INTERVAL_MS = 200;

export interface ToolDownloadProgress {
  /** 已下载字节数 */
  transferred: number;
  /** 总字节数（镜像可能不返回 content-length，此时为 null） */
  total: number | null;
}

export async function downloadToFile(
  url: string,
  targetPath: string,
  onProgress?: (progress: ToolDownloadProgress) => void,
): Promise<void> {
  const response = await fetchWithNetworkProxy(url, {
    signal: AbortSignal.timeout(DOWNLOAD_TIMEOUT_MS),
  });
  if (!response.ok || !response.body) {
    throw new Error(`HTTP ${response.status}`);
  }

  const contentLength = Number(response.headers.get("content-length"));
  const total =
    Number.isFinite(contentLength) && contentLength > 0 ? contentLength : null;

  const reader = response.body.getReader();
  const stream = fs.createWriteStream(targetPath);
  let transferred = 0;
  let lastEmit = 0;

  const writeChunk = (chunk: Uint8Array) =>
    new Promise<void>((resolve, reject) => {
      stream.write(chunk, (error) => (error ? reject(error) : resolve()));
    });

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      await writeChunk(value);
      transferred += value.byteLength;
      const now = Date.now();
      if (now - lastEmit >= PROGRESS_EMIT_INTERVAL_MS) {
        lastEmit = now;
        onProgress?.({ transferred, total });
      }
    }
    onProgress?.({ transferred, total });
  } finally {
    await new Promise<void>((resolve) => stream.end(() => resolve()));
  }
}
