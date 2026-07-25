import {
  CheckCircle2Icon,
  CopyIcon,
  EraserIcon,
  Loader2Icon,
  PlusIcon,
  XCircleIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useImportStore } from "../../stores/import.store";

function CountRow({
  icon,
  label,
  count,
}: {
  icon: React.ReactNode;
  label: string;
  count: number;
}) {
  return (
    <div className="flex items-center gap-2 px-3 py-1.5 text-sm text-sidebar-foreground/70">
      <span className="flex h-4 w-4 shrink-0 items-center justify-center">
        {icon}
      </span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-sidebar-accent/80 text-sidebar-foreground/50 border border-white/5">
        {count}
      </span>
    </div>
  );
}

/**
 * 导入模块侧栏：快速采集入口 + 任务状态摘要 + 清理已完成。
 */
export function SidebarImportsPanel() {
  const { t } = useTranslation();
  const tasks = useImportStore((state) => state.tasks);
  const clearFinished = useImportStore((state) => state.clearFinished);

  const activeCount = tasks.filter(
    (task) => task.status === "pending" || task.status === "processing",
  ).length;
  const failedCount = tasks.filter((task) => task.status === "failed").length;
  const duplicateCount = tasks.filter(
    (task) => task.status === "duplicate",
  ).length;
  const completedCount = tasks.filter(
    (task) => task.status === "completed",
  ).length;
  const finishedCount = tasks.length - activeCount;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3">
      <div className="pt-3">
        <button
          type="button"
          onClick={() =>
            window.dispatchEvent(new CustomEvent("shortcut:newItem"))
          }
          className="flex w-full items-center justify-center gap-2 rounded-lg border border-dashed border-sidebar-border px-3 py-2 text-sm text-sidebar-foreground/70 transition-colors hover:border-primary/50 hover:text-sidebar-foreground"
        >
          <PlusIcon className="h-4 w-4" aria-hidden="true" />
          {t("imports.panelCapture", "快速采集")}
        </button>
      </div>

      <div className="flex items-center gap-2 px-3 pb-1 pt-4">
        <span className="text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/50">
          {t("imports.panelOverview", "任务概览")}
        </span>
        <div className="h-px flex-1 bg-sidebar-border/60" />
      </div>

      <CountRow
        icon={
          <Loader2Icon
            className={`h-4 w-4 ${activeCount > 0 ? "animate-spin text-primary" : ""}`}
            aria-hidden="true"
          />
        }
        label={t("imports.panelActive", "进行中")}
        count={activeCount}
      />
      <CountRow
        icon={<CheckCircle2Icon className="h-4 w-4 text-emerald-500" aria-hidden="true" />}
        label={t("imports.panelCompleted", "已完成")}
        count={completedCount}
      />
      <CountRow
        icon={<CopyIcon className="h-4 w-4 text-amber-500" aria-hidden="true" />}
        label={t("imports.panelDuplicate", "重复")}
        count={duplicateCount}
      />
      <CountRow
        icon={<XCircleIcon className="h-4 w-4 text-destructive" aria-hidden="true" />}
        label={t("imports.panelFailed", "失败")}
        count={failedCount}
      />

      <div className="mt-auto pt-4">
        <button
          type="button"
          onClick={() => void clearFinished()}
          disabled={finishedCount === 0}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-sidebar-border px-3 py-1.5 text-xs text-sidebar-foreground/60 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground disabled:opacity-40"
        >
          <EraserIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("imports.clearFinished", "清理已完成")}
        </button>
      </div>
    </div>
  );
}
