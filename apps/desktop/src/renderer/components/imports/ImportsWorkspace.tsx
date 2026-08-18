import { useEffect, useMemo, useState } from "react";
import {
  PauseIcon,
  PlayIcon,
  CompassIcon,
  RotateCcwIcon,
  SearchIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportTask, KnowledgeItem } from "@guizhi/shared/types";
import { filterTasks, useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useAskStore } from "../../stores/ask.store";
import { useUIStore } from "../../stores/ui.store";
import { LoadErrorState } from "../ui/LoadErrorState";
import { Spinner } from "../ui/Spinner";
import { ImportsBulkBar } from "./ImportsBulkBar";
import { ImportsEmptyState, ImportsFilteredEmpty } from "./ImportsEmptyState";
import { ImportTaskDetailModal } from "./ImportTaskDetailModal";
import { ImportTaskRow } from "./ImportTaskRow";
import {
  PlatformDiscoveryPanel,
} from "./PlatformDiscoveryPanel";
import { DISCOVERY_DRAFT_KEY } from "./platform-discovery-draft";

function isRunning(task: ImportTask): boolean {
  return task.status === "pending" || task.status === "processing";
}

/**
 * 导入任务页：队列状态、筛选与搜索、多选批量处理、
 * 打开结果条目，重复任务支持「打开已有条目」与「仍要创建副本」。
 */
export function ImportsWorkspace() {
  const [view, setView] = useState<"tasks" | "discovery">(() =>
    sessionStorage.getItem(DISCOVERY_DRAFT_KEY) ? "discovery" : "tasks",
  );
  return view === "discovery"
    ? <PlatformDiscoveryPanel onBack={() => setView("tasks")} />
    : <ImportTasksWorkspace onOpenDiscovery={() => setView("discovery")} />;
}

function ImportTasksWorkspace({ onOpenDiscovery }: { onOpenDiscovery: () => void }) {
  const { t } = useTranslation();
  const tasks = useImportStore((state) => state.tasks);
  const hasLoaded = useImportStore((state) => state.hasLoaded);
  const loadError = useImportStore((state) => state.loadError);
  const filter = useImportStore((state) => state.filter);
  const setFilter = useImportStore((state) => state.setFilter);
  const query = useImportStore((state) => state.query);
  const setQuery = useImportStore((state) => state.setQuery);
  const selectionIds = useImportStore((state) => state.selectionIds);
  const toggleSelection = useImportStore((state) => state.toggleSelection);
  const rangeSelectTo = useImportStore((state) => state.rangeSelectTo);
  const clearSelection = useImportStore((state) => state.clearSelection);
  const fetchTasks = useImportStore((state) => state.fetchTasks);
  const retryTasks = useImportStore((state) => state.retryTasks);
  const clearFinished = useImportStore((state) => state.clearFinished);
  const queueState = useImportStore((state) => state.queueState);
  const toggleQueuePaused = useImportStore((state) => state.toggleQueuePaused);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const requestAskDraft = useUIStore((state) => state.requestAskDraft);
  const newAskSession = useAskStore((state) => state.newSession);

  const [now, setNow] = useState(() => Date.now());
  // 详情弹窗按 id 记而不是把整条任务存下来：任务还在跑时会不断有新状态推过来，
  // 存快照的话弹窗会停在打开那一刻，越看越不对
  const [detailTaskId, setDetailTaskId] = useState<string | null>(null);

  useEffect(() => {
    void fetchTasks();
  }, [fetchTasks]);

  const visible = useMemo(
    () => filterTasks(tasks, filter, query),
    [tasks, filter, query],
  );
  const selected = useMemo(
    () => tasks.filter((task) => selectionIds.includes(task.id)),
    [tasks, selectionIds],
  );
  const detailTask = detailTaskId
    ? (tasks.find((task) => task.id === detailTaskId) ?? null)
    : null;
  const activeCount = tasks.filter(isRunning).length;
  const failedIds = tasks
    .filter((task) => task.status === "failed")
    .map((task) => task.id);

  // 有任务在跑时才起秒级 tick：行内的「已用时长」全靠它推进
  useEffect(() => {
    if (activeCount === 0) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activeCount]);

  useEffect(() => {
    if (selectionIds.length === 0) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        clearSelection();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [selectionIds.length, clearSelection]);

  const openItem = async (itemId: string) => {
    setAppModule("library");
    setScope("all");
    await selectItem(itemId);
  };

  const askAboutItem = (item: KnowledgeItem) => {
    // 这是围绕一条新导入内容开始的讨论，不该污染用户刚刚浏览的历史会话。
    // 仅当当前会话已有消息时 newSession 才会实际换 ID；空白会话直接复用。
    newAskSession();
    // 当前问答引擎会从全库检索，标题是把召回收窄到这条导入结果的稳定锚点；
    // 只预填不发送，用户能先改成自己的问题，也不会因误点消耗模型调用。
    requestAskDraft(
      t("imports.askAboutItemDraft", "请优先围绕「{{title}}」这条导入内容，概括核心观点与可复用结论。", {
        title: item.title || t("library.untitled", "无标题"),
      }),
    );
    setAppModule("ask");
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="shrink-0 text-sm font-semibold text-foreground">
          {t("imports.title", "导入任务")}
        </h2>
        {/* 队列读出来之前不报数：否则会先写「共 0 条」再跳成真实条数 */}
        {hasLoaded ? (
          <span className="truncate text-xs text-muted-foreground/70">
            {queueState.paused
              ? t("imports.summaryPaused", "队列已暂停 · 等待 {{pending}}", {
                  pending: queueState.pendingCount,
                })
              : activeCount > 0
              ? t("imports.summaryActive", "共 {{total}} 条 · 进行中 {{active}}", {
                  total: tasks.length,
                  active: activeCount,
                })
              : t("imports.summaryTotal", "共 {{total}} 条", {
                  total: tasks.length,
                })}
          </span>
        ) : null}

        <div className="relative ml-auto w-56 min-w-[7rem] max-w-[14rem] flex-1">
          <SearchIcon
            className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground/60"
            aria-hidden="true"
          />
          <input
            type="text"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("imports.searchPlaceholder", "搜索标题或链接")}
            className="h-8 w-full rounded-lg border border-border bg-background/60 pl-8 pr-7 text-xs text-foreground outline-none transition-colors placeholder:text-muted-foreground/60 focus:border-primary/50"
          />
          {query ? (
            <button
              type="button"
              onClick={() => setQuery("")}
              title={t("header.clearSearch", "清除搜索")}
              aria-label={t("header.clearSearch", "清除搜索")}
              className="absolute right-1.5 top-1/2 flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
            >
              <XIcon className="h-3 w-3" aria-hidden="true" />
            </button>
          ) : null}
        </div>

        {failedIds.length > 0 ? (
          <button
            type="button"
            onClick={() => void retryTasks(failedIds)}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
          >
            <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />
            {t("imports.retryAllFailed", "重试失败 {{count}}", {
              count: failedIds.length,
            })}
          </button>
        ) : null}
        <button
          type="button"
          onClick={onOpenDiscovery}
          className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
        >
          <CompassIcon className="h-4 w-4" aria-hidden="true" />
          {t("imports.platformDiscovery", "平台发现")}
        </button>
        {queueState.pendingCount > 0 || queueState.paused ? (
          <button
            type="button"
            onClick={() => void toggleQueuePaused()}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
            aria-pressed={queueState.paused}
          >
            {queueState.paused ? (
              <PlayIcon className="h-4 w-4" aria-hidden="true" />
            ) : (
              <PauseIcon className="h-4 w-4" aria-hidden="true" />
            )}
            {queueState.paused
              ? t("imports.resumeQueue", "继续队列")
              : t("imports.pauseQueue", "暂停队列")}
          </button>
        ) : null}
        <button
          type="button"
          onClick={() => void clearFinished()}
          className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
        >
          <Trash2Icon className="h-4 w-4" aria-hidden="true" />
          {t("imports.clearFinished", "清理已完成")}
        </button>
      </div>

      {selected.length > 0 ? <ImportsBulkBar selected={selected} /> : null}

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-3">
        {!hasLoaded ? (
          <div className="delayed-fade-in flex h-32 items-center justify-center">
            <Spinner size="sm" tone="muted" />
          </div>
        ) : loadError && tasks.length === 0 ? (
          <LoadErrorState message={loadError} onRetry={() => void fetchTasks()} />
        ) : tasks.length === 0 ? (
          <ImportsEmptyState />
        ) : visible.length === 0 ? (
          <ImportsFilteredEmpty
            onReset={() => {
              setFilter("all");
              setQuery("");
            }}
          />
        ) : (
          <div className="space-y-1.5">
            {visible.map((task) => (
              <ImportTaskRow
                key={task.id}
                task={task}
                now={now}
                isChecked={selectionIds.includes(task.id)}
                hasSelection={selectionIds.length > 0}
                onToggle={(event) => {
                  if (event.shiftKey) {
                    rangeSelectTo(task.id);
                    return;
                  }
                  toggleSelection(task.id);
                }}
                onOpenItem={(itemId) => void openItem(itemId)}
                onOpenDetail={() => setDetailTaskId(task.id)}
              />
            ))}
          </div>
        )}
      </div>

      {/* 任务被「清理已完成」删掉时 detailTask 会变成 null，弹窗跟着消失 */}
      {detailTask ? (
        <ImportTaskDetailModal
          task={detailTask}
          isOpen={detailTaskId !== null}
          onClose={() => setDetailTaskId(null)}
          onOpenItem={(itemId) => void openItem(itemId)}
          onAskAboutItem={askAboutItem}
        />
      ) : null}
    </div>
  );
}
