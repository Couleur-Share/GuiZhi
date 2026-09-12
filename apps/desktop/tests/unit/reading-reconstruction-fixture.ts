import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
export function reconstructionFixture(): ThemedReadingVersion {
  return { id: "version", itemId: "item", sourceKind: "body", role: "current", formatVersion: 2,
    source: { title: "原题", content: "仅原文含有的私有文字", sourceUri: null, fingerprint: "a".repeat(64), blocks: [{ id: "b0", markdown: "原始正文", html: "<p>原始正文</p>", text: "原始正文" }] },
    options: { style: "", generateImages: false, maxImages: 0, research: false, action: "create" },
    reconstruction: { notes: ["已有理解"], outline: { title: "重新拟定的标题", direction: "蓝色", questions: [], sections: [{ title: "新的结构", brief: "补充例子" }] }, queries: [], references: [], draft: [{ title: "新的结构", markdown: "可以扩写的专题", referenceIds: [] }], interactions: [{ id: "cost", kind: "calculator", inputs: [{ name: "price", label: "价格", min: 0 }, { name: "count", label: "数量", min: 0 }], expression: "price/count", unit: "元" }] },
    design: { html: `<main><h1>重新拟定的标题</h1><nav><a href="#section">查看方法</a></nav><section id="section"><h2>新的结构</h2><p>${"这是一段扩写后的文章说明，包含背景解释和具体例子，独立于原始正文。".repeat(4)}</p><div data-reading-tool="cost"></div></section></main>`, css: "main{max-width:980px;margin:auto;padding:24px}html[data-theme=dark]{--theme-surface:#112233;--theme-text:#eef4ff}", direction: "蓝色", assets: [] }, assets: [], warnings: [], createdAt: 1, updatedAt: 1 };
}
