import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  forwardRef,
  useImperativeHandle,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import {
  filterForumReplies,
  parseForumReplySection,
  type ForumReplyEntry,
} from "@guizhi/shared/utils/forum-note";
import { highlightText } from "./highlight-text";
import { MarkdownBody } from "./MarkdownPreview";
import { scrollElementIntoContainer } from "./scroll-into-container";

export interface ForumDiscussionHandle {
  /** 滚到指定楼；若被搜索过滤掉会先需要调用方清 query */
  scrollToFloor: (floor: number) => boolean;
  getReplies: () => ForumReplyEntry[];
  getScrollElement: () => HTMLElement | null;
}

/**
 * 论坛「讨论」标签：楼层卡片列表。
 * 搜索状态由工具栏 PanelFindBar 持有，这里负责过滤、高亮与滚入视野。
 */
export const ForumDiscussionView = forwardRef<
  ForumDiscussionHandle,
  {
    content: string;
    query: string;
    activeIndex: number;
    onMatchCountChange: (count: number) => void;
    /** 非激活标签时仍挂载保滚动，但不要跟工具栏上下跳转 */
    findNavEnabled?: boolean;
    catalog?: ReactNode;
    onReplyToClick?: (replyTo: NonNullable<ForumReplyEntry["replyTo"]>) => void;
  }
>(function ForumDiscussionView(
  {
    content,
    query,
    activeIndex,
    onMatchCountChange,
    findNavEnabled = true,
    catalog,
    onReplyToClick,
  },
  ref,
) {
  const { t } = useTranslation();
  const listRef = useRef<HTMLDivElement>(null);
  const cardRefs = useRef<Map<number, HTMLLIElement>>(new Map());
  const replies = useMemo(() => parseForumReplySection(content), [content]);
  const visible = useMemo(
    () => filterForumReplies(replies, query),
    [replies, query],
  );
  const needle = query.trim();
  const visibleKey = visible.map((reply) => reply.floor).join(",");

  useImperativeHandle(
    ref,
    () => ({
      scrollToFloor: (floor: number) => {
        const card = cardRefs.current.get(floor);
        const container = listRef.current;
        if (!card || !container) {
          return false;
        }
        const mark = card.querySelector("mark");
        scrollElementIntoContainer(
          container,
          mark instanceof HTMLElement ? mark : card,
        );
        return true;
      },
      getReplies: () => replies,
      getScrollElement: () => listRef.current,
    }),
    [replies],
  );

  useEffect(() => {
    if (!findNavEnabled) {
      return;
    }
    onMatchCountChange(needle ? visible.length : 0);
  }, [findNavEnabled, needle, visible.length, onMatchCountChange]);

  useLayoutEffect(() => {
    if (!findNavEnabled || !needle || visible.length === 0) {
      return;
    }
    const container = listRef.current;
    const index =
      ((activeIndex % visible.length) + visible.length) % visible.length;
    const reply = visible[index];
    const card = reply ? cardRefs.current.get(reply.floor) : null;
    if (!container || !card) {
      return;
    }
    const target =
      (card.querySelector("mark") as HTMLElement | null) ?? card;
    scrollElementIntoContainer(container, target);
  }, [activeIndex, findNavEnabled, needle, visible, visibleKey]);

  if (replies.length === 0) {
    return (
      <div className="flex h-full items-center justify-center px-6 text-sm text-muted-foreground">
        {t("library.forumRepliesEmpty", "暂无讨论内容")}
      </div>
    );
  }

  const activeCard =
    findNavEnabled && needle && visible.length > 0
      ? ((activeIndex % visible.length) + visible.length) % visible.length
      : -1;

  const listBody =
    needle && visible.length === 0 ? (
      <div className="flex h-full items-center justify-center px-6 text-sm text-muted-foreground">
        {t("library.forumRepliesFilterEmpty", "没有匹配的楼层")}
      </div>
    ) : (
      <div ref={listRef} className="h-full overflow-y-auto px-4 py-4">
        <ul className="mx-auto flex max-w-3xl flex-col gap-3">
          {visible.map((reply, index) => (
            <ReplyCard
              key={`${reply.floor}-${reply.author}`}
              reply={reply}
              query={needle}
              active={index === activeCard}
              onReplyToClick={onReplyToClick}
              cardRef={(node) => {
                if (node) {
                  cardRefs.current.set(reply.floor, node);
                } else {
                  cardRefs.current.delete(reply.floor);
                }
              }}
            />
          ))}
        </ul>
      </div>
    );

  if (!catalog) {
    return listBody;
  }

  return (
    <div className="flex h-full min-h-0">
      {catalog}
      <div className="min-h-0 min-w-0 flex-1">{listBody}</div>
    </div>
  );
});

function ReplyCard({
  reply,
  query,
  active,
  cardRef,
  onReplyToClick,
}: {
  reply: ForumReplyEntry;
  query: string;
  active: boolean;
  cardRef: (node: HTMLLIElement | null) => void;
  onReplyToClick?: (replyTo: NonNullable<ForumReplyEntry["replyTo"]>) => void;
}) {
  return (
    <li
      ref={cardRef}
      data-floor={reply.floor}
      className={
        active
          ? "rounded-xl border border-border/70 bg-card/40 px-4 py-3 ring-1 ring-foreground/25"
          : "rounded-xl border border-border/70 bg-card/40 px-4 py-3"
      }
    >
      <div className="mb-2 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-[12px]">
        <span className="font-semibold text-foreground">
          {highlightText(`${reply.floor} 楼`, query)}
        </span>
        <span className="text-muted-foreground">
          {highlightText(reply.author || "匿名", query)}
        </span>
      </div>
      {reply.replyTo ? (
        <div className="mb-3 rounded-lg border border-border/50 bg-muted/30 px-3 py-2 text-[12px] leading-relaxed text-muted-foreground">
          <div className="mb-1 font-medium text-foreground/80">
            {onReplyToClick ? (
              <button
                type="button"
                onClick={() => onReplyToClick(reply.replyTo!)}
                className="text-left text-primary underline-offset-2 hover:underline"
              >
                回复 @
                {highlightText(reply.replyTo.author, query)}
                {reply.replyTo.floor != null
                  ? highlightText(`（${reply.replyTo.floor} 楼）`, query)
                  : ""}
              </button>
            ) : (
              <>
                回复 @
                {highlightText(reply.replyTo.author, query)}
                {reply.replyTo.floor != null
                  ? highlightText(`（${reply.replyTo.floor} 楼）`, query)
                  : ""}
              </>
            )}
          </div>
          {reply.replyTo.snippet ? (
            <p className="line-clamp-4 whitespace-pre-wrap">
              {highlightText(reply.replyTo.snippet, query)}
            </p>
          ) : null}
        </div>
      ) : null}
      {reply.content ? (
        <MarkdownBody content={reply.content} highlightQuery={query || undefined} />
      ) : null}
    </li>
  );
}
