import { beforeEach, expect, it, vi } from 'vitest';
const engine = vi.hoisted(() => vi.fn());
vi.mock('../../../src/renderer/services/knowledge-ai/qa', () => ({ askKnowledgeBase: engine, createQaDeps: () => ({}), QaNoSourceError: class extends Error {} }));
import { useAskStore as store } from '../../../src/renderer/stores/ask.store';
import { useArticleAskStore } from '../../../src/renderer/stores/article-ask.store';
import { deferred } from '../../helpers/heavy-user';

beforeEach(() => {
  store.getState().stop(); engine.mockReset();
  store.setState({ activeSessionId: 'A', messages: [{ id: 'm', question: 'q', answer: 'a', sources: [], steps: [], status: 'done' }], sessions: [], articleSession: null, loadError: null, saveError: null, isRunning: false });
  useArticleAskStore.setState({ sessionId: null, messages: [], target: null });
  window.api.askSession = { ...window.api.askSession, save: vi.fn(async x => ({ ...x, createdAt: 1, updatedAt: 2 })),
    get: vi.fn(async id => ({ id, title: id, createdAt: 1, updatedAt: 2, messagesJson: '[]' })) };
});
it('保存失败阻止新建与切换，当前回答可重试保存', async () => {
  vi.mocked(window.api.askSession.save).mockRejectedValue(new Error('磁盘已满'));
  await store.getState().newSession(); await store.getState().switchSession('B');
  expect(store.getState().activeSessionId).toBe('A'); expect(store.getState().messages[0].answer).toBe('a');
  expect(store.getState().saveError).toBe('磁盘已满');
  vi.mocked(window.api.askSession.save).mockImplementation(async x => ({ ...x, createdAt: 1, updatedAt: 2 }));
  expect(await store.getState().persist()).toBe(true);
});
it('读取失败保留回答并可重试同一目标，乱序请求只接受最后一次', async () => {
  vi.mocked(window.api.askSession.get).mockRejectedValueOnce(new Error('读取失败'));
  await store.getState().switchSession('B'); expect(store.getState().activeSessionId).toBe('A');
  await store.getState().retryLoad(); expect(store.getState().activeSessionId).toBe('B');
  const late = deferred<any>();
  vi.mocked(window.api.askSession.get).mockImplementationOnce(() => late.promise);
  const old = store.getState().switchSession('C');
  await vi.waitFor(() => expect(window.api.askSession.get).toHaveBeenCalledWith('C'));
  await store.getState().switchSession('D');
  late.resolve({ id: 'C', messagesJson: '[]' }); await old;
  expect(store.getState().activeSessionId).toBe('D');
});
it('切换保存中断时的部分回答，迟到流不会修改新会话或新请求状态', async () => {
  const late = deferred<any>(); let stream!: (text: string) => void;
  engine.mockImplementation((_q, _h, _deps, _step, _signal, onText) => { stream = onText; return late.promise; });
  const running = store.getState().ask('继续提问');
  await vi.waitFor(() => expect(engine).toHaveBeenCalled()); stream('已经收到的回答');
  await store.getState().switchSession('B');
  const saved = vi.mocked(window.api.askSession.save).mock.calls.at(-1)![0];
  expect(JSON.parse(saved.messagesJson).at(-1)).toMatchObject({ answer: '已经收到的回答', status: 'error' });
  stream('迟到'); late.resolve({ text: '迟到', sources: [] }); await running;
  expect(store.getState().activeSessionId).toBe('B'); expect(store.getState().messages).toEqual([]);
});
