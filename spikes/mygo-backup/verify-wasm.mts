// 新版（Go + modernc VACUUM INTO）生成的备份，旧版（node-sqlite3-wasm）能否恢复并继续使用。
// 用旧版真实代码：backup.ts 的 validateBackupFile（恢复前校验）与 packages/db 的 initDatabase（恢复后打开）。
// 用法：pnpm dlx tsx spikes/mygo-backup/verify-wasm.mts <备份文件> <输出的回写副本路径>
import fs from "node:fs";
import Module from "node:module";
import os from "node:os";
import path from "node:path";

const [backup, writtenCopy] = process.argv.slice(2);
if (!backup || !writtenCopy) throw new Error("用法: verify-wasm.mts <备份文件> <回写副本路径>");

// 隔离：不读真实用户目录（backup.ts 间接依赖 runtime-paths / diagnostic-log）。
const isolated = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-backup-wasm-"));
process.env.APPDATA = isolated;
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy({ app: { getPath: () => isolated, isPackaged: false } } as Record<string | symbol, unknown>, {
  get: (t, p) => (p in t ? t[p] : noop),
});
const loader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = loader._load;
loader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "electron") return electronStub;
  if (request === "virtual:reading-libraries") return { __esModule: true, default: { compiler: "", animation: "", components: {}, runtime: {} } }; // vite 虚拟模块，备份校验用不到
  return originalLoad.call(this, request, ...rest);
};

function report(name: string, ok: boolean, detail = "") {
  console.log(`${ok ? "[ OK ]" : "[FAIL]"} ${name.padEnd(34)} ${detail}`);
  if (!ok) process.exitCode = 1;
}

const { validateBackupFile } = await import("../../apps/desktop/src/main/services/backup.ts");
const { initDatabase, closeDatabase, KnowledgeItemDB } = await import("../../packages/db/src/index.ts");
const { segmentTextForFts } = await import("../../packages/db/src/fts.ts");

// 1) 恢复前校验：旧版真实函数。
const v = validateBackupFile(backup);
report("旧版 validateBackupFile", v.ok, v.error ?? "");

// 2) 恢复后打开：旧版真实的 initDatabase（含迁移执行器），在副本上进行，不动原备份。
fs.copyFileSync(backup, writtenCopy);
const database = initDatabase(writtenCopy);
const count = () => (database.get("SELECT count(*) AS c FROM knowledge_items") as { c: number }).c;
const before = count();
report("旧版 initDatabase 打开并读条目数", before > 0, `items=${before}`);

// 3) 旧版全文检索在备份上可用（中文预分词）。
const phrase = `"${segmentTextForFts("检索").trim()}"`;
const hits = database.all("SELECT item_id FROM knowledge_fts WHERE knowledge_fts MATCH ?", [phrase]);
report("旧版 FTS 中文检索", hits.length > 0, `match ${phrase} -> ${hits.length} 条`);

// 4) 旧版继续写入（含 FTS 同步），随后关库。
const items = new KnowledgeItemDB(database);
items.create({ title: "恢复后新增的笔记", content: "旧版在新版备份上继续写入。", tagNames: ["恢复"] } as never);
report("旧版在备份上继续写入", count() === before + 1, `items=${count()}`);
closeDatabase();
console.log("回写副本：" + writtenCopy);
