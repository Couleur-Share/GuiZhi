import { beforeEach, expect, it, vi } from 'vitest';
import { useAskStore as ask } from '../../../src/renderer/stores/ask.store';
import { useArticleAskStore } from '../../../src/renderer/stores/article-ask.store';
import { __resetPendingSaves, useKnowledgeStore as library } from '../../../src/renderer/stores/knowledge.store';
import { useSettingsStore } from '../../../src/renderer/stores/settings.store';
import { deferred, heavyItem } from '../../helpers/heavy-user';
import { mergeDraftTags } from '@guizhi/shared/types/knowledge-draft';

beforeEach(() => {
  localStorage.removeItem('guizhi-ask-active-session');
  __resetPendingSaves(); useSettingsStore.setState({ autoSave: false });
  ask.setState({ activeSessionId: 'A', articleSession: null, messages: [{ id: 'a', question: '原问题', answer: '原回答', status: 'done', sources: [], steps: [] }], loadError: null, saveError: null });
  useArticleAskStore.setState({ sessionId: null, messages: [], target: null });
  window.api.askSession = { ...window.api.askSession, save: vi.fn(async input => ({ ...input, createdAt: 1, updatedAt: 2 })) };
  window.api.knowledge = { ...window.api.knowledge, counts: vi.fn(async () => ({} as never)), list: vi.fn(async () => ({ entries: [], total: 0 })) };
  library.setState({ selectedId: 'A', selectedItem: heavyItem('A'), entries: [], saveError: null, saveConflict: null, hasUnsavedChanges: false });
});

it('新建请求晚返回时，不覆盖用户后来选择并编辑的资料', async () => {
  const pending = deferred<ReturnType<typeof heavyItem>>();
  window.api.knowledge.create = vi.fn(() => pending.promise);
  window.api.knowledge.get = async id => heavyItem(id);
  const creation = library.getState().createItem();
  await vi.waitFor(() => expect(window.api.knowledge.create).toHaveBeenCalled());
  await library.getState().selectItem('B'); library.getState().updateSelected({ content: 'B 的未保存草稿' });
  pending.resolve(heavyItem('new')); await creation;
  expect(library.getState().selectedId).toBe('B');
  expect(library.getState().selectedItem?.content).toBe('B 的未保存草稿');
});

it('新建成功清除前一条详情加载错误', async () => {
  library.setState({ detailLoading: false, detailError: '旧条目读取失败' });
  window.api.knowledge.create = async () => heavyItem('new');
  await library.getState().createItem();
  expect(library.getState().selectedId).toBe('new');
  expect(library.getState().detailError).toBeNull();
});

it('新建在途时继续编辑，保存失败仍停在原草稿', async () => {
  const pending = deferred<ReturnType<typeof heavyItem>>();
  window.api.knowledge.create = vi.fn(() => pending.promise);
  window.api.knowledge.saveDraft = async () => ({ ok: false, error: '磁盘已满' });
  const creation = library.getState().createItem();
  await vi.waitFor(() => expect(window.api.knowledge.create).toHaveBeenCalled());
  library.getState().updateSelected({ content: '继续输入的草稿' });
  pending.resolve(heavyItem('new')); await creation;
  expect(library.getState().selectedId).toBe('A');
  expect(library.getState().selectedItem?.content).toBe('继续输入的草稿');
  expect(library.getState().saveError).toBe('磁盘已满');
});

it('重开全库会话再保存，未被最终引用的已读证据仍完整保留', async () => {
  const evidenceSources = [{ ordinal: 2, kind: 'item', refId: 'read-only', title: '实际读过的来源', evidence: { version: 1, text: '当时读到的片段', fingerprint: 'historical' } }];
  window.api.askSession.get = async id => ({ id, title: '历史会话', createdAt: 1, updatedAt: 2,
    messagesJson: JSON.stringify([{ id: 'm', question: '问题', answer: '答案', status: 'done', sources: [], evidenceSources }]) });
  await ask.getState().switchSession('B'); await ask.getState().persist();
  expect(JSON.parse(vi.mocked(window.api.askSession.save).mock.calls.at(-1)![0].messagesJson)[0].evidenceSources).toEqual(evidenceSources);
});

it.each(['{broken', '{}', '[null]', '[{"question":2}]'])('损坏历史 %s 显示读取错误并保留当前回答', async messagesJson => {
  window.api.askSession.get = async id => ({ id, title: '损坏历史', createdAt: 1, updatedAt: 2, messagesJson });
  await ask.getState().switchSession('B');
  expect(ask.getState().activeSessionId).toBe('A');
  expect(ask.getState().messages[0]?.answer).toBe('原回答');
  expect(ask.getState().loadError).toBeTruthy();
  expect(localStorage.getItem('guizhi-ask-active-session')).not.toBe('B');
});

it.each(['local', 'server'] as const)('正文冲突选择 %s 后，保留服务端新增标签与本地标签增量', async choice => {
  const remote = { ...heavyItem('A', '服务端正文'), tags: [{ id: 'remote', name: '外部新增', colorKey: 'gray', createdAt: 1, updatedAt: 1 }] };
  library.getState().updateSelected({ content: '本地正文', tagNames: ['本地新增'] });
  window.api.knowledge.saveDraft = vi.fn()
    .mockResolvedValueOnce({ ok: false, item: remote, conflicts: ['content'], error: '冲突' })
    .mockImplementation(async input => ({ ok: true, item: { ...remote, content: input.patch.content ?? remote.content,
      tags: mergeDraftTags(input.base.tagNames ?? [], input.patch.tagNames ?? [], remote.tags.map(t => t.name)).map(name => ({ ...remote.tags[0], id: name, name })) } }));
  expect(await library.getState().flushPendingSave()).toBe(false);
  expect(await library.getState().resolveSaveConflict(choice)).toBe(true);
  expect(library.getState().selectedItem?.tags.map(t => t.name)).toEqual(['外部新增', '本地新增']);
});
