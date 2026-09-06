import {
  ChevronDownIcon,
  ChevronRightIcon,
  Loader2Icon,
  MessageCircleIcon,
  RefreshCwIcon,
} from "lucide-react";
import { useSourceComments } from "./SourceCommentsContext";
import { LoadErrorState } from "../ui/LoadErrorState";
import { useTranslation } from "react-i18next";
import { Select } from "../ui/Select";

export function SourceCommentsCard() {
  const { t } = useTranslation();
  const state = useSourceComments();
  if (!state?.supported) return null;
  const {
    comments,
    open,
    setOpen,
    limit,
    setLimit,
    loading,
    reading,
    error,
    loadError,
    attempted,
    load,
    refresh,
  } = state;
  if (!open && !comments.length && !loadError) return null;
  return (
    <div className="w-full overflow-hidden rounded-xl border border-border bg-background/55">
      <div className="flex min-h-9 items-center gap-2 px-3 py-2">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className="inline-flex min-w-0 flex-1 items-center gap-2 text-left text-xs font-medium text-foreground"
        >
          {open ? (
            <ChevronDownIcon className="h-3.5 w-3.5" />
          ) : (
            <ChevronRightIcon className="h-3.5 w-3.5" />
          )}
          <MessageCircleIcon className="h-3.5 w-3.5 text-muted-foreground" />
          {t("library.sourceComments", "来源评论")}
          {comments.length > 0 ? (
            <span className="text-muted-foreground">{comments.length}</span>
          ) : null}
        </button>
      </div>

      {loadError ? (
        <LoadErrorState message={loadError} onRetry={() => void load()} />
      ) : null}
      {open && !loadError ? (
        <div className="max-h-64 overflow-y-auto border-t border-border/60 px-3 py-2">
          <div className="mb-2 flex items-center justify-end gap-2">
            <Select
              ariaLabel={t("library.commentCaptureLimit", "评论采集数量")}
              value={String(limit)}
              onChange={(value) => setLimit(Number(value) as 10 | 20 | 50)}
              options={[
                { value: "10", label: "10" },
                { value: "20", label: "20" },
                { value: "50", label: "50" },
              ]}
              className="w-20 shrink-0"
              menuMinWidth={80}
              triggerClassName="flex h-7 w-full cursor-pointer items-center justify-between gap-1 rounded-md bg-muted px-2 text-xs text-foreground transition-colors hover:bg-muted/80 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
            />
            <button
              type="button"
              disabled={loading || reading || !!loadError}
              onClick={() => void refresh()}
              className="inline-flex h-7 items-center gap-1 rounded-md border border-border px-2 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground disabled:opacity-50"
            >
              {loading ? (
                <Loader2Icon className="h-3 w-3 animate-spin" />
              ) : (
                <RefreshCwIcon className="h-3 w-3" />
              )}
              {comments.length > 0
                ? t("library.refreshComments", "刷新")
                : t("library.collectSourceComments", "采集评论")}
            </button>
          </div>
          {error ? (
            <p className="mb-2 break-words text-xs text-destructive">{error}</p>
          ) : null}
          {reading || loading ? (
            <p role="status" className="py-2 text-xs text-muted-foreground">
              {t("common.loading", "加载中...")}
            </p>
          ) : null}
          {comments.length === 0 && !loading && !reading && !error ? (
            <p className="py-4 text-center text-xs text-muted-foreground">
              {attempted
                ? t("library.commentsLoadedEmpty", "未取得评论，可稍后重试")
                : t("library.noSourceComments", "还没有来源评论")}
            </p>
          ) : (
            <div className="divide-y divide-border/60">
              {comments.map((comment) => (
                <article key={comment.id} className="py-2.5">
                  <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                    <span className="font-medium text-foreground/80">
                      {comment.authorName ||
                        t("library.unknownAuthor", "未知用户")}
                    </span>
                    <span>
                      {t("library.commentLikes", "{{count}} 赞", {
                        count: comment.likeCount,
                      })}
                    </span>
                    {comment.publishedAt ? (
                      <time>
                        {new Date(comment.publishedAt).toLocaleDateString()}
                      </time>
                    ) : null}
                  </div>
                  <p className="mt-1 whitespace-pre-wrap break-words text-xs leading-relaxed text-foreground/85">
                    {comment.content}
                  </p>
                </article>
              ))}
            </div>
          )}
        </div>
      ) : null}
    </div>
  );
}
