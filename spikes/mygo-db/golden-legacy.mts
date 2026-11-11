// 旧版 .NET 归知数据库迁移（migration:detectLegacy / migration:runLegacy）的差分基准：
// 真实的 migration.ipc.ts + packages/db/src/legacy-migration.ts，旧库由本脚本按 EF Core 的表结构造出。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

export interface LegacyGoldenCtx {
  ipcHandlers: Map<string, (...args: unknown[]) => unknown>;
  userData: string;
  out: string;
  initDatabase: (p: string) => any;
  closeDatabase: () => void;
}

const LEGACY_SCHEMA = `
CREATE TABLE Collections (Id TEXT PRIMARY KEY, Name TEXT, Icon TEXT, SortOrder INTEGER, IsDeleted INTEGER, CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
CREATE TABLE Tags (Id TEXT PRIMARY KEY, Name TEXT, ColorKey INTEGER, CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
CREATE TABLE SourceRecords (Id TEXT PRIMARY KEY, SourceType INTEGER, OriginalUri TEXT, CanonicalUri TEXT, ContentHash TEXT, Platform TEXT, CapturedAtUtc TEXT);
CREATE TABLE KnowledgeItems (Id TEXT PRIMARY KEY, Title TEXT, Content TEXT, Summary TEXT, Transcript TEXT, ItemType INTEGER, Status INTEGER, CollectionId TEXT, SourceId TEXT, IsFavorite INTEGER, IsPinned INTEGER, IsDeleted INTEGER, DeletedAtUtc TEXT, CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
CREATE TABLE KnowledgeItemTags (KnowledgeItemId TEXT, TagId TEXT);
`;
const WIKI_SCHEMA = `
CREATE TABLE WikiPages (Id TEXT PRIMARY KEY, Title TEXT, NormalizedTitle TEXT, Kind INTEGER, Summary TEXT, Body TEXT, AliasesJson TEXT, Provider TEXT, Model TEXT, PromptVersion TEXT, GeneratedAtUtc TEXT, CreatedAtUtc TEXT, UpdatedAtUtc TEXT);
CREATE TABLE WikiPageLinks (FromPageId TEXT, ToPageId TEXT, CreatedAtUtc TEXT);
CREATE TABLE WikiPageSources (WikiPageId TEXT, KnowledgeItemId TEXT, CreatedAtUtc TEXT);
CREATE TABLE WikiIngestions (KnowledgeItemId TEXT PRIMARY KEY, ContentHash TEXT, Model TEXT, PromptVersion TEXT, UpdatedAtUtc TEXT);
`;

const T1 = "2026-07-19 16:35:19.7184038";
const T2 = "2026-07-20 01:02:03";
const T3 = "2025-12-31T23:59:59.5+08:00";

const FULL_DATA = `
INSERT INTO Collections VALUES ('c1','研究','📚',2,0,'${T1}','${T2}'),('c2','旧的',NULL,1,0,'${T2}','${T2}'),('c3','已删除集合','x',0,1,'${T1}','${T1}'),('c4',NULL,NULL,NULL,0,'坏时间','');
INSERT INTO Tags VALUES ('t9','zeta',0,'${T1}','${T1}'),('t1','alpha',1,'${T1}','${T1}'),('t5','中文标签',2,'${T2}','${T2}'),('t3','blue',5,'${T1}','${T1}'),('t4','gray-oob',6,'${T1}','${T1}'),('t6','neg',-1,'${T1}','${T1}'),('t7','frac',1.5,'${T1}','${T1}'),('t8',NULL,3,NULL,NULL);
INSERT INTO SourceRecords VALUES ('s1',3,'https://a.example/x','https://a.example/x?c=1','h1','web','${T1}'),('s2',4,'https://v.example/y',NULL,NULL,'bilibili','${T2}'),('s3',2,'C:\\\\f.pdf',NULL,'h3',NULL,'${T3}'),('s4',0,NULL,NULL,NULL,NULL,NULL),('s5',1,'t',NULL,NULL,NULL,'${T1}'),('s6',9,'u',NULL,NULL,NULL,'${T1}'),('sx',3,'orphan',NULL,NULL,NULL,'${T1}');
INSERT INTO KnowledgeItems VALUES
 ('i1','第一篇 hello','正文 content 一',  '摘要',NULL,1,2,'c1','s1',1,0,0,NULL,'${T1}','${T2}'),
 ('i2','Second','body two',NULL,'转写文本',2,3,'c3','s2',0,1,0,NULL,'${T2}','${T2}'),
 ('i3','Trashed','gone',NULL,NULL,0,0,'c2','s3',0,0,1,'${T2}','${T1}','${T1}'),
 ('i4',NULL,NULL,NULL,NULL,99,4,NULL,'s4',NULL,NULL,0,NULL,NULL,NULL),
 ('i5','Types','x',NULL,NULL,6,1,'nope','s5',2,'a',0,NULL,'${T3}','${T3}'),
 ('i6','Deleted no ts','y',NULL,NULL,'3','3',NULL,'s6',1,1,1,NULL,'${T1}','${T1}'),
 ('i7','No source','z',NULL,NULL,4,2,'c4',NULL,0,0,0,NULL,'${T1}','${T1}'),
 ('i8','tags many','multi',NULL,NULL,3,0,NULL,NULL,0,0,0,NULL,'${T1}','${T1}');
INSERT INTO KnowledgeItemTags VALUES ('i1','t9'),('i1','t1'),('i1','t5'),('i1','t1'),('i2','t3'),('i2','ghost'),('ghost','t1'),('i8','t9'),('i8','t8'),('i8','t3'),('i8','t1'),('i3','t4'),('i4','t6'),('i5','t7');
`;
const FULL_WIKI = `
INSERT INTO WikiPages VALUES ('w1','主题页','主题页',0,'摘要','正文','["别名"]','p','m','v1','${T1}','${T1}','${T2}'),('w2','Entity','entity',1,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL),('w3','Concept','concept',2,'s','b',NULL,'p','m','v2','${T2}','${T2}','${T2}'),('w4','Odd','odd',7,'s','b',NULL,'p','m','v2','${T2}','${T2}','${T2}');
INSERT INTO WikiPageLinks VALUES ('w1','w2','${T1}'),('w1','w2','${T2}'),('w2','w3',NULL),('w1','ghost','${T1}'),('ghost','w1','${T1}');
INSERT INTO WikiPageSources VALUES ('w1','i1','${T1}'),('w1','i1','${T1}'),('w2','i2','${T2}'),('w2','ghost','${T1}'),('ghost','i1','${T1}');
INSERT INTO WikiIngestions VALUES ('i1','hash1','m','v1','${T1}'),('i2',NULL,NULL,NULL,NULL),('ghost','h','m','v','${T1}');
`;

const splitSql = (blocks: string[]): string[] => blocks.flatMap((b) => b.split(/;\s*\n|;\s*$/).map((s) => s.trim()).filter(Boolean));

const DUMP: Record<string, string> = {
  collections: "SELECT * FROM collections ORDER BY rowid",
  tags: "SELECT * FROM tags ORDER BY rowid",
  items: "SELECT * FROM knowledge_items ORDER BY rowid",
  itemTags: "SELECT * FROM knowledge_item_tags ORDER BY rowid",
  sources: "SELECT * FROM source_records ORDER BY rowid",
  fts: "SELECT rowid, item_id, title, content, tags FROM knowledge_fts ORDER BY rowid",
  ftsMap: "SELECT * FROM knowledge_fts_map ORDER BY item_id",
  wikiPages: "SELECT * FROM wiki_pages ORDER BY rowid",
  wikiLinks: "SELECT * FROM wiki_page_links ORDER BY rowid",
  wikiSources: "SELECT * FROM wiki_page_sources ORDER BY rowid",
  wikiIngestions: "SELECT * FROM wiki_ingestions ORDER BY rowid",
};

interface LegacyCase {
  name: string;
  /** 旧库：schema + 数据；null 表示不放旧库文件 */
  legacy: { sql: string[]; wiki?: boolean; header?: "wal" } | null;
  /** 目标库预置：往 knowledge_items 里塞一条，使其非空 */
  targetNotEmpty?: boolean;
  walResidue?: boolean;
  ops: Array<{ op: "detect" } | { op: "run"; path?: "none" | "explicit" | "missing" | "empty" | "other-nolegacy" }>;
}

export async function buildLegacyGolden(ctx: LegacyGoldenCtx): Promise<unknown> {
  const dataDir = path.join(ctx.userData, "data");
  const legacyFile = path.join(dataDir, "guizhi.db");
  const extraDir = path.join(ctx.out, "legacy-extra");
  const explicitFile = path.join(extraDir, "other-legacy.db");
  const notLegacyFile = path.join(extraDir, "not-legacy.db");
  const tmpPattern = /guizhi-legacy-migration-\d+\.db/;
  const normPath = (s: string) => s.split(legacyFile).join("<LEGACY>").split(explicitFile).join("<EXPLICIT>").split(notLegacyFile).join("<NOTLEGACY>").split(extraDir).join("<EXTRA>").split(ctx.userData).join("<UD>").replace(/\\/g, "/");



  const { default: AdapterDatabase } = await import("../../packages/db/src/adapter.ts");
  const writeLegacy = (file: string, spec: NonNullable<LegacyCase["legacy"]>) => {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.rmSync(file, { force: true });
    const db = new (AdapterDatabase as any)(file);
    db.exec(LEGACY_SCHEMA);
    if (spec.wiki !== false) db.exec(WIKI_SCHEMA);
    for (const sql of splitSql(spec.sql)) db.exec(sql);
    db.close();
    if (spec.header === "wal") {
      const fd = fs.openSync(file, "r+");
      fs.writeSync(fd, Buffer.from([2, 2]), 0, 2, 18);
      fs.closeSync(fd);
    }
  };

  const dumpTarget = (db: any): Record<string, unknown[][]> => {
    const d: Record<string, unknown[][]> = {};
    for (const [name, sql] of Object.entries(DUMP)) d[name] = (db.all(sql) as Array<Record<string, unknown>>).map((r) => Object.values(r));
    return d;
  };

  const cases: LegacyCase[] = [
    { name: "full", legacy: { sql: [FULL_DATA, FULL_WIKI] }, ops: [{ op: "detect" }, { op: "run" }, { op: "detect" }, { op: "run" }] },
    { name: "no-wiki-tables", legacy: { sql: [FULL_DATA], wiki: false }, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "wal-header", legacy: { sql: [FULL_DATA, FULL_WIKI], header: "wal" }, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "empty-legacy", legacy: { sql: [] }, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "only-deleted", legacy: { sql: [`INSERT INTO KnowledgeItems VALUES ('d1','t','c',NULL,NULL,0,0,NULL,NULL,0,0,1,'${T1}','${T1}','${T1}');`] }, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "target-not-empty", legacy: { sql: [FULL_DATA] }, targetNotEmpty: true, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "no-legacy-file", legacy: null, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "explicit-path", legacy: null, ops: [{ op: "run", path: "explicit" }] },
    { name: "explicit-missing", legacy: null, ops: [{ op: "run", path: "missing" }] },
    { name: "explicit-empty-string-falls-back", legacy: { sql: [FULL_DATA] }, ops: [{ op: "run", path: "empty" }] },
    { name: "explicit-not-legacy", legacy: null, ops: [{ op: "run", path: "other-nolegacy" }] },
    { name: "wal-residue", legacy: { sql: [FULL_DATA] }, walResidue: true, ops: [{ op: "detect" }, { op: "run" }] },
    { name: "unique-violation-rolls-back", legacy: { sql: [FULL_DATA, "INSERT INTO WikiPages VALUES ('a','t','same',0,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)", "INSERT INTO WikiPages VALUES ('b','t2','same',0,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)"] }, ops: [{ op: "run" }, { op: "detect" }] },
  ];

  // 空库（仅 schema）留给 Go 侧测试复制使用
  {
    const emptyDb = ctx.initDatabase(path.join(ctx.out, "empty-schema.db"));
    void emptyDb;
    ctx.closeDatabase();
  }

  const runStart = Date.now();
  const now = (v: unknown) => (typeof v === "number" && v >= runStart - 5000 && v <= Date.now() + 5000 ? "NOW" : v);
  const normRows = (rows: unknown[][]) => rows.map((r) => r.map(now));
  const out: unknown[] = [];

  for (const c of cases) {
    fs.rmSync(dataDir, { recursive: true, force: true });
    fs.rmSync(extraDir, { recursive: true, force: true });
    fs.mkdirSync(extraDir, { recursive: true });
    if (c.legacy) writeLegacy(legacyFile, c.legacy);
    if (c.name === "explicit-path") writeLegacy(explicitFile, { sql: [FULL_DATA, FULL_WIKI] });
    if (c.name === "explicit-not-legacy") {
      const db = new (AdapterDatabase as any)(notLegacyFile);
      db.exec("CREATE TABLE Other (x)");
      db.close();
    }
    if (c.walResidue) fs.writeFileSync(legacyFile + "-wal", Buffer.from([1, 2, 3]));

    const work = path.join(ctx.out, `legacy-${c.name}.db`);
    fs.rmSync(work, { force: true });
    const db = ctx.initDatabase(work);
    if (c.targetNotEmpty) {
      db.run("INSERT INTO knowledge_items (id, title, content, item_type, status, created_at, updated_at) VALUES ('pre','p','c','note','active',1,1)");
    }
    const migrationIpc = await import("../../apps/desktop/src/main/ipc/migration.ipc.ts");
    ctx.ipcHandlers.clear();
    migrationIpc.registerMigrationIPC(db as never);

    const results: unknown[] = [];
    const ops = JSON.parse(JSON.stringify(c.ops));
    for (const op of c.ops) {
      try {
        let value: unknown;
        if (op.op === "detect") value = await ctx.ipcHandlers.get("migration:detectLegacy")!({});
        else {
          const arg = op.path === "explicit" ? explicitFile : op.path === "missing" ? path.join(extraDir, "missing.db") : op.path === "empty" ? "" : op.path === "other-nolegacy" ? notLegacyFile : undefined;
          value = await (ctx.ipcHandlers.get("migration:runLegacy") as (...a: unknown[]) => unknown)({}, ...(arg === undefined ? [] : [arg]));
        }
        results.push({ value: JSON.parse(JSON.stringify(value ?? null, (_k, v) => (typeof v === "string" ? normPath(v) : v))) });
      } catch (error) {
        results.push({ error: normPath((error as Error).message) });
      }
    }
    const dump = dumpTarget(db);
    for (const k of Object.keys(dump)) dump[k] = normRows(dump[k]);
    ctx.closeDatabase();
    fs.rmSync(work, { force: true });
    // 临时拷贝必须清理干净
    const leftovers = fs.readdirSync(os.tmpdir()).filter((n) => tmpPattern.test(n));
    out.push({ name: c.name, legacy: c.legacy ? { stmts: splitSql(c.legacy.sql), wiki: c.legacy.wiki !== false, header: c.legacy.header ?? null } : null, extra: c.name === "explicit-path" ? "explicit" : c.name === "explicit-not-legacy" ? "notlegacy" : null, targetNotEmpty: c.targetNotEmpty === true, walResidue: c.walResidue === true, ops, results, dump, tmpLeftovers: leftovers.length });
  }
  fs.rmSync(dataDir, { recursive: true, force: true });
  fs.rmSync(extraDir, { recursive: true, force: true });
  return { dumpSql: DUMP, legacySchema: splitSql([LEGACY_SCHEMA]), wikiSchema: splitSql([WIKI_SCHEMA]), fullStmts: splitSql([FULL_DATA, FULL_WIKI]), cases: out };
}
