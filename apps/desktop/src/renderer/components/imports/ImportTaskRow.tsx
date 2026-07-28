import type { ReactNode } from "react";
import {
  BanIcon,
  CheckIcon,
  CheckCircle2Icon,
  CopyIcon,
  CopyPlusIcon,
  ExternalLinkIcon,
  FileTextIcon,
  GlobeIcon,
  Loader2Icon,
  RotateCcwIcon,
  SettingsIcon,
  TriangleAlertIcon,
  TypeIcon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportTask } from "@guizhi/shared/types";
import { useImportStore } from "../../stores/import.store";
import { useUIStore } from "../../stores/ui.store";
import { formatItemTime, getItemTypeMeta } from "../library/type-meta";
import {
  formatDuration,
  getStageLabel,
  needsCaptureToolSetup,
  resolveTaskFolder,
  resolveTaskHost,
  STALL_THRESHOLD_MS,
} from "./import-task-meta";

function sourceKindIcon(kind: ImportTask["sourceKind"]): ReactNode {
  if (kind === "url") {
    return <GlobeIcon className="h-3.5 w-3.5" aria-hidden="true" />;
  }
  if (kind === "file") {
    return <FileTextIcon className="h-3.5 w-3.5" aria-hidden="true" />;
  }
  return <TypeIcon className="h-3.5 w-3.5" aria-hidden="true" />;
}

/**
 * 前导槽：默认显示条目类型图标，悬停或进入多选后原地换成勾选框。
 * 图标与勾选框叠在同一个 16px 方格里交叉淡入，行内元素不会因此位移。
 */
function LeadingSlot({
  task,
  isChecked,
  hasSelection,
  onToggle,
}: {
  task: ImportTask;
  isChecked: boolean;
  hasSelection: boolean;
  onToggle: (event: React.MouseEvent) => void;
}) {
  const { t } = useTranslation();
  const typeMeta = task.itemType ? getItemTypeMeta(task.itemType) : null;
  const label = typeMeta
    ? t(typeMeta.labelKey, typeMeta.fallback)
    : t("imports.select", "选择");
  const boxVisible = isChecked || hasSelection;
  // 三档互斥，不要写成叠加的 opacity-0 + opacity-100——那样谁生效取决于
  // Tailwind 生成 CSS 的先后顺序，不取决于这里的书写顺序
  const boxTone = isChecked
    ? "border-primary bg-primary opacity-100"
    : hasSelection
      ? "border-border opacity-100"
      : "border-border opacity-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100";

  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={isChecked}
      aria-label={t("imports.select", "选择")}
      title={label}
      className="relative mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center outline-none"
    >
      <span
        aria-hidden="true"
        className={`text-muted-foreground transition-opacity duration-quick ${
          boxVisible ? "opacity-0" : "opacity-100 group-hover/row:opacity-0"
        }`}
      >
        {typeMeta ? typeMeta.icon : sourceKindIcon(task.sourceKind)}
      </span>
      <span
        aria-hidden="true"
        className={`absolute inset-0 flex items-center justify-center rounded border transition-opacity duration-quick ${boxTone}`}
      >
        {isChecked ? (
          <CheckIcon
            className="h-3 w-3 text-primary-foreground"
            aria-hidden="true"
          />
        ) : null}
      </span>
    </button>
  );
}

function StatusBadge({ task }: { task: ImportTask }) {
  const { t } = useTranslation();
  const base =
    "inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] leading-none";

  switch (task.status) {
    case "pending":
      return (
        <span className={`${base} bg-muted text-muted-foreground`}>
          {t("imports.statusPending", "等待中")}
        </span>
      );
    case "processing": {
      const stage = getStageLabel(task.stage);
      return (
        <span className={`${base} bg-primary/10 text-primary`}>
          <Loader2Icon className="h-2.5 w-2.5 animate-spin" aria-hidden="true" />
          {t(stage.key, stage.fallback)}
        </span>
      );
    }
    case "completed":
      // 入库了但缺了文字稿这类东西时，绿色的「已完成」等于骗人——
      // 用户点开条目才发现是个空壳，一批三十条里还挑不出是哪几条
      return task.warning ? (
        <span
          className={`${base} bg-amber-500/10 text-amber-600 dark:text-amber-400`}
        >
          <TriangleAlertIcon className="h-2.5 w-2.5" aria-hidden="true" />
          {t("imports.statusDegraded", "完成（有缺失）")}
        </span>
      ) : (
        <span
          className={`${base} bg-green-500/10 text-green-600 dark:text-green-400`}
        >
          <CheckCircle2Icon className="h-2.5 w-2.5" aria-hidden="true" />
          {t("imports.statusCompleted", "已完成")}
        </span>
      );
    case "duplicate":
      return (
        <span
          className={`${base} bg-amber-500/10 text-amber-600 dark:text-amber-400`}
        >
          <CopyIcon className="h-2.5 w-2.5" aria-hidden="true" />
          {t("imports.statusDuplicate", "重复内容")}
        </span>
      );
    case "canceled":
      return (
        <span className={`${base} bg-muted text-muted-foreground`}>
          <BanIcon className="h-2.5 w-2.5" aria-hidden="true" />
          {t("imports.statusCanceled", "已取消")}
        </span>
      );
    case "failed":
    default:
      return (
        <span className={`${base} bg-destructive/10 text-destructive`}>
          <TriangleAlertIcon className="h-2.5 w-2.5" aria-hidden="true" />
          {t("imports.statusFailed", "失败")}
        </span>
      );
  }
}

function RowAction({
  label,
  onClick,
  destructive,
  children,
}: {
  label: string;
  onClick: () => void;
  destructive?: boolean;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      className={`inline-flex h-6 w-6 items-center justify-center rounded-md transition-colors ${
        destructive
          ? "text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

/**
 * 进度信息。视频链路可跑几十分钟，只有一个转圈无法区分「在推进」和「卡死」，
 * 所以给出任务已用时长；单个阶段超过阈值还没动静时补一条本阶段耗时。
 */
function ProgressHint({ task, now }: { task: ImportTask; now: number }) {
  const { t } = useTranslation();
  if (task.status !== "processing" && task.status !== "pending") {
    return null;
  }
  const elapsed = Math.max(0, now - task.createdAt);
  const inStage = Math.max(0, now - task.updatedAt);
  const stalled = task.status === "processing" && inStage >= STALL_THRESHOLD_MS;

  return (
    <>
      <span>
        {t("imports.elapsed", "已用 {{duration}}", {
          duration: formatDuration(elapsed),
        })}
      </span>
      {stalled ? (
        <span className="text-amber-600 dark:text-amber-400">
          {t("imports.stageElapsed", "本阶段 {{duration}}", {
            duration: formatDuration(inStage),
          })}
        </span>
      ) : null}
    </>
  );
}

/**
 * 导入任务行。终态任务的操作收进悬停浮出的图标条——一屏几十条已完成任务，
 * 每条常驻两个带文字的按钮会把列表撑成两倍高，且全是重复文案。
 */
export function ImportTaskRow({
  task,
  now,
  isChecked,
  hasSelection,
  onToggle,
  onOpenItem,
}: {
  task: ImportTask;
  now: number;
  isChecked: boolean;
  hasSelection: boolean;
  onToggle: (event: React.MouseEvent) => void;
  onOpenItem: (itemId: string) => void;
}) {
  const { t } = useTranslation();
  const cancelTask = useImportStore((state) => state.cancelTask);
  const retryTask = useImportStore((state) => state.retryTask);
  const removeTask = useImportStore((state) => state.removeTask);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );

  const isRunning = task.status === "pending" || task.status === "processing";
  const openableItemId =
    task.status === "completed"
      ? (task.resultItemId ?? null)
      : task.status === "duplicate"
        ? (task.duplicateItemId ?? null)
        : null;
  const host = resolveTaskHost(task);
  const folder = resolveTaskFolder(task);
  const typeMeta = task.itemType ? getItemTypeMeta(task.itemType) : null;
  const timeLabel = formatItemTime(isRunning ? task.createdAt : task.updatedAt);
  const timeTooltip = t("imports.timeTooltip", "入队 {{created}}｜更新 {{updated}}", {
    created: new Date(task.createdAt).toLocaleString(),
    updated: new Date(task.updatedAt).toLocaleString(),
  });
  // 显示名顶掉原始地址时，气泡才拿得出行里没有的东西
  const sourceTooltip = task.displayName ? task.sourceInput : undefined;

  return (
    <div
      className={`group/row relative rounded-xl border px-3 py-2 transition-colors ${
        isChecked
          ? "border-primary/50 bg-primary/10"
          : "border-border/70 bg-background/50 hover:bg-accent/30"
      }`}
    >
      <div className="flex items-start gap-2.5">
        <LeadingSlot
          task={task}
          isChecked={isChecked}
          hasSelection={hasSelection}
          onToggle={onToggle}
        />

        <div className="min-w-0 flex-1">
          {openableItemId ? (
            <button
              type="button"
              onClick={() => onOpenItem(openableItemId)}
              title={sourceTooltip}
              className="block w-full truncate text-left text-sm text-foreground transition-colors hover:text-primary hover:underline"
            >
              {task.displayName || task.sourceInput}
            </button>
          ) : (
            <p className="truncate text-sm text-foreground" title={sourceTooltip}>
              {task.displayName || task.sourceInput}
            </p>
          )}

          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
            <StatusBadge task={task} />
            {host ? <span className="truncate">{host}</span> : null}
            {folder ? (
              <span className="max-w-[18rem] truncate">{folder}</span>
            ) : null}
            {typeMeta ? <span>{t(typeMeta.labelKey, typeMeta.fallback)}</span> : null}
            <ProgressHint task={task} now={now} />
          </div>

          {task.error ? (
            <p className="mt-1 break-words text-xs text-destructive/90">
              {task.error}
            </p>
          ) : null}

          {task.warning ? (
            <p className="mt-1 break-words text-xs text-amber-600 dark:text-amber-400">
              {task.warning}
            </p>
          ) : null}
        </div>

        <span
          title={timeTooltip}
          className="mt-0.5 shrink-0 text-[11px] tabular-nums text-muted-foreground/70"
        >
          {timeLabel}
        </span>
      </div>

      {/* 浮出的动作条盖住时间列；隐藏时必须让出点击，否则整行右侧都被它吃掉 */}
      <div className="pointer-events-none absolute right-2 top-1.5 flex items-center gap-0.5 rounded-lg border border-border bg-popover px-1 py-0.5 opacity-0 shadow-sm transition-opacity duration-quick group-hover/row:pointer-events-auto group-hover/row:opacity-100 group-focus-within/row:pointer-events-auto group-focus-within/row:opacity-100">
        {isRunning ? (
          <RowAction
            label={t("common.cancel", "取消")}
            onClick={() => void cancelTask(task.id)}
          >
            <BanIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </RowAction>
        ) : null}

        {task.status === "failed" || task.status === "canceled" ? (
          <RowAction
            label={t("imports.retry", "重试")}
            onClick={() => void retryTask(task.id)}
          >
            <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </RowAction>
        ) : null}

        {needsCaptureToolSetup(task.error) ? (
          <RowAction
            label={t("imports.goToCaptureSettings", "前往设置安装工具")}
            onClick={() => requestSettingsSection("general")}
          >
            <SettingsIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </RowAction>
        ) : null}

        {openableItemId ? (
          <RowAction
            label={
              task.status === "duplicate"
                ? t("imports.openExisting", "打开已有条目")
                : t("imports.openItem", "打开条目")
            }
            onClick={() => onOpenItem(openableItemId)}
          >
            <ExternalLinkIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </RowAction>
        ) : null}

        {task.status === "duplicate" ? (
          <RowAction
            label={t("imports.createCopy", "仍要创建副本")}
            onClick={() => void retryTask(task.id, true)}
          >
            <CopyPlusIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </RowAction>
        ) : null}

        {!isRunning ? (
          <RowAction
            destructive
            label={t("imports.removeTask", "从列表中删除")}
            onClick={() => void removeTask(task.id)}
          >
            <XIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </RowAction>
        ) : null}
      </div>
    </div>
  );
}
