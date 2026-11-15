// 从渲染层生成原生界面的品牌图标与平台元数据：来源平台 logo、模型厂商图标、两者的名称与配色。
// Generate the brand icons and the platform / model-category tables of the native UI from the renderer sources.
//
//   node apps/desktop-go/scripts/gen-native-brand-icons.mjs           # 重写两个 brand_gen.go
//   node apps/desktop-go/scripts/gen-native-brand-icons.mjs --check   # 只比较：不一致就非零退出
//
// 输入（渲染层，切换前仍是事实来源）：
//   components/ui/PlatformLogos.tsx       各平台 logo（只允许 <svg> + <path>，见 parsePlatformLogos）
//   components/library/platform-meta.tsx  平台 → 名称键、图标、着色 class
//   components/ui/ModelIcons.tsx          模型分类 → 厂商图标，特殊分类（Custom/Other/香蕉）与字母兜底的样式
//   assets/providers/*.svg                厂商图标源文件
// 输出：
//   native/kit/icons/brand_gen.go   品牌 SVG 源（icons 包，与 lucide 清单分开）
//   native/kit/brand_gen.go         平台表与模型分类表（kit 包，名称经 locales 取，随界面语言）
//
// 解析是针对现有写法的严格正则：渲染层改了结构，这里会抛出明确的错误而不是悄悄生成残缺的表。
// 渲染层删除（切换）之后，本脚本随之退役，生成的 Go 文件成为源码。

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { lucideCanonical } from "./gen-native-icons.mjs";

const REPO_ROOT = path.resolve(import.meta.dirname, "../../..");
const RENDERER = path.join(REPO_ROOT, "apps/desktop/src/renderer");
const SRC = {
  logos: path.join(RENDERER, "components/ui/PlatformLogos.tsx"),
  models: path.join(RENDERER, "components/ui/ModelIcons.tsx"),
  meta: path.join(RENDERER, "components/library/platform-meta.tsx"),
  providers: path.join(RENDERER, "assets/providers"),
};
const OUT_ICONS = path.join(REPO_ROOT, "apps/desktop-go/native/kit/icons/brand_gen.go");
const OUT_KIT = path.join(REPO_ROOT, "apps/desktop-go/native/kit/brand_gen.go");

/** Tailwind 默认色板里渲染层实际用到的色阶；用到新的色阶时在这里补，不会悄悄取错色。 */
const TAILWIND = { "green-600": "#16a34a", "green-400": "#4ade80" };

const HEX_RE = /^#[0-9a-fA-F]{6}$/;

function readText(file) {
  // Windows 检出的工作区文件是 CRLF；Go 原始字符串会丢掉 \r，必须先统一成 LF 才能让产物与平台无关
  return fs.readFileSync(file, "utf8").replace(/^\uFEFF/, "").replace(/\r\n/g, "\n");
}

/** 去掉块注释与行注释（行注释前必须是空白或分隔符，避免误伤 "http://" 这类字符串）。 */
export function stripComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[\s,;{(])\/\/.*$/gm, "$1");
}

/** 解析 JSX 属性文本；值只允许字符串，或 className={className} 这一种表达式。 */
export function parseJsxAttrs(text, where) {
  const re = /([A-Za-z][\w-]*)\s*=\s*(?:"([^"]*)"|\{([^{}]*)\})/g;
  const attrs = {};
  for (const m of text.matchAll(re)) {
    if (m[1] in attrs) throw new Error(`${where}：属性 ${m[1]} 重复`);
    if (m[2] === undefined) {
      if (m[1] !== "className" || m[3].trim() !== "className") throw new Error(`${where}：不支持的属性表达式 ${m[1]}={${m[3]}}`);
      attrs[m[1]] = null;
    } else attrs[m[1]] = m[2];
  }
  const leftover = text.replace(re, "").trim();
  if (leftover) throw new Error(`${where}：无法识别的属性文本 ${JSON.stringify(leftover)}`);
  return attrs;
}

const SVG_ATTRS = new Set(["className", "viewBox", "fill", "xmlns", "aria-hidden"]);
const PATH_ATTRS = { d: "d", fill: "fill", fillRule: "fill-rule", clipRule: "clip-rule" };

/**
 * PlatformLogos.tsx → [{ key, viewBox, mono, svg }]。
 * 只接受「一个 <svg> 里若干 <path>」的写法；mono 表示根 fill 是 currentColor（用文字色着色），否则按自带颜色画。
 */
export function parsePlatformLogos(src) {
  const code = stripComments(src);
  const re = /export function (\w+?)Logo\(\{ className \}: PlatformLogoProps\)\s*\{\s*return \(\s*<svg\b([\s\S]*?)>\s*([\s\S]*?)<\/svg>\s*\);\s*\}/g;
  const logos = [];
  for (const m of code.matchAll(re)) {
    const key = m[1].toLowerCase();
    const where = `PlatformLogos.tsx 的 ${m[1]}Logo`;
    const attrs = parseJsxAttrs(m[2], where);
    for (const k of Object.keys(attrs)) if (!SVG_ATTRS.has(k)) throw new Error(`${where}：<svg> 出现了不支持的属性 ${k}`);
    if (!attrs.viewBox || !/^-?\d+(\.\d+)?( -?\d+(\.\d+)?){3}$/.test(attrs.viewBox)) throw new Error(`${where}：缺少合法的 viewBox`);
    if (!attrs.fill) throw new Error(`${where}：<svg> 缺少 fill`);
    const pathRe = /<path\b([\s\S]*?)\/>/g;
    const paths = [];
    for (const p of m[3].matchAll(pathRe)) {
      const pa = parseJsxAttrs(p[1], `${where} 的 <path>`);
      if (!pa.d) throw new Error(`${where}：<path> 缺少 d`);
      const out = [];
      for (const [k, v] of Object.entries(pa)) {
        if (!(k in PATH_ATTRS)) throw new Error(`${where}：<path> 出现了不支持的属性 ${k}`);
        out.push(` ${PATH_ATTRS[k]}="${v}"`);
      }
      paths.push(`<path${out.join("")}/>`);
    }
    if (m[3].replace(pathRe, "").trim() !== "") throw new Error(`${where}：<svg> 里除 <path> 外还有别的内容`);
    if (paths.length === 0) throw new Error(`${where}：没有 <path>`);
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${attrs.viewBox}" fill="${attrs.fill}">${paths.join("")}</svg>`;
    logos.push({ key, fn: `${m[1]}Logo`, viewBox: attrs.viewBox, mono: attrs.fill === "currentColor", svg });
  }
  const declared = (code.match(/export function \w+Logo\(/g) ?? []).length;
  if (declared !== logos.length) throw new Error(`PlatformLogos.tsx 有 ${declared} 个 Logo 函数，只识别出 ${logos.length} 个：写法变了，请更新生成器`);
  return logos;
}

/** "text-foreground" 等着色 class → { kind, … }。 */
export function parseColorClass(cls, where) {
  const tokens = cls.trim().split(/\s+/);
  if (tokens.length === 1) {
    if (tokens[0] === "text-foreground") return { kind: "foreground" };
    if (tokens[0] === "text-current") return { kind: "current" };
    const fixed = tokens[0].match(/^text-\[(#[0-9a-fA-F]{6})\]$/);
    if (fixed) return { kind: "fixed", hex: fixed[1] };
  }
  if (tokens.length === 2) {
    const light = tokens[0].match(/^text-([a-z]+-\d+)$/);
    const dark = tokens[1].match(/^dark:text-([a-z]+-\d+)$/);
    if (light && dark) {
      if (!TAILWIND[light[1]] || !TAILWIND[dark[1]]) throw new Error(`${where}：色阶 ${light[1]} / ${dark[1]} 不在生成器的 TAILWIND 表里，请补上`);
      return { kind: "pair", light: TAILWIND[light[1]], dark: TAILWIND[dark[1]] };
    }
  }
  throw new Error(`${where}：无法识别的着色 class ${JSON.stringify(cls)}`);
}

/** 取 `{` 开头的对象字面量（含花括号）：从 start 处的 `{` 起按括号配对。 */
function objectLiteralAt(code, start, where) {
  if (code[start] !== "{") throw new Error(`${where}：位置 ${start} 不是 {`);
  let depth = 0;
  for (let i = start; i < code.length; i++) {
    if (code[i] === "{") depth++;
    else if (code[i] === "}" && --depth === 0) return code.slice(start, i + 1);
  }
  throw new Error(`${where}：花括号不配对`);
}

/** platform-meta.tsx → { platforms: [{ id, labelKey, fallback, icon, color }], unknown }。 */
export function parsePlatformMeta(src) {
  const code = stripComments(src);
  const entryRe = /(\w+):\s*\{\s*labelKey:\s*"([^"]+)",\s*fallback:\s*"([^"]*)",\s*Icon:\s*(\w+),\s*colorClass:\s*"([^"]+)",?\s*\}/g;
  const parseEntry = (m) => ({
    labelKey: m[2],
    fallback: m[3],
    icon: m[4],
    color: parseColorClass(m[5], `platform-meta.tsx 的 ${m[1]}`),
  });

  const head = code.match(/export const SOURCE_PLATFORM_META\b[^=]*=\s*/);
  if (!head) throw new Error("platform-meta.tsx：找不到 SOURCE_PLATFORM_META");
  const body = objectLiteralAt(code, head.index + head[0].length, "SOURCE_PLATFORM_META");
  const platforms = [...body.matchAll(entryRe)].map((m) => ({ id: m[1], ...parseEntry(m) }));
  const declared = (body.match(/labelKey:/g) ?? []).length;
  if (declared !== platforms.length) throw new Error(`SOURCE_PLATFORM_META 有 ${declared} 项，只识别出 ${platforms.length} 项：写法变了，请更新生成器`);

  const unknownHead = code.match(/const UNKNOWN_PLATFORM_META\b[^=]*=\s*/);
  if (!unknownHead) throw new Error("platform-meta.tsx：找不到 UNKNOWN_PLATFORM_META");
  const unknownBody = objectLiteralAt(code, unknownHead.index + unknownHead[0].length, "UNKNOWN_PLATFORM_META");
  const um = [...`unknown: ${unknownBody}`.matchAll(entryRe)];
  if (um.length !== 1) throw new Error("UNKNOWN_PLATFORM_META 写法变了，请更新生成器");
  return { platforms, unknown: parseEntry(um[0]) };
}

/** "linear-gradient(135deg, #eef2ff 0%, #dbeafe 100%)" → { angle, from, to }（颜色是十六进制）。 */
export function parseHexGradient(text, where) {
  const m = text.match(/^linear-gradient\((\d+)deg,\s*(#[0-9a-fA-F]{6})\s+0%,\s*(#[0-9a-fA-F]{6})\s+100%\)$/);
  if (!m) throw new Error(`${where}：无法识别的渐变 ${JSON.stringify(text)}`);
  return { angle: Number(m[1]), from: { hex: m[2] }, to: { hex: m[3] } };
}

/** "1px solid #fde047" 或 "1px solid rgba(37,99,235,0.18)" → 颜色。 */
export function parseBorder(text, where) {
  const hex = text.match(/^1px solid (#[0-9a-fA-F]{6})$/);
  if (hex) return { hex: hex[1] };
  const rgba = text.match(/^1px solid rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)$/);
  if (rgba) return { rgba: [Number(rgba[1]), Number(rgba[2]), Number(rgba[3]), Number(rgba[4])] };
  throw new Error(`${where}：无法识别的边框 ${JSON.stringify(text)}`);
}

/** ModelIcons.tsx → 分类表与各兜底样式。 */
export function parseModelIcons(src) {
  const code = stripComments(src);

  const imports = new Map();
  for (const m of code.matchAll(/import (\w+) from "\.\.\/\.\.\/assets\/providers\/([\w-]+)\.svg";/g)) imports.set(m[1], m[2]);

  const srcHead = code.match(/const CATEGORY_ICON_SRC\b[^=]*=\s*/);
  if (!srcHead) throw new Error("ModelIcons.tsx：找不到 CATEGORY_ICON_SRC");
  const srcBody = objectLiteralAt(code, srcHead.index + srcHead[0].length, "CATEGORY_ICON_SRC");
  const categories = [];
  for (const m of srcBody.matchAll(/(?:"([^"]+)"|([A-Za-z_]\w*)):\s*(\w+|""),/g)) {
    const name = m[1] ?? m[2];
    const ref = m[3];
    if (ref === '""') categories.push({ category: name, provider: null });
    else if (imports.has(ref)) categories.push({ category: name, provider: imports.get(ref) });
    else throw new Error(`ModelIcons.tsx：分类 ${name} 引用了未导入的 ${ref}`);
  }
  const lines = srcBody.split("\n").filter((l) => /:\s*(\w+|""),/.test(l)).length;
  if (lines !== categories.length) throw new Error(`CATEGORY_ICON_SRC 有 ${lines} 行，只识别出 ${categories.length} 项：写法变了，请更新生成器`);

  const specHead = code.match(/const SPECIAL_CATEGORY_ICON\b[^=]*=\s*/);
  if (!specHead) throw new Error("ModelIcons.tsx：找不到 SPECIAL_CATEGORY_ICON");
  const specBody = objectLiteralAt(code, specHead.index + specHead[0].length, "SPECIAL_CATEGORY_ICON");
  const specialIcons = new Map([...specBody.matchAll(/(\w+):\s*(\w+),/g)].map((m) => [m[1], m[2]]));

  const styleHead = code.match(/const SPECIAL_CATEGORY_ICON_STYLE\b[\s\S]*?=\s*(?=\{\s*\w+:\s*\{)/);
  if (!styleHead) throw new Error("ModelIcons.tsx：找不到 SPECIAL_CATEGORY_ICON_STYLE");
  const styleBody = objectLiteralAt(code, styleHead.index + styleHead[0].length, "SPECIAL_CATEGORY_ICON_STYLE");
  const specials = [];
  for (const m of styleBody.matchAll(/(\w+):\s*\{\s*background:\s*"([^"]+)",\s*color:\s*"([^"]+)",\s*border:\s*"([^"]+)",?\s*\}/g)) {
    if (!specialIcons.has(m[1])) throw new Error(`ModelIcons.tsx：特殊分类 ${m[1]} 没有图标`);
    if (!HEX_RE.test(m[3])) throw new Error(`ModelIcons.tsx：特殊分类 ${m[1]} 的前景色不是十六进制：${m[3]}`);
    specials.push({
      category: m[1],
      icon: specialIcons.get(m[1]),
      gradient: parseHexGradient(m[2], `特殊分类 ${m[1]} 的背景`),
      color: { hex: m[3] },
      border: parseBorder(m[4], `特殊分类 ${m[1]} 的边框`),
    });
  }
  if (specials.length !== specialIcons.size) throw new Error("SPECIAL_CATEGORY_ICON 与 SPECIAL_CATEGORY_ICON_STYLE 的分类对不上");
  const iconBox = code.match(/size=\{size \* ([\d.]+)\}\s*strokeWidth=\{([\d.]+)\}/);
  if (!iconBox) throw new Error("ModelIcons.tsx：找不到特殊分类图标的 size/strokeWidth");
  const boxRadius = code.match(/data-category-icon=\{category\}[\s\S]*?borderRadius:\s*(\d+),/);
  if (!boxRadius) throw new Error("ModelIcons.tsx：找不到特殊分类图标盒的 borderRadius");

  const imgStyle = code.match(/borderRadius:\s*(\d+),\s*objectFit:\s*"contain"/);
  if (!imgStyle) throw new Error("ModelIcons.tsx：找不到厂商图标 <img> 的圆角");

  const banana = code.match(
    /category === "([^"]+)"\)\s*\{[\s\S]*?fontSize:\s*size \* ([\d.]+),\s*background:\s*"([^"]+)",\s*borderRadius:\s*(\d+),\s*border:\s*"([^"]+)",[\s\S]*?>\s*([^\s<][^<]*?)\s*<\/div>/,
  );
  if (!banana) throw new Error("ModelIcons.tsx：找不到 🍌 分类的样式");

  const letter = code.match(
    /const fontSize = size \* ([\d.]+);[\s\S]*?background:\s*"linear-gradient\((\d+)deg,\s*rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\),\s*rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)\)",[\s\S]*?color:\s*"(#[0-9a-fA-F]{6})",[\s\S]*?fontWeight:\s*(\d+),/,
  );
  if (!letter) throw new Error("ModelIcons.tsx：找不到字母兜底的样式");

  return {
    categories,
    specials,
    specialIconScale: Number(iconBox[1]),
    specialBoxRadius: Number(boxRadius[1]),
    imageRadius: Number(imgStyle[1]),
    banana: {
      category: banana[1],
      fontScale: Number(banana[2]),
      gradient: parseHexGradient(banana[3], "🍌 分类的背景"),
      radius: Number(banana[4]),
      border: parseBorder(banana[5], "🍌 分类的边框"),
      glyph: banana[6],
    },
    letter: {
      fontScale: Number(letter[1]),
      angle: Number(letter[2]),
      from: letter.slice(3, 7).map(Number),
      to: letter.slice(7, 11).map(Number),
      color: { hex: letter[11] },
      weight: Number(letter[12]),
    },
  };
}

/** 带 <image> 的厂商 SVG（豆包）→ 内嵌 PNG 与它在画布里的位置；结构不符就抛错。 */
export function parseRasterSvg(key, text) {
  const where = `providers/${key}.svg`;
  const root = text.match(/^<svg\b([^>]*)>/);
  const vb = root?.[1].match(/viewBox="0 0 (\d+) (\d+)"/);
  if (!vb || vb[1] !== vb[2]) throw new Error(`${where}：画布不是正方形 viewBox`);
  const images = [...text.matchAll(/<image\b[^>]*xlink:href="data:image\/png;base64,([A-Za-z0-9+/=\s]+)"[^>]*\/>/g)];
  if (images.length !== 1 || (text.match(/<image\b/g) ?? []).length !== 1) throw new Error(`${where}：应当恰有一个内嵌 PNG <image>`);
  const png = images[0][1].replace(/\s+/g, "");
  if (!png.startsWith("iVBORw0KGgo")) throw new Error(`${where}：内嵌图片不是 PNG`);
  const rects = [...text.matchAll(/<rect x="(\d+)" y="(\d+)" width="(\d+)" height="(\d+)" fill="url\(#pattern[^)"]*\)"\s*\/>/g)];
  if (rects.length !== 1) throw new Error(`${where}：应当恰有一个以图案填充的 <rect>`);
  const [x, y, w, h] = rects[0].slice(1).map(Number);
  const canvas = Number(vb[1]);
  if (w !== h || x !== y || x * 2 + w !== canvas) throw new Error(`${where}：图片没有居中于画布（生成器的原生渲染按居中摆放）`);
  return { png, canvas, x, y, w, h };
}

/** assets/providers/*.svg → [{ key, svg?, raster?, degraded }]。 */
export function loadProviders(dir) {
  const out = [];
  for (const file of fs.readdirSync(dir).filter((f) => f.endsWith(".svg")).sort()) {
    const key = file.replace(/\.svg$/, "");
    const text = readText(path.join(dir, file)).trim();
    if (text.includes("`")) throw new Error(`providers/${file}：含反引号，无法放进 Go 原始字符串`);
    if (/<image\b/.test(text)) {
      out.push({ key, raster: parseRasterSvg(key, text), degraded: [] });
      continue;
    }
    // MyGo 的 SVG 渲染不画滤镜与图案：厂商图标里用到时记在产物注释里，画面与浏览器有差异
    const degraded = ["filter", "pattern"].filter((t) => new RegExp(`<${t}\\b`).test(text));
    out.push({ key, svg: text, degraded });
  }
  return out;
}

const pascal = (s) => s.split(/[-_]/).map((p) => p[0].toUpperCase() + p.slice(1)).join("");
const goStr = (s) => JSON.stringify(s);
/** i18n 键 library.platformDouyin → locales 的访问器 LibraryPlatformDouyin（MyGo 的命名：各段首字母大写后拼接）。 */
const accessor = (key) => key.split(".").map((p) => p[0].toUpperCase() + p.slice(1)).join("");

function goHex(h) {
  return `ui.Hex(${goStr(h.hex)})`;
}
function goColor(c) {
  if (c.hex) return goHex(c);
  if (c.rgba) return `ui.RGBA(${c.rgba[0]}, ${c.rgba[1]}, ${c.rgba[2]}, ${c.rgba[3]})`;
  throw new Error(`未知颜色 ${JSON.stringify(c)}`);
}
const goRgba = (a) => `ui.RGBA(${a[0]}, ${a[1]}, ${a[2]}, ${a[3]})`;
const goGradient = (g) => `gradientBox{angle: ${g.angle}, from: ${goColor(g.from)}, to: ${goColor(g.to)}}`;

function goBrandColor(c) {
  switch (c.kind) {
    case "foreground":
      return "brandColor{kind: colorForeground}";
    case "current":
      return "brandColor{kind: colorCurrent}";
    case "fixed":
      return `brandColor{kind: colorFixed, light: ${goHex({ hex: c.hex })}}`;
    case "pair":
      return `brandColor{kind: colorPair, light: ${goHex({ hex: c.light })}, dark: ${goHex({ hex: c.dark })}}`;
    default:
      throw new Error(`未知着色 ${c.kind}`);
  }
}

/** 汇总所有输入并做交叉校验，返回两个产物的文本。 */
export function render() {
  const logos = parsePlatformLogos(readText(SRC.logos));
  const meta = parsePlatformMeta(readText(SRC.meta));
  const models = parseModelIcons(readText(SRC.models));
  const providers = loadProviders(SRC.providers);

  // —— 键空间：平台 logo 与厂商图标共用 Brand 常量，不能撞名
  const brandKeys = new Map();
  const addBrand = (key, origin) => {
    if (brandKeys.has(key)) throw new Error(`品牌图标键 ${key} 重复：${brandKeys.get(key)} 与 ${origin}`);
    brandKeys.set(key, origin);
  };
  for (const l of logos) addBrand(l.key, `PlatformLogos.tsx ${l.fn}`);
  for (const p of providers) addBrand(p.key, `providers/${p.key}.svg`);

  // —— 交叉校验：表里引用的图标都必须存在
  const logoByFn = new Map(logos.map((l) => [l.fn, l]));
  const providerKeys = new Set(providers.map((p) => p.key));
  const lucideNames = new Set();
  const classify = (icon, where) => {
    if (logoByFn.has(icon)) return { brand: logoByFn.get(icon).key };
    if (/Icon$/.test(icon)) {
      lucideNames.add(icon);
      return { lucide: icon };
    }
    throw new Error(`${where}：无法识别的图标 ${icon}（既不是 PlatformLogos 里的 Logo，也不是 lucide 的 XxxIcon）`);
  };
  const platformIcons = meta.platforms.map((p) => ({ ...p, ...classify(p.icon, `platform-meta.tsx 的 ${p.id}`) }));
  const unknownIcon = { ...meta.unknown, ...classify(meta.unknown.icon, "UNKNOWN_PLATFORM_META") };
  for (const s of models.specials) lucideNames.add(s.icon);
  for (const c of models.categories) if (c.provider && !providerKeys.has(c.provider)) throw new Error(`ModelIcons.tsx：分类 ${c.category} 指向不存在的厂商图标 ${c.provider}`);
  const canonical = lucideCanonical([...lucideNames]);
  for (const [n, c] of canonical) if (!c) throw new Error(`lucide-react 里没有 ${n}`);

  // —— 产物 1：icons/brand_gen.go
  const iconsLines = [
    "// Code generated by apps/desktop-go/scripts/gen-native-brand-icons.mjs; DO NOT EDIT.",
    "",
    "package icons",
    "",
    "// 品牌图标的键：平台 logo 取 PlatformLogos.tsx 里 XxxLogo 的小写 xxx，厂商图标取 assets/providers 的文件名。",
    "const (",
  ];
  const keys = [...brandKeys.keys()].sort();
  for (const k of keys) iconsLines.push(`\tBrand${pascal(k)} Brand = ${goStr(k)}`);
  iconsLines.push(")", "", "// brandSources 是各品牌图标的源：矢量（svg，mono 表示用文字色着色）或内嵌位图（raster）。", "var brandSources = map[Brand]brandSource{");
  const logoByKey = new Map(logos.map((l) => [l.key, l]));
  const providerByKey = new Map(providers.map((p) => [p.key, p]));
  for (const k of keys) {
    const l = logoByKey.get(k);
    if (l) {
      iconsLines.push(`\tBrand${pascal(k)}: {svg: \`${l.svg}\`, mono: ${l.mono}},`);
      continue;
    }
    const p = providerByKey.get(k);
    if (p.degraded.length > 0) iconsLines.push(`\t// 含 ${p.degraded.join("、")}：MyGo 不画，与浏览器里的样子有差异（柔化光晕、图案填充）。`);
    if (p.raster) {
      const r = p.raster;
      iconsLines.push(`\tBrand${pascal(k)}: {raster: &rasterSource{png: ${goStr(r.png)}, canvas: ${r.canvas}, size: ${r.w}}},`);
    } else iconsLines.push(`\tBrand${pascal(k)}: {svg: \`${p.svg}\`},`);
  }
  iconsLines.push("}", "");

  // —— 产物 2：kit/brand_gen.go
  const kitLines = [
    "// Code generated by apps/desktop-go/scripts/gen-native-brand-icons.mjs; DO NOT EDIT.",
    "",
    "package kit",
    "",
    "import (",
    '\t"github.com/egoist/mygo/ui"',
    "",
    '\t"guizhi-mygo-shell/native/kit/icons"',
    '\t"guizhi-mygo-shell/native/locales"',
    ")",
    "",
    "// platformTable 对应渲染层的 SOURCE_PLATFORM_META（侧栏「平台」分区、表格「来源」列）。",
    "var platformTable = map[string]platformMeta{",
  ];
  const platformEntry = (p) => {
    const icon = p.brand ? `brand: icons.Brand${pascal(p.brand)}` : `lucide: icons.${canonical.get(p.lucide)}`;
    return `{label: locales.${accessor(p.labelKey)}, ${icon}, color: ${goBrandColor(p.color)}}`;
  };
  for (const p of platformIcons) kitLines.push(`\t${goStr(p.id)}: ${platformEntry(p)},`);
  kitLines.push("}", "", "// unknownPlatform 对应 UNKNOWN_PLATFORM_META：数据库里的平台值本版本不认识时用它。", `var unknownPlatform = platformMeta${platformEntry(unknownIcon)}`, "");

  kitLines.push("// modelBrandByCategory 对应 CATEGORY_ICON_SRC；没有厂商图标的分类（如 ERNIE）不在表里，走字母兜底。", "var modelBrandByCategory = map[string]icons.Brand{");
  for (const c of models.categories) if (c.provider) kitLines.push(`\t${goStr(c.category)}: icons.Brand${pascal(c.provider)},`);
  kitLines.push("}", "");

  kitLines.push("// modelSpecialByCategory 对应 SPECIAL_CATEGORY_ICON 与 SPECIAL_CATEGORY_ICON_STYLE。", "var modelSpecialByCategory = map[string]modelSpecial{");
  for (const s of models.specials) {
    kitLines.push(
      `\t${goStr(s.category)}: {icon: icons.${canonical.get(s.icon)}, box: ${goGradient(s.gradient)}, color: ${goColor(s.color)}, border: ${goColor(s.border)}},`,
    );
  }
  kitLines.push("}", "");

  const b = models.banana;
  kitLines.push(
    "// 模型分类图标的几何与兜底样式，取自 ModelIcons.tsx。",
    "const (",
    `\tmodelImageRadius       float32 = ${models.imageRadius} // 厂商图标 <img> 的圆角`,
    `\tmodelSpecialRadius     float32 = ${models.specialBoxRadius} // 特殊分类图标盒的圆角`,
    `\tmodelSpecialIconScale  float32 = ${models.specialIconScale} // 特殊分类里图标占盒边长的比例`,
    `\tmodelBananaCategory            = ${goStr(b.category)}`,
    `\tmodelBananaGlyph               = ${goStr(b.glyph)}`,
    `\tmodelBananaFontScale   float32 = ${b.fontScale}`,
    `\tmodelBananaRadius      float32 = ${b.radius}`,
    `\tmodelLetterFontScale   float32 = ${models.letter.fontScale}`,
    `\tmodelLetterFontWeight          = ${models.letter.weight}`,
    ")",
    "",
    "var (",
    `\tmodelBananaBox    = ${goGradient(b.gradient)}`,
    `\tmodelBananaBorder = ${goColor(b.border)}`,
    `\tmodelLetterFrom   = ${goRgba(models.letter.from)}`,
    `\tmodelLetterTo     = ${goRgba(models.letter.to)}`,
    `\tmodelLetterAngle  = float32(${models.letter.angle})`,
    `\tmodelLetterColor  = ${goColor(models.letter.color)}`,
    ")",
    "",
  );

  return {
    icons: gofmt(iconsLines.join("\n")),
    kit: gofmt(kitLines.join("\n")),
    counts: { logos: logos.length, providers: providers.length, platforms: platformIcons.length, categories: models.categories.length },
  };
}

/** 产物必须已是 gofmt 格式（CI 有 gofmt 检查，常量块的列对齐靠它）。 */
function gofmt(src) {
  return execFileSync("gofmt", [], { input: src, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
}

export function main(argv) {
  const { icons, kit, counts } = render();
  const outputs = [
    [OUT_ICONS, icons],
    [OUT_KIT, kit],
  ];
  const summary = `平台 logo ${counts.logos}，厂商图标 ${counts.providers}，平台 ${counts.platforms}，模型分类 ${counts.categories}`;
  // Windows 检出（core.autocrlf=true）的工作区文件是 CRLF，比较前统一成 LF，否则 --check 永远报过期
  const read = (f) => (fs.existsSync(f) ? fs.readFileSync(f, "utf8").replace(/\r\n/g, "\n") : null);
  if (argv.includes("--check")) {
    const stale = outputs.filter(([f, text]) => read(f) !== text);
    for (const [f] of stale) console.error(`过期：${path.relative(REPO_ROOT, f)}（请运行 node apps/desktop-go/scripts/gen-native-brand-icons.mjs 并提交）`);
    if (stale.length === 0) console.log(`品牌图标与渲染层一致（${summary}）`);
    return stale.length === 0 ? 0 : 1;
  }
  for (const [f, text] of outputs) {
    fs.mkdirSync(path.dirname(f), { recursive: true });
    fs.writeFileSync(f, text);
    console.log(`写入 ${path.relative(REPO_ROOT, f)}`);
  }
  console.log(summary);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
