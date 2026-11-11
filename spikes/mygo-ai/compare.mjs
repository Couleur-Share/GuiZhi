// 比较 TS 基线与 Go 版结果（忽略 info：块数与耗时只作参考）。
// 用法：node compare.mjs <ts-out.json> <go-out.json>
import fs from "node:fs";
const [a, b] = process.argv.slice(2).map((f) => JSON.parse(fs.readFileSync(f, "utf8")));
const keys = [...new Set([...Object.keys(a), ...Object.keys(b)])];
let diff = 0;
for (const k of keys) {
  const x = { ...a[k] }, y = { ...b[k] };
  delete x.info; delete y.info;
  const fields = [...new Set([...Object.keys(x), ...Object.keys(y)])].filter((f) => JSON.stringify(x[f]) !== JSON.stringify(y[f]));
  if (!a[k] || !b[k]) { console.log(`✗ ${k}: 只在一侧出现`); diff++; continue; }
  if (fields.length) {
    diff++;
    console.log(`✗ ${k}`);
    for (const f of fields) console.log(`    ${f}: TS=${JSON.stringify(x[f])}  Go=${JSON.stringify(y[f])}`);
  }
}
const ms = (o) => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, v.info]));
console.log(`\n${keys.length} 个场景，${diff} 个不一致`);
