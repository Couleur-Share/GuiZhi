import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AskSessionMeta } from '@guizhi/shared/types';
import { PencilIcon, Trash2Icon } from 'lucide-react';
import { Button } from '../ui/Button';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { useAskStore } from '../../stores/ask.store';
import { HistoryRename } from './HistoryControls';
/** 本文历史独立分页，不改变侧栏全局会话的筛选条件。 */
export function ArticleHistoryList({ itemId, onOpen }: { itemId: string; onOpen: (id: string) => Promise<void> }) {
  const { t } = useTranslation();
  const [entries, setEntries] = useState<AskSessionMeta[]>([]), [query, setQuery] = useState('');
  const [cursor, setCursor] = useState<string | null>(null), [error, setError] = useState(''), [busy, setBusy] = useState(false), [retry, setRetry] = useState(0);
  const [renaming, setRenaming] = useState<AskSessionMeta | null>(null), [deleting, setDeleting] = useState<AskSessionMeta | null>(null);
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
    <input value={query} onChange={e => setQuery(e.target.value)} aria-label={t('articleAsk.historySearch', '搜索本文历史')} placeholder={t('articleAsk.historySearchPlaceholder', '搜索问题、回答或标题')} className="mb-2 w-full rounded border border-border bg-background px-2 py-1 text-xs" />
    {entries.map(s => <div key={s.id} className="group flex items-center gap-0.5">
      <Button size="sm" variant="ghost" className="min-w-0 flex-1 justify-start truncate" disabled={busy} onClick={() => void onOpen(s.id)}>{s.title}</Button>
      <button type="button" onClick={() => setRenaming(s)} title={t('ask.renameSession', '重命名会话')} aria-label={t('ask.renameSession', '重命名会话')} className="hidden h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground group-hover:inline-flex group-focus-within:inline-flex"><PencilIcon className="h-3.5 w-3.5" aria-hidden="true" /></button>
      <button type="button" onClick={() => setDeleting(s)} title={t('ask.deleteSession', '删除会话')} aria-label={t('ask.deleteSession', '删除会话')} className="hidden h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-destructive group-hover:inline-flex group-focus-within:inline-flex"><Trash2Icon className="h-3.5 w-3.5" aria-hidden="true" /></button>
    </div>)}
    {!busy && !entries.length && !error && <p className="text-xs text-muted-foreground">{t('articleAsk.historyNoMatch', '暂无匹配的本文历史')}</p>}
    {error && <p role="alert" className="text-xs text-destructive">{error}<Button size="sm" variant="ghost" onClick={() => setRetry(n => n+1)}>{t('common.retry', '重试')}</Button></p>}
    {cursor && <Button size="sm" variant="secondary" disabled={busy} onClick={() => void more()}>{t('imports.loadMore', '加载更多')}</Button>}
    {busy && <p role="status" className="text-xs">{t('common.loading', '加载中…')}</p>}
    <HistoryRename session={renaming} close={() => { setRenaming(null); setRetry(n => n + 1); }} />
    <ConfirmDialog isOpen={deleting !== null} onClose={() => setDeleting(null)} title={t('ask.deleteSession', '删除会话')}
      message={t('ask.deleteSessionConfirm', '删除会话「{{title}}」？聊天记录将被永久移除。', { title: deleting?.title ?? '' })}
      confirmText={t('common.confirm', '确认')} cancelText={t('common.cancel', '取消')} variant="destructive"
      onConfirm={() => { const target = deleting; setDeleting(null); if (target) void useAskStore.getState().deleteSession(target.id).then(() => setRetry(n => n + 1)); }} />
  </div>;
}
