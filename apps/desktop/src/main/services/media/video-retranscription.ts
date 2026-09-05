import { SourceAccessDB } from "@guizhi/db";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { detectVideoPlatform } from "@guizhi/shared/utils/video-platforms";
import {
  getPlatformParseCode,
  PlatformParseError,
} from "@guizhi/shared/utils/platform-parse-error";
import type Database from "../../database/sqlite";
import { downloadVideoAudio, type VideoUrlDeps } from "../import/video-url";
import { isHttpUrl } from "../import/url-normalize";

/** 重转写优先使用已保存的分享入口；没有历史入口的旧条目仍尝试规范链接。 */
export async function downloadItemVideoAudio(
  db: Database.Database,
  item: Pick<KnowledgeItem, "id" | "sourceUri">,
  deps: VideoUrlDeps,
): Promise<{ dir: string; filePath: string }> {
  const canonicalUrl = item.sourceUri?.trim() ?? "";
  const platform = detectVideoPlatform(canonicalUrl);
  if (!platform) {
    throw new Error("该条目没有本地媒体文件，来源链接也不是可解析的视频平台");
  }
  const accessUrl = new SourceAccessDB(db).get(item.id, canonicalUrl);
  const url =
    accessUrl &&
    isHttpUrl(accessUrl) &&
    detectVideoPlatform(accessUrl) === platform
      ? accessUrl
      : canonicalUrl;
  try {
    return await downloadVideoAudio(url, platform, deps);
  } catch (error) {
    if (
      platform === "xiaohongshu" &&
      getPlatformParseCode(error) === "token_invalid"
    ) {
      throw new PlatformParseError(
        "token_invalid",
        "小红书访问链接已失效或缺少访问令牌。请在小红书中「分享 → 复制链接」，" +
          "将新链接重新导入以更新访问入口，再回到此条目重试；也请确认原笔记仍可打开。",
        { cause: error },
      );
    }
    throw error;
  }
}
