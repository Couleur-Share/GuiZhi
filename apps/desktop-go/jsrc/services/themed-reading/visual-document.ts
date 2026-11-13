import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import type { parseHTML } from "linkedom";
import { createHash } from "node:crypto";
import { normalizeReadingGraphic } from "./visual-normalize";
import { escapeThemedText as esc } from "./content";
import { validateReadingVisualResults } from "@guizhi/shared/utils/reading-visuals";

export const READING_VISUAL_CSS = `.gz-system-motion-controls button:disabled{opacity:.5;cursor:default}.gz-system-visual-scroll{overflow-x:auto;max-width:100%;padding:8px}.gz-system-visual-scroll>svg{display:block;width:100%;min-width:560px;height:auto}.gz-system-visual-scroll text{font-family:var(--reader-font-family,system-ui,sans-serif)}[data-reading-visual] text:not([fill]):not([style]){fill:var(--theme-text)}[data-reading-visual]{margin:1.5em 0;max-width:100%;min-width:0}[data-reading-visual] figcaption{font-size:.95em;padding:.5em 0}[data-reading-visual]:not([data-motion-state=playing]):not([data-motion-state=paused]) svg *{animation:none!important}[data-reading-visual] svg *{animation-play-state:paused!important}[data-reading-visual][data-motion-state=playing] svg *{animation-play-state:running!important}html[data-reading-motion=off] [data-reading-visual] svg *{animation:none!important}@media(prefers-reduced-motion:reduce){html:not([data-reading-motion=on]) [data-reading-visual] svg *{animation:none!important}}@media print{[data-reading-visual] svg *{animation:none!important}.gz-system-motion-controls,.gz-system-motion-toggle{display:none!important}.gz-system-visual-scroll{overflow:visible}.gz-system-visual-scroll>svg{width:100%!important;min-width:0!important;max-width:100%!important}}`;
export function validateVisualSlots(version: ThemedReadingVersion, doc: ReturnType<typeof parseHTML>["document"]) {
  const visuals = version.reconstruction?.visuals ?? [];
  const results = version.design.visualResults ?? [];
  validateReadingVisualResults(results);
  for (const result of results) {
    const v = visuals.find(v => v.id === result.id && v.kind !== "svg");
    if (!v || createHash("sha256").update(JSON.stringify(v)).digest("hex") !== result.sourceHash) throw new Error("图形编译结果与定义不一致");
    if (result.status === "ready") normalizeReadingGraphic(result.svg, result.css, result.id);
  }
  const slots = [...doc.querySelectorAll("[data-reading-visual]")];
  for (const v of visuals) {
    const matching = slots.filter(s => s.getAttribute("data-reading-visual") === v.id);
    if (matching.length !== 1 || !matching[0].querySelector("figcaption")?.textContent.trim() || matching[0].parentElement.closest("[data-reading-visual]")) throw new Error("图形需要唯一容器和完整文字说明");
    const slot = matching[0];
    if (v.kind === "svg" && slot.querySelectorAll("svg").length !== 1) throw new Error("自定义图形需要一个完整SVG");
    for (const a of version.reconstruction.animations ?? []) {
      if (a.visualId !== v.id) continue;
      const target = doc.getElementById(a.targetId), path = a.pathId && doc.getElementById(a.pathId);
      if (!target?.closest("svg") || !slot.contains(target) || (target.localName === "text" || target.localName === "tspan" || target.querySelector("text,tspan"))) throw new Error("动画目标必须是当前SVG中的图形，不能隐藏文字");
      if (["draw", "morph"].includes(a.preset) && target.localName !== "path") throw new Error("描线与形变需要路径目标");
      if (["motion", "morph"].includes(a.preset) && (!path || path.localName !== "path" || path.closest("svg") !== target.closest("svg") || path === target)) throw new Error("动画路径必须在同一个SVG内");
    }
  }
  if (slots.some(s => !visuals.some(v => v.id === s.getAttribute("data-reading-visual")))) throw new Error("页面存在未知图形容器");
}
export function insertReadingVisuals(version: ThemedReadingVersion, doc: ReturnType<typeof parseHTML>["document"]): string {
  let styles = "";
  for (const v of version.reconstruction?.visuals ?? []) {
    const slot = [...doc.querySelectorAll("[data-reading-visual]")].find(s => s.getAttribute("data-reading-visual") === v.id);
    if (v.kind === "svg") {
      const svg = slot.querySelector("svg"), wrapper = doc.createElement("div");
      wrapper.className = "gz-system-visual-scroll"; svg.replaceWith(wrapper); wrapper.append(svg);
      svg.setAttribute("role", "img"); svg.setAttribute("aria-label", `${v.title}：${v.description}`);
      const width = Number(svg.getAttribute("viewBox")?.split(/[\s,]+/)[2]);
      if (Number.isFinite(width) && width > 0) {
        const size = `calc(${Math.min(width, 4000) / 16} * var(--reader-font-size,16px))`;
        svg.setAttribute("style", `width:100%;min-width:calc(${Math.min(width, 4000) * .8 / 16} * var(--reader-font-size,16px));max-width:${size};height:auto;margin-inline:auto`);
      }
    }
    if (v.kind !== "svg") {
      const result = version.design.visualResults?.find(r => r.id === v.id && r.status === "ready");
      if (result) {
        const normalized = normalizeReadingGraphic(result.svg, result.css, v.id);
        const wrapper = doc.createElement("div"); wrapper.className = "gz-system-visual-scroll";
        wrapper.innerHTML = normalized.svg;
        const svg = wrapper.querySelector("svg"); svg.setAttribute("role", "img"); svg.setAttribute("aria-label", `${v.title}：${v.description}`);
        const width = Number(svg.getAttribute("viewBox")?.split(/[\s,]+/)[2]);
        if (Number.isFinite(width) && width > 0) {
          const size = `calc(${Math.min(width, 4000) / 16} * var(--reader-font-size,16px))`;
          svg.setAttribute("style", `width:100%;min-width:calc(${Math.min(width, 4000) * .8 / 16} * var(--reader-font-size,16px));max-width:${size};height:auto;margin-inline:auto`);
        }
        slot.prepend(wrapper); styles += normalized.css;
        if (/@keyframes/.test(normalized.css)) slot.setAttribute("data-css-motion", "true");
      }
      if (v.kind === "chart") {
        const c = v.chart, details = doc.createElement("details");
        details.innerHTML = `<summary>查看数据表${c.unit ? `（${esc(c.unit)}）` : ""}</summary><div class="gz-system-table"><table><caption>${esc(v.title)}</caption><thead><tr><th scope="col">项目</th>${c.series.map(s => `<th scope="col">${esc(s.name)}</th>`).join("")}</tr></thead><tbody>${c.categories.map((label, i) => `<tr><th scope="row">${esc(label)}</th>${c.series.map(s => `<td>${s.values[i]}</td>`).join("")}</tr>`).join("")}</tbody></table></div>`;
        if (!result) details.setAttribute("open", ""); slot.appendChild(details);
      }
    }
  }
  return styles;
}
