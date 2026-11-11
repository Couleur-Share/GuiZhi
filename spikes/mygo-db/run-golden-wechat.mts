// 只生成公众号采集基线：用法 run-golden-wechat.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）
import fs from "node:fs";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-wechat.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });
const { buildWechatGolden } = await import("./golden-wechat.mts");
const golden = await buildWechatGolden();
fs.writeFileSync(path.join(out, "golden-wechat.json"), JSON.stringify(golden, null, 1));
console.log("ok", path.join(out, "golden-wechat.json"), "cases", golden.cases.length);
