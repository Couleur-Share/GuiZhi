// 从渲染层生成 Viva 原生界面的来源平台图标与平台表：品牌 logo 编译成 gfx.Icon 的 Go 值，平台名称与着色查表。
// Generate the source-platform icons and table of the Viva UI from the renderer sources.
//
//   node apps/desktop-go/scripts/gen-viva-brand-icons.mjs           # 重写 viva/icons/brand_gen.go 与 viva/kit/brand_gen.go
//   node apps/desktop-go/scripts/gen-viva-brand-icons.mjs --check   # 只比较：不一致就非零退出
//
// 输入（渲染层，切换前仍是事实来源）：
//   components/ui/PlatformLogos.tsx       各平台 logo（解析规则复用 gen-native-brand-icons.mjs：只允许 <svg> + <path>）
//   components/library/platform-meta.tsx  平台 → 名称键、图标、着色 class
// 输出：
//   viva/icons/brand_gen.go   品牌 logo（gfx.Icon；由 Viva 自带的 iconcompile 编译，运行期没有 SVG 解析）
//   viva/kit/brand_gen.go     平台表（名称经 locales 取，随界面语言；着色规则见 kit/brand.go）
//
// 单色 logo（根 fill 是 currentColor）按「填充、用调用方给的颜色着色」编译（根上补 stroke-width="0"）；
// 自带多色的 logo（2Libra）保留各路径的颜色。模型厂商图标属于 AI 设置模块（M3），不在这里。
// 解析器目前借用 MyGo 版的生成器（native/kit 退役时把解析函数并进本文件）。

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { lucideCanonical } from "./gen-native-icons.mjs";
import { parsePlatformLogos, parsePlatformMeta } from "./gen-native-brand-icons.mjs";

const REPO_ROOT = path.resolve(import.meta.dirname, "../../..");
const GO_ROOT = path.join(REPO_ROOT, "apps/desktop-go");
const RENDERER = path.join(REPO_ROOT, "apps/desktop/src/renderer");
const SRC = {
  logos: path.join(RENDERER, "components/ui/PlatformLogos.tsx"),
  meta: path.join(RENDERER, "components/library/platform-meta.tsx"),
};
const OUT_ICONS = path.join(GO_ROOT, "viva/icons/brand_gen.go");
const OUT_KIT = path.join(GO_ROOT, "viva/kit/brand_gen.go");

const LICENSE = [
  "Platform logos: paths from Simple Icons v16 (CC0-1.0); the NGA and Appinn marks are drawn in-house.",
  "Trademarks remain the property of their owners and are used only to identify the source of an item.",
  "Compiled from components/ui/PlatformLogos.tsx of the renderer.",
].join("\n");

function readText(file) {
  return fs.readFileSync(file, "utf8").replace(/^\uFEFF/, "").replace(/\r\n/g, "\n");
}

const goStr = (s) => JSON.stringify(s);
/** i18n 键 library.platformDouyin → locales 的访问器 LibraryPlatformDouyin。 */
const accessor = (key) => key.split(".").map((p) => p[0].toUpperCase() + p.slice(1)).join("");
const pascal = (s) => s.split(/[-_]/).map((p) => p[0].toUpperCase() + p.slice(1)).join("");

/** "#RRGGBB" → tokens.RGBA(r, g, b, 1)。 */
function goHex(hex) {
  const n = parseInt(hex.slice(1), 16);
  return `tokens.RGBA(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, 1)`;
}

function goBrandColor(c) {
  switch (c.kind) {
    case "foreground":
      return "brandColor{kind: colorForeground}";
    case "current":
      return "brandColor{kind: colorCurrent}";
    case "fixed":
      return `brandColor{kind: colorFixed, light: ${goHex(c.hex)}}`;
    case "pair":
      return `brandColor{kind: colorPair, light: ${goHex(c.light)}, dark: ${goHex(c.dark)}}`;
    default:
      throw new Error(`未知着色 ${c.kind}`);
  }
}

/** 产物必须已是 gofmt 格式（CI 有 gofmt 检查，常量块的列对齐靠它）。 */
function gofmt(src) {
  return execFileSync("gofmt", [], { input: src, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
}

/** 把各 logo 的 SVG 写进临时目录，交给 viva/icons/gen 编译，返回生成的 Go 源码。 */
function compileLogos(logos) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-viva-brand-"));
  try {
    for (const l of logos) {
      // 单色 logo：根上补 stroke-width="0"，iconcompile 据此把着色的形状当填充而不是描边
      const svg = l.mono ? l.svg.replace("<svg ", '<svg stroke-width="0" ') : l.svg;
      fs.writeFileSync(path.join(dir, `brand-${l.key}.svg`), svg);
    }
    return execFileSync("go", ["run", "./viva/icons/gen", "-pkg", "icons", "-license", LICENSE, dir], {
      cwd: GO_ROOT,
      encoding: "utf8",
      maxBuffer: 64 * 1024 * 1024,
    });
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

/** 汇总输入并做交叉校验，返回两个产物的文本。 */
export function render() {
  const logos = parsePlatformLogos(readText(SRC.logos));
  const meta = parsePlatformMeta(readText(SRC.meta));
  const logoByFn = new Map(logos.map((l) => [l.fn, l]));

  const lucideNames = new Set();
  const classify = (icon, where) => {
    if (logoByFn.has(icon)) return { brand: logoByFn.get(icon) };
    if (/Icon$/.test(icon)) {
      lucideNames.add(icon);
      return { lucide: icon };
    }
    throw new Error(`${where}：无法识别的图标 ${icon}（既不是 PlatformLogos 里的 Logo，也不是 lucide 的 XxxIcon）`);
  };
  const platforms = meta.platforms.map((p) => ({ ...p, ...classify(p.icon, `platform-meta.tsx 的 ${p.id}`) }));
  const unknown = { ...meta.unknown, ...classify(meta.unknown.icon, "UNKNOWN_PLATFORM_META") };
  const canonical = lucideCanonical([...lucideNames]);
  for (const [n, c] of canonical) if (!c) throw new Error(`lucide-react 里没有 ${n}`);

  const entry = (p) => {
    const icon = p.brand ? `icons.Brand${pascal(p.brand.key)}` : `icons.${canonical.get(p.lucide)}`;
    return `{label: locales.${accessor(p.labelKey)}, icon: ${icon}, color: ${goBrandColor(p.color)}}`;
  };
  const kit = [
    "// Code generated by apps/desktop-go/scripts/gen-viva-brand-icons.mjs; DO NOT EDIT.",
    "",
    "package kit",
    "",
    "import (",
    '\t"guizhi-mygo-shell/viva/icons"',
    '\t"guizhi-mygo-shell/viva/locales"',
    '\t"guizhi-mygo-shell/viva/tokens"',
    ")",
    "",
    "// platformTable 对应渲染层的 SOURCE_PLATFORM_META（侧栏「平台」分区、表格「来源」列、任务详情的来源）。",
    "var platformTable = map[string]platformMeta{",
    ...platforms.map((p) => `\t${goStr(p.id)}: ${entry(p)},`),
    "}",
    "",
    "// unknownPlatform 对应 UNKNOWN_PLATFORM_META：数据库里的平台值本版本不认识时用它。",
    `var unknownPlatform = platformMeta${entry(unknown)}`,
    "",
  ];

  return {
    icons: compileLogos(logos),
    kit: gofmt(kit.join("\n")),
    counts: { logos: logos.length, platforms: platforms.length },
  };
}

export function main(argv) {
  const { icons, kit, counts } = render();
  const outputs = [
    [OUT_ICONS, icons],
    [OUT_KIT, kit],
  ];
  const summary = `平台 logo ${counts.logos}，平台 ${counts.platforms}`;
  // Windows 检出（core.autocrlf=true）的工作区文件是 CRLF，比较前统一成 LF，否则 --check 永远报过期
  const read = (f) => (fs.existsSync(f) ? fs.readFileSync(f, "utf8").replace(/\r\n/g, "\n") : null);
  if (argv.includes("--check")) {
    const stale = outputs.filter(([f, text]) => read(f) !== text);
    for (const [f] of stale) console.error(`过期：${path.relative(REPO_ROOT, f)}（请运行 node apps/desktop-go/scripts/gen-viva-brand-icons.mjs 并提交）`);
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
