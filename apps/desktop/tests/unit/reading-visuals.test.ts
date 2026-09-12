import { describe, expect, it } from "vitest";
import { validateReadingVisuals, validateReadingVisualResults } from "@guizhi/shared/utils/reading-visuals";
import type { ReadingVisual } from "@guizhi/shared/types/reading-visuals";
import { readingComponentCss } from "../../src/main/services/themed-reading/visual-components";
const graph: ReadingVisual = { id: "process", kind: "mermaid", title: "流程", description: "完整流程说明", source: "flowchart TD\nA[开始] --> B[结束]" };
describe("阅读视觉数据", () => {
  it("兼容未声明图形的旧页，并支持常规流程", () => { expect(() => validateReadingVisuals()).not.toThrow(); expect(() => validateReadingVisuals([graph])).not.toThrow(); });
  it("支持时序图的实线与虚线返回箭头", () => expect(() => validateReadingVisuals([{ ...graph, source: "sequenceDiagram\nA->>B: 请求\nB-->>A: 响应" }])).not.toThrow());
  it.each(["%%{init: {securityLevel:'loose'}}%%\nflowchart TD\nA-->B", "flowchart TD\nclick A callback", "flowchart TD\nA[<img src=x>]", "flowchart TD\nA@{ img: 'https://example.com' }"])("拒绝配置与外部资源 %s", source => expect(() => validateReadingVisuals([{ ...graph, source }])).toThrow());
  it("图表必须有原稿依据且值有限", () => {
    const v: ReadingVisual = { id: "data", kind: "chart", title: "统计", description: "年度数据", chart: { type: "bar", categories: ["甲"], series: [{ name: "数量", values: [10] }], unit: "个", evidence: { section: 0, quote: "甲为10个" } } };
    expect(() => validateReadingVisuals([v], [], [{ markdown: "甲为10个。" }])).not.toThrow();
    expect(() => validateReadingVisuals([v])).toThrow(/依据/);
    v.chart.series[0].values[0] = Infinity;
    expect(() => validateReadingVisuals([v], [], [{ markdown: "甲为10个。" }])).toThrow(/数值/);
  });
  it("动画不接受任意选择器和重复目标", () => {
    const v = { ...graph, kind: "svg" as const };
    expect(() => validateReadingVisuals([v], [{ visualId: v.id, preset: "draw", targetId: "body *" }])).toThrow();
    expect(() => validateReadingVisuals([v], Array(2).fill({ visualId: v.id, preset: "draw", targetId: "line" }))).toThrow();
  });
  it("编译失败必须保留原因", () => expect(() => validateReadingVisualResults([{ id: "a", sourceHash: "a".repeat(64), compilerVersion: "1", status: "failed" }])).toThrow());
  it("仅按需包含有前缀的官方组件 CSS", () => {
    const css = readingComponentCss('<aside class="gz-ui-alert">说明</aside>');
    expect(css).toContain(".gz-ui-alert"); expect(css).not.toContain(".gz-ui-timeline>"); expect(css).not.toContain(".alert{");
    expect(css).toContain(".gz-reading-components");
  });
});
