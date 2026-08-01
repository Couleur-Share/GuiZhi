import { useMemo, useState } from "react";
import { ChevronLeftIcon, ChevronRightIcon, SearchIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ForumReplyEntry } from "@guizhi/shared/utils/forum-note";

const CATALOG_AUTO_OPEN_MIN = 20;
const SNIPPET_MAX = 36;

function floorSnippet(reply: ForumReplyEntry): string {
  const raw =
    reply.content.replace(/\s+/g, " ").trim() ||
    reply.replyTo?.snippet.replace(/\s+/g, " ").trim() ||
    "";
  if (raw.length <= SNIPPET_MAX) {
    return raw;
  }
  return `${raw.slice(0, SNIPPET_MAX)}…`;
}

/**
 * 讨论区左侧楼层目录：跳楼、本地过滤、可折叠。
 */
export function ForumFloorCatalog({
  replies,
  open,
  onOpenChange,
  activeFloor,
  onSelectFloor,
}: {
  replies: ForumReplyEntry[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  activeFloor?: number | null;
  onSelectFloor: (floor: number) => void;
}) {
  const { t } = useTranslation();
  const [filter, setFilter] = useState("");
  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) {
      return replies;
    }
    return replies.filter((reply) => {
      const hay = [
        String(reply.floor),
        `${reply.floor} 楼`,
        reply.author,
        reply.content,
        reply.replyTo?.author ?? "",
        reply.replyTo?.snippet ?? "",
      ]
        .join("\n")
        .toLowerCase();
      return hay.includes(needle);
    });
  }, [filter, replies]);

  if (!open) {
    return (
      <div className="flex w-8 shrink-0 flex-col items-center border-r border-border/60 py-2">
        <button
          type="button"
          onClick={() => onOpenChange(true)}
          aria-label={t("library.forumCatalogExpand", "展开楼层目录")}
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
        >
          <ChevronRightIcon className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    );
  }

  return (
    <div className="flex w-[11.5rem] shrink-0 flex-col border-r border-border/60">
      <div className="flex h-8 shrink-0 items-center gap-1 border-b border-border/50 px-1.5">
        <span className="min-w-0 flex-1 truncate text-[11px] font-medium text-muted-foreground">
          {t("library.forumCatalogTitle", "楼层")}
          <span className="ml-1 tabular-nums opacity-70">{replies.length}</span>
        </span>
        <button
          type="button"
          onClick={() => onOpenChange(false)}
          aria-label={t("library.forumCatalogCollapse", "收起楼层目录")}
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
        >
          <ChevronLeftIcon className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
      <div className="shrink-0 border-b border-border/40 px-1.5 py-1.5">
        <div className="flex h-6 items-center gap-1 rounded-md border border-border/60 bg-background/50 px-1.5">
          <SearchIcon
            className="h-3 w-3 shrink-0 text-muted-foreground/60"
            aria-hidden="true"
          />
          <input
            type="search"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={t("library.forumCatalogFilter", "筛选…")}
            className="min-w-0 flex-1 bg-transparent text-[11px] outline-none placeholder:text-muted-foreground/50"
          />
        </div>
      </div>
      <ul className="min-h-0 flex-1 overflow-y-auto py-1">
        {visible.map((reply) => {
          const active = activeFloor === reply.floor;
          const snippet = floorSnippet(reply);
          return (
            <li key={`${reply.floor}-${reply.author}`}>
              <button
                type="button"
                onClick={() => onSelectFloor(reply.floor)}
                className={`flex w-full flex-col gap-0.5 px-2 py-1.5 text-left transition-colors ${
                  active
                    ? "bg-primary/15 text-foreground"
                    : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                }`}
              >
                <span className="truncate text-[11px] font-medium">
                  {reply.floor} 楼 · {reply.author || "匿名"}
                </span>
                {snippet ? (
                  <span className="line-clamp-2 text-[10px] leading-snug opacity-70">
                    {snippet}
                  </span>
                ) : null}
              </button>
            </li>
          );
        })}
        {visible.length === 0 ? (
          <li className="px-2 py-3 text-center text-[11px] text-muted-foreground/70">
            {t("library.forumCatalogEmpty", "无匹配楼层")}
          </li>
        ) : null}
      </ul>
    </div>
  );
}

export function defaultCatalogOpen(replyCount: number): boolean {
  return replyCount >= CATALOG_AUTO_OPEN_MIN;
}
