import { act, render } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReaderBridge } from "../../../src/renderer/reader/bridge";
import { initReaderI18n } from "../../../src/renderer/reader/i18n";
import { layoutStyle } from "../../../src/renderer/reader/layout";
import {
  parseHostMessage,
  parseLayout,
  READER_PROTOCOL,
  type HostMessage,
  type HostView,
  type ReaderLayout,
  type ReaderTheme,
} from "../../../src/renderer/reader/protocol";
import { ReaderApp } from "../../../src/renderer/reader/ReaderApp";

// 台账 B-068：阅读区起点与尺寸带小数时，宿主发 layout 消息，页面把根节点挪过去并按精确尺寸设宽高；整数坐标是严格的 no-op。

vi.mock("../../../src/renderer/components/library/ImageLightbox", async () =>
  import("../../../src/renderer/reader/lightbox-shim"),
);

// CodeMirror 在 jsdom 里没有版面：换成只暴露 DOM 骨架的假编辑器（与 reader-app.test.tsx 同一做法）。
vi.mock("../../../src/renderer/components/library/MarkdownEditor", async () => {
  const React = await import("react");
  return {
    MarkdownEditor: () =>
      React.createElement(
        "div",
        { "data-testid": "markdown-editor" },
        React.createElement("div", { className: "cm-editor" }, React.createElement("div", { className: "cm-content" })),
      ),
  };
});

if (!Element.prototype.scrollIntoView) {
  Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
}

class FakeResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const LIGHT: ReaderTheme = { dark: false, hue: 210, saturation: 35, basePx: 18, motion: "standard" };
// 台账 B-068 的实测值：根字号 18 px、1280×800 里阅读区是 698, 222.75, 582×577.25，子窗口取整成 698, 223 → 偏移 (0, -0.25)。
const FRACTIONAL: ReaderLayout = { dx: 0, dy: -0.25, width: 582, height: 577.25 };

function rig() {
  let handler: ((message: HostMessage) => void) | null = null;
  const bridge: ReaderBridge = {
    send: () => undefined,
    listen(next) {
      handler = next;
      return () => {
        handler = null;
      };
    },
  };
  return {
    bridge,
    post: (message: HostMessage) => {
      act(() => {
        handler?.(message);
      });
    },
  };
}

function view(kind: HostView["view"], patch: Partial<HostView> = {}): HostView {
  return {
    t: "view",
    seq: 1,
    itemId: "a",
    tab: "body",
    view: kind,
    restoreTop: null,
    catalogOpen: false,
    tocOpen: false,
    ...patch,
  };
}

const FORUM = ["### 1 楼 · 甲", "第一楼的内容", "", "### 2 楼 · 乙", "第二楼的内容"].join("\n");

// 每种阅读岛视图：它们共用同一个根节点，版面样式必须对所有视图一视同仁。
const VIEWS: Array<[string, HostView]> = [
  ["original", view({ kind: "original", content: "# 标题\n\n正文", sourceKind: "body" })],
  ["markdown", view({ kind: "markdown", content: "识别文字", tab: "recognized" }, { tab: "recognized" })],
  ["replies", view({ kind: "replies", content: FORUM }, { tab: "replies" })],
  ["gallery", view({ kind: "gallery", content: "![图](local-image://a.png)" }, { tab: "images" })],
  ["editor", view({ kind: "editor", content: "# 草稿", sourceKind: "body" })],
  [
    "snapshot",
    view({
      kind: "snapshot",
      document: "<!doctype html><html><body><h2>第一章</h2></body></html>",
      instanceId: "inst-1",
      versionId: "",
      width: "fluid",
      assets: [],
    }),
  ],
];

function mount() {
  const r = rig();
  const utils = render(<ReaderApp bridge={r.bridge} />);
  r.post({ t: "init", v: READER_PROTOCOL, theme: LIGHT, lang: "zh" });
  /** 页面的根节点：阅读区所有内容的外层，版面样式落在它上面。 */
  const root = () => utils.container.firstElementChild as HTMLElement;
  return { ...r, ...utils, root };
}

beforeAll(async () => {
  await initReaderI18n("zh");
});

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
  const html = document.documentElement;
  html.className = "";
  html.removeAttribute("style");
  html.removeAttribute("data-motion");
  html.lang = "zh-CN";
});

describe("layoutStyle：版面 → 根节点的内联样式", () => {
  it("没有版面（整数像素）是 undefined：根节点不带任何内联样式", () => {
    expect(layoutStyle(null)).toBeUndefined();
  });

  it("小数偏移与精确尺寸原样落成 left / top / width / height", () => {
    expect(layoutStyle(FRACTIONAL)).toEqual({ left: 0, top: -0.25, width: 582, height: 577.25 });
    expect(layoutStyle({ dx: 0.4, dy: -0.5, width: 100.5, height: 40 })).toEqual({
      left: 0.4,
      top: -0.5,
      width: 100.5,
      height: 40,
    });
  });

  it("不用 transform：版面偏移之后盒子照常吸附到设备像素，transform 会把边线抗锯齿成灰边", () => {
    expect(Object.keys(layoutStyle(FRACTIONAL) ?? {})).not.toContain("transform");
  });
});

describe("layout 消息的解析", () => {
  it("版面与 null（回到满视口）都能解析", () => {
    expect(parseHostMessage(JSON.stringify({ t: "layout", layout: FRACTIONAL }))).toEqual({ t: "layout", layout: FRACTIONAL });
    expect(parseHostMessage(JSON.stringify({ t: "layout", layout: null }))).toEqual({ t: "layout", layout: null });
  });

  it("缺 layout 字段与 null 不是一回事：缺字段整条丢弃，不能当成清除", () => {
    expect(parseHostMessage(JSON.stringify({ t: "layout" }))).toBeNull();
  });

  it.each([
    ["字段缺失", { dx: 0, dy: 0, width: 10 }],
    ["字段不是数字", { dx: "0", dy: 0, width: 10, height: 10 }],
    ["偏移超出取整误差", { dx: 1.5, dy: 0, width: 10, height: 10 }],
    ["负的垂直偏移超出", { dx: 0, dy: -1.01, width: 10, height: 10 }],
    ["零宽", { dx: 0, dy: 0, width: 0, height: 10 }],
    ["负高", { dx: 0, dy: 0, width: 10, height: -1 }],
    ["尺寸过大", { dx: 0, dy: 0, width: 20001, height: 10 }],
    ["不是对象", [0, 0, 10, 10]],
    ["字符串", "x"],
  ])("坏版面整条丢弃：%s", (_name, layout) => {
    expect(parseLayout(layout)).toBeNull();
    expect(parseHostMessage(JSON.stringify({ t: "layout", layout }))).toBeNull();
  });

  it("边界值可以：偏移恰为 ±1、尺寸恰为上限", () => {
    expect(parseLayout({ dx: 1, dy: -1, width: 20000, height: 0.5 })).toEqual({ dx: 1, dy: -1, width: 20000, height: 0.5 });
  });

  it("NaN 与无穷经 JSON 会变成 null，直接传入也被拒绝", () => {
    expect(parseLayout({ dx: Number.NaN, dy: 0, width: 10, height: 10 })).toBeNull();
    expect(parseLayout({ dx: 0, dy: 0, width: Number.POSITIVE_INFINITY, height: 10 })).toBeNull();
  });
});

describe("阅读岛页面：小数像素版面", () => {
  it("没有收到 layout 时根节点没有 style 属性（整数像素严格 no-op）", () => {
    const { root, post } = mount();
    post(view({ kind: "original", content: "正文", sourceKind: "body" }));
    expect(root().hasAttribute("style")).toBe(false);
  });

  it("收到 layout 把根节点挪 (dx, dy) 并按精确尺寸设宽高", () => {
    const { root, post } = mount();
    post({ t: "layout", layout: FRACTIONAL });
    post(view({ kind: "original", content: "正文", sourceKind: "body" }));
    const style = root().style;
    expect(style.getPropertyValue("left")).toBe("0px");
    expect(style.getPropertyValue("top")).toBe("-0.25px");
    expect(style.getPropertyValue("width")).toBe("582px");
    expect(style.getPropertyValue("height")).toBe("577.25px");
    expect(style.getPropertyValue("transform")).toBe("");
    // 根节点仍是相对定位的（left / top 靠它生效）、仍然裁切。
    expect(root().className).toContain("relative");
    expect(root().className).toContain("overflow-hidden");
  });

  it("layout 比 view 晚到也生效，并且换内容（条目、标签、视图种类）不丢版面", () => {
    const { root, post, container } = mount();
    post(view({ kind: "original", content: "正文", sourceKind: "body" }));
    expect(root().hasAttribute("style")).toBe(false);
    post({ t: "layout", layout: FRACTIONAL });
    expect(root().style.getPropertyValue("top")).toBe("-0.25px");
    post(view({ kind: "replies", content: FORUM }, { seq: 2, itemId: "b", tab: "replies" }));
    expect(container.querySelector("[data-article-reader]")).not.toBeNull();
    expect(root().style.getPropertyValue("top")).toBe("-0.25px");
    expect(root().style.getPropertyValue("height")).toBe("577.25px");
  });

  it("layout 为 null 回到满视口：内联样式被清掉", () => {
    const { root, post } = mount();
    post({ t: "layout", layout: FRACTIONAL });
    post(view({ kind: "original", content: "正文", sourceKind: "body" }));
    expect(root().hasAttribute("style")).toBe(true);
    post({ t: "layout", layout: null });
    expect(root().style.getPropertyValue("top")).toBe("");
    expect(root().style.getPropertyValue("width")).toBe("");
    expect(root().style.getPropertyValue("height")).toBe("");
    expect(root().style.getPropertyValue("left")).toBe("");
  });

  it("版面变化（窗口缩放）只改根节点样式，不重建内容", () => {
    const { root, post, container } = mount();
    post({ t: "layout", layout: FRACTIONAL });
    post(view({ kind: "original", content: "正文", sourceKind: "body" }));
    const reader = container.querySelector("[data-article-reader]");
    post({ t: "layout", layout: { dx: 0, dy: 0.25, width: 582, height: 577.75 } });
    expect(container.querySelector("[data-article-reader]")).toBe(reader);
    expect(root().style.getPropertyValue("top")).toBe("0.25px");
    expect(root().style.getPropertyValue("height")).toBe("577.75px");
  });

  it.each(VIEWS)("所有阅读岛视图共用同一个根节点：%s", async (_name, hostView) => {
    const { root, post, container } = mount();
    post({ t: "layout", layout: FRACTIONAL });
    post(hostView);
    // 编辑器是懒加载的分块：等它挂好，证明版面样式不影响内容的挂载。
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.querySelector("[data-article-reader]") ?? container.querySelector("iframe")).not.toBeNull();
    expect(root().style.getPropertyValue("left")).toBe("0px");
    expect(root().style.getPropertyValue("top")).toBe("-0.25px");
    expect(root().style.getPropertyValue("width")).toBe("582px");
    expect(root().style.getPropertyValue("height")).toBe("577.25px");
    // 内容是根节点的后代：整页只有这一个带版面的节点，内部的滚动区、sticky 与 fixed 元素不被动过。
    expect(container.querySelectorAll("[style*='top: -0.25px']").length).toBe(1);
  });
});
