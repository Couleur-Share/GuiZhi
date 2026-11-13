import { parseHTML } from "linkedom";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { escapeThemedMarkupAttributes } from "./content";

/** 新阅读页勾选生图后，至少保留一个真实生图槽位；SVG 和原图不能替代。 */
export function ensureRequestedReadingImage(version: ThemedReadingVersion): void {
  if (version.formatVersion < 2 || !version.options.generateImages) return;
  if (version.options.maxImages < 1) throw new Error("已开启生成主题图片，请将图片上限设为至少一张");
  const generated = version.assets.filter(a => a.role === "generated");
  const unfinished = generated.filter(a => a.status !== "ready");
  // 中断恢复时，已完成的生图仍可复用，不重复付费。
  const candidates = unfinished.length ? unfinished : generated;
  if (!candidates.length) throw new Error("已勾选生成主题图片，但没有有效配图方案，请重新设计阅读页后重试");
  const doc = parseHTML(`<html><body>${version.design.html}</body></html>`).document;
  const used = new Set([...doc.querySelectorAll("img[data-theme-asset]")].map(e => e.getAttribute("data-theme-asset")));
  if (candidates.some(a => used.has(a.id))) return;
  const asset = candidates[0], figure = doc.createElement("figure"), img = doc.createElement("img"), caption = doc.createElement("figcaption");
  figure.className = "gz-system-requested-image";
  img.setAttribute("data-theme-asset", asset.id); img.setAttribute("alt", asset.alt);
  caption.textContent = asset.alt; figure.append(img, caption);
  const root = doc.querySelector("main") ?? doc.body, paragraph = root.querySelector("p");
  if (paragraph && !paragraph.closest("nav,details,[hidden]")) paragraph.after(figure); else root.append(figure);
  escapeThemedMarkupAttributes(doc.body);
  version.design.html = doc.body.innerHTML;
  version.design.css += "\n.gz-system-requested-image{width:100%;max-width:800px;margin:1.5em auto}.gz-system-requested-image img{display:block;width:100%;max-height:440px;object-fit:contain;border-radius:12px}.gz-system-requested-image figcaption{margin-top:.5em;font-size:.9em;line-height:1.6}";
}
