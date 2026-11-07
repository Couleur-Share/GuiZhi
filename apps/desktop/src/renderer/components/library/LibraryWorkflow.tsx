import { ItemConfirmDialog } from "./item-menus";
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLibraryWorkflowStore } from '../../stores/library-workflow.store';
import { useKnowledgeStore } from '../../stores/knowledge.store';
import { Button } from '../ui/Button';

export function ReviewNavigation() {
  const { t } = useTranslation();
  const { reviewIds, skipped, busy, navigate } = useLibraryWorkflowStore();
  const selected = useKnowledgeStore(s => s.selectedId), index = reviewIds.indexOf(selected ?? '');
  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (event.defaultPrevented || target?.closest('input,textarea,[contenteditable="true"],[role="textbox"]')) return;
      if (!event.altKey || event.ctrlKey || event.metaKey || !['ArrowLeft','ArrowRight'].includes(event.key)) return;
      event.preventDefault(); event.stopPropagation(); void navigate(event.key === 'ArrowLeft' ? -1 : 1);
    };
    window.addEventListener('keydown', listener); return () => window.removeEventListener('keydown', listener);
  }, [navigate]);
  if (!reviewIds.length) return null;
  return <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-4 py-2">
    <span className="mr-auto text-xs text-muted-foreground">{index + 1} / {reviewIds.length}{skipped.length ? ` · ${t('library.workflowSkipped', '已跳过 {{count}} 条已删除资料', { count: skipped.length })}` : ''}</span>
    <Button size="sm" variant="ghost" disabled={busy || index <= 0} onClick={() => void navigate(-1)} title="Alt + ←">{t('library.workflowPrevious', '上一条')}</Button>
    <Button size="sm" variant="ghost" disabled={busy || index >= reviewIds.length - 1} onClick={() => void navigate(1)} title="Alt + →">{t('library.workflowNext', '下一条')}</Button>
    <Button size="sm" variant="secondary" disabled={busy || index >= reviewIds.length - 1} onClick={() => void navigate(1)}>{t('library.workflowSaveNext', '保存并下一条')}</Button>
  </div>;
}
export function LibraryBatchResults() {
  const { t } = useTranslation();
  const [confirm, setConfirm] = useState(false);
  const { notice, results, busy, retryFailed, dismiss, command } = useLibraryWorkflowStore();
  if (!notice && !results.length) return null;
  const failures = results.filter(row => !row.ok);
  return <div role="status" className="border-b border-border bg-muted/30 px-4 py-2 text-xs">
    <div className="flex items-center gap-2"><span className="min-w-0 flex-1">{busy ? t('library.workflowProcessing', '处理中，已核对 {{count}} 项', { count: results.length }) : notice}</span>
      {failures.length > 0 && <Button size="sm" variant="secondary" className="shrink-0 whitespace-nowrap" disabled={busy} onClick={() => command?.kind === "delete" ? setConfirm(true) : void retryFailed()}>{t('library.workflowRetryFailed', '仅重试失败项')}</Button>}
      <Button size="sm" variant="ghost" className="shrink-0 whitespace-nowrap" disabled={busy} onClick={dismiss}>{t('library.workflowDismiss', '收起')}</Button></div>
    <ItemConfirmDialog state={confirm ? { kind: "delete-forever", ids: failures.map(row => row.id) } : null} onClose={() => setConfirm(false)} />
    {results.length > 0 && <details><summary className="cursor-pointer">{t('library.workflowResults', '逐条结果（{{count}}）', { count: results.length })}</summary><ul className="max-h-36 overflow-auto">{results.map(row => <li key={row.id}>{row.id}：{row.ok ? t('common.success', '成功') : row.error}</li>)}</ul></details>}
  </div>;
}
