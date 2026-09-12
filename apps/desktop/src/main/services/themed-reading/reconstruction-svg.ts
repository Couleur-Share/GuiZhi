import type { parseHTML } from "linkedom";

export const READING_SVG_TAGS = "svg g path circle ellipse rect line polyline polygon text tspan defs linearGradient radialGradient stop marker clipPath title desc".split(" ");
export const READING_SVG_ATTRIBUTES = [
  "viewBox", "xmlns", "preserveAspectRatio", "d", "x", "y", "dx", "dy",
  "x1", "x2", "y1", "y2", "cx", "cy", "r", "rx", "ry", "fx", "fy",
  "points", "fill", "fill-opacity", "fill-rule", "stroke", "stroke-width",
  "stroke-opacity", "stroke-linecap", "stroke-linejoin", "stroke-dasharray",
  "stroke-dashoffset", "opacity", "width", "height", "transform",
  "offset", "stop-color", "stop-opacity", "gradientUnits", "gradientTransform",
  "spreadMethod", "text-anchor", "dominant-baseline", "font-size", "font-family",
  "font-weight", "font-style", "letter-spacing", "vector-effect",
  "marker-start", "marker-mid", "marker-end", "markerWidth", "markerHeight",
  "refX", "refY", "orient", "markerUnits", "clip-path", "clipPathUnits",
];

const referenceTargets: Record<string, string[]> = {
  fill: ["lineargradient", "radialgradient"],
  stroke: ["lineargradient", "radialgradient"],
  "marker-start": ["marker"], "marker-mid": ["marker"], "marker-end": ["marker"],
  "clip-path": ["clippath"],
};
export interface ReadingSvgReference { property: string; id: string }
const tagNames = new Map(READING_SVG_TAGS.map(tag => [tag.toLowerCase(), tag]));
const attributeNames = new Map(READING_SVG_ATTRIBUTES.map(attr => [attr.toLowerCase(), attr]));

export function readingSvgTag(tag: string): string {
  return tagNames.get(tag.toLowerCase()) ?? tag;
}

// 只接受当前文档的简单片段 ID；不解码 URL、不允许回落资源或外部地址。
export function readingSvgReference(property: string, fragment: string): ReadingSvgReference {
  if (!Object.hasOwn(referenceTargets, property) || !/^#[a-zA-Z][\w-]*$/.test(fragment)) {
    throw new Error("SVG 绘图只允许引用页内渐变、箭头或裁剪路径");
  }
  return { property, id: fragment.slice(1) };
}

export function cleanReadingSvgAttributes(tag: string, attrs: Record<string, string>): void {
  if (!READING_SVG_TAGS.includes(tag)) return;
  // 兼容旧页面的小写属性，以及 HTML 解析器重序列化后的 SVG 标签。
  for (const [name, value] of Object.entries(attrs)) {
    const canonical = attributeNames.get(name.toLowerCase());
    if (canonical && canonical !== name) { delete attrs[name]; attrs[canonical] = value; }
  }
  for (const property of Object.keys(referenceTargets)) {
    const value = attrs[property];
    if (!value) continue;
    if (property === "fill" || property === "stroke") {
      if (/^(?:#[\da-f]{3,8}|[a-z]+|rgba?\([\d.,%\s]+\))$/i.test(value)) continue;
      if (/^var\(--theme-[a-zA-Z0-9_-]+\)$/.test(value)) continue;
    } else if (value === "none") continue;
    const match = /^url\(\s*(['"]?)(#[a-zA-Z][\w-]*)\1\s*\)$/.exec(value);
    if (!match) throw new Error("SVG 绘图引用无效，请使用 url(#页内ID)");
    readingSvgReference(property, match[2]);
    attrs[property] = `url(${match[2]})`;
  }
  if (attrs.xmlns && attrs.xmlns !== "http://www.w3.org/2000/svg") delete attrs.xmlns;
}

export function validateReadingSvgElements(doc: ReturnType<typeof parseHTML>["document"]): void {
  const allowed = new Set(READING_SVG_TAGS.map(tag => tag.toLowerCase()));
  for (const svg of doc.querySelectorAll("svg")) {
    for (const node of svg.querySelectorAll("*")) {
      if (!allowed.has(node.localName.toLowerCase())) throw new Error(`SVG 图解含不支持的元素：${node.localName}`);
    }
    const box = svg.getAttribute("viewBox") ?? svg.getAttribute("viewbox");
    if (box) {
      const values = box.trim().split(/[\s,]+/).map(Number);
      if (values.length !== 4 || values.some(v => !Number.isFinite(v)) || values[2] <= 0 || values[3] <= 0) {
        throw new Error("SVG viewBox 无效，请提供四个数值且宽高大于零");
      }
    }
  }
}

export function validateReadingSvg(
  doc: ReturnType<typeof parseHTML>["document"], references: ReadingSvgReference[],
): void {
  const verify = ({ property, id }: ReadingSvgReference) => {
    const target = doc.getElementById(id);
    if (!target?.closest("svg") || !referenceTargets[property].includes(target.localName.toLowerCase())) {
      throw new Error(`SVG ${property} 引用的绘图定义不存在或类型不匹配：${id}`);
    }
  };
  references.forEach(verify);
  for (const svg of doc.querySelectorAll("svg")) {
    if (svg.querySelectorAll("*").length > 1500) throw new Error("SVG 图解过于复杂，请精简节点");
    for (const node of [svg, ...svg.querySelectorAll("*")]) {
      for (const property of Object.keys(referenceTargets)) {
        const value = node.getAttribute(property);
        if (value?.startsWith("url(")) verify(readingSvgReference(property, value.slice(4, -1)));
      }
      for (const attr of ["aria-labelledby", "aria-describedby"]) {
        const ids = node.getAttribute(attr)?.trim().split(/\s+/) ?? [];
        if (ids.some(id => !doc.getElementById(id))) throw new Error("SVG 图解的文字说明引用不存在");
      }
    }
  }
}
