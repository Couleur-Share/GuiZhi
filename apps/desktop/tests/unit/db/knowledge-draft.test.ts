import { expect, it } from 'vitest';
import Adapter from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { saveKnowledgeDraft } from '@guizhi/db/knowledge-draft';

it('事务保存检测正文冲突，元数据不冲突，标签合并双方的独立增删', () => {
  const db = new Adapter(':memory:');
  try {
    db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
    const items = new KnowledgeItemDB(db);
    const item = items.create({ title: 'A', content: '原文', tagNames: ['原标签'] });
    items.update(item.id, { isFavorite: true, tagNames: ['原标签', '外部标签'] });
    const result = saveKnowledgeDraft(db, { id: item.id, requestId: '1', base: { content: '原文', tagNames: ['原标签'] }, patch: { content: '草稿', tagNames: ['新标签'] } });
    expect(result.ok).toBe(true); expect(result.item?.isFavorite).toBe(true);
    expect(result.item?.tags.map(t => t.name).sort()).toEqual(['外部标签', '新标签'].sort());
    const conflict = saveKnowledgeDraft(db, { id: item.id, requestId: '2', base: { content: '原文' }, patch: { content: '另一份草稿' } });
    expect(conflict.ok).toBe(false); expect(conflict.conflicts).toEqual(['content']);
    expect(items.get(item.id)?.content).toBe('草稿');
  } finally { db.close(); }
});
