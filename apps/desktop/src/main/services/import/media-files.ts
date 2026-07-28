/**
 * 本地媒体文件资产化：按类型拷贝进 assets 目录，
 * 供 local-image:// 和 local-video:// 协议服务（音频复用 videos 目录与协议）。
 * 资产文件名只用十六进制串 + 扩展名，避免自定义协议的 URL 编码问题；
 * 原始文件名保留在条目内容与来源记录中。
 */
import { randomUUID } from "crypto";
import { constants as fsConstants } from "fs";
import fs from "fs/promises";
import path from "path";
import { getImagesDir, getVideosDir } from "../../runtime-paths";
import { computeFileHash } from "./content-hash";

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
 *
 * 给了 contentHash 就用它的前 16 位，同一份内容永远得到同一个名字（媒体
 * 导入去重）；不给则随机（AI 配图每张都是新的，去重无从谈起）。
 */
export function buildAssetFileName(
  extension: string,
  prefix = "import-",
  contentHash?: string,
): string {
  const stem = contentHash
    ? contentHash.slice(0, 16)
    : randomUUID().replace(/-/g, "").slice(0, 16);
  return `${prefix}${stem}${extension.toLowerCase()}`;
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

/**
 * 拷贝媒体文件进资产目录，返回资产文件名。
 *
 * 文件名取内容哈希而不是随机 UUID，于是同一份文件重复导入落在同一个资产上，
 * 磁盘只存一份——此前导入两次同一个 1GB 视频，磁盘上就是实打实的两个 1GB。
 * 名字的形状（`import-` + 16 位小写十六进制 + 小写扩展名）与随机版一致，
 * 协议解析、清理白名单、`gen-` 前缀的区分都不受影响。
 *
 * 共享资产不会被误删：清理走的是「全库正文里还有没有人引用这个文件名」
 * （`listReferencedAssets`），两个条目指向同一份，删掉其一另一份仍在引用。
 */
export async function saveMediaAsset(
  sourcePath: string,
  kind: MediaFileKind,
): Promise<string> {
  const targetDir = getMediaAssetDir(kind);
  await fs.mkdir(targetDir, { recursive: true });
  const assetFileName = buildAssetFileName(
    path.extname(sourcePath),
    "import-",
    await computeFileHash(sourcePath),
  );
  try {
    // COPYFILE_EXCL：已存在就不覆盖。用它而不是先 access 再 copy，
    // 是因为两个导入任务并发落同一份文件时，先查后写会撞在一起
    await fs.copyFile(
      sourcePath,
      path.join(targetDir, assetFileName),
      fsConstants.COPYFILE_EXCL,
    );
  } catch (error) {
    if ((error as NodeJS.ErrnoException)?.code !== "EEXIST") {
      throw error;
    }
    // 命中已有资产：内容一致，不用再拷一遍
  }
  return assetFileName;
}
