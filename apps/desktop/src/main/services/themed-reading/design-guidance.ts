import { parseHTML } from "linkedom";
import type { ThemedReadingBlock } from "@guizhi/shared/types";

export const THEMED_READING_PLAN_GUIDANCE = "direction 要具体描述：原文中的内容关系、主阅读结构、哪些段落需要视觉强调、目录与正文宽度、窄屏变化、成对明暗配色，以及插画如何辅助对应内容。不要只给出风格名称或配色，也不要把加大图作为唯一视觉方案。";

/** 提供实际回填 HTML 的结构，不把 Markdown 表面写法误当成最终可设计的元素。 */
export function describeThemedBlock(block: ThemedReadingBlock) {
  const { document } = parseHTML(`<html><body>${block.html}</body></html>`);
  const root = document.body.firstElementChild;
  const heading = document.body.querySelector("h1,h2,h3,h4,h5,h6");
  const list = document.body.querySelector("ul,ol");
  const directItems = list ? [...list.children].filter((child) => child.localName === "li").length : 0;
  const table = document.body.querySelector("table");
  const rows = table ? [...table.querySelectorAll("tr")] : [];
  return {
    element: root?.localName ?? "p",
    structure: {
      heading: heading ? { level: Number(heading.localName.slice(1)), text: heading.textContent.trim().slice(0, 180) } : undefined,
      list: list ? { ordered: list.localName === "ol", items: directItems, nestedItems: list.querySelectorAll("li").length - directItems } : undefined,
      table: table ? { rows: rows.length, columns: rows.reduce((max, row) => Math.max(max, [...row.children].filter((cell) => ["td", "th"].includes(cell.localName)).length), 0) } : undefined,
      paragraphs: document.body.querySelectorAll("p").length,
      images: document.body.querySelectorAll("img").length,
      codeBlocks: document.body.querySelectorAll("pre").length,
      quotes: document.body.querySelectorAll("blockquote").length,
    },
  };
}
