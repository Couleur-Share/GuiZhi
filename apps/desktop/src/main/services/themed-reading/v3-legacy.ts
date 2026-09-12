import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { parseHTML } from "linkedom";
import { createReadingGeneration, semanticReadingChunks } from "./v3-pipeline";
import { normalizeV3Section } from "./v3-document";

/** 只在用户继续旧工作任务时适配；当前版、上一版与原文均不改写。 */
export function adaptLegacyReadingWork(page: ThemedReadingVersion): void {
  if (page.formatVersion >= 3 || page.role !== "working") return;
  const oldFormat = page.formatVersion,
    oldDesign = page.design,
    parts = page.designParts ?? [];
  page.formatVersion = 3;
  page.options = {
    ...page.options,
    enhancedInteraction: false,
    researchDepth: "deep",
  };
  page.reconstruction ??= {
    notes: [],
    queries: [],
    references: [],
    interactions: [],
    draft: semanticReadingChunks(page.source.content).map((markdown, i) => ({
      title: i ? `正文 ${i + 1}` : page.source.title,
      markdown,
      referenceIds: [],
    })),
    outline: {
      title: page.source.title,
      direction: page.designDirection ?? "保留原有内容",
      questions: [],
      sections: [{ title: page.source.title, brief: "保留原文" }],
    },
  };
  const g = (page.generation = createReadingGeneration(page));
  page.design = null;
  delete page.designParts;
  const designs = oldDesign ? [oldDesign] : oldFormat === 1 ? parts : [];
  for (const [i, design] of designs.entries()) {
    if (design.composition) continue;
    const id = `legacy${i}`,
      doc = parseHTML(`<html><body>${design.html}</body></html>`).document;
    for (const slot of doc.querySelectorAll("[data-source-block]")) {
      const block = page.source.blocks.find(
        (b) => b.id === slot.getAttribute("data-source-block"),
      );
      if (block) slot.innerHTML = block.html;
      slot.removeAttribute("data-source-block");
    }
    const html = doc.body.innerHTML;
    g.sectionOrder ??= [];
    g.sectionOrder.push(id);
    try {
      const normalized = normalizeV3Section(html, id, false);
      g.sections.push({ id, html: normalized.html });
      g.css += design.css + "\n" + normalized.css;
      g.revision++;
    } catch (error) {
      (g.candidates ??= {})[id] = html;
      g.issues.push({
        kind: "format",
        unit: id,
        message: error instanceof Error ? error.message : String(error),
      });
    }
  }
  // 已保存整页候选只做局部修复；分章检查点继续补足其余内容。
  g.done = Boolean(oldDesign && !oldDesign.composition);
}
