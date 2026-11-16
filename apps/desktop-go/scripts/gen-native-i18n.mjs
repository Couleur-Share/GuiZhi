// 把渲染层的 i18next 目录（嵌套 JSON、{{var}} 插值）转成 MyGo 的 ARB 目录（扁平键、ICU 消息），再生成 Go 函数。
// Convert the renderer's i18next catalogs into MyGo ARB catalogs, then generate the typed Go functions.
//
//   node apps/desktop-go/scripts/gen-native-i18n.mjs            # 写 native/locales/{zh-Hans,en}.json 并运行 mygo generate
//   node apps/desktop-go/scripts/gen-native-i18n.mjs --force    # 同上，且无论 ARB 是否变化都重新运行 mygo generate（pnpm gen 用）
//   node apps/desktop-go/scripts/gen-native-i18n.mjs --check    # 只比较：产物与当前源不一致就非零退出（供 CI / lint 使用）
//
// 输入：
//   apps/desktop/src/renderer/i18n/locales/{zh,en}.json   渲染层目录（切换前仍是界面文案的事实来源）
//   apps/desktop-go/native/i18n-src/{zh,en}.json          原生版独有的文案（顶层命名空间 native，渲染层没有）
// 输出：
//   apps/desktop-go/native/locales/{zh-Hans,en}.json      ARB 目录（键：嵌套路径用 _ 连接；插值 {{x}} → {x}）
//   apps/desktop-go/native/locales/catalog_gen.go         mygo generate 的产物（入库，go build / go test 不依赖 CLI）
//   apps/desktop-go/viva/locales/catalog_gen.go           Viva 路线的产物：普通 Go，每个键一个类型化函数，按当前语言返回中文或英文（不依赖 MyGo）
//
// 源语言是简体中文（mygo.json 的 i18n.source = zh-Hans）：它必须有每一条消息，英文缺的键回落到中文。

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const REPO_ROOT = path.resolve(import.meta.dirname, "../../..");
const GO_ROOT = path.join(REPO_ROOT, "apps/desktop-go");
const RENDERER_LOCALES = path.join(REPO_ROOT, "apps/desktop/src/renderer/i18n/locales");
const NATIVE_SRC = path.join(GO_ROOT, "native/i18n-src");
const OUT_DIR = path.join(GO_ROOT, "native/locales");
const VIVA_OUT = path.join(GO_ROOT, "viva/locales/catalog_gen.go");

/** 渲染层语言码 → ARB 文件名与 @@locale。 */
export const LANGUAGES = [
  { src: "zh", tag: "zh-Hans" },
  { src: "en", tag: "en" },
];

const KEY_RE = /^[A-Za-z][A-Za-z0-9_]*$/;
const INTERP_RE = /\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/y;

/** 嵌套对象 → 叶子 [路径数组, 字符串]。 */
export function flatten(obj, prefix = []) {
  const out = [];
  for (const [k, v] of Object.entries(obj)) {
    if (v !== null && typeof v === "object" && !Array.isArray(v)) out.push(...flatten(v, [...prefix, k]));
    else if (typeof v === "string") out.push([[...prefix, k], v]);
    else throw new Error(`不支持的目录值类型：${[...prefix, k].join(".")}（${Array.isArray(v) ? "数组" : typeof v}）`);
  }
  return out;
}

/** i18next 文案 → ICU MessageFormat；返回 { message, params }（params 按首次出现的顺序）。 */
export function toIcu(text) {
  const params = [];
  let out = "";
  for (let i = 0; i < text.length; ) {
    const ch = text[i];
    if (ch === "{" && text[i + 1] === "{") {
      INTERP_RE.lastIndex = i;
      const m = INTERP_RE.exec(text);
      if (!m) throw new Error(`无法解析的插值（含格式或嵌套？）：${JSON.stringify(text.slice(i, i + 40))}`);
      if (!params.includes(m[1])) params.push(m[1]);
      out += `{${m[1]}}`;
      i += m[0].length;
    } else if (ch === "{" || ch === "}") {
      out += `'${ch}'`; // 字面花括号用撇号引起来
      i += 1;
    } else if (ch === "'") {
      // 撇号紧挨着大括号（含我们生成的占位符）时会被当成引号，必须写成两个；其余撇号原样
      const next = text[i + 1];
      out += next === "{" || next === "}" ? "''" : "'";
      i += 1;
    } else {
      out += ch;
      i += 1;
    }
  }
  return { message: out, params };
}

/** 路径 → ARB 键；不合法字符一律换成 _，之后统一检查碰撞。 */
export function arbKey(parts) {
  return parts.join("_").replace(/[^A-Za-z0-9_]/g, "_");
}

function readJson(file) {
  return JSON.parse(fs.readFileSync(file, "utf8"));
}

/** 读取一个语言的全部消息（渲染层 + 原生独有），返回 Map<arbKey, { text, path }>。 */
export function loadLanguage(src) {
  const map = new Map();
  const add = (parts, text, origin) => {
    const key = arbKey(parts);
    if (!KEY_RE.test(key)) throw new Error(`${origin}：键 ${parts.join(".")} 转成 ${key} 后不合法（必须以字母开头）`);
    const prev = map.get(key);
    if (prev && prev.path !== parts.join(".")) throw new Error(`键碰撞：${prev.path} 与 ${parts.join(".")} 都转成 ${key}`);
    if (prev) throw new Error(`重复键：${parts.join(".")}（${origin}）`);
    map.set(key, { text, path: parts.join(".") });
  };
  for (const [parts, text] of flatten(readJson(path.join(RENDERER_LOCALES, `${src}.json`)))) add(parts, text, `渲染层 ${src}.json`);
  const extra = path.join(NATIVE_SRC, `${src}.json`);
  if (fs.existsSync(extra)) for (const [parts, text] of flatten(readJson(extra))) add(parts, text, `原生独有 ${src}.json`);
  return map;
}

/** 生成各语言的 ARB 对象（键顺序：源语言的顺序；其余语言只含自己有的键）。 */
export function buildCatalogs() {
  const loaded = new Map(LANGUAGES.map((l) => [l.src, loadLanguage(l.src)]));
  const source = loaded.get(LANGUAGES[0].src);
  const problems = [];
  for (const l of LANGUAGES.slice(1)) {
    const other = loaded.get(l.src);
    for (const k of source.keys()) if (!other.has(k)) problems.push(`${l.tag} 缺少键 ${source.get(k).path}`);
    for (const k of other.keys()) if (!source.has(k)) problems.push(`${l.tag} 多出源语言没有的键 ${other.get(k).path}`);
  }
  const catalogs = new Map();
  for (const l of LANGUAGES) {
    const arb = { "@@locale": l.tag };
    for (const [key, { text, path: origin }] of loaded.get(l.src)) {
      let converted;
      try {
        converted = toIcu(text);
      } catch (e) {
        problems.push(`${l.tag} ${origin}：${e.message}`);
        continue;
      }
      arb[key] = converted.message;
      if (l === LANGUAGES[0] && converted.params.length > 0) {
        // 源语言目录描述占位符：统一 Object，调用方可以直接传数字或字符串
        arb[`@${key}`] = { placeholders: Object.fromEntries(converted.params.map((p) => [p, { type: "Object" }])) };
      }
    }
    catalogs.set(l.tag, arb);
  }
  // 各语言占位符集合必须一致，否则生成的函数签名对不上
  const srcArb = catalogs.get(LANGUAGES[0].tag);
  for (const l of LANGUAGES.slice(1)) {
    const arb = catalogs.get(l.tag);
    for (const key of Object.keys(arb)) {
      if (key.startsWith("@") || !(key in srcArb)) continue;
      const a = toIcu(loaded.get(LANGUAGES[0].src).get(key).text).params.slice().sort().join(",");
      const b = toIcu(loaded.get(l.src).get(key).text).params.slice().sort().join(",");
      if (a !== b) problems.push(`${key}：${LANGUAGES[0].tag} 的占位符 [${a}] 与 ${l.tag} 的 [${b}] 不一致`);
    }
  }
  return { catalogs, problems, loaded, counts: Object.fromEntries(LANGUAGES.map((l) => [l.tag, loaded.get(l.src).size])) };
}

const GO_RESERVED = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func", "go", "goto", "if",
  "import", "interface", "map", "package", "range", "return", "select", "struct", "switch", "type", "var",
  // 生成代码里用到的辅助函数名与内建名：参数不得与它们同名
  "useEn", "fmtArg", "string", "any",
]);

/** ARB 键 → Go 函数名：按下划线切段，每段首字母大写（settings_backgroundTasks → SettingsBackgroundTasks）。 */
export function goFuncName(key) {
  return key
    .split("_")
    .filter(Boolean)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join("");
}

/** i18next 文案 → 片段数组：{ lit } 或 { param }；与 toIcu 同一套插值规则。 */
export function parseTemplate(text) {
  const parts = [];
  let lit = "";
  for (let i = 0; i < text.length; ) {
    if (text[i] === "{" && text[i + 1] === "{") {
      INTERP_RE.lastIndex = i;
      const m = INTERP_RE.exec(text);
      if (!m) throw new Error(`无法解析的插值（含格式或嵌套？）：${JSON.stringify(text.slice(i, i + 40))}`);
      if (lit) parts.push({ lit });
      lit = "";
      parts.push({ param: m[1] });
      i += m[0].length;
    } else {
      lit += text[i];
      i += 1;
    }
  }
  if (lit) parts.push({ lit });
  return parts;
}

/** Go 字符串字面量。JSON.stringify 的转义是 Go 解释型字符串字面量的子集（\uXXXX、\n、\"、\\ 都合法）。 */
function goString(text) {
  return JSON.stringify(text);
}

function goExprOf(parts, rename) {
  if (parts.length === 0) return '""';
  return parts.map((p) => (p.lit !== undefined ? goString(p.lit) : `fmtArg(${rename.get(p.param)})`)).join(" + ");
}

/** 生成 viva/locales/catalog_gen.go 的源码：每个键一个函数，参数按源语言里首次出现的顺序，类型 any。 */
export function renderViva(loaded) {
  const zh = loaded.get("zh");
  const en = loaded.get("en");
  const names = new Map();
  const lines = [];
  lines.push("// Code generated by apps/desktop-go/scripts/gen-native-i18n.mjs from the renderer's i18n catalogs; DO NOT EDIT.");
  lines.push("// 由 gen-native-i18n.mjs 生成：文案来自渲染层 i18n/locales/{zh,en}.json 与 native/i18n-src，改源后重新生成，不要手改。");
  lines.push("");
  lines.push("package locales");
  lines.push("");
  for (const [key, { text, path: origin }] of zh) {
    const name = goFuncName(key);
    if (!/^[A-Z][A-Za-z0-9]*$/.test(name)) throw new Error(`键 ${origin} 生成的函数名 ${name} 不合法`);
    if (names.has(name)) throw new Error(`函数名碰撞：${names.get(name)} 与 ${origin} 都生成 ${name}`);
    names.set(name, origin);
    const enEntry = en.get(key);
    if (!enEntry) throw new Error(`en 缺少键 ${origin}`);
    const zhParts = parseTemplate(text);
    const enParts = parseTemplate(enEntry.text);
    const params = [];
    for (const p of [...zhParts, ...enParts]) if (p.param !== undefined && !params.includes(p.param)) params.push(p.param);
    const rename = new Map(params.map((p) => [p, GO_RESERVED.has(p) ? `${p}_` : p]));
    const signature = params.length === 0 ? "" : `${params.map((p) => rename.get(p)).join(", ")} any`;
    lines.push(`// ${name} 是 ${origin}：${text.replace(/\s+/g, " ").slice(0, 80)}`);
    lines.push(`func ${name}(${signature}) string {`);
    lines.push("\tif useEn() {");
    lines.push(`\t\treturn ${goExprOf(enParts, rename)}`);
    lines.push("\t}");
    lines.push(`\treturn ${goExprOf(zhParts, rename)}`);
    lines.push("}");
    lines.push("");
  }
  return lines.join("\n");
}

function gofmt(src) {
  return execFileSync("gofmt", [], { input: src, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
}

function serialize(arb) {
  return `${JSON.stringify(arb, null, 2)}\n`;
}

function runMygoGenerate() {
  // 用 go run pkg@version 临时运行 CLI：不用把 CLI 的依赖（打包 rpm 的库等）写进本模块的 go.mod；
  // 版本取 go.mod 里的 MyGo，保证生成的代码与运行时同版本。
  const env = { ...process.env, CGO_ENABLED: "0" };
  const version = execFileSync("go", ["list", "-m", "-f", "{{.Version}}", "github.com/egoist/mygo"], { cwd: GO_ROOT, env, encoding: "utf8" }).trim();
  execFileSync("go", ["run", `github.com/egoist/mygo/cmd/mygo@${version}`, "generate"], { cwd: GO_ROOT, stdio: "inherit", env });
}

export function main(argv) {
  const check = argv.includes("--check");
  const force = argv.includes("--force");
  const { catalogs, problems, counts, loaded } = buildCatalogs();
  if (problems.length > 0) {
    console.error(`目录转换失败（${problems.length} 个问题）：`);
    for (const p of problems.slice(0, 40)) console.error(`  - ${p}`);
    if (problems.length > 40) console.error(`  …另有 ${problems.length - 40} 个`);
    return 1;
  }
  let stale = 0;
  for (const [tag, arb] of catalogs) {
    const file = path.join(OUT_DIR, `${tag}.json`);
    const next = serialize(arb);
    // Windows 检出（core.autocrlf=true）的工作区文件是 CRLF，比较前统一成 LF，否则 --check 永远报过期
    const prev = fs.existsSync(file) ? fs.readFileSync(file, "utf8").replace(/\r\n/g, "\n") : null;
    if (prev === next) continue;
    stale += 1;
    if (check) console.error(`过期：${path.relative(REPO_ROOT, file)}（请运行 node apps/desktop-go/scripts/gen-native-i18n.mjs 并提交）`);
    else {
      fs.mkdirSync(OUT_DIR, { recursive: true });
      fs.writeFileSync(file, next);
      console.log(`写入 ${path.relative(REPO_ROOT, file)}`);
    }
  }
  console.log(`消息数：${Object.entries(counts).map(([t, n]) => `${t} ${n}`).join("，")}`);
  // Viva 路线：普通 Go 的类型化函数
  const viva = gofmt(renderViva(loaded));
  const vivaPrev = fs.existsSync(VIVA_OUT) ? fs.readFileSync(VIVA_OUT, "utf8").replace(/\r\n/g, "\n") : null;
  if (vivaPrev !== viva) {
    stale += 1;
    if (check) console.error(`过期：${path.relative(REPO_ROOT, VIVA_OUT)}（请运行 node apps/desktop-go/scripts/gen-native-i18n.mjs 并提交）`);
    else {
      fs.mkdirSync(path.dirname(VIVA_OUT), { recursive: true });
      fs.writeFileSync(VIVA_OUT, viva);
      console.log(`写入 ${path.relative(REPO_ROOT, VIVA_OUT)}`);
    }
  }
  if (check) return stale === 0 ? 0 : 1;
  // --force：ARB 没变也重新生成 Go。MyGo 升级后生成器的输出可能变，CI 的「重新生成后 git diff」靠它发现。
  if (force || stale > 0 || !fs.existsSync(path.join(OUT_DIR, "catalog_gen.go"))) runMygoGenerate();
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
