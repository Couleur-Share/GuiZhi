/**
 * 正文 HTML → Markdown（Turndown）。在旧配置上补两条规则：
 * - 所有 <pre> 输出围栏代码块：保留原文、不做 Markdown 转义，语言取自 class；
 * - 数据表格输出 GFM 管道表格；布局表格仍按块拆开，避免把整页版面压进一行单元格。
 */
import TurndownService from "turndown";

/** 预处理阶段写在 <pre> 上的语言标记；Readability 会清掉 class，data 属性能保留下来。 */
export const CODE_LANGUAGE_ATTRIBUTE = "data-gz-lang";

const LANGUAGE_PATTERNS = [
  /(?:^|\s)highlight-source-([\w+#.-]+)/,
  /(?:^|\s)(?:language|lang)-([\w+#.-]+)/,
  /(?:^|\s)highlight-([\w+#.-]+)/,
  /(?:^|\s)brush:\s*([\w+#.-]+)/,
];
const NOT_LANGUAGES = new Set(["default", "none", "text", "plain", "plaintext", "nohighlight", "notranslate"]);

/** 从代码块自身、内层 code 与外层两级容器的 class / data-lang 推断语言。 */
export function codeLanguage(pre: Element): string {
  const parent = pre.parentElement;
  for (const node of [pre, pre.querySelector("code"), parent, parent?.parentElement]) {
    if (!node) continue;
    const declared = node.getAttribute("data-lang") ?? node.getAttribute("data-language");
    const classes = node.getAttribute("class") ?? "";
    for (const candidate of [declared, ...LANGUAGE_PATTERNS.map((pattern) => pattern.exec(classes)?.[1])]) {
      const value = candidate?.trim().toLowerCase();
      if (value && /^[a-z0-9_+#.-]{1,30}$/.test(value) && !NOT_LANGUAGES.has(value)) return value;
    }
  }
  return "";
}

const BLOCK_IN_CODE = new Set([
  "ADDRESS", "ARTICLE", "ASIDE", "BLOCKQUOTE", "DD", "DIV", "DL", "DT", "FIGURE", "FOOTER", "H1", "H2", "H3",
  "H4", "H5", "H6", "HEADER", "LI", "OL", "P", "SECTION", "TABLE", "TR", "UL",
]);

/** 代码文字：<br> 与按行排列的块元素换行；其余照搬文本节点（textContent 会把这两类行首尾粘在一起）。 */
function codeText(pre: Node): string {
  let text = "";
  let afterBlock = false;
  const walk = (node: Node) => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === 3) {
        const value = (child as Text).data;
        // 块元素之间用于排版的换行已由块边界表达，不再重复成空行。
        if (afterBlock && /^\s*$/.test(value) && value.includes("\n")) continue;
        text += value;
        afterBlock = false;
      } else if (child.nodeType === 1) {
        if (child.nodeName === "BR") {
          text += "\n";
          afterBlock = false;
          continue;
        }
        const block = BLOCK_IN_CODE.has(child.nodeName);
        if (block && text && !text.endsWith("\n")) text += "\n";
        walk(child);
        if (block) {
          if (!text.endsWith("\n")) text += "\n";
          afterBlock = true;
        }
      }
    }
  };
  walk(pre);
  return text.replace(/\u00a0/g, " ").replace(/^\n+/, "").replace(/\s+$/, "");
}

function ownerTable(node: Node): Element | null {
  for (let parent = node.parentNode; parent; parent = parent.parentNode)
    if (parent.nodeName === "TABLE") return parent as Element;
  return null;
}

function ownRows(table: Element): Element[] {
  return Array.from(table.querySelectorAll("tr")).filter((row) => ownerTable(row) === table);
}

function ownCells(row: Element): Element[] {
  return Array.from(row.children).filter((cell) => cell.nodeName === "TD" || cell.nodeName === "TH");
}

function caption(table: Element): Element | undefined {
  return Array.from(table.children).find((node) => node.nodeName === "CAPTION");
}

function span(cell: Element, name: string, limit: number): number {
  const value = Number.parseInt(cell.getAttribute(name) ?? "1", 10);
  return Number.isFinite(value) && value > 1 ? Math.min(value, limit) : 1;
}

const LAYOUT_BLOCKS = "h1, h2, h3, h4, h5, h6, pre, blockquote, hr, form, table";

/**
 * 数据表格才输出管道表格。判定沿用 Readability 的数据表规则（标题、表头单元格、尺寸），
 * 没有表头的表格再看单元格：含标题、代码、多段落或长文的视为版面布局。
 */
function isDataTable(table: Element): boolean {
  const role = (table.getAttribute("role") ?? "").toLowerCase();
  if (role === "presentation" || role === "none" || table.getAttribute("datatable") === "0") return false;
  if (table.querySelector("table")) return false;
  const rows = ownRows(table);
  const width = Math.max(0, ...rows.map((row) => ownCells(row).reduce((sum, cell) => sum + span(cell, "colspan", 64), 0)));
  if (!rows.length || !width) return false;
  if (table.getAttribute("summary") || caption(table)?.textContent?.trim()) return true;
  if (table.querySelector("th, thead, tfoot, col, colgroup")) return true;
  if (rows.length <= 1 || width <= 1) return false;
  return !rows.some((row) =>
    ownCells(row).some(
      (cell) =>
        !!cell.querySelector(LAYOUT_BLOCKS) ||
        cell.querySelectorAll("p, li").length > 2 ||
        (cell.textContent ?? "").replace(/\s+/g, " ").trim().length > 400,
    ),
  );
}

function cellMarkdown(content: string): string {
  return content.replace(/\s*\n\s*/g, " ").replace(/\|/g, "\\|").trim();
}

function pipeTable(table: Element, converted: WeakMap<Node, string>): string {
  const rows = ownRows(table);
  const grid: string[][] = rows.map(() => []);
  rows.forEach((row, r) => {
    let column = 0;
    for (const cell of ownCells(row)) {
      while (grid[r][column] !== undefined) column++;
      const colspan = span(cell, "colspan", 64);
      const rowspan = span(cell, "rowspan", rows.length - r);
      for (let dr = 0; dr < rowspan; dr++)
        for (let dc = 0; dc < colspan; dc++)
          grid[r + dr][column + dc] = dr || dc ? "" : cellMarkdown(converted.get(cell) ?? "");
      column += colspan;
    }
  });
  const body = grid.filter((cells) => cells.some((cell) => cell));
  const width = Math.max(0, ...body.map((cells) => cells.length));
  if (!body.length || !width) return "";
  const line = (cells: string[]) => `| ${Array.from({ length: width }, (_, i) => cells[i] ?? "").join(" | ")} |`;
  const node = caption(table);
  const title = node ? cellMarkdown(converted.get(node) ?? "") : "";
  return [
    "\n\n",
    title ? `${title}\n\n` : "",
    line(body[0]),
    `\n| ${Array(width).fill("---").join(" | ")} |`,
    ...body.slice(1).map((cells) => `\n${line(cells)}`),
    "\n\n",
  ].join("");
}

export function htmlToMarkdown(html: string): string {
  const kinds = new WeakMap<Element, boolean>();
  const converted = new WeakMap<Node, string>();
  const isPipe = (table: Element | null) => {
    if (!table) return false;
    let kind = kinds.get(table);
    if (kind === undefined) kinds.set(table, (kind = isDataTable(table)));
    return kind;
  };
  const inPipeCell = (node: Node) => {
    for (let parent = node.parentNode; parent; parent = parent.parentNode)
      if (parent.nodeName === "TD" || parent.nodeName === "TH") return isPipe(ownerTable(parent));
    return false;
  };
  const turndown = new TurndownService({ headingStyle: "atx", codeBlockStyle: "fenced", bulletListMarker: "-" });
  // 内联 SVG 不移除：图表里的文字标签可能是正文。
  turndown.remove(["script", "style", "noscript", "iframe", "template", "canvas", "button", "select", "textarea", "input"]);
  turndown.addRule("preformatted", {
    filter: "pre",
    replacement: (_content, node) => {
      const text = codeText(node);
      if (!text.trim()) return "";
      // 管道表格的单元格只能放一行：表格里的代码块降级为行内代码。
      if (inPipeCell(node)) {
        const inline = text.replace(/\s+/g, " ").trim();
        const ticks = "`".repeat(Math.max(1, ...(inline.match(/`+/g) ?? []).map((run) => run.length + 1)));
        const pad = ticks.length > 1 ? " " : "";
        return `${ticks}${pad}${inline}${pad}${ticks}`;
      }
      const fence = "`".repeat(Math.max(3, ...(text.match(/`+/g) ?? []).map((run) => run.length + 1)));
      const language = node.getAttribute(CODE_LANGUAGE_ATTRIBUTE) ?? "";
      return `\n\n${fence}${language}\n${text}\n${fence}\n\n`;
    },
  });
  turndown.addRule("pipeTableCell", {
    filter: (node) =>
      node.nodeName === "CAPTION"
        ? node.parentNode?.nodeName === "TABLE" && isPipe(node.parentNode as Element)
        : (node.nodeName === "TD" || node.nodeName === "TH") && isPipe(ownerTable(node)),
    // 单元格先按普通规则转换、暂存内容，由所在表格统一排成网格。
    replacement: (content, node) => {
      converted.set(node, content);
      return "";
    },
  });
  turndown.addRule("pipeTable", {
    filter: (node) => node.nodeName === "TABLE" && isPipe(node),
    replacement: (_content, node) => pipeTable(node, converted),
  });
  return turndown.turndown(html).trim();
}
