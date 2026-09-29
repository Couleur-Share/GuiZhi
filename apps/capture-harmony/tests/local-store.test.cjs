const test = require('node:test');
const assert = require('node:assert/strict');
const { load } = require('./support.cjs');

// 只替换系统边界（Preferences）；校验与迁移逻辑使用真实的 LocalStore.ets 与 Protocol.ts。
const { LocalStore } = load('services/LocalStore.ets', {
  '@kit.AbilityKit': { common: {} }, '@kit.ArkData': { preferences: {} },
  '../core/Protocol': load('core/Protocol.ts'), '../core/DraftQueue': {},
});
const store = raw => new LocalStore({ get: async () => raw, put: async () => {}, flush: async () => {} });
const draft = extra => ({ requestId: 'draft-0001-aaaaaaaa', input: 'https://example.com', mode: 'auto',
  attempted: false, binding: '', createdAt: 1, ...extra });
const state = drafts => JSON.stringify({ version: 1, editor: '', mode: 'auto', drafts });

test('0.3.0 及更早保存的草稿没有失败原因字段，读取时补默认值且不丢草稿', async () => {
  const loaded = await store(state([draft({})])).load();
  assert.equal(loaded.drafts.length, 1);
  assert.equal(loaded.drafts[0].lastError, '');
  assert.equal(loaded.drafts[0].lastErrorAt, 0);
  assert.equal(loaded.drafts[0].input, 'https://example.com');
});

test('失败原因字段类型异常时归一化为空，合法值保持不变', async () => {
  const loaded = await store(state([draft({ lastError: 42, lastErrorAt: 'x' }),
    draft({ requestId: 'draft-0002-aaaaaaaa', lastError: '中转拒绝', lastErrorAt: 123 })])).load();
  assert.equal(loaded.drafts[0].lastError, '');
  assert.equal(loaded.drafts[0].lastErrorAt, 0);
  assert.equal(loaded.drafts[1].lastError, '中转拒绝');
  assert.equal(loaded.drafts[1].lastErrorAt, 123);
});

test('空存储返回全新状态；版本或草稿字段非法时拒绝读取，保护原数据', async () => {
  const empty = await store('').load();
  assert.equal(empty.drafts.length, 0);
  assert.equal(empty.version, 1);
  await assert.rejects(store(JSON.stringify({ version: 2, editor: '', mode: 'auto', drafts: [] })).load(), /无法读取/);
  await assert.rejects(store(state([draft({ mode: 'bad' })])).load(), /校验失败/);
  await assert.rejects(store(state([draft({ attempted: 'yes' })])).load(), /校验失败/);
});
