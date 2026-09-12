import { beforeEach, expect, it } from 'vitest';
import { __resetPendingSaves, useKnowledgeStore as store } from '../../../src/renderer/stores/knowledge.store';
import { useSettingsStore } from '../../../src/renderer/stores/settings.store';
import { deferred, heavyItem } from '../../helpers/heavy-user';

beforeEach(() => {
  __resetPendingSaves(); useSettingsStore.setState({ autoSave: false });
  store.setState({ selectedId: 'A', selectedItem: heavyItem('A'), entries: [], hasUnsavedChanges: false,
    saveError: null, saveConflict: null, detailError: null, detailLoading: false, isSaving: false });
  window.api.knowledge = { ...window.api.knowledge, saveDraft: undefined,
    list: async () => ({ entries: [], total: 0 }), counts: async () => ({} as never) };
});
it('收藏回执保留草稿，继续输入不会丢失原有修改', async () => {
  let saved = heavyItem('A');
  window.api.knowledge.update = async (_id, patch) => (saved = { ...saved, ...patch });
  store.getState().updateSelected({ content: '原文与草稿' });
  await store.getState().toggleFavorite('A');
  expect(store.getState().selectedItem?.content).toBe('原文与草稿');
  store.getState().updateSelected({ content: store.getState().selectedItem!.content + '与继续输入' });
  expect(await store.getState().flushPendingSave()).toBe(true);
  expect(saved.content).toBe('原文与草稿与继续输入');
});
it('读取失败不留下可编辑的旧条目，重试可以恢复', async () => {
  window.api.knowledge.get = async () => { throw new Error('读取失败'); };
  expect(await store.getState().selectItem('B')).toBe(false);
  expect(store.getState().selectedItem).toBeNull();
  expect(store.getState().detailError).toContain('读取失败');
  store.getState().updateSelected({ content: '不能误改 A' });
  expect(store.getState().hasUnsavedChanges).toBe(false);
  window.api.knowledge.get = async () => heavyItem('B');
  expect(await store.getState().selectItem('B')).toBe(true);
  expect(store.getState().selectedItem?.id).toBe('B');
});
it('同一 ID 的乱序加载也只接受最后一次请求', async () => {
  const first = deferred<ReturnType<typeof heavyItem>>(); let calls = 0;
  window.api.knowledge.get = async () => ++calls === 1 ? first.promise : heavyItem('B', '新版本');
  const p1 = store.getState().selectItem('B'); await Promise.resolve(); await Promise.resolve();
  const p2 = store.getState().selectItem('B'); await p2;
  first.resolve(heavyItem('B', '旧版本')); await p1;
  expect(store.getState().selectedItem?.content).toBe('新版本');
});
it('保存失败阻止切换，冲突保留本地和服务端两份内容', async () => {
  store.getState().updateSelected({ content: '本地修改' });
  window.api.knowledge.saveDraft = async () => ({ ok: false, item: heavyItem('A', '外部修改'), conflicts: ['content'], error: '冲突' });
  expect(await store.getState().selectItem('B')).toBe(false);
  expect(store.getState().selectedId).toBe('A');
  expect(store.getState().selectedItem?.content).toBe('本地修改');
  expect(store.getState().saveConflict?.result.item?.content).toBe('外部修改');
  expect(await store.getState().resolveSaveConflict('server')).toBe(true);
  expect(store.getState().selectedItem?.content).toBe('外部修改');
});
