import type { ThemedComposition, ThemedStatement } from "@guizhi/shared/types/themed-composition";
import type { ThemedReadingSource } from "@guizhi/shared/types/themed-reading";

export function sampleComposition(source: ThemedReadingSource): ThemedComposition {
  const blocks = source.blocks.filter(block => block.text.trim());
  const quote = (index: number): ThemedStatement => ({ text: blocks[index].text.slice(0, 600), kind: "quote", evidence: [{ blockId: blocks[index].id, quote: blocks[index].text.slice(0, 600) }] });
  return { version: 1, chapters: [{ blockIds: source.blocks.map(block => block.id), palette: "marine", category: "测试专题", subtitle: quote(0), lead: quote(0), sections: [
    { title: "原文观点", layout: "cards", items: blocks.map((_, i) => ({ title: `观点 ${i + 1}`, body: quote(i) })) },
    { title: "计算成本", layout: "calculator", calculator: "unit-cost", intro: quote(0) },
    { title: "选择情境", layout: "explorer", items: [{ title: "情境一", body: quote(0) }, { title: "情境二", body: quote(blocks.length - 1) }] },
  ] }] };
}
