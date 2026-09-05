import { useEffect, useState, type ReactNode } from "react";
import {
  CheckCircle2Icon,
  ChevronDownIcon,
  CircleDashedIcon,
  FolderOpenIcon,
  Loader2Icon,
  RefreshCwIcon,
  Trash2Icon,
  TriangleAlertIcon,
  XIcon,
  type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Input } from "../../ui/Input";
import { ConfirmDialog } from "../../ui/ConfirmDialog";

/** 引擎在 UI 上的归一化状态；三个引擎的原始状态各不相同，由各自的行组件映射到这里 */
export type EngineState = "probing" | "ready" | "missing" | "invalid" | "error";

/**
 * 主操作位。没有可做的动作时（如已确认是最新版）渲染成静态状态而不是按钮——
 * 与「关于应用」里应用自身的更新入口保持同一套语义：有更新才给更新按钮。
 */
export type CaptureEnginePrimaryAction =
  | {
      kind: "button";
      label: string;
      icon: LucideIcon;
      /** 未安装 / 有更新时用实心主按钮，其余降级为描边按钮 */
      emphasized: boolean;
      busy: boolean;
      onClick: () => void;
    }
  | { kind: "status"; label: string };

export interface CaptureEngineCustomPath {
  value: string;
  placeholder: string;
  hint: string;
  onPick: () => void;
  onClear: () => void;
}

export interface CaptureEngineRemoveAction {
  label: string;
  confirmTitle: string;
  confirmMessage: string;
  busy: boolean;
  onConfirm: () => void;
}

/** 高级面板里的维护动作（如重新安装）——耗时且不常用，不该占据主操作位 */
export interface CaptureEngineMaintenanceAction {
  label: string;
  busy: boolean;
  onClick: () => void;
}

export interface CaptureEngineRowProps {
  engineId: string;
  name: string;
  /** 一句话说明这个引擎干什么用 */
  purpose: string;
  state: EngineState;
  /** 维护时覆盖就绪徽章，避免同时声称引擎可用。 */
  activityLabel?: string;
  /** 位于主行下、始终可见的任务进度。 */
  activityContent?: ReactNode;
  /** 禁用维护动作，与动作自身是否正在执行分开。 */
  actionsDisabled?: boolean;
  /** 可选引擎未安装时不报警，只作中性提示 */
  optional?: boolean;
  /** 状态补充信息（版本 · 来源），拼在 purpose 之后 */
  detail?: string;
  /** 安装 / 卸载进行中的文案，存在时整行替换 purpose + detail */
  busyText?: string;
  /** 安装进度百分比；null 表示无法计算总量 */
  progressPercent?: number | null;
  /** 省略表示当前没有可做的主操作（如已装好且上游没有「更新」概念） */
  primary?: CaptureEnginePrimaryAction;
  isRefreshing: boolean;
  onRefresh: () => void;
  /** 当前生效的可执行文件路径，仅在高级面板展示 */
  activePath?: string;
  customPath?: CaptureEngineCustomPath;
  reinstall?: CaptureEngineMaintenanceAction;
  remove?: CaptureEngineRemoveAction;
}

const STATE_ICON: Record<EngineState, LucideIcon> = {
  probing: Loader2Icon,
  ready: CheckCircle2Icon,
  missing: TriangleAlertIcon,
  invalid: TriangleAlertIcon,
  error: TriangleAlertIcon,
};

function StatusBadge({
  state,
  optional,
}: {
  state: EngineState;
  optional: boolean;
}) {
  const { t } = useTranslation();
  const Icon = state === "missing" && optional ? CircleDashedIcon : STATE_ICON[state];

  const label =
    state === "probing"
      ? t("settings.captureStateProbing", "检测中")
      : state === "ready"
        ? t("settings.captureStateReady", "已就绪")
        : state === "invalid"
          ? t("settings.captureStateInvalid", "路径无效")
          : state === "error"
            ? t("settings.captureStateError", "检测失败")
            : optional
              ? t("settings.captureStateOptional", "未安装（可选）")
              : t("settings.captureStateMissing", "未安装");

  const tone =
    state === "ready"
      ? "border-emerald-600/40 bg-emerald-500/10 text-emerald-800 dark:text-emerald-200"
      : state === "invalid" || state === "error"
        ? "border-destructive/30 bg-destructive/10 text-destructive"
        : state === "missing" && !optional
          ? "border-amber-600/40 bg-amber-500/10 text-amber-800 dark:text-amber-200"
          : "border-border bg-muted/60 text-muted-foreground";

  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium ${tone}`}
    >
      <Icon
        className={`h-3 w-3 ${state === "probing" ? "animate-spin" : ""}`}
        aria-hidden="true"
      />
      {label}
    </span>
  );
}

/**
 * 采集引擎的统一行：主行只展示「名称 + 状态徽章 + 用途 + 主操作」，
 * 生效路径、自定义路径、重新检测、移除 / 卸载都收进折叠的高级面板——
 * 三个引擎共用同一套布局与同一处的销毁入口，避免各写各的样式。
 */
export function CaptureEngineRow({
  engineId,
  name,
  purpose,
  state,
  activityLabel,
  activityContent,
  actionsDisabled = false,
  optional = false,
  detail,
  busyText,
  progressPercent,
  primary,
  isRefreshing,
  onRefresh,
  activePath,
  customPath,
  reinstall,
  remove,
}: CaptureEngineRowProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(state === "invalid");
  const [confirmingRemove, setConfirmingRemove] = useState(false);

  // 自定义路径跑不起来时摊开面板，否则用户看不到出问题的那个输入框。
  // 只在状态切换到 invalid 的那一刻展开，之后用户手动收起不会被再次弹开。
  useEffect(() => {
    if (state === "invalid") {
      setExpanded(true);
    }
  }, [state]);

  const description =
    busyText ?? [purpose, detail].filter(Boolean).join(" · ");

  return (
    <div
      data-testid={`capture-engine-${engineId}`}
      className="border-b border-border/70 transition-colors last:border-0 hover:bg-muted/20"
    >
      <div className="flex items-center justify-between gap-4 px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{name}</span>
            {activityLabel ? (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-full border border-primary/25 bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                <CircleDashedIcon className="h-3 w-3" aria-hidden="true" />
                {activityLabel}
              </span>
            ) : <StatusBadge state={state} optional={optional} />}
          </div>
          <div className="mt-0.5 text-xs text-muted-foreground">
            {description}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {!primary ? null : primary.kind === "status" ? (
            <span className="inline-flex h-9 items-center gap-1.5 px-1 text-sm text-emerald-600 dark:text-emerald-400">
              <CheckCircle2Icon className="h-4 w-4" aria-hidden="true" />
              {primary.label}
            </span>
          ) : (
            <button
              type="button"
              onClick={primary.onClick}
              disabled={primary.busy}
              className={`inline-flex h-9 items-center gap-1.5 rounded-lg px-3 text-sm font-medium transition-colors disabled:opacity-60 ${
                primary.emphasized
                  ? "bg-primary text-primary-foreground hover:bg-primary/90"
                  : "border border-border text-foreground hover:bg-muted/60"
              }`}
            >
              {primary.busy ? (
                <Loader2Icon
                  className="h-4 w-4 animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <primary.icon className="h-4 w-4" aria-hidden="true" />
              )}
              {primary.label}
            </button>
          )}
          <button
            type="button"
            onClick={() => setExpanded((value) => !value)}
            aria-expanded={expanded}
            title={t("settings.captureAdvanced", "高级选项")}
            aria-label={t("settings.captureAdvanced", "高级选项")}
            className="inline-flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
          >
            <ChevronDownIcon
              className={`h-4 w-4 transition-transform duration-base ${expanded ? "rotate-180" : ""}`}
              aria-hidden="true"
            />
          </button>
        </div>
      </div>

      {activityContent}

      {progressPercent != null ? (
        <div className="px-4 pb-3">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-primary transition-all duration-200"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
        </div>
      ) : null}

      {expanded ? (
        <div className="space-y-3 border-t border-border/70 bg-muted/20 px-4 py-3">
          {activePath ? (
            <div className="flex items-center gap-3">
              <span className="w-20 shrink-0 text-xs text-muted-foreground">
                {t("settings.captureActivePath", "生效路径")}
              </span>
              <code className="min-w-0 flex-1 truncate rounded-lg bg-muted/60 px-3 py-2 text-xs">
                {activePath}
              </code>
            </div>
          ) : null}

          {customPath ? (
            <div className="flex items-start gap-3">
              <span className="w-20 shrink-0 pt-3 text-xs text-muted-foreground">
                {t("settings.captureCustomPath", "自定义路径")}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  {/* 只读：该路径会被主进程 spawn，只接受文件选择器返回的值 */}
                  <div className="min-w-0 flex-1">
                    <Input
                      value={customPath.value}
                      readOnly
                      placeholder={customPath.placeholder}
                      aria-label={t("settings.captureCustomPath", "自定义路径")}
                    />
                  </div>
                  <button
                    type="button"
                    onClick={customPath.onPick}
                    title={t("settings.captureBrowse", "选择文件")}
                    aria-label={t("settings.captureBrowse", "选择文件")}
                    className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
                  >
                    <FolderOpenIcon className="h-4 w-4" aria-hidden="true" />
                  </button>
                  {customPath.value ? (
                    <button
                      type="button"
                      onClick={customPath.onClear}
                      title={t("settings.captureClearPath", "清除自定义路径")}
                      aria-label={t(
                        "settings.captureClearPath",
                        "清除自定义路径",
                      )}
                      className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
                    >
                      <XIcon className="h-4 w-4" aria-hidden="true" />
                    </button>
                  ) : null}
                </div>
                <p className="mt-1.5 text-xs text-muted-foreground">
                  {customPath.hint}
                </p>
              </div>
            </div>
          ) : null}

          <div className="flex items-center justify-between gap-3 pt-0.5">
            <button
              type="button"
              onClick={onRefresh}
              disabled={isRefreshing || actionsDisabled}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground disabled:opacity-60"
            >
              <RefreshCwIcon
                className={`h-3.5 w-3.5 ${isRefreshing ? "animate-spin" : ""}`}
                aria-hidden="true"
              />
              {t("settings.captureRecheck", "重新检测")}
            </button>
            <div className="flex items-center gap-2">
              {reinstall ? (
                <button
                  type="button"
                  onClick={reinstall.onClick}
                  disabled={reinstall.busy || actionsDisabled}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground disabled:opacity-60"
                >
                  {reinstall.busy ? (
                    <Loader2Icon
                      className="h-3.5 w-3.5 animate-spin"
                      aria-hidden="true"
                    />
                  ) : (
                    <RefreshCwIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  )}
                  {reinstall.label}
                </button>
              ) : null}
              {remove ? (
                <button
                  type="button"
                  onClick={() => setConfirmingRemove(true)}
                  disabled={remove.busy || actionsDisabled}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive disabled:opacity-60"
                >
                  {remove.busy ? (
                    <Loader2Icon
                      className="h-3.5 w-3.5 animate-spin"
                      aria-hidden="true"
                    />
                  ) : (
                    <Trash2Icon className="h-3.5 w-3.5" aria-hidden="true" />
                  )}
                  {remove.label}
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

      {remove ? (
        <ConfirmDialog
          isOpen={confirmingRemove}
          onClose={() => setConfirmingRemove(false)}
          onConfirm={() => {
            setConfirmingRemove(false);
            remove.onConfirm();
          }}
          title={remove.confirmTitle}
          message={remove.confirmMessage}
          confirmText={remove.label}
          cancelText={t("common.cancel", "取消")}
          variant="destructive"
        />
      ) : null}
    </div>
  );
}
