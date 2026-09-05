import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ResearchComparison, ResearchRunDetail, ResearchRun } from "@guizhi/shared/types";
import { Select } from "../ui/Select";

export function ResearchComparisonPanel({ detail }: { detail: ResearchRunDetail }) {
  const { t } = useTranslation();
  const [result, setResult] = useState<ResearchComparison | null>(null);
  const [baselines, setBaselines] = useState<ResearchRun[]>([]);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    setResult(null); setError(null);
    void Promise.all([window.api.research.compare(detail.run.id), window.api.research.baselines(detail.run.id)]).then(([comparison, runs]) => { if (active) { setResult(comparison); setBaselines(runs); } }).catch((e) => { if (active) setError(String(e)); });
    return () => { active = false; };
  }, [detail.run.id, detail.run.updatedAt, detail.run.context?.baselineRunId]);
  const choose = async (baseline: string) => {
    try { await window.api.research.setBaseline(detail.run.id, baseline); }
    catch (e) { setError(String(e)); }
  };
  return <div className="space-y-3" data-testid="research-comparison">
    <p className="text-sm text-muted-foreground">{t("research.comparisonHint", "新增发现不等于新发布，未检索到不等于已删除。")}</p>
    {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
    {baselines.length ? <Select ariaLabel={t("research.baseline", "比较基线")} disabled={detail.run.status === "collecting" || detail.run.reportStatus === "generating"} value={result?.baselineRunId ?? ""} onChange={(id) => void choose(id)} options={baselines.map((r) => ({ value: r.id, label: `${new Date(r.rangeTo).toLocaleString()} · ${r.status}` }))} /> : <p className="text-sm">{t("research.noBaseline", "同序列尚无可比较的历史结果。使用“重新研究”创建下一轮。")}</p>}
    {result?.warnings.map((w) => <p key={w} className="text-sm text-muted-foreground">{t(`research.comparisonWarnings.${w}`, w)}</p>)}
    {result?.changes.map((change) => <div key={(change.current ?? change.previous)!.id} className="rounded-xl border border-border p-3 text-sm">
      <span className="mr-2 text-xs text-muted-foreground">{t(`research.change.${change.kind}`, change.kind)}</span>
      <a href={(change.current ?? change.previous)!.url} target="_blank" rel="noreferrer" className="hover:underline">{(change.current ?? change.previous)!.title}</a>
      {change.engagementChanges && Object.keys(change.engagementChanges).length ? <p className="mt-1 text-xs text-muted-foreground">{t("research.engagementDifference", "两次有效互动数值之差")}：{Object.entries(change.engagementChanges).map(([key, value]) => `${key} ${value >= 0 ? "+" : ""}${value}`).join(" · ")}</p> : null}
    </div>)}
  </div>;
}
