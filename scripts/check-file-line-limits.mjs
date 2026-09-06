/**
 * 文件行数门禁。
 *
 * baseline 是「历史遗留文件的临时豁免额度」，不是长期许可：条目一旦指向
 * 已删除的文件，或对应文件已经缩到 PREFERRED_LIMIT 以下，本脚本会报错要求
 * 删掉该条目。否则豁免会在重构之后继续留着，变成给大文件开的后门。
 */
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";

const HARD_LIMIT = 2000;
const PREFERRED_LIMIT = 1500;
const REPORT_THRESHOLD = 1400;
const ROOTS = ["apps", "packages", "scripts"];
const SOURCE_EXTENSIONS = new Set([".js", ".mjs", ".ts", ".mts", ".tsx"]);
const IGNORED_DIRECTORIES = new Set(["dist", "node_modules", "out"]);
const baseline = JSON.parse(
  await readFile("config/file-line-limit-baseline.json", "utf8"),
);

async function collectSourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    if (entry.isDirectory() && IGNORED_DIRECTORIES.has(entry.name)) continue;
    const entryPath = path.join(directory, entry.name);
    if (entryPath.replaceAll("\\", "/") === "apps/desktop/resources/crawl4ai") continue;
    if (entry.isDirectory()) {
      files.push(...(await collectSourceFiles(entryPath)));
    } else if (
      entry.isFile() &&
      !entry.name.endsWith(".d.ts") &&
      SOURCE_EXTENSIONS.has(path.extname(entry.name))
    ) {
      files.push(entryPath);
    }
  }
  return files;
}

function countLines(content) {
  if (!content) return 0;
  return content.endsWith("\n")
    ? content.split("\n").length - 1
    : content.split("\n").length;
}

const files = (await Promise.all(ROOTS.map(collectSourceFiles))).flat();
const violations = [];
const inventory = [];
const lineCounts = new Map();
for (const file of files) {
  const normalizedPath = file.split(path.sep).join("/");
  const lines = countLines(await readFile(file, "utf8"));
  lineCounts.set(normalizedPath, lines);
  if (lines >= REPORT_THRESHOLD)
    inventory.push({ file: normalizedPath, lines });
  const allowedLegacyLines = baseline[normalizedPath];
  const exceedsHardLimit = lines > HARD_LIMIT;
  const exceedsPreferredLimit =
    !Number.isInteger(allowedLegacyLines) && lines > PREFERRED_LIMIT;
  const exceedsLegacyBaseline =
    Number.isInteger(allowedLegacyLines) && lines > allowedLegacyLines;
  if (exceedsHardLimit || exceedsPreferredLimit || exceedsLegacyBaseline) {
    violations.push({
      file: normalizedPath,
      lines,
      allowedLegacyLines,
      exceedsHardLimit,
    });
  }
}

const staleBaselineEntries = [];
for (const entryPath of Object.keys(baseline)) {
  const actualLines = lineCounts.get(entryPath);
  if (actualLines === undefined) {
    staleBaselineEntries.push(`${entryPath}: 文件已不存在`);
  } else if (actualLines <= PREFERRED_LIMIT) {
    staleBaselineEntries.push(
      `${entryPath}: 已降到 ${actualLines} 行（<= ${PREFERRED_LIMIT}），豁免可以删除`,
    );
  }
}

if (staleBaselineEntries.length > 0) {
  console.error(
    "config/file-line-limit-baseline.json 存在失效豁免，请删除以下条目:",
  );
  for (const entry of staleBaselineEntries) {
    console.error(`- ${entry}`);
  }
  process.exitCode = 1;
}

if (violations.length > 0) {
  console.error(`File line limit exceeded:`);
  for (const violation of violations) {
    const limits = [
      violation.exceedsHardLimit && `hard limit ${HARD_LIMIT}`,
      violation.allowedLegacyLines
        ? `legacy baseline ${violation.allowedLegacyLines}`
        : `preferred limit ${PREFERRED_LIMIT}`,
    ].filter(Boolean);
    console.error(`- ${violation.file}: ${violation.lines}; ${limits.join(", ")}`);
  }
  process.exitCode = 1;
} else if (staleBaselineEntries.length === 0) {
  console.log(
    `File line limit passed: new files <= ${PREFERRED_LIMIT}; legacy files did not grow; hard limit ${HARD_LIMIT}.`,
  );
}

if (process.argv.includes("--report")) {
  console.log(`\nFiles at or above ${REPORT_THRESHOLD} lines:`);
  for (const entry of inventory.sort(
    (left, right) => right.lines - left.lines,
  )) {
    console.log(`- ${entry.lines.toString().padStart(5)} ${entry.file}`);
  }
}
