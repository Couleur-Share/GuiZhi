import { randomUUID } from 'node:crypto';
import type Database from './adapter';
import type { KnowledgeBatchAction, KnowledgeBatchResult } from '@guizhi/shared/types/knowledge-batch';
export const KNOWLEDGE_BATCH_SCHEMA = `
CREATE TABLE IF NOT EXISTS knowledge_selection_sets(id TEXT PRIMARY KEY,ids_json TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS knowledge_batch_results(run_id TEXT NOT NULL,item_id TEXT NOT NULL,command_json TEXT NOT NULL,result_json TEXT NOT NULL,updated_at INTEGER NOT NULL,PRIMARY KEY(run_id,item_id));
CREATE INDEX IF NOT EXISTS idx_knowledge_batch_updated ON knowledge_batch_results(updated_at DESC);
`;
export function freezeKnowledgeSelection(db: Database.Database, ids: string[]): string {
  const id = randomUUID();
  db.transaction(() => {
    db.run('DELETE FROM knowledge_selection_sets WHERE created_at<?', Date.now()-86400000);
    db.run('INSERT INTO knowledge_selection_sets VALUES(?,?,?)', id, JSON.stringify(ids), Date.now());
  })(); return id;
}
export function recordKnowledgeBatch(db: Database.Database, runId: string, command: KnowledgeBatchAction, results: KnowledgeBatchResult[]): void {
  db.transaction(() => {
    for (const result of results) db.run('INSERT OR REPLACE INTO knowledge_batch_results VALUES(?,?,?,?,?)', runId, result.id, JSON.stringify(command), JSON.stringify(result), Date.now());
  })();
}
export function readKnowledgeBatch(db: Database.Database, runId: string): { results: KnowledgeBatchResult[]; command?: KnowledgeBatchAction } {
  const rows = db.all('SELECT command_json,result_json FROM knowledge_batch_results WHERE run_id=? ORDER BY updated_at,item_id', runId) as { command_json: string; result_json: string }[];
  return { results: rows.map(row => JSON.parse(row.result_json)), command: rows[0] ? JSON.parse(rows[0].command_json) : undefined };
}
