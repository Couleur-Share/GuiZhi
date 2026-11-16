// 从渲染层实际用到的 lucide-react 图标生成 Go 侧的 SVG 清单（native/kit/icons/icons_gen.go）。
// Generate the embedded SVG table from the lucide-react icons the renderer actually imports.
//
//   node apps/desktop-go/scripts/gen-native-icons.mjs           # 重写 icons_gen.go
//   node apps/desktop-go/scripts/gen-native-icons.mjs --check   # 只比较：不一致就非零退出
//
// lucide-react 的位置：LUCIDE_REACT_DIR 环境变量，否则 apps/desktop/node_modules/lucide-react（需要 pnpm install）。
// 产物入库，所以普通的 go build / go test 不依赖 node_modules；渲染层新增图标后重新运行本脚本。
// MyGo 的 ui.Icon 直接吃 Lucide 的 SVG（描边色取文字色），不需要改写。
//
// 另有 Viva 目标：同一份 SVG 经 viva/icons/gen（Viva 自带的 iconcompile）编译成 gfx.Icon 的 Go 值，
// 写到 viva/icons/icons_gen.go（普通 Go，运行期没有 SVG 解析）。

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const REPO_ROOT = path.resolve(import.meta.dirname, "../../..");
const RENDERER = path.join(REPO_ROOT, "apps/desktop/src/renderer");
const GO_ROOT = path.join(REPO_ROOT, "apps/desktop-go");
const OUT_FILE = path.join(REPO_ROOT, "apps/desktop-go/native/kit/icons/icons_gen.go");
const OUT_FILE_VIVA = path.join(REPO_ROOT, "apps/desktop-go/viva/icons/icons_gen.go");

/** 只是类型、不是图标的导出。 */
const NOT_ICONS = new Set(["LucideIcon", "LucideProps", "Icon", "icons", "createLucideIcon", "IconNode"]);

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "node_modules") walk(full, out);
    } else if (/\.(ts|tsx)$/.test(entry.name)) out.push(full);
  }
  return out;
}

/** 渲染层所有 `import { A, B as C, type D } from "lucide-react"` 里的导出名（取 as 之前的名字）。 */
export function usedLucideNames() {
  const names = new Set();
  const re = /import\s*(?:type\s*)?\{([^}]*)\}\s*from\s*["']lucide-react["']/g;
  for (const file of walk(RENDERER)) {
    const text = fs.readFileSync(file, "utf8");
    for (const m of text.matchAll(re)) {
      for (const part of m[1].split(",")) {
        const item = part.trim();
        if (!item || item.startsWith("type ")) continue;
        names.add(item.split(/\s+as\s+/)[0].trim());
      }
    }
  }
  for (const n of NOT_ICONS) names.delete(n);
  return [...names].sort();
}

function lucideDir() {
  const dir = process.env.LUCIDE_REACT_DIR || path.join(REPO_ROOT, "apps/desktop/node_modules/lucide-react");
  if (!fs.existsSync(path.join(dir, "dist/esm/lucide-react.js"))) {
    throw new Error(`找不到 lucide-react（${dir}）：请先 pnpm install，或用 LUCIDE_REACT_DIR 指向它`);
  }
  return dir;
}

/** 别名 → 图标文件（解析 lucide-react.js 里的 export 行）。 */
function aliasMap(dir) {
  const text = fs.readFileSync(path.join(dir, "dist/esm/lucide-react.js"), "utf8");
  const map = new Map();
  for (const m of text.matchAll(/export \{([^}]+)\} from '\.\/icons\/([^']+)\.js';/g)) {
    for (const part of m[1].split(",")) {
      const alias = part.trim().split(/\s+as\s+/)[1];
      if (alias) map.set(alias, m[2]);
    }
  }
  return map;
}

/** 图标文件 → { canonical, nodes }；canonical 取文件名转帕斯卡（Search、CircleAlert）。 */
function readIcon(dir, file) {
  const text = fs.readFileSync(path.join(dir, "dist/esm/icons", `${file}.js`), "utf8");
  const m = text.match(/createLucideIcon\("([^"]+)",\s*(\[[\s\S]*?\])\);/);
  if (!m) throw new Error(`无法解析图标文件 ${file}.js`);
  const nodes = new Function(`return ${m[2]}`)();
  return { canonical: m[1], nodes };
}

/**
 * 渲染层的 lucide 导入名（GlobeIcon、Globe…）→ canonical 名（Globe，即 icons_gen.go 里的常量名）。
 * 找不到的名字对应 undefined；品牌图标生成器用它把 platform-meta / ModelIcons 里的通用图标接到同一张清单。
 */
export function lucideCanonical(importNames) {
  const dir = lucideDir();
  const aliases = aliasMap(dir);
  const out = new Map();
  for (const n of importNames) {
    const file = aliases.get(n);
    out.set(n, file ? readIcon(dir, file).canonical : undefined);
  }
  return out;
}

const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");

function toSvg(nodes) {
  const inner = nodes
    .map(([tag, attrs]) => {
      const a = Object.entries(attrs)
        .filter(([k]) => k !== "key")
        .map(([k, v]) => ` ${k}="${esc(v)}"`)
        .join("");
      return `<${tag}${a}/>`;
    })
    .join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${inner}</svg>`;
}

export function render() {
  const dir = lucideDir();
  const aliases = aliasMap(dir);
  const icons = new Map(); // canonical → svg
  const missing = [];
  for (const name of usedLucideNames()) {
    const file = aliases.get(name);
    if (!file) {
      missing.push(name);
      continue;
    }
    const { canonical, nodes } = readIcon(dir, file);
    icons.set(canonical, toSvg(nodes));
  }
  if (missing.length > 0) throw new Error(`lucide-react 里没有这些导出：${missing.join(", ")}`);
  const names = [...icons.keys()].sort();
  const lines = [];
  lines.push("// Code generated by apps/desktop-go/scripts/gen-native-icons.mjs; DO NOT EDIT.");
  lines.push("");
  lines.push("package icons");
  lines.push("");
  lines.push("// 渲染层用到的 lucide 图标（canonical 名）。新增图标：渲染层引入后重新运行生成脚本。");
  lines.push("const (");
  for (const n of names) lines.push(`\t${n} Name = "${n}"`);
  lines.push(")");
  lines.push("");
  lines.push("// sources 是各图标的 SVG 源；首次使用时才解析（见 Get）。");
  lines.push("var sources = map[Name]string{");
  for (const n of names) lines.push(`\t${n}: \`${icons.get(n)}\`,`);
  lines.push("}");
  lines.push("");
  return { text: gofmt(lines.join("\n")), count: names.length, icons };
}

/** Viva 目标：把各图标的 SVG 写进临时目录，交给 viva/icons/gen 编译，返回生成的 Go 源码。 */
export function renderViva(icons) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-viva-icons-"));
  try {
    for (const [name, svg] of icons) fs.writeFileSync(path.join(dir, `${name}.svg`), svg);
    return execFileSync("go", ["run", "./viva/icons/gen", "-pkg", "icons", dir], {
      cwd: GO_ROOT,
      encoding: "utf8",
      maxBuffer: 64 * 1024 * 1024,
    });
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

/** 产物必须已是 gofmt 格式（CI 有 gofmt 检查，常量块的列对齐靠它）。 */
function gofmt(src) {
  return execFileSync("gofmt", [], { input: src, encoding: "utf8", maxBuffer: 16 * 1024 * 1024 });
}

export function main(argv) {
  const { text, count, icons } = render();
  const vivaText = renderViva(icons);
  // Windows 检出（core.autocrlf=true）的工作区文件是 CRLF，比较前统一成 LF，否则 --check 永远报过期
  const read = (file) => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").replace(/\r\n/g, "\n") : null);
  const targets = [
    [OUT_FILE, text],
    [OUT_FILE_VIVA, vivaText],
  ];
  if (argv.includes("--check")) {
    let stale = 0;
    for (const [file, want] of targets) {
      if (read(file) === want) continue;
      stale += 1;
      console.error(`过期：${path.relative(REPO_ROOT, file)}（请运行 node apps/desktop-go/scripts/gen-native-icons.mjs 并提交）`);
    }
    if (stale === 0) console.log(`图标清单与渲染层一致（${count} 个）`);
    return stale === 0 ? 0 : 1;
  }
  for (const [file, content] of targets) {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, content);
    console.log(`写入 ${path.relative(REPO_ROOT, file)}（${count} 个图标）`);
  }
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
