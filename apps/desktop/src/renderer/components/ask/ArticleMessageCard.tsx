import { EvidenceExcerpt } from "./EvidenceExcerpt";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { ArticleMessage, ArticleSource } from "@guizhi/shared/types/article-ask";
import { AnswerBody } from "./AnswerBody";
import { useToast } from "../ui/Toast";

export function ArticleMessageCard({ message, running, onRetry, onLocate }: {
  message: ArticleMessage; running: boolean; onRetry: () => void; onLocate?: (source: ArticleSource) => Promise<boolean>;
}) {
  const { t } = useTranslation(), { showToast } = useToast();
  const [sourceIndex, setSourceIndex] = useState<number | null>(null), [locationError, setLocationError] = useState(false);
  const source = sourceIndex === null ? null : message.sources[sourceIndex];
  const setSource = (value: ArticleSource | null) => setSourceIndex(value ? message.sources.indexOf(value) : null);
  const open = (value: ArticleSource) => { setSource(value); setLocationError(false); };
  const locate = async () => {
    if (!source) return;
    try {
      if (source.kind === "web" && source.url) {
        const url = new URL(source.url);
        if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("来源链接无效");
        window.open(url.href, "_blank", "noopener,noreferrer"); return;
      }
      setLocationError(!(await onLocate?.(source)));
    } catch { setLocationError(true); }
  };
  const copy = async () => {
    try { await navigator.clipboard.writeText(message.answer); showToast(t("ask.copied", "已复制"), "success"); }
    catch (e) { showToast(t("articleAsk.copyFailed", "复制失败"), "error", { detail: String(e) }); }
  };
  return <article className="space-y-3 border-b border-border/60 pb-5" data-testid="article-ask-message">
    <p className="whitespace-pre-wrap rounded-xl bg-primary/10 p-3 text-sm">{message.question}</p>
    {message.context?.target.selection ? <blockquote className="max-h-32 overflow-auto border-l-2 border-primary/50 pl-3 text-xs text-muted-foreground">{message.context.target.selection}</blockquote> : null}
    {message.status === "running" ? <p role="status" className="animate-pulse text-xs text-muted-foreground">{message.step}</p> : null}
    {message.webStatus === "failed" || message.webStatus === "partial" ? <p role="status" className="text-xs font-medium text-destructive">{t("articleAsk.unverified", "未完成联网查证")}</p> : null}
    {message.warnings.map((warning, i) => <p key={i} className="text-xs text-muted-foreground">{warning}</p>)}
    {message.answer ? <AnswerBody answer={message.answer} ordinals={message.sources.map(s => s.ordinal)} onCitation={n => { const found = message.sources.find(s => s.ordinal === n); if (found) open(found); }} /> : null}
    {message.error ? <p role="alert" className="whitespace-pre-wrap break-words text-sm text-destructive">{message.error}</p> : null}
    {message.truncated ? <p role="status" className="text-xs text-destructive">{t("ask.answerTruncated", "回答达到模型输出长度上限，内容可能不完整。")}</p> : null}
    <div className="flex flex-wrap gap-1.5">{message.sources.map(s => <button type="button" key={s.ordinal} onClick={() => open(s)} className="max-w-full truncate rounded border border-border px-2 py-1 text-left text-xs">[{s.ordinal}] {s.title}</button>)}</div>
    {source?.evidence && locationError ? <p role="status" className="text-xs text-muted-foreground">无法定位当前来源，历史摘录仍可查看。</p> : null}
    {source?.evidence || source?.cleared ? <EvidenceExcerpt evidence={source.evidence} cleared={source.cleared} onCurrent={() => void locate()} onClose={() => setSource(null)} /> : source ? <section className="space-y-2 rounded-lg border border-border bg-muted/30 p-3 text-xs" aria-label={t("articleAsk.excerpt", "引用摘录")}>
      <p className="font-medium">{source.title}</p>
      <p className="max-h-48 overflow-auto whitespace-pre-wrap">{source.text || "未保存历史片段；当前版本可能已变化。"}</p>
      {locationError ? <p role="status">{t("articleAsk.locateFailed", "无法定位到当前来源，以上是回答时保存的摘录。")}</p> : null}
      <button type="button" className="text-primary underline" onClick={() => void locate()}>{source.kind === "web" ? t("articleAsk.openSource", "打开网页来源") : t("articleAsk.locate", "定位到原文")}</button>
      <button type="button" className="ml-3 text-muted-foreground" onClick={() => setSource(null)}>{t("common.close", "关闭")}</button>
    </section> : null}
    <div className="flex gap-3 text-xs text-muted-foreground">
      {message.answer ? <button type="button" onClick={() => void copy()}>{t("ask.copyAnswer", "复制")}</button> : null}
      <button type="button" onClick={onRetry} disabled={running} className="disabled:opacity-50">{t("ask.retryTurn", "重新回答")}</button>
      {message.model ? <span className="ml-auto truncate">{message.model}</span> : null}
    </div>
  </article>;
}
