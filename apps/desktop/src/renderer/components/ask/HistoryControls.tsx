import { useEffect, useState } from 'react';
import { useAskHistoryStore } from '../../stores/ask-history.store';
import { Select } from '../ui/Select';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import type { AskSessionMeta } from '@guizhi/shared/types';
export function HistoryControls() {
  const query = useAskHistoryStore(s => s.query), articles = useAskHistoryStore(s => s.articles), [search, setSearch] = useState(query.search ?? '');
  const filter = useAskHistoryStore(s => s.filter);
  useEffect(() => { const timer = setTimeout(() => { if (search !== (useAskHistoryStore.getState().query.search ?? '')) void filter({ ...useAskHistoryStore.getState().query, search }); }, 250); return () => clearTimeout(timer); }, [search, filter]);
  return <div className="my-2 space-y-2 px-1">
    <input aria-label="搜索历史问题与回答" placeholder="搜索标题、问题与回答" value={search} onChange={e => setSearch(e.target.value)} className="w-full rounded-lg border border-border bg-background px-3 py-2 text-xs" />
    <div className="flex gap-2">
      <Select className="min-w-0 flex-1" ariaLabel="会话范围" value={query.scope || ''} options={[{ value: '', label: '全部会话' }, { value: 'knowledge', label: '全库问答' }, { value: 'article', label: '本文问答' }]} onChange={value => void filter({ ...query, scope: value ? value as 'article' | 'knowledge' : undefined, itemId: undefined })} />
      <Select className="min-w-0 flex-1" ariaLabel="历史时间范围" value={query.from ? (Date.now() - query.from < 8 * 86400000 ? '7' : '30') : ''} options={[{ value: '', label: '全部时间' }, { value: '7', label: '近 7 天' }, { value: '30', label: '近 30 天' }]} onChange={value => void filter({ ...query, from: value ? Date.now() - Number(value) * 86400000 : undefined })} />
    </div>
    {articles.length || query.itemId ? <Select ariaLabel="关联文章" value={query.itemId || ''} options={[{ value: '', label: '全部关联文章' }, ...articles.map(({ id, title }) => ({ value: id, label: title || '文章' }))]} onChange={value => void filter({ ...query, itemId: value || undefined })} /> : null}
  </div>;
}
export function HistoryRename({ session, close }: { session: AskSessionMeta | null; close: () => void }) {
  const [title, setTitle] = useState(''), [busy, setBusy] = useState(false);
  useEffect(() => setTitle(session?.title ?? ''), [session]);
  const save = async () => { if (!session) return; setBusy(true); try { if (await useAskHistoryStore.getState().update(session.id, { title })) close(); } finally { setBusy(false); } };
  return <Modal isOpen={Boolean(session)} onClose={close} title="重命名会话"><input className="mb-3 w-full rounded border border-border bg-background p-2" aria-label="会话标题" value={title} maxLength={200} onChange={e => setTitle(e.target.value)} /><Button disabled={busy || !title.trim()} onClick={() => void save()}>保存</Button></Modal>;
}
