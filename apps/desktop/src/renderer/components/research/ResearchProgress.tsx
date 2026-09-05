import { useEffect, useState } from "react";
import { AlertTriangleIcon, CheckCircle2Icon, Loader2Icon } from "lucide-react";
import type { ResearchRunDetail, ResearchSource } from "@guizhi/shared/types";
import { PlatformIcon } from "../library/platform-meta";
import { Button } from "../ui/Button";
import { useToast } from "../ui/Toast";
import { elapsedTime, researchSummary, RUN_STATUS_NAMES, SOURCE_NAMES, sourceDescription, sourceStatusName } from "./research-presentation";

export function ResearchProgress({ detail, selectedSource, onSelectSource }: {
  detail: ResearchRunDetail;
  selectedSource: ResearchSource | "all";
  onSelectSource: (source: ResearchSource | "all") => void;
}) {
  const [now, setNow] = useState(Date.now);
  const [loggingIn, setLoggingIn] = useState(false);
  const { showToast } = useToast();
  const running = detail.run.status === "collecting";
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [running]);
  const done = detail.sources.filter((source) => !["pending", "running"].includes(source.status)).length;
  const total = detail.sources.length;
  const Icon = running ? Loader2Icon : detail.run.status === "ready" ? CheckCircle2Icon : AlertTriangleIcon;
  const login = async (source: "douyin" | "xiaohongshu") => {
    setLoggingIn(true);
    try {
      await window.api.research.verifyAndRetrySource(detail.run.id, source);
    } catch (error) {
      showToast("无法开始验证并补采", "error", { detail: error instanceof Error ? error.message : String(error) });
    } finally { setLoggingIn(false); }
  };
  return <div className="space-y-3">
    <div className="rounded-xl border border-border bg-muted/30 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="status" className="flex items-center gap-2 text-sm font-medium">
          <Icon className={`h-4 w-4 text-primary ${running ? "animate-spin" : ""}`} />
          {RUN_STATUS_NAMES[detail.run.status]}
        </div>
        <span className="text-xs tabular-nums text-muted-foreground">{running ? "已用时" : "耗时"} {elapsedTime(detail.run.createdAt, detail.run.completedAt ?? now)}</span>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">{researchSummary(detail)}</p>
      {running ? <div role="progressbar" aria-label="平台采集完成进度" aria-valuemin={0} aria-valuemax={total} aria-valuenow={done} aria-valuetext={`已结束 ${done}/${total} 个平台`} className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full bg-primary transition-[width]" style={{ width: `${total ? done / total * 100 : 0}%` }} />
      </div> : <p className="mt-1 text-xs text-muted-foreground">{detail.candidates.length > 0 ? "可按平台审查候选、批量导入，或在「研究报告」中生成报告。" : "可调整关键词、扩大时间范围，或处理平台提示后重新采集。"}</p>}
    </div>
    <div className="grid gap-2 md:grid-cols-3" aria-label="平台覆盖与进度">
      {detail.run.sources.map((platform) => detail.sources.find((source) => source.source === platform)).filter(Boolean).map((source) => (
        <div key={source.source} className={`rounded-xl border p-3 text-sm ${selectedSource === source.source ? "border-primary bg-primary/5" : "border-border"}`}>
          <button type="button" onClick={() => onSelectSource(selectedSource === source.source ? "all" : source.source)} aria-pressed={selectedSource === source.source} aria-label={`筛选${SOURCE_NAMES[source.source]}候选`} className="flex w-full flex-wrap items-center justify-between gap-2 text-left">
            <span className="flex items-center gap-2 font-medium"><PlatformIcon platform={source.source} className="h-4 w-4" />{SOURCE_NAMES[source.source]}</span>
            <span className={`inline-flex items-center gap-1 text-xs ${source.error ? "text-destructive" : "text-muted-foreground"}`}>{source.status === "running" ? <Loader2Icon className="h-3 w-3 animate-spin" /> : null}{sourceStatusName(source)}</span>
          </button>
          <p className="mt-2 text-xs tabular-nums text-muted-foreground">{source.collectedCount} 条候选{source.startedAt ? ` · ${elapsedTime(source.startedAt, source.finishedAt ?? now)}` : ""}</p>
          <p className={`mt-1 break-words text-xs leading-relaxed ${source.error ? "text-destructive" : "text-muted-foreground"}`}>{sourceDescription(source)}</p>
          {!running && source.source !== "bilibili" && (source.error || source.collectedCount === 0) ? <Button size="sm" variant="ghost" className="mt-2" disabled={loggingIn || detail.run.reportStatus === "generating"} onClick={() => void login(source.source as "douyin" | "xiaohongshu")}>{source.source === "douyin" ? "打开抖音搜索页验证" : "检查登录 / 验证"}</Button> : null}
          {!running && source.source !== "bilibili" && (source.error || source.collectedCount === 0) ? <p className="mt-1 text-xs text-muted-foreground">{detail.run.reportStatus === "generating" ? "请先等待或取消报告生成，再验证并补采。" : "验证通过后自动补采当前平台"}</p> : null}
        </div>
      ))}
    </div>
  </div>;
}
