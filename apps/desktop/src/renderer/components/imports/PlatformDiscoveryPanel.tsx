import { useEffect, useMemo, useState } from "react";
import {
  ArrowLeftIcon,
  CheckIcon,
  Loader2Icon,
  LogInIcon,
  SearchIcon,
  SquareArrowOutUpRightIcon,
} from "lucide-react";
import type {
  CommentLimit,
  PlatformCapturePlatform,
  PlatformDiscoveryItem,
  PlatformSessionStatus,
} from "@guizhi/shared/types";
import {
  detectPlatformCapturePlatform,
  detectPlatformCreatorUrl,
} from "@guizhi/shared/utils/platform-capture";
import { useTranslation } from "react-i18next";
import { useCollectionStore } from "../../stores/collection.store";
import { useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { Select } from "../ui/Select";
import { useToast } from "../ui/Toast";
import {
  DISCOVERY_DRAFT_KEY,
  DISCOVERY_OPEN_VIEW_KEY,
} from "./platform-discovery-draft";
import { SavedDiscoveryViewsPanel } from "./SavedDiscoveryViewsPanel";

interface DiscoveryDraft {
  url?: string;
  collectionId?: string;
  tagNames?: string[];
}

function readDraft(): DiscoveryDraft {
  try {
    const raw = sessionStorage.getItem(DISCOVERY_DRAFT_KEY);
    sessionStorage.removeItem(DISCOVERY_DRAFT_KEY);
    return raw ? (JSON.parse(raw) as DiscoveryDraft) : {};
  } catch {
    return {};
  }
}

const PLATFORM_NAMES: Record<PlatformCapturePlatform, string> = {
  xiaohongshu: "小红书",
  douyin: "抖音",
  linuxdo: "LINUX DO",
};

export function updateDiscoverySelection(
  current: string[],
  item: Pick<PlatformDiscoveryItem, "externalId" | "importedItemId">,
): { ids: string[]; reachedLimit: boolean } {
  if (item.importedItemId) return { ids: current, reachedLimit: false };
  if (current.includes(item.externalId)) {
    return {
      ids: current.filter((id) => id !== item.externalId),
      reachedLimit: false,
    };
  }
  if (current.length >= 50) return { ids: current, reachedLimit: true };
  return { ids: [...current, item.externalId], reachedLimit: false };
}

export function PlatformDiscoveryPanel({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const initial = useMemo(readDraft, []);
  const initialSavedViewId = useMemo(() => {
    const id = sessionStorage.getItem(DISCOVERY_OPEN_VIEW_KEY);
    sessionStorage.removeItem(DISCOVERY_OPEN_VIEW_KEY);
    return id;
  }, []);
  const initialPlatform = initial.url
    ? (detectPlatformCapturePlatform(initial.url) ?? "xiaohongshu")
    : "xiaohongshu";
  const [mode, setMode] = useState<"creator" | "search">(
    initial.url ? "creator" : "search",
  );
  const [platform, setPlatform] =
    useState<PlatformCapturePlatform>(initialPlatform);
  const [query, setQuery] = useState(initial.url ?? "");
  const [items, setItems] = useState<PlatformDiscoveryItem[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [loggingIn, setLoggingIn] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [statuses, setStatuses] = useState<PlatformSessionStatus[]>([]);
  const [collectionId, setCollectionId] = useState(initial.collectionId ?? "");
  const [tagDraft, setTagDraft] = useState((initial.tagNames ?? []).join(", "));
  const [commentLimit, setCommentLimit] = useState<CommentLimit>(0);
  const collections = useCollectionStore((state) => state.collections);
  const fetchCollections = useCollectionStore(
    (state) => state.fetchCollections,
  );
  const enqueue = useImportStore((state) => state.enqueue);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const setAppModule = useUIStore((state) => state.setAppModule);

  const refreshStatuses = async () => {
    setStatuses(await window.api.platformCapture.getStatuses());
  };

  useEffect(() => {
    void fetchCollections();
    void refreshStatuses().catch(() => undefined);
  }, [fetchCollections]);

  const detectedCreator =
    mode === "creator" ? detectPlatformCreatorUrl(query.trim()) : null;
  const effectivePlatform = detectedCreator?.platform ?? platform;
  const status = statuses.find((entry) => entry.platform === effectivePlatform);

  const discover = async (append = false) => {
    const trimmed = query.trim();
    setError(null);
    const creator =
      mode === "creator" ? detectPlatformCreatorUrl(trimmed) : null;
    const operationPlatform = creator?.platform ?? platform;
    const operationStatus = statuses.find(
      (entry) => entry.platform === operationPlatform,
    );
    if (!operationStatus?.available) {
      setError(t("imports.browserUnavailable", "归知内置登录窗口暂不可用"));
      return;
    }
    if (!operationStatus.loggedIn) {
      setError(t("imports.loginRequired", "请先登录平台账号"));
      return;
    }
    if (mode === "creator") {
      if (!creator) {
        setError(
          t("imports.creatorUrlInvalid", "请输入单个小红书或抖音作者主页链接"),
        );
        return;
      }
      setPlatform(creator.platform);
    } else if (!trimmed) {
      setError(t("imports.searchKeywordRequired", "请输入搜索关键词"));
      return;
    }
    setLoading(true);
    try {
      const nextCursor = append ? cursor : null;
      const page =
        mode === "creator"
          ? await window.api.platformCapture.discoverCreator({
              platform: detectPlatformCreatorUrl(trimmed)!.platform,
              url: trimmed,
              cursor: nextCursor,
              limit: 20,
            })
          : await window.api.platformCapture.search({
              platform,
              keyword: trimmed,
              cursor: nextCursor,
              limit: 20,
            });
      if (append && page.cursor && page.cursor === cursor) {
        setHasMore(false);
        throw new Error(
          t(
            "imports.discoveryCursorRepeated",
            "平台返回了重复游标，已停止加载",
          ),
        );
      }
      setItems((current) =>
        append
          ? [
              ...current,
              ...page.items.filter(
                (item) =>
                  !current.some(
                    (entry) => entry.externalId === item.externalId,
                  ),
              ),
            ].slice(0, 100)
          : page.items,
      );
      if (!append) setSelected([]);
      setCursor(page.cursor);
      setHasMore(page.hasMore);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
      await refreshStatuses().catch(() => undefined);
    }
  };

  const login = async () => {
    setLoggingIn(true);
    setError(null);
    try {
      await window.api.platformCapture.login(effectivePlatform);
      await refreshStatuses();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoggingIn(false);
    }
  };

  const toggle = (item: PlatformDiscoveryItem) => {
    setSelected((current) => {
      const next = updateDiscoverySelection(current, item);
      if (next.reachedLimit) {
        showToast(
          t("imports.discoverySelectionLimit", "单次最多选择 50 条"),
          "warning",
        );
      }
      return next.ids;
    });
  };

  const importSelected = async () => {
    const chosen = items.filter((item) => selected.includes(item.externalId));
    if (chosen.length === 0) return;
    const tagNames = tagDraft
      .split(/[,，]/)
      .map((value) => value.trim())
      .filter(Boolean);
    try {
      await enqueue(
        chosen.map((item) => ({
          kind: "url" as const,
          input: item.url,
          collectionId: collectionId || null,
          tagNames: tagNames.length ? tagNames : undefined,
          captureStrategy: "authenticated" as const,
          commentLimit: effectivePlatform === "linuxdo" ? 0 : commentLimit,
        })),
      );
      showToast(
        t("imports.discoveryEnqueued", "已加入 {{count}} 个认证采集任务", {
          count: chosen.length,
        }),
        "success",
      );
      onBack();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const openExisting = async (itemId: string) => {
    setAppModule("library");
    setScope("all");
    await selectItem(itemId);
  };

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <button
          type="button"
          onClick={onBack}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-xs hover:bg-muted/60"
        >
          <ArrowLeftIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("imports.backToTasks", "返回任务")}
        </button>
        <h2 className="text-sm font-semibold">
          {t("imports.platformDiscovery", "平台发现")}
        </h2>
        <span className="text-xs text-muted-foreground">
          {t("imports.discoveryPrivacy", "临时查询，不保存搜索历史或浏览轨迹")}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        <div className="mx-auto max-w-5xl space-y-4">
          <div className="rounded-xl border border-border bg-background/55 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <div className="inline-flex rounded-lg border border-border p-0.5">
                {(["creator", "search"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    onClick={() => {
                      setMode(value);
                      setItems([]);
                      setSelected([]);
                      setError(null);
                    }}
                    className={`h-8 rounded-md px-3 text-xs ${mode === value ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted/60"}`}
                  >
                    {value === "creator"
                      ? t("imports.creatorDiscovery", "作者主页")
                      : t("imports.keywordSearch", "关键词搜索")}
                  </button>
                ))}
              </div>
              {mode === "search" ? (
                <div className="inline-flex rounded-lg border border-border p-0.5">
                  {(["xiaohongshu", "douyin"] as const).map((value) => (
                    <button
                      key={value}
                      type="button"
                      onClick={() => {
                        setPlatform(value);
                        setItems([]);
                        setSelected([]);
                      }}
                      className={`h-8 rounded-md px-3 text-xs ${platform === value ? "bg-accent text-foreground" : "text-muted-foreground"}`}
                    >
                      {PLATFORM_NAMES[value]}
                    </button>
                  ))}
                </div>
              ) : null}
              <div className="relative min-w-64 flex-1">
                <SearchIcon
                  className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
                  aria-hidden="true"
                />
                <input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") void discover(false);
                  }}
                  placeholder={
                    mode === "creator"
                      ? t("imports.creatorUrlPlaceholder", "粘贴作者主页链接")
                      : t("imports.keywordPlaceholder", "输入平台关键词")
                  }
                  className="h-9 w-full rounded-lg border border-border bg-background pl-9 pr-3 text-sm outline-none focus:border-primary/50"
                />
              </div>
              {!status?.loggedIn ? (
                <button
                  type="button"
                  disabled={!loggingIn && !status?.available}
                  onClick={() =>
                    loggingIn
                      ? void window.api.platformCapture.cancelLogin(
                          effectivePlatform,
                        )
                      : void login()
                  }
                  className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm disabled:opacity-50"
                >
                  {loggingIn ? (
                    <Loader2Icon
                      className="h-4 w-4 animate-spin"
                      aria-hidden="true"
                    />
                  ) : (
                    <LogInIcon className="h-4 w-4" aria-hidden="true" />
                  )}
                  {loggingIn
                    ? t("imports.cancelLogin", "取消登录")
                    : t("settings.platformLogin", "登录")}
                </button>
              ) : null}
              <button
                type="button"
                disabled={loading}
                onClick={() => void discover(false)}
                className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-50"
              >
                {loading ? (
                  <Loader2Icon
                    className="h-4 w-4 animate-spin"
                    aria-hidden="true"
                  />
                ) : (
                  <SearchIcon className="h-4 w-4" aria-hidden="true" />
                )}
                {t("imports.discover", "发现")}
              </button>
              {loading ? (
                <button
                  type="button"
                  onClick={() =>
                    void window.api.platformCapture.cancelDiscovery()
                  }
                  className="h-9 rounded-lg border border-border px-3 text-xs"
                >
                  {t("common.cancel", "取消")}
                </button>
              ) : null}
            </div>
            {error ? (
              <p className="mt-2 break-words text-xs text-destructive">
                {error}
              </p>
            ) : null}
          </div>

          <SavedDiscoveryViewsPanel
            platform={effectivePlatform}
            mode={mode}
            query={query}
            initialViewId={initialSavedViewId}
          />

          {items.length > 0 ? (
            <>
              <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border bg-background/55 px-4 py-3">
                <span className="text-sm font-medium">
                  {t("imports.discoverySelectedCount", "已选 {{count}} / 50", {
                    count: selected.length,
                  })}
                </span>
                <span className="min-w-0 flex-1" />
                <input
                  value={tagDraft}
                  onChange={(event) => setTagDraft(event.target.value)}
                  placeholder={t(
                    "imports.tagsCommaSeparated",
                    "标签（逗号分隔）",
                  )}
                  className="h-8 w-44 rounded-lg border border-border bg-background px-2.5 text-xs outline-none"
                />
                <Select
                  value={collectionId}
                  onChange={setCollectionId}
                  options={[
                    { value: "", label: t("library.noCollection", "未分类") },
                    ...collections.map((entry) => ({
                      value: entry.id,
                      label: entry.name,
                    })),
                  ]}
                  className="w-36"
                />
                {effectivePlatform !== "linuxdo" ? (
                  <Select
                    value={String(commentLimit)}
                    onChange={(value) =>
                      setCommentLimit(Number(value) as CommentLimit)
                    }
                    options={[
                      {
                        value: "0",
                        label: t("imports.commentsOff", "不采评论"),
                      },
                      {
                        value: "10",
                        label: t("imports.comments10", "热门评论 10"),
                      },
                      {
                        value: "20",
                        label: t("imports.comments20", "热门评论 20"),
                      },
                      {
                        value: "50",
                        label: t("imports.comments50", "热门评论 50"),
                      },
                    ]}
                    className="w-36"
                  />
                ) : null}
                <button
                  type="button"
                  disabled={selected.length === 0}
                  onClick={() => void importSelected()}
                  className="h-8 rounded-lg bg-primary px-4 text-xs font-medium text-primary-foreground disabled:opacity-50"
                >
                  {t("imports.importSelected", "导入所选")}
                </button>
              </div>

              <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
                {items.map((item) => {
                  const checked = selected.includes(item.externalId);
                  return (
                    <div
                      key={`${item.platform}:${item.externalId}`}
                      role={item.importedItemId ? undefined : "button"}
                      tabIndex={item.importedItemId ? undefined : 0}
                      onClick={() => {
                        if (!item.importedItemId) toggle(item);
                      }}
                      onKeyDown={(event) => {
                        if (
                          !item.importedItemId &&
                          (event.key === "Enter" || event.key === " ")
                        ) {
                          event.preventDefault();
                          toggle(item);
                        }
                      }}
                      className={`overflow-hidden rounded-xl border text-left transition-colors ${item.importedItemId ? "border-border opacity-65" : checked ? "cursor-pointer border-primary bg-primary/5" : "cursor-pointer border-border bg-background/60 hover:bg-accent/30"}`}
                    >
                      <div className="flex min-h-28 gap-3 p-3">
                        {item.coverUrl ? (
                          <img
                            src={item.coverUrl}
                            alt=""
                            className="h-24 w-20 shrink-0 rounded-lg bg-muted object-cover"
                            referrerPolicy="no-referrer"
                          />
                        ) : (
                          <div className="flex h-24 w-20 shrink-0 items-center justify-center rounded-lg bg-muted text-xs text-muted-foreground">
                            {item.mediaType}
                          </div>
                        )}
                        <div className="min-w-0 flex-1">
                          <div className="flex items-start gap-2">
                            <p className="line-clamp-2 flex-1 text-sm font-medium">
                              {item.title}
                            </p>
                            <span
                              className={`flex h-5 w-5 shrink-0 items-center justify-center rounded border ${checked ? "border-primary bg-primary text-primary-foreground" : "border-border"}`}
                            >
                              {checked ? (
                                <CheckIcon className="h-3.5 w-3.5" />
                              ) : null}
                            </span>
                          </div>
                          <p className="mt-2 truncate text-xs text-muted-foreground">
                            {item.author || PLATFORM_NAMES[item.platform]} ·{" "}
                            {item.mediaType}
                          </p>
                          {item.publishedAt ? (
                            <p className="mt-1 text-xs text-muted-foreground">
                              {new Date(item.publishedAt).toLocaleDateString()}
                            </p>
                          ) : null}
                          {item.importedItemId ? (
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation();
                                void openExisting(item.importedItemId!);
                              }}
                              className="mt-2 inline-flex items-center gap-1 text-xs text-primary hover:underline"
                            >
                              {t("imports.alreadyImported", "已导入，打开条目")}
                              <SquareArrowOutUpRightIcon className="h-3 w-3" />
                            </button>
                          ) : null}
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>

              {hasMore && items.length < 100 ? (
                <div className="flex justify-center">
                  <button
                    type="button"
                    disabled={loading}
                    onClick={() => void discover(true)}
                    className="h-9 rounded-lg border border-border px-4 text-sm hover:bg-muted/60 disabled:opacity-50"
                  >
                    {loading
                      ? t("common.loading", "加载中")
                      : t("imports.loadMore", "加载更多")}
                  </button>
                </div>
              ) : null}
            </>
          ) : !loading ? (
            <div className="flex h-48 flex-col items-center justify-center rounded-xl border border-dashed border-border text-center text-sm text-muted-foreground">
              <SearchIcon
                className="mb-3 h-7 w-7 opacity-50"
                aria-hidden="true"
              />
              {t(
                "imports.discoveryEmpty",
                "登录后发现作者作品或搜索平台内容；结果默认不勾选",
              )}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
