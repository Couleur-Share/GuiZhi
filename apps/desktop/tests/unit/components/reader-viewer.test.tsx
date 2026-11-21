import { act, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createChannel, type Channel } from "../../../src/renderer/reader/bridge";
import { ViewerApp } from "../../../src/renderer/reader/ViewerApp";
import {
  VIEWER_PROTOCOL,
  parseViewerMessage,
  type ViewerEvent,
  type ViewerHostMessage,
} from "../../../src/renderer/reader/viewer-protocol";

// 真组件（yet-another-react-lightbox）在 jsdom 里缺 ResizeObserver 等；这里只关心页面交给它什么、关闭时说了什么。
const lightbox = vi.hoisted(() => ({ calls: [] as Array<Record<string, unknown>> }));
vi.mock("../../../src/renderer/components/library/ImageLightbox", async () => {
  const { createElement } = await import("react");
  return {
    ImageLightbox: (props: Record<string, unknown>) => {
      lightbox.calls.push(props);
      return createElement("div", { "data-testid": "lightbox" });
    },
  };
});

const theme = { dark: true, hue: 200, saturation: 55, basePx: 18, motion: "reduced" };

function init(overrides: Partial<ViewerHostMessage> = {}): ViewerHostMessage {
  return {
    t: "init",
    v: VIEWER_PROTOCOL,
    theme: theme as ViewerHostMessage["theme"],
    lang: "zh",
    images: [{ src: "local-image://a b.png", alt: "图 A" }, { src: "https://example.com/x.jpg" }],
    index: 1,
    ...overrides,
  };
}

function fakeBridge() {
  let handler: ((message: ViewerHostMessage) => void) | null = null;
  const sent: ViewerEvent[] = [];
  const bridge: Channel<ViewerHostMessage, ViewerEvent> = {
    send: (event) => void sent.push(event),
    listen: (next) => {
      handler = next;
      return () => {
        if (handler === next) handler = null;
      };
    },
  };
  return {
    bridge,
    sent,
    listening: () => handler !== null,
    deliver: (message: ViewerHostMessage) => act(() => handler?.(message)),
  };
}

afterEach(() => {
  lightbox.calls.length = 0;
  document.documentElement.className = "";
  document.documentElement.removeAttribute("style");
});

describe("图片查看岛页面", () => {
  it("挂载即握手，收到图片之前什么也不画", () => {
    const { bridge, sent } = fakeBridge();
    const view = render(<ViewerApp bridge={bridge} />);
    expect(sent).toEqual([{ t: "ready", v: VIEWER_PROTOCOL }]);
    expect(view.queryByTestId("lightbox")).toBeNull();
    expect(lightbox.calls).toHaveLength(0);
  });

  it("init 之后把图片与起始序号交给 ImageLightbox，本地图片改写成岛自己的 media 路径", () => {
    const { bridge, deliver } = fakeBridge();
    const view = render(<ViewerApp bridge={bridge} />);
    deliver(init());
    expect(view.getByTestId("lightbox")).toBeTruthy();
    const props = lightbox.calls.at(-1)!;
    expect(props.startIndex).toBe(1);
    expect(props.images).toEqual([
      { src: "./media/local-image/a%20b.png", alt: "图 A" },
      { src: "https://example.com/x.jpg", alt: undefined },
    ]);
  });

  it("init 带的外观写到 <html> 上：深色类、色相、饱和度、根字号、动效档位、语言", () => {
    const { bridge, deliver } = fakeBridge();
    render(<ViewerApp bridge={bridge} />);
    deliver(init({ lang: "en" }));
    const root = document.documentElement;
    expect(root.classList.contains("dark")).toBe(true);
    expect(root.style.getPropertyValue("--theme-hue")).toBe("200");
    expect(root.style.getPropertyValue("--theme-saturation")).toBe("55");
    expect(root.style.getPropertyValue("--base-font-size")).toBe("18px");
    expect(root.dataset.motion).toBe("reduced");
    expect(root.lang).toBe("en");
  });

  it("用户关闭查看器（onClose）时告诉宿主 close", () => {
    const { bridge, sent, deliver } = fakeBridge();
    render(<ViewerApp bridge={bridge} />);
    deliver(init());
    act(() => (lightbox.calls.at(-1)!.onClose as () => void)());
    expect(sent).toEqual([{ t: "ready", v: VIEWER_PROTOCOL }, { t: "close" }]);
  });

  it("再收到一份 init 就换成新的图片与序号", () => {
    const { bridge, deliver } = fakeBridge();
    render(<ViewerApp bridge={bridge} />);
    deliver(init());
    deliver(init({ images: [{ src: "https://example.com/y.png" }], index: 0 }));
    const props = lightbox.calls.at(-1)!;
    expect(props.startIndex).toBe(0);
    expect(props.images).toEqual([{ src: "https://example.com/y.png", alt: undefined }]);
  });

  it("卸载时取消订阅", () => {
    const { bridge, listening } = fakeBridge();
    const view = render(<ViewerApp bridge={bridge} />);
    expect(listening()).toBe(true);
    view.unmount();
    expect(listening()).toBe(false);
  });

  it("经真桥的 { data } 形状送达", () => {
    const posted: string[] = [];
    const viva = {
      onmessage: null as ((event: { data?: unknown } | string) => void) | null,
      postMessage: (text: string) => void posted.push(text),
    };
    const channel = createChannel<ViewerHostMessage, ViewerEvent>(parseViewerMessage, { viva } as unknown as Window);
    const view = render(<ViewerApp bridge={channel} />);
    act(() => viva.onmessage?.({ data: JSON.stringify(init()) }));
    expect(view.getByTestId("lightbox")).toBeTruthy();
    expect(posted).toEqual([JSON.stringify({ t: "ready", v: VIEWER_PROTOCOL })]);
  });
});

describe("图片查看岛协议：宿主消息的解析", () => {
  const good = JSON.stringify(init());

  it("合规的 init 原样解析，alt 只在是字符串时保留", () => {
    const message = parseViewerMessage(JSON.stringify(init({ images: [{ src: "https://e.com/a.png", alt: "x" }, { src: "https://e.com/b.png" }] })));
    expect(message?.images).toEqual([{ src: "https://e.com/a.png", alt: "x" }, { src: "https://e.com/b.png" }]);
  });

  it("起始序号夹在图片数之内，小数取整", () => {
    expect(parseViewerMessage(JSON.stringify({ ...init(), index: 99 }))?.index).toBe(1);
    expect(parseViewerMessage(JSON.stringify({ ...init(), index: -3 }))?.index).toBe(0);
    expect(parseViewerMessage(JSON.stringify({ ...init(), index: 0.9 }))?.index).toBe(0);
  });

  it("形状不对的一律丢弃", () => {
    const base = JSON.parse(good) as Record<string, unknown>;
    const bad: unknown[] = [
      "不是 JSON",
      "[]",
      { ...base, t: "view" },
      { ...base, v: "1" },
      { ...base, images: [] },
      { ...base, images: "x" },
      { ...base, images: [{ src: "" }] },
      { ...base, images: [{ alt: "无地址" }] },
      { ...base, images: [null] },
      { ...base, index: "1" },
      { ...base, index: Number.NaN },
      { ...base, theme: { ...theme, basePx: 4 } },
      { ...base, theme: "dark" },
    ];
    for (const entry of bad) {
      const text = typeof entry === "string" ? entry : JSON.stringify(entry);
      expect(parseViewerMessage(text), text).toBeNull();
    }
  });

  it("语言只认 en，其余按中文", () => {
    expect(parseViewerMessage(JSON.stringify({ ...init(), lang: "fr" }))?.lang).toBe("zh");
    expect(parseViewerMessage(JSON.stringify({ ...init(), lang: "en" }))?.lang).toBe("en");
  });
});