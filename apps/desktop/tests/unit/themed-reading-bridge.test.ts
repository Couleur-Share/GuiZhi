// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { JSDOM } from "jsdom";
import { THEMED_READING_BRIDGE } from "../../src/main/services/themed-reading/theme-bridge";

const opened: JSDOM[] = [];
afterEach(() => { for (const dom of opened.splice(0)) dom.window.close(); });

function reader() {
  const host = new JSDOM("");
  const page = new JSDOM('<html data-instance="reader-1"><body><details><summary>展开</summary><p>隐藏的限定条件</p></details></body></html>', {
    runScripts: "outside-only",
    beforeParse(window) {
      Object.defineProperty(window, "parent", { value: host.window });
      Object.defineProperty(window, "ResizeObserver", { value: class { observe() { /* 布局由浏览器测试覆盖 */ } } });
      Object.defineProperty(window, "CSS", { value: { highlights: new Map() } });
      Object.defineProperty(window, "Highlight", { value: class {} });
    },
  });
  opened.push(page, host);
  page.window.eval(THEMED_READING_BRIDGE);
  const send = (type: string, value: unknown, validSource = true, id = "reader-1") => page.window.dispatchEvent(new page.window.MessageEvent("message", {
    source: (validSource ? host.window : page.window) as unknown as Window,
    origin: "null", data: { id, type, value },
  }));
  return { page, host, send };
}

describe("主题页固定可信桥接", () => {
  it("允许加载后重新获取高度，拒绝其他窗口或错误实例的刷新", () => {
    const { page, host, send } = reader();
    const posted = vi.spyOn(host.window, "postMessage");
    vi.spyOn(page.window.document.documentElement, "getBoundingClientRect").mockReturnValue({ height: 1536.4 } as DOMRect);
    send("reader-refresh", null, false);
    send("reader-refresh", null, true, "other");
    expect(posted).not.toHaveBeenCalled();
    send("reader-refresh", null);
    expect(posted).toHaveBeenCalledWith({ id: "reader-1", type: "height", value: 1537 }, "*");
  });
  it("只接受正确父窗口和实例的外观消息，并验证字号与字体值", () => {
    const { page, send } = reader();
    const root = page.window.document.documentElement;
    send("appearance", { theme: "dark", fontSize: 20 }, false);
    expect(root.dataset.theme).toBeUndefined();
    send("appearance", { theme: "dark", fontSize: 20 }, true, "other");
    expect(root.dataset.theme).toBeUndefined();
    send("appearance", { theme: "dark", fontSize: 20, fontFamily: "system-ui, sans-serif" });
    expect(root.dataset.theme).toBe("dark");
    expect(root.style.getPropertyValue("--reader-font-size")).toBe("20px");
    expect(root.style.getPropertyValue("--reader-font-family")).toBe("system-ui, sans-serif");
    send("appearance", { theme: "light", fontSize: 0, fontFamily: "x;display:none" });
    expect(root.dataset.theme).toBe("light");
    expect(root.style.getPropertyValue("--reader-font-size")).toBe("20px");
    expect(root.style.getPropertyValue("--reader-font-family")).toBe("system-ui, sans-serif");
  });

  it("有效查找展开折叠正文，其他窗口的查找不能操作页面", () => {
    const { page, send } = reader();
    const details = page.window.document.querySelector("details");
    const query = { query: "限定条件", index: 0, request: 1 };
    send("find", query, false);
    expect(details.open).toBe(false);
    send("find", query);
    expect(details.open).toBe(true);
  });

  it("目录激活标识当前章节，并在阅读器计算位置前展开目标折叠区", () => {
    const { page } = reader();
    const document = page.window.document;
    document.body.insertAdjacentHTML("afterbegin", '<nav><a data-reader-toc-link href="#first">第一节</a><a data-reader-toc-link href="#second">第二节</a></nav>');
    const details = document.querySelector("details");
    details.id = "first";
    details.querySelector("p").id = "second";
    const links = [...document.querySelectorAll<HTMLAnchorElement>("nav a")];
    links[0].click();
    expect(details.open).toBe(true);
    expect(links[0].getAttribute("aria-current")).toBe("location");
    links[1].click();
    expect(links[0].hasAttribute("aria-current")).toBe(false);
    expect(links[1].getAttribute("aria-current")).toBe("location");
    expect(document.querySelectorAll("[data-reader-target]")).toHaveLength(1);
    expect(document.querySelector("[data-reader-target]").id).toBe("second");
  });
});
