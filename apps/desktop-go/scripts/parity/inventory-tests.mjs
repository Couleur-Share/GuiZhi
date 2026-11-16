#!/usr/bin/env node
/**
 * React 版 vitest 用例索引（L4 行为清单的来源之一）。
 *
 * 扫描 apps/desktop/tests 下的 *.test.ts(x)，用 TypeScript AST 取出 describe / it / test 的层级名称，
 * 并按导入关系把每个测试文件归到渲染层的某个模块（components/<目录>、stores、services、i18n、lib…）。
 * 产物是「用例 ID → 模块」的索引：Viva 版每个模块验收时，要逐条对账——
 * 要么有对应的 headless 会话测试 / Go 单测，要么登记为偏差（A/B/C）。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/inventory-tests.mjs                打印按模块汇总的 Markdown
 *   node apps/desktop-go/scripts/parity/inventory-tests.mjs --out <文件>    另写出完整 JSON 索引
 *   node apps/desktop-go/scripts/parity/inventory-tests.mjs --write         把汇总写回 docs/agents/viva-parity-inventory.md 的 tests 区块
 */
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { writeBlock } from "./lib/doc-block.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(here, "../../../..");
const TESTS = path.join(REPO, "apps/desktop/tests");
const require = createRequire(import.meta.url);
const ts = require("typescript");

function listTests(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name === "e2e") continue;
      listTests(p, out);
    } else if (/\.(test|spec)\.(ts|tsx)$/.test(e.name)) {
      out.push(p);
    }
  }
  return out.sort();
}

/** 导入说明符 → 模块名；识别不了返回 null。 */
export function moduleOfSpecifier(spec) {
  let m = /renderer\/components\/([A-Za-z0-9-]+)/.exec(spec);
  if (m) return m[1];
  m = /renderer\/(stores|services|i18n|lib|hooks|utils|styles)\b/.exec(spec);
  if (m) return m[1];
  m = /^@renderer\/components\/([A-Za-z0-9-]+)/.exec(spec);
  if (m) return m[1];
  m = /^@renderer\/(stores|services|i18n|lib|hooks|utils)\b/.exec(spec);
  if (m) return m[1];
  if (/^@guizhi\/db\b/.test(spec)) return "db";
  if (/^@guizhi\/core\b/.test(spec)) return "core";
  if (/^@(guizhi\/)?shared\b/.test(spec)) return "shared";
  return null;
}

/** it / test / describe 的调用名（含 .skip/.only/.todo/.each 修饰）。 */
function callKind(expr) {
  let node = expr;
  const mods = [];
  // it.each(table)("name") 的形态：外层 CallExpression 的 expression 是 it.each(table)
  if (ts.isCallExpression(node)) {
    const inner = node.expression;
    if (ts.isPropertyAccessExpression(inner) && inner.name.text === "each") {
      node = inner.expression;
      mods.push("each");
    } else {
      return null;
    }
  }
  while (ts.isPropertyAccessExpression(node)) {
    mods.unshift(node.name.text);
    node = node.expression;
  }
  if (!ts.isIdentifier(node)) return null;
  const base = node.text;
  if (!["describe", "it", "test"].includes(base)) return null;
  return { base, mods };
}

function nameOf(arg) {
  if (!arg) return null;
  if (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) return arg.text;
  if (ts.isTemplateExpression(arg)) return arg.getText().slice(1, -1);
  return null;
}

export function indexTests() {
  const cases = [];
  const files = [];
  for (const file of listTests(TESTS)) {
    const rel = path.relative(REPO, file).replaceAll("\\", "/");
    const src = fs.readFileSync(file, "utf8");
    const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);

    // 模块归属：统计导入说明符
    const counts = new Map();
    let dom = false;
    for (const st of sf.statements) {
      if (ts.isImportDeclaration(st) && ts.isStringLiteral(st.moduleSpecifier)) {
        const spec = st.moduleSpecifier.text;
        if (spec.startsWith("@testing-library/react")) dom = true;
        const mod = moduleOfSpecifier(spec);
        if (mod) counts.set(mod, (counts.get(mod) ?? 0) + 1);
      }
    }
    // 优先选非 ui/i18n/shared 的模块；都没有再退回出现最多的
    const ranked = [...counts.entries()].sort((a, b) => b[1] - a[1]);
    const preferred = ranked.find(([m]) => !["ui", "i18n", "shared", "lib", "utils", "hooks", "styles"].includes(m));
    const segs = path.relative(TESTS, file).split(path.sep);
    const dirName = segs.length > 2 ? segs[1] : "unit";
    const module = (preferred ?? ranked[0])?.[0] ?? `(目录:${dirName})`;

    const stack = [];
    let count = 0;
    const visit = (node) => {
      if (ts.isCallExpression(node)) {
        const kind = callKind(node.expression);
        if (kind) {
          const name = nameOf(node.arguments[0]);
          if (name !== null) {
            if (kind.base === "describe") {
              stack.push(name);
              ts.forEachChild(node, visit);
              stack.pop();
              return;
            }
            const line = sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;
            cases.push({
              id: `${rel}::${[...stack, name].join(" > ")}`,
              file: rel,
              line,
              module,
              dom,
              flags: kind.mods.filter((m) => ["skip", "only", "todo", "each", "skipIf", "runIf", "concurrent"].includes(m)),
              name: [...stack, name].join(" > "),
            });
            count++;
          }
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sf);
    files.push({ file: rel, module, dom, cases: count, imports: Object.fromEntries(counts) });
  }
  return { files, cases };
}

export function renderMarkdown({ files, cases }) {
  const byModule = new Map();
  for (const f of files) {
    const e = byModule.get(f.module) ?? { files: 0, cases: 0, domCases: 0 };
    e.files++;
    e.cases += f.cases;
    if (f.dom) e.domCases += f.cases;
    byModule.set(f.module, e);
  }
  const lines = ["| 模块 | 测试文件 | 用例 | 其中渲染 DOM 的用例 |", "| --- | ---: | ---: | ---: |"];
  let tf = 0, tc = 0, td = 0;
  for (const [mod, e] of [...byModule].sort((a, b) => b[1].cases - a[1].cases)) {
    lines.push(`| ${mod} | ${e.files} | ${e.cases} | ${e.domCases} |`);
    tf += e.files; tc += e.cases; td += e.domCases;
  }
  lines.push(`| **合计** | ${tf} | ${tc} | ${td} |`);
  const flagged = cases.filter((c) => c.flags.some((f) => ["skip", "todo", "only"].includes(f))).length;
  lines.push("");
  lines.push(`含 skip/todo/only 标记的用例：${flagged}；\`it.each\` 参数化用例按 1 条计（实际展开数更多）。`);
  return lines.join("\n");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const index = indexTests();
  const outIdx = args.indexOf("--out");
  if (outIdx >= 0) {
    const out = path.resolve(args[outIdx + 1]);
    fs.mkdirSync(path.dirname(out), { recursive: true });
    fs.writeFileSync(out, JSON.stringify(index, null, 1) + "\n");
  }
  if (args.includes("--write")) {
    console.log(writeBlock("tests", renderMarkdown(index)) ? "已更新 tests 区块。" : "tests 区块已是最新。");
  } else {
    console.log(renderMarkdown(index));
  }
}
