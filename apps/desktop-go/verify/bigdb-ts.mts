// TS 版（@guizhi/db，node-sqlite3-wasm）在同样数据上的耗时，用来判断某个慢点是 Go 移植引入的还是产品本来就有。
// 只读写 --work 指定的临时副本，不触碰用户数据目录。
//   用法：tsx bigdb-ts.mts backfill <工作目录> <条数列表，逗号分隔>   # 补齐查询随条数的增长
//         tsx bigdb-ts.mts big <大库 knowledge.db 路径> <工作目录>      # 大库上的 counts/list/搜索
import fs from "node:fs";
import path from "node:path";
import { initDatabase, closeDatabase, KnowledgeItemDB } from "../../../packages/db/src/index.ts";

const [mode, a, b] = process.argv.slice(2);
const ms = (t: number) => Math.round(performance.now() - t);

if (mode === "backfill") {
  const dir = a;
  for (const n of (b ?? "4000,8000").split(",").map(Number)) {
    fs.rmSync(dir, { recursive: true, force: true });
    fs.mkdirSync(dir, { recursive: true });
    const db = initDatabase(path.join(dir, "knowledge.db"));
    const items = new KnowledgeItemDB(db);
    const t0 = performance.now();
    for (let i = 0; i < n; i++) items.create({ title: `样本标题 ${i}`, content: `样本内容 hello world ${i} 知识库 检索 备份` } as never);
    const tCreate = ms(t0);
    const t1 = performance.now();
    const filled = items.backfillMissingFtsRows();
    console.log(JSON.stringify({ n, createMs: tCreate, backfillMs: ms(t1), filled }));
    closeDatabase();
  }
} else if (mode === "big") {
  const src = a, dir = b;
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  const work = path.join(dir, "knowledge.db");
  fs.copyFileSync(src, work);
  const t: Record<string, number> = {};
  let t0 = performance.now();
  const db = initDatabase(work);
  t.initDatabase = ms(t0);
  const items = new KnowledgeItemDB(db);
  const time = <T,>(k: string, f: () => T): T => { const s = performance.now(); const v = f(); t[k] = ms(s); return v; };
  time("counts", () => items.counts());
  const p1 = time("listFirstPage", () => items.list({ scope: "all", limit: 50 } as never)) as any;
  time("listNextPage", () => items.list({ scope: "all", limit: 50, cursor: p1.nextCursor } as never));
  time("searchRare", () => items.list({ scope: "all", search: "ZXQVBIG10000", limit: 50 } as never));
  time("searchCommon", () => items.list({ scope: "all", search: "SQLite", limit: 50 } as never));
  time("searchCommonZh", () => items.list({ scope: "all", search: "知识库 检索", limit: 50 } as never));
  time("facetCountsWithSearch", () => items.counts({ scope: "all", search: "SQLite" } as never));
  const created = time("createOne", () => items.create({ title: "大库写入探针", content: "probe zzbigprobe" } as never)) as any;
  time("createSecond", () => items.create({ title: "大库写入探针2", content: "probe zzbigprobe2" } as never));
  time("trashOne", () => items.moveToTrash([created.id]));
  // 不测 backfillMissingFtsRows：它在大库上是 O(n²)（见 backfill 模式的 4000/8000 条实测），会跑很久。
  console.log(JSON.stringify(t));
  closeDatabase();
}
