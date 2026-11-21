import { useEffect, useMemo, useState } from "react";
import { ImageLightbox } from "../components/library/ImageLightbox";
import type { Channel } from "./bridge";
import { rewriteMediaUrl } from "./media-urls";
import { applyTheme } from "./theme";
import {
  VIEWER_PROTOCOL,
  type ViewerEvent,
  type ViewerHostMessage,
  type ViewerInit,
} from "./viewer-protocol";

/**
 * 图片查看岛的页面：宿主开一个盖满窗口的岛，把图片列表与起始序号交给它，这里渲染渲染层自己的 ImageLightbox
 * （同一个组件、同一份配置，所以缩放、平移、键盘与计数的行为与渲染层一致）。关闭（Esc、关闭钮、点背景）只是告诉宿主，
 * 岛由宿主销毁。
 *
 * 页面背景是纯黑（viewer.css）：ImageLightbox 的容器是 rgba(0,0,0,.88)，渲染层里它压在应用界面上，岛是不透明的原生窗口，
 * 透不出后面的界面，所以这里压在黑底上（台账 A 类）；外观（字体与根字号）仍按主题写到 <html> 上，计数等文字与渲染层一致。
 */
export function ViewerApp({ bridge }: { bridge: Channel<ViewerHostMessage, ViewerEvent> }) {
  const [init, setInit] = useState<ViewerInit | null>(null);

  useEffect(() => {
    const stop = bridge.listen((message) => {
      applyTheme(document.documentElement, message.theme, message.lang);
      setInit(message);
    });
    bridge.send({ t: "ready", v: VIEWER_PROTOCOL });
    return stop;
  }, [bridge]);

  const images = useMemo(
    () => init?.images.map((image) => ({ src: rewriteMediaUrl(image.src), alt: image.alt })) ?? [],
    [init],
  );
  if (!init) return null;
  return (
    <ImageLightbox
      images={images}
      startIndex={init.index}
      onClose={() => bridge.send({ t: "close" })}
    />
  );
}