/** 统一折叠空白，DOM 选段与 Markdown 正文可按相同文字匹配。 */
export const normalizeArticleText = (value: string) => value.replace(/!\[[^\]]*\]\([^)]*\)/g, "").replace(/\[([^\]]+)\]\([^)]*\)/g, "$1").replace(/[*_`#>]/g, "").replace(/\s+/g, " ").trim();

/** 优先保留选段及邻段，再按中英文词/双字匹配补充；无需全库索引。 */
export function selectArticleContext(text: string, question: string, selection = "", budget = 12000): { text: string; clipped: boolean } {
  const blocks = text.replace(/\r\n/g, "\n").split(/\n\s*\n/).flatMap(p => p.match(/[\s\S]{1,1200}/g) ?? []).filter(p => normalizeArticleText(p));
  if (text.length <= budget) return { text, clipped: false };
  const selected = normalizeArticleText(selection);
  const query = normalizeArticleText(question + " " + selection).toLowerCase();
  const terms = [...new Set(query.match(/[a-z0-9]{2,}|[\u3400-\u9fff]{2}/g) ?? [])];
  const ranked = blocks.map((block, i) => {
    const normalized = normalizeArticleText(block).toLowerCase();
    return { i, score: terms.reduce((n, term) => n + (normalized.includes(term) ? 1 : 0), 0) };
  }).sort((a, b) => b.score - a.score || a.i - b.i);
  const chosen = new Set<number>();
  let size = 0;
  // 选段可能跨越分块；单独保留它，匹配到的相邻块也优先进入。
  const lead = selected ? `选中段落：\n${selection.slice(0, 4000)}\n\n` : "";
  size += lead.length;
  const add = (i: number) => { if (i >= 0 && i < blocks.length && !chosen.has(i) && size + blocks[i].length <= budget) { chosen.add(i); size += blocks[i].length + 2; } };
  const anchor = selected ? blocks.findIndex(p => normalizeArticleText(p).includes(selected.slice(0, 80))) : -1;
  if (anchor >= 0) [anchor, anchor - 1, anchor + 1].forEach(add);
  ranked.forEach(r => add(r.i));
  return { text: lead + [...chosen].sort((a, b) => a - b).map(i => blocks[i]).join("\n\n"), clipped: true };
}
