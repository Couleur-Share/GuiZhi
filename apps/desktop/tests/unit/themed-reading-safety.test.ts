// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import type { ThemedReadingAsset, ThemedReadingDesign, ThemedReadingSource, ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { cleanThemedCss, themedColorContrast, THEMED_READING_CONTENT_GUARD_CSS } from "../../src/main/services/themed-reading/theme-css";
import { cleanThemedHtml, themedAssetMapper, validateThemedDesign } from "../../src/main/services/themed-reading/sanitize";
import { themedReadingDocument, THEMED_READING_BRIDGE_HASH } from "../../src/main/services/themed-reading/document";

const source: ThemedReadingSource = {
  title: "啤酒知识", content: "# 生啤\n\n完整正文与限定条件", sourceUri: null, fingerprint: "fingerprint",
  blocks: [{ id: "b0", markdown: "# 生啤", html: "<h1>生啤</h1>", text: "生啤" }, { id: "b1", markdown: "完整正文与限定条件", html: "<p>完整正文与限定条件 <a href='https://example.com'>原文链接</a></p>", text: "完整正文与限定条件" }],
};
const asset: ThemedReadingAsset = { id: "hero", role: "generated", purpose: "头图", prompt: "麦穗", alt: "啤酒主题插画", aspectRatio: "16:9", status: "ready", fileName: "theme-hero.png" };
const design = (html = '<div data-source-block="b0"></div><div data-source-block="b1"></div>'): ThemedReadingDesign => ({ direction: "暖色杂志", html, css: ".layout{display:grid;grid-template-columns:1fr 1fr;gap:24px}", assets: [] });
const page = (override: Partial<ThemedReadingVersion> = {}): ThemedReadingVersion => ({
  id: "version", itemId: "item", sourceKind: "body", role: "current", formatVersion: 1,
  source, design: design(), assets: [asset], options: { style: "", generateImages: true, maxImages: 3 }, warnings: [], createdAt: 1, updatedAt: 1, ...override,
});

describe("主题页设计与安全边界", () => {
  it.each([
    '<div data-source-block="b0"></div>',
    '<div data-source-block="b0"></div><div data-source-block="b0"></div>',
    '<div data-source-block="b1"></div><div data-source-block="b0"></div>',
    '<div data-source-block="b0"><div data-source-block="b1"></div></div>',
    '<div data-source-block="b0">擅自改写</div><div data-source-block="b1"></div>',
    '<svg><div data-source-block="b0"></div></svg><div data-source-block="b1"></div>',
  ])("拒绝遗漏、重复、重排和非法嵌套正文: %s", (html) => expect(() => validateThemedDesign(design(html), source, [])).toThrow());

  it("拒绝正文外新增事实，仅接受已知导航标签与原文标题", () => {
    expect(() => validateThemedDesign(design("<h1>啤酒知识</h1><nav>目录</nav>" + design().html), source, [])).not.toThrow();
    expect(() => validateThemedDesign(design("<p>喝酒能够治疗疾病</p>" + design().html), source, [])).toThrow("新增了正文文字");
    expect(() => validateThemedDesign(design('<img data-theme-asset="unknown">' + design().html), source, [])).toThrow("未声明");
    expect(() => validateThemedDesign(design('<nav><a href="https://evil.example">原文</a></nav>' + design().html), source, [])).toThrow("新增外部链接");
    expect(() => validateThemedDesign(design('<img data-theme-asset="hero">' + design().html), source, [{ ...asset, status: "pending" }])).not.toThrow();
  });

  it("保留响应式、主题变量、Flex/Grid，剥离联网及隐藏正文能力", () => {
    const safe = cleanThemedCss(`@import url(https://evil.example/style);@font-face{src:url(https://evil.example/font)}
      @media(max-width:640px){.layout{display:grid;grid-template-columns:1fr;gap:16px}}
      :root{--theme-accent:#b8862c}.x{color:var(--theme-accent);display:none;opacity:0;position:fixed;transform:translateX(-9999px);font-size:0;overflow:hidden;order:9;content:'fake';background:url(local-image://secret.png);--reader-font-size:0}
      .x::before{content:'fake'}.x:hover{color:transparent}`);
    expect(safe).toContain("@media");
    expect(safe).toContain("grid-template-columns:minmax(0,1fr)");
    expect(safe).toContain("var(--theme-accent)");
    for (const forbidden of ["@import", "@font-face", "url(", "display:none", "opacity", "position", "transform", "font-size:0", "overflow", "order", "content:", "--reader-font-size:0", ":hover", "::before"]) expect(safe).not.toContain(forbidden);
  });

  it("目录交互保留边界和焦点提示，不能在悬停或锚点状态隐藏正文", () => {
    const safe = cleanThemedCss(`nav a:hover{border:1px solid currentColor;cursor:pointer;text-decoration:underline;color:transparent;background:#000;display:none}
      nav a:focus-visible{outline:2px solid currentColor;outline-offset:3px;font-size:0}
      nav:focus-within{box-shadow:0 0 8px #0002;--theme-text:#fff;--theme-surface:#fff}
      section:target{border-left:3px solid #b8862c;margin-left:64px;display:grid}
      .bad:focus-visible{outline:999px solid #fff;outline-offset:999rem;cursor:none}
      .fake::before{border:1px solid #fff}`);
    expect(safe).toContain("nav a:hover{border:1px solid currentColor;cursor:pointer;text-decoration:underline}");
    expect(safe).toContain("nav a:focus-visible{outline:2px solid currentColor;outline-offset:3px}");
    expect(safe).toContain("nav:focus-within{box-shadow:0 0 8px #0002}");
    expect(safe).toContain("section:target{border-left:3px solid #b8862c}");
    for (const forbidden of ["transparent", "background", "display", "font-size", "--theme", "margin-left", "999", "cursor:none", "::before"]) expect(safe).not.toContain(forbidden);
    expect(cleanThemedCss(safe)).toBe(safe);
  });

  it("HTML/SVG 清理不能越过图片清单，也不执行脚本或表单", () => {
    const safe = cleanThemedHtml(`<script>alert(1)</script><form><input></form><iframe src="https://evil.example"></iframe><img src="local-image://private.png" onerror="alert(1)"><img data-theme-asset="hero"><svg><foreignObject><iframe></iframe></foreignObject><path d="M0 0L1 1" fill="url(https://evil.example)"></path></svg><a href="javascript:alert(1)">链接</a>`, themedAssetMapper([asset]));
    expect(safe).toContain("local-image://theme-hero.png");
    for (const forbidden of ["private.png", "<script", "<form", "<iframe", "onerror", "javascript:", "foreignobject", "evil.example"]) expect(safe).not.toContain(forbidden);
    expect(safe).toContain('<path d="M0 0L1 1"');
  });

  it("明暗主题只接受成对且对比合格的正文配色，同时保留标题层级", () => {
    expect(themedColorContrast("#fff", "#000")).toBe(21);
    expect(themedColorContrast("transparent", "#000")).toBe(0);
    const safe = cleanThemedCss(`.light{--theme-surface:#fff8e8;--theme-text:#362819}
      [data-theme="dark"] .chapter{--theme-surface:#25211b;--theme-text:#f4ead8}
      .bad{--theme-surface:#fff;--theme-text:#eee}.transparent{--theme-surface:transparent;--theme-text:#000}
      .missing{--theme-text:#111}h1{font-size:2.5rem}h2{font-size:1.5rem}`);
    expect(safe).toContain("--theme-surface:#fff8e8");
    expect(safe).toContain("--theme-surface:#25211b");
    expect(safe).not.toContain("--theme-text:#eee");
    expect(safe).not.toContain("--theme-text:#111");
    expect(safe).not.toContain("--theme-surface:transparent");
    expect(safe).toContain("font-size:2.5rem");
    expect(safe).toContain("font-size:1.5rem");
    expect(THEMED_READING_CONTENT_GUARD_CSS).not.toContain("font-size:1.2em");
    expect(THEMED_READING_CONTENT_GUARD_CSS).not.toContain("padding:.4em");
  });

  it("分章节设计接受保留原始ID的正文子集", () => {
    expect(() => validateThemedDesign(design('<div data-source-block="b1"></div>'), { ...source, blocks: [source.blocks[1]] }, [])).not.toThrow();
  });

  it("文档只执行固定桥接hash，重新清理备份中的正文和属性", () => {
    const tampered = page({ source: { ...source, blocks: source.blocks.map((block) => ({ ...block, html: block.html + '<script>alert(1)</script><img src="local-image://secret.png">' })) } });
    const html = themedReadingDocument(tampered, 'instance"><script>evil</script>');
    const doc = parseHTML(html).document;
    expect(doc.querySelectorAll("script")).toHaveLength(1);
    const script = doc.querySelector("script").textContent;
    expect(createHash("sha256").update(script).digest("base64")).toBe(THEMED_READING_BRIDGE_HASH);
    expect(html).not.toContain("alert(1)");
    expect(html).not.toContain("local-image://secret.png");
    expect(html).toContain("完整正文与限定条件");
    expect(html).toContain("https://example.com");
    expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]').getAttribute("content")).toContain("connect-src 'none'");
  });

  it("导出无脚本、无实例、无local-image，仅接受清单图片ID对应的内嵌栅格", () => {
    const input = page({ design: design('<img data-theme-asset="hero">' + design().html) });
    const html = themedReadingDocument(input, undefined, { hero: "data:image/png;base64,aGVsbG8=" });
    expect(html).toContain("data:image/png;base64,aGVsbG8=");
    expect(html).not.toContain("<script");
    expect(html).not.toContain("data-instance");
    expect(html).not.toContain("local-image:");
    const bad = themedReadingDocument(input, undefined, { hero: "data:image/svg+xml;base64,aGVsbG8=" });
    expect(bad).not.toContain("data:image/svg");
    expect(themedReadingDocument(input)).not.toContain("local-image:");
  });
});
