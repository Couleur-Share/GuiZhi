import { useTranslation } from "react-i18next";
import { Button } from "../ui/Button";
import { useKnowledgeStore } from '../../stores/knowledge.store';

/** 两份内容在用户明确选择前都保留；比较区只读，不偷换正在编辑的文本。 */
export function DraftConflictNotice() {
  const { t } = useTranslation();
  const conflict = useKnowledgeStore(s => s.saveConflict);
  const local = useKnowledgeStore(s => s.selectedItem);
  const resolve = useKnowledgeStore(s => s.resolveSaveConflict);
  if (!conflict || conflict.itemId !== local?.id) return null;
  return <section role="alert" className="border-b border-border bg-muted p-3 text-sm">
    <p>{t('library.draftConflict', '内容已在其他操作中修改，草稿尚未覆盖已保存版本。')}</p>
    <details><summary className="cursor-pointer py-2">{t('library.draftConflictCompare', '比较两份内容')}</summary>
      {conflict.result.conflicts?.map(field => <div key={field} className="grid grid-cols-2 gap-3">
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap">{t('library.draftConflictMine', '我的修改：')}{local[field]}</pre>
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap">{t('library.draftConflictSaved', '已保存版本：')}{conflict.result.item?.[field]}</pre>
      </div>)}
    </details>
    <div className="flex gap-4"><Button size="sm" variant="secondary" onClick={() => void resolve('local')}>{t('library.draftConflictKeepMine', '保留我的修改并保存')}</Button>
      <Button size="sm" variant="secondary" onClick={() => void resolve('server')}>{t('library.draftConflictUseSaved', '使用已保存版本')}</Button></div>
  </section>;
}
