import { describe, expect, it, vi } from "vitest";
import { createBridge } from "../../../src/renderer/reader/bridge";
import type { HostMessage } from "../../../src/renderer/reader/protocol";

/**
 * 假窗口：桥的形状与 Viva v0.6.4 的 WebBridgeScript 一致——页面发给宿主用 postMessage(text)，
 * 宿主发给页面时 deliver(text) 以 { data: text } 调用 onmessage（internal/platform/web_cuts.go）。
 * 第一版桥按「直接给字符串」写，真 WebView2 里所有宿主消息都被丢弃（readersmoke 抓到）；这里把真桥的契约钉住。
 */
function fakeWindow() {
  const posted: string[] = [];
  const viva = {
    onmessage: null as ((event: { data?: unknown } | string) => void) | null,
    postMessage: (text: string) => void posted.push(text),
    deliver(text: string) {
      if (typeof viva.onmessage === "function") viva.onmessage({ data: text });
    },
  };
  return { win: { viva } as unknown as Window, viva, posted };
}

const theme = { dark: false, hue: 220, saturation: 70, basePx: 16, motion: "standard" };

describe("阅读岛桥：与 Viva 的 window.viva 契约", () => {
  it("宿主消息以 { data } 送达（Viva 的 deliver 的形状）", () => {
    const { win, viva } = fakeWindow();
    const received: HostMessage[] = [];
    createBridge(win).listen((m) => received.push(m));
    viva.deliver(JSON.stringify({ t: "init", v: 1, theme, lang: "zh" }));
    expect(received).toEqual([{ t: "init", v: 1, theme, lang: "zh" }]);
  });

  it("直接给字符串的投递方式同样收下", () => {
    const { win, viva } = fakeWindow();
    const received: HostMessage[] = [];
    createBridge(win).listen((m) => received.push(m));
    viva.onmessage?.(JSON.stringify({ t: "theme", theme, lang: "en" }));
    expect(received).toEqual([{ t: "theme", theme, lang: "en" }]);
  });

  it("形状不对的消息被丢掉：不是字符串的 data、不是 JSON、缺 data", () => {
    const { win, viva } = fakeWindow();
    const handler = vi.fn();
    createBridge(win).listen(handler);
    viva.onmessage?.({ data: { t: "init" } });
    viva.onmessage?.({ data: 42 });
    viva.onmessage?.({});
    viva.onmessage?.({ data: "不是 JSON" });
    viva.onmessage?.(undefined as unknown as string);
    expect(handler).not.toHaveBeenCalled();
  });

  it("取消订阅只摘掉自己的处理函数", () => {
    const { win, viva } = fakeWindow();
    const handler = vi.fn();
    const stop = createBridge(win).listen(handler);
    stop();
    expect(viva.onmessage).toBeNull();
    viva.deliver(JSON.stringify({ t: "theme", theme, lang: "zh" }));
    expect(handler).not.toHaveBeenCalled();

    const first = createBridge(win).listen(handler);
    const own = viva.onmessage;
    viva.onmessage = () => undefined; // 之后别人接管了槽位
    first();
    expect(viva.onmessage).not.toBe(own);
    expect(viva.onmessage).not.toBeNull();
  });

  it("发给宿主的事件是 JSON 文本，没有桥时静默丢弃", () => {
    const { win, posted } = fakeWindow();
    createBridge(win).send({ t: "ready", v: 1 });
    expect(posted).toEqual([JSON.stringify({ t: "ready", v: 1 })]);
    expect(() => createBridge({} as Window).send({ t: "ready", v: 1 })).not.toThrow();
    expect(createBridge({} as Window).listen(() => undefined)).toBeTypeOf("function");
  });
});