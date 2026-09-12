import { afterEach, expect, it } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { WikiDB } from '@guizhi/db/wiki';
import { WikiCompilerDB } from '@guizhi/db/wiki-compiler';
import { acceptWikiSuggestion, wikiSuggestionToken } from '@guizhi/db/wiki-publish';
const dbs: Database.Database[] = [];
afterEach(() => { for (const db of dbs.splice(0)) db.close(); });
function fixture() {
  const db = new Database(':memory:'); dbs.push(db); db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  const items = new KnowledgeItemDB(db), wiki = new WikiDB(db), compiler = new WikiCompilerDB(db);
  const item = items.create({ content: '初始正文' });
  const compile = () => {
    const result = compiler.start('model', compiler.preview('model', [item.id]).entries);
    expect(result.ok).toBe(true);
    let work; while ((work = compiler.next(result.job!.id))) compiler.finish(work, [{ title: '知识页', normalizedTitle: '知识页', kind: 'topic', body: work.block.text, summary: '摘要', aliasesJson: null, linkTargets: [] }]);
  };
  compile(); const pageId = wiki.getCatalog()[0].id;
  wiki.updatePageBody({ pageId, body: '人工保留正文', linkTargets: [] });
  items.update(item.id, { content: '待采用的来源正文' }); compile();
  const row = db.get('SELECT draft_json FROM wiki_page_suggestions WHERE page_id=?', pageId) as { draft_json: string };
  const token = wikiSuggestionToken(db, pageId, row.draft_json);
  return { db, item, items, wiki, pageId, token };
}
it.each(['edit', 'trash', 'delete'] as const)('来源在建议预览后 %s，旧建议不得发布', action => {
  const { db, items, item, pageId, token } = fixture();
  if (action === 'edit') items.update(item.id, { content: '已变成另一个事实' });
  else if (action === 'trash') items.moveToTrash([item.id]);
  else items.deleteForever([item.id]);
  expect(acceptWikiSuggestion(db, pageId, token)).toBe(false);
  expect((db.get('SELECT body FROM wiki_pages WHERE id=?', pageId) as { body: string }).body).toBe('人工保留正文');
});
it('未变化的建议可采用，并保留人工原文历史', () => {
  const { db, wiki, pageId, token } = fixture();
  expect(acceptWikiSuggestion(db, pageId, token)).toBe(true);
  expect(wiki.getPage(pageId)?.page.body).toContain('待采用的来源正文');
  expect(db.get("SELECT id FROM wiki_page_revisions WHERE page_id=? AND body='人工保留正文'", pageId)).toBeTruthy();
});
