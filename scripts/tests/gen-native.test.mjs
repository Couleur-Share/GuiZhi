// 原生界面生成器（apps/desktop-go/scripts/gen-native-*.mjs）的单测：解析函数的边界，以及它们对真实渲染层源码的冒烟。
// 不调用 gofmt、不写文件；产物是否与源一致由 CI 的「重新生成后 git diff」把关。
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";

const ROOT = path.resolve(import.meta.dirname, "../..");
const scripts = (name) => import(pathToFileURL(path.join(ROOT, "apps/desktop-go/scripts", name)).href);
const read = async (rel) => (await import("node:fs")).readFileSync(path.join(ROOT, rel), "utf8").replace(/\r\n/g, "\n");

// ───────────────────────── 主题令牌 ─────────────────────────

test("theme: stripComments 去掉块注释", async () => {
  const { stripComments } = await scripts("gen-native-theme.mjs");
  assert.equal(stripComments("a/* x */b/*\ny*/c"), "abc");
});

test("theme: parseRules 进入 @layer、跳过其他 at-rule、规整选择器空白", async () => {
  const { parseRules } = await scripts("gen-native-theme.mjs");
  const rules = parseRules(`
    @layer base { :root { --a: 1; } .dark   .x { --b: 2; } }
    @media (min-width: 1px) { .skip { --c: 3; } }
    @keyframes k { from { --d: 4; } }
    .top { --e: 5; }
  `);
  assert.deepEqual(
    rules.map((r) => r.selector),
    [":root", ".dark .x", ".top"],
  );
});

test("theme: parseDecls 只取自定义属性，括号里的分号不分隔", async () => {
  const { parseDecls } = await scripts("gen-native-theme.mjs");
  const d = parseDecls("color: red; --a: hsl(1 2% 3%); --u: url(data:image/png;base64,xx); --s:  0 1px  2px rgba(0,0,0,.1)");
  assert.deepEqual([...d.keys()], ["--a", "--u", "--s"]);
  assert.equal(d.get("--u"), "url(data:image/png;base64,xx)");
  assert.equal(d.get("--s"), "0 1px 2px rgba(0,0,0,.1)");
});

test("theme: findBlock 找不到时抛出带选择器的错误", async () => {
  const { findBlock, parseRules } = await scripts("gen-native-theme.mjs");
  assert.throws(() => findBlock(parseRules(".a { --x: 1; }"), ".b", "--x"), /找不到声明了 --x 的 \.b 块/);
});

test("theme: resolveVar 从声明它的那一层继续查 var()，并识别循环", async () => {
  const { resolveVar, parseDecls } = await scripts("gen-native-theme.mjs");
  const html = parseDecls("--base: 10 20% 30%; --card: var(--base)");
  const elem = parseDecls("--card: 0 0% 100%; --panel: var(--card); --tint: hsl(var(--base) / 0.5)");
  // 元素级的 --panel 引用元素级的 --card；元素里没有的 --base 回落到 html
  const panel = resolveVar("--panel", [elem, html]);
  assert.equal(panel.type, "color");
  assert.equal(panel.l.c, 100);
  const tint = resolveVar("--tint", [elem, html]);
  assert.equal(tint.a, 0.5);
  assert.equal(tint.l.c, 30);
  // html 里的 --card 仍然指向 --base，不被元素级的重写影响
  assert.equal(resolveVar("--card", [html]).l.c, 30);
  assert.throws(() => resolveVar("--a", [parseDecls("--a: var(--b); --b: var(--a)")]), /循环引用/);
  assert.deepEqual(resolveVar("--nope", [html]), { type: "missing", name: "--nope" });
});

test("theme: evalValue 认得 none、transparent、阴影，其余归为 other", async () => {
  const { evalValue } = await scripts("gen-native-theme.mjs");
  assert.equal(evalValue("none", [], 0).type, "none");
  assert.equal(evalValue("transparent", [], 0).a, 0);
  const s = evalValue("0 4px 12px -2px rgba(1, 2, 3, 0.25)", [], 0);
  assert.deepEqual([s.type, s.x, s.y, s.blur, s.spread, s.r, s.a], ["shadow", 0, 4, 12, -2, 1, 0.25]);
  assert.equal(evalValue("linear-gradient(red, blue)", [], 0).type, "other");
});

test("theme: parseGlassFills 读壁纸模式的半透明规则，类名与变量对不上就报错", async () => {
  const { parseGlassFills } = await scripts("gen-native-theme.mjs");
  const fills = parseGlassFills([
    { selector: ".app-background-mode-image .bg-card", body: "background-color: hsl(var(--card) / 0.22) !important" },
    { selector: ".dark .app-background-mode-image .bg-card", body: "background-color: hsl(var(--card) / 0.3) !important" },
    { selector: ".other .bg-card", body: "background-color: hsl(var(--card) / 0.9) !important" },
  ]);
  assert.equal(fills.light.get("card"), 0.22);
  assert.equal(fills.dark.get("card"), 0.3);
  assert.throws(
    () => parseGlassFills([{ selector: ".app-background-mode-image .bg-card", body: "background-color: hsl(var(--muted) / 0.2) !important" }]),
    /引用了 --muted/,
  );
});

test("theme: fieldName 去掉 -- 与 app- 前缀并按驼峰", async () => {
  const { fieldName } = await scripts("gen-native-theme.mjs");
  assert.equal(fieldName("--sidebar-accent-foreground"), "SidebarAccentForeground");
  assert.equal(fieldName("--app-ui-surface-bg"), "UiSurfaceBg");
});

test("theme: 真实 globals.css 的四种模式算得出，壁纸模式的底色全是半透明", async () => {
  const { buildModes, parseMotion, parsePresets, parseDefaults } = await scripts("gen-native-theme.mjs");
  const css = await read("apps/desktop/src/renderer/styles/globals.css");
  const modes = buildModes(css);
  const fillNames = [...modes.light.keys()].filter((k) => k.startsWith("--fill-"));
  assert.ok(fillNames.length >= 5, `--fill-* 太少：${fillNames.length}`);
  for (const name of fillNames) {
    for (const key of ["light", "dark"]) assert.equal(modes[key].get(name).type, "color", `${key} ${name}`);
    for (const key of ["glassLight", "glassDark"]) assert.ok(modes[key].get(name).a < 1, `${key} ${name} 在壁纸模式应当半透明`);
    assert.equal(modes.light.get(name).a, modes.light.get(name.replace("--fill-", "--")).a, `${name} 在非壁纸模式取令牌本色`);
  }
  const motion = parseMotion(css);
  assert.equal(motion.standard.length, motion.reduced.length);
  assert.ok(motion.standard.every((ms, i) => motion.reduced[i] <= ms), "减弱档的时长不应比标准档长");

  const appearance = await read("apps/desktop/src/renderer/stores/settings/settings-appearance.ts");
  const defaults = parseDefaults(await read("apps/desktop/src/renderer/stores/settings/settings-defaults.ts"));
  const { themes, fonts } = parsePresets(appearance);
  assert.ok(themes.some((t) => t.id === defaults.themeColor), "默认主题色必须在预设里");
  assert.ok(fonts.some((f) => f.id === defaults.fontSize), "默认字号档必须在预设里");
});

// ───────────────────────── i18n 目录 ─────────────────────────

test("i18n: toIcu 转换插值、字面花括号与撇号", async () => {
  const { toIcu } = await scripts("gen-native-i18n.mjs");
  assert.deepEqual(toIcu("共 {{count}} 条，进行中 {{ active }}"), { message: "共 {count} 条，进行中 {active}", params: ["count", "active"] });
  assert.deepEqual(toIcu("重复 {{n}} 与 {{n}}"), { message: "重复 {n} 与 {n}", params: ["n"] });
  assert.equal(toIcu("JSON 用 { 和 } 包起来").message, "JSON 用 '{' 和 '}' 包起来");
  assert.equal(toIcu("don't").message, "don't");
  assert.equal(toIcu("it's {{x}}").message, "it's {x}");
  // ICU 里只有紧贴在花括号之前的撇号才会开始一段引用：占位符前要写成两个，占位符后的撇号照旧
  assert.equal(toIcu("'{{x}}'").message, "''{x}'");
  assert.equal(toIcu("{{x}}'s").message, "{x}'s");
  assert.throws(() => toIcu("{{count, plural, one {a} other {b}}}"), /无法解析的插值/);
});

test("i18n: flatten 与 arbKey", async () => {
  const { flatten, arbKey } = await scripts("gen-native-i18n.mjs");
  assert.deepEqual(flatten({ a: { b: "x", c: { d: "y" } }, e: "z" }), [
    [["a", "b"], "x"],
    [["a", "c", "d"], "y"],
    [["e"], "z"],
  ]);
  assert.throws(() => flatten({ a: ["x"] }), /不支持的目录值类型/);
  assert.equal(arbKey(["library", "platform-x", "y"]), "library_platform_x_y");
});

test("i18n: 真实目录里中英键与占位符对等，没有转换问题", async () => {
  const { buildCatalogs } = await scripts("gen-native-i18n.mjs");
  const { problems, counts } = buildCatalogs();
  assert.deepEqual(problems, []);
  const sizes = Object.values(counts);
  assert.ok(sizes.length >= 2 && sizes.every((n) => n === sizes[0] && n > 500), `各语言消息数应一致且不少：${JSON.stringify(counts)}`);
});

// ───────────────────────── lucide 图标 ─────────────────────────

test("icons: usedLucideNames 排序去重且数量合理，lucideCanonical 解析别名", async () => {
  const { usedLucideNames, lucideCanonical } = await scripts("gen-native-icons.mjs");
  const names = usedLucideNames();
  assert.ok(names.length > 100, `渲染层用到的 lucide 图标只找到 ${names.length} 个`);
  assert.deepEqual(names, [...new Set(names)].sort());
  const canon = lucideCanonical(["GlobeIcon", "Globe", "NoSuchIconX"]);
  assert.equal(canon.get("GlobeIcon"), "Globe");
  assert.equal(canon.get("Globe"), "Globe");
  assert.equal(canon.get("NoSuchIconX"), undefined);
});

// ───────────────────────── 品牌图标 ─────────────────────────

test("brand: stripComments 去注释但不误伤 http:// 与块注释", async () => {
  const { stripComments } = await scripts("gen-native-brand-icons.mjs");
  const out = stripComments('/** doc */\nxmlns="http://www.w3.org/2000/svg" // tail\n  // whole line\nx: 1,');
  assert.ok(out.includes('xmlns="http://www.w3.org/2000/svg"'));
  assert.ok(!out.includes("tail") && !out.includes("whole") && !out.includes("doc"));
});

test("brand: parseJsxAttrs 只收字符串与 className={className}", async () => {
  const { parseJsxAttrs } = await scripts("gen-native-brand-icons.mjs");
  assert.deepEqual(parseJsxAttrs('className={className} viewBox="0 0 24 24" aria-hidden="true"', "t"), {
    className: null,
    viewBox: "0 0 24 24",
    "aria-hidden": "true",
  });
  assert.throws(() => parseJsxAttrs("onClick={go}", "t"), /不支持的属性表达式/);
  assert.throws(() => parseJsxAttrs('fill="a" fill="b"', "t"), /重复/);
  assert.throws(() => parseJsxAttrs('fill="a" disabled', "t"), /无法识别的属性文本/);
});

const LOGO = (inner, svgAttrs = 'className={className} viewBox="-2 -2 28 28" fill="currentColor" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"') =>
  `export function DemoLogo({ className }: PlatformLogoProps) {\n  return (\n    <svg ${svgAttrs}>\n      ${inner}\n    </svg>\n  );\n}\n`;

test("brand: parsePlatformLogos 生成独立 SVG，JSX 驼峰属性转成 SVG 连字符", async () => {
  const { parsePlatformLogos } = await scripts("gen-native-brand-icons.mjs");
  const [logo] = parsePlatformLogos(LOGO('<path fillRule="evenodd" clipRule="evenodd" d="M0 0h1v1z" />'));
  assert.equal(logo.key, "demo");
  assert.equal(logo.mono, true);
  assert.equal(
    logo.svg,
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="-2 -2 28 28" fill="currentColor"><path fill-rule="evenodd" clip-rule="evenodd" d="M0 0h1v1z"/></svg>',
  );
  const [multi] = parsePlatformLogos(LOGO('<path d="M0 0z" fill="#21AA6C" />', 'className={className} viewBox="0 0 192 192" fill="none" xmlns="http://www.w3.org/2000/svg"'));
  assert.equal(multi.mono, false, "根 fill 不是 currentColor 的 logo 按自带颜色画");
});

test("brand: parsePlatformLogos 遇到不认识的结构就抛错，不悄悄漏掉", async () => {
  const { parsePlatformLogos } = await scripts("gen-native-brand-icons.mjs");
  assert.throws(() => parsePlatformLogos(LOGO('<g><path d="M0 0z" /></g>')), /除 <path> 外还有别的内容/);
  assert.throws(() => parsePlatformLogos(LOGO('<path d="M0 0z" stroke="red" />')), /不支持的属性 stroke/);
  assert.throws(() => parsePlatformLogos(LOGO('<path d="M0 0z" />', 'viewBox="0 0 24 24" xmlns="x" onClick="y" fill="currentColor"')), /不支持的属性 onClick/);
  assert.throws(() => parsePlatformLogos(LOGO('<path d="M0 0z" />', 'fill="currentColor"')), /viewBox/);
  assert.throws(() => parsePlatformLogos(LOGO('<path d="M0 0z" />') + "export function OtherLogo(props: any) { return null; }"), /写法变了/);
});

test("brand: parseColorClass 解析四类着色，色阶不在表里就报错", async () => {
  const { parseColorClass } = await scripts("gen-native-brand-icons.mjs");
  assert.deepEqual(parseColorClass("text-foreground", "t"), { kind: "foreground" });
  assert.deepEqual(parseColorClass("text-current", "t"), { kind: "current" });
  assert.deepEqual(parseColorClass("text-[#00A1D6]", "t"), { kind: "fixed", hex: "#00A1D6" });
  assert.deepEqual(parseColorClass("text-green-600 dark:text-green-400", "t"), { kind: "pair", light: "#16a34a", dark: "#4ade80" });
  assert.throws(() => parseColorClass("text-red-600 dark:text-red-400", "t"), /TAILWIND/);
  assert.throws(() => parseColorClass("text-muted-foreground", "t"), /无法识别/);
});

test("brand: parseHexGradient 与 parseBorder", async () => {
  const { parseHexGradient, parseBorder } = await scripts("gen-native-brand-icons.mjs");
  assert.deepEqual(parseHexGradient("linear-gradient(135deg, #eef2ff 0%, #dbeafe 100%)", "t"), { angle: 135, from: { hex: "#eef2ff" }, to: { hex: "#dbeafe" } });
  assert.throws(() => parseHexGradient("linear-gradient(to right, red, blue)", "t"), /无法识别的渐变/);
  assert.deepEqual(parseBorder("1px solid #fde047", "t"), { hex: "#fde047" });
  assert.deepEqual(parseBorder("1px solid rgba(37,99,235,0.18)", "t"), { rgba: [37, 99, 235, 0.18] });
  assert.throws(() => parseBorder("2px dashed red", "t"), /无法识别的边框/);
});

const RASTER = (over = {}) => {
  const rect = over.rect ?? '<rect x="15" y="15" width="90" height="90" fill="url(#pattern0_1_2)"/>';
  const image = over.image ?? '<image id="i" width="320" height="320" xlink:href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUg=="/>';
  return `<svg width="120" height="120" viewBox="${over.viewBox ?? "0 0 120 120"}" fill="none" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">${rect}<defs>${image}</defs></svg>`;
};

test("brand: parseRasterSvg 取出内嵌 PNG 与居中位置，结构不符就拒绝", async () => {
  const { parseRasterSvg } = await scripts("gen-native-brand-icons.mjs");
  assert.deepEqual(parseRasterSvg("k", RASTER()), { png: "iVBORw0KGgoAAAANSUhEUg==", canvas: 120, x: 15, y: 15, w: 90, h: 90 });
  assert.throws(() => parseRasterSvg("k", RASTER({ rect: '<rect x="0" y="0" width="90" height="90" fill="url(#pattern0)"/>' })), /没有居中/);
  assert.throws(() => parseRasterSvg("k", RASTER({ image: '<image xlink:href="data:image/png;base64,AAAA"/>' })), /不是 PNG/);
  assert.throws(() => parseRasterSvg("k", RASTER({ image: "" })), /恰有一个内嵌 PNG/);
  assert.throws(() => parseRasterSvg("k", RASTER({ viewBox: "0 0 120 100" })), /正方形/);
});

test("brand: 真实渲染层源码能被生成器完整解析", async () => {
  const gen = await scripts("gen-native-brand-icons.mjs");
  const logos = gen.parsePlatformLogos(await read("apps/desktop/src/renderer/components/ui/PlatformLogos.tsx"));
  assert.ok(logos.length >= 9, `平台 logo 只解析出 ${logos.length} 个`);
  assert.ok(logos.every((l) => l.svg.startsWith('<svg xmlns="http://www.w3.org/2000/svg"') && !l.svg.includes("`")));
  assert.deepEqual(
    logos.filter((l) => !l.mono).map((l) => l.key),
    ["twolibra"],
    "目前只有 2Libra 带自己的颜色",
  );

  const meta = gen.parsePlatformMeta(await read("apps/desktop/src/renderer/components/library/platform-meta.tsx"));
  const ids = meta.platforms.map((p) => p.id);
  for (const id of ["douyin", "bilibili", "wechat", "web", "local"]) assert.ok(ids.includes(id), `平台表缺 ${id}`);
  assert.equal(meta.unknown.labelKey, "library.platformUnknown");
  const logoFns = new Set(logos.map((l) => l.fn));
  for (const p of meta.platforms) assert.ok(logoFns.has(p.icon) || /Icon$/.test(p.icon), `${p.id} 的图标 ${p.icon} 既不是 Logo 也不是 lucide`);

  const models = gen.parseModelIcons(await read("apps/desktop/src/renderer/components/ui/ModelIcons.tsx"));
  const byCategory = new Map(models.categories.map((c) => [c.category, c.provider]));
  assert.equal(byCategory.get("GPT"), "openai");
  assert.equal(byCategory.get("Azure OpenAI"), "azureai");
  assert.equal(byCategory.get("ERNIE"), null, "ERNIE 在渲染层是空占位，走字母兜底");
  assert.deepEqual(models.specials.map((s) => s.category).sort(), ["Custom", "Other"]);
  assert.equal(models.banana.glyph, "🍌");

  const providers = gen.loadProviders(path.join(ROOT, "apps/desktop/src/renderer/assets/providers"));
  assert.ok(providers.length >= 15);
  const keys = new Set(providers.map((p) => p.key));
  for (const [category, provider] of byCategory) if (provider) assert.ok(keys.has(provider), `${category} 指向不存在的 ${provider}`);
  const doubao = providers.find((p) => p.key === "doubao");
  assert.equal(doubao.raster.canvas, 120, "豆包是套了壳的位图，必须走位图通道");
  assert.ok(providers.filter((p) => p.svg).every((p) => !p.svg.includes("\r") && !p.svg.includes("`")));
});

// ───────────────────────── Tailwind 色阶对拍 ─────────────────────────
// 渲染层的 Toast、对话框、平台 logo 直接用了 Tailwind 的色阶（不是主题令牌）。
// 原生界面里抄下来的几处必须逐项对得上 tailwindcss/colors：抄错一位十六进制或升级后色值变了，这里先红。

const tailwindColors = () => createRequire(path.join(ROOT, "apps/desktop/package.json"))("tailwindcss/colors");

function tailwindHex(colors, name) {
  const m = name.match(/^([a-z]+)-(\d+)$/);
  assert.ok(m, `色阶名 ${name} 格式不对`);
  const hex = colors[m[1]]?.[m[2]];
  assert.ok(hex, `tailwindcss/colors 里没有 ${name}`);
  return hex.toLowerCase();
}

test("tailwind: native/kit/tailwind.go 的色阶与 tailwindcss/colors 逐项一致", async () => {
  const src = await read("apps/desktop-go/native/kit/tailwind.go");
  const block = src.match(/var tailwindPalette = map\[string\]string\{([\s\S]*?)\n\}/);
  assert.ok(block, "找不到 tailwindPalette 的定义，写法变了就同步改这条测试");
  const entries = [...block[1].matchAll(/"([a-z]+-\d+)":\s*"(#[0-9a-fA-F]{6})"/g)];
  assert.ok(entries.length >= 25, `只解析出 ${entries.length} 项，解析规则可能失效`);
  const colors = tailwindColors();
  for (const [, name, hex] of entries) assert.equal(hex.toLowerCase(), tailwindHex(colors, name), name);
});

test("tailwind: 品牌图标生成器的 TAILWIND 表与 tailwindcss/colors 一致", async () => {
  const { TAILWIND } = await scripts("gen-native-brand-icons.mjs");
  const colors = tailwindColors();
  for (const [name, hex] of Object.entries(TAILWIND)) assert.equal(hex.toLowerCase(), tailwindHex(colors, name), name);
});