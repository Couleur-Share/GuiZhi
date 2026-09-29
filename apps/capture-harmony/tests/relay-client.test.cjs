const test = require('node:test');
const assert = require('node:assert/strict');
const { clock, settle, loadRelayClient, RelayRequestError } = require('./support.cjs');

const connection = { origin: 'https://example.invalid', credential: 'test-only' };
const body = { requestId: 'request-id-0001-aaaa', input: 'https://example.com', mode: 'auto' };
const fixture = script => {
  const time = clock();
  const { RelayClient, calls } = loadRelayClient(time, script);
  return { time, calls, client: new RelayClient(connection) };
};
const rejection = promise => promise.then(() => assert.fail('应当失败'), error => error);

test('HTTP 错误携带状态码与净化后的服务端错误码，文案含可复制的原因', async () => {
  const { client } = fixture([{ status: 400, body: { success: false, error: 'no_links' } }]);
  const error = await rejection(client.send(body));
  assert.ok(error instanceof RelayRequestError);
  assert.equal(error.status, 400);
  assert.equal(error.code, 'no_links');
  assert.match(error.message, /没有识别到链接/);
  assert.match(error.message, /HTTP 400 \/ no_links/);
});

test('无法解析或包含异常字符的错误码一律归为 request_failed，不把服务端文本带进界面', async () => {
  for (const raw of ['<html>bad gateway</html>', JSON.stringify({ error: '../../etc/passwd' }),
    JSON.stringify({ error: 'x'.repeat(80) }), JSON.stringify({ error: 42 })]) {
    const { client } = fixture([{ status: 502, body: raw }]);
    const error = await rejection(client.send(body));
    assert.equal(error.status, 502);
    assert.equal(error.code, 'request_failed', raw);
    assert.doesNotMatch(error.message, /etc\/passwd|bad gateway/);
  }
});

test('频率限制 429 启动一分钟本地冷却，冷却期内的请求不出网并带 local_rate_limit', async () => {
  const { client, calls, time } = fixture([{ status: 429, body: { error: 'rate_limited' } }, { status: 200, body: { paired: true } }]);
  const first = await rejection(client.send(body));
  assert.deepEqual([first.status, first.code], [429, 'rate_limited']);
  const blocked = await rejection(client.session());
  assert.deepEqual([blocked.status, blocked.code], [429, 'local_rate_limit']);
  assert.equal(calls.length, 1, '冷却期内不应再发请求');
  time.advance(60001);
  assert.equal((await client.session()).paired, true);
  assert.equal(calls.length, 2);
});

test('配额类 429（今日上限/收件箱已满/设备上限）不启动冷却，避免遮住真实原因', async () => {
  for (const code of ['daily_limit', 'inbox_full', 'device_limit']) {
    const { client, calls } = fixture([{ status: 429, body: { error: code } }, { status: 200, body: { paired: true } }]);
    const first = await rejection(client.send(body));
    assert.deepEqual([first.status, first.code], [429, code]);
    assert.equal((await client.session()).paired, true, code);
    assert.equal(calls.length, 2, `${code} 之后的请求应正常发出`);
  }
});

test('网络超时抛出 code=network、status=0 的错误，供发送分类判为不确定', async () => {
  const { client, time } = fixture(['hang']);
  const pending = rejection(client.send(body));
  await settle();
  [...time.timers.values()][0].fn();
  const error = await pending;
  assert.ok(error instanceof RelayRequestError);
  assert.deepEqual([error.status, error.code], [0, 'network']);
  assert.match(error.message, /网络超时/);
});

test('近期记录分页：不带游标取最新一页，带游标取严格更早的一页；非法游标退回最新一页，小数取整', async () => {
  const cases = [[undefined, '/v1/history'], [0, '/v1/history'], [1700000000123, '/v1/history?before=1700000000123'],
    [1700000000123.9, '/v1/history?before=1700000000123'], [-5, '/v1/history'], [Number.NaN, '/v1/history']];
  for (const [before, path] of cases) {
    const { client, calls } = fixture([{ status: 200, body: [] }]);
    assert.deepEqual(Array.from(await client.history(before)), []);
    assert.equal(calls[0].url, `https://example.invalid${path}`, `before=${before}`);
    assert.equal(calls[0].options.method, 'GET');
    assert.equal(calls[0].options.header.Authorization, 'Bearer test-only', '分页请求同样带设备凭证');
  }
});
