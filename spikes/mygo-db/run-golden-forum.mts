// 只生成论坛连接器基线：用法 run-golden-forum.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）
import fs from "node:fs";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-forum.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });
const { forumGolden } = await import("./golden-forum.mts");
fs.writeFileSync(path.join(out, "golden-forum.json"), JSON.stringify(forumGolden, null, 1));
console.log("ok", path.join(out, "golden-forum.json"), "recs", forumGolden.recs.length);
