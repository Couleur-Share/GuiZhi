import { reconstructionDocument } from "./reconstruction-document";
import { v3InnerDocument, v3WrapperDocument } from "./v3-document";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { cleanThemedHtml, themedAssetMapper, validateThemedDesign } from "./sanitize";
import { cleanThemedCss, THEMED_READING_BASE_CSS, THEMED_READING_CONTENT_GUARD_CSS } from "./theme-css";
import { THEMED_READING_BRIDGE, THEMED_READING_BRIDGE_HASH } from "./theme-bridge";
import { escapeThemedText, escapeThemedMarkupAttributes } from "./content";
import { parseHTML } from "linkedom";
import { enhanceThemedNavigation } from "./reading-navigation";
import { THEMED_READING_PRESENTATION_CSS, THEMED_READING_NAVIGATION_CSS } from "./reading-presentation";
import { THEMED_EDITORIAL_CSS } from "./editorial-presentation";
import { compositionDocument } from "./composition-document";

export { THEMED_READING_BRIDGE_HASH } from "./theme-bridge";

/** 视频总结常用「整段加粗 + 中文章节号」，只提升这种明确标题，不改写正文或公众号。 */
function promoteNumberedThemeHeadings(slot: Element): void {
  const numbered = /^(?:[一二三四五六七八九十百零〇]+[、．.]|[（(][一二三四五六七八九十百零〇]+[）)]|第[一二三四五六七八九十百零〇\d]+[章节]|\d{1,2}、|\d{1,2}\.\s)\s*\S/u;
  for (const paragraph of [...slot.children]) {
    const text = paragraph.textContent.trim();
    const emphasis = paragraph.children[0];
    if (paragraph.tagName !== "P" || paragraph.children.length !== 1 || !emphasis?.matches("strong,b") || text.length > 100 || !numbered.test(text) || text !== emphasis.textContent.trim() || emphasis.querySelector("img,svg,br")) continue;
    const heading = slot.ownerDocument.createElement("h2");
    for (const attribute of [...paragraph.attributes]) heading.setAttribute(attribute.name, attribute.value);
    // 移动原节点，避免重新序列化 URL 的实体字面量或修改任何文字。
    while (paragraph.firstChild) heading.appendChild(paragraph.firstChild);
    paragraph.replaceWith(heading);
  }
}

/** 无 instanceId 的离线导出禁止脚本；assetUrls 仅接受清单 ID 对应的内嵌栅格图片。 */
export function themedReadingDocument(version: ThemedReadingVersion, instanceId?: string, assetUrls?: Record<string, string>): string {
  if (version?.formatVersion === 3) return v3WrapperDocument(v3InnerDocument(version, assetUrls ?? {}, true));
  if (version?.formatVersion === 2) return reconstructionDocument(version, instanceId, assetUrls);
  if (version?.formatVersion !== 1) throw new Error("不支持的主题页格式版本");
  validateThemedDesign(version.design, version.source, version.assets);
  if (version.design.composition) return compositionDocument(version, instanceId, assetUrls);
  const map = themedAssetMapper(version.assets, instanceId ? assetUrls : (assetUrls ?? {}));
  const safe = cleanThemedHtml(version.design.html, map);
  const { document } = parseHTML(`<html><body>${safe}</body></html>`);
  for (const [index, slot] of [...document.querySelectorAll("[data-source-block]")].entries()) {
    const block = version.source.blocks[index];
    // 备份和数据库的 block.html 均不可信，正文回填前再次清理。
    slot.innerHTML = cleanThemedHtml(block.html, map, true);
    promoteNumberedThemeHeadings(slot);
    if (!slot.id) slot.id = `source-${block.id}`;
    let parent = slot.parentElement;
    let depth = 0;
    while (parent && parent !== document.body) {
      if (++depth > 16) throw new Error("主题布局嵌套超过 16 层，请简化正文容器");
      parent.setAttribute("data-source-layout", ""); parent = parent.parentElement;
    }
  }
  for (const image of document.querySelectorAll("img[data-theme-asset]")) {
    const asset = version.assets.find((candidate) => candidate.id === image.getAttribute("data-theme-asset"));
    if (!image.getAttribute("src")) image.remove();
    else image.setAttribute("alt", asset?.alt ?? "主题插画");
  }
  // 重复标题仅作为原文标题出现，导航锚点只允许命中当前文档。
  for (const link of document.querySelectorAll("a[href^='#']")) {
    const target = link.getAttribute("href").slice(1);
    if (!document.getElementById(target)) link.removeAttribute("href");
  }
  enhanceThemedNavigation(document);
  const csp = `default-src 'none'; script-src ${instanceId ? `'sha256-${THEMED_READING_BRIDGE_HASH}'` : "'none'"}; style-src 'unsafe-inline'; img-src ${instanceId && !assetUrls ? "local-image:" : "data:"}; connect-src 'none'; font-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'`;
  // 保留经过清理的非对称列宽与正文限宽，不能用等宽网格覆盖所有布局。
  const editorial = document.querySelector(".reading-page") ? THEMED_EDITORIAL_CSS : "";
  const guard = `${THEMED_READING_CONTENT_GUARD_CSS}${THEMED_READING_NAVIGATION_CSS}${editorial}`;
  escapeThemedMarkupAttributes(document.body);
  return `<!doctype html><html${instanceId ? ` data-instance="${escapeThemedText(instanceId)}"` : ""}><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${escapeThemedText(csp)}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${escapeThemedText(version.source.title)}</title><style>${THEMED_READING_BASE_CSS}${THEMED_READING_PRESENTATION_CSS}${cleanThemedCss(version.design.css)}${guard}</style></head><body>${document.body.innerHTML}${instanceId ? `<script>${THEMED_READING_BRIDGE}</script>` : ""}</body></html>`;
}
