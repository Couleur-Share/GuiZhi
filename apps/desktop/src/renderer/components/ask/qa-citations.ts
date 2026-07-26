/**
 * 把回答正文里的 `[1]` 引用标记改写成可点击的锚点链接。
 *
 * 模型按提示词在句末标 `[1]`，而这些标记此前是纯文本：屏幕上和底部那排
 * 来源 chip 各说各的，读到一句话想看它的依据只能自己去下面数编号。
 */

export const QA_CITE_HREF_PREFIX = "#qa-cite=";

/** 与 qa.ts 的 extractCitedOrdinals 同一套口径，兼容全角括号与多编号 */
const CITATION_MARKER = /[[【]\s*(\d{1,3}(?:\s*[,，、]\s*\d{1,3})*)\s*[\]】]/g;

function rewriteMarkers(text: string, valid: Set<number>): string {
  return text.replace(CITATION_MARKER, (match, group: string) => {
    const ordinals = group
      .split(/[,，、]/)
      .map((token) => Number.parseInt(token.trim(), 10));
    if (ordinals.some((value) => !Number.isFinite(value))) {
      return match;
    }
    // 没有对应来源的编号原样留着：做成死链比不可点更糟
    if (!ordinals.some((value) => valid.has(value))) {
      return match;
    }
    if (ordinals.length === 1) {
      // 单编号时整个 `[1]` 都可点，链接文字里的方括号要转义成字面量
      return `[\\[${ordinals[0]}\\]](${QA_CITE_HREF_PREFIX}${ordinals[0]})`;
    }
    const inner = ordinals
      .map((value) =>
        valid.has(value)
          ? `[${value}](${QA_CITE_HREF_PREFIX}${value})`
          : String(value),
      )
      .join(", ");
    return `\\[${inner}\\]`;
  });
}

/** 行内代码里的方括号数字不是引用（正则、数组下标都长这样），跳过 */
function rewriteLine(line: string, valid: Set<number>): string {
  return line
    .split(/(`[^`]*`)/g)
    .map((segment) =>
      segment.startsWith("`") ? segment : rewriteMarkers(segment, valid),
    )
    .join("");
}

export function linkifyCitations(
  markdown: string,
  validOrdinals: Set<number>,
): string {
  if (validOrdinals.size === 0 || !markdown) {
    return markdown;
  }
  let inFence = false;
  return markdown
    .split("\n")
    .map((line) => {
      if (/^\s*(```|~~~)/.test(line)) {
        inFence = !inFence;
        return line;
      }
      return inFence ? line : rewriteLine(line, validOrdinals);
    })
    .join("\n");
}
