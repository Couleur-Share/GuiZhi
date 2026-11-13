import { READING_SCROLLBARS } from "./reading-scrollbars";
import libraries from "virtual:reading-libraries";
import { readingComponentCss } from "./visual-components";
import { validateVisualSlots, insertReadingVisuals, READING_VISUAL_CSS } from "./visual-document";
import { READING_SVG_TAGS, READING_SVG_ATTRIBUTES, cleanReadingSvgAttributes, readingSvgTag, readingSvgReference, validateReadingSvg, validateReadingSvgElements, type ReadingSvgReference } from "./reconstruction-svg";
import sanitizeHtml from "sanitize-html";
import * as css from "css-tree";
import { parseHTML } from "linkedom";
import { createHash } from "node:crypto";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { validateReadingReconstruction } from "@guizhi/shared/utils/reading-reconstruction";
import { READING_EXPRESSION_SCRIPT } from "@guizhi/shared/utils/reading-expression-runtime";
import { themedAssetMapper } from "./sanitize";
import { escapeThemedText as esc, escapeThemedMarkupAttributes } from "./content";
import { THEMED_READING_BRIDGE, THEMED_READING_BRIDGE_HASH } from "./theme-bridge";

export function cleanReconstructionCss(value: string, inline = false, references: ReadingSvgReference[] = []): string {
  if (typeof value !== "string" || value.length > 300000) throw new Error("页面样式过大");
  const tree = css.parse(value, { context: inline ? "declarationList" : "stylesheet", positions: false, parseCustomProperty: true });
  css.walk(tree, function(node) {
    if (node.type === "Url") references.push(readingSvgReference(this.declaration?.property ?? "", node.value));
    if (node.type === "Raw") throw new Error("样式含不支持的资源或语法");
    if (node.type === "Atrule" && !["media", "supports"].includes(node.name.toLowerCase())) throw new Error("样式不能加载外部资源或定义其他规则");
    if (node.type === "Function" && ["url", "expression", "paint", "image", "image-set", "attr"].includes(node.name.toLowerCase())) throw new Error("样式函数不允许");
    if (node.type === "Declaration" && (["behavior", "-moz-binding", "content"].includes(node.property.toLowerCase()) || (node.property === "position" && /fixed/i.test(css.generate(node.value))))) throw new Error("样式不能覆盖宿主或注入内容");
  });
  return css.generate(tree).replace(/<\//g, "<\\/");
}
const tags = [..."div section article main header footer nav aside p span br hr h1 h2 h3 h4 h5 h6 b strong i em u s del small sub sup blockquote pre code ul ol li table thead tbody tfoot tr td th caption colgroup col a img figure figcaption details summary".split(" "), ...READING_SVG_TAGS];
export function cleanReconstructionHtml(html: string, map: (url: string) => string | undefined, references: ReadingSvgReference[] = []): string {
  return sanitizeHtml(html, {
    parser: { lowerCaseTags: false, lowerCaseAttributeNames: false },
    allowedTags: tags, allowedAttributes: { "*": ["id", "class", "style", "title", "role", "aria-label", "data-theme-asset", "data-reading-tool", "data-reading-panel", "data-reading-tags", "data-reading-visual", "aria-hidden", "aria-labelledby", "aria-describedby", ...READING_SVG_ATTRIBUTES], a: ["href", "class", "id", "style", "title", "aria-label"], img: ["data-theme-asset", "src", "alt", "class", "style"], details: ["id", "class", "style", "open"], td: ["class", "style", "rowspan", "colspan"], th: ["class", "style", "scope", "rowspan", "colspan"], ol: ["class", "style", "start"] },
    allowedSchemes: ["https", "http", "data", "local-image"], allowProtocolRelative: false, parseStyleAttributes: false,
    transformTags: { "*": (tag, attributes) => {
      // HTML 属性沿用大小写不敏感语义，SVG 专用属性随后恢复标准大小写。
      const attrs = Object.fromEntries(Object.entries(attributes).map(([name, value]) => [name.toLowerCase(), value]));
      tag = tag.toLowerCase();
      if (attrs.style) attrs.style = cleanReconstructionCss(attrs.style, true, references);
      if (tag === "img") { const src = map(attrs["data-theme-asset"] ?? ""); if (src) attrs.src = src; else delete attrs.src; }
      if (attrs.href && !/^#[a-zA-Z][\w-]*$/.test(attrs.href)) delete attrs.href;
      const safeTag = readingSvgTag(tag);
      cleanReadingSvgAttributes(safeTag, attrs);
      return { tagName: safeTag, attribs: attrs };
    } },
  });
}
export function validateReconstructionPage(version: ThemedReadingVersion): void {
  validateReadingReconstruction(version.reconstruction);
  const d = version.design; if (!d || !d.html.trim() || d.html.length > 2000000 || d.composition) throw new Error("重构页缺少自由 HTML 设计");
  const svgReferences: ReadingSvgReference[] = [];
  cleanReconstructionCss(d.css, false, svgReferences);
  const raw = parseHTML(`<html><body>${d.html}</body></html>`).document;
  if (raw.querySelector("script,iframe,object,embed,form,input,button,link,style,base,meta") || [...raw.querySelectorAll("*")].some(e => [...e.attributes].some(a => /^on/i.test(a.name)))) throw new Error("模型页面含脚本、资源加载或交互代码，请改用工具占位符");
  validateReadingSvgElements(raw);
  const safe = parseHTML(`<html><body>${cleanReconstructionHtml(d.html, () => undefined, svgReferences)}</body></html>`).document;
  const normalize = (v: string) => v.replace(/\s/g, "");
  if (normalize(raw.body.textContent) !== normalize(safe.body.textContent) || safe.body.textContent.trim().length < 80 || !safe.querySelector("h1") || !safe.querySelector("h2")) throw new Error("页面清理后内容缺失或文章结构不完整");
  const ids = new Set<string>();
  for (const node of safe.querySelectorAll("[id]")) { if (!/^[a-zA-Z][\w-]*$/.test(node.id) || node.id.startsWith("gz-system") || ids.has(node.id)) throw new Error("页面 ID 无效或重复"); ids.add(node.id); }
  validateReadingSvg(safe, svgReferences);
  validateVisualSlots(version, safe);
  for (const link of safe.querySelectorAll('a[href^="#"]')) if (!ids.has(link.getAttribute("href").slice(1))) throw new Error("导航没有目标章节");
  for (const image of safe.querySelectorAll("img")) if (!version.assets.some(a => a.id === image.getAttribute("data-theme-asset"))) throw new Error("引用了未声明图片");
  for (const tool of version.reconstruction.interactions) {
    const slots = [...safe.querySelectorAll("[data-reading-tool]")].filter(e => e.getAttribute("data-reading-tool") === tool.id);
    if (slots.length !== 1 || slots[0].textContent.trim() || slots[0].children.length) throw new Error("交互工具需唯一空占位符");
    if (tool.kind !== "calculator") for (const choice of tool.choices) if (![...safe.querySelectorAll("[data-reading-panel]")].some(e => e.getAttribute("data-reading-panel") === `${tool.id}:${choice.value}`)) throw new Error("交互选项缺少解释面板");
  }
  if ([...safe.querySelectorAll("[data-reading-tool]")].some(e => !version.reconstruction.interactions.some(t => t.id === e.getAttribute("data-reading-tool")))) throw new Error("未知交互工具");
}
const BASE = `*{box-sizing:border-box}html{color-scheme:light;--theme-surface:#f7f9fc;--theme-text:#182536}html[data-theme=dark]{color-scheme:dark;--theme-surface:#111923;--theme-text:#e5edf5}body{margin:0;background:var(--theme-surface);color:var(--theme-text);font:var(--reader-font-size,16px)/1.8 var(--reader-font-family,system-ui,sans-serif);overflow-wrap:anywhere}img,svg{max-width:100%;height:auto}img{object-fit:contain}main{min-width:0}a{color:inherit;text-underline-offset:4px}nav a,.gz-system-controls button{min-height:44px;cursor:pointer}a:focus-visible,button:focus-visible,input:focus-visible,summary:focus-visible{outline:3px solid #368fbd;outline-offset:4px}pre,.gz-system-table{overflow-x:auto}details>summary{cursor:pointer;min-height:44px}button,input{font:inherit;color:inherit}input{max-width:100%;width:100%;padding:10px;border:1px solid #8495a8;border-radius:8px;background:var(--theme-surface)}.gz-system-controls{display:flex;flex-wrap:wrap;gap:10px;margin:20px 0}.gz-system-controls button{border:1px solid #8495a8;border-radius:10px;padding:8px 16px;background:var(--theme-surface)}.gz-system-controls button[aria-pressed=true]{box-shadow:inset 0 0 0 2px currentColor;font-weight:bold}.gz-system-fields{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,180px),1fr));gap:16px}.gz-system-result{display:block;padding:16px;border:1px solid #8495a8;border-radius:10px;margin-top:16px;font-weight:700}.gz-system-footer{max-width:1000px;margin:40px auto;padding:20px}.gz-system-theme{margin:12px;padding:6px 12px;border:1px solid #8495a8;border-radius:20px;background:var(--theme-surface);cursor:pointer}[hidden]{display:none!important}@media(max-width:600px){body{overflow-wrap:anywhere}}`;
/** 运行时不包含模型文本；工具定义通过 inert JSON 节点读取。 */
const SCRIPT = `(()=>{${READING_EXPRESSION_SCRIPT};const evaluate=evaluateReadingExpression;const tools=JSON.parse(document.getElementById('gz-system-data').textContent);const root=document.documentElement;const theme=()=>{if(!root.dataset.instance&&!root.dataset.theme)root.dataset.theme=matchMedia('(prefers-color-scheme:dark)').matches?'dark':'light'};theme();document.querySelector('.gz-system-theme')?.addEventListener('click',()=>{root.dataset.theme=root.dataset.theme==='dark'?'light':'dark'});for(const t of tools){const slot=[...document.querySelectorAll('[data-reading-tool]')].find(e=>e.dataset.readingTool===t.id);if(!slot)continue;if(t.kind==='calculator'){const result=slot.querySelector('output');slot.addEventListener('input',()=>{try{const values={};for(const f of t.inputs){const input=slot.querySelector('[name="'+f.name+'"]');if(!input.value.trim())throw Error('填写上方数值后显示结果');const n=Number(input.value);if(!Number.isFinite(n)||n<(f.min??-1e9)||n>(f.max??1e9))throw Error('输入数值超出范围');values[f.name]=n}result.textContent=new Intl.NumberFormat('zh-CN',{maximumFractionDigits:4}).format(evaluate(t.expression,values))+(t.unit?' '+t.unit:'')}catch(e){result.textContent=e.message}})}else{const panels=[...document.querySelectorAll('[data-reading-panel]')].filter(e=>e.dataset.readingPanel.startsWith(t.id+':'));const choose=value=>{for(const b of slot.querySelectorAll('button'))b.setAttribute('aria-pressed',String(b.dataset.choice===value));for(const p of panels)p.hidden=t.kind==='filter'&&value==='all'?false:p.dataset.readingPanel!==t.id+':'+value};slot.addEventListener('click',e=>{const b=e.target.closest('button[data-choice]');if(b)choose(b.dataset.choice)});choose(t.choices[0].value)}}document.addEventListener('click',e=>{const a=e.target.closest('a[href^="#"]');if(!a)return;let target=document.getElementById(a.getAttribute('href').slice(1));while(target){if(target.tagName==='DETAILS')target.open=true;target=target.parentElement}},true);window.addEventListener('message',e=>{if(e.source!==parent||!root.dataset.instance||e.data?.id!==root.dataset.instance)return;if(e.data.type==='find'&&e.data.value?.query){document.querySelectorAll('[data-reading-panel]').forEach(p=>p.hidden=false)}},true)})();`;
export const READING_RECONSTRUCTION_SCRIPT_HASH = createHash("sha256").update(SCRIPT).digest("base64");
export function reconstructionDocument(version: ThemedReadingVersion, instanceId?: string, assetUrls?: Record<string, string>): string {
  validateReconstructionPage(version);
  const map = themedAssetMapper(version.assets, instanceId ? assetUrls : (assetUrls ?? {}));
  const doc = parseHTML(`<html><body>${cleanReconstructionHtml(version.design.html, map)}</body></html>`).document;
  for (const table of doc.querySelectorAll("table")) { const wrap = doc.createElement("div"); wrap.className = "gz-system-table"; table.replaceWith(wrap); wrap.appendChild(table); }
  for (const t of version.reconstruction.interactions) {
    const slot = [...doc.querySelectorAll("[data-reading-tool]")].find(e => e.getAttribute("data-reading-tool") === t.id);
    slot.innerHTML = t.kind === "calculator" ? `<div class="gz-system-fields">${t.inputs.map(f => `<label>${esc(f.label)}${f.unit ? `（${esc(f.unit)}）` : ""}<input type="number" step="any" name="${f.name}" min="${f.min ?? -1e9}" max="${f.max ?? 1e9}" inputmode="decimal"></label>`).join("")}</div><output aria-live="polite" class="gz-system-result">填写上方数值后显示结果</output>` : `<div class="gz-system-controls" role="group">${t.choices.map(c => `<button type="button" data-choice="${c.value}" aria-pressed="false">${esc(c.label)}</button>`).join("")}</div>`;
  }
  const visualCss = insertReadingVisuals(version, doc);
  const motion = (version.reconstruction.animations?.length ?? 0) > 0 || Boolean(doc.querySelector("[data-css-motion]"));
  const motionScript = libraries.animation.replace(/<\/script/gi, "<\\/script");
  const motionHash = createHash("sha256").update(motionScript).digest("base64");
  escapeThemedMarkupAttributes(doc.body);
  const used = new Set(version.reconstruction.draft.flatMap(s => s.referenceIds));
  const refs = version.reconstruction.references.filter(r => r.status === "ready" && used.has(r.id));
  const footer = !instanceId && refs.length ? `<details class="gz-system-footer"><summary>参考资料</summary><ul>${refs.map(r => `<li><a href="${esc(r.url)}" target="_blank" rel="noreferrer noopener">${esc(r.title)}</a> · 获取于 ${new Date(r.capturedAt).toISOString().slice(0, 10)}</li>`).join("")}</ul></details>` : "";
  const csp = `default-src 'none';script-src 'sha256-${READING_RECONSTRUCTION_SCRIPT_HASH}'${motion ? ` 'sha256-${motionHash}'` : ""}${instanceId ? ` 'sha256-${THEMED_READING_BRIDGE_HASH}'` : ""};style-src 'unsafe-inline';img-src ${instanceId && !assetUrls ? "local-image:" : "data:"};connect-src 'none';font-src 'none';base-uri 'none';form-action 'none'`;
  return `<!doctype html><html lang="zh-CN"${instanceId ? ` data-instance="${esc(instanceId)}"` : ""}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="${esc(csp)}"><title>${esc(version.reconstruction.outline.title)}</title><style>${BASE}\n${readingComponentCss(version.design.html)}\n${READING_VISUAL_CSS}\n${visualCss}\n${cleanReconstructionCss(version.design.css)}\n${READING_SCROLLBARS}</style></head><body class="gz-reading-components">${!instanceId ? '<button class="gz-system-theme" type="button">切换外观</button>' : ""}${doc.body.innerHTML}${footer}<script type="application/json" id="gz-system-data">${JSON.stringify(version.reconstruction.interactions).replace(/</g, "\\u003c")}</script><script>${SCRIPT}</script>${motion ? `<script type="application/json" id="gz-system-motion-data">${JSON.stringify(version.reconstruction.animations ?? []).replace(/</g, "\\u003c")}</script><script>${motionScript}</script>` : ""}${instanceId ? `<script>${THEMED_READING_BRIDGE}</script>` : ""}</body></html>`;
}
