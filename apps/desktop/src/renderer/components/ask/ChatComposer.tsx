import { useLayoutEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

/** 受控输入区：输入法确认不发送，流式过程仍允许编辑下一条草稿。 */
export function ChatComposer({ value, onChange, onSend, onStop, running, disabled = false }: {
  value: string; onChange: (value: string) => void; onSend: () => void; onStop: () => void; running: boolean; disabled?: boolean;
}) {
  const { t } = useTranslation();
  const ref = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const input = ref.current;
    if (!input) return;
    const resize = () => { input.style.height = "auto"; input.style.height = `${Math.min(input.scrollHeight, 136)}px`; };
    resize();
    const observer = new ResizeObserver(resize); observer.observe(input);
    return () => observer.disconnect();
  }, [value]);
  return <div className="flex items-end gap-2">
    <textarea ref={ref} rows={2} maxLength={8000} value={value} onChange={e => onChange(e.target.value)}
      aria-label={t("articleAsk.question", "输入问题")}
      placeholder={t("articleAsk.placeholder", "哪里不清楚？Enter 发送，Shift+Enter 换行")}
      onKeyDown={e => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing && !running && !disabled) { e.preventDefault(); onSend(); } }}
      className="min-h-12 min-w-0 flex-1 resize-none rounded-lg border border-border bg-background px-3 py-2 text-sm focus-visible:outline-primary" />
    <button type="button" onClick={running ? onStop : onSend} disabled={!running && (disabled || !value.trim())}
      className="shrink-0 rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50">
      {running ? t("ask.stop", "停止") : t("ask.send", "发送")}
    </button>
  </div>;
}
