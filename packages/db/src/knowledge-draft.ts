import type Database from './adapter';
import { KnowledgeItemDB } from './knowledge';
import { mergeDraftTags, type SaveKnowledgeDraftInput, type SaveKnowledgeDraftResult } from '@guizhi/shared/types/knowledge-draft';

/** 在同一事务内比较基线与写入，不把异步读取后的旧正文覆盖到数据库。 */
export function saveKnowledgeDraft(db: Database.Database, input: SaveKnowledgeDraftInput): SaveKnowledgeDraftResult {
  if (!input || typeof input.id !== 'string' || typeof input.requestId !== 'string' || !input.patch || !input.base) {
    return { ok: false, error: '草稿保存参数无效' };
  }
  for (const fields of [input.base, input.patch]) {
    if (Object.keys(fields).some(key => !['title', 'content', 'tagNames'].includes(key)) ||
        (['title', 'content'] as const).some(key => fields[key] !== undefined && typeof fields[key] !== 'string') ||
        (fields.tagNames !== undefined && (!Array.isArray(fields.tagNames) || fields.tagNames.some(name => typeof name !== 'string')))) {
      return { ok: false, error: '草稿字段无效' };
    }
  }
  return db.transaction(() => {
    const items = new KnowledgeItemDB(db), current = items.get(input.id);
    if (!current || current.deletedAt != null) return { ok: false, error: '条目不存在或已移到回收站' };
    const conflicts = (['title', 'content'] as const).filter(field =>
      input.patch[field] !== undefined && input.base[field] !== current[field] && input.patch[field] !== current[field]);
    if (conflicts.length) return { ok: false, item: current, conflicts, error: '该内容已在其他操作中修改，请选择要保留的版本' };
    const patch = { ...input.patch };
    if (patch.tagNames) patch.tagNames = mergeDraftTags(input.base.tagNames ?? [], patch.tagNames, current.tags.map(tag => tag.name));
    const item = items.update(input.id, patch);
    return item ? { ok: true, item } : { ok: false, error: '条目不存在' };
  })();
}
