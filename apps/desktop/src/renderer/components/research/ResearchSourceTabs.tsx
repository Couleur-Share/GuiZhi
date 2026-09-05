import type { ResearchRunDetail, ResearchSource } from "@guizhi/shared/types";
import { AlertCircleIcon, LayersIcon, Loader2Icon } from "lucide-react";
import { PlatformIcon } from "../library/platform-meta";
import { SOURCE_NAMES } from "./research-presentation";

export function ResearchSourceTabs({ detail, value, onChange }: {
  detail: ResearchRunDetail;
  value: ResearchSource | "all";
  onChange: (value: ResearchSource | "all") => void;
}) {
  const sources = ["all", ...detail.run.sources] as const;
  return <div role="tablist" aria-label="候选平台" className="flex shrink-0 flex-wrap gap-1.5 px-5 pt-3">
    {sources.map((source, index) => {
      const sourceRun = detail.sources.find((item) => item.source === source);
      const count = source === "all" ? detail.candidates.length : detail.candidates.filter((item) => item.source === source).length;
      const active = value === source;
      return <button key={source} type="button" role="tab" id={`research-source-${source}`} aria-selected={active} aria-controls="research-candidates" tabIndex={active ? 0 : -1}
        onClick={() => onChange(source)}
        onKeyDown={(event) => {
          const offset = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
          if (!offset && event.key !== "Home" && event.key !== "End") return;
          event.preventDefault();
          const next = event.key === "Home" ? 0 : event.key === "End" ? sources.length - 1 : (index + offset + sources.length) % sources.length;
          onChange(sources[next]);
          event.currentTarget.parentElement?.querySelector<HTMLButtonElement>(`#research-source-${sources[next]}`)?.focus();
        }}
        className={`inline-flex h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg border px-2.5 text-xs font-medium transition-colors duration-quick focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 focus-visible:ring-offset-2 focus-visible:ring-offset-background ${active ? "border-primary/20 bg-primary/10 text-primary" : "border-transparent text-muted-foreground hover:bg-accent hover:text-foreground"}`}>
        {source === "all" ? <LayersIcon className="h-3.5 w-3.5" /> : <PlatformIcon platform={source} className="h-3.5 w-3.5" />}
        {source === "all" ? "综合" : SOURCE_NAMES[source]}
        <span className={`inline-flex h-5 min-w-5 items-center justify-center rounded-md px-1 text-[11px] tabular-nums ${active ? "bg-primary/10 text-primary" : "bg-muted/60 text-muted-foreground"}`}>{count}</span>
        {sourceRun?.status === "running" ? <Loader2Icon aria-label="采集中" className="h-3.5 w-3.5 animate-spin" /> : sourceRun?.error ? <AlertCircleIcon aria-label="平台异常" className="h-3.5 w-3.5 text-destructive" /> : null}
      </button>;
    })}
  </div>;
}
