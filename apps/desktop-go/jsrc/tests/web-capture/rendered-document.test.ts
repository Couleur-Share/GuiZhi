// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { captureRenderedDocument } from "../../services/web-capture/web-rendered-document";

afterEach(() => {
  document.body.innerHTML = "";
});
function fixture(text: string, modern = false) {
  document.body.innerHTML =
    '<article><h1>正文</h1><div class="cm-editor"><div class="cm-gutters">1 2</div><div class="cm-content">可视的两行</div></div></article>';
  const editor = document.querySelector<HTMLElement>(".cm-editor")!;
  const content = document.querySelector<HTMLElement>(".cm-content")!;
  const view = {
    dom: editor,
    state: { doc: { length: text.length, toString: () => text } },
  };
  Object.assign(
    content,
    modern ? { cmTile: { root: { view } } } : { cmView: { view } },
  );
  return view;
}
describe("虚拟代码编辑器快照", () => {
  it.each([false, true])(
    "从文档模型保存全部行、缩进和 HTML 字符；不改变原 DOM：%s",
    (modern) => {
      const text =
        Array.from({ length: 100 }, (_, i) => `\t  <value n="${i}">`).join(
          "\n",
        ) + "\n\n";
      fixture(text, modern);
      const snapshot = captureRenderedDocument();
      const copy = new DOMParser().parseFromString(snapshot.html, "text/html");
      expect(snapshot.restoredEditors).toBe(1);
      expect(copy.querySelector("pre code")?.textContent).toBe(text);
      expect(copy.querySelector(".cm-gutters")).toBeNull();
      expect(document.querySelector(".cm-content")?.textContent).toBe(
        "可视的两行",
      );
    },
  );
  it("不能读取完整模型时不把可见片段当成成功", () => {
    fixture("完整代码");
    delete (
      document.querySelector(".cm-content") as HTMLElement & {
        cmView?: unknown;
      }
    ).cmView;
    expect(captureRenderedDocument()).toMatchObject({
      html: "",
      error: expect.any(String),
    });
  });
  it("模型不匹配或超过预算时拒绝", () => {
    const view = fixture("完整代码");
    view.state.doc.length = 1024 * 1024 + 1;
    expect(captureRenderedDocument().error).toBeTruthy();
    view.state.doc.length = 5;
    expect(captureRenderedDocument().error).toBeTruthy();
  });
  it("普通 pre 不受影响", () => {
    document.body.innerHTML =
      "<pre><code>  print(&quot;归知&quot;)\n</code></pre>";
    expect(captureRenderedDocument()).toEqual({
      html: document.documentElement.outerHTML,
      restoredEditors: 0,
    });
  });
});

/** 在 host 上挂 open shadow root 并写入内容。 */
function attach(host: Element, html: string, mode: "open" | "closed" = "open") {
  const shadow = host.attachShadow({ mode });
  shadow.innerHTML = html;
  return shadow;
}
const parse = (html: string) => new DOMParser().parseFromString(html, "text/html");
const bodyText = (html: string) =>
  parse(html).body.textContent?.replace(/\s+/g, " ").trim();

describe("shadow DOM 快照", () => {
  it("升级后移入 shadow DOM 的正文被展开进快照；页面自身的 DOM 不变", () => {
    document.body.innerHTML =
      "<article><h1>标题</h1><code-box></code-box></article>";
    const host = document.querySelector("code-box")!;
    attach(host, "<span>html</span><pre><code>const a = 1;</code></pre>");
    const snapshot = captureRenderedDocument();
    const copy = parse(snapshot.html);
    expect(copy.querySelector("code-box span")?.textContent).toBe("html");
    expect(copy.querySelector("code-box pre code")?.textContent).toBe(
      "const a = 1;",
    );
    expect(snapshot.flattenedShadowRoots).toBe(1);
    expect(host.children).toHaveLength(0);
    expect(host.shadowRoot?.querySelector("pre")).not.toBeNull();
  });

  it("默认插槽：light 子节点按插槽位置出现一次，不重复", () => {
    document.body.innerHTML =
      "<fancy-card><p>正文一段</p></fancy-card>";
    attach(
      document.querySelector("fancy-card")!,
      "<header>卡片</header><div class='body'><slot></slot></div>",
    );
    const copy = parse(captureRenderedDocument().html);
    expect(copy.querySelectorAll("fancy-card p")).toHaveLength(1);
    expect(copy.querySelector("fancy-card .body p")?.textContent).toBe(
      "正文一段",
    );
    expect(copy.querySelector("fancy-card header")?.textContent).toBe("卡片");
  });

  it("具名插槽：只展开被渲染的节点，shadow 里没有对应插槽的 light 节点不进入快照", () => {
    document.body.innerHTML =
      '<tab-set><h2 slot="title">标题</h2><p slot="orphan">不会被渲染</p></tab-set>';
    attach(
      document.querySelector("tab-set")!,
      '<nav><slot name="title"></slot></nav>',
    );
    const text = bodyText(captureRenderedDocument().html);
    expect(text).toContain("标题");
    expect(text).not.toContain("不会被渲染");
  });

  it("没有分配节点的插槽展开其后备内容", () => {
    document.body.innerHTML = "<empty-state></empty-state>";
    attach(
      document.querySelector("empty-state")!,
      "<slot><em>暂无数据</em></slot>",
    );
    expect(parse(captureRenderedDocument().html).querySelector("empty-state em")?.textContent).toBe(
      "暂无数据",
    );
  });

  it("嵌套宿主逐层展开，被插槽渲染的宿主也展开", () => {
    document.body.innerHTML =
      "<outer-box><inner-box id='slotted'></inner-box></outer-box>";
    const outer = document.querySelector("outer-box")!;
    const slotted = document.querySelector("#slotted")!;
    attach(slotted, "<b>被插槽渲染的内部</b>");
    const shadow = attach(outer, "<section><slot></slot><deep-box></deep-box></section>");
    attach(shadow.querySelector("deep-box")!, "<i>shadow 里的 shadow</i>");
    const snapshot = captureRenderedDocument();
    const copy = parse(snapshot.html);
    expect(copy.querySelector("outer-box section inner-box b")?.textContent).toBe(
      "被插槽渲染的内部",
    );
    expect(copy.querySelector("outer-box section deep-box i")?.textContent).toBe(
      "shadow 里的 shadow",
    );
    // 只统计文档里能直接遍历到的顶层宿主：outer-box 展开了一次（slotted 在其内部随之展开）。
    expect(snapshot.flattenedShadowRoots).toBe(1);
  });

  it("closed shadow root 读不到，保留原来的 light DOM", () => {
    document.body.innerHTML = "<secret-box><p>light 内容</p></secret-box>";
    attach(document.querySelector("secret-box")!, "<p>读不到</p>", "closed");
    const snapshot = captureRenderedDocument();
    expect(bodyText(snapshot.html)).toBe("light 内容");
    expect(snapshot.flattenedShadowRoots).toBeUndefined();
  });

  it("与代码编辑器同页：两者都被处理，对应关系不受编辑器替换影响", () => {
    fixture("function demo() {}\n");
    const article = document.querySelector("article")!;
    const host = document.createElement("note-box");
    article.append(host);
    attach(host, "<p>编辑器之后的 shadow 内容</p>");
    const snapshot = captureRenderedDocument();
    const copy = parse(snapshot.html);
    expect(snapshot.restoredEditors).toBe(1);
    expect(copy.querySelector("pre code")?.textContent).toBe("function demo() {}\n");
    expect(copy.querySelector("note-box p")?.textContent).toBe(
      "编辑器之后的 shadow 内容",
    );
  });

  it("位于代码编辑器内部的宿主随编辑器一并被替换，不报错", () => {
    fixture("code");
    const host = document.createElement("inner-host");
    document.querySelector(".cm-gutters")!.append(host);
    attach(host, "<p>编辑器内部</p>");
    const snapshot = captureRenderedDocument();
    expect(snapshot.error).toBeUndefined();
    expect(snapshot.restoredEditors).toBe(1);
    expect(bodyText(snapshot.html)).not.toContain("编辑器内部");
    // 副本已随编辑器被替换，不应计入展开数。
    expect(snapshot.flattenedShadowRoots).toBeUndefined();
  });

  it("某个宿主读取失败时保留它的原样，其余宿主照常展开", () => {
    document.body.innerHTML = "<bad-box><p>原样保留</p></bad-box><good-box></good-box>";
    const bad = attach(document.querySelector("bad-box")!, "<slot></slot>");
    bad.querySelector("slot")!.assignedNodes = () => {
      throw new Error("读取失败");
    };
    attach(document.querySelector("good-box")!, "<p>正常展开</p>");
    const snapshot = captureRenderedDocument();
    const copy = parse(snapshot.html);
    expect(copy.querySelector("bad-box p")?.textContent).toBe("原样保留");
    expect(copy.querySelector("good-box p")?.textContent).toBe("正常展开");
    expect(snapshot.flattenedShadowRoots).toBe(1);
  });

  it("展开不会触发页面自定义元素的构造函数", () => {
    const constructed = vi.fn();
    class Probe extends HTMLElement {
      constructor() {
        super();
        constructed();
      }
    }
    customElements.define("probe-box", Probe);
    document.body.innerHTML = "<probe-box></probe-box>";
    attach(document.querySelector("probe-box")!, "<probe-box>嵌套同名元素</probe-box>");
    const before = constructed.mock.calls.length;
    const snapshot = captureRenderedDocument();
    // cloneNode(true) 复制整棵文档时会实例化一次（既有行为）；shadow 内的同名元素不应再实例化。
    expect(constructed.mock.calls.length - before).toBeLessThanOrEqual(1);
    expect(bodyText(snapshot.html)).toContain("嵌套同名元素");
  });

  it("函数文本可独立运行：不引用外部作用域（executeJavaScript 会把它序列化）", () => {
    document.body.innerHTML = "<x-host></x-host>";
    attach(document.querySelector("x-host")!, "<p>序列化后仍然可用</p>");
    const run = new Function(`return (${captureRenderedDocument.toString()})()`);
    const snapshot = run() as ReturnType<typeof captureRenderedDocument>;
    expect(parse(snapshot.html).querySelector("x-host p")?.textContent).toBe(
      "序列化后仍然可用",
    );
    expect(snapshot.flattenedShadowRoots).toBe(1);
  });
});
