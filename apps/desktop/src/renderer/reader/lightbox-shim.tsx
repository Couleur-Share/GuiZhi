import { useEffect, useRef } from "react";

/**
 * 阅读岛里替换 components/library/ImageLightbox 的垫片（jsgen/build-reader.mts 的别名）。
 *
 * 真正的查看器要盖住整个窗口（渲染层里它是全窗口浮层），而岛只占阅读区那一块；所以岛里不画查看器：
 * 挂载即把图片列表与起始序号交给宿主（宿主另开一个盖满窗口的查看器岛，见 reader/lightbox 入口），
 * 并立刻调用 onClose 把调用方的 viewerIndex 复位，页面里不留任何浮层。
 */
export interface LightboxImage {
  src: string;
  alt?: string;
}

type Opener = (images: LightboxImage[], index: number) => void;

let opener: Opener | null = null;

/** 阅读页登记「怎么把图片交给宿主」；卸载时传 null。 */
export function setLightboxOpener(next: Opener | null): void {
  opener = next;
}

export function ImageLightbox({
  images,
  startIndex = 0,
  onClose,
}: {
  images: LightboxImage[];
  startIndex?: number;
  onClose: () => void;
}) {
  const latest = useRef({ images, startIndex, onClose });
  latest.current = { images, startIndex, onClose };
  useEffect(() => {
    const { images: list, startIndex: index, onClose: close } = latest.current;
    opener?.(list, index);
    close();
  }, []);
  return null;
}
