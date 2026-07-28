import { createHash } from "crypto";
import { createReadStream } from "fs";
import { pipeline } from "stream/promises";

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

/**
 * 文件内容哈希：媒体资产按内容去重用。
 *
 * 与上面那个是两码事，别混用——那个先做文本标准化（压空白、小写化），
 * 拿去处理二进制会直接改坏字节；而且它一次性收下整个字符串，
 * 视频上限是 1GB，读进内存不可行。这里流式读，内存占用与文件大小无关。
 */
export async function computeFileHash(filePath: string): Promise<string> {
  const hash = createHash("sha256");
  await pipeline(createReadStream(filePath), hash);
  return hash.digest("hex");
}
