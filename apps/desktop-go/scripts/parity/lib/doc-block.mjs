/**
 * 在 Markdown 文档里维护「由脚本生成」的区块：
 *   <!-- parity:<名称>:begin --> … <!-- parity:<名称>:end -->
 * 盘点脚本的 --write 用它刷新分母表；脚本测试用它核对文档里的区块与源码重新统计的结果一致。
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = path.resolve(here, "../../../../..");
export const INDEX_DOC = path.join(REPO_ROOT, "docs/agents/viva-parity-inventory.md");

const lf = (s) => s.replace(/\r\n/g, "\n");
const markers = (name) => ({ begin: `<!-- parity:${name}:begin -->`, end: `<!-- parity:${name}:end -->` });

/** 读出区块内容（已按 LF 归一化、去首尾空白）；找不到标记时抛错。 */
export function readBlock(text, name) {
  const t = lf(text);
  const { begin, end } = markers(name);
  const i = t.indexOf(begin);
  const j = t.indexOf(end);
  if (i < 0 || j < i) throw new Error(`文档里缺少区块标记 ${begin} … ${end}`);
  return t.slice(i + begin.length, j).trim();
}

/** 用新内容替换区块，返回新文本（LF）。 */
export function replaceBlock(text, name, content) {
  const t = lf(text);
  const { begin, end } = markers(name);
  const i = t.indexOf(begin);
  const j = t.indexOf(end);
  if (i < 0 || j < i) throw new Error(`文档里缺少区块标记 ${begin} … ${end}`);
  return `${t.slice(0, i + begin.length)}\n${lf(content).trim()}\n${t.slice(j)}`;
}

/** 刷新索引文档里的一个区块并写回；返回内容是否发生变化。 */
export function writeBlock(name, content, docPath = INDEX_DOC) {
  const before = fs.readFileSync(docPath, "utf8");
  const after = replaceBlock(before, name, content);
  if (lf(before) === after) return false;
  fs.writeFileSync(docPath, after);
  return true;
}
