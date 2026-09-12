import type Database from './adapter';
import { segmentTextForFts, buildFtsMatchQuery } from './fts';
import type { AskSessionFilter, AskSessionPage } from '@guizhi/shared/types/ask';
export const ASK_HISTORY_SCHEMA = `
CREATE TABLE IF NOT EXISTS ask_session_meta(id TEXT PRIMARY KEY REFERENCES ask_sessions(id) ON DELETE CASCADE, custom_title TEXT, pinned INTEGER NOT NULL DEFAULT 0);
CREATE VIRTUAL TABLE IF NOT EXISTS ask_session_fts USING fts5(session_id UNINDEXED,title,questions,answers);
`;
export const ASK_HISTORY_TRIGGERS = `
CREATE TRIGGER IF NOT EXISTS ask_history_deleted AFTER DELETE ON ask_sessions BEGIN DELETE FROM ask_session_fts WHERE session_id=OLD.id; DELETE FROM ask_session_meta WHERE id=OLD.id; END;
`;
export function writeAskHistory(db: Database.Database, id: string): void {
  const row = db.get('SELECT COALESCE(m.custom_title,s.title) AS title,s.messages_json FROM ask_sessions s LEFT JOIN ask_session_meta m ON m.id=s.id WHERE s.id=?', id) as { title: string; messages_json: string } | undefined;
  if (!row) return;
  let messages: { question?: string; answer?: string }[] = [];
  try { const value = JSON.parse(row.messages_json); if (Array.isArray(value)) messages = value; } catch { /* 损坏的老消息仍可按标题检索。 */ }
  db.run('DELETE FROM ask_session_fts WHERE session_id=?', id);
  db.run('INSERT INTO ask_session_fts(session_id,title,questions,answers) VALUES(?,?,?,?)', id, segmentTextForFts(row.title), segmentTextForFts(messages.map(m => m?.question ?? '').join('\n')), segmentTextForFts(messages.map(m => m?.answer ?? '').join('\n')));
}
export function migrateAskHistory(db: Database.Database): void {
  db.exec(ASK_HISTORY_SCHEMA);
  if (!db.get("SELECT 1 FROM sqlite_master WHERE type='table' AND name='ask_sessions'")) return;
  db.exec(ASK_HISTORY_TRIGGERS);
  for (const row of db.all('SELECT id FROM ask_sessions') as { id: string }[]) writeAskHistory(db, row.id);
}
export function queryAskHistory(db: Database.Database, input: AskSessionFilter & { search?: string; from?: number; to?: number; cursor?: string | null; limit?: number }): AskSessionPage {
  const fingerprint = JSON.stringify([input.scope, input.itemId, input.search?.trim() || '', input.from, input.to]);
  const match = input.search?.trim() ? buildFtsMatchQuery(input.search, 'recall') : null;
  if (input.search?.trim() && !match) return { entries: [], nextCursor: null };
  const conditions = ['1=1'], params: unknown[] = [];
  if (match) params.push(match);
  if (input.scope) { conditions.push('s.scope=?'); params.push(input.scope); }
  if (input.itemId) { conditions.push('s.item_id=?'); params.push(input.itemId); }
  if (input.from !== undefined) { conditions.push('s.updated_at>=?'); params.push(input.from); }
  if (input.to !== undefined) { conditions.push('s.updated_at<=?'); params.push(input.to); }
  if (input.cursor) {
    const cursor = JSON.parse(Buffer.from(input.cursor, 'base64url').toString()) as { fingerprint: string; rank: number; pinned: number; updatedAt: number; id: string };
    if (cursor.fingerprint !== fingerprint || typeof cursor.id !== 'string') throw new Error('会话筛选已变化，请从第一页重新加载');
    if (match) { conditions.push('(f.rank>? OR (f.rank=? AND (s.updated_at,s.id)<(?,?)))'); params.push(cursor.rank, cursor.rank, cursor.updatedAt, cursor.id); }
    else { conditions.push('(COALESCE(m.pinned,0),s.updated_at,s.id)<(?,?,?)'); params.push(cursor.pinned, cursor.updatedAt, cursor.id); }
  }
  const limit = Math.min(500, Math.max(1, input.limit ?? 50));
  const rows = db.all(`SELECT s.id,COALESCE(m.custom_title,s.title) AS title,COALESCE(m.pinned,0) AS pinned,s.created_at,s.updated_at,s.scope,s.item_id,s.article_title,s.options_json,${match ? 'f.rank' : '0'} AS rank
    FROM ask_sessions s LEFT JOIN ask_session_meta m ON m.id=s.id
    ${match ? 'JOIN (SELECT session_id,bm25(ask_session_fts,0,5,2,1) AS rank FROM ask_session_fts WHERE ask_session_fts MATCH ?) f ON f.session_id=s.id' : ''}
    WHERE ${conditions.join(' AND ')} ORDER BY ${match ? 'f.rank ASC' : 'COALESCE(m.pinned,0) DESC'},s.updated_at DESC,s.id DESC LIMIT ?`, ...params, limit + 1) as { id: string; title: string; pinned: number; created_at: number; updated_at: number; scope: 'knowledge' | 'article'; item_id: string | null; article_title: string | null; options_json: string; rank: number }[];
  const more = rows.length > limit; if (more) rows.pop();
  const entries = rows.map(row => ({ id: row.id, title: row.title, pinned: row.pinned === 1, createdAt: row.created_at, updatedAt: row.updated_at,
    ...(row.scope === 'article' ? { scope: 'article' as const, itemId: row.item_id ?? undefined, articleTitle: row.article_title ?? undefined, ...JSON.parse(row.options_json || '{}') } : {}) }));
  // 筛选选项来自全部会话元数据，不能只使用当前 50 条结果。
  const articles = input.cursor ? undefined : db.all(`SELECT s.item_id AS id,s.article_title AS title FROM ask_sessions s
    WHERE s.scope='article' AND s.item_id IS NOT NULL AND NOT EXISTS
      (SELECT 1 FROM ask_sessions newer WHERE newer.scope='article' AND newer.item_id=s.item_id AND (newer.updated_at,newer.id)>(s.updated_at,s.id))
    ORDER BY s.article_title,s.item_id`) as { id: string; title: string }[];
  const last = rows.at(-1);
  return { entries, articles, nextCursor: more && last ? Buffer.from(JSON.stringify({ fingerprint, rank: last.rank, pinned: last.pinned, updatedAt: last.updated_at, id: last.id })).toString('base64url') : null };
}
