import { afterEach, expect, it } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { WikiDB } from '@guizhi/db/wiki';
import { WikiCompilerDB } from '@guizhi/db/wiki-compiler';
import { wikiBlocks, wikiFingerprint } from '@guizhi/db/wiki-material';
import { longArticle } from '../../helpers/heavy-user';
const dbs: Database.Database[] = [];
function fixture() { const db = new Database(':memory:'); dbs.push(db); db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES); return { db, items: new KnowledgeItemDB(db), wiki: new WikiDB(db), compiler: new WikiCompilerDB(db) }; }
afterEach(() => { for (const db of dbs.splice(0)) db.close(); });
const contribution = (body: string, title = '全文知识') => [{ title, normalizedTitle: title, kind: 'topic' as const, summary: '全文', body, aliasesJson: null, linkTargets: [] }];
function start(compiler: WikiCompilerDB, id: string) { const result = compiler.start('model', compiler.preview('model', [id]).entries, true); expect(result.ok).toBe(true); return result.job!.id; }
function drain(compiler: WikiCompilerDB, id: string) { let work; let calls = 0; while ((work = compiler.next(id))) { compiler.finish(work, contribution(work.block.text)); calls++; } return calls; }
it('彻底删除清除编译原文检查点；清空 Wiki 不留下可复活的贡献和任务', () => {
  const { db, items, compiler, wiki } = fixture();
  const item = items.create({ content: '必须清除的检查点' }); start(compiler, item.id);
  items.deleteForever([item.id]);
  expect(db.all('SELECT * FROM wiki_compile_blocks')).toEqual([]);
  expect(db.all('SELECT * FROM wiki_compile_items')).toEqual([]);
  const another = items.create({ content: '可编译' }); drain(compiler, start(compiler, another.id)); wiki.clearAll();
  expect(db.all('SELECT * FROM wiki_contributions')).toEqual([]); expect(db.all('SELECT * FROM wiki_compile_jobs')).toEqual([]);
  expect(wiki.getCatalog()).toEqual([]);
});
it('长文前中后与独立文字稿全部分块；尾部修改改变指纹，前插段落复用旧块', () => {
  const item = { id: 'a', title: '长文', content: longArticle, transcript: '独立文字稿唯一事实', review_status: 'clear', deleted_at: null };
  const blocks = wikiBlocks(item), text = blocks.map(b => b.text).join('\n');
  expect(text).toContain('独立文字稿唯一事实'); expect(blocks.every(b => b.text.length <= 3000)).toBe(true);
  expect(wikiFingerprint(item)).not.toBe(wikiFingerprint({ ...item, content: item.content + '尾部新事实' }));
  const changed = wikiBlocks({ ...item, content: '前面新增一段。\n\n' + item.content });
  expect(blocks.filter(b => changed.some(next => next.hash === b.hash))).toHaveLength(blocks.length);
});
it('历史升级失败仍需用户选择，不会降为后台可执行；代码围栏跨块闭合', () => {
  const { db, items, compiler } = fixture(); const item = items.create({content:'旧资料'});
  db.run("INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,updated_at) VALUES(?,'old','model','v1',1)", item.id);
  const id = start(compiler, item.id), work = compiler.next(id)!; compiler.finish(work, undefined, '超时'); compiler.next(id);
  db.run('UPDATE wiki_ingestions SET next_attempt_at=0 WHERE item_id=?', item.id);
  expect(compiler.preview('model',[item.id]).entries[0].state).toBe('upgrade');
  const blocks = wikiBlocks({id:'f',title:'代码',content:'```ts\n'+('const value = 1;\n'.repeat(600))+'```',transcript:null,review_status:'clear',deleted_at:null});
  expect(blocks.length).toBeGreaterThan(1);
  for (const block of blocks) { expect(block.text.length).toBeLessThanOrEqual(3000); expect(block.text.trim().split('\n')[0]).toBe('```ts'); expect(block.text.trim().split('\n').at(-1)).toBe('```'); }
});
it('只含文字稿可编译；待复核不进入自动集合；旧来源必须明确选择升级', () => {
  const { db, items, compiler } = fixture();
  const transcript = items.create({ title: '', content: '', transcript: '唯一的语音事实' });
  const review = items.create({ content: '缺失', reviewStatus: 'needs_review', reviewReasons: ['采集不完整'] });
  db.run("INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,updated_at) VALUES(?,'old','model','v1',1)", transcript.id);
  expect(compiler.preview('model').entries.find(e => e.id === review.id)?.state).toBe('review');
  const selected = compiler.preview('model', [transcript.id]).entries;
  expect(selected[0].state).toBe('upgrade'); expect(compiler.start('model', selected).ok).toBe(false);
  const job = start(compiler, transcript.id); expect(drain(compiler, job)).toBe(1);
  expect(compiler.preview('model', [transcript.id]).entries[0].state).toBe('current');
});
it('中断可继续已成功块不重调；暂停只阻止后续请求；旧版本不能发布', () => {
  const { items, compiler, wiki } = fixture(); const item = items.create({ content: '第一段\n\n第二段\n\n第三段' });
  const id = start(compiler, item.id), first = compiler.next(id)!;
  compiler.control(id, 'paused'); compiler.finish(first, contribution(first.block.text)); expect(compiler.next(id)).toBeNull();
  compiler.control(id, 'running'); compiler.next(id); compiler.interrupt();
  expect(compiler.get(id)?.status).toBe('interrupted'); compiler.control(id, 'running');
  expect(drain(compiler, id)).toBe(2); expect(compiler.get(id)?.status).toBe('completed');
  expect(wiki.getPage(wiki.getCatalog()[0].id)?.page.body).toContain('第三段');
  items.update(item.id, { content: '完全不同' }); const next = start(compiler, item.id); const work = compiler.next(next)!;
  items.update(item.id, { content: '再度变化' }); compiler.finish(work, contribution('不能发布的旧版本')); compiler.next(next);
  expect(compiler.get(next)?.status).toBe('failed'); expect(wiki.getPage(wiki.getCatalog()[0].id)?.page.body).not.toContain('不能发布的旧版本');
});
it('章节删除后旧贡献消失；人工页面正文与元信息不自动覆盖，更新进入建议', () => {
  const { db, items, compiler, wiki } = fixture(); const item = items.create({ content: '保留章节\n\n删除章节的旧结论' });
  drain(compiler, start(compiler, item.id)); const pageId = wiki.getCatalog()[0].id;
  items.update(item.id, { content: '保留章节' }); expect(drain(compiler, start(compiler, item.id))).toBe(0);
  expect(wiki.getPage(pageId)?.page.body).not.toContain('旧结论');
  wiki.updatePageBody({ pageId, body: '人工确认内容', linkTargets: [] }); db.run("UPDATE wiki_pages SET summary='人工摘要' WHERE id=?", pageId);
  items.update(item.id, { content: '新增知识' }); drain(compiler, start(compiler, item.id));
  expect(wiki.getPage(pageId)?.page).toMatchObject({ body: '人工确认内容', summary: '人工摘要' });
  expect(db.get('SELECT reason FROM wiki_page_suggestions WHERE page_id=?', pageId)).toBeTruthy();
});
