import { useState } from "react";
import { useTranslation } from "react-i18next";
import { extractLocalAssetRefs } from "@guizhi/shared/utils/media-refs";
import { ImageLightbox } from "./ImageLightbox";

/**
 * 图片条目的配图画廊：点开进查看器。
 * 图片引用从正文的 local-image:// 链接解析，与正文共用同一份数据。
 */
export function ImageGallery({ content }: { content: string }) {
  const { t } = useTranslation();
  const [viewerIndex, setViewerIndex] = useState<number | null>(null);

  const images = extractLocalAssetRefs(content, "local-image").map(
    (ref, position) => ({
      src: `local-image://${ref}`,
      alt: t("library.imageIndex", "图 {{index}}", { index: position + 1 }),
    }),
  );

  if (images.length === 0) {
    return (
      <div className="flex h-full items-center justify-center px-6 text-center">
        <p className="text-sm text-muted-foreground">
          {t("library.imagesEmpty", "这条内容没有配图")}
        </p>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto px-6 py-4">
      <div className="flex flex-wrap gap-3">
        {images.map((image, position) => (
          <button
            key={image.src}
            type="button"
            onClick={() => setViewerIndex(position)}
            title={t("library.imageZoomOpen", "点击查看大图")}
            className="overflow-hidden rounded-xl border border-border/60 transition-colors hover:border-primary/60"
          >
            <img
              src={image.src}
              alt={image.alt}
              loading="lazy"
              className="max-h-80 w-auto max-w-full object-contain"
            />
          </button>
        ))}
      </div>
      {viewerIndex !== null ? (
        <ImageLightbox
          images={images}
          startIndex={viewerIndex}
          onClose={() => setViewerIndex(null)}
        />
      ) : null}
    </div>
  );
}
