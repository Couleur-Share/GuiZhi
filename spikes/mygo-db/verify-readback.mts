// 用 node-sqlite3-wasm 读回 Go 侧写过的库：完整性、Go 写入的 FTS 行、归知自己的检索查询。
import path from "node:path";
import { createRequire } from "node:module";
import { buildFtsMatchQuery } from "../../packages/db/src/fts.ts";

const require = createRequire(path.resolve("packages/db/package.json"));
const { Database } = require("node-sqlite3-wasm");

const file = process.argv[2];
const db = new Database(file);
const qc = db.get("PRAGMA quick_check");
const ic = db.all("PRAGMA integrity_check");
const match = buildFtsMatchQuery("索引行", "phrase");
const rows = db.all(
  "SELECT item_id FROM knowledge_fts WHERE knowledge_fts MATCH ?",
  [match as string],
);
const total = db.get("SELECT COUNT(*) AS c FROM knowledge_fts")?.c;
console.log(JSON.stringify({ quick_check: qc, integrity: ic, match, goRowFound: rows, ftsRows: total }));
db.close();

