import type Database from './adapter';
import { sanitizeEvidenceJson } from '@guizhi/shared/utils/evidence-sanitizer';
export { sanitizeEvidenceTarget } from '@guizhi/shared/utils/evidence-sanitizer';

export const ASK_EVIDENCE_SCHEMA = `CREATE TABLE IF NOT EXISTS ask_evidence_clearances (item_id TEXT PRIMARY KEY, cleared_at INTEGER NOT NULL);`;

export function sanitizeSessionEvidence(db: Database.Database, json: string): string {
  const cleared = new Set((db.all('SELECT item_id FROM ask_evidence_clearances') as { item_id: string }[]).map(r => r.item_id));
  return sanitizeEvidenceJson(json, cleared);
}
export function clearAskEvidence(db: Database.Database, ids: string[]): void {
  db.transaction(() => {
    for (const id of ids) db.run('INSERT OR REPLACE INTO ask_evidence_clearances(item_id,cleared_at) VALUES(?,?)', id, Date.now());
    for (const row of db.all('SELECT id,messages_json,options_json,item_id FROM ask_sessions') as { id: string; messages_json: string; options_json: string; item_id: string | null }[]) {
      const sanitized = sanitizeSessionEvidence(db, row.messages_json);
      const options = JSON.parse(row.options_json || '{}');
      if (row.item_id && ids.includes(row.item_id) && options.target) options.target = { itemId: row.item_id, view: options.target.view };
      db.run('UPDATE ask_sessions SET messages_json=?,options_json=? WHERE id=?', sanitized, JSON.stringify(options), row.id);
    }
  })();
}
