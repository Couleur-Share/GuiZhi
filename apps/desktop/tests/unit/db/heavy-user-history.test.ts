import { expect, it } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { AskSessionDB } from '@guizhi/db/ask-session';
it('1000 条会话稳定分页、全文检索、重命名与置顶，后续保存不覆盖人工标题', () => {
  const db = new Database(':memory:'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  try {
    const sessions = new AskSessionDB(db);
    db.transaction(() => { for (let i = 0; i < 1000; i++) sessions.save({ id: `s-${i.toString().padStart(4, '0')}`, title: `问答 ${i}`, ...(i === 0 ? { scope: 'article' as const, itemId: 'old-article', articleTitle: '最早文章', target: { itemId: 'old-article', view: 'body' as const } } : {}), messagesJson: JSON.stringify([{ question: i === 0 ? '最早问题的独有关键词' : 'q', answer: i === 0 ? '最早答案的远洋轮船' : 'a' }]) }); })();
    const ids: string[] = []; let cursor: string | null = null;
    do { const page = sessions.query({ cursor }); expect(page.entries.length).toBeLessThanOrEqual(50); ids.push(...page.entries.map(e => e.id)); cursor = page.nextCursor; } while (cursor);
    expect(sessions.query({}).articles).toContainEqual({ id: 'old-article', title: '最早文章' });
    expect(sessions.query({ itemId: 'old-article' }).entries[0].id).toBe('s-0000');
    expect(new Set(ids).size).toBe(1000); expect(ids).toContain('s-0000');
    expect(sessions.query({ search: '远洋轮船' }).entries.map(e => e.id)).toContain('s-0000');
    sessions.updateMeta('s-0000', { title: '重要的历史记录', pinned: true });
    sessions.save({ id: 's-0000', title: '自动标题', messagesJson: '[]' });
    expect(sessions.query({}).entries[0]).toMatchObject({ id: 's-0000', title: '重要的历史记录', pinned: true });
    expect(sessions.query({ search: '重要的历史记录' }).entries).toHaveLength(1);
    expect('messagesJson' in sessions.query({}).entries[0]).toBe(false);
    expect(() => sessions.query({ search: '改变筛选', cursor: sessions.query({}).nextCursor })).toThrow('筛选已变化');
  } finally { db.close(); }
});
