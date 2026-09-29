import { test } from 'node:test';
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createRequire } from 'node:module';
import type { StateStore, CaptureSender } from '../../capture-harmony/entry/src/main/ets/core/DraftQueue';
import type { LocalState as State, Submission, Receipt } from '../../capture-harmony/entry/src/main/ets/core/Protocol';

// “全部发送”的契约：逐条串行、遇到对后续条目同样成立的错误立即停止、绑定其他电脑的草稿跳过，
// 并且任何失败、跳过或中止都不能被汇报为成功。使用真实的 DraftQueue，只替换网络发送方。
const require = createRequire(import.meta.url);
const core = '../../capture-harmony/entry/src/main/ets/core';
const { DraftQueue }: typeof import('../../capture-harmony/entry/src/main/ets/core/DraftQueue') = require(`${core}/DraftQueue.ts`);
const { LocalState }: typeof import('../../capture-harmony/entry/src/main/ets/core/Protocol') = require(`${core}/Protocol.ts`);
const { RelayRequestError }: typeof import('../../capture-harmony/entry/src/main/ets/core/ConnectionCheck') = require(`${core}/ConnectionCheck.ts`);
const { sendBatch, BatchHooks, batchHeadline, batchReport, SKIP_OTHER_COMPUTER, SKIP_STOPPED }:
  typeof import('../../capture-harmony/entry/src/main/ets/core/BatchSend') = require(`${core}/BatchSend.ts`);
const { previewText }: typeof import('../../capture-harmony/entry/src/main/ets/core/TextPreview') = require(`${core}/TextPreview.ts`);

class MemoryStore implements StateStore {
  state = new LocalState();
  async save(state: State) { this.state = structuredClone(state); }
}
const receipt = (body: Submission): Receipt => ({ id: `receipt-${body.requestId}`, requestId: body.requestId,
  createdAt: 1, itemCount: 1, state: 'accepted', progress: null });
const relayError = (status: number, code: string, message = `合成失败 ${code}`) => new RelayRequestError(message, status, code);
const tick = () => new Promise<void>(resolve => setImmediate(resolve));

async function fixture(inputs: string[]) {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState()), ids: string[] = [];
  for (const input of inputs) {
    await queue.edit(input, 'auto');
    const requestId = randomUUID(); ids.push(requestId); await queue.enqueue(requestId);
  }
  return { store, queue, ids };
}
const links = (count: number) => Array.from({ length: count }, (_, i) => `https://example.com/${i}`);

// 按草稿顺序给出每条的结局：Error 表示失败，其余表示成功；同时记录实际出网的请求。
function scripted(outcomes: (Error | 'ok')[], calls: string[] = []): CaptureSender {
  let index = 0;
  return { async send(body: Submission) {
    calls.push(body.requestId);
    const outcome = outcomes[index++];
    if (outcome instanceof Error) throw outcome;
    return receipt(body);
  } };
}
function recorder() {
  const events: string[] = [], hooks = new BatchHooks();
  hooks.started = requestId => { events.push(`start:${requestId}`); };
  hooks.finished = (requestId, done, total) => { events.push(`end:${requestId}:${done}/${total}`); };
  return { events, hooks };
}

test('全部成功：逐条发送，草稿被移除，进度按序推进，汇总使用成功措辞', async () => {
  const { store, queue, ids } = await fixture(links(3));
  const { events, hooks } = recorder(), calls: string[] = [];
  const result = await sendBatch(queue, 'computer-a', scripted(['ok', 'ok', 'ok'], calls), hooks);
  assert.equal(result.ok, true);
  assert.deepEqual(result.sent, ids);
  assert.equal(result.failed.length + result.skipped.length, 0);
  assert.equal(result.stoppedBy, '');
  assert.deepEqual(calls, ids, '按创建先后串行发送');
  assert.deepEqual(events, [`start:${ids[0]}`, `end:${ids[0]}:1/3`, `start:${ids[1]}`, `end:${ids[1]}:2/3`, `start:${ids[2]}`, `end:${ids[2]}:3/3`]);
  assert.equal(store.state.drafts.length, 0);
  assert.equal(batchHeadline(result), '已发送 3 条，中转已接收，电脑上线后将处理');
  assert.equal(batchReport(result, 0), '');
});

test('混合结果：单条内容被拒绝不影响后续，失败草稿保留并可编辑，汇总不再使用成功措辞', async () => {
  const { store, queue, ids } = await fixture(['https://example.com/a', '没有链接的文字', 'https://example.com/c']);
  const result = await sendBatch(queue, 'computer-a',
    scripted(['ok', relayError(400, 'no_links', '没有识别到链接'), 'ok']), recorder().hooks);
  assert.equal(result.ok, false);
  assert.deepEqual(result.sent, [ids[0], ids[2]]);
  assert.equal(result.failed.length, 1);
  assert.equal(result.failed[0].requestId, ids[1]);
  assert.equal(result.failed[0].preview, '没有链接的文字');
  assert.match(result.failed[0].message, /没有识别到链接/);
  assert.equal(result.stoppedBy, '', '单条内容问题不触发中止');
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), [ids[1]]);
  assert.equal(store.state.drafts[0].attempted, false, '400 可证明未保存，草稿恢复可编辑');
  const headline = batchHeadline(result);
  assert.equal(headline, '已发送 2 条，失败 1 条');
  assert.doesNotMatch(headline, /中转已接收/);
  assert.match(batchReport(result, 0), /失败（1 条）：\n· 没有链接的文字 — 没有识别到链接/);
});

test('限流中止：立即停止发送，未处理的草稿原样保留并注明“尚未发送”', async () => {
  const { store, queue, ids } = await fixture(links(4));
  const calls: string[] = [];
  const result = await sendBatch(queue, 'computer-a',
    scripted(['ok', relayError(429, 'rate_limited', '请求过于频繁，请稍后重试'), 'ok', 'ok'], calls), recorder().hooks);
  assert.equal(result.ok, false);
  assert.deepEqual(result.sent, [ids[0]]);
  assert.equal(result.stoppedBy, 'rate_limited');
  assert.equal(result.stoppedMessage, '请求过于频繁，请稍后重试');
  assert.deepEqual(result.failed.map(item => item.requestId), [ids[1]]);
  assert.deepEqual(result.skipped.map(item => item.requestId), [ids[2], ids[3]]);
  assert.ok(result.skipped.every(item => item.reason === SKIP_STOPPED));
  assert.equal(calls.length, 2, '中止后不再出网');
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), [ids[1], ids[2], ids[3]]);
  assert.ok(store.state.drafts.every(draft => draft.attempted === false), '限流与未处理的草稿都没有“已尝试”标记');
  assert.equal(store.state.drafts[1].lastError, '', '未处理的草稿没有错误记录');
  assert.equal(batchHeadline(result), '已发送 1 条，失败 1 条，跳过 2 条。已停止：请求过于频繁，请稍后重试');
});

test('配额与全局性故障都会中止：今日上限、收件箱已满、凭证失效、网络中断、服务故障', async () => {
  const cases: [Error, string][] = [
    [relayError(429, 'daily_limit'), 'daily_limit'], [relayError(429, 'inbox_full'), 'inbox_full'],
    [relayError(401, 'unauthorized'), 'unauthorized'], [relayError(0, 'network'), 'network'],
    [relayError(500, 'request_failed'), 'request_failed'], [relayError(426, 'protocol_mismatch'), 'protocol_mismatch'],
  ];
  for (const [error, code] of cases) {
    const { queue, ids } = await fixture(links(3)), calls: string[] = [];
    const result = await sendBatch(queue, 'computer-a', scripted([error, 'ok', 'ok'], calls), recorder().hooks);
    assert.equal(result.stoppedBy, code, code);
    assert.equal(result.failed.length, 1, code);
    assert.deepEqual(result.skipped.map(item => item.requestId), [ids[1], ids[2]], code);
    assert.equal(calls.length, 1, `${code} 之后不再出网`);
    assert.equal(result.ok, false, code);
  }
});

test('不确定的失败保留已尝试标记；仅内容自身的 400/413 才继续下一条', async () => {
  const { store, queue, ids } = await fixture(links(3));
  const result = await sendBatch(queue, 'computer-a',
    scripted([relayError(413, 'request_failed'), relayError(0, 'network'), 'ok']), recorder().hooks);
  assert.deepEqual(result.failed.map(item => item.requestId), [ids[0], ids[1]], '413 后继续，网络中断后停止');
  assert.equal(result.stoppedBy, 'network');
  assert.deepEqual(result.skipped.map(item => item.requestId), [ids[2]]);
  assert.equal(store.state.drafts[0].attempted, false);
  assert.equal(store.state.drafts[1].attempted, true, '网络中断无法证明未保存');
  assert.equal(store.state.drafts[1].binding, 'computer-a');
});

test('绑定其他电脑的草稿被跳过并说明原因，其余照常发送', async () => {
  const { store, queue, ids } = await fixture(links(3));
  // 第 2 条此前向另一台电脑发送过但结果不明。
  await assert.rejects(queue.send(ids[1], 'computer-b', { async send() { throw new Error('合成响应丢失'); } }));
  assert.equal(store.state.drafts[1].binding, 'computer-b');
  const calls: string[] = [];
  const result = await sendBatch(queue, 'computer-a', scripted(['ok', 'ok'], calls), recorder().hooks);
  assert.deepEqual(result.sent, [ids[0], ids[2]]);
  assert.deepEqual(result.skipped.map(item => item.requestId), [ids[1]]);
  assert.equal(result.skipped[0].reason, SKIP_OTHER_COMPUTER);
  assert.equal(result.stoppedBy, '', '跳过不触发中止');
  assert.equal(result.ok, false, '有跳过就不是完全成功');
  assert.deepEqual(calls, [ids[0], ids[2]]);
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), [ids[1]]);
  assert.match(batchHeadline(result), /^已发送 2 条，跳过 1 条$/);
  assert.match(batchReport(result, 0), /跳过（1 条）：\n· https:\/\/example\.com\/1 — 曾发往另一台电脑/);
});

test('未连接（没有绑定电脑）：不发出任何请求，草稿标记不变，汇总为失败', async () => {
  const { store, queue, ids } = await fixture(links(2));
  const calls: string[] = [];
  const result = await sendBatch(queue, '', scripted(['ok', 'ok'], calls), recorder().hooks);
  assert.equal(calls.length, 0);
  assert.equal(result.sent.length, 0);
  assert.equal(result.failed.length, 1);
  assert.match(result.failed[0].message, /请先配对电脑/);
  assert.equal(result.stoppedBy, 'error');
  assert.deepEqual(result.skipped.map(item => item.requestId), [ids[1]]);
  assert.ok(store.state.drafts.every(draft => draft.attempted === false && draft.binding === ''));
  assert.equal(result.ok, false);
});

test('批量期间草稿被删除或改为编辑：不算失败，也不阻断后续；期间新增的草稿不属于本批', async () => {
  const { store, queue, ids } = await fixture(links(3));
  const late = randomUUID(), calls: string[] = [];
  const sender: CaptureSender = { async send(body: Submission) {
    calls.push(body.requestId);
    if (body.requestId === ids[0]) {
      await queue.remove(ids[1]);
      await queue.edit('批量期间新写的内容', 'text'); await queue.enqueue(late);
    }
    return receipt(body);
  } };
  const { events, hooks } = recorder();
  const result = await sendBatch(queue, 'computer-a', sender, hooks);
  assert.deepEqual(calls, [ids[0], ids[2]], '被删除的不再发送，新增的不属于本批');
  assert.deepEqual(result.sent, [ids[0], ids[2]]);
  assert.equal(result.failed.length + result.skipped.length, 0);
  assert.equal(result.ok, true);
  assert.equal(result.total, 3);
  assert.ok(events.includes(`end:${ids[1]}:2/3`), '被移除的条目也推进进度');
  assert.deepEqual(store.state.drafts.map(draft => draft.requestId), [late]);
});

test('删除请求恰好排在某条发送之前：该条一旦进入在途就拒绝删除，照常发送，不产生“已删除却被发出”', async () => {
  const { queue, ids } = await fixture(links(3));
  const calls: string[] = [], hooks = new BatchHooks();
  let removal: Promise<void> | undefined;
  // started 之后队列才登记在途状态；此时删除请求已排在串行链上，执行时会发现这条已在途。
  hooks.started = requestId => { if (requestId === ids[1]) removal = queue.remove(ids[1]); };
  const result = await sendBatch(queue, 'computer-a', scripted(['ok', 'ok', 'ok'], calls), hooks);
  await assert.rejects(removal as Promise<void>, /正在发送/);
  assert.deepEqual(calls, ids, '草稿照常发送');
  assert.deepEqual(result.sent, ids);
  assert.equal(result.ok, true);
  assert.equal(queue.snapshot().drafts.length, 0);
});

test('已有单条发送在途的草稿被批量避开：不重复发送，也不算失败', async () => {
  const { queue, ids } = await fixture(links(3));
  let release!: (value: Receipt) => void;
  const single = queue.send(ids[1], 'computer-a', { send: () => new Promise<Receipt>(resolve => { release = resolve; }) });
  await tick();
  const calls: string[] = [];
  const result = await sendBatch(queue, 'computer-a', scripted(['ok', 'ok'], calls), recorder().hooks);
  assert.deepEqual(result.sent, [ids[0], ids[2]]);
  assert.equal(result.failed.length + result.skipped.length, 0);
  assert.equal(result.ok, true);
  release(receipt({ requestId: ids[1], input: '', mode: 'auto' }));
  await single;
  assert.equal(queue.snapshot().drafts.length, 0);
});

test('汇总只内联展示前几条原因，复制版包含全部；预览为首行并截断', async () => {
  const inputs = Array.from({ length: 5 }, (_, i) => `第 ${i + 1} 条\n这是第二行`);
  inputs[4] = `很长的一行${'字'.repeat(60)}`;
  const { queue } = await fixture(inputs);
  const result = await sendBatch(queue, 'computer-a',
    scripted(Array.from({ length: 5 }, () => relayError(400, 'invalid_input', '内容格式不正确'))), recorder().hooks);
  assert.equal(result.failed.length, 5);
  assert.equal(result.stoppedBy, '');
  const inline = batchReport(result, 3), full = batchReport(result, 0);
  assert.equal(inline.split('\n').length, 1 + 3 + 1, '标题 + 3 条 + 提示');
  assert.match(inline, /另有 2 条，点击“复制原因”查看全部/);
  assert.equal(full.split('\n').length, 1 + 5);
  assert.match(full, /· 第 1 条 — 内容格式不正确/);
  assert.match(full, /· 很长的一行字+… — 内容格式不正确/);
  assert.doesNotMatch(full, /这是第二行/, '预览只取首行');
});

test('预览：取首行、合并空白、按字数截断，不在代理对中间截断', () => {
  assert.equal(previewText('  第一行  内容 \n第二行', 20), '第一行 内容');
  assert.equal(previewText('\n\n  \n有内容', 20), '有内容');
  assert.equal(previewText('', 10), '');
  assert.equal(previewText('abcdef', 6), 'abcdef');
  assert.equal(previewText('abcdefg', 6), 'abcdef…');
  // 第 6 个位置正好落在 😀（代理对）中间时，退一位而不是切出半个字符。
  assert.equal(previewText('abcde😀f', 6), 'abcde…');
  assert.equal(previewText('abcd😀f', 6), 'abcd😀…');
});
