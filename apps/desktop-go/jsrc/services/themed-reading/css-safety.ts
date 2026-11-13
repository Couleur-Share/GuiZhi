import * as css from "css-tree";

const properties = new Set((
  "color background background-color background-image background-size background-position background-repeat " +
  "border border-width border-style border-color border-radius border-top border-right border-bottom border-left " +
  "border-top-left-radius border-top-right-radius border-bottom-left-radius border-bottom-right-radius box-shadow box-sizing " +
  "width min-width max-width margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left " +
  "margin-block margin-inline margin-block-start margin-block-end margin-inline-start margin-inline-end " +
  "padding-block padding-inline padding-block-start padding-block-end padding-inline-start padding-inline-end " +
  "font-size font-weight font-style font-variant line-height letter-spacing word-spacing text-align text-decoration " +
  "white-space word-break overflow-wrap vertical-align display flex-direction flex-wrap align-items align-content justify-content " +
  "gap row-gap column-gap grid-template-columns list-style-type list-style-position border-collapse border-spacing table-layout " +
  "object-fit object-position fill stroke stroke-width outline outline-width outline-style outline-color outline-offset cursor"
).split(" "));
const functions = new Set(["rgb", "rgba", "hsl", "hsla", "oklch", "oklab", "lab", "lch", "color-mix", "linear-gradient", "radial-gradient", "repeating-linear-gradient", "repeating-radial-gradient", "min", "max", "clamp", "calc", "minmax", "repeat", "var"]);
const variables = /^--(?:theme-[a-z0-9-]+|reader-font-size)$/;

function boundedLength(node: css.CssNode, px: number, relative: number): boolean {
  if (node.type === "Number") return Number(node.value) === 0;
  if (node.type !== "Dimension") return false;
  const value = Number(node.value), unit = node.unit.toLowerCase();
  return value >= 0 && Number.isFinite(value) && ((unit === "px" && value <= px) || ((unit === "em" || unit === "rem") && value <= relative));
}

type LengthBound = { min: number; max: number; unit: string | null };

function functionArgs(node: css.FunctionNode): css.CssNode[][] {
  const groups: css.CssNode[][] = [[]];
  node.children.forEach((part) => {
    if (part.type === "Operator" && part.value === ",") groups.push([]);
    else groups[groups.length - 1].push(part);
  });
  return groups;
}

/** 在限额内静态求界；不执行变量、乘除，也不让越界中间值藏进 min/clamp。 */
function lengthBound(node: css.CssNode, px: number, relative: number, percent = false, depth = 0): LengthBound | null {
  if (depth > 6) return null;
  if (node.type === "Number") return Number(node.value) === 0 ? { min: 0, max: 0, unit: "" } : null;
  if (node.type === "Dimension" || node.type === "Percentage") {
    const unit = node.type === "Percentage" ? "%" : node.unit.toLowerCase();
    const limit = unit === "px" ? px : ["em", "rem"].includes(unit) ? relative : unit === "%" && percent ? 100 : 0;
    const amount = Number(node.value) / limit;
    return limit > 0 && Number.isFinite(amount) && amount >= 0 && amount <= 1 ? { min: amount, max: amount, unit } : null;
  }
  if (node.type !== "Function") return null;
  const name = node.name.toLowerCase();
  if (!["min", "max", "clamp", "calc"].includes(name)) return null;
  const args = functionArgs(node);
  const read = (part: css.CssNode) => lengthBound(part, px, relative, percent, depth + 1);
  if (name === "calc") {
    const parts = args[0];
    if (args.length !== 1 || !parts.length || parts.length > 15 || parts.length % 2 === 0) return null;
    let total = read(parts[0]);
    if (!total) return null;
    for (let index = 1; index < parts.length; index += 2) {
      const operator = parts[index], next = read(parts[index + 1]);
      if (operator.type !== "Operator" || !next || !["+", "-"].includes(operator.value.trim())) return null;
      const subtract = operator.value.trim() === "-";
      // 混合单位减法无法在所有字号下保证非负，只接受同单位的可证明减法。
      if (subtract && next.max !== 0 && (total.unit === null || total.unit !== next.unit)) return null;
      total = {
        min: subtract ? total.min - next.max : total.min + next.min,
        max: subtract ? total.max - next.min : total.max + next.max,
        unit: total.max === 0 ? next.unit : next.max === 0 || total.unit === next.unit ? total.unit : null,
      };
      if (total.min < 0 || total.max > 1) return null;
    }
    return total;
  }
  if (!args.length || args.length > 8 || (name === "clamp" && args.length !== 3) || args.some((group) => group.length !== 1)) return null;
  const bounds = args.map((group) => read(group[0]));
  if (bounds.some((bound) => !bound)) return null;
  const valid = bounds as LengthBound[];
  const sameUnit = valid.every((bound) => bound.unit === valid[0].unit);
  // 不同单位的比较只保留保守上下界，禁止后续通过减法利用换算误差。
  return {
    min: Math.min(...valid.map((bound) => bound.min)),
    max: Math.max(...valid.map((bound) => bound.max)),
    unit: sameUnit ? valid[0].unit : null,
  };
}

function cappedLength(node: css.CssNode, percent: string, px: number, relative: number): boolean {
  if (node.type !== "Function" || node.name.toLowerCase() !== "min") return false;
  const args = functionArgs(node);
  return args.length === 2 && args.every((group) => group.length === 1) &&
    args[1][0].type === "Percentage" && args[1][0].value === percent && !!lengthBound(args[0][0], px, relative);
}

/** 保留有界表达式，同时给每层留白加容器宽度上限，避免嵌套累计挤出正文。 */
function safeSpacing(node: css.Declaration): boolean {
  if (node.value.type !== "Value") return false;
  const property = node.property.toLowerCase();
  const children = node.value.children.toArray();
  if (!children.length || children.length > 4 || !children.every((part) => {
    if (part.type === "Identifier") return (part.name === "auto" && property.startsWith("margin")) || (part.name === "normal" && property.endsWith("gap"));
    return !!lengthBound(part, 64, 4) || cappedLength(part, "4", 64, 4);
  })) return false;
  if (property === "border-spacing") return true;
  if (property === "row-gap" || property === "gap") {
    if (children.length > (property === "gap" ? 2 : 1)) return false;
    // Grid 的百分比行间距不参与自动高度计算；纵向解开旧宽度上限，让容器包含全部行。
    const row = cappedLength(children[0], "4", 64, 4) && children[0].type === "Function" ? functionArgs(children[0])[0][0] : children[0];
    let value = css.generate(row);
    if (property === "gap") {
      const column = children[1] || children[0];
      const columnValue = css.generate(column);
      value += " " + (["Dimension", "Function"].includes(column.type) && !cappedLength(column, "4", 64, 4) ? `min(${columnValue},4%)` : columnValue);
    }
    node.value = css.parse(value, { context: "value" }) as css.Value;
    return true;
  }
  node.value.children.forEach((part, item, list) => {
    if (!["Dimension", "Function"].includes(part.type) || cappedLength(part, "4", 64, 4)) return;
    const bounded = css.parse(`min(${css.generate(part)},4%)`, { context: "value" });
    if (bounded.type === "Value") list.replace(item, bounded.children);
  });
  return true;
}

function gridBreadth(part: css.CssNode, minimum = false): string | null {
  if (part.type === "Dimension" && part.unit.toLowerCase() === "fr") {
    const amount = Number(part.value);
    return amount >= .25 && amount <= 12 ? css.generate(part) : null;
  }
  if (part.type === "Identifier") return ["auto", "min-content", "max-content"].includes(part.name) ? "1fr" : null;
  if (!lengthBound(part, 2400, 160, true)) return null;
  if (!minimum) {
    // 最大轨道至少容纳正文：128px / 8em(rem)，表达式也必须证明下界。
    const readable = lengthBound(part, 2560, 160, true);
    if (!readable || readable.min < .05) return null;
    let tinyPercentage = false;
    css.walk(part, (node) => { if (node.type === "Percentage" && Number(node.value) < 12.5) tinyPercentage = true; });
    if (tinyPercentage) return null;
  }
  return css.generate(part);
}

/** 轨道保留原始宽度/比例，并把不可缩窄的自动最小值改成 0。 */
function safeGrid(node: css.Declaration): boolean {
  if (node.value.type !== "Value") return false;
  let trackCount = 0, automaticRepeats = 0;
  const fractions: number[] = [];
  const rememberFraction = (part: css.CssNode) => {
    if (part.type === "Dimension" && part.unit.toLowerCase() === "fr") fractions.push(Number(part.value));
    if (part.type === "Identifier") fractions.push(1);
  };
  const track = (part: css.CssNode, automatic = false): string | null => {
    if (part.type !== "Function" || part.name.toLowerCase() !== "minmax") {
      const breadth = gridBreadth(part);
      if (breadth) rememberFraction(part);
      return breadth && `minmax(0,${breadth})`;
    }
    const args = functionArgs(part);
    if (args.length !== 2 || args.some((group) => group.length !== 1)) return null;
    const minimum = args[0][0], maximum = gridBreadth(args[1][0]);
    if (!maximum || (!gridBreadth(minimum, true) && !cappedLength(minimum, "100", 2400, 160))) return null;
    rememberFraction(args[1][0]);
    if (!automatic) return `minmax(0,${maximum})`;
    // auto-fit 必须有固定大小才能计算列数；让原下限至多等于容器，不能改成无效的 minmax(0,1fr)。
    if (lengthBound(minimum, 2400, 160) && css.generate(minimum) !== "0") return `minmax(min(${css.generate(minimum)},100%),${maximum})`;
    if (cappedLength(minimum, "100", 2400, 160)) return `minmax(${css.generate(minimum)},${maximum})`;
    return args[1][0].type === "Dimension" && ["px", "rem", "em"].includes(args[1][0].unit) ? `minmax(0,${maximum})` : null;
  };
  const tracks: string[] = [];
  for (const part of node.value.children.toArray()) {
    if (part.type !== "Function" || part.name.toLowerCase() !== "repeat") {
      const value = track(part);
      if (!value) return false;
      tracks.push(value);
      trackCount++;
      continue;
    }
    const args = functionArgs(part);
    if (args.length !== 2 || args[0].length !== 1 || !args[1].length) return false;
    const count = args[0][0];
    const automatic = count.type === "Identifier" && count.name === "auto-fit";
    if (automatic && (++automaticRepeats > 1 || args[1].length !== 1)) return false;
    if (!automatic && (count.type !== "Number" || !Number.isInteger(Number(count.value)) || Number(count.value) < 1 || Number(count.value) > 12)) return false;
    const repeated = args[1].map((part) => track(part, automatic));
    if (repeated.some((value) => !value)) return false;
    trackCount += (count.type === "Number" ? Number(count.value) : 1) * repeated.length;
    tracks.push(`repeat(${css.generate(count)},${repeated.join(" ")})`);
  }
  if (!tracks.length || trackCount > 12) return false;
  if (fractions.length > 1 && Math.max(...fractions) / Math.min(...fractions) > 12) return false;
  node.value = css.parse(tracks.join(" "), { context: "value" }) as css.Value;
  return true;
}

function safeGeometry(node: css.Declaration): boolean {
  if (node.property.toLowerCase() === "grid-template-columns") return safeGrid(node);
  if (node.value.type !== "Value" || node.value.children.size !== 1) return false;
  const part = node.value.children.first;
  if (part.type === "Identifier") return ["auto", "none", "fit-content", "min-content", "max-content"].includes(part.name);
  return !!lengthBound(part, 2400, 160, true);
}

function luminance(hex: string): number | null {
  if (!/^#(?:[a-f\d]{3}|[a-f\d]{6})$/i.test(hex)) return null;
  const expanded = hex.length === 4 ? [...hex.slice(1)].map((char) => char + char).join("") : hex.slice(1);
  const channels = [0, 2, 4].map((index) => parseInt(expanded.slice(index, index + 2), 16) / 255).map((value) => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  return .2126 * channels[0] + .7152 * channels[1] + .0722 * channels[2];
}

/** 正文字色与底色必须同时声明；不可信透明色、变量或低对比组合不会进入页面。 */
export function themedColorContrast(surface: string, text: string): number {
  const first = luminance(surface), second = luminance(text);
  if (first == null || second == null) return 0;
  return (Math.max(first, second) + .05) / (Math.min(first, second) + .05);
}

function cleanPalettePairs(children: css.List<css.CssNode>) {
  const declarations = children.toArray().filter((node): node is css.Declaration => node.type === "Declaration");
  const find = (property: string) => declarations.filter((node) => node.property === property).at(-1);
  const surface = find("--theme-surface"), text = find("--theme-text");
  if (!surface && !text) return;
  if (!surface || !text || themedColorContrast(css.generate(surface.value), css.generate(text.value)) < 4.5) {
    children.forEach((node, item) => { if (node.type === "Declaration" && ["--theme-surface", "--theme-text"].includes(node.property)) children.remove(item); });
  }
}

function declarationAllowed(node: css.Declaration): boolean {
  const property = node.property.toLowerCase();
  const custom = /^--theme-[a-z0-9-]+$/.test(property);
  if (!custom && !properties.has(property)) return false;
  const value = css.generate(node.value).toLowerCase();
  if (/!|[<>]|url\s*\(|expression|\\/.test(value)) return false;
  let safe = true;
  css.walk(node.value, (part) => {
    if (part.type === "Raw" || part.type === "Url") safe = false;
    if (part.type === "Function") {
      if (!functions.has(part.name.toLowerCase())) safe = false;
      if (part.name.toLowerCase() === "var") {
        const first = part.children.first;
        if (first?.type !== "Identifier" || !variables.test(first.name)) safe = false;
      }
    }
    if (part.type === "Dimension" && /^(?:v[hwb]|[sld]v)/.test(part.unit)) safe = false;
    if ((part.type === "Dimension" || part.type === "Number" || part.type === "Percentage") && (Number(part.value) < 0 || !Number.isFinite(Number(part.value)))) safe = false;
  });
  if (property === "display" && !/^(block|inline|inline-block|flex|inline-flex|grid|inline-grid|table|table-row|table-cell|list-item)$/.test(value)) safe = false;
  if (property === "flex-direction" && /reverse/.test(value)) safe = false;
  if (property === "flex-wrap" && value !== "wrap") safe = false;
  if (property === "white-space" && !/^(normal|pre-wrap|break-spaces)$/.test(value)) safe = false;
  if (property === "font-size" && !/^(?:1[2-9]|[2-9]\d)(?:\.\d+)?px$|^(?:0\.[8-9]\d*|[1-5](?:\.\d+)?)(?:r?em)$|^var\(--reader-font-size\)$/.test(value)) safe = false;
  if (property === "font-style" && !/^(normal|italic)$/.test(value)) safe = false;
  if (property === "font-weight" && !/^(normal|bold|bolder|[3-9]00)$/.test(value)) safe = false;
  if (property === "line-height" && !/^(?:1\.[3-9]\d*|[2-3](?:\.\d+)?|normal)$/.test(value)) safe = false;
  if (property === "cursor" && !/^(auto|default|pointer)$/.test(value)) safe = false;
  if (property === "outline-offset" && (node.value.type !== "Value" || node.value.children.size !== 1 || !boundedLength(node.value.children.first, 8, .5))) safe = false;
  if (/^outline(?:-(?:width|style|color))?$/.test(property)) {
    if (/\b(?:none|hidden|transparent)\b/.test(value)) safe = false;
    css.walk(node.value, (part) => {
      if (part.type === "Function") safe = false;
      if (part.type === "Dimension" && !boundedLength(part, 4, .25)) safe = false;
      if ((part.type === "Dimension" || part.type === "Number") && Number(part.value) === 0) safe = false;
    });
  }
  if (/^(?:width|min-width|max-width)$/.test(property) && /^(?:0(?:px|em|rem|%)?|0\.)$/.test(value)) safe = false;
  if (/^(?:padding|margin|gap|row-gap|column-gap|border-spacing)/.test(property) && !safeSpacing(node)) safe = false;
  if (property === "vertical-align" && !/^(baseline|middle|top|bottom|sub|super|text-top|text-bottom)$/.test(value)) {
    if (node.value.type !== "Value" || !node.value.children.toArray().every((part) => boundedLength(part, 8, .5))) safe = false;
  }
  if (/^(?:letter-spacing|word-spacing)$/.test(property)) {
    css.walk(node.value, (part) => {
      if (part.type === "Percentage" || part.type === "Function") safe = false;
      if (part.type === "Dimension" && !boundedLength(part, property === "letter-spacing" ? 4 : 12, property === "letter-spacing" ? .25 : .75)) safe = false;
    });
  }
  if (/^(?:width|min-width|max-width|grid-template-columns)$/.test(property) && !safeGeometry(node)) safe = false;
  if (/^border(?:-(?:width|top|right|bottom|left))?$/.test(property) || property === "box-shadow") {
    css.walk(node.value, (part) => {
      if (part.type === "Function" && ["var", "calc"].includes(part.name.toLowerCase())) safe = false;
      if (part.type === "Dimension" && !boundedLength(part, property === "box-shadow" ? 32 : 8, property === "box-shadow" ? 2 : .5)) safe = false;
    });
    if (property === "box-shadow" && node.value.type === "Value") {
      let component = 0;
      node.value.children.forEach((part) => {
        if (part.type === "Operator" && part.value === ",") component = 0;
        if (part.type === "Dimension" || part.type === "Number") {
          component++;
          if (!boundedLength(part, component === 4 ? 8 : 32, component === 4 ? .5 : 2)) safe = false;
        }
      });
    }
  }
  return safe;
}

const interactionProperties = /^(?:border(?:-(?:color|style|width|top|right|bottom|left|radius))?|box-shadow|outline(?:-(?:color|style|width|offset))?|cursor|text-decoration)$/;

/** 交互只改变可见边界和链接提示，不能切换正文颜色、布局、变量或字号。 */
function cleanInteraction(children: css.List<css.CssNode>) {
  children.forEach((node, item) => {
    if (node.type !== "Declaration" || !interactionProperties.test(node.property.toLowerCase()) ||
      (node.property.toLowerCase() === "text-decoration" && !/\bunderline\b/.test(css.generate(node.value)))) children.remove(item);
  });
}

/** 只保留可重排的阅读样式和有界交互；不允许联网、遮挡、隐藏或改变正文顺序。 */
export function cleanThemedCss(value: string, stylesheet = true): string {
  try {
    const ast = css.parse(value, { context: stylesheet ? "stylesheet" : "declarationList", parseCustomProperty: true });
    css.walk(ast, {
      visit: "Declaration",
      enter(node, item, list) {
        if (!declarationAllowed(node)) list.remove(item);
        else node.important = false;
      },
    });
    css.walk(ast, { visit: "Block", enter(node) { cleanPalettePairs(node.children); } });
    if (ast.type === "DeclarationList") cleanPalettePairs(ast.children);
    if (stylesheet) {
      css.walk(ast, {
        visit: "Atrule",
        enter(node, item, list) {
          const query = node.prelude ? css.generate(node.prelude).toLowerCase() : "";
          if (node.name.toLowerCase() !== "media" || !node.block || !query || /[{}<>\\]|url|var\(/.test(query) || !/^[\w\s():.,/-]+$/.test(query)) list.remove(item);
        },
      });
      css.walk(ast, {
        visit: "Rule",
        enter(node, item, list) {
          // 开放导航反馈，不开放伪元素、全屏和可注入任意选择器的函数伪类。
          const selector = css.generate(node.prelude);
          if (/::|:(?!root\b|first-child\b|last-child\b|nth-child\([\dn+ -]+\)|hover\b|focus-visible\b|focus-within\b|target\b)|[\\<>]/.test(selector)) { list.remove(item); return; }
          if (/:(?:hover|focus-visible|focus-within|target)\b/.test(selector)) cleanInteraction(node.block.children);
          if (node.block.children.isEmpty) list.remove(item);
        },
      });
    }
    return css.generate(ast).replace(/</g, "\\3c ");
  } catch {
    return "";
  }
}
