/**
 * 把回答正文里的 `[1]` 引用标记改写成可点击的锚点链接。
 *
 * 模型按提示词在句末标 `[1]`，而这些标记此前是纯文本：屏幕上和底部那排
 * 来源 chip 各说各的，读到一句话想看它的依据只能自己去下面数编号。
 */

export const QA_CITE_HREF_PREFIX = "#qa-cite=";

/** 与 qa.ts 的 extractCitedOrdinals 同一套口径，兼容全角括号与多编号 */
const CITATION_MARKER = /[[【]\s*(\d{1,3}(?:\s*[,，、]\s*\d{1,3})*)\s*[\]】]/g;

/** 原始编号 → 展示编号 */
export type CitationLabels = ReadonlyMap<number, number>;

function parseOrdinals(group: string): number[] | null {
  const ordinals = group
    .split(/[,，、]/)
    .map((token) => Number.parseInt(token.trim(), 10));
  return ordinals.some((value) => !Number.isFinite(value)) ? null : ordinals;
}

/** 逐行改写标记；围栏代码块与行内代码里的方括号数字不是引用（正则、数组下标都长这样） */
function rewriteOutsideCode(
  markdown: string,
  rewrite: (match: string, ordinals: number[]) => string,
): string {
  const rewriteSegment = (segment: string) =>
    segment.replace(CITATION_MARKER, (match, group: string) => {
      const ordinals = parseOrdinals(group);
      return ordinals ? rewrite(match, ordinals) : match;
    });
  let inFence = false;
  return markdown
    .split("\n")
    .map((line) => {
      if (/^\s*(```|~~~)/.test(line)) {
        inFence = !inFence;
        return line;
      }
      if (inFence) return line;
      return line
        .split(/(`[^`]*`)/g)
        .map((segment) => (segment.startsWith("`") ? segment : rewriteSegment(segment)))
        .join("");
    })
    .join("\n");
}

/**
 * 展示编号：按正文里首次出现的顺序从 1 连续编号，正文未提到的来源排在其后。
 * 智能体检索按发现顺序给资料编号、只有读过的才算来源，原始编号常是 [3][5][12]。
 */
export function buildCitationLabels(
  markdown: string,
  ordinals: readonly number[],
): CitationLabels {
  const valid = new Set(ordinals);
  const order: number[] = [];
  const seen = new Set<number>();
  const visit = (ordinal: number) => {
    if (valid.has(ordinal) && !seen.has(ordinal)) {
      seen.add(ordinal);
      order.push(ordinal);
    }
  };
  if (markdown) {
    rewriteOutsideCode(markdown, (match, found) => {
      found.forEach(visit);
      return match;
    });
  }
  ordinals.forEach(visit);
  return new Map(order.map((ordinal, index) => [ordinal, index + 1]));
}

/**
 * 给了展示编号时，指不到任何来源的编号直接去掉：留着原数字会和重排后的编号撞号，
 * 让读者以为它指向另一份资料。
 */
function relabel(
  ordinals: number[],
  valid: Set<number>,
  labels: CitationLabels | undefined,
): Array<{ ordinal: number; label: number; linked: boolean }> {
  return ordinals
    .filter((ordinal) => !labels || valid.has(ordinal))
    .map((ordinal) => ({
      ordinal,
      label: labels?.get(ordinal) ?? ordinal,
      linked: valid.has(ordinal),
    }));
}

export function linkifyCitations(
  markdown: string,
  validOrdinals: Set<number>,
  labels?: CitationLabels,
): string {
  if (validOrdinals.size === 0 || !markdown) {
    return markdown;
  }
  return rewriteOutsideCode(markdown, (match, ordinals) => {
    // 没有展示编号时，没有对应来源的编号原样留着：做成死链比不可点更糟
    if (!ordinals.some((value) => validOrdinals.has(value))) {
      return labels ? "" : match;
    }
    const parts = relabel(ordinals, validOrdinals, labels);
    if (parts.length === 1) {
      // 单编号时整个 `[1]` 都可点，链接文字里的方括号要转义成字面量
      return `[\\[${parts[0].label}\\]](${QA_CITE_HREF_PREFIX}${parts[0].ordinal})`;
    }
    const inner = parts
      .map((part) =>
        part.linked
          ? `[${part.label}](${QA_CITE_HREF_PREFIX}${part.ordinal})`
          : String(part.label),
      )
      .join(", ");
    return `\\[${inner}\\]`;
  });
}

/** 复制回答时与屏幕上的编号保持一致 */
export function renumberCitations(
  markdown: string,
  validOrdinals: Set<number>,
  labels: CitationLabels,
): string {
  if (validOrdinals.size === 0 || !markdown) {
    return markdown;
  }
  return rewriteOutsideCode(markdown, (_match, ordinals) => {
    if (!ordinals.some((value) => validOrdinals.has(value))) return "";
    return `[${relabel(ordinals, validOrdinals, labels)
      .map((part) => part.label)
      .join(", ")}]`;
  });
}
