/**
 * 托管工具下载共用逻辑：流式写盘 + 节流进度回调（走网络代理）。
 * yt-dlp / ffmpeg 管理器共用。
 */
import { createHash } from "crypto";
import fs from "fs";
import { fetchWithNetworkProxy } from "../network-proxy";

const DOWNLOAD_TIMEOUT_MS = 5 * 60 * 1000;
const PROGRESS_EMIT_INTERVAL_MS = 200;

/**
 * 从 `SHA2-256SUMS` 风格的清单里取出某个文件的哈希。
 *
 * 行格式为 `<64 位十六进制>  <文件名>`，二进制模式下文件名前会多一个 `*`。
 */
export function parseSha256Sums(
  text: string,
  fileName: string,
): string | null {
  for (const rawLine of text.split(/\r?\n/)) {
    const match = /^([0-9a-f]{64})\s+\*?(.+)$/i.exec(rawLine.trim());
    if (match && match[2].trim() === fileName) {
      return match[1].toLowerCase();
    }
  }
  return null;
}

/** 流式算文件 SHA-256（安装包上百 MB，不能整体读进内存） */
export function sha256File(filePath: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const hash = createHash("sha256");
    const stream = fs.createReadStream(filePath);
    stream.on("data", (chunk) => hash.update(chunk));
    stream.on("error", reject);
    stream.on("end", () => resolve(hash.digest("hex")));
  });
}

/**
 * 按镜像顺序取校验清单，返回目标文件的期望哈希；全部取不到时返回 null。
 *
 * 取不到不等于放行——由调用方决定降级策略。这里只负责「能拿到就拿到」：
 * 二进制走的是第三方 GitHub 代理，只要校验和来自另一个源，
 * 代理被攻陷时替换出来的文件就对不上。
 */
export async function fetchExpectedSha256(
  checksumUrls: string[],
  fileName: string,
  timeoutMs = 30_000,
): Promise<string | null> {
  for (const url of checksumUrls) {
    try {
      const response = await fetchWithNetworkProxy(url, {
        signal: AbortSignal.timeout(timeoutMs),
      });
      if (!response.ok) {
        continue;
      }
      const expected = parseSha256Sums(await response.text(), fileName);
      if (expected) {
        return expected;
      }
    } catch {
      // 换下一个源
    }
  }
  return null;
}

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
