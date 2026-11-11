// 深比较两个 JSON 文件，可忽略若干键（逗号分隔，任意层级）。不一致时打印路径并以非零退出。
// 用法：node cmp.mjs <a.json> <b.json> [忽略的键,...]
import fs from "node:fs";
const [fa, fb, ignoreArg] = process.argv.slice(2);
const ignore = new Set((ignoreArg ?? "").split(",").filter(Boolean));
const a = JSON.parse(fs.readFileSync(fa, "utf8")), b = JSON.parse(fs.readFileSync(fb, "utf8"));
let diffs = 0;
const walk = (x, y, p) => {
  if (diffs > 20) return;
  if (x && y && typeof x === "object" && typeof y === "object") {
    for (const k of new Set([...Object.keys(x), ...Object.keys(y)])) if (!ignore.has(k)) walk(x[k], y[k], `${p}.${k}`);
  } else if (JSON.stringify(x) !== JSON.stringify(y)) { diffs++; console.log(`  ${p}: A=${JSON.stringify(x)}  B=${JSON.stringify(y)}`); }
};
walk(a, b, "$");
process.exit(diffs ? 1 : 0);
