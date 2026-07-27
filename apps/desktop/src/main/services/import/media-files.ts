/**
 * 本地媒体文件资产化：按类型拷贝进 assets 目录，
 * 供 local-image:// 和 local-video:// 协议服务（音频复用 videos 目录与协议）。
 * 资产文件名只用 UUID + 扩展名，避免自定义协议的 URL 编码问题；
 * 原始文件名保留在条目内容与来源记录中。
 */
import { randomUUID } from "crypto";
import fs from "fs/promises";
import path from "path";
import { getImagesDir, getVideosDir } from "../../runtime-paths";

export type MediaFileKind = "image" | "audio" | "video";

export const IMAGE_EXTENSIONS = [
  ".png",
  ".jpg",
  ".jpeg",
  ".webp",
  ".gif",
  ".bmp",
] as const;

export const AUDIO_EXTENSIONS = [
  ".mp3",
  ".wav",
  ".m4a",
  ".aac",
  ".ogg",
  ".flac",
] as const;

export const VIDEO_EXTENSIONS = [
  ".mp4",
  ".webm",
  ".mkv",
  ".mov",
  ".avi",
] as const;

const MB = 1024 * 1024;

export const MEDIA_SIZE_LIMITS: Record<MediaFileKind, number> = {
  image: 20 * MB,
  audio: 300 * MB,
  video: 1024 * MB,
};

export function classifyMediaFile(extension: string): MediaFileKind | null {
  const normalized = extension.toLowerCase();
  if ((IMAGE_EXTENSIONS as readonly string[]).includes(normalized)) {
    return "image";
  }
  if ((AUDIO_EXTENSIONS as readonly string[]).includes(normalized)) {
    return "audio";
  }
  if ((VIDEO_EXTENSIONS as readonly string[]).includes(normalized)) {
    return "video";
  }
  return null;
}

/**
 * 资产文件名。
 *
 * prefix 用来区分资产来源：采集导入是 `import-`，AI 生成的正文配图是 `gen-`
 * （见 shared/utils/illustration-note.ts 的 ILLUSTRATION_ASSET_PREFIX）——
 * 详情页据此把配图和原文里的图分开管理。
 */
export function buildAssetFileName(
  extension: string,
  prefix = "import-",
): string {
  return `${prefix}${randomUUID().replace(/-/g, "").slice(0, 16)}${extension.toLowerCase()}`;
}

/** 媒体资产的自定义协议 URL（音频走 local-video 协议：同为文件流服务） */
export function mediaProtocolUrl(
  kind: MediaFileKind,
  assetFileName: string,
): string {
  return kind === "image"
    ? `local-image://${assetFileName}`
    : `local-video://${assetFileName}`;
}

export function getMediaAssetDir(kind: MediaFileKind): string {
  return kind === "image" ? getImagesDir() : getVideosDir();
}

/** 拷贝媒体文件进资产目录，返回资产文件名 */
export async function saveMediaAsset(
  sourcePath: string,
  kind: MediaFileKind,
): Promise<string> {
  const targetDir = getMediaAssetDir(kind);
  await fs.mkdir(targetDir, { recursive: true });
  const assetFileName = buildAssetFileName(path.extname(sourcePath));
  await fs.copyFile(sourcePath, path.join(targetDir, assetFileName));
  return assetFileName;
}
