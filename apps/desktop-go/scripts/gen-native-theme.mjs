// 把渲染层的主题令牌转成 Go：native/kit/tokens_gen.go（MyGo 路线，冻结）与 viva/tokens/tokens_gen.go（Viva 路线，普通 Go，不依赖任何界面工具包）。
// Generate the Go design tokens of the native UI from the renderer's CSS and settings.
//
//   node apps/desktop-go/scripts/gen-native-theme.mjs            # 写两个产物
//   node apps/desktop-go/scripts/gen-native-theme.mjs --check    # 只比较：任一产物与当前源不一致就非零退出
//
// 输入（切换前仍是外观的事实来源，改了它们重新生成即可，不需要两边各抄一遍）：
//   apps/desktop/src/renderer/styles/globals.css                    :root / .dark / .app-background-mode-image / .dark .app-background-mode-image 的自定义属性
//   apps/desktop/src/renderer/stores/settings/settings-appearance.ts MORANDI_THEMES（6 个色相预设）、FONT_SIZES（3 个字号档）
//   apps/desktop/src/renderer/stores/settings/settings-defaults.ts   默认的主题色与字号档
//
// 令牌的计算方式与浏览器一致（这是它能保证与渲染层逐值相同的原因）：
//   - 每个自定义属性在「声明它的那个元素」上求值：var() 先查该元素自己的声明，再查祖先。
//     所以壁纸模式（挂在 App 根 div 上的 .app-background-mode-image）里没有重写的令牌，沿用 html 上已算好的值，不随壁纸模式的 --card 变化。
//   - 有效层叠：浅色 = :root；深色 = :root ← .dark（html 级）；
//     壁纸浅色 = html 浅色 ← .app-background-mode-image（元素级）；
//     壁纸深色 = html 深色 ← .app-background-mode-image ← .dark .app-background-mode-image（后者更具体，只覆盖它写了的属性）。
//   - var(--theme-hue)、var(--theme-saturation) 是输入（设置里的色相与饱和度），生成的函数以 hue、sat 为参数。
//
// 只发射颜色与阴影两类令牌；渐变、backdrop-filter、blur 等没有对应原语的值不发射（数量打印在输出里）。

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const REPO_ROOT = path.resolve(import.meta.dirname, "../../..");
const RENDERER = path.join(REPO_ROOT, "apps/desktop/src/renderer");
const CSS_FILE = path.join(RENDERER, "styles/globals.css");
const APPEARANCE_FILE = path.join(RENDERER, "stores/settings/settings-appearance.ts");
const DEFAULTS_FILE = path.join(RENDERER, "stores/settings/settings-defaults.ts");
const OUT_FILE = path.join(REPO_ROOT, "apps/desktop-go/native/kit/tokens_gen.go");
const OUT_FILE_VIVA = path.join(REPO_ROOT, "apps/desktop-go/viva/tokens/tokens_gen.go");

/**
 * 两个输出目标。mygo：冻结的 MyGo 路线（类型取自 mygo/ui）；viva：普通 Go 的数据包，颜色与阴影是包内自己的类型，
 * 由 viva/kit 转成 gfx.Color 与 gfx.ShadowStyle。
 */
const TARGETS = {
  mygo: {
    out: OUT_FILE,
    pkg: "kit",
    imports: 'import "github.com/egoist/mygo/ui"',
    color: "ui.Color",
    shadow: "ShadowToken",
    transparent: "ui.Transparent",
    rgba: "ui.RGBA",
    fn: { light: "lightTokens", dark: "darkTokens", glassLight: "glassLightTokens", glassDark: "glassDarkTokens" },
    motion: { standard: "motionStandardMs", reduced: "motionReducedMs" },
    motionDoc: "kit.DurationName",
  },
  viva: {
    out: OUT_FILE_VIVA,
    pkg: "tokens",
    imports: "",
    color: "Color",
    shadow: "Shadow",
    transparent: "Transparent",
    rgba: "RGBA",
    fn: { light: "Light", dark: "Dark", glassLight: "GlassLight", glassDark: "GlassDark" },
    motion: { standard: "MotionStandardMs", reduced: "MotionReducedMs" },
    motionDoc: "viva/kit 的 DurationName",
  },
};

/** 输入变量：不作为令牌发射，而是生成函数的参数。 */
const INPUT_VARS = new Set(["--theme-hue", "--theme-saturation", "--base-font-size"]);

export function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

/**
 * 按花括号拆出叶子规则 [{selector, body}]；@layer 递归进入，其他 at-rule（@media、@keyframes…）整体跳过。
 */
export function parseRules(src) {
  const rules = [];
  let i = 0;
  while (i < src.length) {
    const open = src.indexOf("{", i);
    if (open < 0) break;
    let depth = 1;
    let j = open + 1;
    while (j < src.length && depth > 0) {
      if (src[j] === "{") depth += 1;
      else if (src[j] === "}") depth -= 1;
      j += 1;
    }
    const prelude = src.slice(i, open).split(";").pop().trim();
    const body = src.slice(open + 1, j - 1);
    if (prelude.startsWith("@layer")) rules.push(...parseRules(body));
    else if (!prelude.startsWith("@")) rules.push({ selector: prelude.replace(/\s+/g, " "), body });
    i = j;
  }
  return rules;
}

/** 一个规则体里的自定义属性：Map<名, 规范化后的值>；括号内的分号不算分隔。 */
export function parseDecls(body) {
  const decls = new Map();
  let depth = 0;
  let start = 0;
  const flush = (end) => {
    const part = body.slice(start, end).trim();
    const colon = part.indexOf(":");
    if (colon < 0) return;
    const name = part.slice(0, colon).trim();
    if (name.startsWith("--")) decls.set(name, part.slice(colon + 1).trim().replace(/\s+/g, " "));
  };
  for (let i = 0; i < body.length; i += 1) {
    const ch = body[i];
    if (ch === "(") depth += 1;
    else if (ch === ")") depth -= 1;
    else if (ch === ";" && depth === 0) {
      flush(i);
      start = i + 1;
    }
  }
  flush(body.length);
  return decls;
}

/** 在若干规则里找出选择器等于 selector 且声明了 requireVar 的那一块（取第一块）。 */
export function findBlock(rules, selector, requireVar) {
  for (const r of rules) {
    if (r.selector !== selector) continue;
    const decls = parseDecls(r.body);
    if (decls.has(requireVar)) return decls;
  }
  throw new Error(`globals.css 里找不到声明了 ${requireVar} 的 ${selector} 块`);
}

/** 以空格分隔的顶层记号（括号内的空格不分）。 */
function splitTop(text) {
  const out = [];
  let depth = 0;
  let cur = "";
  for (const ch of text) {
    if (ch === "(") depth += 1;
    if (ch === ")") depth -= 1;
    if (/\s/.test(ch) && depth === 0) {
      if (cur) out.push(cur);
      cur = "";
    } else cur += ch;
  }
  if (cur) out.push(cur);
  return out;
}

/** 线性表达式：value = c + hue*H + sat*S（H、S 是生成函数的参数）。 */
function parseComp(text) {
  const t = text.trim().replace(/%$/, "");
  let m = /^(-?\d+(?:\.\d+)?)$/.exec(t);
  if (m) return { c: parseFloat(m[1]), hue: 0, sat: 0 };
  if (t === "var(--theme-hue)") return { c: 0, hue: 1, sat: 0 };
  if (t === "var(--theme-saturation)") return { c: 0, hue: 0, sat: 1 };
  m = /^calc\(var\(--theme-saturation\)\s*\*\s*(\d+(?:\.\d+)?)\)$/.exec(t);
  if (m) return { c: 0, hue: 0, sat: parseFloat(m[1]) };
  return null;
}

function parseTriplet(text) {
  const parts = splitTop(text);
  if (parts.length !== 3) return null;
  const comps = parts.map(parseComp);
  return comps.every(Boolean) ? { h: comps[0], s: comps[1], l: comps[2] } : null;
}

const SHADOW_RE =
  /^(-?\d+(?:\.\d+)?)(?:px)?\s+(-?\d+(?:\.\d+)?)(?:px)?\s+(\d+(?:\.\d+)?)(?:px)?(?:\s+(-?\d+(?:\.\d+)?)(?:px)?)?\s+rgba\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*,\s*([\d.]+)\s*\)$/;

/**
 * 求一个自定义属性的值。scopes 由近到远（元素级在前，html 级在后），Map<名, 原始值>。
 * 返回 {type:"color", h, s, l, a} | {type:"shadow", x, y, blur, spread, r, g, b, a} | {type:"none"} | {type:"other"}。
 * var() 从「声明它的那一层」开始查，与 CSS 的计算值继承一致。
 */
export function resolveVar(name, scopes, from = 0, trail = []) {
  if (trail.includes(name)) throw new Error(`自定义属性循环引用：${[...trail, name].join(" → ")}`);
  for (let i = from; i < scopes.length; i += 1) {
    if (scopes[i].has(name)) return evalValue(scopes[i].get(name), scopes, i, [...trail, name]);
  }
  return { type: "missing", name };
}

export function evalValue(raw, scopes, at, trail = []) {
  const v = raw.trim();
  if (v === "none") return { type: "none" };
  if (v === "transparent") return { type: "color", h: parseComp("0"), s: parseComp("0"), l: parseComp("0"), a: 0 };
  let m = /^var\(\s*(--[\w-]+)\s*\)$/.exec(v);
  if (m) return resolveVar(m[1], scopes, at, trail);
  m = /^hsl\(\s*(.+)\s*\)$/.exec(v);
  if (m) {
    let inner = m[1];
    let alpha = 1;
    const slash = inner.lastIndexOf(" / ");
    if (slash >= 0) {
      const a = inner.slice(slash + 3).trim();
      alpha = a.endsWith("%") ? parseFloat(a) / 100 : parseFloat(a);
      inner = inner.slice(0, slash).trim();
      if (!Number.isFinite(alpha)) return { type: "other" };
    }
    const ref = /^var\(\s*(--[\w-]+)\s*\)$/.exec(inner);
    if (ref) {
      const base = resolveVar(ref[1], scopes, at, trail);
      return base.type === "color" ? { ...base, a: base.a * alpha } : { type: "other" };
    }
    const t = parseTriplet(inner);
    return t ? { type: "color", ...t, a: alpha } : { type: "other" };
  }
  const triplet = parseTriplet(v);
  if (triplet) return { type: "color", ...triplet, a: 1 };
  m = SHADOW_RE.exec(v);
  if (m) {
    const [, x, y, blur, spread, r, g, b, a] = m;
    return { type: "shadow", x: +x, y: +y, blur: +blur, spread: spread === undefined ? 0 : +spread, r: +r, g: +g, b: +b, a: +a };
  }
  return { type: "other" };
}

/**
 * 壁纸模式里 `.bg-X`（Tailwind 工具类）被强制改成半透明：
 *   .app-background-mode-image .bg-card { background-color: hsl(var(--card) / 0.22) !important }
 * 这类规则不是自定义属性，没法按令牌读，但它们决定了「面板底色在壁纸模式下有多透」，所以单独抽成 --fill-X。
 * 返回 {light: Map<X, alpha>, dark: Map<X, alpha>}（dark 是 `.dark .app-background-mode-image` 的更具体规则）。
 */
export function parseGlassFills(rules) {
  const out = { light: new Map(), dark: new Map() };
  for (const r of rules) {
    // 选择器可能是逗号列表（.bg-sidebar-accent 与 /40 /50 /80 变体共用一条规则），取第一项判定
    const m = /^(\.dark )?\.app-background-mode-image \.bg-([a-z-]+)(?:\\\/\d+)?(?:,|$)/.exec(r.selector);
    if (!m) continue;
    const decl = /background-color:\s*hsl\(\s*var\((--[\w-]+)\)\s*\/\s*([\d.]+)\s*\)\s*!important/.exec(r.body);
    if (!decl) continue;
    if (decl[1] !== `--${m[2]}`) throw new Error(`壁纸半透明规则 .bg-${m[2]} 引用了 ${decl[1]}，与类名不一致，生成器需要更新`);
    (m[1] ? out.dark : out.light).set(m[2], parseFloat(decl[2]));
  }
  return out;
}

/** CSS 全文 → 四种模式的令牌：Map<名, 值>。 */
export function buildModes(css) {
  const rules = parseRules(stripComments(css));
  const light = findBlock(rules, ":root", "--background");
  const dark = findBlock(rules, ".dark", "--background");
  const glassLight = findBlock(rules, ".app-background-mode-image", "--background");
  const glassDark = findBlock(rules, ".dark .app-background-mode-image", "--background");

  const merge = (...layers) => new Map(layers.flatMap((l) => [...l]));
  // html 级：浅色就是 :root；深色是 :root 被 .dark 覆盖（同一元素，.dark 在源码里更靠后）
  const htmlLight = light;
  const htmlDark = merge(light, dark);
  // 元素级（App 根 div）：先 .app-background-mode-image，深色时再被更具体的 .dark .app-background-mode-image 覆盖
  const elemLight = glassLight;
  const elemDark = merge(glassLight, glassDark);

  const evalAll = (own, ancestor) => {
    const scopes = [own, ancestor];
    const names = new Set([...ancestor.keys(), ...own.keys()]);
    const out = new Map();
    for (const name of names) if (!INPUT_VARS.has(name)) out.set(name, resolveVar(name, scopes, 0));
    return out;
  };
  const modes = {
    light: evalAll(htmlLight, new Map()),
    dark: evalAll(htmlDark, new Map()),
    glassLight: evalAll(elemLight, htmlLight),
    glassDark: evalAll(elemDark, htmlDark),
  };

  // --fill-X：`.bg-X` 在该模式下实际画的底色。非壁纸模式 = 令牌本色；壁纸模式 = 令牌本色 × 半透明规则里的 alpha。
  const fills = parseGlassFills(rules);
  const fillNames = new Set([...fills.light.keys(), ...fills.dark.keys()]);
  if (fillNames.size === 0) throw new Error("globals.css 里没有找到壁纸模式的 .bg-X 半透明规则，生成器需要更新");
  for (const x of [...fillNames].sort()) {
    for (const key of ["light", "dark"]) {
      modes[key].set(`--fill-${x}`, modes[key].get(`--${x}`));
    }
    const lightA = fills.light.get(x);
    const darkA = fills.dark.get(x) ?? lightA;
    modes.glassLight.set(`--fill-${x}`, withAlpha(modes.glassLight.get(`--${x}`), lightA));
    modes.glassDark.set(`--fill-${x}`, withAlpha(modes.glassDark.get(`--${x}`), darkA));
  }
  modes.inputs = { hue: light.get("--theme-hue"), saturation: light.get("--theme-saturation"), radius: light.get("--radius") };
  return modes;
}

function withAlpha(color, alpha) {
  if (!color || color.type !== "color") throw new Error("壁纸半透明规则引用的令牌不是颜色");
  return { ...color, a: color.a * alpha };
}

/** 令牌名 → Go 字段名：去掉 -- 与开头的 app-，按 - 驼峰。 */
export function fieldName(cssName) {
  return cssName
    .replace(/^--(app-)?/, "")
    .split("-")
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join("");
}

function goNum(n) {
  return Number.isInteger(n) ? String(n) : String(+n.toFixed(4));
}

function goExpr({ c, hue, sat }) {
  const parts = [];
  if (hue) parts.push(hue === 1 ? "hue" : `hue*${goNum(hue)}`);
  if (sat) parts.push(sat === 1 ? "sat" : `sat*${goNum(sat)}`);
  if (c || parts.length === 0) parts.push(goNum(c));
  return parts.join("+");
}

function goValue(v, kind, T) {
  if (kind === "color") {
    if (!v || v.type !== "color") return T.transparent; // none / 渐变等：该模式没有可画的颜色
    return `col(${goExpr(v.h)}, ${goExpr(v.s)}, ${goExpr(v.l)}, ${goNum(v.a)})`;
  }
  if (!v || v.type !== "shadow") return `${T.shadow}{}`;
  return `${T.shadow}{X: ${goNum(v.x)}, Y: ${goNum(v.y)}, Blur: ${goNum(v.blur)}, Spread: ${goNum(v.spread)}, Color: ${T.rgba}(${v.r}, ${v.g}, ${v.b}, ${goNum(v.a)})}`;
}

const MODE_KEYS = ["light", "dark", "glassLight", "glassDark"];

/**
 * 一个令牌的 Go 类型：名字含 shadow 的是阴影（取值只有阴影或 none）；其余在任一模式里算出颜色就是颜色；否则不发射。
 * 某个模式里该令牌是渐变等无对应原语的值时，在那个模式取透明/零值，并记入 coerced 供 --verbose 列出。
 * 这样做对「高光」类令牌是精确的（它们在非壁纸模式里是 transparent→transparent 的渐变，肉眼不可见），
 * 对其他令牌是需要人确认的近似，所以必须可见。
 */
function classify(name, modes, coerced) {
  const types = MODE_KEYS.map((k) => modes[k].get(name)?.type).filter((t) => t && t !== "missing");
  const set = new Set(types);
  if (set.has("color") && set.has("shadow")) throw new Error(`${name} 在不同模式里既是颜色又是阴影，生成器需要更新`);
  let kind = null;
  if (/shadow/.test(name)) {
    if (set.has("color")) throw new Error(`${name} 名字含 shadow，却算出了颜色，生成器需要更新`);
    kind = set.has("shadow") || set.has("none") ? "shadow" : null;
  } else if (set.has("color")) kind = "color";
  if (kind && set.has("other")) {
    const bad = MODE_KEYS.filter((k) => modes[k].get(name)?.type === "other");
    coerced.push(`${name}（${bad.join("、")}）`);
  }
  return kind;
}

/**
 * 动效时长：:root 里的 --motion-duration-*（标准档）与 html[data-motion="reduced"] 里的覆盖值（减弱档）。
 * 返回 {names: [...], standard: [ms...], reduced: [ms...]}，顺序与 CSS 里 :root 的声明顺序一致。
 */
export function parseMotion(css) {
  const rules = parseRules(stripComments(css));
  const std = findBlock(rules, ":root", "--motion-duration-instant");
  const reduced = findBlock(rules, 'html[data-motion="reduced"]', "--motion-duration-instant");
  const names = [...std.keys()].filter((k) => k.startsWith("--motion-duration-")).map((k) => k.slice("--motion-duration-".length));
  const ms = (decls, name) => {
    const m = /^(\d+)ms$/.exec(decls.get(`--motion-duration-${name}`) ?? "");
    if (!m) throw new Error(`--motion-duration-${name} 不是 ms 值`);
    return +m[1];
  };
  return { names, standard: names.map((n) => ms(std, n)), reduced: names.map((n) => ms(reduced, n)) };
}

/** 从 TS 源里取预设数组。 */
export function parsePresets(appearanceSrc) {
  const themes = [...appearanceSrc.matchAll(/\{\s*id:\s*"([^"]+)",\s*hue:\s*(\d+),\s*saturation:\s*(\d+),\s*name:\s*"([^"]+)"\s*\}/g)].map((m) => ({
    id: m[1],
    hue: +m[2],
    saturation: +m[3],
    name: m[4],
  }));
  const fonts = [...appearanceSrc.matchAll(/\{\s*id:\s*"([^"]+)",\s*value:\s*(\d+),\s*name:\s*"([^"]+)"\s*\}/g)].map((m) => ({
    id: m[1],
    px: +m[2],
    name: m[3],
  }));
  if (themes.length === 0) throw new Error("settings-appearance.ts 里找不到 MORANDI_THEMES");
  if (fonts.length === 0) throw new Error("settings-appearance.ts 里找不到 FONT_SIZES");
  return { themes, fonts };
}

export function parseDefaults(defaultsSrc) {
  const get = (key) => {
    const m = new RegExp(`${key}:\\s*"([^"]+)"`).exec(defaultsSrc);
    if (!m) throw new Error(`settings-defaults.ts 里找不到 ${key}`);
    return m[1];
  };
  const num = (key) => {
    const m = new RegExp(`${key}:\\s*(\\d+)`).exec(defaultsSrc);
    if (!m) throw new Error(`settings-defaults.ts 里找不到 ${key}`);
    return +m[1];
  };
  return { themeColor: get("themeColor"), fontSize: get("fontSize"), hue: num("themeHue"), saturation: num("themeSaturation") };
}

export function render(target = "mygo") {
  const T = TARGETS[target];
  if (!T) throw new Error(`未知输出目标 ${target}`);
  const modes = buildModes(fs.readFileSync(CSS_FILE, "utf8"));
  const { themes, fonts } = parsePresets(fs.readFileSync(APPEARANCE_FILE, "utf8"));
  const defaults = parseDefaults(fs.readFileSync(DEFAULTS_FILE, "utf8"));
  if (!themes.some((t) => t.id === defaults.themeColor)) throw new Error(`默认主题色 ${defaults.themeColor} 不在预设里`);
  if (!fonts.some((f) => f.id === defaults.fontSize)) throw new Error(`默认字号档 ${defaults.fontSize} 不在 FONT_SIZES 里`);
  const radiusRem = (() => {
    const m = /^(\d+(?:\.\d+)?)rem$/.exec(modes.inputs.radius ?? "");
    if (!m) throw new Error("globals.css 里的 --radius 不是 rem 值");
    return parseFloat(m[1]);
  })();

  // 字段顺序：按各模式里首次出现的顺序（light → dark → glassLight → glassDark）
  const order = [];
  const seen = new Set();
  const skipped = [];
  const coerced = [];
  for (const key of MODE_KEYS) {
    for (const name of modes[key].keys()) {
      if (seen.has(name)) continue;
      seen.add(name);
      const kind = classify(name, modes, coerced);
      if (kind) order.push({ name, kind, field: fieldName(name) });
      else skipped.push(name);
    }
  }
  const fields = new Map();
  for (const f of order) {
    if (fields.has(f.field)) throw new Error(`字段名碰撞：${f.name} 与 ${fields.get(f.field).name} 都转成 ${f.field}`);
    fields.set(f.field, f);
  }

  const lines = [];
  lines.push("// Code generated by apps/desktop-go/scripts/gen-native-theme.mjs from the renderer's globals.css and settings; DO NOT EDIT.");
  lines.push("// 由 gen-native-theme.mjs 生成：令牌来自渲染层 globals.css 与外观设置，改源后重新生成，不要手改。");
  lines.push("");
  lines.push(`package ${T.pkg}`);
  lines.push("");
  if (T.imports) {
    lines.push(T.imports);
    lines.push("");
  }
  lines.push("// Tokens 是渲染层的语义令牌（CSS 自定义属性）在原生界面里的原值；字段名是令牌名去掉 -- 与 app- 前缀后的驼峰。");
  lines.push("type Tokens struct {");
  for (const f of order) lines.push(`\t${f.field} ${f.kind === "color" ? T.color : T.shadow} // ${f.name}`);
  lines.push("}");
  lines.push("");
  for (const [key, doc] of [
    ["light", "浅色（:root）"],
    ["dark", "深色（:root ← .dark）"],
    ["glassLight", "壁纸模式、浅色（.app-background-mode-image）"],
    ["glassDark", "壁纸模式、深色（.dark .app-background-mode-image）"],
  ]) {
    const fn = T.fn[key];
    lines.push(`// ${fn} 返回${doc}下的令牌；hue、sat 是设置里的主题色相与饱和度。`);
    lines.push(`func ${fn}(hue, sat float64) Tokens {`);
    lines.push("\treturn Tokens{");
    for (const f of order) lines.push(`\t\t${f.field}: ${goValue(modes[key].get(f.name), f.kind, T)},`);
    lines.push("\t}");
    lines.push("}");
    lines.push("");
  }
  const motion = parseMotion(fs.readFileSync(CSS_FILE, "utf8"));
  const motionNames = ["instant", "quick", "base", "smooth", "slow"];
  if (motion.names.join() !== motionNames.join()) {
    throw new Error(`动效时长档位变了（${motion.names.join(",")}），${T.motionDoc} 需要同步`);
  }
  lines.push("// 动效时长（毫秒），顺序与 DurationName 一致：instant、quick、base、smooth、slow；");
  lines.push("// 标准档是 :root 的 --motion-duration-*，减弱档是 html[data-motion=\"reduced\"] 的覆盖值。");
  lines.push(`var ${T.motion.standard} = [...]int{${motion.standard.join(", ")}}`);
  lines.push(`var ${T.motion.reduced} = [...]int{${motion.reduced.join(", ")}}`);
  lines.push("");
  lines.push("// HuePresets 是设置里的 6 个主题色预设（MORANDI_THEMES）。");
  lines.push("var HuePresets = []HuePreset{");
  for (const t of themes) lines.push(`\t{ID: ${JSON.stringify(t.id)}, Name: ${JSON.stringify(t.name)}, Hue: ${t.hue}, Saturation: ${t.saturation}},`);
  lines.push("}");
  lines.push("");
  lines.push("// FontSizes 是设置里的字号档（FONT_SIZES）；Px 是该档的根字号。");
  lines.push("var FontSizes = []FontSizePreset{");
  for (const f of fonts) lines.push(`\t{ID: ${JSON.stringify(f.id)}, Name: ${JSON.stringify(f.name)}, Px: ${f.px}},`);
  lines.push("}");
  lines.push("");
  lines.push("const (");
  lines.push(`\t// DefaultHuePreset 与 DefaultFontSize 是设置的默认值。`);
  lines.push(`\tDefaultHuePreset = ${JSON.stringify(defaults.themeColor)}`);
  lines.push(`\tDefaultFontSize  = ${JSON.stringify(defaults.fontSize)}`);
  lines.push(`\t// RadiusRem 是 --radius（rem）；控件圆角在此基础上按 Tailwind 的 md/lg 档取值。`);
  lines.push(`\tRadiusRem = ${goNum(radiusRem)}`);
  lines.push(")");
  lines.push("");
  return { text: gofmt(lines.join("\n")), counts: { emitted: order.length, skipped: skipped.length, coerced: coerced.length }, skipped, coerced };
}

/** 产物必须已是 gofmt 格式。 */
function gofmt(src) {
  return execFileSync("gofmt", [], { input: src, encoding: "utf8", maxBuffer: 32 * 1024 * 1024 });
}

export function main(argv) {
  let status = 0;
  for (const [target, T] of Object.entries(TARGETS)) {
    const { text, counts, skipped, coerced } = render(target);
    // Windows 检出（core.autocrlf=true）的工作区文件是 CRLF，比较前统一成 LF，否则 --check 永远报过期
    const prev = fs.existsSync(T.out) ? fs.readFileSync(T.out, "utf8").replace(/\r\n/g, "\n") : null;
    const summary = `令牌 ${counts.emitted} 个（不发射 ${counts.skipped} 个渐变/滤镜类；${counts.coerced} 个在个别模式取透明）`;
    if (argv.includes("--check")) {
      if (prev === text) {
        console.log(`主题令牌与渲染层一致（${target}）：${summary}`);
      } else {
        console.error(`过期：${path.relative(REPO_ROOT, T.out)}（请运行 node apps/desktop-go/scripts/gen-native-theme.mjs 并提交）`);
        status = 1;
      }
      continue;
    }
    fs.mkdirSync(path.dirname(T.out), { recursive: true });
    if (prev !== text) fs.writeFileSync(T.out, text);
    console.log(`${prev === text ? "未变" : "写入"} ${path.relative(REPO_ROOT, T.out)}：${summary}`);
    if (argv.includes("--verbose")) {
      console.log(`不发射：${skipped.join(", ")}`);
      console.log(`个别模式取透明：${coerced.join("；")}`);
    }
  }
  return status;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
