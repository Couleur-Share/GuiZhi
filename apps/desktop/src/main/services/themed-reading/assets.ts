import fs from "node:fs/promises";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { parseHTML } from "linkedom";
import type { ThemedReadingAsset, ThemedReadingVersion } from "@guizhi/shared/types";
import { isSafeAssetFileName } from "@guizhi/shared/utils/media-refs";
import { getImagesDir } from "../../runtime-paths";
import { retainAssetFiles } from "../asset-cleanup";
import { downloadToTempFile } from "../import/safe-fetch";
import { generateImage, type ImageGenModelConfig } from "../illustration/image-gen";

const IMAGE_LIMIT = 20 * 1024 * 1024;
/** 主题插画单次允许较长等待；仍禁止隐式重发，正文配图默认不变。 */
export const THEMED_IMAGE_TIMEOUT_MS = 480_000;
export function imageExtension(data: Buffer): string {
  if (data.length > IMAGE_LIMIT || data.length < 12) throw new Error("图片为空或超过 20 MiB 上限");
  if (data.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))) return "png";
  if (data[0] === 255 && data[1] === 216 && data[2] === 255) return "jpg";
  if (/^GIF8[79]a/.test(data.subarray(0, 6).toString())) return "gif";
  if (data.subarray(0, 4).toString() === "RIFF" && data.subarray(8, 12).toString() === "WEBP") return "webp";
  throw new Error("图片不是受支持的 PNG/JPEG/GIF/WebP 格式");
}

export function localThemeAssetPath(fileName: string): string {
  if (!isSafeAssetFileName(fileName) || !/\.(png|jpe?g|gif|webp)$/i.test(fileName)) throw new Error("主题图片路径不合法");
  return path.join(getImagesDir(), fileName);
}

/** 扫描程序渲染的原文；模型无权新增原始素材地址。 */
export function collectOriginalThemeAssets(version: ThemedReadingVersion): ThemedReadingAsset[] {
  const { document } = parseHTML(`<html><body>${version.source.blocks.map(b => b.html).join("")}</body></html>`);
  const seen = new Set<string>();
  return [...document.querySelectorAll("img")].flatMap((node, i) => {
    const url = node.getAttribute("src") ?? "";
    if (!url || seen.has(url)) return [];
    seen.add(url);
    const local = url.startsWith("local-image://") ? url.slice("local-image://".length) : undefined;
    if (local) localThemeAssetPath(local);
    return [{ id: `original-${i}`, role: "original" as const, purpose: "原文图片", prompt: "", alt: node.getAttribute("alt") ?? "原文图片", aspectRatio: "4:3" as const,
      originalUrl: url, fileName: local, status: "pending" as const }];
  });
}

export async function readThemeAsset(asset: ThemedReadingAsset): Promise<{ data: Buffer; mime: string }> {
  if (!asset.fileName) throw new Error(`图片尚未保存：${asset.alt}`);
  const file = localThemeAssetPath(asset.fileName);
  const stat = await fs.stat(file);
  if (stat.size > IMAGE_LIMIT) throw new Error("图片超过 20 MiB 上限");
  const data = await fs.readFile(file), ext = imageExtension(data);
  if (asset.sha256 && createHash("sha256").update(data).digest("hex") !== asset.sha256) throw new Error(`图片校验失败：${asset.alt}`);
  return { data, mime: `image/${ext === "jpg" ? "jpeg" : ext}` };
}

export async function publishThemeImage(data: Buffer, asset: ThemedReadingAsset, signal: AbortSignal, checkpoint: () => void, cleanup: (fileName: string) => void) {
  const ext = imageExtension(data), sha256 = createHash("sha256").update(data).digest("hex");
  const fileName = `theme-${sha256}.${ext}`;
  const release = retainAssetFiles([fileName]);
  const staged = path.join(getImagesDir(), `.theme-${randomUUID()}.tmp`);
  let committed = false;
  const previous = { ...asset };
  try {
    await fs.mkdir(getImagesDir(), { recursive: true });
    await fs.writeFile(staged, data, { flag: "wx" });
    signal.throwIfAborted();
    await fs.link(staged, localThemeAssetPath(fileName)).catch(async error => {
      if (error.code !== "EEXIST") throw error;
      const existing = await fs.readFile(localThemeAssetPath(fileName));
      if (existing.length !== data.length || createHash("sha256").update(existing).digest("hex") !== sha256) throw new Error("同名主题图片已损坏，未覆盖共享资源");
    });
    signal.throwIfAborted();
    Object.assign(asset, { fileName, sha256, bytes: data.length, status: "ready", error: undefined });
    checkpoint();
    committed = true;
  } finally {
    try { await fs.rm(staged, { force: true }); }
    finally {
      release();
      if (!committed) {
        for (const key of Object.keys(asset)) delete asset[key];
        Object.assign(asset, previous);
        cleanup(fileName);
      }
    }
  }
}

export async function prepareOriginalAsset(asset: ThemedReadingAsset, signal: AbortSignal, checkpoint: () => void, cleanup: (fileName: string) => void) {
  signal.throwIfAborted();
  if (asset.fileName) {
    const release = retainAssetFiles([asset.fileName]);
    try {
      const { data } = await readThemeAsset(asset);
      signal.throwIfAborted();
      Object.assign(asset, { sha256: createHash("sha256").update(data).digest("hex"), bytes: data.length, status: "ready", error: undefined });
      checkpoint();
      return;
    } catch (error) {
      signal.throwIfAborted();
      if (!/^https?:\/\//i.test(asset.originalUrl ?? "")) throw new Error(`原图无法读取，请重新导入来源图片：${error instanceof Error ? error.message : String(error)}`, { cause: error });
      // 只对已保存的原始网络地址补采，不猜测本地图片的替代来源。
    } finally { release(); }
  }
  if (!/^https?:\/\//i.test(asset.originalUrl ?? "")) throw new Error("原图地址不受支持，未发送请求");
  const temp = await downloadToTempFile(asset.originalUrl!, { signal, maxBytes: IMAGE_LIMIT, fileName: "image", accept: "image/*" });
  try { await publishThemeImage(await fs.readFile(temp.filePath), asset, signal, checkpoint, cleanup); }
  finally { await fs.rm(temp.dir, { recursive: true, force: true }); }
}

export async function generateThemeAsset(asset: ThemedReadingAsset, direction: string, config: ImageGenModelConfig, signal: AbortSignal, checkpoint: () => void, cleanup: (fileName: string) => void, onRequest?: () => void) {
  const prompt = `Theme illustration for an article. Visual direction: ${direction}\nPurpose: ${asset.purpose}\nComposition: ${asset.prompt}\nDo not render text, letters, numbers, pseudo-text, labels or watermarks. This is an illustration, not evidence. Respect the requested background and negative space.`;
  let result: Awaited<ReturnType<typeof generateImage>>;
  try {
    result = await generateImage(prompt, asset.aspectRatio, config, { signal, retryDelaysMs: [], scenario: "themedReading", timeoutMs: THEMED_IMAGE_TIMEOUT_MS, ...(onRequest ? { onRequest } : {}) });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (signal.aborted || (error instanceof Error && ["AbortError", "TimeoutError"].includes(error.name)) || /timeout|timed out|超时/i.test(message)) {
      throw new Error(`主题生图未取得完整结果（单次等待上限 ${THEMED_IMAGE_TIMEOUT_MS / 1000} 秒）。请求可能已提交，服务端结果未知；手动继续会再次调用。${message}`, { cause: error });
    }
    throw error;
  }
  signal.throwIfAborted();
  await publishThemeImage(result.data, asset, signal, checkpoint, cleanup);
}
