// 生成主题阅读页数据层基线：用法（仓库根）pnpm dlx tsx spikes/mygo-db/run-golden-themed-db.mts <输出目录>
import fs from "node:fs";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-themed-db.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });
const { buildThemedDbGolden } = await import("./themed-db-cases.mts");
const g = await buildThemedDbGolden();
fs.writeFileSync(path.join(out, "golden-themed-db.json"), JSON.stringify(g, null, 1));
console.log("ok", path.join(out, "golden-themed-db.json"), "recs", g.recs.length);
