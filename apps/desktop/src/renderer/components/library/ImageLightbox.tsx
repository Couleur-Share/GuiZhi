import Lightbox from "yet-another-react-lightbox";
import Counter from "yet-another-react-lightbox/plugins/counter";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import "yet-another-react-lightbox/styles.css";
import "yet-another-react-lightbox/plugins/counter.css";

export interface LightboxImage {
  src: string;
  alt?: string;
}

/**
 * 图片查看器：滚轮缩放、拖拽平移、双击放大、键盘 +/-/方向键，多图左右切换。
 *
 * 交互全部交给 yet-another-react-lightbox 的 Zoom 插件——手写这类手势状态机
 * 很容易在缩放锚点和位移上出错，没有自研的价值。
 */
export function ImageLightbox({
  images,
  startIndex = 0,
  onClose,
}: {
  images: LightboxImage[];
  startIndex?: number;
  onClose: () => void;
}) {
  return (
    <Lightbox
      open
      close={onClose}
      index={startIndex}
      slides={images.map((image) => ({ src: image.src, alt: image.alt }))}
      plugins={[Zoom, Counter]}
      // 桌面端直接滚轮缩放更顺手，不必按住 Ctrl
      zoom={{ scrollToZoom: true, maxZoomPixelRatio: 5 }}
      counter={{ container: { style: { top: "unset", bottom: 0 } } }}
      carousel={{ finite: images.length <= 1 }}
      controller={{ closeOnBackdropClick: true }}
      styles={{
        container: { backgroundColor: "rgba(0, 0, 0, .88)" },
        // 默认按钮只有投影，压在浅色图上几乎看不见
        button: {
          filter: "none",
          color: "#fff",
          backgroundColor: "rgba(0, 0, 0, .5)",
          borderRadius: "10px",
        },
      }}
      noScroll={{ disabled: true }}
    />
  );
}
