/**
 * 国际化门禁（渲染进程）。中英键对称与插值占位符一致由 tests/unit/i18n/locale-parity.test.ts 负责。
 *
 * 1. en.json 的值里不能有中文（漏译）。
 * 2. 代码以字面量引用的键（t("…")、*Key: "…"、i18nKey="…"）必须存在于语言包。
 * 3. 语言包里的键必须被引用：任意字符串字面量等于该键，或命中模板/拼接出来的键前缀。
 * 4. 界面文本里写死的中文按文件计数，不得超过 config/i18n-hardcoded-baseline.json；
 *    计数下降时要求同步收紧基线，避免豁免变成后门。
 *
 * 允许中文的位置：t() / translate() 的参数（兜底文案）、*Fb / fallback / defaultValue 属性、
 * <Trans> 子节点、console 与 logAppError 日志、Error/RegExp 构造参数；
 * renderer/services 下是模型提示词与 AI 编排，不计入第 4 项。
 *
 *   node scripts/check-i18n.mjs                    # 门禁
 *   node scripts/check-i18n.mjs --report           # 列出每处问题
 *   node scripts/check-i18n.mjs --update-baseline  # 用当前计数重写基线（需在评审中说明）
 */
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import ts from "typescript";

const RENDERER_DIR = "apps/desktop/src/renderer";
const BASELINE_PATH = "config/i18n-hardcoded-baseline.json";
const CJK = /[\u3400-\u9fff\uf900-\ufaff]/;
const KEY_SHAPE = /^[A-Za-z][\w-]*(\.[\w-]+)+$/;
const FALLBACK_PROPERTY = /^(fallback|fallbackText|defaultValue)$|Fb$|Fallback$/;
const TRANSLATE_CALLEES = new Set(["t", "translate"]);
const LOG_CALLEES = new Set(["logAppError", "appError"]);
const ALLOWED_CONSTRUCTORS = new Set(["Error", "TypeError", "RangeError", "RegExp"]);

function flatten(object, prefix = "", out = new Map()) {
  for (const [name, value] of Object.entries(object)) {
    const key = prefix ? `${prefix}.${name}` : name;
    if (value && typeof value === "object") flatten(value, key, out);
    else out.set(key, value);
  }
  return out;
}

function collectSourceFiles(directory, files = []) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) collectSourceFiles(entryPath, files);
    else if (/\.tsx?$/.test(entry.name) && !entry.name.endsWith(".d.ts")) files.push(entryPath);
  }
  return files;
}

function calleeName(expression) {
  if (ts.isIdentifier(expression)) return expression.text;
  if (ts.isPropertyAccessExpression(expression)) return expression.name.text;
  return "";
}

function isConsoleCall(expression) {
  return (
    ts.isPropertyAccessExpression(expression) &&
    ts.isIdentifier(expression.expression) &&
    expression.expression.text === "console"
  );
}

function isAllowedContext(node) {
  for (let current = node.parent; current; current = current.parent) {
    if (ts.isCallExpression(current)) {
      const name = calleeName(current.expression);
      if (TRANSLATE_CALLEES.has(name) || LOG_CALLEES.has(name) || ALLOWED_CONSTRUCTORS.has(name)) return true;
      if (isConsoleCall(current.expression)) return true;
    }
    if (ts.isNewExpression(current) && ALLOWED_CONSTRUCTORS.has(calleeName(current.expression))) return true;
    if (
      ts.isPropertyAssignment(current) &&
      (ts.isIdentifier(current.name) || ts.isStringLiteral(current.name)) &&
      FALLBACK_PROPERTY.test(current.name.text)
    ) return true;
    if (ts.isJsxElement(current) && current.openingElement.tagName.getText() === "Trans") return true;
  }
  return false;
}

function literalTexts(node) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return [node.text];
  if (ts.isTemplateExpression(node)) return [node.head.text, ...node.templateSpans.map((span) => span.literal.text)];
  if (ts.isJsxText(node)) return [node.text];
  return [];
}

/**
 * root 为仓库根目录，报告与基线里的路径都相对它。baseline 为 null 时不比较基线。
 * 返回的 errors 已包含基线比较结果；details 是超出基线的文件里每一处写死中文。
 */
export function analyzeI18n({ root, rendererDir = RENDERER_DIR, baseline = {} }) {
  const localesDir = path.join(root, rendererDir, "i18n/locales");
  const excludedDirs = ["services", "i18n"].map((dir) => `${rendererDir}/${dir}/`);
  const zh = flatten(JSON.parse(fs.readFileSync(path.join(localesDir, "zh.json"), "utf8")));
  const en = flatten(JSON.parse(fs.readFileSync(path.join(localesDir, "en.json"), "utf8")));
  const errors = [];
  const details = [];

  for (const [key, value] of en) {
    if (typeof value === "string" && CJK.test(value)) errors.push(`en.json 的 ${key} 含中文，疑似漏译`);
  }

  const literals = new Set();
  const dynamicPrefixes = new Set();
  const missing = new Map();
  const hardcoded = new Map();

  for (const file of collectSourceFiles(path.join(root, rendererDir))) {
    const relative = path.relative(root, file).split(path.sep).join("/");
    const source = ts.createSourceFile(
      file,
      fs.readFileSync(file, "utf8"),
      ts.ScriptTarget.Latest,
      true,
      file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const checkHardcoded = !excludedDirs.some((dir) => relative.startsWith(dir));
    const lineOf = (node) => source.getLineAndCharacterOfPosition(node.getStart()).line + 1;
    const noteMissing = (key, node) => {
      if (KEY_SHAPE.test(key) && !zh.has(key)) missing.set(key, `${relative}:${lineOf(node)}`);
    };

    const visit = (node) => {
      const texts = literalTexts(node);
      if (texts.length) {
        if (ts.isTemplateExpression(node)) {
          if (KEY_SHAPE.test(`${node.head.text}x`)) dynamicPrefixes.add(node.head.text);
        } else if (!ts.isJsxText(node)) {
          literals.add(texts[0]);
          if (texts[0].endsWith(".") && KEY_SHAPE.test(`${texts[0]}x`)) dynamicPrefixes.add(texts[0]);
        }
        if (checkHardcoded && texts.some((text) => CJK.test(text)) && !isAllowedContext(node)) {
          const list = hardcoded.get(relative) ?? [];
          list.push(`${relative}:${lineOf(node)}  ${texts.join("…").trim().replace(/\s+/g, " ").slice(0, 60)}`);
          hardcoded.set(relative, list);
        }
      }
      if (ts.isCallExpression(node) && TRANSLATE_CALLEES.has(calleeName(node.expression))) {
        const [first] = node.arguments;
        if (first && (ts.isStringLiteral(first) || ts.isNoSubstitutionTemplateLiteral(first))) noteMissing(first.text, first);
      }
      if (
        ts.isPropertyAssignment(node) &&
        (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name)) &&
        /Key$/.test(node.name.text) &&
        ts.isStringLiteral(node.initializer)
      ) noteMissing(node.initializer.text, node.initializer);
      if (
        ts.isJsxAttribute(node) &&
        node.name.getText() === "i18nKey" &&
        node.initializer &&
        ts.isStringLiteral(node.initializer)
      ) noteMissing(node.initializer.text, node.initializer);
      ts.forEachChild(node, visit);
    };
    visit(source);
  }

  for (const [key, where] of missing) errors.push(`代码引用了不存在的键 ${key}（${where}）`);
  const prefixes = [...dynamicPrefixes];
  for (const key of zh.keys()) {
    if (!literals.has(key) && !prefixes.some((prefix) => key.startsWith(prefix))) {
      errors.push(`语言包中的键未被引用：${key}`);
    }
  }

  const counts = Object.fromEntries(
    [...hardcoded.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([file, list]) => [file, list.length]),
  );
  if (baseline) {
    for (const [file, count] of Object.entries(counts)) {
      const allowed = baseline[file] ?? 0;
      if (count > allowed) {
        errors.push(`${file} 写死的中文 ${count} 处，基线允许 ${allowed} 处；请改用 t("键", "中文兜底") 并补齐 en.json`);
        details.push(...hardcoded.get(file));
      }
    }
    for (const [file, allowed] of Object.entries(baseline)) {
      const count = counts[file] ?? 0;
      if (count < allowed) {
        errors.push(`${file} 写死的中文已降到 ${count} 处，请把基线改为 ${count}${count === 0 ? "（删除该条目）" : ""}`);
      }
    }
  }
  return { errors, details, counts, hardcoded, keyCount: zh.size };
}

function main() {
  const report = process.argv.includes("--report");
  const updateBaseline = process.argv.includes("--update-baseline");
  const baseline = updateBaseline
    ? null
    : fs.existsSync(BASELINE_PATH) ? JSON.parse(fs.readFileSync(BASELINE_PATH, "utf8")) : {};
  const { errors, details, counts, hardcoded, keyCount } = analyzeI18n({ root: process.cwd(), baseline });

  if (updateBaseline) {
    fs.writeFileSync(BASELINE_PATH, `${JSON.stringify(counts, null, 2)}\n`);
    console.log(`已用当前计数重写 ${BASELINE_PATH}（${Object.keys(counts).length} 个文件）`);
  }
  if (report) {
    for (const list of hardcoded.values()) details.push(...list);
    for (const line of [...new Set(details)]) console.log(line);
  } else {
    for (const line of details) console.log(line);
  }
  if (errors.length) {
    console.error(`国际化检查未通过（${errors.length} 项）：`);
    for (const error of errors.slice(0, report ? errors.length : 40)) console.error(`- ${error}`);
    if (!report && errors.length > 40) console.error(`… 其余 ${errors.length - 40} 项用 --report 查看`);
    process.exitCode = 1;
    return;
  }
  const total = Object.values(counts).reduce((sum, count) => sum + count, 0);
  console.log(`国际化检查通过：${keyCount} 个键均有引用；写死中文 ${total} 处，未超出基线。`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) main();
