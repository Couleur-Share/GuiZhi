/**
 * 网页正文提取（纯函数，在 worker 线程中运行，不访问网络与文件）。
 * Readability 选正文；它明显截断或选错时回退到清洗后的语义正文区域。
 * 结果字段、错误分类与 20 万字符截断沿用此前随包提取器的约定，已入库的原文版本按同一契约比较。
 */
import { createHash } from "node:crypto";
import { Readability } from "@mozilla/readability";
import { parseHTML } from "linkedom";
import type { WebCaptureResult } from "@guizhi/shared/types";
import { CODE_LANGUAGE_ATTRIBUTE, codeLanguage, htmlToMarkdown } from "./web-extract-markdown";

export type WebExtracted = Omit<
  WebCaptureResult,
  "taskId" | "entryUrl" | "finalUrl" | "links" | "capturedAt" | "engineVersion"
>;
/** 提取结果与正文来源；来源与段落字数只供回归和诊断，不进入采集结果。 */
export interface WebArticle {
  result: WebExtracted;
  source: "readability" | "region" | "wechat" | "none";
  readabilityChars: number;
  regionChars: number;
}

const MARKDOWN_LIMIT = 200_000;
// Readability 取到的段落文字不足语义区域的 60% 视为截断或选错；区域段落不足 500 字时差值不可靠，不回退。
// 59 页评估样本与 47 页留出样本：截断页 ≤0.52，正常页 ≥0.85（区域段落 500 字以上的页面）。
const FALLBACK_RATIO = 0.6;
const FALLBACK_MIN_CHARS = 500;
// 语义区域本身的下限：防止把文末标签块之类的短容器当成正文（爱范儿的 id="article-content"）。
const REGION_MIN_CHARS = 200;
// 标题区：主标题所在、文字不超过 1500 字的容器；导语段落至少 30 字。
const TITLE_BLOCK_MAX_CHARS = 1500;
const LEAD_MIN_CHARS = 30;
// Readability 条件清理默认删除链接占比超过 0.2 的块，会连带删掉「标题链接 + 一句说明」的卡片、
// Sphinx「See also」框与文末导读段。放宽 0.1（选项见 Readability 源码，类型声明未列出）；
// 59 页评估样本上新闻页的推荐与订阅区不受影响（它们的链接占比接近 1）。
const LINK_DENSITY_MODIFIER = 0.1;
const HEADING_MARK = "data-gz-h1";
const CONTENT_CLASSES = new Set(["entry-content", "post-content", "article-content", "post-body"]);
const CHROME_NAMES = new Set([
  "sidebar", "widget", "widget-area", "comments", "comment-list", "comment-respond", "respond",
  "disqus_thread", "related-posts", "related-articles", "post-navigation", "social-share", "share-buttons",
]);
// 区域回退时额外清理的模板容器记号（class/id 词元）；含代码、表格或主标题的容器保留。
const CHROME_TOKENS =
  /(?:^|[\s_-])(?:sidebar|widget|comments?|comment-list|respond|disqus|related|recommend(?:ed|ation)?s?|share|sharing|social|newsletter|subscribe|promo|advert(?:isement)?|ads?|breadcrumbs?|pagination|pager|toc|cookie|consent|feedback)(?:$|[\s_-])/i;
const PERMALINK_TEXT = /^[\s\u200b#¶§🔗∞]*$/u;
const HEADING_ANCHORS = ["h1", "h2", "h3", "h4", "h5", "h6"].map((tag) => `${tag} a[href^="#"]`).join(", ");
const LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);

function parsePage(html: string): Document {
  const { document } = parseHTML(
    /<html(?:\s|>)/i.test(html) ? html : `<html><body>${html}</body></html>`,
  );
  return document as unknown as Document;
}

const textLength = (node: Node | null | undefined) =>
  (node?.textContent ?? "").replace(/\s+/g, "").length;

/**
 * 段落文字量：只计表格之外 <p> 里的文字。判断截断只比正文段落——数据表（如浏览器兼容表）、
 * 没有标成导航的链接目录与代码编辑器都会撑大语义区域，却不是 Readability 漏掉的正文。
 */
function paragraphLength(node: Element | null): number {
  let total = 0;
  const pending: [Node, boolean][] = node ? [[node, false]] : [];
  while (pending.length) {
    const [current, inParagraph] = pending.pop()!;
    for (const child of Array.from(current.childNodes)) {
      if (child.nodeType === 3 && inParagraph) total += (child.textContent ?? "").replace(/\s+/g, "").length;
      else if (child.nodeType === 1 && child.nodeName !== "TABLE")
        pending.push([child, inParagraph || child.nodeName === "P"]);
    }
  }
  return total;
}

function absoluteUrl(value: string | null | undefined, base: string): URL | null {
  if (!value?.trim()) return null;
  try {
    return new URL(value.trim(), base);
  } catch {
    return null;
  }
}

interface PageMeta {
  title: string;
  author: string;
  publishedAt: number | null;
  requiresLogin: boolean;
  baseUrl: string;
}

function publishedTime(value: string | null | undefined): number | null {
  // 与原提取器一致：只接受带时区的 ISO 时间。
  const text = value?.trim().replace(/Z$/i, "+00:00").replace(/([+-]\d\d)(\d\d)$/, "$1:$2");
  if (!text || !/[+-]\d\d:\d\d$/.test(text)) return null;
  const time = Date.parse(text);
  return Number.isFinite(time) ? time : null;
}

function readMeta(document: Document, url: string): PageMeta {
  const titleNode = document.querySelector("head > title") ?? document.querySelector("title");
  let title = (titleNode?.textContent ?? "").replace(/\s+/g, " ").trim();
  if (!title) title = (document.querySelector("h1")?.textContent ?? "").replace(/\s+/g, " ").trim();
  title = title.slice(0, 300);
  const base = absoluteUrl(document.querySelector("base[href]")?.getAttribute("href"), url);
  return {
    title,
    author: Array.from(document.querySelectorAll('meta[name="author"]'))
      .map((node) => node.getAttribute("content") ?? "")
      .join(" ")
      .trim()
      .slice(0, 300),
    publishedAt: publishedTime(
      document.querySelector('meta[property="article:published_time"]')?.getAttribute("content"),
    ),
    requiresLogin:
      !!document.querySelector('input[type="password"]') && /登录|登入|sign in|log in|login/i.test(title),
    baseUrl: base && /^https?:$/.test(base.protocol) ? base.href : url,
  };
}

/** 渲染快照里的 CodeMirror（6 与 5）只剩逐行的 DOM：按行拼回一个代码块，否则会被当成零散文字删掉。 */
function restoreEditorLines(document: Document): void {
  for (const editor of Array.from(document.querySelectorAll(".cm-editor, .CodeMirror"))) {
    if (!editor.isConnected) continue;
    const lines = Array.from(editor.querySelectorAll(".cm-line, .CodeMirror-line"));
    if (!lines.length) continue;
    const pre = document.createElement("pre");
    const code = document.createElement("code");
    code.textContent = lines.map((line) => line.textContent ?? "").join("\n");
    pre.appendChild(code);
    editor.replaceWith(pre);
  }
}

/** 折叠与标签页里的内容是正文（如各包管理器的安装命令、版本历史）：去掉隐藏标记，Readability 才会保留。 */
function revealDisclosures(document: Document): void {
  const selector = ["details", "[role='tabpanel']"]
    .flatMap((scope) => [`${scope}[hidden]`, `${scope}[aria-hidden='true']`, `${scope}[style]`, `${scope} [hidden]`, `${scope} [aria-hidden='true']`, `${scope} [style]`])
    .join(", ");
  for (const node of Array.from(document.querySelectorAll(selector))) {
    node.removeAttribute("hidden");
    if (node.getAttribute("aria-hidden") === "true") node.removeAttribute("aria-hidden");
    const style = node.getAttribute("style");
    if (style && /display\s*:\s*none|visibility\s*:\s*hidden/i.test(style))
      node.setAttribute("style", style.replace(/(?:display\s*:\s*none|visibility\s*:\s*hidden)\s*;?/gi, ""));
  }
}

interface TitleBlock {
  heading: string;
  headingText: string;
  leads: { html: string; text: string }[];
}

const normalized = (text: string | null | undefined) => (text ?? "").replace(/\s+/g, "");

/**
 * 主标题所在的小容器（标题区）里的 h1 与导语段落。Readability 常把整个标题区当页面框架删掉
 * （如 MDN 新版式的 layout__header，class 含 header），导语随之丢失；只认文字不多、带导语的容器。
 */
function titleBlock(document: Document): TitleBlock | null {
  const heading = document.querySelector("h1");
  // 只认正文主区域（main / article）里的标题区；站点页眉里的站名与标语不补。
  if (!heading?.closest("main, [role='main'], article")) return null;
  for (let node = heading.parentElement; node && !["BODY", "MAIN", "ARTICLE"].includes(node.nodeName); node = node.parentElement) {
    if (textLength(node) > TITLE_BLOCK_MAX_CHARS) return null;
    let after = false;
    const leads: TitleBlock["leads"] = [];
    for (const child of Array.from(node.querySelectorAll("h1, p"))) {
      if (child === heading) after = true;
      // 标题区里折叠框、侧栏等组件的说明（如 MDN 的浏览器支持徽标）不是导语。
      else if (after && child.nodeName === "P" && textLength(child) >= LEAD_MIN_CHARS && leads.length < 3 && !child.closest("details, aside, nav, footer, form, figure"))
        leads.push({ html: child.outerHTML, text: normalized(child.textContent) });
    }
    if (leads.length) return { heading: heading.outerHTML, headingText: normalized(heading.textContent), leads };
  }
  return null;
}

/** 正文里找不到标题区的导语时，把主标题与导语补回正文开头。 */
function restoreTitleBlock(article: Element, block: TitleBlock | null): void {
  if (!block) return;
  const text = normalized(article.textContent);
  const missing = block.leads.filter((lead) => !text.includes(lead.text));
  if (!missing.length) return;
  const holder = article.ownerDocument.createElement("div");
  holder.innerHTML = (text.includes(block.headingText) ? "" : block.heading) + missing.map((lead) => lead.html).join("");
  article.insertBefore(holder, article.firstChild);
}

/** Readability 会清掉 class、把 h1 降为 h2：先把代码语言与 h1 记在 data 属性上。 */
function prepare(document: Document): void {
  // 脚本与样式从不是正文；先删掉，文字量统计才不会被展开 shadow DOM 带进来的样式表撑大。
  document.querySelectorAll("script, style").forEach((node) => node.remove());
  restoreEditorLines(document);
  revealDisclosures(document);
  for (const pre of Array.from(document.querySelectorAll("pre"))) {
    const language = codeLanguage(pre);
    if (language) pre.setAttribute(CODE_LANGUAGE_ATTRIBUTE, language);
  }
  for (const heading of Array.from(document.querySelectorAll("h1"))) heading.setAttribute(HEADING_MARK, "");
  // 标题里的段落锚点（¶、#、零宽字符或空文字）只是页面跳转按钮，转成 Markdown 后是噪声。
  for (const anchor of Array.from(document.querySelectorAll(HEADING_ANCHORS)))
    if (PERMALINK_TEXT.test(anchor.textContent ?? "") && !anchor.querySelector("img")) anchor.remove();
}

const hasContentClass = (node: Element) =>
  node.getAttribute("itemprop") === "articleBody" ||
  (node.getAttribute("class") ?? "").toLowerCase().split(/\s+/).some((name) => CONTENT_CLASSES.has(name));
const isContentNode = (node: Element) => node.nodeName === "ARTICLE" || hasContentClass(node);

function innermostUnique(nodes: Element[]): Element | null {
  const leaves = nodes.filter((node) => !nodes.some((other) => other !== node && node.contains(other)));
  return leaves.length === 1 ? leaves[0] : null;
}

/** 语义正文区域：先认正文 class（只看 class，不看 id），再认唯一的 main / article；都要有文字长度下限。 */
function semanticRegion(document: Document): Element | null {
  const byClass = Array.from(document.querySelectorAll("[class], [itemprop]")).filter(hasContentClass);
  for (const list of [byClass, Array.from(document.querySelectorAll("main, article, [role='main']"))]) {
    const region = innermostUnique(list);
    if (region && textLength(region) >= REGION_MIN_CHARS) return region;
  }
  return null;
}

/** 清理语义区域里的模板容器：边栏、评论、推荐、订阅、分享，以及正文之外的导航与页眉页脚。 */
function cleanRegion(region: Element): Element {
  // 可展开的数据表把单元格内容包在 <button> 里（如 MDN 兼容表）：拆成普通文字后再清理按钮。
  // 只在区域回退里做：交给 Readability 的文档若保留这些文字，单元格得分会让它把表格内部当成正文。
  for (const button of Array.from(region.querySelectorAll("td button, th button"))) {
    const span = region.ownerDocument.createElement("span");
    while (button.firstChild) span.appendChild(button.firstChild);
    button.replaceWith(span);
  }
  region
    .querySelectorAll("script, style, noscript, template, form, button, input, select, textarea, [hidden], [aria-hidden='true']")
    .forEach((node) => node.remove());
  for (const node of Array.from(region.querySelectorAll("*"))) {
    if (!region.contains(node)) continue;
    const names = `${node.getAttribute("class") ?? ""} ${node.getAttribute("id") ?? ""}`.toLowerCase();
    if (
      names.split(/\s+/).some((name) => CHROME_NAMES.has(name)) ||
      node.getAttribute("role") === "complementary" ||
      (CHROME_TOKENS.test(names) && !node.querySelector("pre, table, h1, h2"))
    )
      node.remove();
  }
  // 页面级导航、页眉页脚可以删；正文（article / 正文 class）内的保留。
  for (const node of Array.from(region.querySelectorAll("aside, nav, footer, header"))) {
    if (!region.contains(node)) continue;
    let inside = false;
    for (let parent = node.parentElement; parent && !inside; parent = parent.parentElement)
      inside = isContentNode(parent);
    if (!inside) node.remove();
  }
  return region;
}

/** 公众号正文初始隐藏、图片由脚本填充；明确正文区域不依赖脚本执行成功。 */
function wechatRoot(document: Document, url: string): Element | null {
  if (absoluteUrl(url, url)?.hostname !== "mp.weixin.qq.com") return null;
  const root = document.querySelector("#js_content");
  if (!root) return null;
  root.removeAttribute("style");
  for (const image of Array.from(root.querySelectorAll("img[data-src]"))) {
    const source = absoluteUrl(image.getAttribute("data-src"), url);
    if (source && /^https?:$/.test(source.protocol)) image.setAttribute("src", source.href);
  }
  return root;
}

/** 正文脱离原页面保存：链接与图片改成绝对地址；解析不出的地址、脚本链接与内联图片不保留。 */
function absolutize(root: Element, base: string): void {
  for (const link of Array.from(root.querySelectorAll("a[href]"))) {
    const target = absoluteUrl(link.getAttribute("href"), base);
    if (target && LINK_PROTOCOLS.has(target.protocol)) link.setAttribute("href", target.href);
    else link.removeAttribute("href");
  }
  for (const image of Array.from(root.querySelectorAll("img"))) {
    const declared = image.getAttribute("src")?.trim();
    const lazy = ["data-src", "data-original", "data-lazy-src"].map((name) => image.getAttribute(name)).find(Boolean);
    const candidate = !declared || /^data:/i.test(declared) ? lazy || image.getAttribute("srcset")?.trim().split(/\s+/)[0] : declared;
    const source = absoluteUrl(candidate, base);
    if (source && /^https?:$/.test(source.protocol)) image.setAttribute("src", source.href);
    else image.remove();
  }
}

function restoreHeadings(root: Element): void {
  for (const heading of Array.from(root.querySelectorAll(`h2[${HEADING_MARK}]`))) {
    const h1 = heading.ownerDocument.createElement("h1");
    while (heading.firstChild) h1.appendChild(heading.firstChild);
    heading.replaceWith(h1);
  }
}

function readabilityContent(document: Document): Element | null {
  try {
    const reader = new Readability<Node>(document, {
      charThreshold: 100,
      serializer: (node) => node,
      linkDensityModifier: LINK_DENSITY_MODIFIER,
    } as ConstructorParameters<typeof Readability<Node>>[1]);
    // Readability 会删掉与标题重复的首个 h1/h2；保留它，使正文与原页面一样以主标题开头。
    const internals = reader as unknown as { _headerDuplicatesTitle?: () => boolean };
    if (typeof internals._headerDuplicatesTitle === "function") internals._headerDuplicatesTitle = () => false;
    const content = reader.parse()?.content;
    return content && content.nodeType === 1 ? (content as Element) : null;
  } catch {
    return null;
  }
}

/** 按 Unicode 码点截断（与此前提取器的字符计数一致，也不会切断代理对）。 */
function truncate(markdown: string): { markdown: string; truncated: boolean } {
  if (markdown.length <= MARKDOWN_LIMIT) return { markdown, truncated: false };
  let count = 0;
  let end = 0;
  for (const char of markdown) {
    if (count === MARKDOWN_LIMIT) return { markdown: markdown.slice(0, end), truncated: true };
    count++;
    end += char.length;
  }
  return { markdown, truncated: false };
}

function classify(status: number, requiresLogin: boolean, title: string, text: string, markdown: string) {
  if (status === 401 || status === 407 || requiresLogin) return { code: "login", message: "网页需要登录" } as const;
  if (status === 403 || status === 429) return { code: "restricted", message: "网站拒绝访问或限制请求频率" } as const;
  if (status >= 400) return { code: "network", message: `网站返回 HTTP ${status}` } as const;
  if (/^(just a moment|verify you are human|人机验证|安全验证)/i.test(title))
    return { code: "captcha", message: "网页要求完成验证码" } as const;
  if (!text || !markdown) return { code: "empty", message: "网页没有可保存的正文" } as const;
  return null;
}

export function extractWebArticle(html: string, url: string, status: number): WebArticle {
  const document = parsePage(html || "");
  const meta = readMeta(document, url);
  let title = meta.title;
  let source: WebArticle["source"] = "readability";
  let readabilityChars = 0;
  let regionChars = 0;
  prepare(document);
  let root = wechatRoot(document, url);
  if (root) {
    source = "wechat";
    title = (
      (document.querySelector("#activity-name")?.textContent ?? "").trim() ||
      (document.querySelector('meta[property="og:title"]')?.getAttribute("content") ?? "").trim() ||
      title
    ).slice(0, 300);
  } else {
    // Readability 会改写传入的文档，语义区域与标题区取自另一份解析结果。
    const regionDocument = parsePage(html || "");
    prepare(regionDocument);
    const block = titleBlock(regionDocument);
    const region = semanticRegion(regionDocument);
    const article = readabilityContent(document);
    const cleaned = region ? cleanRegion(region) : null;
    readabilityChars = paragraphLength(article);
    regionChars = paragraphLength(cleaned);
    if (cleaned && regionChars >= FALLBACK_MIN_CHARS && readabilityChars < FALLBACK_RATIO * regionChars) {
      root = cleaned;
      source = "region";
    } else {
      root = article;
      if (root) restoreTitleBlock(root, block);
    }
  }
  let markdown = "";
  let text = "";
  if (root) {
    root.querySelectorAll("script, style, noscript, template").forEach((node) => node.remove());
    absolutize(root, meta.baseUrl);
    restoreHeadings(root);
    text = (root.textContent ?? "").trim();
    markdown = htmlToMarkdown(root.innerHTML);
  } else source = "none";
  const error = classify(status, meta.requiresLogin, title, text, markdown);
  const clipped = truncate(markdown);
  return {
    result: {
      title: title || url,
      author: meta.author,
      publishedAt: meta.publishedAt,
      dateConfidence: meta.publishedAt ? "exact" : "unknown",
      markdown: clipped.markdown,
      paragraphs: clipped.markdown
        .split(/\n\s*\n/)
        .flatMap((paragraph, index) => (paragraph.trim() ? [{ id: `p${index + 1}`, text: paragraph }] : [])),
      contentHash: createHash("sha256").update(clipped.markdown).digest("hex"),
      complete: !error && !clipped.truncated,
      truncated: clipped.truncated,
      warnings: clipped.truncated ? ["正文超过 200,000 字符，已截断"] : [],
      ...(error ? { error: { ...error, retryable: error.code === "network" } } : {}),
    },
    source,
    readabilityChars,
    regionChars,
  };
}
