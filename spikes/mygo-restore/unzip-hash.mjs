// 解开 zip，输出 {条目名: sha256}，用来比较两个便携包的内容。用法：node unzip-hash.mjs <zip> <out.json>
import fs from "node:fs";
import crypto from "node:crypto";
import { createRequire } from "node:module";
const require = createRequire(new URL("../../apps/desktop/package.json", import.meta.url));
const { unzipSync } = require("fflate");
const files = unzipSync(new Uint8Array(fs.readFileSync(process.argv[2])));
const out = {};
for (const [name, data] of Object.entries(files)) out[name] = crypto.createHash("sha256").update(data).digest("hex");
fs.writeFileSync(process.argv[3], JSON.stringify(out, null, 1));
