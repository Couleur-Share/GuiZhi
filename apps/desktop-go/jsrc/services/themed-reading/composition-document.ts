import type { ThemedCompositionItem, ThemedCompositionSection, ThemedStatement } from "@guizhi/shared/types/themed-composition";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { validateThemedComposition } from "@guizhi/shared/utils/themed-composition";
import { escapeThemedText as esc, escapeThemedMarkupAttributes } from "./content";
import { cleanThemedHtml, themedAssetMapper } from "./sanitize";
import { COMPOSITION_STYLE } from "./composition-style";
import { COMPOSITION_SCRIPT, COMPOSITION_SCRIPT_HASH } from "./composition-script";
import { THEMED_READING_BRIDGE, THEMED_READING_BRIDGE_HASH } from "./theme-bridge";
import { parseHTML } from "linkedom";

function sourceHtml(html: string, map: (url: string) => string | undefined): string {
  const { document } = parseHTML(`<html><body>${cleanThemedHtml(html, map, true)}</body></html>`);
  // 原文不能伪装成受控组件，也不能覆盖溯源、工具和宿主的 ID。
  for (const node of document.querySelectorAll("[class],[id]")) { node.removeAttribute("class"); node.removeAttribute("id"); }
  escapeThemedMarkupAttributes(document.body);
  return document.body.innerHTML;
}

const number = (id: string) => Number(id.slice(1)) + 1;
function statement(value: ThemedStatement, compact = false): string {
  const label = { quote: "原文摘录", summary: "原文归纳", inference: "推导 · 待核对" }[value.kind];
  const emphasis = [...(value.emphasis ?? [])].sort((a, b) => b.length - a.length);
  const pattern = emphasis.length ? new RegExp(`(${emphasis.map(text => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})`, "g") : null;
  const text = pattern ? value.text.split(pattern).map(part => emphasis.includes(part) ? `<strong>${esc(part)}</strong>` : esc(part)).join("") : esc(value.text);
  const references = [...new Set(value.evidence.map(ref => ref.blockId))].map(blockId => ({ blockId, quote: [...new Set(value.evidence.filter(ref => ref.blockId === blockId).map(ref => ref.quote))].join("；") }));
  return `<div class="gz-statement"><p>${text}</p><div class="gz-evidence ${compact ? "gz-compact" : ""}">${!compact || value.kind === "inference" ? `<span class="${value.kind === "inference" ? "gz-inference" : ""}">${label}</span>` : ""}${references.map(ref => `<a href="#source-${ref.blockId}" title="${label} · 原文：${esc(ref.quote)}" aria-label="核对原文第 ${number(ref.blockId)} 段">${number(ref.blockId)}</a>`).join("")}</div></div>`;
}
function card(item: ThemedCompositionItem, step?: number): string {
  return `<article class="gz-card" data-tone="${item.tone ?? "neutral"}">${step ? `<div class="gz-check-step">${step}</div>` : ""}${item.badge ? `<div class="gz-badge">${esc(item.badge)}</div>` : ""}<h3>${esc(item.title)}</h3>${item.metrics?.length ? `<div class="gz-metrics">${item.metrics.map(metric => `<div class="gz-metric"><span>${esc(metric.label)}</span>${statement(metric.value, true)}</div>`).join("")}</div>` : ""}${statement(item.body)}${item.takeaway ? `<div class="gz-takeaway">${statement(item.takeaway, true)}</div>` : ""}</article>`;
}
function calculator(section: ThemedCompositionSection, id: string): string {
  const labels = section.calculator === "unit-cost" ? ["每份价格（元）", "每份数量（单位）", "每日使用数量（单位）"] : ["每单位成分 A（mg）", "每单位成分 B（mg）", "每日使用数量（单位）"];
  return `<div class="gz-tool" data-gz-calculator="${section.calculator}"><div class="gz-tool-label">${section.calculator === "unit-cost" ? "单价与每日成本" : "成分总量换算"}</div><div class="gz-inputs">${labels.map((label, index) => `<label for="${id}-input-${index}">${label}<input id="${id}-input-${index}" type="number" inputmode="decimal" min="0" max="1000000" step="any" autocomplete="off"></label>`).join("")}</div><output class="gz-result" aria-live="polite">填写上方数值后显示计算结果</output><p class="gz-tool-note">${section.calculator === "unit-cost" ? "计算方法：价格 ÷ 数量 × 每日用量。" : "计算方法：（成分 A + 成分 B）× 每日用量。"}仅按输入换算，不给出用量或功效建议。</p></div>`;
}

/** 新内容层仅由有界 JSON 生成；原文另存索引，不放宽旧 HTML 的清理策略。 */
export function compositionDocument(version: ThemedReadingVersion, instanceId?: string, assetUrls?: Record<string, string>): string {
  const composition = version.design.composition;
  validateThemedComposition(composition, version.source, version.assets.map(asset => asset.id));
  const map = themedAssetMapper(version.assets, instanceId ? assetUrls : (assetUrls ?? {}));
  const art = (id?: string) => {
    if (!id) return "";
    const asset = version.assets.find(asset => asset.id === id), src = map(id);
    return src ? `<figure class="gz-art"><img src="${esc(src)}" alt="${esc(asset.alt)}"><figcaption>${esc(asset.alt)}</figcaption></figure>` : `<p class="gz-tool-note">图片暂不可用：${esc(asset.alt)}</p>`;
  };
  let sectionNumber = 0;
  const chapters = composition.chapters.map((chapter, chapterIndex) => {
    const prefix = `gz-${chapterIndex}`;
    const sections = chapter.sections.map((section, index) => {
      const id = `${prefix}-${index}`;
      let body: string;
      if (section.layout === "matrix") body = `<div class="gz-matrix" tabindex="0" role="region" aria-label="${esc(section.title)}，可横向滚动"><table><thead><tr><th scope="col">对象</th>${section.columns.map(column => `<th scope="col">${esc(column)}</th>`).join("")}</tr></thead><tbody>${section.rows.map(row => `<tr><th scope="row">${esc(row.label)}</th>${row.cells.map(cell => `<td>${statement(cell)}</td>`).join("")}</tr>`).join("")}</tbody></table></div>`;
      else if (section.layout === "calculator") body = calculator(section, id);
      else if (section.layout === "explorer") body = `<div class="gz-tool gz-explorer"><div class="gz-tool-label">选择一个问题，查看原文中的相关信息</div><div class="gz-choices" role="group" aria-label="${esc(section.title)}">${section.items.map((item, n) => `<button type="button" data-gz-choice="${n}" aria-pressed="${n === 0}" aria-controls="${id}-panel-${n}">${esc(item.title)}</button>`).join("")}</div>${section.items.map((item, n) => `<div class="gz-panel" id="${id}-panel-${n}">${card(item)}</div>`).join("")}</div>`;
      else {
        const grid = `<div class="gz-grid ${section.items.length === 3 ? "gz-grid-three" : ""}">${section.items.map((item, n) => card(item, section.layout === "checklist" ? n + 1 : undefined)).join("")}</div>`;
        body = section.layout === "illustrated" && section.imageId ? `<div class="gz-illustrated">${art(section.imageId)}${grid}</div>` : `<div class="${section.layout === "prose" ? "gz-prose" : ""}">${grid}</div>${art(section.imageId)}`;
      }
      return `<section class="gz-section" id="${id}"><header class="gz-section-head"><span class="gz-section-num">${String(++sectionNumber).padStart(2, "0")}</span><h2>${esc(section.title)}</h2>${section.label ? `<span class="gz-section-label">${esc(section.label)}</span>` : ""}</header>${section.intro ? `<div class="gz-section-intro">${statement(section.intro)}</div>` : ""}${body}</section>`;
    }).join("");
    const candidates = chapter.sections.map((section, index) => ({ section, index }));
    const selected = candidates.filter(({ section }) => section.navLabel);
    const nav = `<nav class="gz-nav" aria-label="专题导航">${(selected.length ? selected : candidates).slice(0, 3).map(({ section, index }, n) => `<a data-reader-toc-link href="#${prefix}-${index}"><span>${String(n + 1).padStart(2, "0")} ↗</span>${esc(section.navLabel ?? section.title)}</a>`).join("")}</nav>`;
    return `${chapterIndex === 0 ? `<header class="gz-hero" id="gz-top"><span class="gz-category">${esc(chapter.category)}</span><h1 class="gz-title">${esc(chapter.displayTitle ?? version.source.title)}</h1><div class="gz-subtitle">${statement(chapter.subtitle, true)}</div><div class="gz-lead-card"><div class="gz-lead-grid"><div class="gz-lead-text"><div class="gz-kicker">阅读提要</div>${statement(chapter.lead)}${nav}</div>${art(chapter.imageId)}</div></div></header>` : `<div class="gz-lead-card gz-section-intro"><div class="gz-lead-grid"><div class="gz-lead-text">${statement(chapter.subtitle)}${statement(chapter.lead)}${nav}</div>${art(chapter.imageId)}</div></div>`}${sections}`;
  }).join("");
  const originals = `<details class="gz-original" id="gz-original"><summary>原文与引用核对 · ${version.source.blocks.length} 段</summary><p class="gz-original-note">上方内容为 AI 重构。引用编号可定位原文；引用匹配不等于事实已经核实。以下保留生成时的完整原文快照及顺序。</p>${version.source.blocks.map(block => `<section class="gz-original-block" id="source-${block.id}"><div class="gz-original-label">原文 ${number(block.id)}</div><div data-source-block="${block.id}">${sourceHtml(block.html, map)}</div></section>`).join("")}</details>`;
  const hashes = `'sha256-${COMPOSITION_SCRIPT_HASH}'${instanceId ? ` 'sha256-${THEMED_READING_BRIDGE_HASH}'` : ""}`;
  const csp = `default-src 'none'; script-src ${hashes}; style-src 'unsafe-inline'; img-src ${instanceId && !assetUrls ? "local-image:" : "data:"}; connect-src 'none'; font-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'`;
  return `<!doctype html><html lang="zh-CN" data-palette="${composition.chapters[0].palette}"${instanceId ? ` data-instance="${esc(instanceId)}"` : ""}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="${esc(csp)}"><meta name="referrer" content="no-referrer"><title>${esc(version.source.title)}</title><style>${COMPOSITION_STYLE}</style></head><body class="${instanceId ? "gz-host" : ""}"><div class="gz-top"><div class="gz-top-inner"><span class="gz-brand">GUIZHI EDITORIAL · 主题阅读</span><div class="gz-actions"><a href="#gz-original">核对原文</a><button class="gz-theme" type="button" aria-label="切换深浅主题">外观</button></div></div></div><main class="gz-page">${chapters}${originals}<footer class="gz-footer"><a href="#gz-top">返回顶部</a><a href="#gz-original">原文与引用</a></footer></main><script>${COMPOSITION_SCRIPT}</script>${instanceId ? `<script>${THEMED_READING_BRIDGE}</script>` : ""}</body></html>`;
}
