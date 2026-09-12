import { randomUUID } from 'node:crypto';
import type Database from './adapter';
export const SOURCE_REVISIONS_SCHEMA = `CREATE TABLE IF NOT EXISTS source_capture_revisions (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 title TEXT NOT NULL, content TEXT NOT NULL, transcript TEXT, review_json TEXT NOT NULL, captured_at INTEGER NOT NULL
); CREATE INDEX IF NOT EXISTS idx_source_capture_item ON source_capture_revisions(item_id,captured_at DESC);`;
export function saveSourceRevision(db: Database.Database, itemId: string, value: { title: string; content: string; transcript?: string | null; reasons: string[] }): void {
  db.run('INSERT INTO source_capture_revisions VALUES(?,?,?,?,?,?,?)', randomUUID(), itemId, value.title, value.content, value.transcript ?? null, JSON.stringify(value.reasons), Date.now());
}
export function listSourceRevisions(db: Database.Database, itemId: string) {
  return db.all('SELECT id,title,content,transcript,review_json AS reviewJson,captured_at AS capturedAt FROM source_capture_revisions WHERE item_id=? ORDER BY captured_at DESC,id DESC LIMIT 20', itemId) as { id: string; title: string; content: string; transcript: string | null; reviewJson: string; capturedAt: number }[];
}
