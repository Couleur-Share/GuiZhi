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

describe("阅读岛页面：阅读区根节点的标记", () => {
  // 渲染层 ContentPanel 在阅读区根节点（data-article-reader 所在元素）上写 data-article-editing 与 data-article-target，
  // 「问答」取选区引用时据此认出这是哪一条的哪个视图；岛里没有编辑态，editing 恒为 false。
  const root = (container: HTMLElement) => container.querySelector("[data-article-reader]") as HTMLElement;
  const target = (container: HTMLElement) => JSON.parse(root(container).getAttribute("data-article-target") ?? "null");

  it("原文：目标取条目、标签与来源种类", () => {
    const { post, container } = mount();
    post(view({ itemId: "item-9", tab: "summary", view: { kind: "original", content: "总结", sourceKind: "summary" } }));
    expect(root(container).getAttribute("data-article-editing")).toBe("false");
    expect(target(container)).toEqual({ itemId: "item-9", view: "summary", sourceKind: "summary" });
    post(view({ seq: 2, itemId: "item-9", tab: "body", view: { kind: "original", content: "正文", sourceKind: "body" } }));
    expect(target(container)).toEqual({ itemId: "item-9", view: "body", sourceKind: "body" });
  });

  it("论坛讨论、识别文字：来源种类取 body；图库（images 标签）对应 body 视图", () => {
    const { post, container } = mount();
    post(view({ itemId: "f1", tab: "replies", view: { kind: "replies", content: FORUM } }));
    expect(root(container).getAttribute("data-article-editing")).toBe("false");
    expect(target(container)).toEqual({ itemId: "f1", view: "replies", sourceKind: "body" });
    post(view({ seq: 2, itemId: "p1", tab: "recognized", view: { kind: "markdown", content: "识别出的文字", tab: "recognized" } }));
    expect(target(container)).toEqual({ itemId: "p1", view: "recognized", sourceKind: "body" });
    post(view({ seq: 3, itemId: "p1", tab: "images", view: { kind: "gallery", content: "![图](local-image://p.png)" } }));
    expect(root(container).getAttribute("data-article-editing")).toBe("false");
    expect(target(container)).toEqual({ itemId: "p1", view: "body", sourceKind: "body" });
  });

  it("换条目后目标跟着换（不残留上一条的）", () => {
    const { post, container } = mount();
    post(view({ itemId: "a" }));
    expect(target(container).itemId).toBe("a");
    post(view({ seq: 2, itemId: "b" }));
    expect(target(container).itemId).toBe("b");
  });
});

describe("阅读岛页面：不可信内容（台账 A-018 的验收项）", () => {
  // 条目正文来自网页抓取、论坛与用户粘贴，是不可信输入。岛的内容安全策略管不到 <meta http-equiv=refresh>、被点击的 <a> 与岛自己脚本
  // 触发的导航（A-018），所以页面一侧要先把内容清理干净：这里用恶意内容走一遍真实的渲染链路
  // （ReaderApp → 渲染层的 MarkdownPreview：rehype-raw + rehype-sanitize），检查产物 DOM，而不是只信「同一套组件」。
  // javascript: 链接有三道闸门（消毒白名单、react-markdown 的 urlTransform、链接组件的 resolveSafeHref），任意一道在位都挡得住，
  // 所以这里断言最终 DOM；变异检查：去掉消毒 / 放行 iframe / 放行 script·meta·base 各被杀，三道闸门全开也被杀，单开一道是等价变异。
  const HOSTILE_BLOCK = [
    "<script>window.__pwned = 1</script>",
    '<meta http-equiv="refresh" content="0;url=https://evil.example/m">',
    '<iframe src="https://evil.example/f"></iframe>',
    '<base href="https://evil.example/">',
    '<form action="https://evil.example/post"><input name="x"></form>',
    '<object data="https://evil.example/o"></object><embed src="https://evil.example/e">',
    '<link rel="stylesheet" href="https://evil.example/s.css">',
    '<style>@import url("https://evil.example/i.css");</style>',
    '<img src="x" onerror="window.__pwned = 2">',
    '<svg onload="window.__pwned = 3"><circle r="1"/></svg>',
    "[脚本链接](javascript:window.__pwned=4)",
    '<a href="javascript:window.__pwned=5">内联脚本链接</a>',
    '<a href="https://example.com/ok" onclick="window.__pwned=6" target="_blank">正常链接</a>',
    "正文最后一段",
  ].join("\n\n");
  const FORBIDDEN = "script, meta, iframe, frame, frameset, base, form, object, embed, link, style, noscript, template";

  function expectClean(container: HTMLElement) {
    const reader = container.querySelector("[data-article-reader]") as HTMLElement;
    expect(reader, "阅读区应已渲染").not.toBeNull();
    expect(Array.from(reader.querySelectorAll(FORBIDDEN)).map((el) => el.tagName)).toEqual([]);
    const handlers = Array.from(reader.querySelectorAll("*")).flatMap((el) =>
      Array.from(el.attributes)
        .filter((attr) => /^on/i.test(attr.name))
        .map((attr) => `${el.tagName}.${attr.name}`),
    );
    expect(handlers).toEqual([]);
    for (const anchor of Array.from(reader.querySelectorAll("a"))) {
      expect(anchor.getAttribute("href") ?? "").not.toMatch(/^\s*(javascript|data|vbscript):/i);
    }
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
    expect(reader.textContent).toContain("正文最后一段");
  }

  it("原文：脚本、meta refresh、内嵌框架、<base>、表单、对象、外链样式、事件属性、javascript: 链接都被清掉，正文与正常链接留下", () => {
    const { post, container } = mount();
    post(view({ view: { kind: "original", content: `# 标题\n\n${HOSTILE_BLOCK}`, sourceKind: "body" } }));
    expectClean(container);
    expect(container.querySelector("h1")?.textContent).toBe("标题");
    const ok = container.querySelector('a[href="https://example.com/ok"]');
    expect(ok?.textContent).toBe("正常链接");
    expect(ok?.hasAttribute("onclick")).toBe(false);
  });

  it("Markdown 预览（图中文字）同样清理", () => {
    const { post, container } = mount();
    post(view({ tab: "recognized", view: { kind: "markdown", content: HOSTILE_BLOCK, tab: "recognized" } }));
    expectClean(container);
  });

  it("论坛楼层卡片的正文同样清理（卡片正文是网页抓来的）", () => {
    const { post, container } = mount();
    const floors = ["### 1 楼 · 甲", HOSTILE_BLOCK, "", "### 2 楼 · 乙", "> 回复 @甲（1 楼）：<script>window.__pwned = 7</script>", "第二楼的内容"].join("\n");
    post(view({ tab: "replies", view: { kind: "replies", content: floors } }));
    expectClean(container);
    expect(container.querySelectorAll("li[data-floor]")).toHaveLength(2);
  });

  it("清理之后留下的链接点击只交宿主，页面不导航；脚本链接没有可点的地址", () => {
    const { post, container, events } = mount();
    post(view({ view: { kind: "original", content: HOSTILE_BLOCK, sourceKind: "body" } }));
    const ok = container.querySelector('a[href="https://example.com/ok"]') as HTMLAnchorElement;
    expect(fireEvent.click(ok)).toBe(false);
    expect(events("link")).toEqual([{ t: "link", seq: 1, url: "https://example.com/ok" }]);
    for (const anchor of Array.from(container.querySelectorAll("a")).filter((a) => !a.getAttribute("href"))) {
      fireEvent.click(anchor);
    }
    expect(events("link")).toHaveLength(1);
  });
});