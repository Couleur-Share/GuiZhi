import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ResearchEvidencePacket, ResearchRunDetail } from "@guizhi/shared/types";
import { Button } from "../ui/Button";
import { runGuardedMutation } from "../../stores/operation-error.store";

export function ResearchEvidencePanel({ detail, reference, onOpenItem }: { detail: ResearchRunDetail; reference?: string | null; onOpenItem?: (id: string) => Promise<void> }) {
  const { t } = useTranslation();
  const [packet, setPacket] = useState<ResearchEvidencePacket | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [opened, setOpened] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    setPacket(null); setError(null);
    if (detail.run.context?.savedReportId) void window.api.research.evidence(detail.run.id).then((value) => { if (active) setPacket(value); }).catch((e) => { if (active) setError(String(e)); });
    return () => { active = false; };
  }, [detail.run.id, detail.run.context?.savedReportId]);
  useEffect(() => { setOpened(reference ?? null); }, [reference]);
  const busy = detail.run.status === "collecting" || detail.run.reportStatus === "generating";
  return <div className="space-y-3" data-testid="research-evidence">
    {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
    {packet ? <div className="rounded-xl border border-border p-3">
      <p className="mb-2 text-sm font-medium">{t("research.evidenceReferences", "查看报告引用原文")}</p>
      <div className="flex flex-wrap gap-2">{packet.items.map((item) => <Button key={item.ref} size="sm" variant="secondary" onClick={() => setOpened(opened === item.ref ? null : item.ref)}>[{item.ref}] {item.title.slice(0, 24)}</Button>)}</div>
      {packet.items.filter((item) => item.ref === opened).map((item) => <div key={item.ref} className="mt-3 space-y-2 text-sm">
        <a href={item.url} target="_blank" rel="noreferrer" className="text-primary hover:underline">{item.title}</a>
        {item.excerptTruncated ? <p className="text-muted-foreground">{t("research.excerptLimited", "仅以下摘录进入本次报告上下文")}</p> : null}
        {item.passages?.map((p) => <blockquote key={`${p.kind}:${p.position}`} className="whitespace-pre-wrap border-l-2 border-primary/40 pl-3">
          <p className="text-xs text-muted-foreground">{t(`research.passage.${p.kind}`, p.kind)} · {p.startMs != null ? `${Math.floor(p.startMs / 1000)}s` : `#${p.position + 1}`} {p.author}</p>{p.text}
        </blockquote>)}
      </div>)}
      {packet.localItems?.map((item) => <details key={item.ref} open={reference === item.ref || undefined} className="mt-2 text-sm"><summary>[{item.ref}] {item.title} · {new Date(item.updatedAt).toLocaleString()}</summary><Button size="sm" variant="secondary" onClick={() => void onOpenItem?.(item.itemId)}>{t("research.openLocal", "打开本地条目")}</Button><p className="whitespace-pre-wrap">{item.excerpt}</p></details>)}
    </div> : null}
    {(detail.documents ?? []).map((doc) => <details key={doc.id} className="rounded-xl border border-border p-3">
      <summary className="cursor-pointer text-sm font-medium">{doc.title} · {t(`research.readStatus.${doc.status}`, doc.status)}</summary>
      <p className="mt-2 text-xs text-muted-foreground">{new Date(doc.capturedAt).toLocaleString()} · {doc.warning || doc.error}{doc.truncated ? ` · ${t("research.materialTruncated", "材料超过保存上限，已截断")}` : ""}</p>
      <div className="my-2 flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" disabled={busy} onClick={() => void runGuardedMutation("research.read", "重试精读", async () => { await window.api.research.retryReading(detail.run.id, doc.candidateId); })}>{t("research.retryReading", "重试精读")}</Button>
        <Button size="sm" variant="secondary" disabled={!doc.passages.length || busy} onClick={() => void runGuardedMutation("research.excerpt", "保存研究摘录", async () => { if (doc.savedItemId && onOpenItem) await onOpenItem(doc.savedItemId); else await window.api.research.saveExcerpt(detail.run.id, doc.candidateId); })}>{doc.savedItemId ? t("research.openSavedExcerpt", "已保存 · 打开条目") : t("research.saveExcerpt", "保存研究摘录")}</Button>
      </div>
      <div className="max-h-80 space-y-2 overflow-auto text-sm">{doc.passages.map((p) => <p key={p.position} className="whitespace-pre-wrap"><span className="mr-2 text-xs text-muted-foreground">{t(`research.passage.${p.kind}`, p.kind)}{p.startMs != null ? ` ${Math.floor(p.startMs / 1000)}s` : ""}</span>{p.text}</p>)}</div>
    </details>)}
    {!packet && !detail.documents?.length ? <p className="text-sm text-muted-foreground">{t("research.noReading", "尚无精读材料；快速模式只使用候选元数据。")}</p> : null}
  </div>;
}

export function ResearchCoverage({ detail }: { detail: ResearchRunDetail }) {
  const { t } = useTranslation();
  return <details className="mt-3 rounded-lg border border-border p-2 text-xs text-muted-foreground">
    <summary className="cursor-pointer">{t("research.planCoverage", "查询计划与实际覆盖")} · {t(`research.phase.${detail.run.context?.phase ?? "idle"}`, "旧版研究")}</summary>
    {detail.run.context?.plan ? <div className="my-2"><p>{detail.run.context.plan.queries.join(" / ")}</p>{detail.run.context.plan.fallback ? <p>{detail.run.context.plan.fallback}</p> : null}</div> : null}
    {(detail.attempts ?? []).map((attempt) => <p key={attempt.id} className="mt-1">{attempt.source} · {attempt.query} · {attempt.method} · {new Date(attempt.startedAt).toLocaleTimeString()}–{attempt.finishedAt ? new Date(attempt.finishedAt).toLocaleTimeString() : "…"} · {t("research.coverageCounts", "返回 {{total}}，近期 {{recent}}，日期未知 {{unknown}}", { total: attempt.returnedCount, recent: attempt.inWindowCount, unknown: attempt.unknownDateCount })}{attempt.capped ? ` · ${t("research.sampleCapped", "已达到采样上限")}` : ""}{attempt.error ? ` · ${attempt.failureStage ?? "query"} · ${attempt.error}` : ""}</p>)}
  </details>;
}
