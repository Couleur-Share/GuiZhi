import { BanIcon, ListChecksIcon, RotateCcwIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportTask } from "@guizhi/shared/types";
import { useImportStore } from "../../stores/import.store";

function BulkAction({
  label,
  count,
  onClick,
  destructive,
  children,
}: {
  label: string;
  count?: number;
  onClick: () => void;
  destructive?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs transition-colors ${
        destructive
          ? "text-destructive hover:bg-destructive/10"
          : "text-foreground hover:bg-muted/60"
      }`}
    >
      {children}
      <span>{label}</span>
      {count === undefined ? null : (
        <span className="tabular-nums text-muted-foreground">{count}</span>
      )}
    </button>
  );
}

/**
 * 批量操作条。按选中项的实际状态分组：重试只作用于失败/已取消的任务——
 * 队列的 retry 会把已完成任务重新跑一遍，一股脑全发过去等于重复采集。
 */
export function ImportsBulkBar({ selected }: { selected: ImportTask[] }) {
  const { t } = useTranslation();
  const clearSelection = useImportStore((state) => state.clearSelection);
  const selectVisible = useImportStore((state) => state.selectVisible);
  const retryTasks = useImportStore((state) => state.retryTasks);
  const cancelTasks = useImportStore((state) => state.cancelTasks);
  const removeTasks = useImportStore((state) => state.removeTasks);

  const isRunning = (task: ImportTask) =>
    task.status === "pending" || task.status === "processing";
  const retryable = selected
    .filter((task) => task.status === "failed" || task.status === "canceled")
    .map((task) => task.id);
  const cancelable = selected.filter(isRunning).map((task) => task.id);
  const removable = selected.filter((task) => !isRunning(task)).map((task) => task.id);

  return (
    <div className="flex h-9 shrink-0 items-center gap-1 border-b border-border bg-primary/5 px-4">
      <button
        type="button"
        onClick={clearSelection}
        title={t("imports.clearSelection", "取消选择")}
        aria-label={t("imports.clearSelection", "取消选择")}
        className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
      >
        <XIcon className="h-4 w-4" aria-hidden="true" />
      </button>
      <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground">
        {t("imports.selectedCount", "已选 {{count}} 项", {
          count: selected.length,
        })}
      </span>

      <BulkAction
        label={t("imports.selectVisible", "全选当前")}
        onClick={selectVisible}
      >
        <ListChecksIcon className="h-4 w-4" aria-hidden="true" />
      </BulkAction>
      {retryable.length > 0 ? (
        <BulkAction
          label={t("imports.retry", "重试")}
          count={retryable.length}
          onClick={() => void retryTasks(retryable)}
        >
          <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />
        </BulkAction>
      ) : null}
      {cancelable.length > 0 ? (
        <BulkAction
          label={t("common.cancel", "取消")}
          count={cancelable.length}
          onClick={() => void cancelTasks(cancelable)}
        >
          <BanIcon className="h-4 w-4" aria-hidden="true" />
        </BulkAction>
      ) : null}
      {removable.length > 0 ? (
        <BulkAction
          destructive
          label={t("imports.removeTask", "从列表中删除")}
          count={removable.length}
          onClick={() => void removeTasks(removable)}
        >
          <XIcon className="h-4 w-4" aria-hidden="true" />
        </BulkAction>
      ) : null}
    </div>
  );
}
