import { afterEach, expect, it, vi } from 'vitest';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';

const databases: Database.Database[] = [];
function fixture() {
  const db = new Database(':memory:');
  databases.push(db);
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  const items = new KnowledgeItemDB(db);
  // 检查资源上限需要观察缓存本身；返回值另行验证，避免仅检查淘汰实现。
  const cache = (items as unknown as { countCache: Map<string, { total: number; expiresAt: number }> }).countCache;
  return { db, items, cache };
}
afterEach(() => { vi.restoreAllMocks(); for (const db of databases.splice(0)) db.close(); });

it('大量只读筛选不会无限保留计数缓存，长排除条件也不常驻复制', () => {
  const { db, items, cache } = fixture();
  vi.spyOn(Date, 'now').mockReturnValue(Date.now());
  db.transaction(() => {
    for (let i = 0; i < 5000; i++) db.run(
      'INSERT INTO knowledge_items (id,title,content,created_at,updated_at) VALUES (?,?,?,?,?)',
      `item-${i}`, `笔记 ${i}`, '用于验证列表查询和计数', i, i,
    );
  })();
  for (let i = 0; i < 300; i++) {
    const result = items.list({ scope: 'all', limit: 20,
      excludedItemIds: Array.from({ length: 500 }, (_, n) => `item-${n + i}`) });
    expect(result.total).toBe(4500);
    expect(result.entries).toHaveLength(20);
  }
  const estimatedUtf16KeyBytes = [...cache.keys()].reduce((size, key) => size + key.length * 2, 0);
  expect(cache.size).toBeLessThanOrEqual(128);
  expect(estimatedUtf16KeyBytes).toBeLessThan(32 * 1024);
});

it('查询其他条件时移除过期记录，并保持短时命中和写入失效', () => {
  const { db, items, cache } = fixture();
  let now = Date.now();
  vi.spyOn(Date, 'now').mockImplementation(() => now);
  const get = vi.spyOn(db, 'get');
  const counts = () => get.mock.calls.filter(([sql]) => sql.startsWith('SELECT COUNT(*) AS count FROM knowledge_items')).length;
  expect(items.list({ scope: 'all' }).total).toBe(0);
  expect(items.list({ scope: 'all', offset: 20 }).total).toBe(0);
  expect(counts()).toBe(1);
  now += 6000;
  items.list({ scope: 'favorites' });
  expect(cache.size).toBe(1);
  expect(items.list({ scope: 'all' }).total).toBe(0);
  expect(counts()).toBe(3);
  items.create({ title: '新增', content: '正文' });
  expect(items.list({ scope: 'all' }).total).toBe(1);
});
