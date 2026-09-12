import { afterEach, expect, it } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { runMigrations } from '@guizhi/db/migrations';
import { AskSessionDB } from '@guizhi/db/ask-session';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { clearAskEvidence } from '@guizhi/db/ask-evidence';
import { evidenceUrl } from '@guizhi/shared/utils/evidence';
const dbs: Database.Database[] = [];
function fixture() { const db = new Database(':memory:'); dbs.push(db); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES); return { db, items: new KnowledgeItemDB(db), sessions: new AskSessionDB(db) }; }
afterEach(() => { for (const db of dbs.splice(0)) db.close(); });
it('默认删除原文仍保留当时片段，选择清除会同步移除嵌入上下文且迟到保存不能复活', () => {
  const { db, items, sessions } = fixture(); const item = items.create({ title: '原文', content: '第一版本' });
  const input = { id: 's', title: '问答', scope: 'article' as const, itemId: item.id, target: { itemId: item.id, view: 'body' as const, selection: '第一版本' },
    messagesJson: JSON.stringify([{ question: 'q', answer: '根据第一版本回答', context: { target: { itemId: item.id, selection: '第一版本' }, sources: [] },
      evidenceSources: [{ kind: 'item', refId: item.id, evidence: { sourceId: item.id, text: '额外未引用证据' } }], sources: [{ ordinal: 1, kind: 'article', text: '第一版本', target: { itemId: item.id }, url: 'https://example.com/?token=private', evidence: { sourceId: item.id, text: '第一版本', capturedAt: 1 } }] }]) };
  sessions.save(input); items.update(item.id, { content: '第二版本' }); items.deleteForever([item.id]);
  expect(sessions.get('s')?.messagesJson).toContain('第一版本');
  clearAskEvidence(db, [item.id]); sessions.save(input);
  const message = JSON.parse(sessions.get('s')!.messagesJson)[0];
  expect(message.answer).toBe('根据第一版本回答'); expect(message.context).toBeUndefined();
  expect(message.sources[0]).toMatchObject({ cleared: true, title: '来源已清除' });
  expect(message.evidenceSources[0].evidence.text).toBe(''); expect(message.evidenceSources[0].cleared).toBe(true);
  expect(message.sources[0].text).toBeUndefined(); expect(message.sources[0].url).toBeUndefined();
  expect(sessions.get('s')?.target?.selection).toBeUndefined();
});
it('清除与删除在事务故障时回滚，迁移重复运行不改变证据', () => {
  const { db, items, sessions } = fixture(); const item = items.create({ content: '原文' });
  const input = { id: 'old', title: '老会话', messagesJson: JSON.stringify([{ answer: '不变', sources: [{ kind: 'item', refId: item.id, title: '未保存片段' }] }]) };
  sessions.save(input);
  expect(() => db.transaction(() => { clearAskEvidence(db, [item.id]); throw new Error('写入失败'); })()).toThrow();
  expect(sessions.get('old')?.messagesJson).toBe(input.messagesJson);
  runMigrations(db); expect(runMigrations(db)).toEqual([]);
  expect(sessions.get('old')?.messagesJson).toBe(input.messagesJson);
});
it('证据链接去除访问凭证而保留普通参数', () => {
  expect(evidenceUrl('https://user:pass@example.com/p?id=7&access_token=secret#secret')).toBe('https://example.com/p?id=7');
  expect(evidenceUrl('file:///private')).toBeUndefined();
});
