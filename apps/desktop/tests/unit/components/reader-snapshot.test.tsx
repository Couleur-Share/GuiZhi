import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReaderBridge } from "../../../src/renderer/reader/bridge";
import { initReaderI18n } from "../../../src/renderer/reader/i18n";
import {
  parseHostMessage,
  READER_PROTOCOL,
  type HostMessage,
  type HostView,
  type PageEvent,
  type ReaderTheme,
  type SnapshotWidth,
} from "../../../src/renderer/reader/protocol";
import { restoreLocalImageUrl } from "../../../src/renderer/reader/media-urls";
import { inlineLocalImages, localImageRefs } from "../../../src/renderer/reader/snapshot-images";
import { ReaderApp } from "../../../src/renderer/reader/ReaderApp";
import { safeSnapshotLink } from "../../../src/renderer/reader/SnapshotIslandView";

// 岛的构建把渲染层的 ImageLightbox 换成垫片（jsgen/build-reader.mts 的别名）；单测里用 mock 走同一条接缝。
vi.mock("../../../src/renderer/components/library/ImageLightbox", async () =>
  import("../../../src/renderer/reader/lightbox-shim"),
);

const LIGHT: ReaderTheme = { dark: false, hue: 210, saturation: 35, basePx: 16, motion: "standard" };
const INSTANCE = "inst-1";
const DOC = "<!doctype html><html><body><h2>第一章</h2><p>正文</p></body></html>";
const ASSETS = ["local-image://a.png", "local-image://b.png"];

/** 假桥：记录页面发出的事件，测试里用 post 扮演宿主。 */
function rig() {
  let handler: ((message: HostMessage) => void) | null = null;
  const sent: PageEvent[] = [];
  const bridge: ReaderBridge = {
    send: (event) => void sent.push(event),
    listen(next) {
      handler = next;
      return () => {
        handler = null;
      };
    },
  };
  return {
    bridge,
    sent,
    post: (message: HostMessage) => act(() => void handler?.(message)),
    events<T extends PageEvent["t"]>(t: T) {
      return sent.filter((event): event is Extract<PageEvent, { t: T }> => event.t === t);
    },
  };
}

function snapshotView(
  patch: Partial<Omit<Extract<HostView["view"], { kind: "snapshot" }>, "kind">> = {},
  top: Partial<HostView> = {},
): HostView {
  return {
    t: "view",
    seq: 1,
    itemId: "item-1",
    tab: "body",
    view: {
      kind: "snapshot",
      document: DOC,
      instanceId: INSTANCE,
      versionId: "ver-1",
      width: "fluid",
      assets: ASSETS,
      ...patch,
    },
    restoreTop: null,
    catalogOpen: false,
    tocOpen: false,
    ...top,
  };
}

function mount(view: HostView = snapshotView()) {
  const r = rig();
  const utils = render(<ReaderApp bridge={r.bridge} />);
  r.post({ t: "init", v: READER_PROTOCOL, theme: LIGHT, lang: "zh" });
  r.post(view);
  const frame = utils.container.querySelector("iframe") as HTMLIFrameElement;
  return { ...r, ...utils, frame };
}

/** 扮演 iframe 里的桥脚本：来源、origin、id 都可以改，用来验证校验。 */
function fromFrame(
  frame: HTMLIFrameElement,
  type: string,
  value: unknown,
  opts: { source?: Window | null; origin?: string; id?: unknown } = {},
) {
  const data = { id: "id" in opts ? opts.id : INSTANCE, type, value };
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", {
        data,
        source: "source" in opts ? opts.source : frame.contentWindow,
        origin: opts.origin ?? "null",
      }),
    );
  });
}

// ResizeObserver：useSnapshotReader 用它判断目录能否常驻正文侧边的窄轨（窗口宽度减去 iframe 宽度的一半 ≥ 64）。
const observers: Array<() => void> = [];
class FakeResizeObserver {
  constructor(callback: () => void) {
    observers.push(callback);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

let scrollTo: ReturnType<typeof vi.fn>;
let hostWidth = 0;
let frameWidth = 0;

beforeAll(async () => {
  await initReaderI18n("zh");
});

beforeEach(() => {
  observers.length = 0;
  hostWidth = 0;
  frameWidth = 0;
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  scrollTo = vi.fn();
  Object.defineProperty(Element.prototype, "scrollTo", { configurable: true, writable: true, value: scrollTo });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => hostWidth });
  vi.spyOn(HTMLIFrameElement.prototype, "getBoundingClientRect").mockImplementation(
    () => ({ width: frameWidth, height: 0, top: 0, left: 0, right: 0, bottom: 0, x: 0, y: 0, toJSON: () => ({}) }),
  );
  if (!Element.prototype.scrollIntoView) {
    Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
  }
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Reflect.deleteProperty(Element.prototype, "scrollTo");
  Reflect.deleteProperty(HTMLElement.prototype, "clientWidth");
  localStorage.clear();
  const root = document.documentElement;
  root.className = "";
  root.removeAttribute("style");
  root.removeAttribute("data-motion");
});

describe("阅读岛页面：网页快照的协议解析", () => {
  const base = { t: "view", seq: 3, itemId: "i", tab: "body" };
  const snapshot = { kind: "snapshot", document: DOC, instanceId: "x", versionId: "", width: "wide", assets: ASSETS };

  it("snapshot 视图的各字段都解析出来", () => {
    expect(parseHostMessage(JSON.stringify({ ...base, view: snapshot }))).toMatchObject({
      t: "view",
      view: { kind: "snapshot", document: DOC, instanceId: "x", versionId: "", width: "wide", assets: ASSETS },
    });
  });

  it("缺字段、宽度未知、实例标识为空、图片不是本地图片都丢弃整条", () => {
    const bad = [
      { ...snapshot, document: 1 },
      { ...snapshot, instanceId: "" },
      { ...snapshot, instanceId: 7 },
      { ...snapshot, versionId: null },
      { ...snapshot, width: "huge" },
      { ...snapshot, width: undefined },
      { ...snapshot, assets: "local-image://a.png" },
      { ...snapshot, assets: ["https://example.com/a.png"] },
      { ...snapshot, assets: [1] },
    ];
    for (const view of bad) expect(parseHostMessage(JSON.stringify({ ...base, view }))).toBeNull();
  });

  it("ui 消息的 snapshotWidth 可选，未知取值忽略", () => {
    expect(parseHostMessage(JSON.stringify({ t: "ui", catalogOpen: false, tocOpen: true, snapshotWidth: "original" }))).toEqual({
      t: "ui",
      catalogOpen: false,
      tocOpen: true,
      snapshotWidth: "original",
    });
    expect(parseHostMessage(JSON.stringify({ t: "ui", catalogOpen: false, tocOpen: false }))).toEqual({
      t: "ui",
      catalogOpen: false,
      tocOpen: false,
    });
    expect(parseHostMessage(JSON.stringify({ t: "ui", catalogOpen: false, tocOpen: false, snapshotWidth: "huge" }))).toEqual({
      t: "ui",
      catalogOpen: false,
      tocOpen: false,
    });
  });
});

describe("阅读岛页面：网页快照的 iframe", () => {
  it("渲染沙箱 iframe：只许脚本、无来源、不带 referrer，文档放进 srcDoc", () => {
    const { frame } = mount();
    expect(frame).toBeTruthy();
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(frame.getAttribute("srcdoc")).toBe(DOC);
    expect(frame.hasAttribute("src")).toBe(false);
    expect(frame.getAttribute("aria-label")).toBe("微信公众号原文快照");
    expect(JSON.parse(frame.dataset.articleTarget ?? "{}")).toEqual({ itemId: "item-1", view: "snapshot", versionId: "ver-1" });
    expect(frame.dataset.articleInstance).toBe(INSTANCE);
    expect(frame.className).toContain("mx-auto");
  });

  it("宽度三档与 WebSnapshotPane 一致，宿主改宽度只改样式、不重载 iframe", () => {
    // jsdom 的 CSSStyleDeclaration 不认 min()，写进去会被丢掉；这里记录 React 给 style.width 赋的值。
    const assigned: string[] = [];
    const descriptor = Object.getOwnPropertyDescriptor(CSSStyleDeclaration.prototype, "width")!;
    Object.defineProperty(CSSStyleDeclaration.prototype, "width", {
      configurable: true,
      get: descriptor.get,
      set(value: string) {
        assigned.push(String(value));
        descriptor.set?.call(this, value);
      },
    });
    try {
      const { frame, post, container } = mount();
      // fluid：舒适阅读宽度（字号 16 → 16*36+48 = 624）。
      expect(assigned.at(-1)).toBe("min(100%, 624px)");
      expect(frame.style.maxWidth).toBe("100%");
      const widths: Array<[SnapshotWidth, string]> = [
        ["original", "390px"],
        ["wide", "100%"],
        ["fluid", "min(100%, 624px)"],
      ];
      for (const [width, css] of widths) {
        post({ t: "ui", catalogOpen: false, tocOpen: false, snapshotWidth: width });
        expect(container.querySelector("iframe")).toBe(frame); // 同一个节点：没有重建
        expect(assigned.at(-1)).toBe(css);
        expect(frame.getAttribute("srcdoc")).toBe(DOC);
      }
    } finally {
      Object.defineProperty(CSSStyleDeclaration.prototype, "width", descriptor);
    }
  });

  it("view 消息带的宽度作为初始值；不带 snapshotWidth 的 ui 消息不改宽度", () => {
    const { frame, post } = mount(snapshotView({ width: "original" }));
    expect(frame.style.width).toBe("390px");
    post({ t: "ui", catalogOpen: false, tocOpen: true });
    expect(frame.style.width).toBe("390px");
    post({ t: "ui", catalogOpen: false, tocOpen: true, snapshotWidth: "wide" });
    expect(frame.style.width).toBe("100%");
  });

  it("文档更新（同一内容身份）只换 srcDoc", () => {
    const { frame, post, container } = mount();
    post(snapshotView({ document: `${DOC}<p>更新</p>`, instanceId: "inst-2" }, { seq: 2 }));
    expect(container.querySelector("iframe")).toBe(frame);
    expect(frame.getAttribute("srcdoc")).toBe(`${DOC}<p>更新</p>`);
    expect(frame.dataset.articleInstance).toBe("inst-2");
  });
});

describe("阅读岛页面：iframe 消息的来源校验", () => {
  it("高度：只认本 iframe、origin 为 null、实例标识一致的消息", () => {
    const { frame } = mount();
    const other = document.createElement("iframe");
    document.body.append(other);
    fromFrame(frame, "height", 900, { source: other.contentWindow });
    fromFrame(frame, "height", 900, { source: null });
    fromFrame(frame, "height", 900, { source: window });
    fromFrame(frame, "height", 900, { origin: "https://evil.example" });
    fromFrame(frame, "height", 900, { origin: "" });
    fromFrame(frame, "height", 900, { id: "inst-other" });
    fromFrame(frame, "height", 900, { id: undefined });
    expect(frame.style.height).toBe("400px");
    fromFrame(frame, "height", 900);
    expect(frame.style.height).toBe("900px");
    other.remove();
  });

  it("换了实例标识之后，旧实例的消息作废", () => {
    const { frame, post } = mount();
    post(snapshotView({ instanceId: "inst-2" }, { seq: 2 }));
    fromFrame(frame, "height", 900); // 仍用 inst-1
    expect(frame.style.height).toBe("400px");
    fromFrame(frame, "height", 900, { id: "inst-2" });
    expect(frame.style.height).toBe("900px");
  });

  it("非法的高度与锚点被忽略，合法值夹在范围内", () => {
    const { frame } = mount();
    for (const value of [NaN, Infinity, "900", null, {}]) fromFrame(frame, "height", value);
    expect(frame.style.height).toBe("400px");
    fromFrame(frame, "height", 50);
    expect(frame.style.height).toBe("100px");
    fromFrame(frame, "height", 9_999_999);
    expect(frame.style.height).toBe("2000000px");

    for (const value of [NaN, -Infinity, "10", null]) fromFrame(frame, "anchor", value);
    expect(scrollTo).not.toHaveBeenCalled();
    fromFrame(frame, "anchor", 321);
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 321 });
    fromFrame(frame, "anchor", -50);
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 });
    fromFrame(frame, "anchor", 9_999_999);
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 2_000_000 });
  });

  it("来源不对的 link / image / find-open / escape 一概不转交宿主", () => {
    const { frame, sent } = mount();
    const before = sent.length;
    const bad = { origin: "https://evil.example" };
    fromFrame(frame, "link", "https://example.com/", bad);
    fromFrame(frame, "image", ASSETS[0], bad);
    fromFrame(frame, "find-open", null, bad);
    fromFrame(frame, "escape", null, bad);
    fromFrame(frame, "link", "https://example.com/", { id: "x" });
    expect(sent.length).toBe(before);
  });
});

describe("阅读岛页面：网页快照的页内查找", () => {
  it("宿主的 find 转交 iframe（带实例标识与递增的请求号，查找词截断到 1000）", () => {
    const { frame, post } = mount();
    const spy = vi.spyOn(frame.contentWindow!, "postMessage");
    post({ t: "find", query: "关键词", index: 2 });
    expect(spy).toHaveBeenCalledTimes(1);
    const [first, target] = spy.mock.calls[0] as [{ id: string; type: string; value: { query: string; index: number; request: number } }, string];
    expect(target).toBe("*");
    expect(first).toMatchObject({ id: INSTANCE, type: "find", value: { query: "关键词", index: 2 } });
    post({ t: "find", query: "关键词", index: 3 });
    const second = spy.mock.calls[1][0] as typeof first;
    expect(second.value.request).toBeGreaterThan(first.value.request);
    post({ t: "find", query: "x".repeat(1500), index: 0 });
    expect((spy.mock.calls[2][0] as typeof first).value.query).toHaveLength(1000);
  });

  it("find-result：请求号对得上才回传命中数，并把命中处滚到 top − 60", () => {
    const { frame, post, events } = mount();
    const spy = vi.spyOn(frame.contentWindow!, "postMessage");
    post({ t: "find", query: "词", index: 0 });
    const request = (spy.mock.calls[0][0] as { value: { request: number } }).value.request;

    fromFrame(frame, "find-result", { request: request - 1, count: 5, top: 500 }); // 过期的回包
    expect(events("matches")).toEqual([]);
    expect(scrollTo).not.toHaveBeenCalled();

    fromFrame(frame, "find-result", { request, count: 5, top: 500 });
    expect(events("matches")).toEqual([{ t: "matches", seq: 1, count: 5 }]);
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 440 });

    fromFrame(frame, "find-result", { request, count: 0, top: null }); // 无命中：不滚动
    expect(events("matches").at(-1)).toEqual({ t: "matches", seq: 1, count: 0 });
    expect(scrollTo).toHaveBeenCalledTimes(1);

    fromFrame(frame, "find-result", { request, count: 3, top: 10 }); // 靠近顶部：不滚到负数
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 });
  });

  it("命中数越界、不是整数、请求号缺失都丢弃", () => {
    const { frame, post, events } = mount();
    const spy = vi.spyOn(frame.contentWindow!, "postMessage");
    post({ t: "find", query: "词", index: 0 });
    const request = (spy.mock.calls[0][0] as { value: { request: number } }).value.request;
    for (const value of [
      { request, count: -1 },
      { request, count: 1.5 },
      { request, count: 10001 },
      { request, count: "3" },
      { count: 3 },
      null,
      "x",
    ]) {
      fromFrame(frame, "find-result", value);
    }
    expect(events("matches")).toEqual([]);
    fromFrame(frame, "find-result", { request, count: 10000 }); // 上限含端点
    expect(events("matches")).toEqual([{ t: "matches", seq: 1, count: 10000 }]);
  });

  it("iframe 里的 Ctrl+F 与 Esc 映射成宿主既有的快捷键事件", () => {
    const { frame, events } = mount();
    fromFrame(frame, "find-open", null);
    fromFrame(frame, "escape", null);
    fromFrame(frame, "find-open", "x"); // value 必须是 null
    fromFrame(frame, "escape", 1);
    expect(events("key")).toEqual([
      { t: "key", seq: 1, key: "f", ctrl: true, alt: false, shift: false },
      { t: "key", seq: 1, key: "escape", ctrl: false, alt: false, shift: false },
    ]);
  });

  it("上报的序号跟着最新的 view 走", () => {
    const { frame, post, events } = mount();
    post(snapshotView({}, { seq: 7 }));
    fromFrame(frame, "find-open", null);
    expect(events("key").at(-1)).toMatchObject({ seq: 7 });
  });
});

describe("阅读岛页面：网页快照的外链", () => {
  it("只放行 http / https 且无凭证的地址，规整后交宿主", () => {
    const { frame, events } = mount();
    fromFrame(frame, "link", "https://example.com/a?b=1#c");
    fromFrame(frame, "link", "http://example.com");
    expect(events("link")).toEqual([
      { t: "link", seq: 1, url: "https://example.com/a?b=1#c" },
      { t: "link", seq: 1, url: "http://example.com/" },
    ]);
  });

  it("拒绝 javascript: / data: / file: / 带凭证 / 无效 / 非字符串", () => {
    const { frame, events } = mount();
    for (const value of [
      "javascript:alert(1)",
      "JAVASCRIPT:alert(1)",
      "data:text/html,<script>alert(1)</script>",
      "file:///C:/Windows/win.ini",
      "local-image://a.png",
      "gzreader://doc/index.html",
      "https://user@example.com/",
      "https://user:pass@example.com/",
      "http://:pass@example.com/",
      "/relative/path",
      "not a url",
      "",
      `https://example.com/${"a".repeat(9000)}`,
      42,
      null,
      { href: "https://example.com" },
    ]) {
      fromFrame(frame, "link", value);
    }
    expect(events("link")).toEqual([]);
  });

  it("safeSnapshotLink 与 WebSnapshotPane 同一条规则", () => {
    expect(safeSnapshotLink("https://example.com")).toBe("https://example.com/");
    expect(safeSnapshotLink("ftp://example.com")).toBeNull();
    expect(safeSnapshotLink("https://a:b@example.com")).toBeNull();
    expect(safeSnapshotLink("javascript:void(0)")).toBeNull();
  });
});

describe("阅读岛页面：网页快照的图片", () => {
  it("点击清单里的图片：把整份清单与命中的序号交宿主（图片查看岛）", () => {
    const { frame, events } = mount();
    fromFrame(frame, "image", ASSETS[1]);
    expect(events("lightbox")).toEqual([
      { t: "lightbox", seq: 1, images: [{ src: ASSETS[0] }, { src: ASSETS[1] }], index: 1 },
    ]);
  });

  it("桥接脚本回报宿主改写后的岛内地址：还原成清单里的写法再找序号", () => {
    const { frame, events } = mount();
    // 宿主把文档里的 local-image://b.png 改写成 ./media/local-image/b.png（viva/reader 的 AdaptSnapshotDocument）。
    fromFrame(frame, "image", "./media/local-image/b.png");
    expect(events("lightbox")).toEqual([
      { t: "lightbox", seq: 1, images: [{ src: ASSETS[0] }, { src: ASSETS[1] }], index: 1 },
    ]);
  });

  it("岛内地址的百分号编码要解码；清单外的岛内地址、越权路径都不打开", () => {
    expect(restoreLocalImageUrl("./media/local-image/a%20b.png")).toBe("local-image://a b.png");
    expect(restoreLocalImageUrl("./media/local-image/sub/%E5%9B%BE.png")).toBe("local-image://sub/图.png");
    expect(restoreLocalImageUrl("./media/local-image/%E0%A4%A.png")).toBe("local-image://%E0%A4%A.png");
    expect(restoreLocalImageUrl("local-image://a.png")).toBe("local-image://a.png");
    expect(restoreLocalImageUrl("https://example.com/a.png")).toBe("https://example.com/a.png");
    expect(restoreLocalImageUrl("./media/local-video/a.mp4")).toBe("./media/local-video/a.mp4");
    const { frame, events } = mount();
    for (const value of ["./media/local-image/missing.png", "./media/local-image/../a.png", "./media/local-image/"]) {
      fromFrame(frame, "image", value);
    }
    expect(events("lightbox")).toEqual([]);
  });

  it("不在清单里的地址、远程地址、非字符串都不打开", () => {
    const { frame, events } = mount();
    for (const value of ["local-image://missing.png", "https://example.com/a.png", "", 3, null, ASSETS]) {
      fromFrame(frame, "image", value);
    }
    expect(events("lightbox")).toEqual([]);
  });

  it("图片清单随 view 更新", () => {
    const { frame, post, events } = mount();
    post(snapshotView({ assets: ["local-image://c.png"] }, { seq: 2 }));
    fromFrame(frame, "image", ASSETS[0]);
    expect(events("lightbox")).toEqual([]);
    fromFrame(frame, "image", "local-image://c.png");
    expect(events("lightbox")).toEqual([{ t: "lightbox", seq: 2, images: [{ src: "local-image://c.png" }], index: 0 }]);
  });
});

describe("阅读岛页面：网页快照的目录与版面状态", () => {
  const states = (events: PageEvent[]) =>
    events.filter((event): event is Extract<PageEvent, { t: "snapshotState" }> => event.t === "snapshotState");

  function layout(frame: HTMLIFrameElement) {
    fromFrame(frame, "reader-layout", {
      rows: [
        { index: 0, top: 100, height: 20, text: "第一章", heading: true, font: 16 },
        { index: 1, top: 400, height: 20, text: "第二章", heading: true, font: 16 },
      ],
      font: 16,
      ready: false,
    });
  }

  /** 把窗口量成够宽：目录常驻窄轨（宿主据此隐藏原生工具栏的「目录」钮）。 */
  function makeWide() {
    hostWidth = 1000;
    frameWidth = 400;
    act(() => observers.forEach((notify) => notify()));
  }

  it("挂载后上报初始版面：窄窗、目录收起", () => {
    const { sent } = mount();
    expect(states(sent)[0]).toEqual({ t: "snapshotState", seq: 1, wideRail: false, open: false });
  });

  it("窗口变宽：上报 wideRail，目录窄轨出现", () => {
    const { sent, getByTestId } = mount();
    expect(screen.queryByTestId("snapshot-toc")).toBeNull();
    makeWide();
    expect(states(sent).at(-1)).toEqual({ t: "snapshotState", seq: 1, wideRail: true, open: false });
    expect(getByTestId("snapshot-toc")).toBeTruthy();
    expect(screen.getByRole("button", { name: "展开文章目录" }).getAttribute("aria-expanded")).toBe("false");
  });

  it("宿主的 ui 消息展开目录：目录浮层出现，不回声", () => {
    const { frame, post, sent } = mount();
    layout(frame);
    expect(screen.queryByRole("navigation")).toBeNull();
    const before = states(sent).length;
    post({ t: "ui", catalogOpen: false, tocOpen: true });
    expect(screen.getByRole("navigation", { name: "文章章节目录" })).toBeTruthy();
    expect(screen.getByText("第一章")).toBeTruthy();
    expect(screen.getByText("第二章")).toBeTruthy();
    expect(states(sent).length).toBe(before);
    post({ t: "ui", catalogOpen: false, tocOpen: false });
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(states(sent).length).toBe(before);
  });

  it("view 消息带的 tocOpen 是初始开合", () => {
    mount(snapshotView({}, { tocOpen: true }));
    expect(screen.getByRole("navigation", { name: "文章章节目录" })).toBeTruthy();
  });

  it("页面里点目录项：滚到章节、收起目录并上报开合", () => {
    const { frame, post, sent } = mount();
    layout(frame);
    post({ t: "ui", catalogOpen: false, tocOpen: true });
    fireEvent.click(screen.getByText("第二章"));
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 376 }); // 400 − 24
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(states(sent).at(-1)).toEqual({ t: "snapshotState", seq: 1, wideRail: false, open: false });
  });

  it("页面里点「返回顶部」：回到顶部、收起并上报", () => {
    const { post, sent } = mount();
    post({ t: "ui", catalogOpen: false, tocOpen: true });
    fireEvent.click(screen.getByText("返回顶部"));
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 });
    expect(states(sent).at(-1)).toMatchObject({ open: false });
  });

  it("窄轨模式下鼠标移入展开目录、上报开合与窄轨状态", () => {
    const { sent, getByTestId } = mount();
    makeWide();
    fireEvent.mouseEnter(getByTestId("snapshot-toc"));
    expect(states(sent).at(-1)).toEqual({ t: "snapshotState", seq: 1, wideRail: true, open: true });
    expect(screen.getByRole("navigation", { name: "文章章节目录" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "展开文章目录" }).getAttribute("aria-expanded")).toBe("true");
  });

  it("新的 view 序号重报版面状态（宿主会丢弃序号过期的事件）", () => {
    const { post, sent } = mount();
    makeWide();
    post({ t: "ui", catalogOpen: false, tocOpen: true });
    post(snapshotView({}, { seq: 4, tocOpen: true }));
    expect(states(sent).at(-1)).toEqual({ t: "snapshotState", seq: 4, wideRail: true, open: true });
  });

  it("目录窗口外按下指针：收起并上报", () => {
    const { post, sent } = mount();
    post({ t: "ui", catalogOpen: false, tocOpen: true });
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(states(sent).at(-1)).toMatchObject({ open: false });
  });
});

describe("阅读岛页面：网页快照的渲染完成回执", () => {
  it("iframe 加载完成才报 rendered；同一份文档的新序号直接确认；换了文档要等加载", () => {
    const { frame, post, events } = mount();
    expect(events("rendered")).toEqual([]);
    fireEvent.load(frame);
    expect(events("rendered")).toEqual([{ t: "rendered", seq: 1 }]);

    post(snapshotView({ assets: ["local-image://c.png"] }, { seq: 2 })); // 文档没变，不会重载
    expect(events("rendered").at(-1)).toEqual({ t: "rendered", seq: 2 });

    post(snapshotView({ document: `${DOC}<p>更新</p>` }, { seq: 3 })); // 文档变了，要等 iframe 重新加载
    expect(events("rendered").at(-1)).toEqual({ t: "rendered", seq: 2 });
    fireEvent.load(frame);
    expect(events("rendered").at(-1)).toEqual({ t: "rendered", seq: 3 });
  });

  it("加载完成时把当前查找词交给 iframe", () => {
    const { frame, post } = mount();
    post({ t: "find", query: "词", index: 1 });
    const spy = vi.spyOn(frame.contentWindow!, "postMessage");
    fireEvent.load(frame);
    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({ id: INSTANCE, type: "find", value: expect.objectContaining({ query: "词", index: 1 }) }),
      "*",
    );
  });
});

// ---- 本地图片：页面读出来内联成 data: 地址（不透明来源的沙箱 iframe 够不到岛的自有地址，见 snapshot-images.ts） ----

const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);
function imageResponse(type = "image/png", bytes: Uint8Array = PNG, ok = true) {
  return { ok, blob: async () => new Blob([bytes as BlobPart], { type }) } as Response;
}
function dataUrlOf(type: string, bytes: Uint8Array = PNG, index = 0) {
  return `data:${type};base64,${btoa(String.fromCharCode(...bytes))}#${index}`;
}
const IMAGE_DOC =
  '<html><body><img src="./media/local-image/a.png"><img src="./media/local-image/a.png"><img src="./media/local-image/b%20c.png"></body></html>';

describe("snapshot-images", () => {
  it("找出文档里的岛内本地图片地址：去重、保持出现顺序，名称到引号括号空白为止", () => {
    expect(localImageRefs(IMAGE_DOC)).toEqual(["./media/local-image/a.png", "./media/local-image/b%20c.png"]);
    expect(localImageRefs('<div style="background:url(./media/local-image/x.png)"></div>')).toEqual([
      "./media/local-image/x.png",
    ]);
    expect(localImageRefs("<p>没有图片</p><img src='https://example.com/a.png'>")).toEqual([]);
  });

  it("读到的图片内联成 data: 地址；清单原始写法按 data: 地址索引", async () => {
    const fetchImage = vi.fn(async () => imageResponse());
    const { doc, originals } = await inlineLocalImages(IMAGE_DOC, localImageRefs(IMAGE_DOC), { fetch: fetchImage });
    const a = dataUrlOf("image/png", PNG, 0);
    const b = dataUrlOf("image/png", PNG, 1); // 字节相同的两张图各有各的地址
    expect(doc).toBe(`<html><body><img src="${a}"><img src="${a}"><img src="${b}"></body></html>`);
    expect(fetchImage).toHaveBeenCalledTimes(2); // 同一个引用只读一次
    expect(originals.get(a)).toBe("local-image://a.png");
    expect(originals.get(b)).toBe("local-image://b c.png");
  });

  it("单张失败（状态码、非图片、网络错误）只让这一张保持原样", async () => {
    const doc = '<img src="./media/local-image/ok.png"><img src="./media/local-image/404.png"><img src="./media/local-image/page.png"><img src="./media/local-image/boom.png">';
    const fetchImage = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("404.png")) return imageResponse("image/png", PNG, false);
      if (url.endsWith("page.png")) return imageResponse("text/html");
      if (url.endsWith("boom.png")) throw new Error("network");
      return imageResponse();
    });
    const result = await inlineLocalImages(doc, localImageRefs(doc), { fetch: fetchImage as typeof fetch });
    expect(result.doc).toBe(
      `<img src="${dataUrlOf("image/png", PNG, 0)}"><img src="./media/local-image/404.png"><img src="./media/local-image/page.png"><img src="./media/local-image/boom.png">`,
    );
  });

  it("超出总字节预算的图片保持原样（不让一篇文章撑爆内存）", async () => {
    const doc = '<img src="./media/local-image/a.png"><img src="./media/local-image/b.png">';
    const fetchImage = vi.fn(async () => imageResponse());
    const result = await inlineLocalImages(doc, localImageRefs(doc), { fetch: fetchImage, maxBytes: PNG.length });
    const inlined = [...result.doc.matchAll(/data:image\/png/g)].length;
    expect(inlined).toBe(1);
    expect(result.doc).toContain("./media/local-image/");
  });

  it("被中止时抛出 AbortError，调用方丢弃", async () => {
    const abort = new AbortController();
    const fetchImage = vi.fn(async () => {
      abort.abort();
      return imageResponse();
    });
    await expect(
      inlineLocalImages(IMAGE_DOC, localImageRefs(IMAGE_DOC), { fetch: fetchImage, signal: abort.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("阅读岛页面：网页快照的本地图片", () => {
  it("没有本地图片的文档同步交给 iframe（行为不变）", () => {
    const { frame, events } = mount();
    expect(frame.getAttribute("srcdoc")).toBe(DOC);
    expect(frame.style.visibility).toBe("");
    act(() => void frame.dispatchEvent(new Event("load")));
    expect(events("rendered")).toEqual([{ t: "rendered", seq: 1 }]);
  });

  it("有本地图片的文档先读图：读完前 iframe 不可见、空白占位的加载不算渲染完成；读完交出内联后的文档", async () => {
    let release: (value: Response) => void = () => {};
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((resolve) => (release = resolve))));
    const doc = '<html><body><img src="./media/local-image/a.png"></body></html>';
    const { frame, events } = mount(snapshotView({ document: doc, assets: ["local-image://a.png"] }));
    expect(frame.getAttribute("srcdoc")).toBe("");
    expect(frame.style.visibility).toBe("hidden");
    act(() => void frame.dispatchEvent(new Event("load"))); // 空白占位的加载
    expect(events("rendered")).toEqual([]);

    await act(async () => release(imageResponse()));
    await waitFor(() => expect(frame.getAttribute("srcdoc")).toContain(dataUrlOf("image/png", PNG, 0)));
    expect(frame.style.visibility).toBe("");
    act(() => void frame.dispatchEvent(new Event("load")));
    expect(events("rendered")).toEqual([{ t: "rendered", seq: 1 }]);
  });

  it("点击内联后的图片：桥接脚本回报的是 data: 地址，换回清单里的写法找到序号", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => imageResponse()));
    const doc = '<html><body><img src="./media/local-image/b.png"></body></html>';
    const { frame, events } = mount(snapshotView({ document: doc }));
    await waitFor(() => expect(frame.getAttribute("srcdoc")).toContain("data:image/png"));
    fromFrame(frame, "image", dataUrlOf("image/png", PNG, 0));
    expect(events("lightbox")).toEqual([
      { t: "lightbox", seq: 1, images: [{ src: ASSETS[0] }, { src: ASSETS[1] }], index: 1 },
    ]);
  });

  it("读图失败的图片保持相对地址，点它仍按清单写法找到序号", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => imageResponse("image/png", PNG, false)));
    const doc = '<html><body><img src="./media/local-image/b.png"></body></html>';
    const { frame, events } = mount(snapshotView({ document: doc }));
    await waitFor(() => expect(frame.style.visibility).toBe(""));
    expect(frame.getAttribute("srcdoc")).toBe(doc);
    fromFrame(frame, "image", "./media/local-image/b.png");
    expect(events("lightbox")).toHaveLength(1);
  });
});