import { useEffect, useState } from 'react';
import type { WikiCompileJob, WikiCompilePreview } from '@guizhi/shared/types/wiki-compiler';
import { wikiPreview, wikiModelIdentity, runWikiJob, controlWikiJob } from '../../services/knowledge-ai/wiki-v2';
import { useWikiStore } from '../../stores/wiki.store';
import { Modal } from '../ui/Modal';
import { Checkbox } from '../ui/Checkbox';
import { Button } from '../ui/Button';

const statusLabel = { running: '编译中', paused: '已暂停', cancelled: '已取消', interrupted: '上次中断', failed: '部分失败', completed: '全文完成' };
export function WikiCompileDialog() {
  const [open, setOpen] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [preview, setPreview] = useState<WikiCompilePreview | null>(null), [selected, setSelected] = useState<string[]>([]);
  const [jobs, setJobs] = useState<WikiCompileJob[]>([]), [filter, setFilter] = useState(''), [limit, setLimit] = useState(50);
  const refresh = async () => {
    try {
      const [next, result] = await Promise.all([wikiPreview(), window.api.wiki.compiler({ action: 'jobs' })]);
      if (!result.ok) throw new Error(result.error);
      setPreview(next); setJobs(result.jobs ?? []);
      setSelected(next.entries.filter(e => ['ready','upgrade'].includes(e.state)).slice(0, 10).map(e => e.id));
    } catch (e) { setError(String(e)); }
  };
  useEffect(() => { const show = () => { setOpen(true); setError(''); void refresh(); }; window.addEventListener('wiki-compile-preview', show); return () => window.removeEventListener('wiki-compile-preview', show); }, []);
  const run = async (job: WikiCompileJob) => {
    setBusy(true); useWikiStore.setState({ isCompiling: true });
    try {
      const final = await runWikiJob(job, next => setJobs(list => [next, ...list.filter(j => j.id !== next.id)]));
      if (final.error) setError(final.error);
    } catch (e) { setError(String(e)); await window.api.wiki.compiler({ action: 'control', id: job.id, status: 'paused' }); }
    finally { setBusy(false); useWikiStore.setState({ isCompiling: false }); await refresh(); await useWikiStore.getState().refresh(); }
  };
  const start = async () => {
    setError('');
    try {
      const result = await window.api.wiki.compiler({ action: 'start', model: wikiModelIdentity(), selected: preview!.entries.filter(e => selected.includes(e.id)), allowUpgrade: true });
      if (!result.ok || !result.job) { if (result.preview) { setPreview(result.preview); setSelected(result.preview.entries.filter(e => ['ready','upgrade'].includes(e.state)).map(e => e.id)); } throw new Error(result.error || '任务未启动'); }
      await run(result.job);
    } catch (e) { setError(String(e)); }
  };
  const control = async (job: WikiCompileJob, action: 'paused' | 'cancelled' | 'running') => {
    try {
      if (action === 'running') {
        const result = await window.api.wiki.compiler({ action: 'control', id: job.id, status: action });
        if (!result.ok || !result.job) throw new Error(result.error);
        await run(result.job);
      } else { const next = await controlWikiJob(job.id, action); if (next) setJobs(list => list.map(j => j.id === next.id ? next : j)); }
    } catch (e) { setError(String(e)); }
  };
  const chosen = preview?.entries.filter(e => selected.includes(e.id)) ?? [];
  const requests = chosen.reduce((count, e) => count + e.blocks - e.reusable, 0);
  const visible = preview?.entries.filter(e => e.state !== 'current' && e.title.toLowerCase().includes(filter.toLowerCase())) ?? [];
  return <Modal isOpen={open} onClose={() => setOpen(false)} title="全文编译与历史升级" size="xl">
    <div className="space-y-4 text-sm">
      <p>历史 Wiki 保持可读。请选择本批资料，确认范围后才调用模型；默认选择 10 条。</p>
      {error ? <p role="alert" className="whitespace-pre-wrap text-destructive">{error}</p> : null}
      {preview ? <>
        <p>可执行 {preview.counts.ready} · 需复核 {preview.counts.review} · 等待重试/继续 {preview.counts.waiting} · 待选择升级 {preview.counts.upgrade}</p>
        {!wikiModelIdentity() ? <p>尚未配置 Wiki 模型，请先在 AI 设置中完成配置。</p> : null}
        <input className="w-full rounded border border-border bg-background px-3 py-2" aria-label="筛选编译资料" placeholder="按标题筛选资料" value={filter} onChange={e => { setFilter(e.target.value); setLimit(50); }} />
        <div className="max-h-60 space-y-2 overflow-auto rounded border border-border p-3">
          {visible.slice(0, limit).map(entry => <div key={entry.id}>
            <Checkbox checked={selected.includes(entry.id)} onChange={checked => setSelected(ids => checked ? [...ids, entry.id] : ids.filter(id => id !== entry.id))} disabled={busy || !['ready','upgrade'].includes(entry.state)} label={`${entry.title || '无标题'} · ${entry.blocks} 块${entry.reusable ? `（复用 ${entry.reusable} 块）` : ''}`} />
            {entry.reason ? <p className="ml-6 text-xs text-muted-foreground">{entry.reason}</p> : null}
          </div>)}
          {visible.length > limit ? <Button variant="ghost" onClick={() => setLimit(limit + 50)}>显示更多资料</Button> : null}
        </div>
        <p>本批 {chosen.length} 条 · {chosen.reduce((count, e) => count + e.blocks, 0)} 块 · 预计 {requests}–{requests * 2} 次模型调用（含至多一次格式纠错）。</p>
        <Button disabled={busy || !chosen.length || !wikiModelIdentity()} onClick={() => void start()}>开始所选资料</Button>
      </> : <p role="status">正在计算编译范围…</p>}
      {jobs.length ? <section className="space-y-3 border-t border-border pt-3"><p className="font-medium">编译任务</p>{jobs.slice(0, 10).map(job => <div key={job.id} className="space-y-2 rounded border border-border p-3">
        <p>{statusLabel[job.status]} · 已完成 {job.completed}/{job.total} 块{job.failed ? ` · 失败 ${job.failed} 块` : ''}</p>
        {job.error ? <p className="text-destructive">{job.error}</p> : null}
        {job.status === 'running' ? <div className="flex gap-2"><Button variant="secondary" onClick={() => void control(job, 'paused')}>暂停后续请求</Button><Button variant="ghost" onClick={() => void control(job, 'cancelled')}>取消任务</Button></div> : job.status !== 'completed' ? <Button variant="secondary" disabled={busy} onClick={() => void control(job, 'running')}>继续未完成分块</Button> : null}
      </div>)}</section> : null}
    </div>
  </Modal>;
}
