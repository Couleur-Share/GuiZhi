import type { EvidenceSnapshot } from '@guizhi/shared/types/evidence';
import { useTranslation } from 'react-i18next';

export function EvidenceExcerpt({ evidence, cleared, onCurrent, onClose }: {
  evidence?: EvidenceSnapshot; cleared?: boolean; onCurrent: () => void; onClose: () => void;
}) {
  const { t } = useTranslation();
  const removed = cleared || evidence?.cleared;
  return <section className="mt-3 space-y-2 rounded-lg border border-border bg-muted/30 p-3 text-xs" aria-label={t('ask.evidenceTitle', '回答时的证据')}>
    <p className="font-medium">{removed ? t('ask.evidenceCleared', '来源已清除') : evidence?.title || t('ask.evidenceNotSaved', '未保存历史片段')}</p>
    {removed ? <p>{t('ask.evidenceClearedHint', '相关证据正文与链接已清除。回答文本保留原样。')}</p> : evidence ? <>
      <p className="text-muted-foreground">{t('ask.evidenceCapturedAt', '回答时的片段 · {{time}}', { time: new Date(evidence.capturedAt).toLocaleString() })}</p>
      {evidence.reviewStatus === 'needs_review' ? <p role="status" className="text-destructive">{t('ask.evidenceNeedsReview', '资料待复核：{{reasons}}', { reasons: evidence.reviewReasons.join('；') || t('ask.evidenceUnverified', '尚未确认完整性') })}</p> : null}
      <p className="max-h-64 overflow-auto whitespace-pre-wrap break-words">{evidence.text}</p>
    </> : <p>{t('ask.evidenceMissing', '此会话没有保存当时的证据；当前版本可能已变化。')}</p>}
    {!removed ? <button type="button" className="text-primary underline" onClick={onCurrent}>{t('ask.evidenceViewCurrent', '查看当前版本')}</button> : null}
    <button type="button" className="ml-3 text-muted-foreground" onClick={onClose}>{t('common.close', '关闭')}</button>
  </section>;
}
