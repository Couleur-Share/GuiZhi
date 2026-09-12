import { afterEach, expect, it, vi } from 'vitest';
const mock = vi.hoisted(() => ({ embed: vi.fn(), config: { id: 'provider-model', model: 'embed', provider: 'openai', apiKey: 'test-only', apiUrl: 'https://example.com' } }));
vi.mock('../../../src/renderer/services/knowledge-ai/embeddings', () => ({ embedTexts: mock.embed, resolveEmbeddingConfig: () => mock.config }));
import { queryVector, resetQueryVectors } from '../../../src/renderer/services/knowledge-ai/query-vectors';
import { hybridSearchItems } from '../../../src/renderer/services/knowledge-ai/hybrid-search';
import { deferred } from '../../helpers/heavy-user';
afterEach(() => { resetQueryVectors(); mock.embed.mockReset(); vi.useRealTimers(); });
it('一个调用者取消不影响共享请求的其他等待者，成功结果可复用', async () => {
  const result = deferred<number[][]>(); let signal!: AbortSignal;
  mock.embed.mockImplementation((_config, _texts, s) => { signal = s; return result.promise; });
  const first = new AbortController(), second = new AbortController();
  const a = queryVector(mock.config, '同一问题', first.signal).catch(e => e.name), b = queryVector(mock.config, '同一问题', second.signal);
  await vi.waitFor(() => expect(mock.embed).toHaveBeenCalledTimes(1));
  // 等待两位调用者完成配置摘要，不依赖异步摘要完成顺序。
  await new Promise(resolve => setTimeout(resolve, 30));
  first.abort(); expect(await a).toBe('AbortError'); expect(signal.aborted).toBe(false);
  result.resolve([[1, 0]]); expect(await b).toEqual([1, 0]);
  expect(await queryVector(mock.config, '同一问题')).toEqual([1, 0]); expect(mock.embed).toHaveBeenCalledTimes(1);
});
it('embedding 挂起 3 秒即返回已有 FTS；最后等待者取消会向下传递取消', async () => {
  let signal!: AbortSignal;
  mock.embed.mockImplementation((_c, _t, s) => { signal = s; return new Promise(() => {}); });
  window.api.knowledge = { ...window.api.knowledge, list: vi.fn(async () => ({ entries: [{ id: 'local', title: '本地命中', snippet: '正文' } as any], total: 1 })) };
  const warning = vi.fn(), began = Date.now();
  const result = await hybridSearchItems('问题', 5, { onWarning: warning });
  expect(Date.now() - began).toBeLessThan(3300); expect(result[0].id).toBe('local');
  expect(signal.aborted).toBe(true); expect(warning).toHaveBeenCalledOnce();
});
