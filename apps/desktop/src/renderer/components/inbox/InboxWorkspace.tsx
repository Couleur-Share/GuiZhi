import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertTriangleIcon,
  CheckIcon,
  CompassIcon,
  FolderInputIcon,
  Loader2Icon,
  NetworkIcon,
  RefreshCwIcon,
  ScanSearchIcon,
  SparklesIcon,
} from "lucide-react";
import type { InboxItem } from "@guizhi/shared/types";
import { useInboxStore } from "../../stores/inbox.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { Select } from "../ui/Select";
import { Spinner } from "../ui/Spinner";
import { LoadErrorState } from "../ui/LoadErrorState";
import { useToast } from "../ui/Toast";
import { Checkbox } from "../ui/Checkbox";
import { ThemedReadingTasks } from "../themed-reading/ThemedReadingTasks";

function knowledgeItemId(item: InboxItem): string | null {
  return "itemId" in item ? item.itemId : null;
}

export function InboxWorkspace() {
  const { t } = useTranslation();
  const [showThemeTasks, setShowThemeTasks] = useState(false);
  const { showToast } = useToast();
  const items = useInboxStore((state) => state.items);
  const filter = useInboxStore((state) => state.filter);
  const total = useInboxStore((state) => state.total);
  const counts = useInboxStore((state) => state.counts);
  const selectionIds = useInboxStore((state) => state.selectionIds);
  const toggleSelection = useInboxStore((state) => state.toggleSelection);
  const setSelection = useInboxStore((state) => state.setSelection);
  const clearSelection = useInboxStore((state) => state.clearSelection);
  const refresh = useInboxStore((state) => state.refresh);
  const organize = useInboxStore((state) => state.organize);
  const markReviewed = useInboxStore((state) => state.markReviewed);
  const acknowledgeImportWarning = useInboxStore(
    (state) => state.acknowledgeImportWarning,
  );
  const smartClassify = useInboxStore((state) => state.smartClassify);
  const isLoading = useInboxStore((state) => state.isLoading);
  const loadError = useInboxStore((state) => state.loadError);
  const wikiCandidates = useInboxStore(s => s.wikiCandidates);
  const sectionErrors = useInboxStore(s => s.sectionErrors);
  const collections = useCollectionStore((state) => state.collections);
  const fetchCollections = useCollectionStore(
    (state) => state.fetchCollections,
  );
  const retryTask = useImportStore((state) => state.retryTask);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const refreshKnowledge = useKnowledgeStore((state) => state.refreshAll);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [collectionId, setCollectionId] = useState("");
  const [tagText, setTagText] = useState("");
  const [runningKind, setRunningKind] = useState<"semantic-pending" | null>(
    null,
  );
  const [acknowledgingTaskId, setAcknowledgingTaskId] = useState<string | null>(
    null,
  );
  const [classificationProgress, setClassificationProgress] = useState<{
    completed: number;
    total: number;
  } | null>(null);
  const classificationControllerRef = useRef<AbortController | null>(null);

  useEffect(() => {
    void refresh();
    void fetchCollections();
  }, [fetchCollections, refresh]);

  useEffect(() => () => classificationControllerRef.current?.abort(), []);

  const visible = useMemo(
    () =>
      filter === "all" ? items : items.filter((item) => item.kind === filter),
    [filter, items],
  );
  const selectableVisibleIds = useMemo(
    () =>
      visible.flatMap((item) => {
        const itemId = knowledgeItemId(item);
        return itemId ? [itemId] : [];
      }),
    [visible],
  );
  const selectedVisibleCount = selectableVisibleIds.filter((id) =>
    selectionIds.includes(id),
  ).length;
  const allVisibleSelected =
    selectableVisibleIds.length > 0 &&
    selectedVisibleCount === selectableVisibleIds.length;
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected;
  const selectedUnclassifiedIds = useMemo(() => {
    const selected = new Set(selectionIds);
    return items.flatMap((item) =>
      item.kind === "unclassified" && selected.has(item.itemId)
        ? [item.itemId]
        : [],
    );
  }, [items, selectionIds]);

  const openItem = async (id: string) => {
    setAppModule("library");
    setScope("all");
    await selectItem(id);
  };

  const applyOrganization = async () => {
    const tags = tagText
      .split(/[,，]/)
      .map((tag) => tag.trim())
      .filter(Boolean);
    const changed = await organize({
      ...(collectionId ? { collectionId } : {}),
      ...(tags.length ? { addTagNames: tags } : {}),
    });
    showToast(t("inbox.organized", "已整理 {{count}} 条内容", { count: changed }), "success");
    setTagText("");
  };

  const runSemantic = async () => {
    setRunningKind("semantic-pending");
    try {
      const { useSemanticStore } = await import("../../stores/semantic.store");
      const store = useSemanticStore.getState();
      if (store.isIndexing) {
        showToast(t("inbox.semanticRunning", "语义索引正在执行"), "info");
        return;
      }

      const notice = await store.runIndexing(false);
      // 处理中心已经展示本轮回执，避免稍后进入 AI 问答时重复弹出。
      useSemanticStore.getState().consumeNotice();
      if (!notice) {
        showToast(t("inbox.semanticStartFailed", "语义索引未能启动，请稍后重试"), "error");
      } else if (notice.kind === "done") {
        showToast(t("inbox.semanticIndexed", "已索引 {{count}} 条", { count: notice.indexed }), "success");
      } else if (notice.kind === "partial") {
        showToast(
          t("inbox.semanticPartial", "索引 {{indexed}} 条，{{failed}} 条失败：{{message}}", {
            indexed: notice.indexed,
            failed: notice.failed,
            message: notice.message ?? t("common.unknownError", "未知错误"),
          }),
          "warning",
        );
      } else if (notice.kind === "failed") {
        showToast(t("inbox.semanticFailed", "索引失败：{{message}}", { message: notice.message ?? t("common.unknownError", "未知错误") }), "error");
      } else if (notice.kind === "not-configured") {
        showToast(t("inbox.semanticNotConfigured", "尚未配置语义索引模型"), "error");
        requestSettingsSection("ai");
      } else {
        showToast(t("inbox.semanticNothing", "没有需要索引的内容"), "info");
      }
      await refresh();
    } catch (error) {
      showToast(
        t("inbox.semanticError", "语义索引执行失败：{{message}}", { message: error instanceof Error ? error.message : String(error) }),
        "error",
      );
    } finally {
      setRunningKind(null);
    }
  };

  const runWiki = () => {
    window.dispatchEvent(new Event("wiki-compile-preview"));
  };

  const acknowledgeWarning = async (taskId: string) => {
    setAcknowledgingTaskId(taskId);
    try {
      const changed = await acknowledgeImportWarning(taskId);
      showToast(
        changed > 0 ? t("inbox.warningAcknowledged", "已保留导入记录并从处理中心移除") : t("inbox.warningAlreadyHandled", "该警告已经处理"),
        changed > 0 ? "success" : "info",
      );
    } catch (error) {
      showToast(
        t("inbox.actionFailed", "操作失败：{{message}}", { message: error instanceof Error ? error.message : String(error) }),
        "error",
      );
    } finally {
      setAcknowledgingTaskId(null);
    }
  };

  const runSmartClassification = async () => {
    if (classificationControllerRef.current) {
      classificationControllerRef.current.abort();
      return;
    }
    if (selectedUnclassifiedIds.length === 0) return;
    const controller = new AbortController();
    classificationControllerRef.current = controller;
    setClassificationProgress({
      completed: 0,
      total: Math.ceil(selectedUnclassifiedIds.length / 20),
    });
    try {
      const result = await smartClassify(
        selectedUnclassifiedIds,
        collections.map((collection) => collection.name),
        {
          signal: controller.signal,
          onProgress: (completed, total) =>
            setClassificationProgress({ completed, total }),
        },
      );
      await Promise.all([fetchCollections(), refreshKnowledge()]);
      const created = result.createdCollectionNames;
      const summary =
        result.classified > 0
          ? created.length > 0
            ? t("inbox.classifiedWithNew", "已智能归类 {{count}} 条，新建 {{created}} 个知识库", { count: result.classified, created: created.length })
            : t("inbox.classifiedReused", "已智能归类 {{count}} 条，全部复用现有知识库", { count: result.classified })
          : t("inbox.nothingToClassify", "没有仍需归类的内容");
      showToast(
        result.skipped > 0
          ? t("inbox.classifySkipped", "{{summary}}；跳过 {{count}} 条已变更内容", { summary, count: result.skipped })
          : summary,
        result.classified > 0 ? "success" : "info",
        created.length > 0 ? { detail: created.join(t("inbox.nameSeparator", "、")) } : undefined,
      );
    } catch (error) {
      if (
        controller.signal.aborted ||
        (error instanceof Error && error.name === "AbortError")
      ) {
        showToast(t("inbox.classifyStopped", "已停止 AI 归类，尚未写入任何分类"), "info");
      } else if (
        error instanceof Error &&
        error.message === "AI_NOT_CONFIGURED"
      ) {
        showToast(t("inbox.aiNotConfigured", "尚未配置可用的 AI 文本模型"), "error");
        requestSettingsSection("ai");
      } else {
        showToast(
          t("inbox.classifyFailed", "AI 智能归类失败：{{message}}", { message: error instanceof Error ? error.message : String(error) }),
          "error",
        );
      }
    } finally {
      if (classificationControllerRef.current === controller) {
        classificationControllerRef.current = null;
        setClassificationProgress(null);
      }
    }
  };

  if (showThemeTasks) return <ThemedReadingTasks onBack={() => setShowThemeTasks(false)} />;
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-5">
        <h2 className="text-sm font-semibold text-foreground">{t("nav.inbox", "处理中心")}</h2>
        <button type="button" onClick={() => setShowThemeTasks(true)} className="rounded-lg border border-border px-2.5 py-1 text-xs hover:bg-accent">{t("themedReading.tasks", "主题页任务")}</button>
        <span className="text-xs text-muted-foreground">
          {filter === "wiki-pending"
            ? t("inbox.countWikiPending", "{{count}} 条内容待编译", { count: counts[filter] })
            : filter === "semantic-pending"
              ? t("inbox.countSemanticPending", "{{count}} 条内容待索引", { count: counts[filter] })
              : t("inbox.countPending", "{{count}} 条内容待处理", { count: filter === "all" ? total : counts[filter] })}
        </span>
        {selectableVisibleIds.length > 0 ? (
          <Checkbox
            checked={allVisibleSelected}
            indeterminate={someVisibleSelected}
            onChange={(checked) =>
              checked ? setSelection(selectableVisibleIds) : clearSelection()
            }
            label={t("inbox.selectAllVisible", "全选当前列表")}
            className="ml-auto"
          />
        ) : null}
        <button
          type="button"
          onClick={() => void refresh()}
          className={`${selectableVisibleIds.length > 0 ? "" : "ml-auto"} rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground`}
          title={t("common.refresh", "刷新")}
          aria-label={t("inbox.refresh", "刷新处理中心")}
        >
          <RefreshCwIcon
            className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`}
          />
        </button>
      </div>

      {selectionIds.length > 0 ? (
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-primary/5 px-5 py-2">
          <span className="text-sm text-foreground">
            {t("inbox.selectedCount", "已选 {{count}} 条", { count: selectionIds.length })}
          </span>
          <Select
            value={collectionId}
            onChange={setCollectionId}
            placeholder={t("inbox.moveToCollection", "归入知识库")}
            ariaLabel={t("inbox.moveToCollection", "归入知识库")}
            options={collections.map((collection) => ({
              value: collection.id,
              label: `${collection.icon ?? "📚"} ${collection.name}`,
            }))}
            className="w-44"
          />
          <input
            value={tagText}
            onChange={(event) => setTagText(event.target.value)}
            placeholder={t("inbox.addTagsPlaceholder", "添加标签，逗号分隔")}
            className="h-9 w-52 rounded-lg border border-border bg-background px-3 text-sm outline-none focus:border-primary/50"
          />
          <button
            type="button"
            onClick={() => void applyOrganization()}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground"
          >
            <FolderInputIcon className="h-4 w-4" />
            {t("inbox.organizeSelected", "批量整理")}
          </button>
          {selectedUnclassifiedIds.length > 0 ? (
            <button
              type="button"
              onClick={() => void runSmartClassification()}
              aria-busy={classificationProgress !== null}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-primary/30 px-3 text-sm font-medium text-primary hover:bg-primary/10"
            >
              {classificationProgress ? (
                <Loader2Icon
                  className="h-4 w-4 animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <SparklesIcon className="h-4 w-4" aria-hidden="true" />
              )}
              {classificationProgress
                ? t("inbox.stopClassify", "停止归类 {{completed}}/{{total}}", { completed: classificationProgress.completed, total: classificationProgress.total })
                : t("inbox.smartClassify", "AI 智能归类 {{count}}", { count: selectedUnclassifiedIds.length })}
            </button>
          ) : null}
          <button
            type="button"
            onClick={clearSelection}
            className="h-9 rounded-lg px-3 text-sm text-muted-foreground hover:bg-muted"
          >
            {t("common.cancel", "取消")}
          </button>
        </div>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {wikiCandidates ? <div className="mb-2 rounded-lg border border-border p-3 text-sm">{t("inbox.wikiCandidates", "Wiki：可执行 {{ready}} · 需复核 {{review}} · 等待重试/继续 {{waiting}} · 待选择升级 {{upgrade}}", { ready: wikiCandidates.ready, review: wikiCandidates.review, waiting: wikiCandidates.waiting, upgrade: wikiCandidates.upgrade })} <button className="text-primary hover:underline" onClick={() => window.dispatchEvent(new Event("wiki-compile-preview"))}>{t("inbox.wikiCandidatesView", "查看范围和任务")}</button></div> : null}
        {Object.entries(sectionErrors).map(([kind, error]) => <div key={kind} role="alert" className="mb-2 rounded-lg border border-border p-3 text-sm">{kind === "wiki" ? t("inbox.wikiStatusFailed", "Wiki状态读取失败：{{error}}", { error }) : t("inbox.semanticStatusFailed", "语义索引状态读取失败：{{error}}", { error })} <button className="text-primary hover:underline" onClick={() => void refresh()}>{t("common.retry", "重试")}</button></div>)}
        {isLoading && items.length === 0 ? (
          <div className="flex h-32 items-center justify-center">
            <Spinner size="sm" tone="muted" />
          </div>
        ) : loadError ? (
          <LoadErrorState message={loadError} onRetry={() => void refresh()} />
        ) : visible.length === 0 ? (
          <div className="flex h-52 flex-col items-center justify-center text-center">
            <CheckIcon className="h-10 w-10 text-emerald-500" />
            <p className="mt-3 text-sm font-medium text-foreground">
              {t("inbox.groupDone", "当前分组已处理完")}
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {visible.map((item) => {
              if ("count" in item) {
                const semantic = item.kind === "semantic-pending";
                const Icon = semantic ? ScanSearchIcon : NetworkIcon;
                const isRunning = runningKind === item.kind;
                return (
                  <div
                    key={item.id}
                    className="flex items-center gap-3 rounded-xl border border-primary/20 bg-primary/5 p-4"
                  >
                    <Icon className="h-5 w-5 text-primary" />
                    <div className="flex-1">
                      <p className="text-sm font-medium text-foreground">
                        {semantic ? t("inbox.semanticAggregateTitle", "语义索引待更新") : t("inbox.wikiAggregateTitle", "Wiki 待编译")}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {semantic
                          ? t("inbox.semanticAggregateDesc", "{{count}} 条内容待建立语义索引；不计入待处理内容数。", { count: item.count })
                          : t("inbox.wikiAggregateDesc", "{{count}} 条内容待编译为 Wiki；不计入待处理内容数。", { count: item.count })}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() =>
                        void (semantic ? runSemantic() : runWiki())
                      }
                      disabled={runningKind !== null}
                      aria-busy={isRunning}
                      className="inline-flex h-8 min-w-20 items-center justify-center gap-1.5 rounded-lg border border-border px-3 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      {isRunning ? (
                        <>
                          <Loader2Icon
                            className="h-3.5 w-3.5 animate-spin"
                            aria-hidden="true"
                          />
                          {semantic ? t("inbox.indexing", "索引中…") : t("wiki.compilingShort", "编译中…")}
                        </>
                      ) : (
                        t("inbox.runNow", "立即执行")
                      )}
                    </button>
                  </div>
                );
              }
              const itemId = knowledgeItemId(item);
              const selected = itemId ? selectionIds.includes(itemId) : false;
              return (
                <div
                  key={item.id}
                  className="flex items-start gap-3 rounded-xl border border-border bg-background/60 p-4"
                >
                  {itemId ? (
                    <Checkbox
                      checked={selected}
                      onChange={() => toggleSelection(itemId)}
                      ariaLabel={t("inbox.selectItem", "选择 {{title}}", { title: item.title })}
                      className="mt-1 shrink-0"
                    />
                  ) : item.kind === "import-issue" ? (
                    <AlertTriangleIcon className="mt-0.5 h-4 w-4 text-amber-500" />
                  ) : (
                    <CompassIcon className="mt-0.5 h-4 w-4 text-primary" />
                  )}
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-foreground">
                      {item.title || t("library.untitled", "无标题")}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {item.kind === "review-required"
                        ? item.reasons.join("；") || t("inbox.reviewRequiredReason", "采集结果需要人工复核")
                        : item.kind === "unclassified"
                          ? t("inbox.unclassifiedReason", "尚未归入任何知识库")
                          : item.kind === "import-issue"
                            ? item.message
                            : t("inbox.discoveryReason", "定时发现的新候选，尚未导入")}
                    </p>
                  </div>
                  {itemId ? (
                    <button
                      type="button"
                      onClick={() => void openItem(itemId)}
                      className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted"
                    >
                      {t("common.open", "打开")}
                    </button>
                  ) : null}
                  {item.kind === "review-required" ? (
                    <button
                      type="button"
                      onClick={() => void markReviewed([item.itemId])}
                      className="h-8 rounded-lg bg-primary px-2.5 text-xs text-primary-foreground"
                    >
                      {t("library.markReviewed", "标记已复核")}
                    </button>
                  ) : null}
                  {item.kind === "import-issue" && item.status === "failed" ? (
                    <button
                      type="button"
                      onClick={() => void retryTask(item.taskId).then(refresh)}
                      className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted"
                    >
                      {t("common.retry", "重试")}
                    </button>
                  ) : null}
                  {item.kind === "import-issue" &&
                  (item.duplicateItemId || item.resultItemId) ? (
                    <button
                      type="button"
                      onClick={() =>
                        void openItem(
                          item.duplicateItemId || item.resultItemId!,
                        )
                      }
                      className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted"
                    >
                      {t("inbox.viewItem", "查看条目")}
                    </button>
                  ) : null}
                  {item.kind === "import-issue" &&
                  item.status === "completed" ? (
                    <button
                      type="button"
                      onClick={() => void acknowledgeWarning(item.taskId)}
                      disabled={acknowledgingTaskId === item.taskId}
                      className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      {acknowledgingTaskId === item.taskId
                        ? t("inbox.processing", "处理中…")
                        : t("inbox.dismissWarning", "不再提醒")}
                    </button>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
