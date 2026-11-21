import {
  READER_PROTOCOL,
  isRecord,
  parseLang,
  parseTheme,
  type ReaderLang,
  type ReaderTheme,
} from "./protocol";

/**
 * 图片查看岛的消息协议（v1）：宿主（apps/desktop-go/viva/reader/viewer*.go）与查看页（ViewerApp）之间的文本 JSON。
 *
 * 查看页只渲染渲染层自己的 ImageLightbox（yet-another-react-lightbox，滚轮缩放、拖拽平移、键盘与计数），
 * 所以协议很小：页面握手（ready）、宿主给图片与起始序号（init）、页面要求关闭（close）。
 * 宿主把页面事件当作不可信输入校验（viva/reader/viewer_protocol.go 另有一份），两侧字段名必须一致。
 *
 * Image viewer island protocol v1: ready / init / close / error.
 */

export const VIEWER_PROTOCOL = READER_PROTOCOL;

export interface ViewerImage {
  /** local-image://<名称> 或 http(s) 地址；本地的由页面改写成岛自己的 media/ 路径。 */
  src: string;
  alt?: string;
}

export interface ViewerInit {
  t: "init";
  v: number;
  theme: ReaderTheme;
  lang: ReaderLang;
  images: ViewerImage[];
  /** 起始序号，已夹在 [0, images.length - 1]。 */
  index: number;
}

export type ViewerHostMessage = ViewerInit;

export type ViewerEvent =
  | { t: "ready"; v: number }
  /** 用户关闭了查看器（Esc、关闭钮、点背景）：宿主据此销毁这个岛。 */
  | { t: "close" }
  | { t: "error"; message: string };

function parseImages(value: unknown): ViewerImage[] | null {
  if (!Array.isArray(value) || value.length === 0) return null;
  const images: ViewerImage[] = [];
  for (const entry of value) {
    if (!isRecord(entry) || typeof entry.src !== "string" || entry.src === "") return null;
    const alt = typeof entry.alt === "string" ? entry.alt : undefined;
    images.push(alt === undefined ? { src: entry.src } : { src: entry.src, alt });
  }
  return images;
}

/** 解析宿主发来的一条消息；形状不对返回 null。 */
export function parseViewerMessage(text: string): ViewerHostMessage | null {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isRecord(raw) || raw.t !== "init" || typeof raw.v !== "number") return null;
  const theme = parseTheme(raw.theme);
  const images = parseImages(raw.images);
  if (!theme || !images || typeof raw.index !== "number" || !Number.isFinite(raw.index)) return null;
  const index = Math.min(Math.max(Math.trunc(raw.index), 0), images.length - 1);
  return { t: "init", v: raw.v, theme, lang: parseLang(raw.lang), images, index };
}