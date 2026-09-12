import { parseHTML } from "linkedom";
import { renderThemedReadingBlocks } from "./content";
import type { ReadingDraftSection } from "@guizhi/shared/types/reading-reconstruction";

/** 普通正文由已保存编辑稿回填，模型只需输出布局和真正需要设计的内容。 */
export function prepareDesignCopy(draft: ReadingDraftSection[], keepIds = false) {
  const copies = new Map<string, string>();
  const manuscript = draft.map((section, sectionIndex) => ({
    title: section.title,
    blocks: renderThemedReadingBlocks(section.markdown).map((block, index) => {
      const id = `s${sectionIndex}b${index}`;
      const doc = parseHTML(`<html><body>${block.html}</body></html>`).document;
      // 图片仍走素材清单；带脚注 ID 的块由模型编排，避免跨章节 ID 冲突。
      const reusable = !doc.querySelector("img,[id],[data-reading-copy]");
      if (reusable) copies.set(id, block.html);
      return { ...(reusable ? { copyId: id } : {}), markdown: block.markdown };
    }),
  }));
  return {
    manuscript,
    expand(html: string) {
      if (!html.includes("data-reading-copy")) return html;
      const doc = parseHTML(`<html><body>${html}</body></html>`).document;
      const used = new Set<string>();
      for (const node of doc.querySelectorAll("[data-reading-copy]")) {
        const id = node.getAttribute("data-reading-copy");
        if (node.localName !== "div" || node.childNodes.length || !copies.has(id) || used.has(id)) throw new Error("正文引用必须为唯一、有效的空 div");
        used.add(id); node.innerHTML = copies.get(id); node.removeAttribute("data-reading-copy");
        if(keepIds)node.setAttribute("data-reading-copy-id",id);
      }
      return doc.body.innerHTML;
    },
  };
}
