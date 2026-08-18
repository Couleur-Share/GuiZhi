import { useEffect, useState } from "react";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  Loader2Icon,
  MessageCircleIcon,
  RefreshCwIcon,
} from "lucide-react";
import type { KnowledgeItem, SourceComment } from "@guizhi/shared/types";
import { detectPlatformCapturePlatform } from "@guizhi/shared/utils/platform-capture";
import { useTranslation } from "react-i18next";
import { Select } from "../ui/Select";

export function SourceCommentsCard({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const platform = item.sourceUri
    ? detectPlatformCapturePlatform(item.sourceUri)
    : null;
  const sourceCommentsPlatform =
    platform === "xiaohongshu" || platform === "douyin" ? platform : null;
  const [open, setOpen] = useState(false);
  const [comments, setComments] = useState<SourceComment[]>([]);
  const [limit, setLimit] = useState<10 | 20 | 50>(20);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setOpen(false);
    setComments([]);
    setError(null);
  }, [item.id]);

  useEffect(() => {
    if (!open || !sourceCommentsPlatform) return;
    void window.api.platformCapture
      .listComments(item.id)
      .then(setComments)
      .catch((cause) =>
        setError(cause instanceof Error ? cause.message : String(cause)),
      );
  }, [item.id, open, sourceCommentsPlatform]);

  // LINUX DO 的楼层已经完整落在条目的「讨论」小节里；再存一份来源评论
  // 会形成两个内容相同但刷新状态不同的入口。
  if (!sourceCommentsPlatform) return null;

  const refresh = async () => {
    setLoading(true);
    setError(null);
    try {
      setComments(
        await window.api.platformCapture.refreshComments({
          itemId: item.id,
          limit,
        }),
      );
      setOpen(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      // 首次补采时卡片通常仍是折叠态；失败信息如果不展开，用户只会看到
      // loading 一闪而过，像是按钮没有响应。
      setOpen(true);
    } finally {
      setLoading(false);
    }
  };

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
        <Select
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
          disabled={loading}
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
            : t("library.captureComments", "补采")}
        </button>
      </div>

      {open ? (
        <div className="max-h-64 overflow-y-auto border-t border-border/60 px-3 py-2">
          {error ? (
            <p className="mb-2 break-words text-xs text-destructive">{error}</p>
          ) : null}
          {comments.length === 0 && !loading ? (
            <p className="py-4 text-center text-xs text-muted-foreground">
              {t("library.noSourceComments", "还没有来源评论")}
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
