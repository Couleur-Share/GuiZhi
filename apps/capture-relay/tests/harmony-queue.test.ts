import { test } from 'node:test';
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createRequire } from 'node:module';
import type { StateStore, CaptureSender } from '../../capture-harmony/entry/src/main/ets/core/DraftQueue';
import type { LocalState as State, Submission, Receipt } from '../../capture-harmony/entry/src/main/ets/core/Protocol';

// 鸿蒙端草稿队列的并发契约：网络请求不占用串行链，同一草稿严格单飞。
// 鸿蒙工程不属于 npm 工作区；通过 tsx 的 CJS 钩子执行同一份客户端逻辑。
const require = createRequire(import.meta.url);
const { DraftQueue }: typeof import('../../capture-harmony/entry/src/main/ets/core/DraftQueue') = require('../../capture-harmony/entry/src/main/ets/core/DraftQueue.ts');
const { LocalState }: typeof import('../../capture-harmony/entry/src/main/ets/core/Protocol') = require('../../capture-harmony/entry/src/main/ets/core/Protocol.ts');
const { RelayRequestError }: typeof import('../../capture-harmony/entry/src/main/ets/core/ConnectionCheck') = require('../../capture-harmony/entry/src/main/ets/core/ConnectionCheck.ts');

class MemoryStore implements StateStore {
  state = new LocalState();
  async save(state: State) { this.state = structuredClone(state); }
}
const receipt = (body: { requestId: string }): Receipt => ({ id: `receipt-${body.requestId}`, requestId: body.requestId,
  createdAt: 1, itemCount: 1, state: 'accepted', progress: null });
const relayError = (status: number, code: string) => new RelayRequestError(`合成失败 ${code}`, status, code);

function gate<T>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
// 让微任务与已排队的串行步骤走完。
const tick = () => new Promise<void>(resolve => setImmediate(resolve));
// 断言某个操作在限定时间内完成：若被网络请求阻塞会得到 'blocked' 而不是让测试卡死。
async function settledWithin(promise: Promise<unknown>, ms = 300): Promise<'done' | 'blocked'> {
  return Promise.race([promise.then(() => 'done' as const), new Promise<'blocked'>(resolve => setTimeout(() => resolve('blocked'), ms))]);
}

async function fixture(count = 1) {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState()), ids: string[] = [];
  for (let i = 0; i < count; i++) {
    await queue.edit(`https://example.com/${i}`, 'auto');
    const requestId = randomUUID(); ids.push(requestId); await queue.enqueue(requestId);
  }
  return { store, queue, ids };
}
const okSender = (calls?: string[]): CaptureSender => ({ async send(body: Submission) { calls?.push(body.requestId); return receipt(body); } });

test('发送在途期间：编辑、新建、分享落盘都不被阻塞，回执到达后只移除已发送的一条', async () => {
  const { store, queue, ids } = await fixture();
  const wait = gate<Receipt>();
  const sending = queue.send(ids[0], 'computer-a', { send: () => wait.promise });
  await tick();
  assert.equal(queue.isSending(ids[0]), true);
  assert.equal(store.state.drafts[0].attempted, true, '发出请求前已先落盘');

  const newId = randomUUID(), shareId = randomUUID();
  assert.equal(await settledWithin(queue.edit('新的想法', 'text')), 'done', '编辑不应等待网络');
  assert.equal(await settledWithin(queue.enqueue(newId)), 'done', '新建不应等待网络');
  assert.equal(await settledWithin(queue.receive(shareId, 'https://example.com/share')), 'done', '分享落盘不应等待网络');
  await queue.edit('继续输入', 'auto');
  assert.deepEqual(queue.snapshot().drafts.map(draft => draft.requestId), [ids[0], newId, shareId]);

  wait.resolve(receipt({ requestId: ids[0] }));
  await sending;
  assert.equal(queue.isSending(ids[0]), false);
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), [newId, shareId], '只移除已发送的草稿');
  assert.equal(store.state.editor, '继续输入', '期间的编辑不被回执步骤覆盖');
  assert.deepEqual(queue.snapshot(), store.state);
});

test('同一草稿并发发送：第二次同步被拒绝，不会发出第二个请求；结束后可再次发送', async () => {
  const { queue, ids } = await fixture();
  const wait = gate<Receipt>(), calls: string[] = [];
  const first = queue.send(ids[0], 'computer-a', { send: body => { calls.push(body.requestId); return wait.promise; } });
  await assert.rejects(queue.send(ids[0], 'computer-a', okSender(calls)), /正在发送/);
  await tick();
  assert.deepEqual(calls, [ids[0]], '只有第一次调用会出网');
  assert.equal(queue.isSending(ids[0]), true);
  wait.reject(relayError(500, 'request_failed'));
  await assert.rejects(first, /合成失败/);
  assert.equal(queue.isSending(ids[0]), false, '失败后在途标记清除');
  await queue.send(ids[0], 'computer-a', okSender(calls));
  assert.deepEqual(calls, [ids[0], ids[0]], '沿用同一请求编号重试');
  assert.equal(queue.snapshot().drafts.length, 0);
});

test('在途草稿不能删除或改为可编辑；结果证明未保存后才放开', async () => {
  const { store, queue, ids } = await fixture();
  const wait = gate<Receipt>();
  const sending = queue.send(ids[0], 'computer-a', { send: () => wait.promise });
  await tick();
  await assert.rejects(queue.remove(ids[0]), /正在发送/);
  await assert.rejects(queue.restoreForEdit(ids[0]), /正在发送/);
  assert.equal(store.state.drafts.length, 1, '被拒绝的操作不改动数据');
  assert.equal(store.state.drafts[0].attempted, true);
  wait.reject(relayError(400, 'no_links'));
  await assert.rejects(sending);
  await queue.restoreForEdit(ids[0]);
  assert.equal(queue.snapshot().editor, 'https://example.com/0');
  assert.equal(queue.snapshot().drafts.length, 0);
});

test('两条草稿同时在途互不干扰：一条成功，一条被限流后保持未发送', async () => {
  const { store, queue, ids } = await fixture(2);
  const [a, b] = [gate<Receipt>(), gate<Receipt>()];
  const sendA = queue.send(ids[0], 'computer-a', { send: () => a.promise });
  const sendB = queue.send(ids[1], 'computer-a', { send: () => b.promise });
  await tick();
  assert.equal(queue.isSending(ids[0]) && queue.isSending(ids[1]), true);
  b.reject(relayError(429, 'rate_limited'));
  await assert.rejects(sendB, /合成失败/);
  assert.equal(queue.isSending(ids[1]), false);
  assert.equal(queue.isSending(ids[0]), true, '另一条仍在途');
  a.resolve(receipt({ requestId: ids[0] }));
  await sendA;
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), [ids[1]]);
  assert.equal(store.state.drafts[0].attempted, false, '限流不代表已保存，恢复发送前状态');
  assert.match(store.state.drafts[0].lastError, /rate_limited/);
});

test('在途期间的编辑不会被失败回写覆盖；失败原因写入最新状态', async () => {
  const { store, queue, ids } = await fixture();
  const wait = gate<Receipt>();
  const sending = queue.send(ids[0], 'computer-a', { send: () => wait.promise });
  await tick();
  await queue.edit('失败期间写下的想法', 'text');
  wait.reject(relayError(0, 'network'));
  await assert.rejects(sending);
  assert.equal(store.state.editor, '失败期间写下的想法');
  assert.equal(store.state.mode, 'text');
  assert.equal(store.state.drafts[0].attempted, true, '网络中断无法证明未保存，保留已尝试');
  assert.match(store.state.drafts[0].lastError, /network/);
});

test('起步校验失败（草稿不存在、绑定其他电脑）也会清除在途标记，不影响后续发送', async () => {
  const { queue, ids } = await fixture();
  await assert.rejects(queue.send('missing-draft', 'computer-a', okSender()), /草稿不存在/);
  assert.equal(queue.isSending('missing-draft'), false);
  await assert.rejects(queue.send(ids[0], '', okSender()), /请先配对/);
  assert.equal(queue.isSending(ids[0]), false);
  await assert.rejects(queue.send(ids[0], 'computer-a', { async send() { throw new Error('合成响应丢失'); } }));
  await assert.rejects(queue.send(ids[0], 'computer-b', okSender()), /另一台电脑/);
  assert.equal(queue.isSending(ids[0]), false);
  await queue.send(ids[0], 'computer-a', okSender());
  assert.equal(queue.snapshot().drafts.length, 0);
});

// ---------- enqueueMany：超过 20 个链接时按份拆成多条草稿 ----------

class CountingStore extends MemoryStore {
  saves = 0;
  async save(state: State) { this.saves += 1; await super.save(state); }
}

test('enqueueMany：多份草稿只落盘一次，顺序与模式保持，输入框随之清空', async () => {
  const store = new CountingStore(), queue = new DraftQueue(store, new LocalState());
  await queue.edit('原始的超长输入', 'auto');
  const before = store.saves, ids = [randomUUID(), randomUUID(), randomUUID()];
  await queue.enqueueMany(ids, ['https://a.com/1\nhttps://a.com/2', 'https://a.com/3', 'https://a.com/4'], 'urls');
  assert.equal(store.saves - before, 1, '三份草稿只写一次盘，不存在中间状态');
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), ids);
  assert.deepEqual(store.state.drafts.map(draft => draft.input), ['https://a.com/1\nhttps://a.com/2', 'https://a.com/3', 'https://a.com/4']);
  assert.deepEqual(store.state.drafts.map(draft => draft.mode), ['urls', 'urls', 'urls']);
  assert.deepEqual(store.state.drafts.map(draft => draft.attempted), [false, false, false]);
  assert.equal(store.state.editor, '');
  assert.deepEqual(queue.snapshot(), store.state);
});

test('enqueueMany 原子性：任一份不合格则一条都不保存，也不清空输入框', async () => {
  const store = new CountingStore(), queue = new DraftQueue(store, new LocalState());
  await queue.edit('要保留的输入', 'auto');
  const saves = store.saves;
  await assert.rejects(queue.enqueueMany([randomUUID(), randomUUID()], ['https://a.com/1', 'a'.repeat(32769)], 'urls'), /32 KiB/);
  await assert.rejects(queue.enqueueMany([randomUUID(), randomUUID()], ['https://a.com/1', '   '], 'urls'), /请输入/);
  await assert.rejects(queue.enqueueMany([], [], 'urls'), /参数无效/);
  await assert.rejects(queue.enqueueMany([randomUUID()], ['https://a.com/1', 'https://a.com/2'], 'urls'), /参数无效/);
  assert.equal(store.saves, saves, '被拒绝的调用不写盘');
  assert.equal(store.state.drafts.length, 0);
  assert.equal(store.state.editor, '要保留的输入');
});

test('enqueueMany 上限：加上现有草稿超过 100 条时整体拒绝，恰好 100 条可以', async () => {
  const { store, queue } = await fixture(98);
  await assert.rejects(queue.enqueueMany([randomUUID(), randomUUID(), randomUUID()], ['https://a.com/1', 'https://a.com/2', 'https://a.com/3'], 'urls'), /100 条/);
  assert.equal(store.state.drafts.length, 98, '一条都不保存');
  await queue.enqueueMany([randomUUID(), randomUUID()], ['https://a.com/1', 'https://a.com/2'], 'urls');
  assert.equal(store.state.drafts.length, 100);
});

test('enqueueMany 不被在途的网络发送阻塞，也不影响那条草稿', async () => {
  const { store, queue, ids } = await fixture();
  const wait = gate<Receipt>();
  const sending = queue.send(ids[0], 'computer-a', { send: () => wait.promise });
  await tick();
  const added = [randomUUID(), randomUUID()];
  assert.equal(await settledWithin(queue.enqueueMany(added, ['https://a.com/1', 'https://a.com/2'], 'urls')), 'done');
  wait.resolve(receipt({ requestId: ids[0] }));
  await sending;
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), added, '只移除已发送的一条');
});