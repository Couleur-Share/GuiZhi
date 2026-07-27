/**
 * 正文配图在 Markdown 里的读写。
 *
 * 配图必须落在条目正文里：资产回收（main/services/asset-cleanup.ts）的引用集合
 * 来自对 `knowledge_items.content` 的 `LIKE '%local-image://%'` 扫描，
 * 不在正文里的图会在下一次彻底删除时被当成孤儿删掉。Markdown 导出与全文检索
 * 同样只认正文。
 *
 * 「插在第几段之后」用**块序号**表达，不做原文片段的模糊匹配：策划与插入
 * 共用 `splitContentBlocks`，序号一致，行为可单测。
 */
import type { IllustrationEntry } from "../types/illustration";

/** 生成的配图用这个文件名前缀，与采集导入的 `import-` 资产区分开 */
export const ILLUSTRATION_ASSET_PREFIX = "gen-";

/** 太短的段落撑不起一张图的信息量，不作为配图锚点 */
export const ANCHOR_MIN_CHARS = 60;

const FENCE_LINE = /^\s{0,3}(?:```|~~~)/;
const HEADING_LINE = /^\s{0,3}#{1,6}\s/;
const IMAGE_ONLY_BLOCK = /^(?:!\[[^\]]*\]\([^)]+\)\s*)+$/;

export interface ContentBlock {
  /** 块序号：策划提示词与插入定位共用同一套编号 */
  index: number;
  text: string;
  /** 在 content.split("\n") 中的行区间（闭区间） */
  startLine: number;
  endLine: number;
}

/**
 * 按空行切块并编号。
 *
 * 代码围栏内的空行不算段落边界——否则带空行的代码块会被从中间切开，
 * 配图可能插进代码里。
 */
export function splitContentBlocks(content: string): ContentBlock[] {
  const lines = content.split("\n");
  const blocks: ContentBlock[] = [];
  let start = -1;
  let inFence = false;

  const flush = (end: number): void => {
    if (start < 0 || end < start) {
      start = -1;
      return;
    }
    const text = lines.slice(start, end + 1).join("\n").trim();
    if (text) {
      blocks.push({
        index: blocks.length,
        text,
        startLine: start,
        endLine: end,
      });
    }
    start = -1;
  };

  for (const [index, line] of lines.entries()) {
    if (FENCE_LINE.test(line)) {
      if (start < 0) {
        start = index;
      }
      inFence = !inFence;
      continue;
    }
    if (!inFence && line.trim() === "") {
      flush(index - 1);
      continue;
    }
    if (start < 0) {
      start = index;
    }
  }
  flush(lines.length - 1);
  return blocks;
}

/** 整块只有标题行（插在它后面会把标题和它的正文隔开） */
function isHeadingOnlyBlock(text: string): boolean {
  return text
    .split("\n")
    .filter((line) => line.trim())
    .every((line) => HEADING_LINE.test(line));
}

/**
 * 可以在其后插图的段落：跳过元数据引用块、已有的图、纯标题块、代码块与短段。
 * 序号仍是全量块的序号，模型只看候选、插入按全量定位。
 */
export function listAnchorBlocks(
  content: string,
  minChars = ANCHOR_MIN_CHARS,
): ContentBlock[] {
  return splitContentBlocks(content).filter((block) => {
    const text = block.text;
    if (text.startsWith(">")) return false;
    if (FENCE_LINE.test(text)) return false;
    if (IMAGE_ONLY_BLOCK.test(text)) return false;
    if (isHeadingOnlyBlock(text)) return false;
    return text.length >= minChars;
  });
}

export function isIllustrationAsset(fileName: string): boolean {
  return fileName.startsWith(ILLUSTRATION_ASSET_PREFIX);
}

/** alt 落在 `![...]` 里，方括号与换行会把图片语法切断 */
function sanitizeAlt(alt: string): string {
  const normalized = alt
    .replace(/[\r\n]+/g, " ")
    .replace(/[[\]]/g, "")
    .trim();
  return normalized || "配图";
}

export function illustrationMarkdown(
  alt: string,
  assetFileName: string,
): string {
  return `![${sanitizeAlt(alt)}](local-image://${assetFileName})`;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function illustrationTokenPattern(assetFileName: string): RegExp {
  return new RegExp(
    `!\\[[^\\]]*\\]\\(local-image://${escapeRegExp(assetFileName)}\\)`,
    "g",
  );
}

/** 正文里由本功能生成的配图（按出现顺序） */
export function listIllustrations(content: string): IllustrationEntry[] {
  const matches = content.matchAll(
    /!\[([^\]]*)\]\(local-image:\/\/(gen-[\w.-]+)\)/g,
  );
  return [...matches].map((match) => ({
    alt: match[1],
    assetFileName: match[2],
  }));
}

/**
 * 某张配图所依附的段落：它前面最近的一个可配图段落。
 *
 * 「重新生成这一张」据此只对那一段重新策划——不必存一份隐藏的 shot 规格，
 * 换来的构图也确实是同一段的另一种画法，而不是同一张图再抽一次。
 */
export function findIllustrationAnchor(
  content: string,
  assetFileName: string,
): ContentBlock | null {
  const blocks = splitContentBlocks(content);
  const imageAt = blocks.findIndex((block) =>
    illustrationTokenPattern(assetFileName).test(block.text),
  );
  if (imageAt < 0) {
    return null;
  }
  const anchors = new Set(
    listAnchorBlocks(content).map((block) => block.index),
  );
  for (let index = imageAt - 1; index >= 0; index--) {
    if (anchors.has(blocks[index].index)) {
      return blocks[index];
    }
  }
  return null;
}

export interface IllustrationInsert {
  afterBlock: number;
  alt: string;
  assetFileName: string;
}

/**
 * 把配图作为独立段落插进正文。
 *
 * 从后往前插：先插前面的段会把后面所有块的行号顶偏。
 * 序号越界（用户在策划与生成之间编辑过正文）时追加到末尾，不丢图。
 */
export function insertIllustrations(
  content: string,
  inserts: IllustrationInsert[],
): string {
  if (inserts.length === 0) {
    return content;
  }
  const blocks = splitContentBlocks(content);
  const lines = content.split("\n");

  for (const insert of [...inserts].sort(
    (a, b) => b.afterBlock - a.afterBlock,
  )) {
    const markdown = illustrationMarkdown(insert.alt, insert.assetFileName);
    const block = blocks[insert.afterBlock];
    const next = block ? block.endLine + 1 : lines.length;
    if (next >= lines.length) {
      lines.push("", markdown);
    } else if (lines[next].trim() === "") {
      lines.splice(next + 1, 0, markdown, "");
    } else {
      lines.splice(next, 0, "", markdown, "");
    }
  }
  return dropRepeatedBlankLines(lines).join("\n").trim();
}

export function removeIllustration(
  content: string,
  assetFileName: string,
): string {
  const pattern = illustrationTokenPattern(assetFileName);
  const lines = content.split("\n").flatMap((line) => {
    const stripped = line.replace(pattern, "");
    if (stripped === line) {
      return [line];
    }
    const rest = stripped.trim();
    // 整行就是这张图，连行一起删掉；行里还有别的内容则只摘走图
    return rest ? [rest] : [];
  });
  return dropRepeatedBlankLines(lines).join("\n").trim();
}

/** 原位换图（重新生成单张），保持它在正文里的位置不变 */
export function replaceIllustration(
  content: string,
  assetFileName: string,
  next: { assetFileName: string; alt: string },
): string {
  return content.replace(
    illustrationTokenPattern(assetFileName),
    illustrationMarkdown(next.alt, next.assetFileName),
  );
}

/** 插入与删除都会留下连续空行，折叠成单个段落间距 */
function dropRepeatedBlankLines(lines: string[]): string[] {
  return lines.filter(
    (line, index) => line.trim() !== "" || lines[index - 1]?.trim() !== "",
  );
}
