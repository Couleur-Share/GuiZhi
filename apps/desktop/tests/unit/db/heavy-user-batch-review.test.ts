import { expect, it } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { readKnowledgeBatch } from '@guizhi/db/knowledge-batch-log';
import { executeKnowledgeBatch } from '../../../src/main/services/knowledge-batch';

it('批量回执落库失败回滚对应条目，其他条目继续并持久化成功回执', async () => {
  const db = new Database(':memory:');
  try {
    db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
    const items = new KnowledgeItemDB(db), first = items.create({ content: '保留原值' }), second = items.create({ content: '继续执行' });
    db.exec(`CREATE TRIGGER fail_batch_receipt BEFORE INSERT ON knowledge_batch_results WHEN NEW.item_id='${first.id}' BEGIN SELECT RAISE(ABORT,'注入回执写入失败'); END;`);
    const result = await executeKnowledgeBatch(db, [first.id, second.id], { kind: 'update', patch: { isFavorite: true } }, 'run');
    expect(result[0].ok).toBe(false); expect(result[0].error).toContain('回执写入失败');
    expect(items.get(first.id)?.isFavorite).toBe(false);
    expect(items.get(second.id)?.isFavorite).toBe(true);
    expect(readKnowledgeBatch(db, 'run').results).toContainEqual({ id: second.id, ok: true });
  } finally { db.close(); }
});

it('回执传输丢失后重试同一批次，已提交的删除仍返回成功且不重做', async () => {
  const db = new Database(':memory:');
  try {
    db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
    const items = new KnowledgeItemDB(db), item = items.create({ content: '待删除' });
    items.moveToTrash([item.id]);
    const command = { kind: 'delete' as const, clearEvidence: true };
    expect(await executeKnowledgeBatch(db, [item.id], command, 'replay')).toEqual([{ id: item.id, ok: true }]);
    expect(await executeKnowledgeBatch(db, [item.id], command, 'replay')).toEqual([{ id: item.id, ok: true }]);
    expect(readKnowledgeBatch(db, 'replay').results).toEqual([{ id: item.id, ok: true }]);
  } finally { db.close(); }
});
