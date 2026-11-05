import { DatabaseSync } from 'node:sqlite';
import { check, credential, digest, hash, id, inspectToken } from './validation';
import type { ReadingHead, ReadingPublish } from '@guizhi/shared/types/reading-sync';

interface Library { id: string; generation: string; writer_hash: string; sequence: number; digest: string | null;
  manifest: string | null; key_id: string | null; frozen: number; }
export interface ReadingLimits { bytes: number; objects: number; libraries: number; }
const APP_ID = 1197101651;

export class ReadingDatabase {
  readonly db: DatabaseSync;
  constructor(file: string, readonly limits: ReadingLimits = { bytes: 256 * 1024 * 1024, objects: 20000, libraries: 100 }) {
    for (const value of Object.values(limits)) check(Number.isSafeInteger(value) && value > 0, 'invalid_limits');
    this.db = new DatabaseSync(file);
    try {
      const appId = Number(this.get<{ application_id: number }>('PRAGMA application_id')?.application_id);
      const version = Number(this.get<{ user_version: number }>('PRAGMA user_version')?.user_version);
      const tables = this.get<{ n: number }>("SELECT count(*) n FROM sqlite_master WHERE type='table'")!.n;
      check((appId === APP_ID && version === 1) || (appId === 0 && version === 0 && tables === 0), 'wrong_database');
      this.db.exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
        CREATE TABLE IF NOT EXISTS reading_libraries(id TEXT PRIMARY KEY,generation TEXT NOT NULL,writer_hash TEXT UNIQUE NOT NULL,
          sequence INTEGER NOT NULL DEFAULT 0,digest TEXT,manifest TEXT,key_id TEXT,frozen INTEGER NOT NULL DEFAULT 0);
        CREATE TABLE IF NOT EXISTS reading_objects(library TEXT NOT NULL REFERENCES reading_libraries(id),digest TEXT NOT NULL,
          token TEXT NOT NULL,bytes INTEGER NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(library,digest));
        CREATE TABLE IF NOT EXISTS reading_refs(library TEXT NOT NULL,digest TEXT NOT NULL,PRIMARY KEY(library,digest),
          FOREIGN KEY(library,digest) REFERENCES reading_objects(library,digest));
        CREATE TABLE IF NOT EXISTS reading_pairs(id TEXT PRIMARY KEY,library TEXT NOT NULL REFERENCES reading_libraries(id),
          expires INTEGER NOT NULL,device_id TEXT,credential_hash TEXT,proof TEXT,bundle TEXT,active INTEGER NOT NULL DEFAULT 0);
        CREATE UNIQUE INDEX IF NOT EXISTS reading_device_credential ON reading_pairs(credential_hash) WHERE credential_hash IS NOT NULL;
        CREATE TABLE IF NOT EXISTS reading_rates(key TEXT PRIMARY KEY,reset INTEGER NOT NULL,count INTEGER NOT NULL);
        PRAGMA application_id=${APP_ID}; PRAGMA user_version=1;`);
    } catch (error) { this.db.close(); throw error; }
  }
  get<T>(sql: string, ...values: (string | number | null)[]): T | undefined { return this.db.prepare(sql).get(...values) as T | undefined; }
  all<T>(sql: string, ...values: (string | number | null)[]): T[] { return this.db.prepare(sql).all(...values) as T[]; }
  run(sql: string, ...values: (string | number | null)[]) { return this.db.prepare(sql).run(...values); }
  transaction<T>(fn: () => T): T {
    this.db.exec('BEGIN IMMEDIATE');
    try { const value = fn(); this.db.exec('COMMIT'); return value; }
    catch (error) { this.db.exec('ROLLBACK'); throw error; }
  }
  library(libraryId: string): Library {
    const row = this.get<Library>('SELECT * FROM reading_libraries WHERE id=?', id(libraryId));
    check(row, 'not_found', 404); return row;
  }
  provision(libraryId: string, generation: string, writer: string) {
    id(libraryId); id(generation); const writerHash = hash(credential(writer));
    return this.transaction(() => {
      const existing = this.get<Library>('SELECT * FROM reading_libraries WHERE id=?', libraryId);
      if (existing) { check(existing.generation === generation && existing.writer_hash === writerHash, 'library_conflict', 409); return; }
      check(this.get<{ n: number }>('SELECT count(*) n FROM reading_libraries')!.n < this.limits.libraries, 'library_quota', 429);
      check(!this.get('SELECT id FROM reading_libraries WHERE writer_hash=?', writerHash), 'credential_conflict', 409);
      this.run('INSERT INTO reading_libraries(id,generation,writer_hash) VALUES(?,?,?)', libraryId, generation, writerHash);
    });
  }
  auth(libraryId: string, raw: string, writerOnly = false) {
    const library = this.library(libraryId), credentialHash = hash(credential(raw));
    if (library.writer_hash === credentialHash) return library;
    check(!writerOnly, 'writer_required', 403);
    check(this.get('SELECT id FROM reading_pairs WHERE library=? AND credential_hash=? AND active=1', libraryId, credentialHash), 'unauthorized', 401);
    return library;
  }
  rate(key: string, now = Date.now()) {
    this.run('DELETE FROM reading_rates WHERE reset<=?', now);
    const row = this.get<{ count: number }>('SELECT count FROM reading_rates WHERE key=?', key);
    check(!row || row.count < 240, 'rate_limited', 429);
    this.run('INSERT INTO reading_rates VALUES(?,?,1) ON CONFLICT(key) DO UPDATE SET count=count+1', key, now + 60000);
  }
  head(libraryId: string): ReadingHead {
    const row = this.library(libraryId);
    return { sequence: row.sequence, generation: row.generation, digest: row.digest, manifestToken: row.manifest, keyId: row.key_id };
  }
  put(libraryId: string, objectDigest: string, token: string) {
    const library = this.library(libraryId); digest(objectDigest);
    check(!library.frozen, 'key_rotation_required', 409);
    const header = inspectToken(token, libraryId, library.generation);
    check(['article', 'image'].includes(header.kind) && hash(token) === objectDigest, 'invalid_object');
    this.transaction(() => {
      const old = this.get<{ token: string }>('SELECT token FROM reading_objects WHERE library=? AND digest=?', libraryId, objectDigest);
      if (old) {
        check(old.token === token, 'object_conflict', 409);
        // 重试即续租暂存对象，避免后续新对象触发回收时删掉刚确认的旧对象。
        this.run('UPDATE reading_objects SET created=? WHERE library=? AND digest=?', Date.now(), libraryId, objectDigest);
        return;
      }
      // 只回收一天前的无引用暂存对象，不清除当前已发布快照。
      this.run(`DELETE FROM reading_objects WHERE library=? AND created<? AND NOT EXISTS
        (SELECT 1 FROM reading_refs r WHERE r.library=reading_objects.library AND r.digest=reading_objects.digest)`, libraryId, Date.now() - 86400000);
      const usage = this.get<{ bytes: number; count: number }>('SELECT coalesce(sum(bytes),0) bytes,count(*) count FROM reading_objects WHERE library=?', libraryId)!;
      check(usage.bytes + Buffer.byteLength(token) <= this.limits.bytes && usage.count < this.limits.objects, 'storage_quota', 413);
      this.run('INSERT INTO reading_objects VALUES(?,?,?,?,?)', libraryId, objectDigest, token, Buffer.byteLength(token), Date.now());
    });
  }
  publish(libraryId: string, payload: ReadingPublish): ReadingHead {
    return this.transaction(() => {
      const library = this.library(libraryId);
      check(!library.frozen, 'key_rotation_required', 409);
      const header = inspectToken(payload.manifestToken, libraryId, library.generation);
      check(header.kind === 'manifest' && header.revision === payload.sequence, 'invalid_manifest');
      check(Array.isArray(payload.objectDigests) && payload.objectDigests.length <= 10000, 'invalid_manifest');
      const refs = [...new Set(payload.objectDigests.map(digest))].sort();
      check(refs.length === payload.objectDigests.length, 'duplicate_object');
      const nextDigest = hash(payload.manifestToken);
      if (library.sequence === payload.sequence && library.digest === nextDigest) {
        const current = this.all<{ digest: string }>('SELECT digest FROM reading_refs WHERE library=? ORDER BY digest', libraryId);
        check(JSON.stringify(current.map(row => row.digest)) === JSON.stringify(refs), 'publication_conflict', 409);
        return this.head(libraryId);
      }
      check(payload.sequence === library.sequence + 1 && payload.parentDigest === library.digest, 'head_conflict', 409);
      check(!library.key_id || library.key_id === header.keyId, 'wrong_key', 409);
      for (const ref of refs) check(this.get('SELECT digest FROM reading_objects WHERE library=? AND digest=?', libraryId, ref), 'missing_object', 409);
      this.run('DELETE FROM reading_refs WHERE library=?', libraryId);
      for (const ref of refs) this.run('INSERT INTO reading_refs VALUES(?,?)', libraryId, ref);
      this.run('UPDATE reading_libraries SET sequence=?,digest=?,manifest=?,key_id=? WHERE id=?', payload.sequence, nextDigest, payload.manifestToken, header.keyId, libraryId);
      return this.head(libraryId);
    });
  }
  close() { this.db.close(); }
}
