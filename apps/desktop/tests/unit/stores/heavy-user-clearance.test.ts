import { expect, it, vi } from 'vitest';
import { useAskStore } from '../../../src/renderer/stores/ask.store';
import { useArticleAskStore } from '../../../src/renderer/stores/article-ask.store';
import { useLibraryWorkflowStore } from '../../../src/renderer/stores/library-workflow.store';
import { useKnowledgeStore } from '../../../src/renderer/stores/knowledge.store';
import { sanitizeCachedMessages } from '../../../src/renderer/stores/conversation-evidence';
import { deferred } from '../../helpers/heavy-user';

it('仅成功清除的来源同步移除两种问答缓存，迟到证据也不能复活', async () => {
  const itemId = 'clearance-review-source';
  const source: any = { ordinal: 1, kind: 'item', refId: itemId, title: '旧来源', evidence: { version: 1, kind: 'item', sourceId: itemId, title: '旧来源', text: '旧证据', fingerprint: 'old', capturedAt: 1 } };
  const message: any = { id: 'm', question: '问题', answer: '保留答案', status: 'done', sources: [source], evidenceSources: [source], steps: [], context: { target: { itemId, selection: '嵌入原文' }, sources: [source] } };
  useAskStore.setState({ articleSession: null, activeSessionId: 'global', messages: [message] });
  useArticleAskStore.setState({ sessionId: 'article', itemId, target: { itemId, view: 'body', selection: '选段' }, messages: [message] });
  useKnowledgeStore.setState({ selectedId: null, selectedItem: null, selectionIds: [], entries: [], page: 1 });
  useLibraryWorkflowStore.setState({ busy: false, results: [] });
  window.api.knowledge = { ...window.api.knowledge, list: async () => ({ entries: [], total: 0 }), counts: async () => ({}), selection: vi.fn(async () => ({ ok: true, results: [{ id: itemId, ok: false, error: '写入失败' }] })) };
  expect(await useLibraryWorkflowStore.getState().execute([itemId], { kind: 'delete', clearEvidence: true })).toBe(false);
  expect(useAskStore.getState().messages[0].sources[0].evidence?.text).toBe('旧证据');
  vi.mocked(window.api.knowledge.selection).mockResolvedValue({ ok: true, results: [{ id: itemId, ok: true }] });
  expect(await useLibraryWorkflowStore.getState().retryFailed()).toBe(true);
  expect(useAskStore.getState().messages[0].sources[0].cleared).toBe(true);
  expect(useAskStore.getState().messages[0].evidenceSources![0].evidence?.text).toBe('');
  expect(useArticleAskStore.getState().messages[0].context).toBeUndefined();
  expect(useArticleAskStore.getState().target?.selection).toBeUndefined();
  expect(useAskStore.getState().messages[0].answer).toBe('保留答案');
  expect(sanitizeCachedMessages([message])[0].sources[0].cleared).toBe(true);
});

it('批量执行中改变筛选，迟到失败回执不重新选中旧范围', async () => {
  const pending = deferred<any>();
  useKnowledgeStore.setState({ selectedId: null, selectedItem: null, selectionIds: ['batch-item'], searchQuery: '' });
  useLibraryWorkflowStore.setState({ busy: false, results: [] });
  window.api.knowledge = { ...window.api.knowledge, list: async () => ({ entries: [], total: 0 }), counts: async () => ({}), selection: vi.fn(() => pending.promise) };
  const running = useLibraryWorkflowStore.getState().execute(['batch-item'], { kind: 'update', patch: { isFavorite: true } });
  await vi.waitFor(() => expect(window.api.knowledge.selection).toHaveBeenCalled());
  useKnowledgeStore.getState().setSearchQuery('新范围');
  pending.resolve({ ok: true, results: [{ id: 'batch-item', ok: false, error: '失败可重试' }] }); await running;
  expect(useKnowledgeStore.getState().selectionIds).toEqual([]);
  expect(useLibraryWorkflowStore.getState().results[0].error).toBe('失败可重试');
});
