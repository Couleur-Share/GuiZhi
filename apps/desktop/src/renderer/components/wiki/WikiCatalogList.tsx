import { useEffect, useMemo, useState } from "react";
import { Link2Icon, PencilIcon, SearchIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { WikiPageKind } from "@guizhi/shared/types";
import {
  selectVisibleCatalog,
  useWikiStore,
  type WikiCatalogSort,
} from "../../stores/wiki.store";
import { LoadErrorState } from "../ui/LoadErrorState";
import { Spinner } from "../ui/Spinner";

const KIND_LABEL_KEYS: Record<WikiPageKind, [string, string]> = {
  topic: ["wiki.kindTopic", "主题"],
  entity: ["wiki.kindEntity", "实体"],
  concept: ["wiki.kindConcept", "概念"],
};

export function WikiKindBadge({ kind }: { kind: WikiPageKind }) {
  const { t } = useTranslation();
  const [key, fallback] = KIND_LABEL_KEYS[kind];
  return (
    <span className="inline-flex shrink-0 items-center rounded-full bg-muted/70 px-1.5 py-0.5 text-[10px] text-muted-foreground">
      {t(key, fallback)}
    </span>
  );
}

const SORT_OPTIONS: { id: WikiCatalogSort; key: string; fallback: string }[] = [
  { id: "recent", key: "wiki.sortRecent", fallback: "最近更新" },
  { id: "linked", key: "wiki.sortLinked", fallback: "被引用最多" },
  { id: "title", key: "wiki.sortTitle", fallback: "标题" },
];

/** 搜索输入到发起 FTS 查询的静默期 */
const SEARCH_DEBOUNCE_MS = 250;

/**
 * 目录列。搜索走 wiki_fts（覆盖正文）而不是标题子串匹配——
 * 索引早就建好了，之前只有 AI 问答在用，界面这个框搜不到正文里的任何词。
 */
export function WikiCatalogList() {
  const { t } = useTranslation();
  const catalog = useWikiStore((state) => state.catalog);
  const backlinkCounts = useWikiStore((state) => state.backlinkCounts);
  const catalogFilter = useWikiStore((state) => state.catalogFilter);
  const catalogSort = useWikiStore((state) => state.catalogSort);
  const setCatalogSort = useWikiStore((state) => state.setCatalogSort);
  const searchQuery = useWikiStore((state) => state.searchQuery);
  const searchHitIds = useWikiStore((state) => state.searchHitIds);
  const isSearching = useWikiStore((state) => state.isSearching);
  const searchError = useWikiStore((state) => state.searchError);
  const runSearch = useWikiStore((state) => state.runSearch);
  const selectedPageId = useWikiStore((state) => state.selectedPageId);
  const selectPage = useWikiStore((state) => state.selectPage);

  const [input, setInput] = useState(searchQuery);

  useEffect(() => {
    if (input === searchQuery) {
      return;
    }
    const timer = window.setTimeout(
      () => void runSearch(input),
      SEARCH_DEBOUNCE_MS,
    );
    return () => window.clearTimeout(timer);
  }, [input, searchQuery, runSearch]);

  const visible = useMemo(
    () =>
      selectVisibleCatalog({
        catalog,
        backlinkCounts,
        filter: catalogFilter,
        sort: catalogSort,
        searchHitIds,
      }),
    [catalog, backlinkCounts, catalogFilter, catalogSort, searchHitIds],
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 space-y-1.5 px-3 py-2">
        <div className="flex h-8 items-center gap-1.5 rounded-lg border border-border bg-background/60 px-2.5">
          <SearchIcon
            className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70"
            aria-hidden="true"
          />
          <input
            type="text"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            placeholder={t("wiki.filterPlaceholder", "搜索标题与正文…")}
            className="min-w-0 flex-1 bg-transparent text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
          />
          {isSearching ? <Spinner size="xs" tone="muted" /> : null}
          {input ? (
            <button
              type="button"
              onClick={() => {
                setInput("");
                void runSearch("");
              }}
              title={t("header.clearSearch", "清除搜索")}
              aria-label={t("header.clearSearch", "清除搜索")}
              className="flex h-4 w-4 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:text-foreground"
            >
              <XIcon className="h-3 w-3" aria-hidden="true" />
            </button>
          ) : null}
        </div>

        <div className="flex items-center justify-between gap-2 px-0.5">
          <span className="truncate text-[11px] text-muted-foreground/60">
            {t("wiki.catalogCount", "{{count}} 页", { count: visible.length })}
          </span>
          {searchHitIds ? null : (
            <div className="flex shrink-0 items-center gap-0.5">
              {SORT_OPTIONS.map((option) => (
                <button
                  key={option.id}
                  type="button"
                  onClick={() => setCatalogSort(option.id)}
                  aria-pressed={catalogSort === option.id}
                  className={`rounded px-1.5 py-0.5 text-[11px] transition-colors ${
                    catalogSort === option.id
                      ? "bg-primary/10 text-primary"
                      : "text-muted-foreground/60 hover:text-foreground"
                  }`}
                >
                  {t(option.key, option.fallback)}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        {searchError ? (
          // 搜索失败此前只是清空命中集合，画出来和「没有匹配的页面」一模一样
          <LoadErrorState
            message={searchError}
            onRetry={() => void runSearch(searchQuery)}
          />
        ) : visible.length === 0 ? (
          <p className="px-2 py-6 text-center text-xs text-muted-foreground/70">
            {catalog.length === 0
              ? t("wiki.catalogEmpty", "还没有 Wiki 页面")
              : t("wiki.filterEmpty", "没有匹配的页面")}
          </p>
        ) : (
          <div className="space-y-0.5">
            {visible.map((entry) => {
              const backlinks = backlinkCounts[entry.id] ?? 0;
              return (
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
                    {entry.manualEditedAt ? (
                      <PencilIcon
                        className="h-3 w-3 shrink-0 text-primary/70"
                        aria-label={t("wiki.manualEdited", "手动编辑")}
                      />
                    ) : null}
                    <WikiKindBadge kind={entry.kind} />
                  </div>
                  {entry.summary ? (
                    <p className="mt-0.5 line-clamp-2 text-xs leading-snug text-muted-foreground/80">
                      {entry.summary}
                    </p>
                  ) : null}
                  <p className="mt-1 flex items-center gap-1 text-[10px] text-muted-foreground/60">
                    <Link2Icon className="h-2.5 w-2.5" aria-hidden="true" />
                    {backlinks > 0
                      ? t("wiki.backlinkCount", "被 {{count}} 页引用", {
                          count: backlinks,
                        })
                      : t("wiki.orphanHint", "暂无页面引用")}
                  </p>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
