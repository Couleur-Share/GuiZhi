// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseHTML } from "linkedom";
import { cleanThemedCss } from "../../src/main/services/themed-reading/theme-css";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { themeTestPage } from "./themed-reading-test-fixtures";

describe("主题正文 CSS 可见性边界", () => {
  it("拒绝正向巨量留白，以及物理/字符/百分比单位和变量间接绕过", () => {
    for (const value of ["100000px", "100000cm", "10000in", "10000ch", "100000em", "999rem", "999%", "var(--theme-offset)", "calc(64px * 999999)"]) {
      const safe = cleanThemedCss(`.outer{margin-left:${value};padding:${value};gap:${value};border-spacing:${value}}`);
      expect(safe).not.toMatch(/margin-left:|padding:|gap:|border-spacing:/);
    }
  });

  it("保留常规留白、居中、网格与阴影，并限制窄容器的累计缩进", () => {
    const safe = cleanThemedCss(".article{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px;margin:32px auto;padding:24px 2rem;border:1px solid #b8862c;box-shadow:0 2px 12px #0002}");
    expect(safe).toContain("display:grid");
    expect(safe).toContain("grid-template-columns:repeat(2,minmax(0,1fr))");
    expect(safe).toContain("gap:24px min(24px,4%)");
    expect(safe).toContain("margin:min(32px,4%) auto");
    expect(safe).toContain("padding:min(24px,4%) min(2rem,4%)");
    expect(safe).toContain("border:1px solid #b8862c");
    expect(safe).toContain("box-shadow:0 2px 12px #0002");
    expect(cleanThemedCss(safe)).toBe(safe);
  });

  it("阻断用字距、基线、粗边框或巨量阴影把正文遮挡或推出画布", () => {
    const safe = cleanThemedCss(".x{letter-spacing:90px;word-spacing:6em;vertical-align:999cm;border:99999px solid #fff;box-shadow:0 0 0 999999px #fff;font-style:oblique 90deg;font-weight:1}.readable{letter-spacing:.03em;word-spacing:2px;vertical-align:middle;font-style:italic;font-weight:600}");
    expect(safe).not.toMatch(/90px|6em|999cm|99999px|999999px|oblique|font-weight:1/);
    expect(safe).toContain("letter-spacing:.03em");
    expect(safe).toContain("word-spacing:2px");
    expect(safe).toContain("vertical-align:middle");
    expect(safe).toContain("font-weight:600");
  });

  it("几何表达式不可通过变量、算术、无限大或超量网格轨道绕过", () => {
    const safe = cleanThemedCss(".bad{width:var(--theme-size);min-width:calc(100px * 99999);max-width:99999%;grid-template-columns:repeat(999999,1fr);padding:1e999px}.good{width:100%;max-width:min(1200px,100%);grid-template-columns:1fr 2fr}");
    expect(safe).not.toMatch(/var\(|calc\(|99999|1e999/);
    expect(safe).toContain("width:100%");
    expect(safe).toContain("max-width:min(1200px,100%)");
    expect(safe).toContain("grid-template-columns:minmax(0,1fr) minmax(0,2fr)");
  });

  it("保留逻辑留白和可静态求界的响应式表达式，重复清理保持稳定", () => {
    const safe = cleanThemedCss(".chapter{padding-inline:clamp(1rem,calc(1rem + 8px),3rem);padding-block:max(12px,1rem);margin-block:calc(2rem - 1rem);margin-inline:auto;gap:min(16px,2rem)}");
    expect(safe).toContain("padding-inline:min(clamp(1rem,calc(1rem + 8px),3rem),4%)");
    expect(safe).toContain("padding-block:min(max(12px,1rem),4%)");
    expect(safe).toContain("margin-block:min(calc(2rem - 1rem),4%)");
    expect(safe).toContain("margin-inline:auto");
    expect(safe).toContain("gap:min(16px,2rem) min(min(16px,2rem),4%)");
    expect(cleanThemedCss(safe)).toBe(safe);
  });

  it("网格纵向间距参与自动高度，横向保留上限并修复已清理旧值", () => {
    const safe = cleanThemedCss(".single{gap:16px}.pair{gap:16px 24px}.rows{row-gap:clamp(12px,1rem,24px)}.columns{column-gap:16px}.dynamic{gap:clamp(12px,1rem,24px) calc(1rem + 8px)}.legacy{gap:min(16px,4%)}.legacy-pair{gap:min(16px,4%) min(24px,4%)}.legacy-row{row-gap:min(clamp(12px,1rem,24px),4%)}");
    expect(safe).toContain(".single{gap:16px min(16px,4%)}");
    expect(safe).toContain(".pair{gap:16px min(24px,4%)}");
    expect(safe).toContain(".rows{row-gap:clamp(12px,1rem,24px)}");
    expect(safe).toContain(".columns{column-gap:min(16px,4%)}");
    expect(safe).toContain(".dynamic{gap:clamp(12px,1rem,24px) min(calc(1rem + 8px),4%)}");
    expect(safe).toContain(".legacy{gap:16px min(16px,4%)}");
    expect(safe).toContain(".legacy-pair{gap:16px min(24px,4%)}");
    expect(safe).toContain(".legacy-row{row-gap:clamp(12px,1rem,24px)}");
    expect(cleanThemedCss(safe)).toBe(safe);
  });

  it("动态留白拒绝越界中间量、乘除、变量和混合单位减法", () => {
    for (const value of ["clamp(1rem,100vw,3rem)", "min(99999px,1rem)", "calc(32px + 64px)", "calc(1rem * 999)", "calc(1rem / .0001)", "calc(2rem - 99px)", "max(var(--theme-space),1rem)", "calc(4rem + 1px)"]) {
      expect(cleanThemedCss(`.chapter{padding-inline:${value};margin-block:${value};gap:${value}}`)).not.toMatch(/padding-inline:|margin-block:|gap:/);
    }
  });

  it("侧栏与正文保留各自宽度和比例，轨道可以缩窄且网格数量有界", () => {
    const safe = cleanThemedCss(".fish{grid-template-columns:14rem 48rem}.weighted{grid-template-columns:25% 3fr}.cards{grid-template-columns:repeat(3,minmax(18rem,1fr))}.adaptive{grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))}.bad{grid-template-columns:repeat(8,1fr 1fr)}");
    expect(safe).toContain("grid-template-columns:minmax(0,14rem) minmax(0,48rem)");
    expect(safe).toContain("grid-template-columns:minmax(0,25%) minmax(0,3fr)");
    expect(safe).toContain("grid-template-columns:repeat(3,minmax(0,1fr))");
    expect(safe).toContain("grid-template-columns:repeat(auto-fit,minmax(min(16rem,100%),1fr))");
    expect(safe).not.toContain("repeat(8");
    expect(cleanThemedCss(safe)).toBe(safe);
  });

  it("拒绝极窄固定轨道、表达式下界和失衡的 fr 比例，避免正文被挤成竖线", () => {
    for (const tracks of ["1px 1fr", "1.6em 1fr", "7.99rem 1fr", "127px 1fr", "0 1fr", "1% 1fr", "minmax(0,1px) 1fr", "calc(16rem - 15rem) 1fr", "min(1px,14rem) 1fr", ".00001fr 12fr", ".25fr 12fr", "repeat(2,minmax(0,.00001fr))"]) {
      expect(cleanThemedCss(`.layout{display:grid;grid-template-columns:${tracks}}`)).not.toContain("grid-template-columns:");
    }
    const safe = cleanThemedCss(".fixed{grid-template-columns:128px 48rem}.relative{grid-template-columns:8rem 48rem}.weighted{grid-template-columns:.5fr 2fr}.calculated{grid-template-columns:calc(16rem - 8rem) 1fr}");
    expect(safe).toContain("grid-template-columns:minmax(0,128px) minmax(0,48rem)");
    expect(safe).toContain("grid-template-columns:minmax(0,8rem) minmax(0,48rem)");
    expect(safe).toContain("grid-template-columns:minmax(0,.5fr) minmax(0,2fr)");
    expect(safe).toContain("grid-template-columns:minmax(0,calc(16rem - 8rem)) minmax(0,1fr)");
    expect(cleanThemedCss(safe)).toBe(safe);
  });

  it("正文所有祖先均受正常字号、流布局与配色保护，SVG有高度上限", () => {
    const page = themeTestPage({ assets: [] });
    page.design.html = '<main class="outer"><section class="inner"><div data-source-block="b0"></div><details open><summary>展开</summary><div data-source-block="b1"></div></details></section></main>';
    page.design.css = '.outer,.inner{font-size:5em;color:transparent;margin-left:100000in;padding:100000px}.inner{--theme-surface:#fff8e8;--theme-text:#362819}summary{color:transparent}';
    const html = themedReadingDocument(page, "reader"), doc = parseHTML(html).document;
    expect(doc.querySelector("main").hasAttribute("data-source-layout")).toBe(true);
    expect(doc.querySelector("section").hasAttribute("data-source-layout")).toBe(true);
    expect(doc.querySelector("details").hasAttribute("data-source-layout")).toBe(true);
    expect(html).not.toMatch(/100000(?:in|px)/);
    expect(doc.body.textContent).toContain("完整正文与限定条件");
    const style = doc.querySelector("style").textContent;
    expect(style).toContain("margin-inline:0!important");
    expect(style).toContain("font-size:var(--reader-font-size)!important");
    expect(style).toContain("nav,summary{color:var(--theme-text)!important");
    expect(style).toContain("svg,[data-source-block] svg{max-height:30rem!important");
    expect(style).toContain("--theme-surface:#fff8e8");
  });

  it("拒绝病态深层容器，常规文章章节嵌套仍可生成", () => {
    const page = themeTestPage({ assets: [] });
    const slots = '<div data-source-block="b0"></div><div data-source-block="b1"></div>';
    page.design.html = "<section>".repeat(17) + slots + "</section>".repeat(17);
    expect(() => themedReadingDocument(page)).toThrow("嵌套超过 16 层");
    page.design.html = "<section>".repeat(4) + slots + "</section>".repeat(4);
    expect(themedReadingDocument(page)).toContain("完整正文与限定条件");
  });
});
