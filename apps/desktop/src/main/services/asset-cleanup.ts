/**
 * 媒体资产清理。
 *
 * 导入图片/音视频时文件会被拷进资产目录，但此前删除条目只删数据库行——
 * 导入一个 1GB 的视频、彻底删除条目、清空回收站，那 1GB 会永久留在
 * `%APPDATA%/GuiZhi` 里，用户无从得知。
 */
import fs from "fs";
import path from "path";
import type { KnowledgeItemDB } from "@guizhi/db";
import { isSafeAssetFileName } from "@guizhi/shared/utils/media-refs";
import { getImagesDir, getVideosDir } from "../runtime-paths";

/** 资产文件名 → 磁盘路径；越界或非法命名返回 null */
export function resolveAssetPath(
  fileName: string,
  directories: string[],
): string | null {
  if (!isSafeAssetFileName(fileName)) {
    return null;
  }
  for (const directory of directories) {
    const candidate = path.join(directory, fileName);
    // 双保险：文件名已过白名单，这里再确认没跳出目录
    const relative = path.relative(directory, candidate);
    if (relative.startsWith("..") || path.isAbsolute(relative)) {
      continue;
    }
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return null;
}

/**
 * 删除不再被任何条目引用的资产文件，返回实际删除的数量。
 *
 * 调用方需在**删除条目之前**收集 `candidates`（那时正文还在），
 * 删除之后再调用本函数——此时引用检查才能反映真实状态。
 */
export function cleanupOrphanAssets(
  items: KnowledgeItemDB,
  candidates: string[],
): number {
  if (candidates.length === 0) {
    return 0;
  }
  const directories = [getImagesDir(), getVideosDir()];
  let removed = 0;

  for (const fileName of candidates) {
    // 同一份资产可能被复制粘贴到别的条目，仍被引用就不能删
    if (items.isAssetReferenced(fileName)) {
      continue;
    }
    const filePath = resolveAssetPath(fileName, directories);
    if (!filePath) {
      continue;
    }
    try {
      fs.rmSync(filePath, { force: true });
      removed++;
    } catch (error) {
      console.warn(`[assets] 清理资产失败 ${fileName}:`, error);
    }
  }

  if (removed > 0) {
    console.log(`[assets] 已清理 ${removed} 个不再被引用的资产文件`);
  }
  return removed;
}
