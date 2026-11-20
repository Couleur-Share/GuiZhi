import { createRoot } from "react-dom/client";
import "../styles/globals.css";
import "./reader.css";
import { createBridge } from "./bridge";
import { initReaderI18n } from "./i18n";
import { installMediaRewrite } from "./media-urls";
import { ReaderApp } from "./ReaderApp";

/**
 * 阅读岛的入口（jsgen/build-reader.mts 把它打成 reader.js / reader.css，嵌进 Go 壳）。
 * 起始语言取宿主写在 <html lang> 上的值（首屏文案就对），之后以宿主的 theme 消息为准。
 */
installMediaRewrite();
void initReaderI18n(document.documentElement.lang.startsWith("en") ? "en" : "zh");

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(<ReaderApp bridge={createBridge()} />);
}
