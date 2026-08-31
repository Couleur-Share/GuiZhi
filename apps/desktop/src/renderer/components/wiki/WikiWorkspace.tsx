import { useEffect, useState, type CSSProperties } from "react";
import {
  ListIcon,
  Loader2Icon,
  NetworkIcon,
  RefreshCwIcon,
  SparklesIcon,
  SquareIcon,
  TimerIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useWikiStore } from "../../stores/wiki.store";
import { useSettingsStore } from "../../stores/settings.store";
import {
  WIKI_CATALOG_PANE_WIDTH_DEFAULT,
  WIKI_CATALOG_PANE_WIDTH_MAX,
  WIKI_CATALOG_PANE_WIDTH_MIN,
  useUIStore,
} from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { ColumnResizer } from "../ui/ColumnResizer";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { LoadErrorState } from "../ui/LoadErrorState";
import { Spinner } from "../ui/Spinner";
import { WikiCatalogList } from "./WikiCatalogList";
import { WikiGraphView } from "./WikiGraphView";
import { WikiPageDetail } from "./WikiPageDetail";

/**
 * 自动编译状态指示：开关本体在设置页（通用 → Wiki），这里只在开启时留一枚
 * 只读 chip。后台编译全程静默、失败也被吞掉，工具栏若不留痕迹，用户将无从
 * 察觉有个进程每 5 分钟消耗一次 AI 额度。点击回到设置页关掉它。
 */
function AutoCompileIndicator() {
  const { t } = useTranslation();
  const enabled = useSettingsStore((state) => state.wikiCompileEnabled);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );

  if (!enabled) {
    return null;
  }

  return (
    <button
      type="button"
      onClick={() => requestSettingsSection("general")}
      title={t(
        "wiki.autoCompileOnHint",
        "自动编译已开启：每 5 分钟把新条目编译进 Wiki。点击前往设置修改。",
      )}
      className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-primary/40 bg-primary/10 px-2.5 text-xs text-primary transition-colors hover:bg-primary/20"
    >
      <TimerIcon className="h-3.5 w-3.5" aria-hidden="true" />
      {t("wiki.autoCompile", "自动编译")}
    </button>
  );
}

function ViewModeToggle() {
  const { t } = useTranslation();
  const viewMode = useWikiStore((state) => state.viewMode);
  const setViewMode = useWikiStore((state) => state.setViewMode);

  const buttonClass = (active: boolean) =>
    `inline-flex h-6 w-7 items-center justify-center rounded-md transition-colors ${
      active
        ? "bg-primary/15 text-primary"
        : "text-muted-foreground hover:text-foreground"
    }`;

  return (
    <div className="flex shrink-0 items-center rounded-lg border border-border p-0.5">
      <button
        type="button"
        onClick={() => setViewMode("catalog")}
        title={t("wiki.viewCatalog", "目录视图")}
        aria-label={t("wiki.viewCatalog", "目录视图")}
        aria-pressed={viewMode === "catalog"}
        className={buttonClass(viewMode === "catalog")}
      >
        <ListIcon className="h-3.5 w-3.5" aria-hidden="true" />
      </button>
      <button
        type="button"
        onClick={() => setViewMode("graph")}
        title={t("wiki.viewGraph", "关系图谱")}
        aria-label={t("wiki.viewGraph", "关系图谱")}
        aria-pressed={viewMode === "graph"}
        className={buttonClass(viewMode === "graph")}
      >
        <NetworkIcon className="h-3.5 w-3.5" aria-hidden="true" />
      </button>
    </div>
  );
}

/**
 * Wiki 模块（ADR 0023）：AI 从知识条目编译出的互链知识页网络。
 * 顶栏是这个模块唯一的编译入口与状态出口，侧栏只负责筛选。
 */
export function WikiWorkspace() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const status = useWikiStore((state) => state.status);
  const catalog = useWikiStore((state) => state.catalog);
  const hasLoaded = useWikiStore((state) => state.hasLoaded);
  const loadError = useWikiStore((state) => state.loadError);
  const isCompiling = useWikiStore((state) => state.isCompiling);
  const compileProgress = useWikiStore((state) => state.compileProgress);
  const compileNotice = useWikiStore((state) => state.compileNotice);
  const dismissNotice = useWikiStore((state) => state.dismissNotice);
  const refresh = useWikiStore((state) => state.refresh);
  const compileNow = useWikiStore((state) => state.compileNow);
  const cancelCompile = useWikiStore((state) => state.cancelCompile);
  const rebuildAll = useWikiStore((state) => state.rebuildAll);
  const viewMode = useWikiStore((state) => state.viewMode);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const catalogPaneWidth = useUIStore((state) => state.wikiCatalogPaneWidth);
  const setCatalogPaneWidth = useUIStore(
    (state) => state.setWikiCatalogPaneWidth,
  );
  const [showRebuildConfirm, setShowRebuildConfirm] = useState(false);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 编译结果提示
  useEffect(() => {
    if (!compileNotice) {
      return;
    }
    const detail = compileNotice.detail
      ? { detail: compileNotice.detail }
      : undefined;
    if (compileNotice.kind === "done") {
      showToast(
        compileNotice.message
          ? t("wiki.compileDone", "编译完成（{{result}}）", {
              result: compileNotice.message,
            })
          : t("wiki.compileNothing", "没有需要编译的条目"),
        "success",
      );
    } else if (compileNotice.kind === "partial") {
      // 有条目没编出来就不能报绿色，原因逐条挂在详情里
      showToast(
        t("wiki.compilePartial", "编译完成 {{result}}，{{failed}} 条未能生成", {
          result: compileNotice.message,
          failed: compileNotice.detail?.split("\n").length ?? 0,
        }),
        "warning",
        detail,
      );
    } else if (compileNotice.kind === "cancelled") {
      showToast(
        compileNotice.message
          ? t("wiki.compileStoppedPartial", "已停止编译（已完成 {{result}}）", {
              result: compileNotice.message,
            })
          : t("wiki.compileStopped", "已停止编译"),
        "info",
        detail,
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

  const pendingCount = status
    ? Math.max(0, status.eligibleItemCount - status.compiledItemCount)
    : 0;

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="shrink-0 text-sm font-semibold text-foreground">
          {t("nav.wiki", "Wiki")}
        </h2>
        {status ? (
          <span className="truncate text-xs text-muted-foreground">
            {t("wiki.statusLine", "{{pages}} 页 · 已编译 {{compiled}}/{{eligible}} 条", {
              pages: status.pageCount,
              compiled: status.compiledItemCount,
              eligible: status.eligibleItemCount,
            })}
          </span>
        ) : null}
        <span className="min-w-0 flex-1" />

        <ViewModeToggle />
        {/* 编译控件的形态全取决于目录与状态：待编译条数决定按钮文案的宽度，
            有没有页面决定重建按钮在不在。数据到位前先不画，免得工具栏在
            首帧之后横向弹一下 */}
        {hasLoaded ? (
          <>
            {/* 视图控件与编译控件分属两类，用竖线分组 */}
            <div className="h-4 w-px shrink-0 bg-border" aria-hidden="true" />
            <AutoCompileIndicator />

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
                {/* 没有待编译条目时点了也只会弹「没有需要编译的条目」，直接置灰 */}
                <button
                  type="button"
                  onClick={() => void compileNow()}
                  disabled={pendingCount === 0}
                  title={
                    pendingCount === 0
                      ? t("wiki.compileUpToDate", "所有条目都已编译进 Wiki")
                      : undefined
                  }
                  className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <SparklesIcon className="h-4 w-4" aria-hidden="true" />
                  {pendingCount > 0
                    ? t("wiki.compilePending", "编译 {{count}} 条新内容", {
                        count: pendingCount,
                      })
                    : t("wiki.compileNow", "立即编译")}
                </button>
                {catalog.length > 0 ? (
                  <button
                    type="button"
                    onClick={() => setShowRebuildConfirm(true)}
                    title={t("wiki.rebuild", "全量重建")}
                    aria-label={t("wiki.rebuild", "全量重建")}
                    className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
                  >
                    <RefreshCwIcon className="h-4 w-4" aria-hidden="true" />
                  </button>
                ) : null}
              </>
            )}
          </>
        ) : null}
      </div>

      {!hasLoaded ? (
        // 目录读出来之前不画空态：否则每次进 Wiki 都先铺一屏「还是空的」，
        // 再被两栏布局整块顶掉
        <div className="delayed-fade-in flex flex-1 items-center justify-center">
          <Spinner tone="muted" />
        </div>
      ) : loadError && catalog.length === 0 ? (
        <div className="flex flex-1 items-center justify-center">
          <LoadErrorState message={loadError} onRetry={() => void refresh()} />
        </div>
      ) : catalog.length === 0 && !isCompiling ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <NetworkIcon
            className="h-10 w-10 text-muted-foreground/40"
            aria-hidden="true"
          />
          <p className="text-sm text-muted-foreground">
            {t("wiki.empty", "Wiki 还是空的")}
          </p>
          <p className="max-w-md text-xs text-muted-foreground">
            {t(
              "wiki.emptyHint",
              "AI 会把知识条目编译成互相链接的 Wiki 页面。点击「立即编译」开始，或在「设置 → 通用 → Wiki」里打开自动编译，让它在后台持续更新。",
            )}
          </p>
        </div>
      ) : viewMode === "graph" ? (
        <div className="flex min-h-0 flex-1">
          <WikiGraphView />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1">
          <div
            className="relative min-h-0 shrink-0 border-r border-border"
            style={{ width: `${catalogPaneWidth}px` } as CSSProperties}
          >
            <WikiCatalogList />
            <div className="absolute inset-y-0 right-0 z-10 flex">
              <ColumnResizer
                currentWidth={catalogPaneWidth}
                min={WIKI_CATALOG_PANE_WIDTH_MIN}
                max={WIKI_CATALOG_PANE_WIDTH_MAX}
                defaultWidth={WIKI_CATALOG_PANE_WIDTH_DEFAULT}
                onResize={setCatalogPaneWidth}
                ariaLabel={t("wiki.resizeCatalog", "调整目录宽度")}
              />
            </div>
          </div>
          <WikiPageDetail />
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
