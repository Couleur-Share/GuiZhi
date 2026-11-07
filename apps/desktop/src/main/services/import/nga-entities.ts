/** NGA 数据里的 HTML 实体解码 */

export function decodeHtmlEntities(value: string): string {
  return value
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&nbsp;/g, " ");
}

function decodeNumericEntities(value: string): string {
  const fromCode = (match: string, code: number) =>
    Number.isInteger(code) && code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : match;
  return value
    .replace(/&#(\d{1,7});/g, (match, digits: string) => fromCode(match, Number(digits)))
    .replace(/&#x([0-9a-f]{1,6});/gi, (match, hex: string) => fromCode(match, Number.parseInt(hex, 16)));
}

/** 帖子标题在 NGA 数据里常被转义两次（&amp;amp;），按轮解码到不再变化；标题按纯文本展示 */
export function decodeNgaSubject(value: string): string {
  let current = value;
  for (let pass = 0; pass < 3; pass += 1) {
    const next = decodeNumericEntities(decodeHtmlEntities(current));
    if (next === current) break;
    current = next;
  }
  return current;
}
