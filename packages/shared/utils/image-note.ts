/**
 * 图片条目正文的分段：文案 / 图片 / 图中文字。
 *
 * 三者存在同一份 Markdown 里（全文检索与语义索引都指望它），
 * 但混在一屏里读起来很费劲，详情页据此拆成标签页分开看。
 */
import { OCR_SECTION_HEADING } from "./ocr-request";

/** 整段只由图片引用构成（可能连着几张） */
const IMAGE_ONLY_BLOCK = /^(?:!\[[^\]]*\]\([^)]+\)\s*)+$/;

export interface ImageNoteSections {
  /** 文案：元数据引用块、正文文案、状态注记 */
  caption: string;
  /** 「图中文字」小节（含小节标题）；未识别时为空 */
  recognized: string;
}

export function splitImageNoteSections(content: string): ImageNoteSections {
  const headingIndex = content.indexOf(OCR_SECTION_HEADING);
  const head = headingIndex >= 0 ? content.slice(0, headingIndex) : content;

  // 图片交给画廊单独展示，文案里把它们摘出去
  const caption = head
    .split(/\n{2,}/)
    .map((block) => block.trim())
    .filter((block) => block && !IMAGE_ONLY_BLOCK.test(block))
    .join("\n\n");

  return {
    caption,
    recognized: headingIndex >= 0 ? content.slice(headingIndex).trim() : "",
  };
}
