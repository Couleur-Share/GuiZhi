import { describe, it, expect, vi } from "vitest";
vi.mock("virtual:reading-libraries", () => ({
  default: { components: {}, runtime: {} },
}));
import {
  ReadingRecordStream,
  readingPool,
} from "../../src/main/services/themed-reading/v3-stream";
import {
  normalizeV3Section,
  checkReadingScript,
  validateV3Page,
  v3InnerDocument,
  v3WrapperDocument,
} from "../../src/main/services/themed-reading/v3-document";
import { themeTestPage } from "./themed-reading-test-fixtures";

describe("v3 完整记录与恢复", () => {
  const records = [
    { type: "meta", css: "" },
    { type: "section", id: "intro", html: '<h1>转义"与\n换行</h1>' },
    { type: "done" },
  ];
  it("任意分块位置均保留转义文本", () => {
    const raw = records.map((r) => JSON.stringify(r)).join("\n");
    for (let cut = 0; cut < raw.length; cut++) {
      const got = [];
      const p = new ReadingRecordStream((r) => got.push(r));
      p.push(raw.slice(0, cut));
      p.push(raw.slice(cut));
      p.finish();
      expect(got).toEqual(records);
    }
  });
  it("截断和缺少完成标记不可成功", () => {
    const got = [];
    const p = new ReadingRecordStream((r) => got.push(r));
    p.push(
      JSON.stringify(records[0]) +
        "\n" +
        JSON.stringify(records[1]) +
        '\n{"type":',
    );
    expect(() => p.finish()).toThrow();
    expect(got).toHaveLength(2);
  });
  it.each([
    [records[1]],
    [records[0], records[0]],
    [records[0], records[1], records[1]],
    [records[0], records[2], records[1]],
  ])("拒绝重复和乱序 %j", (...input) => {
    const p = new ReadingRecordStream(() => {});
    expect(() =>
      p.push(input.map((r) => JSON.stringify(r)).join("\n") + "\n"),
    ).toThrow();
  });
  it("首次失败后停止派发并等待在途结果", async () => {
    const started = [],
      finished = [];
    await expect(
      readingPool([0, 1, 2, 3], 2, async (n) => {
        started.push(n);
        await new Promise((r) => setTimeout(r, n ? 10 : 1));
        if (!n) throw new Error("429");
        finished.push(n);
      }),
    ).rejects.toThrow("429");
    expect(started).toEqual([0, 1]);
    expect(finished).toEqual([1]);
  });
});
describe("v3 规范化及静态边界", () => {
  it("按钮、完整外壳与内嵌样式可规范化", () => {
    const n = normalizeV3Section(
      '<!doctype html><html><head><style>button{color:red}</style></head><body><button id="b">切换</button><script>document.querySelector("button").addEventListener("click",()=>{});</script></body></html>',
      "unit",
      true,
    );
    expect(n.html).toContain("button");
    expect(n.css).toContain("color:red");
    expect(n.scripts).toHaveLength(1);
    expect(n.html).not.toContain("script");
  });
  it.each([
    '<script src="https://bad.test/x"></script>',
    '<iframe src="https://bad.test"></iframe>',
    '<button onclick="go()">go</button>',
    '<img src="file:///private">',
  ])("具体拒绝无效输入 %s", (html) =>
    expect(() => normalizeV3Section(html, "unit", true)).toThrow(),
  );
  it("语法解析不会执行模型代码", () => {
    expect(
      checkReadingScript({ id: "x", code: "while(true){}", status: "ready" }),
    ).toBeUndefined();
    expect(
      checkReadingScript({ id: "x", code: "const =", status: "ready" })?.unit,
    ).toBe("x");
  });
  it("历史校验状态不能绕过语法检查", () => {
    const page = themeTestPage({
      formatVersion: 3,
      options: {
        style: "",
        maxImages: 0,
        generateImages: false,
        enhancedInteraction: true,
      },
      assets: [],
    });
    page.design = {
      html: `<h1>标题</h1><h2>正文</h2><p>${"完整正文".repeat(30)}</p>`,
      css: "",
      direction: "",
      assets: [],
      scripts: [{ id: "x", code: "const =", status: "ready" }],
    };
    expect(() => validateV3Page(page)).toThrow("语法");
    page.design.scripts[0].code = "document.body.dataset.test='yes'";
    const inner = v3InnerDocument(page);
    expect(inner).toContain("connect-src &#39;none&#39;");
    expect(v3WrapperDocument(inner)).toContain('sandbox="allow-scripts"');
    expect(v3InnerDocument(page, {}, false)).not.toContain("dataset.test");
  });
});
