import { createHash } from "crypto";

/**
 * 内容哈希：用于导入去重的第二重判定。
 * 先做文本标准化（统一换行、压缩空白、小写化），
 * 使排版差异不影响重复识别。
 */
export function computeContentHash(content: string): string {
  const normalized = (content ?? "")
    .replace(/\r\n?/g, "\n")
    .replace(/[\t ]+/g, " ")
    .replace(/\n{2,}/g, "\n")
    .trim()
    .toLowerCase();
  return createHash("sha256").update(normalized, "utf8").digest("hex");
}
