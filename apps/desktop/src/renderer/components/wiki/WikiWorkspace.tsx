import { useEffect, useMemo, useState } from "react";
import {
  BookOpenIcon,
  Link2Icon,
  ListIcon,
  Loader2Icon,
  NetworkIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SearchIcon,
  SparklesIcon,
  SquareIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { WikiCatalogEntry, WikiPageKind } from "@guizhi/shared/types";
import { useWikiStore } from "../../stores/wiki.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useSettingsStore } from "../../stores/settings.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Spinner } from "../ui/Spinner";
import { WikiMarkdown } from "./WikiMarkdown";
import { WikiGraphView } from "./WikiGraphView";
import { formatItemTime } from "../library/type-meta";

const KIND_LABEL_KEYS: Record<WikiPageKind, [string, string]> = {
  topic: ["wiki.kindTopic", "主题"],
  entity: ["wiki.kindEntity", "实体"],
  concept: ["wiki.kindConcept", "概念"],
};

function KindBadge({ kind }: { kind: WikiPageKind }) {
  const { t } = useTranslation();
  const [key, fallback] = KIND_LABEL_KEYS[kind];
  return (
    <span className="inline-flex shrink-0 items-center rounded-full bg-muted/70 px-1.5 py-0.5 text-[10px] text-muted-foreground">
      {t(key, fallback)}
    </span>
  );
}

function CatalogList() {
  const { t } = useTranslation();
  const catalog = useWikiStore((state) => state.catalog);
  const selectedPageId = useWikiStore((state) => state.selectedPageId);
  const selectPage = useWikiStore((state) => state.selectPage);
  const [filter, setFilter] = useState("");

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) {
      return catalog;
    }
    return catalog.filter(
      (entry) =>
        entry.title.toLowerCase().includes(query) ||
        entry.summary.toLowerCase().includes(query),
    );
  }, [catalog, filter]);

  return (
    <div className="flex h-full min-h-0 w-72 shrink-0 flex-col border-r border-border">
      <div className="shrink-0 px-3 py-2">
        <div className="flex h-8 items-center gap-1.5 rounded-lg border border-border bg-background/60 px-2.5">
          <SearchIcon
            className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70"
            aria-hidden="true"
          />
          <input
            type="text"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={t("wiki.filterPlaceholder", "筛选页面…")}
            className="min-w-0 flex-1 bg-transparent text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        {filtered.length === 0 ? (
          <p className="px-2 py-6 text-center text-xs text-muted-foreground/70">
            {catalog.length === 0
              ? t("wiki.catalogEmpty", "还没有 Wiki 页面")
              : t("wiki.filterEmpty", "没有匹配的页面")}
          </p>
        ) : (
          <div className="space-y-0.5">
            {filtered.map((entry) => (
              <button
                key={entry.id}
                type="button"
                onClick={() => void selectPage(entry.id)}
                className={`w-full rounded-lg px-2.5 py-2 text-left transition-colors ${
                  entry.id === selectedPageId
                    ? "bg-primary/10"
                    : "hover:bg-accent/50"
                }`}
              >
                <div className="flex items-center gap-1.5">
                  <span
                    className={`min-w-0 flex-1 truncate text-sm ${
                      entry.id === selectedPageId
                        ? "font-medium text-primary"
                        : "text-foreground"
                    }`}
                  >
                    {entry.title}
                  </span>
                  <KindBadge kind={entry.kind} />
                </div>
                {entry.summary ? (
                  <p className="mt-0.5 truncate text-xs text-muted-foreground/80">
                    {entry.summary}
                  </p>
                ) : null}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function RelatedEntryChips({
  label,
  icon,
  entries,
  onOpen,
}: {
  label: string;
  icon: React.ReactNode;
  entries: { id: string; title: string }[];
  onOpen: (id: string) => void;
}) {
  if (entries.length === 0) {
    return null;
  }
  return (
    <div>
      <p className="mb-1.5 flex items-center gap-1 text-xs font-medium text-muted-foreground">
        {icon}
        {label}
      </p>
      <div className="flex flex-wrap gap-1.5">
        {entries.map((entry) => (
          <button
            key={entry.id}
            type="button"
            onClick={() => onOpen(entry.id)}
            title={entry.title}
            className="inline-flex max-w-64 items-center rounded-lg border border-border/70 bg-background/60 px-2 py-1 text-xs text-foreground transition-colors hover:border-primary/40 hover:bg-primary/5"
          >
            <span className="min-w-0 truncate">{entry.title}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

function PageDetail() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const detail = useWikiStore((state) => state.pageDetail);
  const selectedPageId = useWikiStore((state) => state.selectedPageId);
  const pageRevisions = useWikiStore((state) => state.pageRevisions);
  const restorePreviousRevision = useWikiStore(
    (state) => state.restorePreviousRevision,
  );
  const openByLinkTarget = useWikiStore((state) => state.openByLinkTarget);
  const selectPage = useWikiStore((state) => state.selectPage);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setAppModule = useUIStore((state) => state.setAppModule);

  if (!selectedPageId) {
    return (
      <div className="flex h-full flex-1 items-center justify-center px-8 text-center text-sm text-muted-foreground">
        {t("wiki.noSelection", "在左侧选择一个页面")}
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="flex h-full flex-1 items-center justify-center">
        <Spinner size="sm" tone="muted" />
      </div>
    );
  }

  const { page, backlinks, sources } = detail;

  const navigateLink = (target: string) => {
    void openByLinkTarget(target).then((found) => {
      if (!found) {
        showToast(
          t("wiki.linkNotFound", "页面「{{target}}」不存在", { target }),
          "info",
        );
      }
    });
  };

  const openSourceItem = async (itemId: string) => {
    setAppModule("library");
    await selectItem(itemId);
  };

  const previousRevision = pageRevisions[0];
  const restorePrevious = async () => {
    const ok = await restorePreviousRevision();
    showToast(
      ok
        ? t("wiki.revisionRestored", "已恢复上一版内容")
        : t("wiki.revisionRestoreFailed", "恢复失败"),
      ok ? "success" : "error",
    );
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-3xl px-6 py-5">
        <div className="mb-1 flex items-center gap-2">
          <h1 className="min-w-0 flex-1 text-xl font-semibold text-foreground">
            {page.title}
          </h1>
          <KindBadge kind={page.kind} />
        </div>
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <p className="text-xs text-muted-foreground/70">
            {t("wiki.generatedAt", "由 {{model}} 生成于 {{time}}", {
              model: page.model || "AI",
              time: formatItemTime(page.generatedAt),
            })}
          </p>
          {previousRevision ? (
            <button
              type="button"
              onClick={() => void restorePrevious()}
              title={t("wiki.restorePreviousHint", "恢复到 {{time}} 的内容", {
                time: formatItemTime(previousRevision.createdAt),
              })}
              className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <RotateCcwIcon className="h-3 w-3" aria-hidden="true" />
              {t("wiki.restorePrevious", "恢复上一版")}
            </button>
          ) : null}
        </div>

        <WikiMarkdown body={page.body} onNavigate={navigateLink} />

        {backlinks.length > 0 || sources.length > 0 ? (
          <div className="mt-6 space-y-4 border-t border-border/60 pt-4">
            <RelatedEntryChips
              label={t("wiki.backlinks", "反向链接")}
              icon={<Link2Icon className="h-3.5 w-3.5" aria-hidden="true" />}
              entries={backlinks}
              onOpen={(id) => void selectPage(id)}
            />
            <RelatedEntryChips
              label={t("wiki.sources", "来源条目")}
              icon={<BookOpenIcon className="h-3.5 w-3.5" aria-hidden="true" />}
              entries={sources.map((source) => ({
                id: source.itemId,
                title: source.title,
              }))}
              onOpen={(id) => void openSourceItem(id)}
            />
          </div>
        ) : null}
      </div>
    </div>
  );
}

/**
 * 自动编译开关：工具栏里唯一的持久设置项。
 * 做成药丸开关而非复选框——原生 checkbox 在深色玻璃工具栏里是个突兀的白方块，
 * 且与左右两侧的分段控件、动作按钮不在同一视觉语言上。
 */
function AutoCompileToggle() {
  const { t } = useTranslation();
  const enabled = useSettingsStore((state) => state.wikiCompileEnabled);
  const setEnabled = useSettingsStore((state) => state.setWikiCompileEnabled);

  return (
    <button
      type="button"
      role="switch"
      aria-checked={enabled}
      onClick={() => setEnabled(!enabled)}
      title={t("wiki.autoCompileHint", "开启后每 5 分钟自动把新条目编译进 Wiki")}
      className={`inline-flex h-8 shrink-0 items-center gap-2 rounded-lg border px-2.5 text-xs transition-colors ${
        enabled
          ? "border-primary/40 bg-primary/10 text-primary"
          : "border-border text-muted-foreground hover:bg-muted/60 hover:text-foreground"
      }`}
    >
      <span
        aria-hidden="true"
        className={`relative h-3.5 w-6 shrink-0 rounded-full transition-colors duration-base ${
          enabled ? "bg-primary" : "bg-muted-foreground/25"
        }`}
      >
        <span
          className={`absolute top-0.5 h-2.5 w-2.5 rounded-full transition-all duration-base ${
            enabled ? "left-3 bg-white" : "left-0.5 bg-muted-foreground/70"
          }`}
        />
      </span>
      {t("wiki.autoCompile", "自动编译")}
    </button>
  );
}

/**
 * Wiki 模块（ADR 0023）：AI 从知识条目编译出的互链知识页网络。
 * 头部提供编译控制（自动开关 / 立即编译 / 停止 / 全量重建）与状态。
 */
export function WikiWorkspace() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const status = useWikiStore((state) => state.status);
  const catalog = useWikiStore((state) => state.catalog);
  const isCompiling = useWikiStore((state) => state.isCompiling);
  const compileProgress = useWikiStore((state) => state.compileProgress);
  const compileNotice = useWikiStore((state) => state.compileNotice);
  const dismissNotice = useWikiStore((state) => state.dismissNotice);
  const refresh = useWikiStore((state) => state.refresh);
  const compileNow = useWikiStore((state) => state.compileNow);
  const cancelCompile = useWikiStore((state) => state.cancelCompile);
  const rebuildAll = useWikiStore((state) => state.rebuildAll);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const viewMode = useWikiStore((state) => state.viewMode);
  const setViewMode = useWikiStore((state) => state.setViewMode);
  const [showRebuildConfirm, setShowRebuildConfirm] = useState(false);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 编译结果提示
  useEffect(() => {
    if (!compileNotice) {
      return;
    }
    if (compileNotice.kind === "done") {
      showToast(
        compileNotice.message
          ? t("wiki.compileDone", "编译完成（{{result}}）", {
              result: compileNotice.message,
            })
          : t("wiki.compileNothing", "没有需要编译的条目"),
        "success",
      );
    } else if (compileNotice.kind === "cancelled") {
      showToast(
        compileNotice.message
          ? t("wiki.compileStoppedPartial", "已停止编译（已完成 {{result}}）", {
              result: compileNotice.message,
            })
          : t("wiki.compileStopped", "已停止编译"),
        "info",
      );
    } else if (compileNotice.kind === "not-configured") {
      showToast(t("ask.notConfigured", "尚未配置 AI 服务"), "error");
      requestSettingsSection("ai");
    } else {
      showToast(
        t("wiki.compileFailed", "编译失败：{{message}}", {
          message: compileNotice.message,
        }),
        "error",
      );
    }
    dismissNotice();
  }, [compileNotice, dismissNotice, requestSettingsSection, showToast, t]);

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="truncate text-sm font-semibold text-foreground">
          {t("nav.wiki", "Wiki")}
        </h2>
        {status ? (
          <span className="truncate text-xs text-muted-foreground/70">
            {t("wiki.statusLine", "{{pages}} 页 · 已编译 {{compiled}}/{{eligible}} 条", {
              pages: status.pageCount,
              compiled: status.compiledItemCount,
              eligible: status.eligibleItemCount,
            })}
          </span>
        ) : null}
        <span className="min-w-0 flex-1" />

        {/* 目录 / 图谱视图切换 */}
        <div className="flex items-center rounded-lg border border-border p-0.5">
          <button
            type="button"
            onClick={() => setViewMode("catalog")}
            title={t("wiki.viewCatalog", "目录视图")}
            aria-label={t("wiki.viewCatalog", "目录视图")}
            aria-pressed={viewMode === "catalog"}
            className={`inline-flex h-6 w-7 items-center justify-center rounded-md transition-colors ${
              viewMode === "catalog"
                ? "bg-primary/15 text-primary"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            <ListIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
          <button
            type="button"
            onClick={() => setViewMode("graph")}
            title={t("wiki.viewGraph", "关系图谱")}
            aria-label={t("wiki.viewGraph", "关系图谱")}
            aria-pressed={viewMode === "graph"}
            className={`inline-flex h-6 w-7 items-center justify-center rounded-md transition-colors ${
              viewMode === "graph"
                ? "bg-primary/15 text-primary"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            <NetworkIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        </div>

        {/* 视图控件与编译控件分属两类，用竖线分组 */}
        <div className="h-4 w-px shrink-0 bg-border" aria-hidden="true" />

        <AutoCompileToggle />

        {isCompiling ? (
          <>
            <span className="inline-flex h-8 max-w-80 items-center gap-1.5 rounded-lg bg-primary/10 px-3 text-xs text-primary">
              <Loader2Icon
                className="h-3.5 w-3.5 shrink-0 animate-spin"
                aria-hidden="true"
              />
              {/* 条目标题长度不可控，不截断会把整条工具栏挤变形 */}
              <span className="truncate">
                {compileProgress
                  ? t("wiki.compiling", "编译中（{{current}}/{{total}}）：{{title}}", {
                      current: compileProgress.current,
                      total: compileProgress.total,
                      title: compileProgress.currentTitle,
                    })
                  : t("wiki.compilingShort", "编译中…")}
              </span>
            </span>
            <button
              type="button"
              onClick={cancelCompile}
              title={t("wiki.stopCompile", "停止编译")}
              aria-label={t("wiki.stopCompile", "停止编译")}
              className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
            >
              <SquareIcon className="h-3.5 w-3.5" aria-hidden="true" />
            </button>
          </>
        ) : (
          <>
            <button
              type="button"
              onClick={() => void compileNow()}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              <SparklesIcon className="h-4 w-4" aria-hidden="true" />
              {t("wiki.compileNow", "立即编译")}
            </button>
            {catalog.length > 0 ? (
              <button
                type="button"
                onClick={() => setShowRebuildConfirm(true)}
                title={t("wiki.rebuild", "全量重建")}
                aria-label={t("wiki.rebuild", "全量重建")}
                className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
              >
                <RefreshCwIcon className="h-4 w-4" aria-hidden="true" />
              </button>
            ) : null}
          </>
        )}
      </div>

      {catalog.length === 0 && !isCompiling ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <NetworkIcon
            className="h-10 w-10 text-muted-foreground/40"
            aria-hidden="true"
          />
          <p className="text-sm text-muted-foreground">
            {t("wiki.empty", "Wiki 还是空的")}
          </p>
          <p className="max-w-md text-xs text-muted-foreground/70">
            {t(
              "wiki.emptyHint",
              "AI 会把知识条目编译成互相链接的 Wiki 页面。点击「立即编译」开始，或打开「自动编译」让它在后台持续更新。",
            )}
          </p>
        </div>
      ) : viewMode === "graph" ? (
        <div className="flex min-h-0 flex-1">
          <WikiGraphView />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1">
          <CatalogList />
          <PageDetail />
        </div>
      )}

      {showRebuildConfirm ? (
        <ConfirmDialog
          isOpen={showRebuildConfirm}
          title={t("wiki.rebuild", "全量重建")}
          message={t(
            "wiki.rebuildConfirm",
            "将清空现有全部 Wiki 页面并重新编译所有条目（清理已删除条目残留知识的唯一方式）。编译会消耗较多 AI 调用，确定继续？",
          )}
          confirmText={t("wiki.rebuild", "全量重建")}
          variant="destructive"
          onConfirm={() => {
            setShowRebuildConfirm(false);
            void rebuildAll();
          }}
          onClose={() => setShowRebuildConfirm(false)}
        />
      ) : null}
    </div>
  );
}
