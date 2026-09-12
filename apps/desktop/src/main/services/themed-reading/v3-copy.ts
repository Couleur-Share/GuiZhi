import { parseHTML } from "linkedom";
import type { ReadingGeneration } from "@guizhi/shared/types/reading-page-v3";
import type { ReadingDraftSection } from "@guizhi/shared/types/reading-reconstruction";
import { prepareDesignCopy } from "./design-copy";
import { normalizeV3Section } from "./v3-document";

/** 遗漏的普通正文可确定性回填；相同的正文引用只保留第一次。 */
export function completeReadingCopy(
  draft: ReadingDraftSection[],
  generation: ReadingGeneration,
): void {
  const copy = prepareDesignCopy(draft, true),
    used = new Set<string>();
  for (const section of generation.sections) {
    const doc = parseHTML(`<html><body>${section.html}</body></html>`).document;
    for (const node of doc.querySelectorAll("[data-reading-copy-id]")) {
      const id = node.getAttribute("data-reading-copy-id");
      if (used.has(id)) node.remove();
      else used.add(id);
    }
    section.html = doc.body.innerHTML;
  }
  const plain = (html: string) =>
    parseHTML(
      `<html><body>${html}</body></html>`,
    ).document.body.textContent.replace(/\s/g, "");
  const present = plain(generation.sections.map((s) => s.html).join("\n"));
  for (const [index, chapter] of copy.manuscript.entries()) {
    const missing = chapter.blocks.filter(
      (b) =>
        b.copyId &&
        !present.includes(
          plain(copy.expand(`<div data-reading-copy="${b.copyId}"></div>`)),
        ),
    );
    if (!missing.length) continue;
    let id = `manuscript-${index}`;
    while (generation.sections.some((s) => s.id === id)) id += "-more";
    const doc = parseHTML(
      "<html><body><section><h2></h2></section></body></html>",
    ).document;
    doc.querySelector("h2").textContent = chapter.title;
    const root = doc.querySelector("section");
    for (const block of missing) {
      const slot = doc.createElement("div");
      slot.setAttribute("data-reading-copy", block.copyId);
      root.append(slot);
    }
    const normalized = normalizeV3Section(
      copy.expand(doc.body.innerHTML),
      id,
      false,
    );
    generation.sections.push({ id, html: normalized.html });
    (generation.sectionOrder ??= []).push(id);
    generation.revision++;
  }
}
