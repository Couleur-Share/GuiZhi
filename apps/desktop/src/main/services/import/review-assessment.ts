/**
 * 导入内容的轻量质量检查。
 *
 * 这是准入提示，不是拦截器：解析异常时先保存已获取的内容，再让用户在详情
 * 里明确复核。规则刻意保守，只覆盖能稳定判断的情况；不要靠猜测把短笔记、
 * 数学公式或创作草稿误判为坏数据。
 */
import type { ImportSourceKind } from "@guizhi/shared/types";
import type { ExtractedContent } from "./connectors";

const MIN_PARSED_BODY_CHARS = 80;
const STRUCTURED_TEXT_TYPES = new Set([
  "webpage",
  "document",
  "forum",
]);

export interface ImportReviewAssessment {
  reviewRequired: boolean;
  reasons: string[];
}

/** 将 Markdown 粗略转为可判断正文长度的文本；不参与实际内容写入。 */
function toReviewableText(content: string): string {
  return content
    // 视频等采集条目开头的元数据引用块不能充当正文。
    .replace(/^(?:>[^\n]*(?:\r?\n|$))+\s*/u, "")
    .replace(/!\[([^\]]*)\]\([^)]*\)/gu, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/gu, "$1")
    .replace(/[`*_#>|~-]/gu, " ")
    .replace(/\s+/gu, " ")
    .trim();
}

/**
 * 汇总“已入库但应复核”的原因。
 *
 * warningReason 来自各连接器已知的部分失败；其余两条是解析后才看得出的
 * 质量信号。手输文本跳过长度规则，短句本身完全可能就是有效知识。
 */
export function assessImportReview(
  extracted: ExtractedContent,
  sourceKind: ImportSourceKind,
): ImportReviewAssessment {
  const reasons: string[] = [];
  if (extracted.warningReason?.trim()) {
    reasons.push(extracted.warningReason.trim());
  }

  if (/\uFFFD|[\u0000-\u0008\u000B\u000C\u000E-\u001F]/u.test(extracted.content)) {
    reasons.push("解析文本含有不可识别字符，建议与原始文件核对。");
  }

  const body = toReviewableText(extracted.content);
  if (
    sourceKind !== "text" &&
    STRUCTURED_TEXT_TYPES.has(extracted.itemType) &&
    body.length > 0 &&
    body.length < MIN_PARSED_BODY_CHARS
  ) {
    reasons.push(
      `解析出的正文仅 ${body.length} 字，建议确认是否漏掉了正文或结构内容。`,
    );
  }

  const uniqueReasons = [...new Set(reasons)];
  return {
    reviewRequired: uniqueReasons.length > 0,
    reasons: uniqueReasons,
  };
}
