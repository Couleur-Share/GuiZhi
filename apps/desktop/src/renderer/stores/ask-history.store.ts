import { create } from 'zustand';
import type { AskSessionMeta, AskSessionQuery } from '@guizhi/shared/types/ask';
import { runGuardedMutation } from './operation-error.store';
interface HistoryState {
  articles: { id: string; title: string }[]; entries: AskSessionMeta[]; query: AskSessionQuery; nextCursor: string | null; loading: boolean; error: string | null;
  load: (more?: boolean) => Promise<void>; filter: (query: AskSessionQuery) => Promise<void>;
  update: (id: string, patch: { title?: string; pinned?: boolean }) => Promise<boolean>;
}
let generation = 0;
export const useAskHistoryStore = create<HistoryState>((set, get) => ({
  articles: [], entries: [], query: {}, nextCursor: null, loading: false, error: null,
  filter: async query => { ++generation; set({ query, entries: [], nextCursor: null, loading: false }); await get().load(); },
  load: async (more = false) => {
    if (more && (get().loading || !get().nextCursor)) return;
    const request = ++generation;
    const input = { ...get().query, limit: 50, cursor: more ? get().nextCursor : null };
    set({ loading: true, error: null });
    try {
      const result = await window.api.askSession.query(input);
      if (!more) {
        const targetCount = get().entries.length;
        while (result.nextCursor && result.entries.length < targetCount) {
          const next = await window.api.askSession.query({ ...input, cursor: result.nextCursor });
          if (request !== generation) return;
          result.entries.push(...next.entries); result.nextCursor = next.nextCursor;
        }
      }
      if (request !== generation) return;
      set(s => ({ articles: result.articles ?? s.articles, entries: more ? [...s.entries, ...result.entries.filter(e => !s.entries.some(old => old.id === e.id))] : result.entries, nextCursor: result.nextCursor }));
    } catch (e) { if (request === generation) set({ error: e instanceof Error ? e.message : String(e) }); }
    finally { if (request === generation) set({ loading: false }); }
  },
  update: (id, patch) => runGuardedMutation('ask.history', '更新会话', async () => { await window.api.askSession.updateMeta(id, patch); await get().load(); }),
}));
