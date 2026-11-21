import {
  parseHostMessage,
  type HostMessage,
  type PageEvent,
} from "./protocol";

/**
 * Viva 注入到岛页面里的桥（BridgeMainFrameOnly：只有主框架有）。
 * 宿主 → 页面：Viva 的 deliver 以 `{ data: text }`（MessageEvent 的形状）调用 onmessage，不是直接给字符串
 * （viva v0.6.4 internal/platform/web_cuts.go）。这一点只有真 WebView2 里才暴露：
 * 第一版按「直接给字符串」写，所有宿主消息被静默丢弃，页面永远停在空白占位（台账 R-15，readersmoke 抓到）。
 */
interface VivaBridge {
  postMessage(text: string): void;
  onmessage?: ((event: { data?: unknown } | string) => void) | null;
}

declare global {
  interface Window {
    viva?: VivaBridge;
  }
}

/** 页面与宿主之间的通道；测试与对照工具注入假桥，页面代码不直接碰 window.viva。 */
export interface ReaderBridge {
  /** 给宿主发一个事件；没有桥（普通浏览器里打开页面）时静默丢弃。 */
  send(event: PageEvent): void;
  /** 订阅宿主消息；返回取消订阅的函数。同一时刻只有一个订阅者（桥只有一个 onmessage 槽）。 */
  listen(handler: (message: HostMessage) => void): () => void;
}

export function createBridge(win: Window = window): ReaderBridge {
  return {
    send(event) {
      win.viva?.postMessage(JSON.stringify(event));
    },
    listen(handler) {
      const viva = win.viva;
      if (!viva) return () => undefined;
      const onmessage = (event: { data?: unknown } | string): void => {
        // 真桥给 { data }；字符串形状只为兼容别的投递方（islandprobe 的页面同样两种都收）。
        const text = typeof event === "string" ? event : event?.data;
        if (typeof text !== "string") return;
        const message = parseHostMessage(text);
        // 形状不对的消息直接丢掉：宿主是可信的，所以这只会出现在版本不一致或测试里。
        if (message) handler(message);
      };
      viva.onmessage = onmessage;
      return () => {
        if (viva.onmessage === onmessage) viva.onmessage = null;
      };
    },
  };
}
