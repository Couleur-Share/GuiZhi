import { useEffect } from "react";
import { Loader2Icon, SparklesIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useWikiStore } from "../../stores/wiki.store";

/**
 * Wiki 模块侧栏：编译状态摘要 + 立即编译 + 最近更新页面。
 */
export function SidebarWikiPanel() {
  const { t } = useTranslation();
  const catalog = useWikiStore((state) => state.catalog);
  const status = useWikiStore((state) => state.status);
  const selectedPageId = useWikiStore((state) => state.selectedPageId);
  const isCompiling = useWikiStore((state) => state.isCompiling);
  const compileProgress = useWikiStore((state) => state.compileProgress);
  const refresh = useWikiStore((state) => state.refresh);
  const selectPage = useWikiStore((state) => state.selectPage);
  const compileNow = useWikiStore((state) => state.compileNow);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const pendingCount = status
    ? Math.max(0, status.eligibleItemCount - status.compiledItemCount)
    : 0;
  const recentPages = catalog.slice(0, 15);

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3">
      <div className="pt-3">
        <div className="rounded-lg border border-sidebar-border/70 px-3 py-2.5">
          <p className="text-xs text-sidebar-foreground/60">
            {t("wiki.panelStatus", "{{pages}} 个页面 · {{compiled}}/{{eligible}} 条已编译", {
              pages: status?.pageCount ?? 0,
              compiled: status?.compiledItemCount ?? 0,
              eligible: status?.eligibleItemCount ?? 0,
            })}
          </p>
          <button
            type="button"
            onClick={() => void compileNow()}
            disabled={isCompiling}
            className="mt-2 flex w-full items-center justify-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-60"
          >
            {isCompiling ? (
              <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {isCompiling
              ? compileProgress
                ? t("wiki.panelCompiling", "编译中 {{current}}/{{total}}", {
                    current: compileProgress.current,
                    total: compileProgress.total,
                  })
                : t("wiki.panelCompilingSimple", "编译中…")
              : pendingCount > 0
                ? t("wiki.panelCompilePending", "编译 {{count}} 条新内容", {
                    count: pendingCount,
                  })
                : t("wiki.panelCompileNow", "立即编译")}
          </button>
        </div>
      </div>

      <div className="flex items-center gap-2 px-3 pb-1 pt-4">
        <span className="text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/50">
          {t("wiki.panelRecent", "最近更新")}
        </span>
        <div className="h-px flex-1 bg-sidebar-border/60" />
      </div>

      {recentPages.length === 0 ? (
        <p className="px-3 py-1 text-xs text-sidebar-foreground/40">
          {t("wiki.panelEmpty", "还没有 Wiki 页面，编译后自动生成")}
        </p>
      ) : (
        recentPages.map((page) => (
          <div key={page.id} className="w-full py-0.5">
            <button
              type="button"
              onClick={() => void selectPage(page.id)}
              title={page.title}
              className={`flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-left transition-all duration-smooth ${
                selectedPageId === page.id
                  ? "bg-primary text-white shadow-sm"
                  : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
              }`}
            >
              <span className="min-w-0 flex-1 truncate text-sm">
                {page.title}
              </span>
            </button>
          </div>
        ))
      )}
    </div>
  );
}
