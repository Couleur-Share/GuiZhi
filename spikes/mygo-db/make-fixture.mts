// 用归知自己的 @guizhi/db（node-sqlite3-wasm）生成一份隔离的测试库，
// 并输出 TS 侧的检索结果（id 顺序 + bm25 值）作为 Go 侧比对基准。
// 只写 --out 指定的临时目录，不触碰用户数据目录。
import fs from "node:fs";
import path from "node:path";
import {
  initDatabase,
  closeDatabase,
  KnowledgeItemDB,
} from "../../packages/db/src/index.ts";
import { buildFtsMatchQuery } from "../../packages/db/src/fts.ts";

const out = process.argv[2];
if (!out) throw new Error("用法: make-fixture.mts <输出目录>");
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
const dbPath = path.join(out, "knowledge.db");

const database = initDatabase(dbPath);
const items = new KnowledgeItemDB(database);
const docs = [
  ["归知是本地优先的个人知识库", "采集、整理、检索、问答构成核心闭环。全文检索使用 FTS5。", ["知识库", "本地"]],
  ["Electron 与 WebView2 的取舍", "Electron 自带 Chromium，安装包约 112 MiB；WebView2 复用系统运行时。", ["桌面", "框架"]],
  ["中文分词在 SQLite FTS5 里的做法", "unicode61 不切分 CJK，所以写入前在每个汉字两侧插入空格。", ["检索", "FTS5"]],
  ["MyGo 桌面框架评估", "用 Go 写桌面应用，页面通过 WebView2 渲染，支持持久会话与 cookie。", ["MyGo", "Go"]],
  ["English only note about backups", "Backups use VACUUM INTO and restore through the database adapter.", ["backup"]],
];
for (const [title, content, tagNames] of docs) {
  items.create({ title, content, tagNames } as never);
}

const queries = ["知识库", "全文检索", "桌面 框架", "中文分词", "VACUUM", "归知 检索"];
const expected: Record<string, unknown> = {};
for (const q of queries) {
  const match = buildFtsMatchQuery(q, "phrase");
  if (!match) continue;
  const rows = database
    .all(
      `SELECT item_id, bm25(knowledge_fts, 0.0, 10.0, 1.0, 5.0) AS r
       FROM knowledge_fts WHERE knowledge_fts MATCH ? ORDER BY r ASC, item_id ASC`,
      [match],
    )
    .map((row: any) => ({ id: row.item_id, rank: row.r }));
  expected[q] = { match, rows };
}
const meta = {
  userVersion: database.get("PRAGMA user_version")?.user_version,
  journalMode: database.get("PRAGMA journal_mode")?.journal_mode,
  itemCount: database.get("SELECT COUNT(*) AS c FROM knowledge_items")?.c,
};
closeDatabase();
fs.writeFileSync(path.join(out, "expected.json"), JSON.stringify({ meta, expected }, null, 2));
console.log("fixture ok", dbPath, JSON.stringify(meta));
console.log(fs.readdirSync(out).join(", "));
