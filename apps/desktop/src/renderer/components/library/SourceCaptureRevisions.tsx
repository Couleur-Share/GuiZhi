import { useEffect, useState } from 'react';
import { Button } from '../ui/Button';
import { useKnowledgeStore } from '../../stores/knowledge.store';
import { runGuardedMutation } from '../../stores/operation-error.store';
type Revision = { id: string; title: string; content: string; transcript: string | null; reviewJson: string; capturedAt: number };
export function SourceCaptureRevisions({ itemId }: { itemId: string }) {
  const [rows, setRows] = useState<Revision[]>([]), [error, setError] = useState(''), [retry, setRetry] = useState(0);
  useEffect(() => {
    let current = true; setRows([]); setError('');
    if (window.api?.knowledge?.selection) void window.api.knowledge.selection({ action: 'source-versions', id: itemId }).then(result => {
      if (!current) return; if (!result.ok) setError(result.error || '来源版本读取失败'); else setRows(result.versions ?? []);
    }, e => { if (current) setError(String(e)); });
    return () => { current = false; };
  }, [itemId, retry]);
  const adopt = async (row: Revision) => {
    await runGuardedMutation('library.adoptSource', '采用来源正文', async () => {
      const before = useKnowledgeStore.getState();
      if (before.selectedId !== itemId || before.selectedItem?.id !== itemId || !(await before.flushPendingSave())) throw new Error('请先保存当前草稿');
      const current = useKnowledgeStore.getState();
      if (current.selectedId !== itemId || current.selectedItem?.id !== itemId) throw new Error('当前资料已切换');
      // 残缺的新版本先标待复核，再允许进入草稿；完整版本不代替用户确认旧复核。
      const reasons: string[] = JSON.parse(row.reviewJson);
      if (reasons.length) {
        const saved = await window.api.knowledge.update(itemId, { reviewStatus: 'needs_review', reviewReasons: reasons });
        if (!saved) throw new Error('条目已删除');
        useKnowledgeStore.getState().applyServerItem(saved);
        const latest = useKnowledgeStore.getState();
        if (latest.selectedId !== itemId || latest.selectedItem?.content !== current.selectedItem?.content) throw new Error('编辑内容已变化，来源版本仍保留，请重新核对后采用');
      }
      useKnowledgeStore.getState().updateSelected({ content: row.content || row.transcript || '' });
    });
  };
  if (!rows.length && !error) return null;
  return <details className="mx-4 my-2 shrink-0 rounded border border-border p-2 text-xs"><summary className="cursor-pointer">更新的来源版本（{rows.length}）</summary>
    <p className="my-2 text-muted-foreground">新采集内容保存在这里，编辑正文保持不变。可核对后明确采用正文。</p>
    {error && <p role="alert">{error}<Button size="sm" variant="ghost" onClick={() => setRetry(n => n+1)}>重试</Button></p>}
    {rows.map(row => <details key={row.id} className="my-2"><summary className="cursor-pointer">{row.title} · {new Date(row.capturedAt).toLocaleString()}</summary>
      <pre className="max-h-48 overflow-auto whitespace-pre-wrap font-sans">{row.content}{row.transcript ? `\n\n文字稿：\n${row.transcript}` : ''}</pre>
      {row.reviewJson !== '[]' && <p>本次采集缺失：{JSON.parse(row.reviewJson).join('；')}</p>}
      <Button size="sm" variant="secondary" onClick={() => void adopt(row)}>采用此版本正文到草稿</Button>
    </details>)}
  </details>;
}
