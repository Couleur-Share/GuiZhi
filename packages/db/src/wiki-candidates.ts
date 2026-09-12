import type Database from './adapter';
import type { WikiMaterial } from './wiki-material';

interface CandidateRow extends WikiMaterial {
  ingested_id: string | null;
  content_hash: string;
  prompt_version: string;
  model: string;
  failure_count: number;
  next_attempt_at: number | null;
}

/** 一批三次查询，避免文件库中逐条穿越 WASM / SQLite 边界。 */
export function* wikiCandidateRows(db: Database.Database, ids: string[], model: string) {
  for (let offset = 0; offset < ids.length; offset += 500) {
    const batch = ids.slice(offset, offset + 500), placeholders = batch.map(() => '?').join(',');
    const rows = db.all(`SELECT i.id,i.title,i.content,i.transcript,i.review_status,i.deleted_at,
      w.item_id AS ingested_id,w.content_hash,w.prompt_version,w.model,w.failure_count,w.next_attempt_at
      FROM knowledge_items i LEFT JOIN wiki_ingestions w ON w.item_id=i.id WHERE i.id IN (${placeholders})`, ...batch) as CandidateRow[];
    const byId = new Map(rows.map(row => [row.id, row]));
    const unfinished = new Set((db.all(`SELECT i.item_id,i.fingerprint FROM wiki_compile_items i JOIN wiki_compile_jobs j ON j.id=i.job_id
      WHERE i.item_id IN (${placeholders}) AND i.published=0 AND j.status IN ('running','paused','cancelled','interrupted')`, ...batch) as {item_id: string; fingerprint: string}[]).map(row => `${row.item_id}:${row.fingerprint}`));
    const cached = new Map<string, Set<string>>();
    for (const row of db.all(`SELECT item_id,block_hash FROM wiki_block_cache WHERE model=? AND item_id IN (${placeholders})`, model, ...batch) as {item_id: string; block_hash: string}[]) {
      const hashes = cached.get(row.item_id) ?? new Set<string>(); hashes.add(row.block_hash); cached.set(row.item_id, hashes);
    }
    for (const id of batch) {
      const row = byId.get(id); if (!row) continue;
      yield { item: row, old: row.ingested_id ? row : undefined, unfinished, hashes: cached.get(id) ?? new Set<string>() };
    }
  }
}
