import { createRoot } from "react-dom/client";
import "../styles/globals.css";
import "./viewer.css";
import { createChannel } from "./bridge";
import { ViewerApp } from "./ViewerApp";
import { parseViewerMessage, type ViewerEvent, type ViewerHostMessage } from "./viewer-protocol";

/**
 * 图片查看岛的入口（jsgen/build-reader.mts 的第二次构建把它打成 viewer/viewer.js / viewer.css，嵌进 Go 壳）。
 * 起始语言取宿主写在 <html lang> 上的值；ImageLightbox 自己没有会随语言变化的文案。
 */
const bridge = createChannel<ViewerHostMessage, ViewerEvent>(parseViewerMessage);

// 渲染之外的脚本错误只告诉宿主一个说明（宿主只记诊断，不把它当界面文案）。
window.addEventListener("error", (event) => {
  bridge.send({ t: "error", message: String(event.message ?? "").slice(0, 1024) });
});

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(<ViewerApp bridge={bridge} />);
}