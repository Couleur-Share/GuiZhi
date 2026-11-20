import {
  parseHostMessage,
  type HostMessage,
  type PageEvent,
} from "./protocol";

/** Viva 注入到岛页面里的桥（BridgeMainFrameOnly：只有主框架有）。 */
interface VivaBridge {
  postMessage(text: string): void;
  onmessage?: ((text: string) => void) | null;
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
      const onmessage = (text: string): void => {
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
