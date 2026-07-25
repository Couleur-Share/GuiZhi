import { extractLocalAssetRef } from "@guizhi/shared/utils/media-refs";
import type { KnowledgeItem } from "@guizhi/shared/types";

/**
 * 媒体条目预览：视频 / 音频的内联播放。
 * 资产引用从内容中的 local-video:// 链接解析。
 *
 * 图片不在这里预览——正文标签页里有专门的「图片」页，顶部再放一份是重复占版面。
 */
export function MediaPreview({ item }: { item: KnowledgeItem }) {
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
