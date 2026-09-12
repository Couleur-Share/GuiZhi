import { describe, expect, it } from "vitest";
import { parseHTML } from "linkedom";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { cleanReconstructionCss, cleanReconstructionHtml } from "../../src/main/services/themed-reading/reconstruction-document";
import { readingSvgFixture } from "./reading-svg-fixture";

describe("阅读页 SVG 图解", () => {
  it.each([undefined, "isolated-reader"])("内嵌与导出保留渐变、箭头、变换、文字及无障碍说明 %s", instance => {
    const html = themedReadingDocument(readingSvgFixture(), instance);
    const doc = parseHTML(html).document;
    expect(doc.querySelectorAll("svg")).toHaveLength(3);
    // linkedom 将标签名序列化为小写，HTML 浏览器会恢复 SVG 命名空间大小写。
    expect(html).toMatch(/<linearGradient id="flow-wash"/i);
    expect(html).toMatch(/<radialGradient id="relation-wash"/i);
    expect(html).toMatch(/<clipPath id="compare-clip"/i);
    expect(html).toContain('viewBox="0 0 320 300"');
    expect(html).toContain('markerWidth="7"');
    expect(html).toContain('transform="translate(20 10)"');
    expect(html).toContain('marker-end="url(#flow-arrow)"');
    expect(html).toContain('fill="var(--theme-text)"');
    expect(html).toContain('aria-describedby="flow-desc"');
    expect(doc.getElementById("flow-title").textContent).toBe("记录、理解与联系的阅读流程");
    expect(doc.querySelectorAll("tspan")).toHaveLength(4);
    expect(html).not.toContain("https://");
  });
  it.each(["https://evil.test/paint.svg#x", "//evil.test/a", "data:image/svg+xml,x", "javascript:alert(1)", "#x) url(https://evil.test)"])("拒绝 SVG 外部或混合绘图引用 %s", value => {
    expect(() => cleanReconstructionHtml(`<svg><path fill="url(${value})"/></svg>`, () => undefined)).toThrow();
    expect(() => cleanReconstructionCss(`path{fill:url(${value})}`)).toThrow();
  });
  it.each(["background", "background-image", "cursor", "--paint"])("CSS 片段仅限绘图属性，拒绝 %s", property => {
    expect(() => cleanReconstructionCss(`svg{${property}:url(#flow-wash)}`)).toThrow();
  });
  it("支持 CSS 引号片段以及内联样式，并验证其目标", () => {
    const page = readingSvgFixture();
    page.design.html = page.design.html.replace('fill="url(#flow-wash)"', 'style="fill:url(\'#flow-wash\')"');
    expect(themedReadingDocument(page)).toContain("fill:url(#flow-wash)");
    page.design.html = page.design.html.replace("url('#flow-wash')", "url('#missing')");
    expect(() => themedReadingDocument(page)).toThrow(/SVG.*不存在/);
  });
  it.each(["svg path{fill:url(#missing)}", "svg path{fill:url(#flow-arrow)}"])("CSS 定义缺失或类型错误时触发修复 %s", css => {
    const page = readingSvgFixture(); page.design.css += css;
    expect(() => themedReadingDocument(page)).toThrow(/SVG.*不存在或类型不匹配/);
  });
  it("属性引用缺失、重复 ID 与缺失说明不会静默生成残缺图解", () => {
    const page = readingSvgFixture();
    page.design.html = page.design.html.replace('id="flow-arrow"', 'id="other-arrow"');
    expect(() => themedReadingDocument(page)).toThrow(/SVG/);
    page.design.html = readingSvgFixture().design.html.replace('id="flow-desc"', 'id="flow-title"');
    expect(() => themedReadingDocument(page)).toThrow(/ID/);
    page.design.html = readingSvgFixture().design.html.replace('aria-describedby="flow-desc"', 'aria-describedby="missing"');
    expect(() => themedReadingDocument(page)).toThrow(/文字说明/);
  });
  it("不保留外部图片、复用节点、foreignObject、动画、脚本或事件", () => {
    const html = cleanReconstructionHtml('<svg onload="alert(1)"><image href="https://evil.test"/><use href="https://evil.test/#x"/><foreignObject><iframe src="https://evil.test"></iframe></foreignObject><animate attributeName="href" to="https://evil.test"/><script>alert(1)</script></svg>', () => undefined);
    expect(html).not.toMatch(/image|use|foreignObject|iframe|animate|script|onload|evil/);
  });
  it("兼容旧页面小写 SVG 属性与 HTML 重序列化后的标签", () => {
    const page = readingSvgFixture();
    page.design.html = parseHTML(page.design.html).document.toString().replace('viewBox="0 0 320 300"', 'viewbox="0 0 320 300"');
    const html = themedReadingDocument(page);
    expect(html).toContain('viewBox="0 0 320 300"');
    expect(html).toMatch(/<lineargradient id="flow-wash"/i);
  });
  it("保留 HTML 大小写不敏感语义，属性规范化不绕过资源清理", () => {
    const html = cleanReconstructionHtml('<DIV CLASS="note"><H2>标题</H2><SVG VIEWBOX="0 0 10 10"><RECT WIDTH="10" HEIGHT="10" FILL="red"/></SVG></DIV>', () => undefined);
    expect(html).toContain('<div class="note"><h2>标题</h2><svg viewBox="0 0 10 10">');
    expect(() => cleanReconstructionHtml('<SVG><PATH FILL="url(https://evil.test)"/></SVG>', () => undefined)).toThrow();
    expect(() => cleanReconstructionHtml('<DIV STYLE="background:url(https://evil.test)"></DIV>', () => undefined)).toThrow();
  });
  it.each(['<use href="#flow-wash"/>', '<filter id="blur"/>', '<foreignObject>内容</foreignObject>'])("不支持的图形元素触发修复 %s", extra => {
    const page = readingSvgFixture(); page.design.html = page.design.html.replace('</svg>', `${extra}</svg>`);
    expect(() => themedReadingDocument(page)).toThrow(/SVG 图解含不支持/);
  });
  it.each(["0 0 0 100", "0 0 -10 100", "0 0 NaN 100", "0 0 10"])("无效 viewBox 触发修复 %s", box => {
    const page = readingSvgFixture(); page.design.html = page.design.html.replace('viewBox="0 0 320 300"', `viewBox="${box}"`);
    expect(() => themedReadingDocument(page)).toThrow(/viewBox 无效/);
  });
});
