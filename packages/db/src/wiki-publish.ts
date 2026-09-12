import { materialHash } from "./wiki-material";
import { randomUUID } from 'node:crypto';
import type Database from './adapter';
import { WIKI_COMPILER_VERSION, type WikiContributions } from '@guizhi/shared/types/wiki-compiler';
import { segmentTextForFts } from './fts';
type Draft = WikiContributions[number];
type Existing = { id: string; manual_edited_at: number | null; prompt_version: string; body: string };

function savePage(db: Database.Database, draft: Draft, model: string, id: string, exists: boolean, manual = false) {
  const now = Date.now();
  if (exists) {
    db.run(`INSERT INTO wiki_page_revisions(id,page_id,title,kind,summary,body,aliases_json,model,prompt_version,created_at)
      SELECT ?,id,title,kind,summary,body,aliases_json,model,prompt_version,? FROM wiki_pages WHERE id=?`, randomUUID(), now, id);
    db.run("DELETE FROM wiki_page_revisions WHERE page_id=? AND id NOT IN (SELECT id FROM wiki_page_revisions WHERE page_id=? ORDER BY created_at DESC,rowid DESC LIMIT 10)", id, id);
    db.run('UPDATE wiki_pages SET title=?,kind=?,summary=?,body=?,aliases_json=?,model=?,prompt_version=?,generated_at=?,updated_at=? WHERE id=?', draft.title, draft.kind, draft.summary, draft.body, draft.aliasesJson, model, WIKI_COMPILER_VERSION, now, now, id);
  } else {
    db.run(`INSERT INTO wiki_pages(id,title,normalized_title,kind,summary,body,aliases_json,provider,model,prompt_version,generated_at,created_at,updated_at)
      VALUES(?,?,?,?,?,?,?,'guizhi',?,?,?,?,?)`, id, draft.title, draft.normalizedTitle, draft.kind, draft.summary, draft.body, draft.aliasesJson, model, WIKI_COMPILER_VERSION, now, now, now);
  }
  if (manual) db.run('UPDATE wiki_pages SET manual_edited_at=? WHERE id=?', now, id);
  db.run('DELETE FROM wiki_fts WHERE page_id=?', id);
  db.run('INSERT INTO wiki_fts(page_id,title,summary,body) VALUES(?,?,?,?)', id, segmentTextForFts(draft.title), segmentTextForFts(draft.summary), segmentTextForFts(draft.body));
  db.run('DELETE FROM wiki_page_links WHERE from_page_id=?', id);
  for (const title of draft.linkTargets) {
    const target = db.get('SELECT id FROM wiki_pages WHERE normalized_title=?', title) as { id: string } | undefined;
    if (target && target.id !== id) db.run('INSERT OR IGNORE INTO wiki_page_links(from_page_id,to_page_id,created_at) VALUES(?,?,?)', id, target.id, now);
  }
}

/** 仅从仍有效的分块贡献构建正文；已经移除的章节没有永久残留的入口。 */
export function publishWikiPages(db: Database.Database, titles: string[], model: string): void {
  const links = new Map<string, string[]>();
  try { const parsed = JSON.parse(model); if (Array.isArray(parsed)) model = String(parsed.at(-1)); } catch { /* 旧模型名保持可读。 */ }
  for (const title of new Set(titles)) {
    db.run('DELETE FROM wiki_invalidated_pages WHERE page_title=?', title);
    const contributions = db.all(`SELECT c.item_id,c.block_key,c.contribution_json,i.title AS source_title FROM wiki_contributions c
      JOIN knowledge_items i ON i.id=c.item_id WHERE c.page_title=? AND i.deleted_at IS NULL
      ORDER BY c.item_id,c.block_key`, title) as { item_id: string; block_key: string; contribution_json: string; source_title: string }[];
    const existing = db.get('SELECT id,manual_edited_at,prompt_version,body FROM wiki_pages WHERE normalized_title=?', title) as Existing | undefined;
    if (!existing && !contributions.length) continue;
    const drafts = contributions.map(c => JSON.parse(c.contribution_json) as Draft);
    const first = drafts[0];
    const draft: Draft = { title: first?.title ?? title, normalizedTitle: title, kind: first?.kind ?? 'topic',
      summary: first?.summary ?? '自动来源已移除或等待复核', aliasesJson: first?.aliasesJson ?? null,
      body: drafts.length ? drafts.map((d, i) => `${d.body}\n\n> 来源：《${contributions[i].source_title}》`).join('\n\n---\n\n') : '自动来源已移除或等待复核，目前没有可发布的知识贡献。',
      linkTargets: [...new Set(drafts.flatMap(d => d.linkTargets))] };
    const id = existing?.id ?? randomUUID();
    // 旧页仍有未升级来源时没有足够证据整体重建；保留原页并提出可审阅建议。
    const legacy = existing && existing.prompt_version !== WIKI_COMPILER_VERSION && db.get(`SELECT 1 FROM wiki_page_sources s
      LEFT JOIN wiki_ingestions w ON w.item_id=s.item_id WHERE s.page_id=? AND COALESCE(w.prompt_version,'')!=? LIMIT 1`, id, WIKI_COMPILER_VERSION);
    if (existing?.manual_edited_at != null || legacy) {
      db.run('INSERT OR REPLACE INTO wiki_page_suggestions(page_id,draft_json,reason,created_at) VALUES(?,?,?,?)', id, JSON.stringify({ draft, model, sourceIds: [...new Set(contributions.map(c => c.item_id))] }), legacy ? '旧页面仍有未升级来源，保留原内容，需审阅更新建议' : '人工编辑内容保持不变，需审阅更新建议', Date.now());
      continue;
    }
    savePage(db, draft, model, id, Boolean(existing));
    links.set(id, draft.linkTargets);
    db.run('DELETE FROM wiki_page_suggestions WHERE page_id=?', id);
    db.run('DELETE FROM wiki_page_sources WHERE page_id=?', id);
    for (const sourceId of new Set(contributions.map(c => c.item_id))) db.run('INSERT INTO wiki_page_sources(page_id,item_id,created_at) VALUES(?,?,?)', id, sourceId, Date.now());
  }
  // 本批所有新页创建完成后再补齐前向出链。
  for (const [id, targets] of links) for (const title of targets) {
    const target = db.get('SELECT id FROM wiki_pages WHERE normalized_title=?', title) as { id: string } | undefined;
    if (target && target.id !== id) db.run('INSERT OR IGNORE INTO wiki_page_links(from_page_id,to_page_id,created_at) VALUES(?,?,?)', id, target.id, Date.now());
  }
}
export function flushWikiInvalidations(db: Database.Database): void {
  const rows = db.all('SELECT page_title FROM wiki_invalidated_pages') as { page_title: string }[];
  if (!rows.length) return;
  db.transaction(() => {
    publishWikiPages(db, rows.map(r => r.page_title), '来源更新');
    db.run('DELETE FROM wiki_invalidated_pages');
  })();
}
export function wikiSuggestionToken(db: Database.Database, pageId: string, draftJson: string): string {
  const page = db.get("SELECT title,summary,body,aliases_json,kind,updated_at FROM wiki_pages WHERE id=?", pageId);
  return materialHash(JSON.stringify([draftJson, page]));
}
export function acceptWikiSuggestion(db: Database.Database, pageId: string, token?: string): boolean {
  return db.transaction(() => {
    // 来源失效可能尚未被目录读取消化；采用建议前必须在同一事务中重新核对。
    flushWikiInvalidations(db);
    const row = db.get('SELECT draft_json FROM wiki_page_suggestions WHERE page_id=?', pageId) as { draft_json: string } | undefined;
    if (!row || (token !== undefined && token !== wikiSuggestionToken(db, pageId, row.draft_json))) return false;
    const value = JSON.parse(row.draft_json) as { draft: Draft; model: string; sourceIds: string[] };
    savePage(db, value.draft, value.model, pageId, true, true);
    db.run('DELETE FROM wiki_page_sources WHERE page_id=?', pageId);
    for (const id of value.sourceIds) if (db.get('SELECT id FROM knowledge_items WHERE id=? AND deleted_at IS NULL', id)) db.run('INSERT INTO wiki_page_sources(page_id,item_id,created_at) VALUES(?,?,?)', pageId, id, Date.now());
    db.run('DELETE FROM wiki_page_suggestions WHERE page_id=?', pageId); return true;
  })();
}
