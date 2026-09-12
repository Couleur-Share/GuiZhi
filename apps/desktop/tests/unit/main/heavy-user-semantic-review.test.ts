import { afterEach, expect, it, vi } from 'vitest';
const handlers = vi.hoisted(() => new Map<string, (...args: any[]) => any>());
vi.mock('electron', () => ({ ipcMain: { handle: (name: string, handler: (...args: any[]) => any) => handlers.set(name, handler) }, app: { getVersion: () => 'test' }, session: { defaultSession: {} } }));
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { registerSemanticIPC } from '../../../src/main/ipc/semantic.ipc';
import { IPC_CHANNELS } from '@guizhi/shared/constants';
const dbs: Database.Database[] = [];
afterEach(() => { for (const db of dbs.splice(0)) db.close(); handlers.clear(); vi.restoreAllMocks(); });
it.each(['content', 'transcript', 'title', 'trash'] as const)('嵌入在途时来源 %s 改变，拒绝旧向量回写并保留待处理状态', field => {
  let now = 1000; vi.spyOn(Date, 'now').mockImplementation(() => now);
  const db = new Database(':memory:'); dbs.push(db); db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  const items = new KnowledgeItemDB(db), item = items.create({ title: '标题', content: '第一版本' }); registerSemanticIPC(db);
  const pending = handlers.get(IPC_CHANNELS.SEMANTIC_LIST_PENDING)!({}, { model: 'm', limit: 10 })[0];
  now = 2000;
  if (field === 'trash') items.moveToTrash([item.id]); else items.update(item.id, { [field]: '已变化的来源' });
  now = 3000;
  const applied = handlers.get(IPC_CHANNELS.SEMANTIC_APPLY_EMBEDDINGS)!({}, { itemId: item.id, contentHash: pending.contentHash, model: 'm', dims: 2, chunks: [{ text: '旧分块', vector: [1,0] }] });
  expect(applied).toBe(false); expect(db.all('SELECT * FROM knowledge_embeddings')).toEqual([]);
  if (field !== 'trash') expect(handlers.get(IPC_CHANNELS.SEMANTIC_LIST_PENDING)!({}, { model: 'm', limit: 10 })).toHaveLength(1);
});
it('仅收藏或复核状态变化不拒绝有效向量', () => {
  const db = new Database(':memory:'); dbs.push(db); db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  const items = new KnowledgeItemDB(db), item = items.create({ content: '完整来源' }); registerSemanticIPC(db);
  const pending = handlers.get(IPC_CHANNELS.SEMANTIC_LIST_PENDING)!({}, { model: 'm', limit: 10 })[0];
  items.update(item.id, { isFavorite: true, reviewStatus: 'needs_review' });
  expect(handlers.get(IPC_CHANNELS.SEMANTIC_APPLY_EMBEDDINGS)!({}, { itemId: item.id, contentHash: pending.contentHash, model: 'm', dims: 2, chunks: [{ text: '完整来源', vector: [1,0] }] })).toBe(true);
});
