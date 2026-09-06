/**
 * 导入任务详情弹窗。
 *
 * 终态任务此前完全没有可点开的形态——点标题会跳去知识库，失败的任务连点都点
 * 不了，而它恰恰是最需要看清楚的那种。这里把一条任务的全部可观测信息收在一处：
 * 阶段耗时与 AI 开销、来源、时间、报错与缺失提示，外加一颗把这些一次性带走的
 * 「复制诊断信息」——用户报「这批采集特别慢」时，双方手上得有数。
 */
import { useEffect, useState } from "react";
import { ClipboardCopyIcon, ExternalLinkIcon, LogInIcon, RotateCcwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportTask, KnowledgeItem } from "@guizhi/shared/types";
import { resolveSourcePlatform } from "@guizhi/shared/utils/source-platforms";
import { detectPlatformCapturePlatform } from "@guizhi/shared/utils/platform-capture";
import { Modal } from "../ui/Modal";
import {
  getSourcePlatformMeta,
  PlatformIcon,
} from "../library/platform-meta";
import { useToast } from "../ui/Toast";
import { copyTextToClipboard } from "../../utils/clipboard";
import { useImportStore } from "../../stores/import.store";
import { ImportOriginLabel } from "./ImportOrigin";
import { ImportStageBreakdown } from "./ImportStageBreakdown";
import { ImportCompletionCard } from "./ImportCompletionCard";
import { buildImportTaskReport } from "./import-task-report";
import {
  formatImportTaskError,
  formatImportTaskWarning,
  getAuthenticatedRetryPlatform,
  getStageLabel,
  resolveTaskFolder,
  resolveTaskHost,
  STATUS_LABELS,
} from "./import-task-meta";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 break-words text-xs text-foreground">{children}</div>
    </div>
  );
}

const ACTION_BASE =
  "inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs transition-colors disabled:pointer-events-none disabled:opacity-40";

export function ImportTaskDetailModal({
  task,
  isOpen,
  onClose,
  onOpenItem,
  onAskAboutItem,
}: {
  task: ImportTask;
  isOpen: boolean;
  onClose: () => void;
  onOpenItem: (itemId: string) => void;
  onAskAboutItem: (item: KnowledgeItem) => void;
}) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const retryTask = useImportStore((state) => state.retryTask);
  const [isCopying, setIsCopying] = useState(false);
  const [refreshPair, setRefreshPair] = useState<{
    original: KnowledgeItem;
    refreshed: KnowledgeItem;
  } | null>(null);
  const [completedItem, setCompletedItem] = useState<KnowledgeItem | null>(null);

  const host = resolveTaskHost(task);
  const folder = resolveTaskFolder(task);
  const stats = task.stageStats ?? [];
  // 品牌 logo + 平台名比一行 `v.douyin.com` 认得快，与侧栏「平台」分区、
  // 表格「来源」列共用同一套判定与图标
  const platform = resolveSourcePlatform(task.sourceKind, task.sourceInput);
  const platformMeta = platform ? getSourcePlatformMeta(platform) : null;
  // 通用网页桶例外：「网页」二字什么都没说，主机名才是这里唯一有信息的东西
  const platformLabel = platformMeta
    ? platform === "web" && host
      ? host
      : t(platformMeta.labelKey, platformMeta.fallback)
    : null;
  const openableItemId =
    task.status === "completed"
      ? (task.resultItemId ?? null)
      : task.status === "duplicate"
        ? (task.duplicateItemId ?? null)
        : null;
  const canRetry = task.status === "failed" || task.status === "canceled";
  const authenticatedRetryPlatform = getAuthenticatedRetryPlatform(task);

  const retryAuthenticated = async () => {
    if (!authenticatedRetryPlatform) return;
    try {
      const statuses = await window.api.platformCapture.getStatuses();
      const status = statuses.find((entry) => entry.platform === authenticatedRetryPlatform);
      if (!status?.available) throw new Error("归知内置登录窗口暂不可用");
      if (!status.loggedIn) await window.api.platformCapture.login(authenticatedRetryPlatform);
      await retryTask(task.id, { captureStrategy: "authenticated" });
      onClose();
    } catch (error) {
      showToast(t("imports.authenticatedRetryFailed", "登录态重试未开始"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  useEffect(() => {
    if (!isOpen || !openableItemId) {
      setCompletedItem(null);
      return;
    }
    // preload API 在桌面运行时必定存在，但测试与降级运行时可能只挂了部分桥；
    // 成果卡读不到不该让整张任务详情弹窗崩掉。
    const getItem = window.api?.knowledge?.get;
    if (!getItem) {
      setCompletedItem(null);
      return;
    }
    let cancelled = false;
    getItem(openableItemId)
      .then((item) => {
        if (!cancelled) setCompletedItem(item);
      })
      .catch(() => {
        if (!cancelled) setCompletedItem(null);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, openableItemId]);

  useEffect(() => {
    const originalId = task.refreshOfItemId;
    const refreshedId = task.status === "completed" ? task.resultItemId : null;
    if (!isOpen || !originalId || !refreshedId) {
      setRefreshPair(null);
      return;
    }
    let cancelled = false;
    Promise.all([
      window.api.knowledge.get(originalId),
      window.api.knowledge.get(refreshedId),
    ])
      .then(([original, refreshed]) => {
        if (!cancelled && original && refreshed) setRefreshPair({ original, refreshed });
      })
      .catch(() => {
        if (!cancelled) setRefreshPair(null);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, task.refreshOfItemId, task.resultItemId, task.status]);

  const copyReport = async () => {
    if (isCopying) {
      return;
    }
    setIsCopying(true);
    try {
      // 版本与平台取不到不算失败：诊断文本少一行环境信息仍然有用，
      // 为它把整次复制搞失败是本末倒置
      const [appVersion, platform] = await Promise.all([
        window.electron?.updater?.getVersion?.().catch(() => undefined),
        window.electron?.updater?.getPlatform?.().catch(() => undefined),
      ]);
      const captureStatuses = task.captureStrategy === "authenticated"
        ? await window.api.platformCapture?.getStatuses?.().catch(() => [])
        : [];
      const capturePlatform = detectPlatformCapturePlatform(task.sourceInput);
      const captureStatus = captureStatuses?.find(
        (entry) => entry.platform === capturePlatform,
      );
      const report = buildImportTaskReport(task, {
        translate: (key, fallback, options) =>
          t(key, fallback, options) as unknown as string,
        appVersion,
        platform,
        captureBrowser: captureStatus?.browser
          ? `${captureStatus.browser}${captureStatus.browserVersion ? ` ${captureStatus.browserVersion}` : ""}`
          : undefined,
      });
      await copyTextToClipboard(report);
      showToast(
        t("imports.reportCopied", "已复制诊断信息，可直接粘贴反馈"),
        "success",
      );
    } catch (error) {
      showToast(t("imports.reportCopyFailed", "复制失败"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setIsCopying(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      size="xl"
      title={task.displayName || task.sourceInput}
      subtitle={
        platform ? (
          <span className="inline-flex items-center gap-1.5">
            <PlatformIcon platform={platform} className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{platformLabel}</span>
          </span>
        ) : (
          // 手工粘贴的文本没有来源可言
          (folder ?? undefined)
        )
      }
      headerActions={
        <button
          type="button"
          disabled={isCopying}
          onClick={() => void copyReport()}
          className={`${ACTION_BASE} border border-border text-muted-foreground hover:bg-accent hover:text-foreground`}
        >
          <ClipboardCopyIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("imports.copyReport", "复制诊断信息")}
        </button>
      }
    >
      <div className="space-y-5">
        <div className="grid grid-cols-2 gap-4">
          <div className="col-span-2 grid grid-cols-2 gap-4">
            <Field label={t("imports.originFilter", "提交来源")}>
              <ImportOriginLabel origin={task.origin} />
            </Field>
            {task.origin === "mobile" && task.receivedAt != null ? (
              <Field label={t("imports.receivedAt", "桌面接收时间")}>
                {new Date(task.receivedAt).toLocaleString()}
              </Field>
            ) : null}
          </div>
          <Field label={t("imports.reportStatus", "状态")}>
            {t(STATUS_LABELS[task.status].key, STATUS_LABELS[task.status].fallback)}
            {task.status === "processing" && task.stage
              ? ` · ${t(getStageLabel(task.stage).key, getStageLabel(task.stage).fallback)}`
              : ""}
          </Field>
          <Field label={t("imports.reportQueuedAt", "入队")}>
            {new Date(task.createdAt).toLocaleString()}
          </Field>
          <Field label={t("imports.reportSource", "来源")}>
            <span className="break-all">{task.sourceInput}</span>
          </Field>
          <Field label={t("imports.reportUpdatedAt", "更新")}>
            {new Date(task.updatedAt).toLocaleString()}
          </Field>
        </div>

        {completedItem ? (
          <ImportCompletionCard
            item={completedItem}
            onOpen={() => {
              onOpenItem(completedItem.id);
              onClose();
            }}
            onAsk={() => {
              onAskAboutItem(completedItem);
              onClose();
            }}
          />
        ) : null}

        <div>
          <div className="mb-2 text-xs font-medium text-foreground">
            {t("imports.reportStages", "阶段耗时")}
          </div>
          {stats.length > 0 ? (
            <ImportStageBreakdown stats={stats} />
          ) : (
            // 加统计之前入库的老任务，以及排队时就被取消的任务
            <p className="text-xs text-muted-foreground">
              {t("imports.noStageStats", "本次没有记录阶段耗时")}
            </p>
          )}
        </div>

        {task.error ? (
          <div className="rounded-lg bg-destructive/10 px-3 py-2">
            <div className="text-[11px] text-destructive/80">
              {t("imports.reportError", "报错")}
            </div>
            <p className="mt-0.5 break-words text-xs text-destructive">
              {formatImportTaskError(task.error, t)}
            </p>
          </div>
        ) : null}

        {task.warning ? (
          <div className="rounded-lg bg-amber-500/10 px-3 py-2">
            <div className="text-[11px] text-amber-600/80 dark:text-amber-400/80">
              {t("imports.reportWarning", "缺失提示")}
            </div>
            <p className="mt-0.5 break-words text-xs text-amber-600 dark:text-amber-400">
              {formatImportTaskWarning(task.warning, t)}
            </p>
          </div>
        ) : null}

        {refreshPair ? (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-3">
            <div className="text-xs font-medium text-foreground">
              {t("imports.refreshComparison", "来源刷新待确认")}
            </div>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {t("imports.refreshComparisonHint", "刷新结果保留为未分类副本，原条目没有被覆盖。确认内容后再移动或删除副本。")}
            </p>
            <div className="mt-2 grid grid-cols-2 gap-2 text-xs">
              <div className="rounded-md bg-background/60 p-2">
                <div className="text-muted-foreground">{t("imports.refreshOriginal", "原条目")}</div>
                <div className="mt-0.5 truncate text-foreground">{refreshPair.original.title || t("library.untitled", "无标题")}</div>
                <div className="mt-0.5 text-muted-foreground">{refreshPair.original.content.trim().length} {t("library.wordUnit", "字")}</div>
              </div>
              <div className="rounded-md bg-background/60 p-2">
                <div className="text-muted-foreground">{t("imports.refreshNew", "刷新副本")}</div>
                <div className="mt-0.5 truncate text-foreground">{refreshPair.refreshed.title || t("library.untitled", "无标题")}</div>
                <div className="mt-0.5 text-muted-foreground">{refreshPair.refreshed.content.trim().length} {t("library.wordUnit", "字")}</div>
              </div>
            </div>
          </div>
        ) : null}

        {openableItemId || canRetry ? (
          <div className="flex items-center gap-2 border-t border-border pt-4">
            {openableItemId ? (
              <button
                type="button"
                onClick={() => {
                  onOpenItem(openableItemId);
                  onClose();
                }}
                className={`${ACTION_BASE} bg-primary text-primary-foreground hover:bg-primary/90`}
              >
                <ExternalLinkIcon className="h-3.5 w-3.5" aria-hidden="true" />
                {task.status === "duplicate"
                  ? t("imports.openExisting", "打开已有条目")
                  : t("imports.openItem", "打开条目")}
              </button>
            ) : null}
            {refreshPair ? (
              <button
                type="button"
                onClick={() => {
                  onOpenItem(refreshPair.original.id);
                  onClose();
                }}
                className={`${ACTION_BASE} border border-border text-muted-foreground hover:bg-accent hover:text-foreground`}
              >
                {t("imports.openOriginal", "打开原条目")}
              </button>
            ) : null}
            {canRetry ? (
              <button
                type="button"
                onClick={() => {
                  void retryTask(task.id);
                  onClose();
                }}
                className={`${ACTION_BASE} border border-border text-muted-foreground hover:bg-accent hover:text-foreground`}
              >
                <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
                {t("imports.retry", "重试")}
              </button>
            ) : null}
            {authenticatedRetryPlatform ? (
              <button
                type="button"
                onClick={() => void retryAuthenticated()}
                className={`${ACTION_BASE} bg-primary text-primary-foreground hover:bg-primary/90`}
              >
                <LogInIcon className="h-3.5 w-3.5" aria-hidden="true" />
                {t("imports.authenticatedRetry", "使用登录态重试")}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
