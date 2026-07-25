import { useEffect } from "react";
import {
  BanIcon,
  CheckCircle2Icon,
  CopyPlusIcon,
  ExternalLinkIcon,
  FileTextIcon,
  GlobeIcon,
  Loader2Icon,
  PlusIcon,
  RotateCcwIcon,
  Trash2Icon,
  TriangleAlertIcon,
  CopyIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportStage, ImportTask } from "@guizhi/shared/types";
import { useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useShortcutLabel } from "../../hooks/useShortcutLabel";
import { Spinner } from "../ui/Spinner";
import { formatItemTime } from "../library/type-meta";

function SourceIcon({ task }: { task: ImportTask }) {
  if (task.sourceKind === "url") {
    return (
      <GlobeIcon
        className="h-4 w-4 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
    );
  }
  return (
    <FileTextIcon
      className="h-4 w-4 shrink-0 text-muted-foreground"
      aria-hidden="true"
    />
  );
}

/**
 * 子阶段文案。视频链路会跑元数据 → 下载 → 转码 → 转写 → 排版 → 总结六步，
 * 全程可达几十分钟，只显示「抓取中」用户无法判断是在推进还是卡死了。
 */
const STAGE_LABELS: Record<ImportStage, { key: string; fallback: string }> = {
  fetching: { key: "imports.stageFetching", fallback: "抓取中" },
  extracting: { key: "imports.stageExtracting", fallback: "解析中" },
  saving: { key: "imports.stageSaving", fallback: "入库中" },
  "video-metadata": {
    key: "imports.stageVideoMetadata",
    fallback: "解析视频信息",
  },
  "video-audio": { key: "imports.stageVideoAudio", fallback: "下载音轨" },
  transcoding: { key: "imports.stageTranscoding", fallback: "音频转码" },
  transcribing: { key: "imports.stageTranscribing", fallback: "语音转写" },
  formatting: { key: "imports.stageFormatting", fallback: "文字稿排版" },
  summarizing: { key: "imports.stageSummarizing", fallback: "生成总结" },
  "image-download": {
    key: "imports.stageImageDownload",
    fallback: "下载配图",
  },
  "image-ocr": { key: "imports.stageImageOcr", fallback: "识别图中文字" },
  "forum-replies": { key: "imports.stageForumReplies", fallback: "整理讨论区" },
};

function StatusBadge({ task }: { task: ImportTask }) {
  const { t } = useTranslation();

  switch (task.status) {
    case "pending":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
          {t("imports.statusPending", "等待中")}
        </span>
      );
    case "processing": {
      const stage = STAGE_LABELS[task.stage ?? "fetching"] ?? STAGE_LABELS.saving;
      const stageLabel = t(stage.key, stage.fallback);
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-primary/10 px-2 py-0.5 text-[11px] text-primary">
          <Loader2Icon className="h-3 w-3 animate-spin" aria-hidden="true" />
          {stageLabel}
        </span>
      );
    }
    case "completed":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-green-500/10 px-2 py-0.5 text-[11px] text-green-600 dark:text-green-400">
          <CheckCircle2Icon className="h-3 w-3" aria-hidden="true" />
          {t("imports.statusCompleted", "已完成")}
        </span>
      );
    case "duplicate":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-[11px] text-amber-600 dark:text-amber-400">
          <CopyIcon className="h-3 w-3" aria-hidden="true" />
          {t("imports.statusDuplicate", "重复内容")}
        </span>
      );
    case "canceled":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
          <BanIcon className="h-3 w-3" aria-hidden="true" />
          {t("imports.statusCanceled", "已取消")}
        </span>
      );
    case "failed":
    default:
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-destructive/10 px-2 py-0.5 text-[11px] text-destructive">
          <TriangleAlertIcon className="h-3 w-3" aria-hidden="true" />
          {t("imports.statusFailed", "失败")}
        </span>
      );
  }
}

/**
 * 导入任务页：队列状态、取消/重试、打开结果条目、
 * 重复任务支持「打开已有条目」与「仍要创建副本」。
 */
export function ImportsWorkspace() {
  const { t } = useTranslation();
  const tasks = useImportStore((state) => state.tasks);
  const isLoading = useImportStore((state) => state.isLoading);
  const fetchTasks = useImportStore((state) => state.fetchTasks);
  const cancelTask = useImportStore((state) => state.cancelTask);
  const retryTask = useImportStore((state) => state.retryTask);
  const clearFinished = useImportStore((state) => state.clearFinished);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const captureShortcut = useShortcutLabel("newItem");

  useEffect(() => {
    void fetchTasks();
  }, [fetchTasks]);

  const openItem = async (itemId: string) => {
    setAppModule("library");
    setScope("all");
    await selectItem(itemId);
  };

  const actionButtonClass =
    "inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground";

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground">
          {t("imports.title", "导入任务")}
        </h2>
        <button
          type="button"
          onClick={() =>
            window.dispatchEvent(new CustomEvent("shortcut:newItem"))
          }
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <PlusIcon className="h-4 w-4" aria-hidden="true" />
          {t("capture.title", "快速采集")}
        </button>
        <button
          type="button"
          onClick={() => void clearFinished()}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
        >
          <Trash2Icon className="h-4 w-4" aria-hidden="true" />
          {t("imports.clearFinished", "清理已完成")}
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-3">
        {isLoading && tasks.length === 0 ? (
          <div className="flex h-32 items-center justify-center">
            <Spinner size="sm" tone="muted" />
          </div>
        ) : tasks.length === 0 ? (
          <div className="flex h-40 flex-col items-center justify-center gap-2 text-center">
            <p className="text-sm text-muted-foreground">
              {t("imports.empty", "还没有导入任务")}
            </p>
            <p className="text-xs text-muted-foreground/70">
              {t("imports.emptyHint", "按 {{shortcut}} 打开快速采集", {
                shortcut: captureShortcut,
              })}
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {tasks.map((task) => (
              <div
                key={task.id}
                className="rounded-xl border border-border/70 bg-background/50 px-4 py-3"
              >
                <div className="flex items-center gap-2">
                  <SourceIcon task={task} />
                  <span
                    className="min-w-0 flex-1 truncate text-sm text-foreground"
                    title={task.sourceInput}
                  >
                    {task.displayName || task.sourceInput}
                  </span>
                  <StatusBadge task={task} />
                  <span className="text-[11px] text-muted-foreground/70">
                    {formatItemTime(task.createdAt)}
                  </span>
                </div>

                {task.error ? (
                  <p className="mt-1.5 break-all pl-6 text-xs text-destructive/90">
                    {task.error}
                  </p>
                ) : null}

                <div className="mt-1.5 flex items-center gap-1 pl-6">
                  {task.status === "pending" || task.status === "processing" ? (
                    <button
                      type="button"
                      onClick={() => void cancelTask(task.id)}
                      className={actionButtonClass}
                    >
                      <BanIcon className="h-3.5 w-3.5" aria-hidden="true" />
                      {t("common.cancel", "取消")}
                    </button>
                  ) : null}
                  {task.status === "failed" || task.status === "canceled" ? (
                    <button
                      type="button"
                      onClick={() => void retryTask(task.id)}
                      className={actionButtonClass}
                    >
                      <RotateCcwIcon
                        className="h-3.5 w-3.5"
                        aria-hidden="true"
                      />
                      {t("imports.retry", "重试")}
                    </button>
                  ) : null}
                  {task.status === "completed" && task.resultItemId ? (
                    <button
                      type="button"
                      onClick={() => void openItem(task.resultItemId!)}
                      className={actionButtonClass}
                    >
                      <ExternalLinkIcon
                        className="h-3.5 w-3.5"
                        aria-hidden="true"
                      />
                      {t("imports.openItem", "打开条目")}
                    </button>
                  ) : null}
                  {task.status === "duplicate" ? (
                    <>
                      {task.duplicateItemId ? (
                        <button
                          type="button"
                          onClick={() => void openItem(task.duplicateItemId!)}
                          className={actionButtonClass}
                        >
                          <ExternalLinkIcon
                            className="h-3.5 w-3.5"
                            aria-hidden="true"
                          />
                          {t("imports.openExisting", "打开已有条目")}
                        </button>
                      ) : null}
                      <button
                        type="button"
                        onClick={() => void retryTask(task.id, true)}
                        className={actionButtonClass}
                      >
                        <CopyPlusIcon
                          className="h-3.5 w-3.5"
                          aria-hidden="true"
                        />
                        {t("imports.createCopy", "仍要创建副本")}
                      </button>
                    </>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
