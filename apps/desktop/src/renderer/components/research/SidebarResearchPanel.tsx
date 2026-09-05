import { PlusIcon, ScanSearchIcon } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useResearchStore } from "../../stores/research.store";
import { RUN_STATUS_NAMES } from "./research-presentation";

const STATUS_COLOR: Record<string, string> = {
  collecting: "bg-blue-500",
  ready: "bg-emerald-500",
  partial: "bg-amber-500",
  failed: "bg-red-500",
  canceled: "bg-muted-foreground",
};

export function SidebarResearchPanel() {
  const { t } = useTranslation();
  const runs = useResearchStore((state) => state.runs);
  const selectedRunId = useResearchStore((state) => state.selectedRunId);
  const refresh = useResearchStore((state) => state.refresh);
  const select = useResearchStore((state) => state.select);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return (
    <div className="flex h-full min-h-0 flex-col px-2 pb-3">
      <div className="pt-3">
        <button
          type="button"
          onClick={() => void select(null)}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          <PlusIcon className="h-4 w-4" />
          {t("research.new", "新建研究")}
        </button>
      </div>
      <div className="flex items-center gap-2 px-3 pb-1 pt-4">
        <span className="text-xs font-medium uppercase tracking-wide text-sidebar-foreground/80">
          {t("research.history", "研究历史")}
        </span>
        <div className="h-px flex-1 bg-sidebar-border/60" />
      </div>
      <div className="min-h-0 flex-1 space-y-1 overflow-y-auto">
        {runs.length === 0 ? (
          <div className="px-3 py-8 text-center text-xs text-muted-foreground">
            <ScanSearchIcon className="mx-auto mb-2 h-5 w-5" />
            {t("research.emptyHistory", "还没有研究记录")}
          </div>
        ) : runs.map((run) => (
          <button
            key={run.id}
            type="button"
            onClick={() => void select(run.id)}
            className={`w-full rounded-lg px-3 py-2 text-left transition-colors ${selectedRunId === run.id ? "bg-primary/15" : "hover:bg-sidebar-accent/50"}`}
          >
            <div className="flex items-center gap-2">
              <span className={`h-2 w-2 shrink-0 rounded-full ${STATUS_COLOR[run.status]}`} />
              <span className="min-w-0 flex-1 truncate text-sm font-medium">{run.topic}</span>
            </div>
            <div className="mt-1 flex justify-between text-[11px] text-muted-foreground">
              <span>{run.sources.length} {t("research.sources", "来源")} · {run.candidateCount} {t("research.items", "条")}</span>
              <span>{new Date(run.updatedAt).toLocaleDateString()}</span>
            </div>
            <p className="mt-1 text-[11px] text-muted-foreground">{RUN_STATUS_NAMES[run.status]}</p>
          </button>
        ))}
      </div>
    </div>
  );
}
