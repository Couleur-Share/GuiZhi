// 只生成原文快照净化基线：用法 run-golden-websnap.mts <输出目录>（需从 D:\Project\GuiZhi 目录运行 tsx）
import fs from "node:fs";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-websnap.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });
const { buildWebSnapGolden } = await import("./golden-websnap.mts");
const golden = await buildWebSnapGolden();
fs.writeFileSync(path.join(out, "golden-websnap.json"), JSON.stringify(golden, null, 1));
console.log("ok", path.join(out, "golden-websnap.json"), "html", golden.html.length, "inline", golden.inline.length, "sheet", golden.sheet.length);
