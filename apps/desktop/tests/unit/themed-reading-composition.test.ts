// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseHTML } from "linkedom";
import { createHash } from "node:crypto";
import { validateThemedComposition } from "@guizhi/shared/utils/themed-composition";
import { validateThemedReadingVersion } from "@guizhi/db/themed-reading";
import { validateThemedDesign } from "../../src/main/services/themed-reading/sanitize";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { mergeThemeDesignParts } from "../../src/main/services/themed-reading/design";
import { themeTestPage } from "./themed-reading-test-fixtures";
import { sampleComposition } from "./themed-reading-composition-fixture";

const fixture = () => {
  const page = themeTestPage({ assets: [] });
  page.source.fingerprint = "a".repeat(64);
  page.design = { direction: "专题重构", html: "", css: "", assets: [], composition: sampleComposition(page.source) };
  return page;
};
describe("专题重构的来源与执行边界", () => {
  it("原文快照不变，引用可定位，导出只包含固定哈希脚本", () => {
    const page = fixture(), before = JSON.stringify(page.source);
    const doc = parseHTML(themedReadingDocument(page)).document;
    expect([...doc.querySelectorAll("[data-source-block]")].map(node => node.textContent)).toEqual(page.source.blocks.map(block => block.text));
    expect(JSON.stringify(page.source)).toBe(before);
    for (const a of doc.querySelectorAll('a[href^="#"]')) expect(doc.getElementById(a.getAttribute("href").slice(1))).not.toBeNull();
    const scripts = [...doc.querySelectorAll("script")]; expect(scripts).toHaveLength(1);
    const hash = createHash("sha256").update(scripts[0].textContent).digest("base64");
    expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]').getAttribute("content")).toContain(`'sha256-${hash}'`);
    expect(doc.body.textContent).toContain("原文摘录");
    expect(doc.body.textContent).not.toContain("window.api");
    expect(parseHTML(themedReadingDocument(page, "host-id")).document.querySelectorAll("script")).toHaveLength(2);
  });

  it.each([
    ["伪造引用", (p) => { p.chapters[0].lead.evidence[0].quote = "不存在的证明原句"; }],
    ["不存在的原文", (p) => { p.chapters[0].lead.evidence[0].blockId = "b999"; }],
    ["遗漏原文", (p) => { p.chapters[0].blockIds.pop(); }],
    ["重复原文", (p) => { p.chapters[0].blockIds.push("b0"); }],
    ["摘录被改写", (p) => { p.chapters[0].lead.text = "新的事实"; }],
    ["任意公式", (p) => { p.chapters[0].sections[1].formula = "fetch('/private')"; }],
    ["未知工具", (p) => { p.chapters[0].sections[1].calculator = "diagnose"; }],
    ["未声明素材", (p) => { p.chapters[0].imageId = "unknown"; }],
  ] as const)("拒绝%s", (_name, mutate) => {
    const page = fixture(); mutate(page.design.composition);
    expect(() => validateThemedDesign(page.design, page.source, [])).toThrow("专题重构");
  });

  it("重新读库时验证专题数据，旧版格式仍可读", () => {
    const page = fixture(); expect(() => validateThemedReadingVersion(page)).not.toThrow();
    page.design.composition.chapters[0].lead.evidence[0].quote = "备份伪造引用";
    expect(() => validateThemedReadingVersion(page)).toThrow("引用原句不匹配");
    const old = fixture(); delete old.design.composition;
    expect(() => validateThemedReadingVersion(old)).not.toThrow();
  });

  it("矩阵必须逐格绑定依据，推导与归纳使用不同标记", () => {
    const page = fixture(), chapter = page.design.composition.chapters[0];
    const value = { ...chapter.lead, kind: "inference" as const, text: "有待核对的推导" };
    chapter.sections.push({ title: "对比", layout: "matrix", columns: ["条件"], rows: [{ label: "对象甲", cells: [value] }] });
    expect(themedReadingDocument(page)).toContain("推导 · 待核对");
    chapter.sections.at(-1).rows[0].cells = [];
    expect(() => validateThemedComposition(page.design.composition, page.source, [])).toThrow();
  });

  it("正文不能冒充组件，模型文字不会成为脚本或标签", () => {
    const page = fixture(); page.source.blocks[1].html = '<p id="gz-original" class="gz-panel" onclick="alert(1)">保留正文</p>';
    page.design.composition.chapters[0].sections[0].items[0].title = '<img src=x onerror="alert(1)">';
    const doc = parseHTML(themedReadingDocument(page)).document;
    expect(doc.querySelectorAll("#gz-original")).toHaveLength(1);
    expect(doc.querySelector("[data-source-block] .gz-panel")).toBeNull();
    expect(doc.querySelectorAll("[onclick],[onerror]")).toHaveLength(0);
    expect(doc.querySelector(".gz-card h3").textContent).toContain("<img");
    page.design.html = "<script>not allowed</script>";
    expect(() => themedReadingDocument(page)).toThrow("不能混用");
  });

  it("合并章节保留所有专题结构，拒绝混入旧版章节", () => {
    const page = fixture(); const part = page.design;
    expect(mergeThemeDesignParts([part, part], "跨章专题").composition.chapters).toHaveLength(2);
    expect(() => mergeThemeDesignParts([part, themeTestPage().design], "混合")).toThrow("格式不一致");
  });
});
