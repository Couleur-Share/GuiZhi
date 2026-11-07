import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { runInNewContext } from "node:vm";
import { READING_EXPRESSION_SCRIPT } from "@guizhi/shared/utils/reading-expression-runtime";
import { describe, it, expect } from "vitest";
import { parseHTML } from "linkedom";
import { evaluateReadingExpression } from "@guizhi/shared/utils/reading-expression";
import { readingSourceText, replaceReadingSource } from "@guizhi/shared/utils/reading-source";
import { validateThemedReadingVersion } from "@guizhi/db/themed-reading-validation";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";

describe("旧格式重构阅读页的阅读与导出", () => {
  it("未知格式明确拒绝，离线公式运行时与主进程解析器一致", () => {
    const page = reconstructionFixture(); (page as any).formatVersion = 99;
    expect(() => validateThemedReadingVersion(page)).toThrow();
    for (const expression of ["1+2*3", "min(a,3)+max(2,4)", "-(a+3)/2", "1/0", "process.exit()", "x", "2**3"]) {
      const execute = (fn: () => number) => {try {return fn();} catch {return "error";}};
      expect(execute(() => runInNewContext(`${READING_EXPRESSION_SCRIPT};evaluateReadingExpression(expression,{a:10})`,{expression}))).toBe(execute(() => evaluateReadingExpression(expression,{a:10})));
    }
  });
  it("图片文案编辑保留元数据、画廊与识别段落", () => {
    const item = {itemType:"image" as const,content:"> 平台：图片 · 作者：测试\n\n旧文案\n\n![原图](local-image://a.png)\n\n## 图中文字\n\n识别结果"};
    const result = replaceReadingSource(item,"body","新文案");
    expect(readingSourceText(item,"body")).toBe("旧文案");
    expect(result).toContain("> 平台：图片 · 作者：测试"); expect(result).toContain("![原图](local-image://a.png)"); expect(result).toContain("识别结果");
    expect(readingSourceText({...item,content:result},"body")).toBe("新文案");
  });
  it("允许重写和扩写，不输出原文核对与源码附录，导出不携带桥接", () => {
    const page = reconstructionFixture(); validateThemedReadingVersion(page);
    const html = themedReadingDocument(page), doc = parseHTML(html).document;
    expect(doc.querySelector("h1").textContent).toBe("重新拟定的标题");
    expect(doc.body.textContent).not.toContain("仅原文含有的私有文字");
    expect(html).not.toContain("核对原文"); expect(html).not.toContain("data-instance=");
    expect(doc.querySelectorAll("input")).toHaveLength(2);
    const script = [...doc.querySelectorAll("script")].find(s => !s.hasAttribute("type"));
    // 检测打包函数引用了外部帮助变量的风险。
    expect(script.textContent).not.toMatch(/\beval\s*\(|new Function/);
  });
  it.each(["<script>alert(1)</script>", '<img src=x onerror="alert(1)">', "<iframe></iframe>"])("拒绝模型可执行内容 %s", extra => { const p = reconstructionFixture(); p.design.html += extra; expect(() => themedReadingDocument(p)).toThrow(); });
  it.each(["@import 'https://evil.test/x';", "p{background:url(https://evil.test)}", "body{position:fixed}"])("拒绝加载和覆盖样式 %s", css => { const p = reconstructionFixture(); p.design.css = css; expect(() => themedReadingDocument(p)).toThrow(); });
  it("伪造资料 ID 在读库时被拒绝", () => { const p = reconstructionFixture(); p.reconstruction.draft[0].referenceIds = ["fake"]; expect(() => validateThemedReadingVersion(p)).toThrow(); });
  it("资料仅在导出末尾折叠展示，不带内部正文", () => {
    const p = reconstructionFixture(); p.reconstruction.references = [{ id: "R1", title: "官方资料", url: "https://example.org/ref", capturedAt: 1, text: "不会导出的采集全文", status: "ready" }]; p.reconstruction.draft[0].referenceIds = ["R1"];
    expect(themedReadingDocument(p)).toContain("<summary>参考资料</summary>");
    expect(themedReadingDocument(p)).not.toContain("不会导出的采集全文");
    expect(themedReadingDocument(p, "host")).not.toContain("<summary>参考资料</summary>");
  });
  it("安全公式支持变量与函数，拒绝代码和无效结果", () => {
    expect(evaluateReadingExpression("max(a/2, min(b,3)) + 1", { a: 10, b: 2 })).toBe(6);
    for (const expr of ["globalThis.process", "alert(1)", "a.constructor", "1/0", "x", "1;2"]) expect(() => evaluateReadingExpression(expr, {})).toThrow();
    expect(evaluateReadingExpression("200 / 90", {})).toBeCloseTo(2.2222);
  });
  it("论坛分段编辑保留元数据与其他正文，CRLF 不丢失", () => {
    const item = { itemType: "forum" as const, content: "> 平台：论坛\r\n\r\n## 讨论总结\r\n\r\n旧总结\r\n\r\n## 正文\r\n\r\n主楼\r\n\r\n## 讨论（1 条）\r\n\r\n回复内容" };
    const content = replaceReadingSource(item, "summary", "新总结");
    expect(content).toContain("> 平台：论坛\r\n"); expect(content).toContain("## 正文\r\n\r\n主楼"); expect(content).toContain("回复内容");
    expect(readingSourceText({ ...item, content }, "summary")).toBe("新总结");
    expect(readingSourceText({ ...item, content: replaceReadingSource(item, "body", "新主楼") }, "summary")).toBe("旧总结");
  });
});
