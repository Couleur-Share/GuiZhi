import { useEffect, useState } from 'react';
import { WIKI_COMPILER_VERSION, type WikiContributions } from '@guizhi/shared/types/wiki-compiler';
import { useWikiStore } from '../../stores/wiki.store';
import { Button } from '../ui/Button';
export function WikiUpdateSuggestion({ pageId, promptVersion, updatedAt }: { pageId: string; promptVersion: string; updatedAt: number }) {
  const [revision, setRevision] = useState(0);
  useEffect(() => { const update = () => setRevision(value => value + 1); window.addEventListener("wiki-contributions-updated", update); return () => window.removeEventListener("wiki-contributions-updated", update); }, []);
  const [suggestion, setSuggestion] = useState<{ draft: WikiContributions[number]; reason: string; token: string } | null>(null), [error, setError] = useState('');
  useEffect(() => {
    let current = true; setSuggestion(null); setError('');
    if (window.api.wiki.compiler) void window.api.wiki.compiler({ action: 'suggestion', pageId }).then(result => {
      if (!current) return; if (!result.ok) setError(result.error || '更新建议读取失败'); else setSuggestion(result.suggestion ?? null);
    }).catch(e => { if (current) setError(String(e)); });
    return () => { current = false; };
  }, [pageId, updatedAt, revision]);
  const accept = async () => {
    try {
      const result = await window.api.wiki.compiler({ action: 'suggestion', pageId, accept: true, token: suggestion?.token });
      if (!result.ok) throw new Error(result.error || '更新建议已变化，请刷新');
      setSuggestion(null); await useWikiStore.getState().selectPage(pageId); await useWikiStore.getState().refresh();
    } catch (e) { setError(String(e)); }
  };
  return <div className="space-y-2 text-sm">
    {promptVersion !== WIKI_COMPILER_VERSION ? <p className="text-muted-foreground">旧版覆盖：此页可能仅使用了来源开头。<button className="ml-2 text-primary underline" onClick={() => window.dispatchEvent(new Event('wiki-compile-preview'))}>选择资料升级</button></p> : null}
    {error ? <p role="alert" className="text-destructive">{error}</p> : null}
    {suggestion ? <details className="rounded border border-border p-3"><summary className="cursor-pointer">{suggestion.reason}</summary>
      <p className="my-2 text-muted-foreground">接受后会保存当前版本用于恢复，并采用以下正文及摘要。</p>
      <p>{suggestion.draft.summary}</p><pre className="my-3 max-h-72 overflow-auto whitespace-pre-wrap font-sans">{suggestion.draft.body}</pre>
      <Button onClick={() => void accept()}>接受更新并保留当前版本</Button>
    </details> : null}
  </div>;
}
