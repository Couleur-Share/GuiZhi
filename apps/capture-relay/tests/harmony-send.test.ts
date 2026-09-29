import { test } from 'node:test';
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import { createServer } from '../src/server';
import { hash, secret, id } from '../src/database';
import type { Limits } from '../src/captures';
import type { StateStore, CaptureSender } from '../../capture-harmony/entry/src/main/ets/core/DraftQueue';
import type { LocalState as State, Submission, Receipt } from '../../capture-harmony/entry/src/main/ets/core/Protocol';

// 发送结果分类的契约测试：鸿蒙端据此决定“内容是否可能已被中转保存”，
// 因此这里一部分用例直接对真实中转服务发请求，证明每一类“可判定未保存”的拒绝确实没有入库。
// 鸿蒙工程不属于 npm 工作区；通过 tsx 的 CJS 钩子执行同一份客户端逻辑。
const require = createRequire(import.meta.url);
const { DraftQueue }: typeof import('../../capture-harmony/entry/src/main/ets/core/DraftQueue') = require('../../capture-harmony/entry/src/main/ets/core/DraftQueue.ts');
const { LocalState, apiErrorMessage }: typeof import('../../capture-harmony/entry/src/main/ets/core/Protocol') = require('../../capture-harmony/entry/src/main/ets/core/Protocol.ts');
const { RelayRequestError }: typeof import('../../capture-harmony/entry/src/main/ets/core/ConnectionCheck') = require('../../capture-harmony/entry/src/main/ets/core/ConnectionCheck.ts');
const { classifySendFailure, isQuotaCode }: typeof import('../../capture-harmony/entry/src/main/ets/core/SendOutcome') = require('../../capture-harmony/entry/src/main/ets/core/SendOutcome.ts');

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
const relayError = (status: number, code: string) => new RelayRequestError(`合成失败 ${code}`, status, code);
const failing = (error: Error): CaptureSender => ({ async send() { throw error; } });

async function draftFixture(input = 'https://example.com/a') {
  const store = new MemoryStore(), queue = new DraftQueue(store, new LocalState());
  await queue.edit(input, 'auto');
  const requestId = randomUUID(); await queue.enqueue(requestId);
  return { store, queue, requestId };
}

// ---------- 分类表 ----------

test('发送失败分类：只有能证明未保存才判为 rejected，其余保守', () => {
  const vectors: [number, string, string][] = [
    [400, 'no_links', 'rejected'], [400, 'invalid_input', 'rejected'], [413, 'request_failed', 'rejected'],
    [429, 'daily_limit', 'rejected'], [429, 'inbox_full', 'rejected'],
    [429, 'rate_limited', 'notProcessed'], [429, 'local_rate_limit', 'notProcessed'], [429, '', 'notProcessed'],
    [401, 'unauthorized', 'notProcessed'], [403, 'forbidden', 'notProcessed'], [426, 'protocol_mismatch', 'notProcessed'],
    [0, 'network', 'uncertain'], [409, 'request_conflict', 'uncertain'], [500, 'request_failed', 'uncertain'],
    [502, 'request_failed', 'uncertain'], [404, 'not_found', 'uncertain'], [415, 'json_required', 'uncertain'],
  ];
  for (const [status, code, expected] of vectors) {
    assert.equal(classifySendFailure(relayError(status, code)), expected, `${status}/${code}`);
  }
  // 非 RelayRequestError（回执格式错误、响应解析失败等）无法证明未保存。
  assert.equal(classifySendFailure(new Error('服务响应格式不正确，内容已保留')), 'uncertain');
});

// ---------- 队列行为 ----------

test('失败后草稿的已尝试标记与可编辑性严格跟随分类结果，内存与落盘一致', async () => {
  const cases: [number, string, boolean][] = [
    [400, 'no_links', false], [400, 'input_too_large', false], [413, 'request_failed', false],
    [429, 'daily_limit', false], [429, 'inbox_full', false],
    [401, 'unauthorized', false], [429, 'rate_limited', false], [426, 'protocol_mismatch', false],
    [0, 'network', true], [500, 'request_failed', true], [409, 'request_conflict', true],
  ];
  for (const [status, code, attempted] of cases) {
    const { store, queue, requestId } = await draftFixture();
    await assert.rejects(queue.send(requestId, 'computer-a', failing(relayError(status, code))), /合成失败/);
    const draft = store.state.drafts[0], label = `${status}/${code}`;
    assert.equal(draft.attempted, attempted, label);
    assert.equal(draft.binding, attempted ? 'computer-a' : '', label);
    assert.match(draft.lastError, new RegExp(code), label);
    assert.ok(draft.lastErrorAt > 0, label);
    assert.deepEqual(queue.snapshot(), store.state, label);
    if (attempted) await assert.rejects(queue.restoreForEdit(requestId), /已尝试发送/, label);
    else {
      await queue.restoreForEdit(requestId);
      assert.equal(queue.snapshot().editor, 'https://example.com/a', label);
    }
  }
});

test('此前已尝试过的草稿：未处理类失败保持原绑定，配额类拒绝可证明未保存', async () => {
  const { store, queue, requestId } = await draftFixture();
  await assert.rejects(queue.send(requestId, 'computer-a', failing(new Error('合成响应丢失'))));
  assert.equal(store.state.drafts[0].attempted, true);
  await assert.rejects(queue.send(requestId, 'computer-a', failing(relayError(429, 'rate_limited'))));
  assert.equal(store.state.drafts[0].attempted, true);
  assert.equal(store.state.drafts[0].binding, 'computer-a');
  await assert.rejects(queue.send(requestId, 'computer-b', failing(relayError(401, 'unauthorized'))), /另一台电脑/);
  // 配额检查在幂等查询之后：同一编号若已保存就不会得到 daily_limit，所以出现即可放开编辑。
  await assert.rejects(queue.send(requestId, 'computer-a', failing(relayError(429, 'daily_limit'))));
  assert.equal(store.state.drafts[0].attempted, false);
  assert.equal(store.state.drafts[0].binding, '');
});

test('失败原因落盘失败时保持已尝试的保守状态，并抛出原始发送错误', async () => {
  const { store, queue, requestId } = await draftFixture();
  const original = relayError(400, 'no_links');
  const sender: CaptureSender = { async send() { store.fail = true; throw original; } };
  await assert.rejects(queue.send(requestId, 'computer-a', sender), (error: unknown) => error === original);
  store.fail = false;
  assert.equal(queue.snapshot().drafts[0].attempted, true);
  assert.equal(store.state.drafts[0].attempted, true);
  await assert.rejects(queue.restoreForEdit(requestId), /已尝试发送/);
});

test('回执无效按不确定处理并记录原因；之后成功发送会移除草稿', async () => {
  const { store, queue, requestId } = await draftFixture();
  const bogus: CaptureSender = { async send(body) { return { ...receipt(body), state: 'bogus' } as unknown as Receipt; } };
  await assert.rejects(queue.send(requestId, 'computer-a', bogus), /接收回执/);
  assert.equal(store.state.drafts[0].attempted, true);
  assert.match(store.state.drafts[0].lastError, /接收回执/);
  await queue.send(requestId, 'computer-a', { async send(body) { return receipt(body); } });
  assert.equal(store.state.drafts.length, 0);
});

// ---------- 真实中转契约 ----------

async function relayFixture(limits?: Limits) {
  const dir = mkdtempSync(join(tmpdir(), 'guizhi-harmony-send-'));
  const { app, db } = await createServer({ database: join(dir, 'relay.db'), origin: 'https://capture.example.com', limits });
  const headers = { 'content-type': 'application/json', 'x-guizhi-protocol': '1' };
  const desktop = secret(), phone = secret(), nonce = secret(), invite = secret();
  const desktopHeaders = { ...headers, authorization: `Bearer ${desktop}` };
  const phoneHeaders = { ...headers, authorization: `Bearer ${phone}` };
  db.run('INSERT INTO invites(hash) VALUES(?)', hash(invite));
  await app.inject({ method: 'POST', url: '/v1/mailboxes', headers, payload: { invite, credential: desktop, requestId: id() } });
  const pair = (await app.inject({ method: 'POST', url: '/v1/pairings', headers: desktopHeaders, payload: { nonce } })).json();
  const claim = await app.inject({ method: 'POST', url: '/v1/pairings/native-claim', headers,
    payload: { pairingId: pair.id, nonce, credential: phone, name: '契约测试手机' } });
  const deviceId = claim.json().id;
  return {
    desktopHeaders,
    confirm: () => app.inject({ method: 'POST', url: `/v1/pairings/${pair.id}/confirm`, headers: desktopHeaders, payload: { deviceId } }),
    stored: () => db.get<{ n: number }>('SELECT count(*) AS n FROM captures')!.n,
    // 与手机端 RelayClient 一致地还原错误（HTTP 状态 + 经过净化的 error 字段），再交给真实分类器。
    async send(payload: unknown, extra: Record<string, string> = {}) {
      const response = await app.inject({ method: 'POST', url: '/v1/captures', headers: { ...phoneHeaders, ...extra }, payload: payload as never });
      if (response.statusCode >= 200 && response.statusCode < 300) return { status: response.statusCode, code: '', outcome: null, body: response.json() };
      const raw = String((response.json() as { error?: string }).error ?? '');
      const code = /^[a-z_]{1,50}$/.test(raw) ? raw : 'request_failed';
      return { status: response.statusCode, code, outcome: classifySendFailure(new RelayRequestError('x', response.statusCode, code)), body: null };
    },
    close: async () => { await app.close(); rmSync(dir, { recursive: true, force: true }); },
  };
}
const submission = (input = 'https://example.com/article', mode = 'auto') => ({ requestId: id(), input, mode });

test('中转的 400/413/未鉴权/协议拒绝都没有保存内容，分类与真实响应一致', async () => {
  const relay = await relayFixture();
  try {
    // 尚未被电脑确认：401，属于“未处理”。
    const unpaired = await relay.send(submission());
    assert.deepEqual([unpaired.status, unpaired.code, unpaired.outcome], [401, 'unauthorized', 'notProcessed']);
    await relay.confirm();

    const rejected: [string, unknown, number, string][] = [
      ['超过 32 KiB', submission('a'.repeat(32769), 'text'), 400, 'input_too_large'],
      ['空白内容', submission('   ', 'text'), 400, 'empty_input'],
      ['链接模式但没有链接', submission('这里没有链接', 'urls'), 400, 'no_links'],
      ['超过 20 个链接', submission(Array.from({ length: 21 }, (_, i) => `https://example.com/${i}`).join('\n')), 400, 'too_many_links'],
      ['请求编号格式错误', { requestId: 'short', input: 'https://example.com', mode: 'auto' }, 400, 'invalid_input'],
      ['整个请求体过大', submission('x'.repeat(250000), 'text'), 413, 'request_failed'],
    ];
    for (const [label, payload, status, code] of rejected) {
      const result = await relay.send(payload);
      assert.deepEqual([result.status, result.code, result.outcome], [status, code, 'rejected'], label);
    }
    // 桌面凭证不能提交；协议版本不符：都在业务之前被拦截。
    const forbidden = await relay.send(submission(), { authorization: relay.desktopHeaders.authorization });
    assert.deepEqual([forbidden.status, forbidden.code, forbidden.outcome], [403, 'forbidden', 'notProcessed']);
    const mismatch = await relay.send(submission(), { 'x-guizhi-protocol': '0' });
    assert.deepEqual([mismatch.status, mismatch.code, mismatch.outcome], [426, 'protocol_mismatch', 'notProcessed']);
    assert.equal(relay.stored(), 0, '以上拒绝都不应保存任何内容');

    // 同一编号内容不同：原内容已保存，客户端必须按“不确定”保留标记。
    const first = submission();
    assert.equal((await relay.send(first)).status, 201);
    const conflict = await relay.send({ ...first, input: 'https://example.com/other' });
    assert.deepEqual([conflict.status, conflict.code, conflict.outcome], [409, 'request_conflict', 'uncertain']);
    assert.equal(relay.stored(), 1);
  } finally { await relay.close(); }
});

test('配额类 429 发生在幂等查询之后：已保存的编号不会收到，出现即证明未保存', async () => {
  for (const [code, limits] of [
    ['daily_limit', { dailyItems: 1, pendingItems: 500, pendingBytes: 5 * 1024 * 1024, retentionDays: 30 }],
    ['inbox_full', { dailyItems: 200, pendingItems: 1, pendingBytes: 5 * 1024 * 1024, retentionDays: 30 }],
  ] as [string, Limits][]) {
    const relay = await relayFixture(limits);
    try {
      await relay.confirm();
      const first = submission();
      const stored = await relay.send(first);
      assert.equal(stored.status, 201);
      const over = await relay.send(submission('https://example.com/second'));
      assert.deepEqual([over.status, over.code, over.outcome], [429, code, 'rejected'], code);
      assert.equal(relay.stored(), 1, `${code} 之后不应新增保存`);
      // 额度已用尽后重试已保存的编号：得到原回执，而不是配额错误。
      const retry = await relay.send(first);
      assert.equal(retry.status, 201, code);
      assert.equal(retry.body.id, stored.body.id, code);
      assert.equal(relay.stored(), 1);
    } finally { await relay.close(); }
  }
});

test('频率限制 429 发生在保存之前：被限流的请求没有入库，分类为未处理', async () => {
  const relay = await relayFixture();
  try {
    await relay.confirm();
    const kept = submission();
    assert.equal((await relay.send(kept)).status, 201);
    for (let i = 1; i < 30; i++) assert.equal((await relay.send(submission(`https://example.com/${i}`))).status, 201);
    assert.equal(relay.stored(), 30);
    const limited = await relay.send(submission('https://example.com/limited'));
    assert.deepEqual([limited.status, limited.code, limited.outcome], [429, 'rate_limited', 'notProcessed']);
    assert.equal(relay.stored(), 30, '被限流的请求不应保存');
    // 频率限制先于幂等查询：已保存的编号重试同样得到 429，所以 notProcessed 必须恢复发送前的标记，而不能推断“未保存”。
    const again = await relay.send(kept);
    assert.deepEqual([again.status, again.code], [429, 'rate_limited']);
  } finally { await relay.close(); }
});

// ---------- 错误码覆盖 ----------

function scanRelayErrorCodes(): Map<string, Set<number>> {
  // 用语法树而不是正则：assert 调用可能跨行，编号/状态也可能是省略的默认值。
  const files = ['server.ts', 'database.ts', 'captures.ts', 'accounts.ts'].map(name => fileURLToPath(new URL(`../src/${name}`, import.meta.url)));
  files.push(fileURLToPath(new URL('../../../packages/shared/utils/capture-submission.ts', import.meta.url)));
  const codes = new Map<string, Set<number>>();
  const add = (code: string, status: number) => { codes.set(code, (codes.get(code) ?? new Set<number>()).add(status)); };
  for (const file of files) {
    const source = ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.ES2022, true);
    const visit = (node: ts.Node): void => {
      if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'assert') {
        const code = node.arguments[1], status = node.arguments[2];
        if (code && ts.isStringLiteralLike(code)) add(code.text, status && ts.isNumericLiteral(status) ? Number(status.text) : 400);
      } else if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'CaptureInputError') {
        const code = node.arguments?.[0];
        if (code && ts.isStringLiteralLike(code)) add(code.text, 400);
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
  }
  return codes;
}

// 手机（Bearer、无 Origin、只调用会话/提交/历史与原生配对）不会收到、因此不需要专门文案的错误码。
const NOT_PHONE_REACHABLE: Record<string, string> = {
  not_found: '仅桌面的投递确认与进度接口',
  capture_expired: '仅桌面的投递确认接口',
  ack_required: '仅桌面的进度接口',
  invalid_progress: '仅桌面的进度接口',
  version_conflict: '仅桌面的进度接口',
  invalid_invite: '仅桌面创建邮箱',
  origin_denied: '仅浏览器流程',
  csrf_denied: '仅浏览器流程',
  native_request_required: '仅浏览器误用原生配对入口',
};

test('中转的每个错误码：要么手机端有专门文案，要么明确登记为手机不可达', () => {
  const scanned = scanRelayErrorCodes();
  assert.ok(scanned.size >= 20, `扫描到的错误码过少（${scanned.size}），扫描方式可能已失效`);
  const fallback = apiErrorMessage('__unmapped__');
  const missing = [...scanned.keys()].filter(code => !(code in NOT_PHONE_REACHABLE) && apiErrorMessage(code) === fallback);
  assert.deepEqual(missing, [], `以下错误码手机端没有文案，请在 apiErrorMessage 补充，或登记到 NOT_PHONE_REACHABLE：${missing.join(', ')}`);
  const stale = Object.keys(NOT_PHONE_REACHABLE).filter(code => !scanned.has(code));
  assert.deepEqual(stale, [], `NOT_PHONE_REACHABLE 中存在中转已不再使用的错误码：${stale.join(', ')}`);
  // 兜底错误码由错误处理器统一产生，扫描不到 assert，单独确认仍在。
  assert.match(readFileSync(fileURLToPath(new URL('../src/server.ts', import.meta.url)), 'utf8'), /"request_failed"/);
});

test('新增的 429 错误码必须先决定分类：配额类或频率类，不能默认落入其中一边', () => {
  for (const [code, statuses] of scanRelayErrorCodes()) {
    if (!statuses.has(429)) continue;
    assert.ok(isQuotaCode(code) || code === 'rate_limited',
      `错误码 ${code} 使用了 HTTP 429：请在 SendOutcome.isQuotaCode 或频率类中明确其分类，并补充本文件的契约用例`);
  }
});
