import sanitizeHtml from "sanitize-html";
import { parseHTML } from "linkedom";
import type { ThemedReadingAsset, ThemedReadingDesign, ThemedReadingSource } from "@guizhi/shared/types/themed-reading";
import { cleanThemedCss } from "./theme-css";
import { validateThemedComposition } from "@guizhi/shared/utils/themed-composition";

const tags = "div section article main header footer nav aside p span br hr h1 h2 h3 h4 h5 h6 b strong i em u s del small sub sup blockquote pre code ul ol li table thead tbody tfoot tr td th caption colgroup col a img figure figcaption details summary svg g path circle ellipse rect line polyline polygon input".split(" ");
const svgAttrs = ["viewBox", "viewbox", "xmlns", "d", "x", "y", "x1", "x2", "y1", "y2", "cx", "cy", "r", "rx", "ry", "points", "fill", "stroke", "stroke-width"];
const filePattern = /^[a-zA-Z0-9][a-zA-Z0-9._-]*\.(?:png|jpe?g|gif|webp|avif)$/i;
const dataImagePattern = /^data:image\/(?:png|jpeg|gif|webp|avif);base64,[a-zA-Z0-9+/]+=*$/;
const labels = new Set(["目录", "文章目录", "返回顶部", "展开", "收起", "继续阅读", "阅读导航", "主题阅读", "原文", "来源", "正文", "上一节", "下一节", "核心概念", "阅读指南", "Contents", "Back to top", "Read more", "Source"]);
export const THEMED_INTERFACE_LABELS = [...labels];

export function themedAssetMapper(assets: ThemedReadingAsset[], assetUrls?: Record<string, string>) {
  const urls = new Map<string, string>();
  for (const asset of assets) {
    if (asset.status !== "ready" || !asset.fileName || !filePattern.test(asset.fileName) || asset.fileName.includes("..")) continue;
    const localUrl = `local-image://${asset.fileName}`;
    const target = assetUrls ? assetUrls[asset.id] : localUrl;
    if (!target || (assetUrls && !dataImagePattern.test(target))) continue;
    urls.set(asset.id, target);
    urls.set(localUrl, target);
    if (asset.originalUrl) urls.set(asset.originalUrl, target);
  }
  return (url: string) => urls.get(url);
}

/** HTML 标签与资源按 manifest 放行。正文、模型设计与备份读回均经过同一边界。 */
export function cleanThemedHtml(html: string, map: (url: string) => string | undefined, source = false): string {
  return sanitizeHtml(html, {
    allowedTags: source ? tags : tags.filter((tag) => tag !== "input"),
    allowedAttributes: {
      "*": ["id", "class", "style", "title", "lang", "dir", "data-source-block", "data-theme-asset", ...svgAttrs],
      a: ["href", "id", "class", "style", "title"],
      img: ["src", "alt", "class", "style", "data-theme-asset"],
      td: ["colspan", "rowspan", "style", "class"], th: ["colspan", "rowspan", "style", "class", "scope"],
      ol: ["start", "class", "style"], details: ["open", "id", "class", "style"],
      input: ["type", "checked", "disabled"], svg: [...svgAttrs, "width", "height", "class", "style"],
    },
    allowedSchemes: ["http", "https", "local-image", "data"],
    allowedSchemesByTag: { a: ["http", "https"], img: ["local-image", "data"] },
    allowProtocolRelative: false,
    parseStyleAttributes: false,
    nonTextTags: ["script", "style", "textarea", "option", "noscript", "iframe", "object", "embed", "form", "foreignobject", "template", "math"],
    transformTags: {
      "*": (tagName, attrs) => {
        const next = { ...attrs };
        if (next.style) next.style = source ? "" : cleanThemedCss(next.style, false);
        if (source) { delete next["data-source-block"]; delete next["data-theme-asset"]; }
        for (const key of ["fill", "stroke"]) if (next[key] && !/^(#[a-f0-9]{3,8}|[a-z]+|rgba?\([\d.,%\s]+\))$/i.test(next[key])) delete next[key];
        if (tagName === "a" && next.href && !/^(https?:\/\/|#[\w-])/i.test(next.href)) delete next.href;
        if (tagName === "img") {
          const src = map(next["data-theme-asset"] ?? next.src ?? "");
          if (src) next.src = src;
          else { delete next.src; next.alt = next.alt || "图片未保存"; }
        }
        if (tagName === "input") { next.type = "checkbox"; next.disabled = ""; }
        return { tagName, attribs: next };
      },
    },
  });
}

function sourceCheck(source: ThemedReadingSource) {
  if (!source || typeof source.title !== "string" || typeof source.content !== "string" || !Array.isArray(source.blocks) || !source.blocks.length || source.blocks.length > 100000) throw new Error("主题页正文快照无效");
  const first = Number(source.blocks[0]?.id?.slice(1));
  if (!Number.isSafeInteger(first) || source.blocks.some((block, index) => !block || block.id !== `b${first + index}` || typeof block.html !== "string" || typeof block.text !== "string" || typeof block.markdown !== "string")) throw new Error("主题页正文内容块无效");
}

/** 验证发生在填正文之前，保持空占位符严格一对一且按原文顺序。 */
export function validateThemedDesign(design: ThemedReadingDesign, source: ThemedReadingSource, assets: ThemedReadingAsset[]): void {
  sourceCheck(source);
  if (!design || typeof design.html !== "string" || typeof design.css !== "string" || design.html.length + design.css.length > 12 * 1024 * 1024) throw new Error("主题排版设计格式无效或过大");
  if (!Array.isArray(assets) || assets.length > 10000 || assets.some((asset) => !asset || typeof asset.id !== "string" || !/^[\w-]{1,100}$/.test(asset.id))) throw new Error("主题页资源清单无效");
  if (new Set(assets.map((asset) => asset.id)).size !== assets.length) throw new Error("主题页资源 ID 重复");
  if (design.composition !== undefined) {
    if (design.html.trim() || design.css.trim()) throw new Error("专题重构不能混用可执行的自由布局");
    validateThemedComposition(design.composition, source, assets.map(asset => asset.id));
    return;
  }
  const { document } = parseHTML(`<html><body>${design.html}</body></html>`);
  const slots = [...document.querySelectorAll("[data-source-block]")];
  if (slots.length !== source.blocks.length) throw new Error("正文内容块存在遗漏或重复");
  for (const [index, slot] of slots.entries()) {
    if (slot.getAttribute("data-source-block") !== source.blocks[index].id) throw new Error("正文内容块未知、重复或顺序被改变");
    if (slot.tagName !== "DIV" || slot.children.length || slot.textContent.trim()) throw new Error("正文占位符必须是没有嵌套和文字的空 div");
    if (slot.closest("a,svg,table,thead,tbody,tfoot,tr,select,script,style,template,form,summary")) throw new Error("正文占位符位于非法嵌套结构中");
  }
  for (const link of document.querySelectorAll("a[href]")) {
    const href = link.getAttribute("href");
    if (!href.startsWith("#") && href !== source.sourceUri) throw new Error("设计不能新增外部链接；请使用页内目录或原文来源链接");
  }
  for (const image of document.querySelectorAll("[data-theme-asset]")) {
    if (image.tagName !== "IMG" || !assets.some((asset) => asset.id === image.getAttribute("data-theme-asset"))) throw new Error("设计引用了未声明的图片素材");
  }
  const headings = new Set(source.blocks.filter((block) => /^<h[1-6][ >]/.test(block.html)).map((block) => block.text.trim()));
  const permitted = (text: string) => !text || labels.has(text) || text === source.title.trim() || headings.has(text);
  for (const element of document.body.querySelectorAll("*")) {
    if ([...element.childNodes].some((node) => node.nodeType === 3 && !permitted(node.textContent.trim()))) throw new Error("设计新增了正文文字；请用原文内容块，新增文字仅限标题或导航标签");
  }
  if ([...document.body.childNodes].some((node) => node.nodeType === 3 && !permitted(node.textContent.trim()))) throw new Error("设计含有未经允许的正文文字");
  const safe = cleanThemedHtml(design.html, themedAssetMapper(assets));
  const sanitizedSlots = [...parseHTML(`<html><body>${safe}</body></html>`).document.querySelectorAll("[data-source-block]")];
  if (sanitizedSlots.length !== slots.length || sanitizedSlots.some((slot, index) => slot.getAttribute("data-source-block") !== source.blocks[index].id)) throw new Error("安全清理会丢失正文占位符，请修复布局结构");
  for (const details of document.querySelectorAll("details")) {
    if (!details.querySelector("summary")?.textContent.trim()) throw new Error("折叠区域缺少可访问的展开标签");
  }
}
