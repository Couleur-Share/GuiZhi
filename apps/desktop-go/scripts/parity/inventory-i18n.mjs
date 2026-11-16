#!/usr/bin/env node
/**
 * 渲染层 i18n 键使用盘点（Viva 对等工程的文案分母）。
 *
 * 用 TypeScript AST 扫描 apps/desktop/src/renderer 下所有非测试源码：
 *   1. `t("a.b")` / `t("a.b", "默认文案")` / `i18n.t(...)`：静态键，并记录第二个参数里的内联默认文案；
 *   2. `t(\`prefix.${x}\`)`：动态键，只能确定前缀；
 *   3. 表驱动键：任何恰好等于 zh.json 某个键的字符串字面量（如 `titleKey: "settings.general"`）；
 *   4. 硬编码界面文字：不经 t() 的 CJK 字符串字面量/JSX 文本，以及 JSX 里直接写的可见英文文本；
 *      Viva 版同样必须携带（新代码里不得出现中文字面量，需经生成的文案表）。
 *   5. 三类结论：
 *      - zh.json 里有、源码里没有任何引用的键（候选的死键）；
 *      - 源码引用、但 zh.json 缺失的键：其中带内联默认文案的是「只存在于代码里的文案」，
 *        Viva 版必须原样携带，否则会出现与 React 版不一致的界面文字；
 *      - zh.json 与 en.json 的键集合差异。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/inventory-i18n.mjs                 打印 Markdown 摘要
 *   node apps/desktop-go/scripts/parity/inventory-i18n.mjs --out <文件>     另写出完整 JSON
 *   node apps/desktop-go/scripts/parity/inventory-i18n.mjs --check          只检查，有硬性问题时非零退出
 *   node apps/desktop-go/scripts/parity/inventory-i18n.mjs --write          把摘要写回 docs/agents/viva-parity-inventory.md 的 i18n 区块
 */
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { writeBlock } from "./lib/doc-block.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(here, "../../../..");
const RENDERER = path.join(REPO, "apps/desktop/src/renderer");
const require = createRequire(import.meta.url);
const ts = require("typescript");

function flatten(obj, prefix = "") {
  const out = [];
  for (const [k, v] of Object.entries(obj)) {
    if (v && typeof v === "object") out.push(...flatten(v, `${prefix}${k}.`));
    else out.push(`${prefix}${k}`);
  }
  return out;
}

function readLocale(name) {
  return JSON.parse(fs.readFileSync(path.join(RENDERER, "i18n/locales", `${name}.json`), "utf8"));
}

/** 递归列出渲染层源码（跳过测试与声明文件）。 */
function listSources(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name === "__tests__") continue;
      listSources(p, out);
    } else if (/\.(ts|tsx)$/.test(e.name) && !/\.(test|spec)\.(ts|tsx)$/.test(e.name) && !e.name.endsWith(".d.ts")) {
      out.push(p);
    }
  }
  return out;
}

/** 文件所属模块：components/<目录> 取目录名，其余取顶层目录名。 */
function moduleOf(rel) {
  const parts = rel.split("/");
  if (parts[0] === "components" && parts.length > 2) return parts[1];
  return parts[0];
}

function isTCall(node) {
  const callee = node.expression;
  if (ts.isIdentifier(callee)) return callee.text === "t";
  if (ts.isPropertyAccessExpression(callee)) return callee.name.text === "t";
  return false;
}

/** 取一个表达式里所有可确定的字符串字面量候选（条件表达式取两支）。 */
function literalCandidates(expr) {
  // 空串（如 `labelKey || ""` 的兜底）不是键。
  if (ts.isStringLiteral(expr) || ts.isNoSubstitutionTemplateLiteral(expr)) return expr.text === "" ? [] : [expr.text];
  if (ts.isConditionalExpression(expr)) return [...literalCandidates(expr.whenTrue), ...literalCandidates(expr.whenFalse)];
  if (ts.isParenthesizedExpression(expr)) return literalCandidates(expr.expression);
  if (ts.isBinaryExpression(expr) && expr.operatorToken.kind === ts.SyntaxKind.BarBarToken) {
    return [...literalCandidates(expr.left), ...literalCandidates(expr.right)];
  }
  return [];
}

export function scan() {
  const zh = readLocale("zh");
  const en = readLocale("en");
  const zhKeys = new Set(flatten(zh));
  const enKeys = new Set(flatten(en));

  const staticRefs = new Map(); // key -> [{file,line,default}]
  const literalRefs = new Map(); // key -> [{file,line}]（表驱动）
  const hardcoded = []; // {file, line, kind, text}：不经 t() 的界面文字候选
  const dynamicRefs = []; // {prefix, file, line, text}
  const unresolved = []; // {file, line, text}

  for (const file of listSources(RENDERER)) {
    const rel = path.relative(RENDERER, file).replaceAll("\\", "/");
    const src = fs.readFileSync(file, "utf8");
    const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    const lineOf = (n) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;
    const CJK = /[\u3400-\u9fff\uff00-\uffef]/;
    const isInsideT = (n) => {
      for (let p = n.parent; p; p = p.parent) {
        if (ts.isCallExpression(p) && isTCall(p)) return true;
      }
      return false;
    };
    const visit = (node) => {
      // JSX 里直接写的文本（含中文或英文字母），不经 t()
      if (ts.isJsxText(node)) {
        const text = node.text.replace(/\s+/g, " ").trim();
        if (text && /[A-Za-z\u3400-\u9fff]/.test(text)) hardcoded.push({ file: rel, line: lineOf(node), kind: "jsx-text", text: text.slice(0, 80) });
      } else if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && CJK.test(node.text) && !isInsideT(node)) {
        hardcoded.push({ file: rel, line: lineOf(node), kind: "cjk-literal", text: node.text.replace(/\s+/g, " ").slice(0, 80) });
      }
      if (ts.isCallExpression(node) && isTCall(node) && node.arguments.length > 0) {
        const first = node.arguments[0];
        const second = node.arguments[1];
        const def = second ? literalCandidates(second)[0] : undefined;
        const keys = literalCandidates(first);
        if (keys.length > 0) {
          for (const key of keys) {
            const list = staticRefs.get(key) ?? [];
            list.push({ file: rel, line: lineOf(node), default: typeof def === "string" ? def : undefined });
            staticRefs.set(key, list);
          }
        } else if (ts.isTemplateExpression(first)) {
          dynamicRefs.push({ prefix: first.head.text, file: rel, line: lineOf(node), text: first.getText(sf).slice(0, 120) });
        } else if (!(ts.isStringLiteral(first) || ts.isNoSubstitutionTemplateLiteral(first)) || first.text !== "") {
          unresolved.push({ file: rel, line: lineOf(node), text: first.getText(sf).slice(0, 120) });
        }
      } else if (ts.isTemplateExpression(node) && !(node.parent && ts.isCallExpression(node.parent) && isTCall(node.parent))) {
        // 先把模板串赋给变量、再传给 t() 的写法（如 `settings.color${…}`）：前缀能对上 zh.json 的键时按动态前缀记录。
        const head = node.head.text;
        if (/^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z0-9]+)+/.test(head) && [...zhKeys].some((k) => k.startsWith(head))) {
          dynamicRefs.push({ prefix: head, file: rel, line: lineOf(node), text: node.getText(sf).slice(0, 120), viaVariable: true });
        }
      } else if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
        // 表驱动：字面量恰好是 zh.json 的键（且不是 t() 的第一个参数——那由上面处理）
        const parent = node.parent;
        const isTArg = parent && ts.isCallExpression(parent) && isTCall(parent) && parent.arguments[0] === node;
        if (!isTArg && zhKeys.has(node.text)) {
          const list = literalRefs.get(node.text) ?? [];
          list.push({ file: rel, line: lineOf(node) });
          literalRefs.set(node.text, list);
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sf);
  }

  const dynamicPrefixes = [...new Set(dynamicRefs.map((d) => d.prefix))];
  const referenced = (key) =>
    staticRefs.has(key) || literalRefs.has(key) || dynamicPrefixes.some((p) => p !== "" && key.startsWith(p));

  const unreferenced = [...zhKeys].filter((k) => !referenced(k)).sort();
  const missingInZh = [...staticRefs.keys()].filter((k) => !zhKeys.has(k)).sort();
  const codeOnly = missingInZh.filter((k) => staticRefs.get(k).some((r) => r.default !== undefined));
  const rawKeyLeaks = missingInZh.filter((k) => !staticRefs.get(k).some((r) => r.default !== undefined));
  const zhNotEn = [...zhKeys].filter((k) => !enKeys.has(k)).sort();
  const enNotZh = [...enKeys].filter((k) => !zhKeys.has(k)).sort();

  const byModule = new Map();
  const setOf = (mod, field) => {
    const e = byModule.get(mod) ?? { staticKeys: new Set(), literalKeys: new Set() };
    byModule.set(mod, e);
    return e[field];
  };
  for (const [key, refs] of staticRefs) for (const r of refs) setOf(moduleOf(r.file), "staticKeys").add(key);
  for (const [key, refs] of literalRefs) for (const r of refs) setOf(moduleOf(r.file), "literalKeys").add(key);

  return {
    hardcoded,
    zhKeys, enKeys, staticRefs, literalRefs, dynamicRefs, unresolved,
    dynamicPrefixes, unreferenced, missingInZh, codeOnly, rawKeyLeaks, zhNotEn, enNotZh, byModule,
  };
}

export function renderMarkdown(r) {
  const lines = [];
  lines.push(`- zh.json 键数：${r.zhKeys.size}；en.json 键数：${r.enKeys.size}`);
  lines.push(`- 源码静态 \`t("…")\` 引用的不同键：${r.staticRefs.size}；表驱动字面量引用的不同键：${r.literalRefs.size}；动态模板前缀：${r.dynamicPrefixes.length} 个（${r.dynamicRefs.length} 处）；无法解析的调用：${r.unresolved.length} 处`);
  lines.push(`- zh.json 有而源码无任何引用的键（候选死键，含被动态前缀覆盖之外的）：${r.unreferenced.length}`);
  lines.push(`- 源码引用而 zh.json 缺失的键：${r.missingInZh.length}（其中带内联默认文案、即「只存在于代码里的文案」：${r.codeOnly.length}；既缺失又无默认文案、界面会显示原始键：${r.rawKeyLeaks.length}）`);
  lines.push(`- zh 有而 en 无：${r.zhNotEn.length}；en 有而 zh 无：${r.enNotZh.length}`);
  const cjk = r.hardcoded.filter((h) => h.kind === "cjk-literal").length;
  const jsx = r.hardcoded.filter((h) => h.kind === "jsx-text").length;
  lines.push(`- 不经 t() 的硬编码界面文字候选：CJK 字符串字面量 ${cjk} 处、JSX 直写文本 ${jsx} 处（需人工甄别哪些是用户可见文案，见完整 JSON 的 hardcoded 字段）`);
  const area = (prefix) => r.hardcoded.filter((h) => h.kind === "cjk-literal" && h.file.startsWith(prefix)).length;
  const hcFiles = new Set(r.hardcoded.filter((h) => h.kind === "cjk-literal").map((h) => h.file)).size;
  lines.push(`  - CJK 字面量分布在 ${hcFiles} 个文件：\`services/\` ${area("services/")} 处（多为 AI 提示词与结果文案）、\`stores/\` ${area("stores/")} 处（错误与 Toast 文案）、\`components/\` ${area("components/")} 处（列标题、阶段名、命令面板别名等）`);
  lines.push("");
  lines.push("| 模块（文件所在目录） | 静态引用的不同键 | 表驱动引用的不同键 |");
  lines.push("| --- | ---: | ---: |");
  for (const [mod, e] of [...r.byModule].sort((a, b) => a[0].localeCompare(b[0]))) {
    lines.push(`| ${mod} | ${e.staticKeys.size} | ${e.literalKeys.size} |`);
  }
  return lines.join("\n");
}

function toJSON(r) {
  const obj = (m, mapFn) => Object.fromEntries([...m].sort((a, b) => a[0].localeCompare(b[0])).map(([k, v]) => [k, mapFn(v)]));
  return {
    zhKeyCount: r.zhKeys.size,
    enKeyCount: r.enKeys.size,
    staticRefs: obj(r.staticRefs, (v) => v),
    literalRefs: obj(r.literalRefs, (v) => v),
    dynamicRefs: r.dynamicRefs,
    hardcoded: r.hardcoded,
    unresolved: r.unresolved,
    unreferenced: r.unreferenced,
    missingInZh: r.missingInZh.map((k) => ({ key: k, defaults: [...new Set(r.staticRefs.get(k).map((x) => x.default).filter((d) => d !== undefined))], refs: r.staticRefs.get(k).map((x) => `${x.file}:${x.line}`) })),
    codeOnly: r.codeOnly,
    rawKeyLeaks: r.rawKeyLeaks,
    zhNotEn: r.zhNotEn,
    enNotZh: r.enNotZh,
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const r = scan();
  const outIdx = args.indexOf("--out");
  if (outIdx >= 0) {
    const out = path.resolve(args[outIdx + 1]);
    fs.mkdirSync(path.dirname(out), { recursive: true });
    fs.writeFileSync(out, JSON.stringify(toJSON(r), null, 1) + "\n");
  }
  if (args.includes("--write")) {
    console.log(writeBlock("i18n", renderMarkdown(r)) ? "已更新 i18n 区块。" : "i18n 区块已是最新。");
  } else if (args.includes("--check")) {
    // 硬性问题：zh 与 en 键集合不一致；源码引用了既不在 zh.json 又没有默认文案的键。
    const problems = [];
    if (r.zhNotEn.length) problems.push(`zh 有而 en 无：${r.zhNotEn.length} 个（例如 ${r.zhNotEn.slice(0, 3).join("、")}）`);
    if (r.enNotZh.length) problems.push(`en 有而 zh 无：${r.enNotZh.length} 个（例如 ${r.enNotZh.slice(0, 3).join("、")}）`);
    if (r.rawKeyLeaks.length) problems.push(`缺失且无默认文案的键：${r.rawKeyLeaks.length} 个（例如 ${r.rawKeyLeaks.slice(0, 3).join("、")}）`);
    if (problems.length) {
      console.error(problems.map((p) => "  - " + p).join("\n"));
      process.exit(1);
    }
    console.log("i18n 盘点检查通过。");
  } else {
    console.log(renderMarkdown(r));
  }
}
