// 数据库初始化/迁移（init.ts + migrations.ts）的差分基准。
// 三种起点：fresh（空目录）、current（TS 已建好且带数据）、legacy（把 TS 建好的库改回各个历史形态再清空迁移记录，
// 逼着 39 条迁移全部重新执行）。每种起点记下 TS 跑完后的整库转储（结构 SQL + 全部表数据），Go 在同一份起点上跑完后逐项比较。
import fs from "node:fs";
import path from "node:path";

type Row = Record<string, unknown>;

export async function buildSchemaGolden(ctx: { out: string }): Promise<unknown> {
  const init = await import("../../packages/db/src/init.ts");
  const migrations = await import("../../packages/db/src/migrations.ts");
  const Database = (await import("../../packages/db/src/adapter.ts")).default as any;
  const root = path.join(ctx.out, "schema-golden");
  fs.rmSync(root, { recursive: true, force: true });
  fs.mkdirSync(root, { recursive: true });

  const val = (v: unknown): unknown => {
    if (v instanceof Uint8Array) return "hex:" + Buffer.from(v).toString("hex");
    if (typeof v === "bigint") return Number(v);
    return v;
  };
  const dump = (dbPath: string) => {
    const db = new Database(dbPath);
    try {
      const master = db.all("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY type, name") as Row[];
      const virtual = master.filter((r) => typeof r.sql === "string" && /^CREATE VIRTUAL TABLE/i.test(r.sql as string)).map((r) => r.name as string);
      const shadow = new Set(virtual.flatMap((v) => ["data", "idx", "content", "docsize", "config"].map((s) => `${v}_${s}`)));
      const schema = master.filter((r) => !shadow.has(r.name as string)).map((r) => [r.type, r.name, r.tbl_name, r.sql ?? null]);
      const data: Record<string, { columns: string[]; rows: unknown[][] }> = {};
      for (const r of master) {
        if (r.type !== "table" || shadow.has(r.name as string)) continue;
        const name = r.name as string;
        let rows: Row[];
        if (name === "schema_migrations") {
          rows = db.all("SELECT name FROM schema_migrations ORDER BY name") as Row[]; // applied_at 是时间戳，不比较
        } else {
          try {
            rows = db.all(`SELECT * FROM "${name}" ORDER BY rowid`) as Row[];
          } catch {
            rows = db.all(`SELECT * FROM "${name}"`) as Row[];
          }
        }
        const columns = (db.all(`PRAGMA table_info("${name}")`) as Row[]).map((c) => c.name as string);
        const cols = name === "schema_migrations" ? ["name"] : columns;
        data[name] = { columns: cols, rows: rows.map((row) => cols.map((c) => val(row[c]))) };
      }
      const uv = (db.get("PRAGMA user_version") as Row).user_version;
      return { userVersion: uv, schema, data };
    } finally {
      db.close();
    }
  };

  const ins = (db: any, table: string, row: Row) => {
    const keys = Object.keys(row);
    db.run(`INSERT INTO ${table} (${keys.join(",")}) VALUES (${keys.map(() => "?").join(",")})`, ...keys.map((k) => row[k]));
  };

  // ── 造数据：写进 TS 刚建好的当前结构里 ──
  const seed = (db: any) => {
    ins(db, "collections", { id: "c1", name: "合集", sort_order: 0, created_at: 1, updated_at: 1 });
    const item = (id: string, status: string) => ({ id, title: `标题-${id}`, content: `正文 ${id}`, item_type: "webpage", status, collection_id: id === "i1" ? "c1" : null, is_favorite: 0, is_pinned: 0, created_at: 10, updated_at: 20 });
    ins(db, "knowledge_items", item("i1", "active"));
    ins(db, "knowledge_items", item("i2", "archived"));
    ins(db, "knowledge_items", item("i3", "active"));
    const src = (id: string, itemId: string, type: string, uri: string | null, platform: string | null, at: number) => ({ id, item_id: itemId, source_type: type, source_uri: uri, normalized_uri: uri, platform, captured_at: at });
    ins(db, "source_records", src("s1", "i1", "url", "https://www.xiaohongshu.com/explore/abc", "old_dotnet", 1));
    ins(db, "source_records", src("s2", "i2", "url", "https://mp.weixin.qq.com/s/xyz", null, 2));
    ins(db, "source_records", src("s3", "i3", "text", null, null, 3));
    ins(db, "source_records", src("s4", "i3", "url", "https://example.com/a", "web", 4));
    ins(db, "import_tasks", { id: "t1", source_kind: "url", source_input: "https://x.test/a?token=1", display_name: "n", status: "completed", result_item_id: "i1", created_at: 5, updated_at: 6 });
    ins(db, "import_tasks", { id: "t2", source_kind: "url", source_input: "https://x.test/old", display_name: "n", status: "completed", result_item_id: "i1", created_at: 1, updated_at: 2 });
    ins(db, "source_comments", { id: "cm1", item_id: "i1", platform: "douyin", external_id: "e1", content: "评论", captured_at: 7 });
    ins(db, "ask_sessions", { id: "a1", title: "会话 一", messages_json: JSON.stringify([{ question: "你好 世界", answer: "回答 内容" }, null, { question: 5 }]), created_at: 1, updated_at: 2 });
    ins(db, "ask_sessions", { id: "a2", title: "坏消息", messages_json: "{bad", created_at: 1, updated_at: 3 });
    ins(db, "research_runs", { id: "r1", topic: "话题", day_range: 7, range_from: 1, range_to: 2, depth: "quick", sources_json: "[]", status: "ready", created_at: 1, updated_at: 2 });
    ins(db, "research_source_runs", { run_id: "r1", source: "douyin", status: "succeeded", method: "m", collected_count: 2 });
    ins(db, "research_candidates", { id: "rc1", run_id: "r1", source: "xiaohongshu", external_id: "x", url: "u", normalized_url: "u1", title: "t", media_type: "article", discovery_method: "d", created_at: 1, updated_at: 1 });
    ins(db, "platform_discovery_views", { id: "v1", name: "视图1", platform: "douyin", mode: "keyword", query: "q", created_at: 1, updated_at: 1 });
    ins(db, "platform_discovery_views", { id: "v2", name: "视图2", platform: "douyin", mode: "keyword", query: "q", created_at: 1, updated_at: 1 });
  };

  // ── 把当前结构改回历史形态 ──
  const rebuild = (db: any, table: string, ddl: string | ((cur: string) => string), fixRow?: (row: Row) => Row) => {
    const cur = (db.get("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table) as { sql: string }).sql;
    const next = typeof ddl === "function" ? ddl(cur) : ddl;
    if (next === cur) throw new Error(`${table} 的历史形态变换没有生效`);
    const idx = (db.all("SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL", table) as { sql: string }[]).map((r) => r.sql);
    const rows = db.all(`SELECT * FROM ${table}`) as Row[];
    db.exec(`DROP TABLE ${table}`);
    db.exec(next);
    const cols = new Set((db.all(`PRAGMA table_info(${table})`) as Row[]).map((c) => c.name as string));
    for (const row of rows) {
      const kept: Row = {};
      for (const [k, v] of Object.entries(fixRow ? fixRow(row) : row)) if (cols.has(k)) kept[k] = v;
      ins(db, table, kept);
    }
    for (const sql of idx) {
      try {
        db.exec(sql);
      } catch {
        /* 索引引用了历史形态里不存在的列（如 idx_ask_sessions_article）：历史库本来就没有它 */
      }
    }
  };
  const OLD_KNOWLEDGE_ITEMS = `CREATE TABLE knowledge_items (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  summary TEXT,
  transcript TEXT,
  item_type TEXT NOT NULL DEFAULT 'note'
    CHECK(item_type IN ('note','webpage','video','image','audio','document','snippet')),
  status TEXT NOT NULL DEFAULT 'inbox'
    CHECK(status IN ('inbox','ready','archived')),
  collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
  is_favorite INTEGER NOT NULL DEFAULT 0,
  is_pinned INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
)`;
  const legacyize = (db: any) => {
    db.pragma("foreign_keys = OFF");
    // knowledge_items：三态 + 无 forum，行内状态改回 inbox/ready
    rebuild(db, "knowledge_items", OLD_KNOWLEDGE_ITEMS, (row) => ({ ...row, status: row.id === "i1" ? "inbox" : row.id === "i3" ? "ready" : row.status }));
    rebuild(db, "source_comments", (s) => s.replace(",'linuxdo'", ""));
    rebuild(db, "research_runs", (s) => s.replace(/ {2}time_scope TEXT NOT NULL DEFAULT 'recent' CHECK\(time_scope IN \('recent','all'\)\),\r?\n/, ""));
    rebuild(db, "research_source_runs", (s) => s.replace(",'web'", ""));
    rebuild(db, "research_candidates", (s) => s.replace(",'web'", ""));
    rebuild(db, "source_records", (s) => s.replace(/ {2}access_uri TEXT,\r?\n/, ""));
    rebuild(db, "ask_sessions", (s) =>
      s
        .replace(/ {4}scope TEXT NOT NULL DEFAULT 'knowledge',\r?\n/, "")
        .replace(/ {4}item_id TEXT,\r?\n/, "")
        .replace(/ {4}article_title TEXT,\r?\n/, "")
        .replace(/ {4}options_json TEXT NOT NULL DEFAULT '\{\}',\r?\n/, ""),
    );
    // 老库没有这些：由迁移补建
    for (const t of ["ask_session_meta", "ask_session_fts"]) db.exec(`DROP TABLE IF EXISTS ${t}`);
    for (const r of db.all("SELECT name FROM sqlite_master WHERE type='trigger'") as { name: string }[]) db.exec(`DROP TRIGGER IF EXISTS "${r.name}"`);
    db.exec("DELETE FROM schema_migrations");
    db.exec("PRAGMA user_version = 0");
  };
  // 只剩 0022 及之后待执行：0020 会在去重前重建唯一索引，所以重复数据只能存在于「0020 已执行、0022 未执行」的库里
  const legacyLate = (db: any) => {
    db.exec("DROP INDEX IF EXISTS idx_discovery_candidates_external");
    ins(db, "platform_discovery_candidates", { view_id: "v1", platform: "douyin", external_id: "ext", item_json: "{}", first_seen_at: 1, last_seen_at: 1 });
    ins(db, "platform_discovery_candidates", { view_id: "v2", platform: "douyin", external_id: "ext", item_json: "{}", first_seen_at: 2, last_seen_at: 2 });
    ins(db, "platform_discovery_candidates", { view_id: "v1", platform: "douyin", external_id: "solo", item_json: "{}", first_seen_at: 3, last_seen_at: 3 });
    db.exec("DELETE FROM schema_migrations WHERE name >= '0022'");
    db.exec("PRAGMA user_version = 21");
  };

  const cases: Record<string, unknown> = {};
  const newDb = (file: string) => {
    fs.rmSync(file, { force: true });
    init.initDatabase(file);
    init.closeDatabase();
  };
  const runInit = (pre: string | null, name: string) => {
    const work = path.join(root, `${name}.work.db`);
    for (const suffix of ["", ".clean", ".lock"]) fs.rmSync(work + suffix, { recursive: true, force: true });
    if (pre) fs.copyFileSync(pre, work);
    init.initDatabase(work);
    init.closeDatabase();
    const result = dump(work);
    for (const suffix of ["", ".clean", ".clients"]) fs.rmSync(work + suffix, { recursive: true, force: true });
    return result;
  };

  cases.fresh = { pre: null, post: runInit(null, "fresh") };

  const currentPre = path.join(root, "current.pre.db");
  newDb(currentPre);
  {
    const db = new Database(currentPre);
    seed(db);
    db.close();
  }
  cases.current = { pre: "current.pre.db", post: runInit(currentPre, "current") };

  const legacyPre = path.join(root, "legacy.pre.db");
  newDb(legacyPre);
  {
    const db = new Database(legacyPre);
    seed(db);
    legacyize(db);
    db.close();
  }
  cases.legacy = { pre: "legacy.pre.db", post: runInit(legacyPre, "legacy") };

  const latePre = path.join(root, "legacy-late.pre.db");
  newDb(latePre);
  {
    const db = new Database(latePre);
    seed(db);
    legacyLate(db);
    db.close();
  }
  cases["legacy-late"] = { pre: "legacy-late.pre.db", post: runInit(latePre, "legacy-late") };

  return { cases, migrationNames: migrations.MIGRATIONS.map((m: any) => m.name), schemaVersion: migrations.SCHEMA_VERSION };
}
