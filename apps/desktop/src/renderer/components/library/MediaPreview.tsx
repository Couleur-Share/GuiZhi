import { extractLocalAssetRef } from "@guizhi/shared/utils/media-refs";
import type { KnowledgeItem } from "@guizhi/shared/types";

/**
 * 媒体条目预览：图片 / 视频 / 音频的内联播放。
 * 资产引用从内容中的 local-image:// / local-video:// 链接解析。
 */
export function MediaPreview({ item }: { item: KnowledgeItem }) {
  if (item.itemType === "image") {
    const ref = extractLocalAssetRef(item.content, "local-image");
    if (!ref) {
      return null;
    }
    return (
      <img
        src={`local-image://${ref}`}
        alt={item.title}
        className="max-h-72 max-w-full rounded-xl border border-border/60 object-contain"
      />
    );
  }

  if (item.itemType === "video") {
    const ref = extractLocalAssetRef(item.content, "local-video");
    if (!ref) {
      return null;
    }
    return (
      <video
        controls
        preload="metadata"
        src={`local-video://${ref}`}
        className="max-h-72 w-full rounded-xl border border-border/60 bg-black/80"
      />
    );
  }

  if (item.itemType === "audio") {
    const ref = extractLocalAssetRef(item.content, "local-video");
    if (!ref) {
      return null;
    }
    // 音频资产存放在 videos 目录，复用 local-video 协议
    return <audio controls preload="metadata" src={`local-video://${ref}`} className="w-full" />;
  }

  return null;
}
