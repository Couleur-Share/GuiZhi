import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { ReaderBridge } from "../../../src/renderer/reader/bridge";
import { initReaderI18n } from "../../../src/renderer/reader/i18n";
import { installMediaRewrite } from "../../../src/renderer/reader/media-urls";
import {
  READER_PROTOCOL,
  type HostMessage,
  type HostView,
  type PageEvent,
  type ReaderTheme,
} from "../../../src/renderer/reader/protocol";
import { ReaderApp } from "../../../src/renderer/reader/ReaderApp";

// 岛的构建把渲染层的 ImageLightbox 换成垫片（jsgen/build-reader.mts 的别名）；单测里用 mock 走同一条接缝。
vi.mock("../../../src/renderer/components/library/ImageLightbox", async () =>
  import("../../../src/renderer/reader/lightbox-shim"),
);

// jsdom 没有 scrollIntoView（目录浮层把当前章节滚进视野时要用）。
if (!Element.prototype.scrollIntoView) {
  Object.defineProperty(Element.prototype, "scrollIntoView", {
    configurable: true,
    value: vi.fn(),
  });
}

const LIGHT: ReaderTheme = {
  dark: false,
  hue: 210,
  saturation: 35,
  basePx: 16,
  motion: "standard",
};

/** 假桥：记录页面发出的事件与调用顺序，测试里用 post 扮演宿主。 */
function rig() {
  let handler: ((message: HostMessage) => void) | null = null;
  const sent: PageEvent[] = [];
  const order: string[] = [];
  const bridge: ReaderBridge = {
    send(event) {
      sent.push(event);
      order.push(`send:${event.t}`);
    },
    listen(next) {
      handler = next;
      order.push("listen");
      return () => {
        handler = null;
      };
    },
  };
  return {
    bridge,
    sent,
    order,
    post: (message: HostMessage) =>
      act(() => {
        handler?.(message);
      }),
    events<T extends PageEvent["t"]>(t: T) {
      return sent.filter((event): event is Extract<PageEvent, { t: T }> => event.t === t);
    },
  };
}

function view(patch: Partial<HostView> = {}): HostView {
  return {
    t: "view",
    seq: 1,
    itemId: "a",
    tab: "body",
    view: { kind: "original", content: "# 标题\n\n正文第一段", sourceKind: "body" },
    restoreTop: null,
    catalogOpen: false,
    tocOpen: false,
    ...patch,
  };
}

function mount() {
  const r = rig();
  const utils = render(<ReaderApp bridge={r.bridge} />);
  r.post({ t: "init", v: READER_PROTOCOL, theme: LIGHT, lang: "zh" });
  return { ...r, ...utils };
}

const FORUM = [
  "### 1 楼 · 甲",
  "第一楼的内容",
  "",
  "### 2 楼 · 乙",
  "> 回复 @甲（1 楼）：第一楼的内容",
  "第二楼的内容",
].join("\n");

beforeAll(async () => {
  await initReaderI18n("zh");
});

afterEach(() => {
  const root = document.documentElement;
  root.className = "";
  root.removeAttribute("style");
  root.removeAttribute("data-motion");
  root.lang = "zh-CN";
});

describe("阅读岛页面：启动与外观", () => {
  it("订阅装好之后才报 ready，并带协议版本", () => {
    const r = rig();
    render(<ReaderApp bridge={r.bridge} />);
    expect(r.order[0]).toBe("listen");
    expect(r.order.indexOf("send:ready")).toBeGreaterThan(0);
    expect(r.events("ready")).toEqual([{ t: "ready", v: READER_PROTOCOL }]);
  });

  it("theme 消息写到 <html>：深色类、色相饱和度、根字号、动效、语言", () => {
    const { post } = mount();
    post({
      t: "theme",
      theme: { dark: true, hue: 120, saturation: 50, basePx: 18, motion: "reduced" },
      lang: "en",
    });
    const root = document.documentElement;
    expect(root.classList.contains("dark")).toBe(true);
    expect(root.style.getPropertyValue("--theme-hue")).toBe("120");
    expect(root.style.getPropertyValue("--theme-saturation")).toBe("50");
    expect(root.style.getPropertyValue("--base-font-size")).toBe("18px");
    expect(root.dataset.motion).toBe("reduced");
    expect(root.lang).toBe("en");
    post({ t: "theme", theme: LIGHT, lang: "zh" });
    expect(root.classList.contains("dark")).toBe(false);
    expect(root.lang).toBe("zh-CN");
  });

  it("还没收到 view 时只有空壳，不报 rendered", () => {
    const { container, events } = mount();
    expect(container.querySelector("[data-article-reader]")).toBeNull();
    expect(events("rendered")).toHaveLength(0);
  });
});

describe("阅读岛页面：原文", () => {
  it("按渲染层的 DOM 链画出正文，并带着 seq 报 rendered", () => {
    const { post, container, events } = mount();
    post(view({ seq: 7 }));
    const reader = container.querySelector("[data-article-reader]");
    expect(reader).not.toBeNull();
    expect(
      reader?.querySelector(
        '[data-testid="reconstruction-reader"] [data-testid="themed-reading-pane"] .article-prose-scroll',
      ),
    ).not.toBeNull();
    expect(container.textContent).toContain("正文第一段");
    expect(container.querySelector("h1")?.textContent).toBe("标题");
    expect(events("rendered").at(-1)).toEqual({ t: "rendered", seq: 7 });
  });

  it("同一条换内容保留页面实例，换条目才重建", () => {
    const { post, container } = mount();
    post(view());
    const first = container.querySelector(".article-prose-scroll");
    post(
      view({
        seq: 2,
        view: { kind: "original", content: "新的正文", sourceKind: "body" },
      }),
    );
    expect(container.querySelector(".article-prose-scroll")).toBe(first);
    expect(container.textContent).toContain("新的正文");
    post(view({ seq: 3, itemId: "b" }));
    expect(container.querySelector(".article-prose-scroll")).not.toBe(first);
  });

  it("view 为 none 时只留阅读区空壳", () => {
    const { post, container } = mount();
    post(view({ view: { kind: "none" } }));
    expect(container.querySelector("[data-article-reader]")).not.toBeNull();
    expect(container.querySelector(".article-prose-scroll")).toBeNull();
  });

  it("restoreTop 在打开这条时恢复一次，之后的内容更新不再拉回去", async () => {
    const { post, container } = mount();
    post(view({ restoreTop: 120 }));
    const scroller = container.querySelector(".article-prose-scroll") as HTMLElement;
    await waitFor(() => expect(scroller.scrollTop).toBe(120));
    scroller.scrollTop = 300;
    post(
      view({
        seq: 2,
        restoreTop: 120,
        view: { kind: "original", content: "更新后的正文", sourceKind: "body" },
      }),
    );
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(scroller.scrollTop).toBe(300);
  });
});

describe("阅读岛页面：滚动上报", () => {
  it("滚动停 200 ms 后报一次，位置是停下时的值", () => {
    vi.useFakeTimers();
    const { post, container, events } = mount();
    post(view({ seq: 4 }));
    const scroller = container.querySelector(".article-prose-scroll") as HTMLElement;
    scroller.scrollTop = 40;
    fireEvent.scroll(scroller);
    scroller.scrollTop = 90;
    fireEvent.scroll(scroller);
    act(() => {
      vi.advanceTimersByTime(199);
    });
    expect(events("scroll")).toHaveLength(0);
    act(() => {
      vi.advanceTimersByTime(2);
    });
    expect(events("scroll")).toEqual([{ t: "scroll", seq: 4, top: 90 }]);
  });

  it("换条目时把还没发出的位置按旧 seq 补发", () => {
    vi.useFakeTimers();
    const { post, container, events } = mount();
    post(view({ seq: 4 }));
    const scroller = container.querySelector(".article-prose-scroll") as HTMLElement;
    scroller.scrollTop = 55;
    fireEvent.scroll(scroller);
    post(view({ seq: 5, itemId: "b" }));
    expect(events("scroll")).toEqual([{ t: "scroll", seq: 4, top: 55 }]);
    act(() => {
      vi.advanceTimersByTime(500);
    });
    // 补发之后旧的定时器已清掉，不会再多报一次
    expect(events("scroll")).toHaveLength(1);
  });
});

describe("阅读岛页面：页内查找", () => {
  it("find 把命中词标成 mark，并上报命中数", () => {
    const { post, container, events } = mount();
    post(view({ seq: 2 }));
    expect(events("matches").at(-1)).toEqual({ t: "matches", seq: 2, count: 0 });
    post({ t: "find", query: "正文", index: 0 });
    expect(container.querySelectorAll("mark")).toHaveLength(1);
    expect(events("matches").at(-1)).toEqual({ t: "matches", seq: 2, count: 1 });
    post({ t: "find", query: "", index: 0 });
    expect(container.querySelectorAll("mark")).toHaveLength(0);
    expect(events("matches").at(-1)).toEqual({ t: "matches", seq: 2, count: 0 });
  });
});

describe("阅读岛页面：目录浮层", () => {
  it("tocOpen 为真时显示浮层，关闭时报 toc 事件并带 seq", () => {
    const { post, container, events, getByText } = mount();
    post(
      view({
        seq: 6,
        tocOpen: true,
        view: {
          kind: "original",
          content: "# 第一章\n\n内容\n\n## 第二节\n\n更多内容",
          sourceKind: "body",
        },
      }),
    );
    const toc = container.querySelector('[data-testid="snapshot-toc"]');
    expect(toc).not.toBeNull();
    expect(toc?.textContent).toContain("第一章");
    expect(toc?.textContent).toContain("第二节");
    fireEvent.click(getByText("第二节", { selector: "nav button" }));
    expect(events("toc").at(-1)).toEqual({ t: "toc", seq: 6, open: false });
    // 宿主随后回灌 ui 状态：浮层关闭
    post({ t: "ui", catalogOpen: false, tocOpen: false });
    expect(container.querySelector('[data-testid="snapshot-toc"]')).toBeNull();
  });

  it("没有章节标题时显示空态文案", () => {
    const { post, container } = mount();
    post(
      view({
        tocOpen: true,
        view: { kind: "original", content: "只有一段话", sourceKind: "body" },
      }),
    );
    expect(container.querySelector('[data-testid="snapshot-toc"]')?.textContent).toContain(
      "未识别到明确章节标题",
    );
  });
});

describe("阅读岛页面：交给宿主处理的交互", () => {
  it("外链点击不让页面导航，交宿主；页内锚点放行", () => {
    const { post, container, events } = mount();
    post(
      view({
        view: {
          kind: "original",
          content: "[站点](https://example.com/page)",
          sourceKind: "body",
        },
      }),
    );
    const external = container.querySelector('a[href^="https://example.com"]') as HTMLAnchorElement;
    expect(external).not.toBeNull();
    const notPrevented = fireEvent.click(external);
    expect(notPrevented).toBe(false);
    expect(events("link")).toEqual([{ t: "link", seq: 1, url: "https://example.com/page" }]);
  });

  it("页内锚点放行给浏览器自己滚动，不上报", () => {
    const { post, container, events } = mount();
    post(view());
    const anchor = document.createElement("a");
    anchor.setAttribute("href", "#section");
    container.appendChild(anchor);
    expect(fireEvent.click(anchor)).toBe(true);
    expect(events("link")).toHaveLength(0);
  });

  it("代码块的复制按钮把文本交宿主", async () => {
    const { post, container, events, getByLabelText } = mount();
    post(
      view({
        seq: 9,
        view: { kind: "original", content: "```js\nconsole.log(1)\n```", sourceKind: "body" },
      }),
    );
    expect(container.querySelector("pre")).not.toBeNull();
    fireEvent.click(getByLabelText("复制代码"));
    await waitFor(() => expect(events("copy")).toHaveLength(1));
    expect(events("copy")[0]).toEqual({ t: "copy", seq: 9, text: "console.log(1)" });
  });

  it("只转发 Ctrl/Alt 组合的白名单快捷键；页面右键菜单被抑制", () => {
    const { post, events } = mount();
    post(view({ seq: 3 }));
    fireEvent.keyDown(document.body, { key: "f", ctrlKey: true });
    fireEvent.keyDown(document.body, { key: "ArrowLeft", altKey: true });
    fireEvent.keyDown(document.body, { key: "f" });
    fireEvent.keyDown(document.body, { key: "c", ctrlKey: true });
    expect(events("key")).toEqual([
      { t: "key", seq: 3, key: "f", ctrl: true, alt: false, shift: false },
      { t: "key", seq: 3, key: "arrowleft", ctrl: false, alt: true, shift: false },
    ]);
    expect(fireEvent.contextMenu(document.body)).toBe(false);
  });

  it("点本地图片：把图片列表与序号交宿主，页面里不留查看器", () => {
    installMediaRewrite();
    const { post, container, events } = mount();
    post(
      view({
        seq: 5,
        view: {
          kind: "original",
          content: "![一](local-image://a.png)\n\n![二](local-image://b.png)",
          sourceKind: "body",
        },
      }),
    );
    const images = container.querySelectorAll("img");
    expect(images).toHaveLength(2);
    // 页面里的 local-image:// 被改写成岛自己的路径
    expect(images[1].getAttribute("src")).toBe("./media/local-image/b.png");
    fireEvent.click(images[1]);
    expect(events("lightbox")).toEqual([
      {
        t: "lightbox",
        seq: 5,
        images: [
          { src: "local-image://a.png", alt: "一" },
          { src: "local-image://b.png", alt: "二" },
        ],
        index: 1,
      },
    ]);
    expect(container.querySelector('[role="dialog"]')).toBeNull();
  });
});

describe("阅读岛页面：论坛讨论", () => {
  it("楼层卡片与楼层目录；目录开合报 catalog 事件", () => {
    const { post, container, events, getByLabelText } = mount();
    post(view({ tab: "replies", view: { kind: "replies", content: FORUM } }));
    const cards = container.querySelectorAll("li[data-floor]");
    expect(cards).toHaveLength(2);
    expect(container.textContent).toContain("第二楼的内容");
    fireEvent.click(getByLabelText("展开楼层目录"));
    expect(events("catalog").at(-1)).toEqual({ t: "catalog", seq: 1, open: true });
    post({ t: "ui", catalogOpen: true, tocOpen: false });
    expect(getByLabelText("收起楼层目录")).not.toBeNull();
  });

  it("find 过滤楼层并上报命中数；点跳楼被过滤的楼会请求清除查找词", () => {
    const { post, container, events } = mount();
    post(
      view({ tab: "replies", catalogOpen: true, view: { kind: "replies", content: FORUM } }),
    );
    post({ t: "find", query: "第二楼", index: 0 });
    expect(container.querySelectorAll("li[data-floor]")).toHaveLength(1);
    expect(events("matches").at(-1)?.count).toBe(1);
    const catalogButtons = [...container.querySelectorAll("ul button")].filter((button) =>
      button.textContent?.includes("1 楼"),
    );
    expect(catalogButtons.length).toBeGreaterThan(0);
    fireEvent.click(catalogButtons[0]);
    expect(events("findReset")).toEqual([{ t: "findReset", seq: 1 }]);
    // 本地立刻清掉过滤词，两楼都回来了
    expect(container.querySelectorAll("li[data-floor]")).toHaveLength(2);
  });

  it("点「回复 @」找不到目标楼时给出提示事件", () => {
    const content = ["### 1 楼 · 甲", "内容", "", "### 2 楼 · 乙", "> 回复 @丙（9 楼）：不存在", "回复"].join("\n");
    const { post, container, events } = mount();
    post(view({ tab: "replies", view: { kind: "replies", content } }));
    const jump = [...container.querySelectorAll("button")].find((button) =>
      button.textContent?.startsWith("回复 @"),
    );
    expect(jump).toBeDefined();
    fireEvent.click(jump as HTMLElement);
    expect(events("toast")).toEqual([
      { t: "toast", seq: 1, kind: "warning", text: "该楼未入库或无法定位" },
    ]);
  });

  it("没有楼层时显示空态", () => {
    const { post, container } = mount();
    post(view({ tab: "replies", view: { kind: "replies", content: "没有任何楼层" } }));
    expect(container.textContent).toContain("暂无讨论内容");
  });
});

describe("阅读岛页面：图片相关视图", () => {
  it("图片条目的识别文字用纯 Markdown 预览，并可查找", () => {
    const { post, container, events } = mount();
    post(
      view({
        tab: "recognized",
        view: { kind: "markdown", content: "识别出的文字", tab: "recognized" },
      }),
    );
    expect(container.textContent).toContain("识别出的文字");
    post({ t: "find", query: "识别", index: 0 });
    expect(container.querySelectorAll("mark")).toHaveLength(1);
    expect(events("matches").at(-1)?.count).toBe(1);
  });

  it("画廊分支挂出并报 rendered", () => {
    const { post, container, events } = mount();
    post(view({ seq: 8, tab: "images", view: { kind: "gallery", content: "![图](local-image://p.png)" } }));
    expect(container.querySelector("[data-article-reader]")).not.toBeNull();
    expect(events("rendered").at(-1)).toEqual({ t: "rendered", seq: 8 });
  });
});
