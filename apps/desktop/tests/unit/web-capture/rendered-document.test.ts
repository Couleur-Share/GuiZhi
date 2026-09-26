import { afterEach, describe, expect, it } from "vitest";
import { captureRenderedDocument } from "../../../src/main/services/web-capture/web-rendered-document";

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
