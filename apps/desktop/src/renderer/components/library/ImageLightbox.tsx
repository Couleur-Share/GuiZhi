import Lightbox from "yet-another-react-lightbox";
import Counter from "yet-another-react-lightbox/plugins/counter";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import type { LightboxProps } from "yet-another-react-lightbox";
import "yet-another-react-lightbox/styles.css";
import "yet-another-react-lightbox/plugins/counter.css";

export interface LightboxImage {
  src: string;
  alt?: string;
}

// yet-another-react-lightbox narrows slot styles more than React's CSSProperties,
// while Electron still needs this vendor property to keep the window drag region
// from swallowing the viewer controls. Keep the cast at this integration boundary.
type LightboxSlotStyle = NonNullable<
  NonNullable<LightboxProps["styles"]>[keyof NonNullable<LightboxProps["styles"]>]
>;
const NO_DRAG = { WebkitAppRegion: "no-drag" } as LightboxSlotStyle;

/**
 * 图片查看器：滚轮缩放、拖拽平移、双击放大、键盘 +/-/方向键，多图左右切换。
 * 故意不放工具栏放大/缩小按钮——桌面端滚轮已够用，少两个图标更干净。
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
      render={{ buttonZoom: () => null }}
      counter={{ container: { style: { top: "unset", bottom: 0 } } }}
      carousel={{ finite: images.length <= 1 }}
      controller={{ closeOnBackdropClick: true }}
      styles={{
        // Windows 无边框窗：TitleBar/TopBar 的 drag 区无视 z-index 吞指针事件。
        // 关闭钮中心正好压在 TopBar 上沿（约 y=32），不标 no-drag 就点不中。
        root: NO_DRAG,
        container: {
          backgroundColor: "rgba(0, 0, 0, .88)",
          ...NO_DRAG,
        },
        // Zoom 的 transform 会建层，不抬 z-index 时工具栏偶发被盖住
        toolbar: { zIndex: 1, ...NO_DRAG },
        // 默认按钮只有投影，压在浅色图上几乎看不见
        button: {
          filter: "none",
          color: "#fff",
          backgroundColor: "rgba(0, 0, 0, .5)",
          borderRadius: "10px",
          ...NO_DRAG,
        },
        // 关闭图标是镂空 X：SVG 默认只命中描边，点正中间会穿到下层
        icon: { pointerEvents: "none" },
      }}
      noScroll={{ disabled: true }}
    />
  );
}
