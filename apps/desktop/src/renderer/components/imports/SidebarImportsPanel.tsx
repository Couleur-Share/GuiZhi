import {
  BanIcon,
  CheckCircle2Icon,
  CopyIcon,
  FolderOpenIcon,
  InboxIcon,
  Loader2Icon,
  PlusIcon,
  TriangleAlertIcon,
  XCircleIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  countByFilter,
  useImportStore,
  type ImportFilter,
} from "../../stores/import.store";
import { useToast } from "../ui/Toast";

function FilterRow({
  icon,
  label,
  count,
  active,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors duration-smooth ${
        active
          ? "bg-primary/15 text-primary"
          : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
      }`}
    >
      <span className="flex h-4 w-4 shrink-0 items-center justify-center">
        {icon}
      </span>
      <span
        className={`min-w-0 flex-1 truncate text-left ${active ? "font-medium" : ""}`}
      >
        {label}
      </span>
      <span
        className={`rounded-full border px-1.5 py-0.5 text-[10px] tabular-nums ${
          active
            ? "border-primary/20 bg-primary/10 text-primary/80"
            : "border-white/5 bg-sidebar-accent/80 text-sidebar-foreground/50"
        }`}
      >
        {count}
      </span>
    </button>
  );
}

/**
 * 导入模块侧栏：采集入口 + 状态筛选。
 * 这几行原本只是计数展示，长得像导航却点不动；现在它们就是列表的筛选器。
 */
export function SidebarImportsPanel() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const tasks = useImportStore((state) => state.tasks);
  const filter = useImportStore((state) => state.filter);
  const setFilter = useImportStore((state) => state.setFilter);
  const enqueue = useImportStore((state) => state.enqueue);

  const counts = countByFilter(tasks);

  const importFiles = async () => {
    const files = await window.api.import.selectFiles();
    if (files.length === 0) {
      return;
    }
    try {
      await enqueue(
        files.map((input) => ({
          kind: "file" as const,
          input,
          collectionId: null,
        })),
      );
      showToast(
        t("capture.enqueued", "已加入导入队列（{{count}} 项）", {
          count: files.length,
        }),
        "success",
      );
    } catch (error) {
      showToast(
        t("capture.enqueueFailed", "加入导入队列失败：{{message}}", {
          message: error instanceof Error ? error.message : String(error),
        }),
        "error",
        { detail: error instanceof Error ? error.message : String(error) },
      );
    }
  };

  const rows: {
    id: ImportFilter;
    icon: React.ReactNode;
    label: string;
  }[] = [
    {
      id: "all",
      icon: <InboxIcon className="h-4 w-4" aria-hidden="true" />,
      label: t("imports.panelAll", "全部任务"),
    },
    {
      id: "active",
      icon: (
        <Loader2Icon
          className={`h-4 w-4 ${counts.active > 0 ? "animate-spin text-primary" : ""}`}
          aria-hidden="true"
        />
      ),
      label: t("imports.panelActive", "进行中"),
    },
    {
      id: "completed",
      icon: (
        <CheckCircle2Icon
          className="h-4 w-4 text-emerald-500"
          aria-hidden="true"
        />
      ),
      label: t("imports.panelCompleted", "已完成"),
    },
    // 紧跟「已完成」：它是那一档的子集，隔开摆会让人以为是另一种终态
    {
      id: "degraded",
      icon: (
        <TriangleAlertIcon
          className="h-4 w-4 text-amber-500"
          aria-hidden="true"
        />
      ),
      label: t("imports.panelDegraded", "有缺失"),
    },
    {
      id: "duplicate",
      icon: <CopyIcon className="h-4 w-4 text-amber-500" aria-hidden="true" />,
      label: t("imports.panelDuplicate", "重复"),
    },
    {
      id: "failed",
      icon: (
        <XCircleIcon className="h-4 w-4 text-destructive" aria-hidden="true" />
      ),
      label: t("imports.panelFailed", "失败"),
    },
    {
      id: "canceled",
      icon: <BanIcon className="h-4 w-4" aria-hidden="true" />,
      label: t("imports.panelCanceled", "已取消"),
    },
  ];

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3">
      <div className="space-y-1.5 pt-3">
        <button
          type="button"
          onClick={() =>
            window.dispatchEvent(new CustomEvent("shortcut:newItem"))
          }
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <PlusIcon className="h-4 w-4" aria-hidden="true" />
          {t("imports.panelCapture", "快速采集")}
        </button>
        <button
          type="button"
          onClick={() => void importFiles()}
          className="flex w-full items-center justify-center gap-2 rounded-lg border border-dashed border-sidebar-border px-3 py-2 text-sm text-sidebar-foreground/70 transition-colors hover:border-primary/50 hover:text-sidebar-foreground"
        >
          <FolderOpenIcon className="h-4 w-4" aria-hidden="true" />
          {t("header.newImportFiles", "导入文件")}
        </button>
      </div>

      <div className="flex items-center gap-2 px-3 pb-1 pt-4">
        <span className="text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/50">
          {t("imports.panelOverview", "任务概览")}
        </span>
        <div className="h-px flex-1 bg-sidebar-border/60" />
      </div>

      <div className="space-y-0.5">
        {rows.map((row) => (
          <FilterRow
            key={row.id}
            icon={row.icon}
            label={row.label}
            count={counts[row.id]}
            active={filter === row.id}
            onClick={() => setFilter(row.id)}
          />
        ))}
      </div>
    </div>
  );
}
