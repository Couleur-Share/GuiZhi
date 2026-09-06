import type Database from "./adapter";

export const WEB_CAPTURE_SCHEMA = `
CREATE TABLE IF NOT EXISTS crawl_jobs (
 id TEXT PRIMARY KEY, payload TEXT NOT NULL, status TEXT NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS crawl_pages (
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES crawl_jobs(id) ON DELETE CASCADE,
 url TEXT NOT NULL, depth INTEGER NOT NULL, seed_index INTEGER NOT NULL,
 status TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(job_id,url)
);
CREATE INDEX IF NOT EXISTS idx_crawl_pages_queue ON crawl_pages(job_id,status,depth);
CREATE TABLE IF NOT EXISTS web_source_versions (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 payload TEXT NOT NULL, captured_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_web_versions_item ON web_source_versions(item_id,captured_at);
CREATE TABLE IF NOT EXISTS web_source_baselines (
 item_id TEXT PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
 version_id TEXT REFERENCES web_source_versions(id), content_hash TEXT NOT NULL,
 title TEXT NOT NULL, checked_at INTEGER NOT NULL, remote_hash TEXT NOT NULL,
 summary_stale INTEGER NOT NULL DEFAULT 0
);`;

/** 在迁移框架关闭 FK 的事务内重建 CHECK；保留真实表定义、全部列与索引。 */
export function migrateWebCapture(db: Database): void {
  db.exec(WEB_CAPTURE_SCHEMA);
  for (const name of ["research_source_runs", "research_candidates"]) {
    const table = db.get(
      "SELECT sql FROM sqlite_master WHERE type='table' AND name=?",
      name,
    ) as { sql: string } | undefined;
    if (!table || table.sql.includes("'web'")) continue;
    const expanded = table.sql.replace(
      /'xiaohongshu'\s*,\s*'douyin'\s*,\s*'bilibili'/g,
      "'xiaohongshu','douyin','bilibili','web'",
    );
    if (expanded === table.sql) throw new Error(`无法识别 ${name} 的来源约束`);
    const indexes = db.all(
      "SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL",
      name,
    ) as { sql: string }[];
    const columns = (db.pragma(`table_info(${name})`) as { name: string }[])
      .map((c) => `"${c.name}"`)
      .join(",");
    const before = db.get(`SELECT COUNT(*) AS count FROM ${name}`) as {
      count: number;
    };
    db.exec(expanded.replace(new RegExp(`\\b${name}\\b`), `${name}_web_new`));
    db.exec(
      `INSERT INTO ${name}_web_new (${columns}) SELECT ${columns} FROM ${name}; DROP TABLE ${name}; ALTER TABLE ${name}_web_new RENAME TO ${name};`,
    );
    for (const index of indexes) db.exec(index.sql);
    const after = db.get(`SELECT COUNT(*) AS count FROM ${name}`) as {
      count: number;
    };
    if (before.count !== after.count)
      throw new Error(`迁移 ${name} 行数不一致`);
  }
  const columns = db.pragma("table_info(research_runs)") as { name: string }[];
  if (columns.length && !columns.some((c) => c.name === "time_scope"))
    db.exec(
      "ALTER TABLE research_runs ADD COLUMN time_scope TEXT NOT NULL DEFAULT 'recent' CHECK(time_scope IN ('recent','all'))",
    );
}
