import { parseHTML } from "linkedom";
import * as css from "css-tree";
import { cleanReconstructionHtml } from "./reconstruction-document";
import { validateReadingSvgElements, validateReadingSvg, type ReadingSvgReference } from "./reconstruction-svg";

const palette: Record<string, string> = { "#182536": "var(--theme-text)", "#e5eef8": "var(--theme-visual-surface)", "#4477aa": "var(--theme-visual-1)", "#228866": "var(--theme-visual-2)", "#aa6677": "var(--theme-visual-3)", "#997733": "var(--theme-visual-4)", "#8866aa": "var(--theme-visual-5)", "#337788": "var(--theme-visual-6)" };
for (const color of ["#000000", "#222222", "#333333", "#666666"]) palette[color] = "var(--theme-text)";
for (const color of ["#ffffff", "#f4f4f4", "#ececff", "#ffffde"]) palette[color] = "var(--theme-visual-surface)";
for (const color of ["#999999", "#cccccc", "#dddddd"]) palette[color] = "var(--theme-visual-1)";
const theme = (s: string) => s.replace(/#[a-fA-F0-9]{3,8}\b/g, v => {
  const full = v.length === 4 ? "#" + [...v.slice(1)].map(c => c + c).join("") : v;
  return palette[full.toLowerCase()] ?? v;
});
export function normalizeReadingGraphic(markup: string, extraCss: string, id: string): { svg: string; css: string } {
  if (typeof markup !== "string" || markup.length > 2000000 || typeof extraCss !== "string" || extraCss.length > 300000) throw new Error("编译图形超过大小上限");
  const doc = parseHTML(`<html><body>${markup}</body></html>`).document;
  const root = doc.body.firstElementChild;
  if (root?.localName !== "svg" || doc.body.children.length !== 1 || doc.body.textContent.replace(root.textContent, "").trim()) throw new Error("编译结果必须为单个 SVG");
  doc.body.setAttribute("data-reading-visual", id);
  if ([root, ...root.querySelectorAll("*")].some(e => [...e.attributes].some(a => /^on/i.test(a.name)))) throw new Error("编译图形含事件属性");
  let styles = extraCss;
  for (const node of root.querySelectorAll("style")) { styles += node.textContent.replace(/^\s*<!\[CDATA\[([\s\S]*)\]\]>\s*$/, "$1"); node.remove(); }
  // Mermaid 会附带未使用的图标定义和阴影；展开页内图标，去掉装饰滤镜。
  for (let pass = 0; pass < 3 && root.querySelector("use"); pass++) {
    for (const [index, use] of [...root.querySelectorAll("use")].entries()) {
      const href = use.getAttribute("href") ?? use.getAttribute("xlink:href");
      if (!/^#[\w-]+$/.test(href ?? "")) throw new Error("图形不能引用外部图标");
      const symbol = [...root.querySelectorAll("symbol")].find(s => s.id === href.slice(1));
      if (!symbol) throw new Error("图形引用的图标不存在");
      const group = doc.createElement("g");
      group.innerHTML = symbol.innerHTML;
      if (group.querySelector("[id]")) throw new Error("图标定义不能包含嵌套ID");
      const x = Number(use.getAttribute("x") ?? 0), y = Number(use.getAttribute("y") ?? 0);
      if (!Number.isFinite(x) || !Number.isFinite(y)) throw new Error("图标坐标无效");
      group.setAttribute("transform", `translate(${x} ${y}) ${use.getAttribute("transform") ?? ""}`);
      if (use.id) group.id = use.id; else group.id = `icon-${pass}-${index}`;
      use.replaceWith(group);
    }
  }
  for (const node of root.querySelectorAll("symbol,filter")) node.remove();
  for (const node of [root, ...root.querySelectorAll("*")]) node.removeAttribute("filter");
  styles = styles.replace(/\bfilter\s*:[^;}]+;?/g, "");
  styles = styles.replace(/&gt;/g, ">");
  for (const node of root.querySelectorAll("[style]")) {
    const value = node.getAttribute("style").replace(/(^|;)\s*undefined\s*(?=;|$)/g, "$1").replace(/^;+|;+$/g, "");
    if (value.trim()) node.setAttribute("style", value); else node.removeAttribute("style");
  }
  validateReadingSvgElements(doc);
  const prefix = `rv-${id}-`;
  const namespace = (value: string) => value.startsWith(prefix) ? value : prefix + value.replace(/[^\w-]/gu, char => `u${char.codePointAt(0).toString(16)}-`);
  const ids = new Map<string, string>();
  const originals = new Map<string, Element>();
  for (const node of root.querySelectorAll("[id]")) {
    let value = node.id;
    if (ids.has(value)) {
      if (node.closest("defs")) {
        if (originals.get(value)?.outerHTML !== node.outerHTML) throw new Error("图形重复定义内容不一致");
        node.remove(); continue;
      }
      value = `${value}-part-${ids.size}`; node.id = value;
    }
    originals.set(value, node); ids.set(value, namespace(value));
  }
  if (root.id) ids.set(root.id, namespace(root.id));
  if (new Set(ids.values()).size !== ids.size) throw new Error("图形ID规范化后发生冲突");
  const ref = (value: string) => { const found = ids.get(value); if (!found) throw new Error("图形含跨图引用"); return found; };
  for (const node of [root, ...root.querySelectorAll("*")]) {
    if (node.id) node.id = ref(node.id);
    if (node.hasAttribute("class")) node.setAttribute("class", node.getAttribute("class").split(/\s+/).filter(Boolean).map(namespace).join(" "));
    for (const attr of [...node.attributes]) {
      if (/^(?:href|xlink:href)$/i.test(attr.name)) throw new Error("图形不能链接外部元素");
      let value = attr.value.replace(/url\(\s*['"]?#([\w-]+)['"]?\s*\)/g, (_m, target) => `url(#${ref(target)})`);
      if (/^aria-(?:labelledby|describedby)$/.test(attr.name)) value = value.split(/\s+/).map(ref).join(" ");
      node.setAttribute(attr.name, ["fill", "stroke", "stop-color", "style", "color"].includes(attr.name) ? theme(value) : value);
    }
  }
  const tree = css.parse(styles, { parseCustomProperty: true });
  // Mermaid 默认输出所有图形的公共规则；先移除没有实际目标的规则，
  // 避免把未使用的循环动画和图标样式带入阅读页。
  css.walk(tree, function(node, item, list) {
    if (node.type !== "Rule" || node.prelude.type !== "SelectorList" || this.atrule?.name === "keyframes") return;
    const selectors = node.prelude.children.toArray();
    const used = selectors.some(selector => {
      const copy = css.clone(selector);
      css.walk(copy, part => {
        if (part.type === "IdSelector") part.name = ids.get(part.name) ?? namespace(part.name);
        if (part.type === "ClassSelector") part.name = namespace(part.name);
      });
      try { return [...doc.querySelectorAll(css.generate(copy))].some(e => e === root || root.contains(e)); } catch { return false; }
    });
    if (!used) list.remove(item);
  });
  const keyframes = new Set<string>();
  css.walk(tree, node => {
    if (node.type === "Atrule") {
      if (node.name !== "keyframes") throw new Error("图形样式只允许关键帧规则");
      const name = css.generate(node.prelude); if (!/^[\w-]+$/.test(name)) throw new Error("图形动画名无效"); keyframes.add(name);
    }
  });
  css.walk(tree, function(node) {
    if (node.type === "Raw" || node.type === "Url" || (node.type === "Function" && ["url", "expression", "paint", "image-set", "attr"].includes(node.name.toLowerCase()))) throw new Error("图形 CSS 包含资源或无效语法");
    if (node.type === "Declaration") {
      if (["content", "behavior", "position", "-moz-binding"].includes(node.property) || node.property.startsWith("--")) throw new Error("图形 CSS 属性不允许");
      if (this.atrule?.name === "keyframes" && !["opacity", "transform", "d", "stroke-dasharray", "stroke-dashoffset"].includes(node.property)) throw new Error("图形动画属性不允许");
      if (node.property.startsWith("animation")) {
        const value = css.generate(node.value);
        if (/\binfinite\b|\bvar\(/i.test(value) || (node.property === "animation-iteration-count" && value !== "1")) throw new Error("图形不能循环播放");
        let totalMs = 0;
        css.walk(node.value, function(part) {
          if (node.property === "animation" && part.type === "Number" && !this.function && Number(part.value) > 1) throw new Error("图形不能循环播放");
          if (part.type === "Dimension" && ["s", "ms"].includes(part.unit)) {
            if (Number(part.value) < 0) throw new Error("图形动画时长无效");
            totalMs += Number(part.value) * (part.unit === "s" ? 1000 : 1);
          }
        });
        if (totalMs > 8000) throw new Error("图形动画时长超限");
      }
    }
    if (node.type === "IdSelector") node.name = ref(node.name);
    if (node.type === "ClassSelector") node.name = namespace(node.name);
    if (node.type === "Identifier" && keyframes.has(node.name)) node.name = namespace(node.name);
  });
  const usedAnimations = new Set<string>();
  css.walk(tree, node => { if (node.type === "Declaration" && ["animation", "animation-name"].includes(node.property)) css.walk(node.value, part => { if (part.type === "Identifier") usedAnimations.add(part.name); }); });
  css.walk(tree, (node, item, list) => { if (node.type === "Atrule" && node.name === "keyframes" && !usedAnimations.has(css.generate(node.prelude))) list.remove(item); });
  const scope = `[data-reading-visual="${id}"]`;
  css.walk(tree, function(node) {
    if (node.type !== "Rule" || node.prelude.type !== "SelectorList" || this.atrule?.name === "keyframes") return;
    const selectors = node.prelude.children.toArray().map(s => css.generate(s));
    node.prelude = css.parse(selectors.map(s => s.startsWith(scope) ? s : `${scope} ${s}`).join(","), { context: "selectorList" }) as css.SelectorList;
  });
  const references: ReadingSvgReference[] = [];
  const svg = cleanReconstructionHtml(root.outerHTML, () => undefined, references);
  const safe = parseHTML(svg).document;
  validateReadingSvg(safe, references);
  if (root.textContent.replace(/\s/g, "") !== safe.documentElement.textContent.replace(/\s/g, "")) throw new Error("图形清理后丢失文字");
  return { svg, css: theme(css.generate(tree)).replace(/<\//g, "<\\/") };
}
