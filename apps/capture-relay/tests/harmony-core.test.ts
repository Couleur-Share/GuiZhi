import { test } from 'node:test';
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createRequire } from 'node:module';
import type { StateStore, CaptureSender } from '../../capture-harmony/entry/src/main/ets/core/DraftQueue';
import type { LocalState as State, Submission, Receipt } from '../../capture-harmony/entry/src/main/ets/core/Protocol';
// 鸿蒙工程不属于 npm 工作区；通过 tsx 的 CJS 钩子执行同一份客户端逻辑。
const require = createRequire(import.meta.url);
const { DraftQueue }: typeof import('../../capture-harmony/entry/src/main/ets/core/DraftQueue') = require('../../capture-harmony/entry/src/main/ets/core/DraftQueue.ts');
const { LocalState, parsePairingLink, validateInput, receiptLabel }: typeof import('../../capture-harmony/entry/src/main/ets/core/Protocol') = require('../../capture-harmony/entry/src/main/ets/core/Protocol.ts');

class MemoryStore implements StateStore {
  state = new LocalState();
  fail = false;
  async save(state: State) {
    if (this.fail) throw new Error('合成落盘故障');
    this.state = structuredClone(state);
  }
}
const receipt = (body: Submission): Receipt => ({ id: 'receipt-1', requestId: body.requestId,
  createdAt: 1, itemCount: 1, state: 'accepted', progress: null });

test('鸿蒙配对链接仅接受完整 HTTPS 二维码，拒绝凭证外发目标混淆', () => {
  const id = randomUUID(), nonce = 'a'.repeat(43);
  assert.deepEqual({ ...parsePairingLink(`https://capture.example.com/#pair=${id}&nonce=${nonce}`) },
    { origin: 'https://capture.example.com', pairingId: id, nonce });
  for (const url of [`http://capture.example.com/#pair=${id}&nonce=${nonce}`,
    `https://capture.example.com@evil.example/#pair=${id}&nonce=${nonce}`,
    `https://capture.example.com/path#pair=${id}&nonce=${nonce}`,
    `https://capture.example.com/#pair=${id}&nonce=${nonce}&nonce=${nonce}`,
    `https://capture.example.com/#pair=${id}&nonce=short`]) assert.throws(() => parsePairingLink(url));
});

test('鸿蒙输入长度按 UTF-8 计算，不能将超限分享静默截断', () => {
  validateInput('中'.repeat(10922)); validateInput('😀'.repeat(8192));
  assert.throws(() => validateInput('中'.repeat(10923)));
  assert.throws(() => validateInput('😀'.repeat(8193)));
  assert.throws(() => validateInput('  '));
});

test('先落盘再发送，丢失响应后重启重试保持原编号与内容', async () => {
  const store = new MemoryStore();
  let queue = new DraftQueue(store, new LocalState());
  await queue.edit('https://example.com/a?token=keep&x=2', 'auto');
  const id = randomUUID(); await queue.enqueue(id);
  const sent: Submission[] = [];
  const sender: CaptureSender = { async send(body) {
    assert.equal(store.state.drafts[0].attempted, true);
    sent.push(structuredClone(body));
    if (sent.length === 1) throw new Error('合成响应丢失');
    return receipt(body);
  } };
  await assert.rejects(queue.send(id, 'computer-a', sender));
  queue = new DraftQueue(store, structuredClone(store.state));
  await assert.rejects(queue.send(id, 'computer-b', sender), /另一台电脑/);
  await queue.send(id, 'computer-a', sender);
  assert.deepEqual(sent[0], sent[1]);
  assert.equal(queue.snapshot().drafts.length, 0);
});

test('落盘失败不发出请求；收到回执但清理失败可幂等重试', async () => {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState());
  await queue.edit('离线文字', 'text'); const id = randomUUID(); await queue.enqueue(id);
  let calls = 0;
  const sender: CaptureSender = { async send(body) { calls++; store.fail = true; return receipt(body); } };
  store.fail = true;
  await assert.rejects(queue.send(id, 'computer-a', sender)); assert.equal(calls, 0);
  store.fail = false;
  await assert.rejects(queue.send(id, 'computer-a', sender)); assert.equal(calls, 1);
  assert.equal(queue.snapshot().drafts[0].requestId, id);
  assert.equal(queue.snapshot().drafts[0].attempted, true);
  store.fail = false;
  await queue.send(id, 'computer-a', { async send(body) { return receipt(body); } });
  assert.equal(store.state.drafts.length, 0);
});

test('分享与编辑并发保存不相互覆盖，持久化失败后仍可继续操作', async () => {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState());
  await Promise.all([queue.edit('尚未写完的想法', 'text'), queue.receive(randomUUID(), 'https://example.com/shared')]);
  assert.equal(queue.snapshot().editor, '尚未写完的想法');
  assert.equal(queue.snapshot().drafts[0].input, 'https://example.com/shared');
  store.fail = true;
  await assert.rejects(queue.edit('保存失败的新文字', 'text'));
  assert.equal(queue.snapshot().editor, '尚未写完的想法');
  store.fail = false; await queue.edit('重试后成功', 'text');
  assert.equal(queue.snapshot().editor, '重试后成功');
});

test('错误回执不删除草稿，混合处理结果不能显示全部成功', async () => {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState());
  const id = randomUUID(); await queue.receive(id, '文字');
  await assert.rejects(queue.send(id, 'computer-a', { async send(body) { return { ...receipt(body), requestId: randomUUID() }; } }));
  assert.equal(queue.snapshot().drafts.length, 1);
  const row = receipt({ requestId: id, input: '', mode: 'auto' }); row.itemCount = 2;
  row.progress = { version: 1, items: [{ index: 0, status: 'completed' }, { index: 1, status: 'failed', error: 'login_required' }] };
  assert.match(receiptLabel(row), /成功 1 · 失败 1/);
  row.progress.items[1].status = 'processing'; assert.match(receiptLabel(row), /电脑处理中/);
});

test('分享草稿可编辑，保护已有输入且禁止修改结果不确定的请求', async () => {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState());
  const id = randomUUID(); await queue.receive(id, '分享原文');
  await queue.edit('尚未保存的输入', 'text');
  await assert.rejects(queue.restoreForEdit(id), /先保存/);
  assert.equal(queue.snapshot().drafts.length, 1);
  await queue.edit('', 'auto'); await queue.restoreForEdit(id);
  assert.equal(queue.snapshot().editor, '分享原文');
  assert.equal(queue.snapshot().drafts.length, 0);
  const nextId = randomUUID(); await queue.enqueue(nextId);
  await assert.rejects(queue.send(nextId, 'computer-a', { async send() { throw new Error('合成超时'); } }));
  await assert.rejects(queue.restoreForEdit(nextId), /不能修改/);
});
