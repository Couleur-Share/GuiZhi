import fs from "node:fs";
import path from "node:path";
import postcss from "postcss";
import { describe, expect, it } from "vitest";
import { WINDOW_BREAKPOINTS, windowBreakpointTokens } from "../../../src/renderer/reader/breakpoints";
import { parseTheme } from "../../../src/renderer/reader/protocol";
import { applyTheme } from "../../../src/renderer/reader/theme";
import { windowWidthMedia } from "../../../src/renderer/reader/window-width-media";

// 岛的视口只有阅读区那么宽，渲染层的响应式样式按窗口宽度判定：样式表在构建时把 min-width 媒体查询改成以
// <html data-win-ge> 为条件，页面按宿主给的窗口宽度写这个属性。这里测断点表、外观协议、写属性与样式表转换器，
// 以及入库的 reader.css 确实已经转换过（实测过的缺陷：阅读区 592 px 宽时 sm:px-10 不生效）。

const transform = async (css: string): Promise<string> => (await postcss([windowWidthMedia()]).process(css, { from: undefined })).css;
const base = { dark: false, hue: 210, saturation: 35, basePx: 16, motion: "standard" };

describe("windowBreakpointTokens", () => {
  it("按「窗口宽度 ≥ 断点」列出已达到的断点（与 min-width 同义，边界含等号）", () => {
    expect(windowBreakpointTokens(639)).toBe("");
    expect(windowBreakpointTokens(640)).toBe("640");
    expect(windowBreakpointTokens(1279.5)).toBe("640 768 1024");
    expect(windowBreakpointTokens(1280)).toBe("640 768 1024 1280");
    expect(windowBreakpointTokens(1400)).toBe("640 768 1024 1280 1400");
    expect(windowBreakpointTokens(1536)).toBe("640 768 1024 1280 1400 1536");
    expect(windowBreakpointTokens(1920)).toBe("640 768 1024 1280 1400 1536");
  });

  it("断点表升序且没有重复", () => {
    const sorted = [...WINDOW_BREAKPOINTS].sort((a, b) => a - b);
    expect([...WINDOW_BREAKPOINTS]).toEqual(sorted);
    expect(new Set(WINDOW_BREAKPOINTS).size).toBe(WINDOW_BREAKPOINTS.length);
  });
});

describe("外观协议里的窗口宽度", () => {
  it("可以不给（页面取自身宽度），给了就原样保留", () => {
    expect(parseTheme(base)).toEqual(base);
    expect(parseTheme({ ...base, viewportWidth: 1280 })).toEqual({ ...base, viewportWidth: 1280 });
    expect(parseTheme({ ...base, viewportWidth: 100 })?.viewportWidth).toBe(100);
    expect(parseTheme({ ...base, viewportWidth: 20000 })?.viewportWidth).toBe(20000);
  });

  it("给了就要合规：不是数、不是有限数、越界都整条外观丢弃", () => {
    for (const viewportWidth of ["1280", null, Number.NaN, Number.POSITIVE_INFINITY, 99, 20001, -1]) {
      expect(parseTheme({ ...base, viewportWidth }), String(viewportWidth)).toBeNull();
    }
  });
});

describe("applyTheme 写窗口宽度断点", () => {
  it("宿主给了窗口宽度就按它写，与页面自己的宽度无关", () => {
    const root = document.documentElement;
    applyTheme(root, { ...(base as Parameters<typeof applyTheme>[1]), viewportWidth: 1280 }, "zh");
    expect(root.getAttribute("data-win-ge")).toBe("640 768 1024 1280");
    applyTheme(root, { ...(base as Parameters<typeof applyTheme>[1]), viewportWidth: 700 }, "zh");
    expect(root.getAttribute("data-win-ge")).toBe("640");
  });

  it("没给就取页面自身宽度（jsdom 默认 1024）", () => {
    const root = document.documentElement;
    applyTheme(root, base as Parameters<typeof applyTheme>[1], "zh");
    expect(root.getAttribute("data-win-ge")).toBe(windowBreakpointTokens(window.innerWidth));
  });
});

describe("windowWidthMedia（PostCSS 插件）", () => {
  it("把 min-width 媒体查询展开成以 data-win-ge 为条件的规则，规则的先后顺序不变", async () => {
    const out = await transform(".a{color:red}@media (min-width:640px){.sm\\:px-10{padding-left:2.5rem}.sm\\:py-9{padding-top:2.25rem}}.b{color:blue}");
    expect(out).not.toContain("@media");
    expect(out).toContain('.sm\\:px-10:where(html[data-win-ge~="640"] *)');
    expect(out).toContain('.sm\\:py-9:where(html[data-win-ge~="640"] *)');
    expect(out.indexOf(".a{")).toBeLessThan(out.indexOf(".sm\\:px-10"));
    expect(out.indexOf(".sm\\:py-9")).toBeLessThan(out.indexOf(".b{"));
  });

  it("选择器列表里的每一项都带条件", async () => {
    const out = await transform("@media (min-width:1024px){.a,.b>.c{display:none}}");
    expect(out).toContain('.a:where(html[data-win-ge~="1024"] *)');
    expect(out).toContain('.b>.c:where(html[data-win-ge~="1024"] *)');
  });

  it("与宽度无关的媒体查询原样保留", async () => {
    const css = "@media (prefers-reduced-motion:reduce){.x{animation:none}}";
    expect(await transform(css)).toBe(css);
  });

  it("不认识的宽度媒体查询、表外的断点、以 html / :root 为主体的选择器、嵌套的节点都报错，不悄悄放过", async () => {
    await expect(transform("@media (max-width:640px){.a{color:red}}")).rejects.toThrow(/不认识的宽度媒体查询/);
    await expect(transform("@media (width >= 640px){.a{color:red}}")).rejects.toThrow(/不认识的宽度媒体查询/);
    await expect(transform("@media (min-width:640px) and (max-width:900px){.a{color:red}}")).rejects.toThrow(/不认识的宽度媒体查询/);
    await expect(transform("@media (min-width:999px){.a{color:red}}")).rejects.toThrow(/WINDOW_BREAKPOINTS/);
    await expect(transform("@media (min-width:640px){html .a{color:red}}")).rejects.toThrow(/html/);
    await expect(transform("@media (min-width:640px){:root{--x:1}}")).rejects.toThrow(/:root/);
    await expect(transform("@media (min-width:640px){@supports (display:grid){.a{display:grid}}}")).rejects.toThrow(/嵌套/);
    await expect(transform("@media (min-width:640px){.a::before{content:''}}")).rejects.toThrow(/伪元素/);
    await expect(transform("@media (min-width:640px){.a:after{content:''}}")).rejects.toThrow(/伪元素/);
  });

  it("入库的 reader.css 已经转换过：没有 min-width 媒体查询，样式改由 data-win-ge 决定（查看岛盖满整窗，不转换）", () => {
    const css = fs.readFileSync(path.resolve(__dirname, "../../../../desktop-go/viva/reader/assets/reader.css"), "utf8");
    expect(css).not.toMatch(/@media\s*(screen\s+and\s*)?\(\s*(min|max)-width/);
    for (const width of WINDOW_BREAKPOINTS) expect(css, String(width)).toContain(`data-win-ge~="${width}"`);
  });
});
