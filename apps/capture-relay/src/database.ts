import { DatabaseSync } from "node:sqlite";
import { createHash, randomBytes, randomUUID } from "node:crypto";
export const hash = (value: string) => createHash("sha256").update(value).digest("hex");
export const secret = () => randomBytes(32).toString("base64url");
export const id = () => randomUUID();
export const DAY = 86400000;
export class RelayError extends Error {
  constructor(public code: string, public statusCode = 400) { super(code); }
}
export function assert(value: unknown, code: string, status = 400): asserts value {
  if (!value) throw new RelayError(code, status);
}
export function token(value: unknown): string {
  assert(typeof value === "string" && /^[A-Za-z0-9_-]{43,128}$/.test(value), "invalid_credential");
  return value;
}
export function identifier(value: unknown): string {
  assert(typeof value === "string" && /^[A-Za-z0-9_-]{16,128}$/.test(value), "invalid_id");
  return value;
}
export class RelayDatabase {
  db: DatabaseSync;
  constructor(file: string) {
    this.db = new DatabaseSync(file);
    const version = Number(this.db.prepare("PRAGMA user_version").get()?.user_version ?? 0);
    if (version > 1) { this.db.close(); throw new Error("中转数据库版本较新，请使用兼容镜像"); }
    this.db.exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA secure_delete=ON;
      CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY);
      CREATE TABLE IF NOT EXISTS invites(hash TEXT PRIMARY KEY, used_by TEXT);
      CREATE TABLE IF NOT EXISTS mailboxes(id TEXT PRIMARY KEY, request_id TEXT UNIQUE NOT NULL, credential_hash TEXT NOT NULL UNIQUE, active INTEGER NOT NULL DEFAULT 1);
      CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, mailbox_id TEXT NOT NULL REFERENCES mailboxes(id), name TEXT NOT NULL, kind TEXT NOT NULL,
        credential_hash TEXT UNIQUE NOT NULL, parent_id TEXT REFERENCES devices(id), active INTEGER NOT NULL DEFAULT 0);
      CREATE TABLE IF NOT EXISTS pairings(id TEXT PRIMARY KEY, mailbox_id TEXT NOT NULL REFERENCES mailboxes(id), nonce_hash TEXT UNIQUE NOT NULL,
        expires_at INTEGER NOT NULL, device_id TEXT REFERENCES devices(id), confirmed INTEGER NOT NULL DEFAULT 0);
      CREATE TABLE IF NOT EXISTS captures(id TEXT PRIMARY KEY, mailbox_id TEXT NOT NULL REFERENCES mailboxes(id), device_id TEXT NOT NULL REFERENCES devices(id),
        request_id TEXT NOT NULL, fingerprint TEXT NOT NULL, input TEXT, mode TEXT NOT NULL, item_count INTEGER NOT NULL, bytes INTEGER NOT NULL,
        created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, received_at INTEGER, state TEXT NOT NULL DEFAULT 'accepted', version INTEGER NOT NULL DEFAULT 0, progress TEXT,
        UNIQUE(device_id,request_id));
      CREATE INDEX IF NOT EXISTS captures_delivery ON captures(mailbox_id,state,created_at,id);
      CREATE INDEX IF NOT EXISTS captures_history ON captures(device_id,created_at);
      CREATE TABLE IF NOT EXISTS daily_usage(mailbox_id TEXT NOT NULL, day INTEGER NOT NULL, items INTEGER NOT NULL, PRIMARY KEY(mailbox_id,day));
      CREATE TABLE IF NOT EXISTS rate_limits(key TEXT PRIMARY KEY, reset_at INTEGER NOT NULL, count INTEGER NOT NULL);
      INSERT OR IGNORE INTO schema_migrations VALUES(1); PRAGMA user_version=1;`);
  }
  get<T>(sql: string, ...args: (string | number | null)[]): T | undefined { return this.db.prepare(sql).get(...args) as T | undefined; }
  all<T>(sql: string, ...args: (string | number | null)[]): T[] { return this.db.prepare(sql).all(...args) as T[]; }
  run(sql: string, ...args: (string | number | null)[]) { return this.db.prepare(sql).run(...args); }
  transaction<T>(action: () => T): T {
    this.db.exec("BEGIN IMMEDIATE");
    try { const result = action(); this.db.exec("COMMIT"); return result; }
    catch (error) { this.db.exec("ROLLBACK"); throw error; }
  }
  rate(key: string, limit: number, now = Date.now()) {
    // 数据库计数在重启后继续有效；只存 IP 哈希，不记录地址或正文。
    const row = this.get<{ count: number; reset_at: number }>("SELECT * FROM rate_limits WHERE key=?", key);
    if (row && row.reset_at > now) {
      assert(row.count < limit, "rate_limited", 429);
      this.run("UPDATE rate_limits SET count=count+1 WHERE key=?", key);
    } else this.run("INSERT OR REPLACE INTO rate_limits VALUES(?,?,1)", key, now + 60000);
  }
  cleanup(now = Date.now()) {
    this.transaction(() => {
      this.run("UPDATE captures SET input=NULL,state='expired' WHERE state='accepted' AND expires_at<=?", now);
      this.run("DELETE FROM captures WHERE (state='received' AND received_at<?) OR (state='expired' AND expires_at<?)", now - 30 * DAY, now - 30 * DAY);
      this.run("DELETE FROM pairings WHERE expires_at<?", now - DAY);
      this.run("DELETE FROM devices WHERE active=0 AND NOT EXISTS(SELECT 1 FROM pairings WHERE device_id=devices.id) AND NOT EXISTS(SELECT 1 FROM captures WHERE device_id=devices.id) AND NOT EXISTS(SELECT 1 FROM devices child WHERE child.parent_id=devices.id)");
      this.run("DELETE FROM rate_limits WHERE reset_at<?", now);
      this.run("DELETE FROM daily_usage WHERE day<?", Math.floor(now / DAY) - 2);
    });
    this.db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
  }
  close() { this.db.close(); }
}
