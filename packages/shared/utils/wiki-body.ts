/**
 * Wiki 正文的确定性清洗与结构抽取。
 *
 * 提示词约束不住模型排版——这一点在论坛总结上已经吃过一次亏
 * （见 sanitizeForumSummary）。Wiki 这边的表现是：详情页头部已经渲染了
 * page.title，模型又在 body 第一行放一个同名的一级标题，页面上同一个标题
 * 上下叠两遍。清洗放在这里，落库前与渲染时共用同一份实现。
 */

/**
 * 标题比对用的规范化：忽略大小写、强调符、全角/半角括号差异。
 *
 * 空白是整个去掉而不是折叠成一个——中英混排时半角括号前后习惯留空格
 * （`INP (下次绘制交互)`），全角括号则不留（`INP（下次绘制交互）`），
 * 折叠空白的话这两种写法仍然不相等，标题去重就会漏掉一半。
 */
function normalizeHeadingText(text: string): string {
  return text
    .replace(/[（）]/g, (char) => (char === "（" ? "(" : ")"))
    .replace(/[【】]/g, (char) => (char === "【" ? "[" : "]"))
    .replace(/[*_`]/g, "")
    .replace(/\s+/g, "")
    .toUpperCase();
}

const HEADING_LINE = /^(#{1,6})\s+(.*)$/;

/**
 * 剥掉正文开头与页面标题重复的标题行。
 *
 * 只处理正文的第一个非空行，且只在它是标题、文字与页面标题一致时才剥——
 * 正文中间出现的同名小标题是作者的结构选择，不该动。
 */
export function stripDuplicateTitleHeading(body: string, title: string): string {
  const wanted = normalizeHeadingText(title);
  if (!wanted) {
    return body;
  }
  const lines = body.split(/\r?\n/);
  const firstIndex = lines.findIndex((line) => line.trim().length > 0);
  if (firstIndex < 0) {
    return body;
  }
  const match = HEADING_LINE.exec(lines[firstIndex].trim());
  if (!match || normalizeHeadingText(match[2]) !== wanted) {
    return body;
  }
  // 连同紧随其后的空行一起去掉，避免正文顶部留一段空白
  let nextIndex = firstIndex + 1;
  while (nextIndex < lines.length && lines[nextIndex].trim().length === 0) {
    nextIndex += 1;
  }
  return lines.slice(nextIndex).join("\n");
}

export interface WikiTocEntry {
  /** 标题层级：2 或 3（一级标题已被上面的清洗剥掉，更深的层级不进目录） */
  level: number;
  text: string;
  /** 与 WikiMarkdown 渲染出的 heading id 一致 */
  slug: string;
}

/** 标题文字 → DOM id。同名标题追加序号，保证页内锚点唯一。 */
export function slugifyHeading(text: string, occurrence = 0): string {
  const base =
    text
      .trim()
      .replace(/[*_`[\]()]/g, "")
      .replace(/\s+/g, "-")
      .replace(/[^\p{L}\p{N}-]/gu, "")
      .toLowerCase() || "section";
  return occurrence > 0 ? `${base}-${occurrence}` : base;
}

/**
 * 抽取页内目录（二、三级标题）。
 *
 * 跳过围栏代码块内的 `#` 开头行——Shell 注释和 Markdown 标题长得一样，
 * 不跳过的话代码块里的每行注释都会变成一个目录项。
 */
export function extractWikiToc(body: string): WikiTocEntry[] {
  const entries: WikiTocEntry[] = [];
  const used = new Map<string, number>();
  let inFence = false;

  for (const rawLine of body.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (/^(```|~~~)/.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) {
      continue;
    }
    const match = HEADING_LINE.exec(line);
    if (!match) {
      continue;
    }
    const level = match[1].length;
    const text = match[2].trim().replace(/\s*#+\s*$/, "");
    if (level < 2 || level > 3 || !text) {
      continue;
    }
    const base = slugifyHeading(text);
    const occurrence = used.get(base) ?? 0;
    used.set(base, occurrence + 1);
    entries.push({ level, text, slug: slugifyHeading(text, occurrence) });
  }

  return entries;
}
