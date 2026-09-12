import { clearCachedConversationEvidence } from './conversation-evidence';
import { create } from 'zustand';
import type { KnowledgeItemQuery } from '@guizhi/shared/types';
import type { KnowledgeBatchAction, KnowledgeBatchResult } from '@guizhi/shared/types/knowledge-batch';
import { useKnowledgeStore } from './knowledge.store';
import { runGuardedMutation } from './operation-error.store';

function currentQuery(): KnowledgeItemQuery {
  const s = useKnowledgeStore.getState();
  return { scope: s.scope, collectionId: s.collectionId ?? undefined, tagId: s.tagId ?? undefined,
    platform: s.platform ?? undefined, search: s.searchQuery.trim() || undefined, sortBy: s.sortBy, sortOrder: s.sortOrder };
}
interface Workflow {
  batchId: string | null;
  reviewIds: string[]; skipped: string[]; busy: boolean; notice: string | null;
  results: KnowledgeBatchResult[]; command: KnowledgeBatchAction | null;
  beginReview(id: string): Promise<boolean>;
  navigate(direction: number): Promise<boolean>;
  selectAllMatching(): Promise<void>;
  execute(ids: string[], command: KnowledgeBatchAction, retry?: boolean): Promise<boolean>;
  retryFailed(): Promise<boolean>;
  dismiss(): void;
}
/** 阅读顺序与批量执行独立于当前列表页；冻结后只保留 ID，不搬运正文。 */
export const useLibraryWorkflowStore = create<Workflow>((set, get) => ({
  batchId: null,
  reviewIds: [], skipped: [], busy: false, notice: null, results: [], command: null,
  dismiss: () => set({ notice: null, results: [] }),
  beginReview: async id => {
    if (get().busy) return false;
    set({ busy: true });
    try {
      return await runGuardedMutation('library.review', '打开连续整理', async () => {
        const query = currentQuery();
        const result = await window.api.knowledge.selection({ action: 'freeze', query });
        if (!result.ok || !result.ids) throw new Error(result.error || '读取整理范围失败');
        if (JSON.stringify(query) !== JSON.stringify(currentQuery())) throw new Error('筛选已变化，请重新打开');
        if (!(await useKnowledgeStore.getState().selectItem(id))) throw new Error('请先处理当前保存或加载错误');
        set({ reviewIds: result.ids.includes(id) ? result.ids : [id, ...result.ids], skipped: [] });
      });
    } finally { set({ busy: false }); }
  },
  navigate: async direction => {
    if (get().busy) return false;
    set({ busy: true });
    try {
      return await runGuardedMutation('library.review', '切换资料', async () => {
        const library = useKnowledgeStore.getState();
        if (!(await library.flushPendingSave())) throw new Error(library.saveError || '保存未完成');
        const order = get().reviewIds, step = direction < 0 ? -1 : 1;
        let index = order.indexOf(library.selectedId ?? '') + step;
        for (; index >= 0 && index < order.length; index += step) {
          const id = order[index], item = await window.api.knowledge.get(id);
          if (!item || item.deletedAt != null) {
            set(s => ({ skipped: [...new Set([...s.skipped, id])] })); continue;
          }
          if (!(await useKnowledgeStore.getState().selectItem(id))) throw new Error('资料读取失败，请重试');
          return;
        }
        set({ notice: '已到本次整理范围的边界' });
      });
    } finally { set({ busy: false }); }
  },
  selectAllMatching: async () => {
    await runGuardedMutation('library.selectAll', '选择筛选结果', async () => {
      const query = currentQuery(), result = await window.api.knowledge.selection({ action: 'freeze', query });
      if (!result.ok || !result.ids) throw new Error(result.error || '冻结选择失败');
      if (JSON.stringify(query) !== JSON.stringify(currentQuery())) throw new Error('筛选已变化，请重新选择');
      useKnowledgeStore.getState().setSelection(result.ids);
      set({ notice: `已冻结 ${result.ids.length} 项，之后新增的资料不在本次选择中` });
    });
  },
  execute: async (ids, command, retry = false) => {
    if (get().busy) return false;
    set({ busy: true });
    try {
      const ok = await runGuardedMutation('library.batch', '批量操作', async () => {
        if (!(await useKnowledgeStore.getState().flushPendingSave())) throw new Error('请先处理草稿保存错误');
        const targets = [...new Set(ids)], query = currentQuery();
        if (!retry) set({ results: [], command, batchId: crypto.randomUUID() });
        for (let offset = 0; offset < targets.length; offset += 50) {
          const chunk = targets.slice(offset, offset + 50);
          let results: KnowledgeBatchResult[];
          try {
            const reply = await window.api.knowledge.selection({ action: 'execute', runId: get().batchId ?? undefined, ids: chunk, command });
            if (!reply.ok || !reply.results) throw new Error(reply.error || '批量操作未返回结果');
            results = chunk.map(id => reply.results!.find(result => result.id === id) ?? { id, ok: false, error: '未返回此条结果' });
          } catch (error) { results = chunk.map(id => ({ id, ok: false, error: String(error) })); }
          if (command.kind === 'delete' && command.clearEvidence) clearCachedConversationEvidence(results.filter(row => row.ok).map(row => row.id));
          set(s => ({ results: [...s.results.filter(row => !chunk.includes(row.id)), ...results] }));
        }
        const failures = get().results.filter(row => !row.ok);
        if (JSON.stringify(query) === JSON.stringify(currentQuery())) useKnowledgeStore.getState().setSelection(failures.map(row => row.id));
        await useKnowledgeStore.getState().refreshAll();
        const selected = useKnowledgeStore.getState().selectedId;
        if (selected && targets.includes(selected)) await useKnowledgeStore.getState().selectItem(selected);
        set({ notice: `成功 ${get().results.length - failures.length} 项，失败 ${failures.length} 项` });
      });
      return ok && get().results.every(row => row.ok);
    } finally { set({ busy: false }); }
  },
  retryFailed: () => {
    const { command, results } = get();
    return command ? get().execute(results.filter(row => !row.ok).map(row => row.id), command, true) : Promise.resolve(false);
  },
}));

useKnowledgeStore.subscribe((state, previous) => {
  if (previous.selectionIds.length && (state.scope !== previous.scope || state.collectionId !== previous.collectionId ||
    state.tagId !== previous.tagId || state.platform !== previous.platform || state.searchQuery !== previous.searchQuery)) {
    useLibraryWorkflowStore.setState({ notice: '筛选已变化，原来的跨页选择已清空' });
  }
});
