import { useEffect, useState } from 'react';
import type { AskSessionMeta } from '@guizhi/shared/types';
import { Button } from '../ui/Button';
/** 本文历史独立分页，不改变侧栏全局会话的筛选条件。 */
export function ArticleHistoryList({ itemId, onOpen }: { itemId: string; onOpen: (id: string) => Promise<void> }) {
  const [entries, setEntries] = useState<AskSessionMeta[]>([]), [query, setQuery] = useState('');
  const [cursor, setCursor] = useState<string | null>(null), [error, setError] = useState(''), [busy, setBusy] = useState(false), [retry, setRetry] = useState(0);
  useEffect(() => {
    let current = true;
    setEntries([]); setCursor(null); setError(''); setBusy(true);
    const timer = setTimeout(() => {
      const load = window.api.askSession.query
        ? window.api.askSession.query({ scope: 'article', itemId, search: query, limit: 50 })
        : window.api.askSession.list({ scope: 'article', itemId }).then(entries => ({ entries, nextCursor: null }));
      void load.then(result => { if (current) { setEntries(result.entries); setCursor(result.nextCursor); } }, e => { if (current) setError(String(e)); }).finally(() => { if (current) setBusy(false); });
    }, 200);
    return () => { current = false; clearTimeout(timer); };
  }, [itemId, query, retry]);
  const more = async () => {
    setBusy(true); setError('');
    try { const result = await window.api.askSession.query({ scope: 'article', itemId, search: query, limit: 50, cursor }); setEntries(old => [...old, ...result.entries.filter(e => !old.some(o => o.id === e.id))]); setCursor(result.nextCursor); }
    catch (e) { setError(String(e)); } finally { setBusy(false); }
  };
  return <div className="max-h-56 shrink-0 overflow-auto border-b border-border p-2">
    <input value={query} onChange={e => setQuery(e.target.value)} aria-label="搜索本文历史" placeholder="搜索问题、回答或标题" className="mb-2 w-full rounded border border-border bg-background px-2 py-1 text-xs" />
    {entries.map(s => <Button key={s.id} size="sm" variant="ghost" className="w-full justify-start truncate" disabled={busy} onClick={() => void onOpen(s.id)}>{s.title}</Button>)}
    {!busy && !entries.length && !error && <p className="text-xs text-muted-foreground">暂无匹配的本文历史</p>}
    {error && <p role="alert" className="text-xs text-destructive">{error}<Button size="sm" variant="ghost" onClick={() => setRetry(n => n+1)}>重试</Button></p>}
    {cursor && <Button size="sm" variant="secondary" disabled={busy} onClick={() => void more()}>加载更多</Button>}
    {busy && <p role="status" className="text-xs">加载中…</p>}
  </div>;
}
