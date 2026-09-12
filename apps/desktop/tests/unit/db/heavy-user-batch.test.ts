import { expect, it } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { executeKnowledgeBatch } from '../../../src/main/services/knowledge-batch';
it('冻结 60 条跨页目标；新增不纳入，删除逐条失败，重试不重做成功项', async () => {
  const db = new Database(':memory:');
  try {
    db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
    const items = new KnowledgeItemDB(db);
    const before = Array.from({ length: 60 }, (_, n) => items.create({ title: `整理 ${n}`, content: '内容' }));
    const targets = items.freezeIds({ scope: 'all', sortBy: 'title', sortOrder: 'asc' });
    expect(targets).toHaveLength(60);
    const late = items.create({ title: '后来新增' });
    items.moveToTrash([before[0].id]); items.deleteForever([before[0].id]);
    const result = await executeKnowledgeBatch(db, targets, { kind: 'update', patch: { isFavorite: true } });
    expect(result.filter(row => row.ok)).toHaveLength(59); expect(result.filter(row => !row.ok)).toHaveLength(1);
    expect(items.get(late.id)?.isFavorite).toBe(false);
    const retry = await executeKnowledgeBatch(db, result.filter(row => !row.ok).map(row => row.id), { kind: 'update', patch: { isFavorite: true } });
    expect(retry).toHaveLength(1); expect(retry[0].error).toContain('删除');
  } finally { db.close(); }
});
