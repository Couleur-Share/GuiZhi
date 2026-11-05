// @vitest-environment node
import { beforeEach, expect, it, vi } from 'vitest';
import { EventEmitter } from 'node:events';
import { readingRequest } from '../../../src/main/services/reading-sync/transport';

const state = vi.hoisted(() => ({ options: [] as { headers: Record<string, string> }[], code: 200, body: { ok: true } as object }));
vi.mock('../../../src/main/services/net-safety', () => ({ resolvePublicAddress: async () => ({ address: '203.0.113.10', family: 4 }) }));
vi.mock('../../../src/main/services/network-proxy', () => ({ getHttpRequestAgent: () => undefined, hasAnyProxyConfigured: () => false }));
vi.mock('node:https', () => ({ default: { request: (_url: URL, options: { headers: Record<string, string> }, callback: (response: EventEmitter) => void) => {
  state.options.push(options);
  const request = new EventEmitter() as EventEmitter & { end: () => void; destroy: () => void };
  request.end = () => {
    const response = Object.assign(new EventEmitter(), { statusCode: state.code }); callback(response);
    response.emit('data', Buffer.from(JSON.stringify(state.body))); response.emit('end');
  };
  request.destroy = () => request.emit('error', new Error('network'));
  return request;
} } }));
beforeEach(() => { state.options = []; state.code = 200; state.body = { ok: true }; });
it('无正文配对与撤销不声明 JSON；有正文才发送类型和 UTF-8 字节长度', async () => {
  const signal = new AbortController().signal;
  await readingRequest('https://reading.example.com', '/v1/reading/library_id/pairs/pair_id', 'synthetic-credential', 'POST', undefined, signal);
  expect(state.options[0].headers['Content-Type']).toBeUndefined();
  await readingRequest('https://reading.example.com', '/v1/reading/library_id/reader', 'synthetic-credential', 'DELETE', undefined, signal);
  expect(state.options[1].headers['Content-Type']).toBeUndefined();
  await readingRequest('https://reading.example.com', '/v1/reading/library_id/head', 'synthetic-credential', 'PUT', { value: '合成' }, signal);
  expect(state.options[2].headers['Content-Type']).toBe('application/json');
  expect(state.options[2].headers['Content-Length']).toBe(Buffer.byteLength(JSON.stringify({ value: '合成' })));
});
it('错误仅展示固定映射，服务端任意内容和重定向不会外带凭证', async () => {
  state.code = 302; state.body = { error: 'secret-credential-in-server-text' };
  await expect(readingRequest('https://reading.example.com', '/v1/reading/library_id/head', 'synthetic-credential', 'GET', undefined, new AbortController().signal)).rejects.toThrow('HTTP 302');
  expect(state.options).toHaveLength(1);
  state.code = 429; state.body = { error: 'rate_limited' };
  await expect(readingRequest('https://reading.example.com', '/v1/reading/library_id/head', 'synthetic-credential', 'GET', undefined, new AbortController().signal)).rejects.toThrow('60 秒');
});
