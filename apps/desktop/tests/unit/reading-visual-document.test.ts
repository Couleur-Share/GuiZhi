import { describe, expect, it } from "vitest";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { normalizeReadingGraphic } from "../../src/main/services/themed-reading/visual-normalize";
import { reconstructionDocument, validateReconstructionPage } from "../../src/main/services/themed-reading/reconstruction-document";
import fs from "node:fs";
import path from "node:path";
const markup = '<svg viewBox="0 0 100 100"><defs><marker id="arrow"><path d="M0 0L4 2L0 4"/></marker></defs><path id="line" class="line" d="M0 0L90 90" marker-end="url(#arrow)"/><text x="10" y="30">中文图形</text></svg>';
describe("图形输出边界", () => {
  it.each(["flow", "sequence", "state", "mind", "chart"])("规范化真实编译输出 %s", name => {
    const raw = fs.readFileSync(path.resolve("tests/fixtures/reading-graphics", `${name}.svg`), "utf8");
    const result = normalizeReadingGraphic(raw, "", name);
    expect(result.svg).toContain("<svg"); expect(result.css).not.toContain("infinite");
    expect(normalizeReadingGraphic(result.svg, result.css, name)).toEqual(result);
  });
  it("命名空间可重复读取，文字与引用保留", () => {
    const result = normalizeReadingGraphic(markup, '.line{stroke:#4477aa}', 'test');
    expect(result.svg).toContain('rv-test-arrow'); expect(result.css).toContain('var(--theme-visual-1)');
    expect(normalizeReadingGraphic(result.svg, result.css, 'test')).toEqual(result);
  });
  it.each(['<script>alert(1)</script>', '<foreignObject>外部</foreignObject>', '<image href="https://evil.invalid/a"/>', '<path onload="alert(1)"/>'])("拒绝可执行或外部元素 %s", value => expect(() => normalizeReadingGraphic(`<svg>${value}</svg>`, '', 'test')).toThrow());
  it("禁止外部样式和跨图引用", () => {
    expect(() => normalizeReadingGraphic(markup, '@import "https://evil.invalid/a";', 'test')).toThrow();
    expect(() => normalizeReadingGraphic(markup.replace('url(#arrow)', 'url(#other)'), '', 'test')).toThrow();
  });
  it("只保留受控的关键帧属性", () => {
    const result = normalizeReadingGraphic(markup, '@keyframes enter{0%{opacity:0}100%{opacity:1}}.line{animation:enter 1s}', 'test');
    expect(result.css).toContain('rv-test-enter');
    expect(() => normalizeReadingGraphic(markup, '@keyframes hide{0%{display:none}}', 'test')).toThrow();
    expect(() => normalizeReadingGraphic(markup, '@keyframes enter{0%{opacity:0}100%{opacity:1}}.line{animation:enter 1s 100}', 'test')).toThrow(/循环/);
    expect(() => normalizeReadingGraphic(markup, '@keyframes enter{0%{opacity:0}100%{opacity:1}}.line{animation:enter 6s 6s}', 'test')).toThrow(/时长/);
  });
  it("旧页不带动画运行时，静态图形可正常验证", () => {
    const page = reconstructionFixture();
    expect(reconstructionDocument(page)).not.toContain('gz-system-motion-data');
    page.reconstruction.visuals = [{ id: 'custom', kind: 'svg', title: '图解', description: '完整说明' }];
    page.design.html += `<figure data-reading-visual="custom">${markup}<figcaption>完整说明</figcaption></figure>`;
    page.reconstruction.animations = [{ visualId: 'custom', preset: 'draw', targetId: 'line' }];
    expect(() => validateReconstructionPage(page)).not.toThrow();
    expect(reconstructionDocument(page)).toContain('gz-system-motion-data');
    page.reconstruction.animations[0].targetId = 'section';
    expect(() => validateReconstructionPage(page)).toThrow(/动画目标/);
  });
});
