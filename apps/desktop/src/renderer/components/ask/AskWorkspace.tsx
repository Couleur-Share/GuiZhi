import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowDownIcon, SendIcon, SquareIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAskStore } from "../../stores/ask.store";
import { Spinner } from "../ui/Spinner";
import { AskEmptyState } from "./AskEmptyState";
import { AskMessageCard } from "./AskMessageCard";

/** 距底部多少像素以内算「跟在最新消息上」 */
const STICK_TO_BOTTOM_THRESHOLD_PX = 80;

/**
 * AI 问答页：基于知识库的多轮问答，
 * Agent 检索过程实时展示，回答附可跳转的引用来源。
 */
export function AskWorkspace() {
  const { t } = useTranslation();
  const messages = useAskStore((state) => state.messages);
  const hasLoaded = useAskStore((state) => state.hasLoaded);
  const isRunning = useAskStore((state) => state.isRunning);
  const ask = useAskStore((state) => state.ask);
  const stop = useAskStore((state) => state.stop);
  const initialize = useAskStore((state) => state.initialize);

  const [draft, setDraft] = useState("");
  const [stuckToBottom, setStuckToBottom] = useState(true);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);

  // 恢复上次会话（会话列表与消息持久化在主进程 SQLite）
  useEffect(() => {
    void initialize();
  }, [initialize]);

  const scrollToBottom = useCallback(() => {
    const container = scrollRef.current;
    if (container) {
      container.scrollTop = container.scrollHeight;
    }
  }, []);

  /**
   * 只有用户本来就跟在底部时才自动滚动。
   *
   * 原先是每次 messages 变化都无条件把 scrollTop 拉到底，而流式回答每来
   * 一个 chunk 就变一次——想往上翻看前一轮的内容根本翻不动，一松手就被拽回去。
   */
  useEffect(() => {
    if (stuckToBottom) {
      scrollToBottom();
    }
  }, [messages, stuckToBottom, scrollToBottom]);

  const handleScroll = () => {
    const container = scrollRef.current;
    if (!container) {
      return;
    }
    const distance =
      container.scrollHeight - container.scrollTop - container.clientHeight;
    setStuckToBottom(distance <= STICK_TO_BOTTOM_THRESHOLD_PX);
  };

  const submit = () => {
    const question = draft.trim();
    if (!question || isRunning) {
      return;
    }
    setDraft("");
    setStuckToBottom(true);
    void ask(question);
  };

  const pickExample = (text: string) => {
    setDraft(text);
    inputRef.current?.focus();
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="shrink-0 text-sm font-semibold text-foreground">
          {t("ask.title", "AI 问答")}
        </h2>
        {messages.length > 0 ? (
          <span className="truncate text-xs text-muted-foreground/70">
            {t("ask.turnCount", "{{count}} 轮问答", { count: messages.length })}
          </span>
        ) : null}
      </div>

      <div className="relative min-h-0 flex-1">
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          className="h-full overflow-y-auto px-5 py-4"
        >
          {!hasLoaded ? (
            // 上次会话读出来之前不画空态：有历史记录的用户否则会先看到一整屏
            // 引导，再被消息列表整块换掉
            <div className="delayed-fade-in flex h-full items-center justify-center">
              <Spinner size="sm" tone="muted" />
            </div>
          ) : messages.length === 0 ? (
            <AskEmptyState onPick={pickExample} />
          ) : (
            <div className="mx-auto max-w-3xl space-y-6">
              {messages.map((message) => (
                <AskMessageCard key={message.id} message={message} />
              ))}
            </div>
          )}
        </div>

        {/* 翻上去看历史时，给一条明确的回底部出口 */}
        {!stuckToBottom && messages.length > 0 ? (
          <button
            type="button"
            onClick={() => {
              setStuckToBottom(true);
              scrollToBottom();
            }}
            title={t("ask.scrollToLatest", "回到最新")}
            aria-label={t("ask.scrollToLatest", "回到最新")}
            className="absolute bottom-3 left-1/2 inline-flex h-8 w-8 -translate-x-1/2 items-center justify-center rounded-full border border-border bg-popover text-muted-foreground shadow-sm transition-colors hover:text-foreground"
          >
            <ArrowDownIcon className="h-4 w-4" aria-hidden="true" />
          </button>
        ) : null}
      </div>

      <div className="shrink-0 border-t border-border px-5 py-3">
        <div className="mx-auto flex max-w-3xl items-end gap-2">
          <textarea
            ref={inputRef}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                submit();
              }
            }}
            rows={Math.min(6, Math.max(1, draft.split("\n").length))}
            placeholder={t(
              "ask.placeholder",
              "输入问题，Enter 发送，Shift+Enter 换行",
            )}
            className="min-h-9 flex-1 resize-none rounded-xl border border-border bg-background/60 px-3.5 py-2 text-sm text-foreground placeholder:text-muted-foreground/70 focus:border-primary/50 focus:outline-none"
          />
          {isRunning ? (
            <button
              type="button"
              onClick={stop}
              title={t("ask.stop", "停止")}
              aria-label={t("ask.stop", "停止")}
              className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-border text-foreground transition-colors hover:bg-muted/60"
            >
              <SquareIcon className="h-4 w-4" aria-hidden="true" />
            </button>
          ) : (
            <button
              type="button"
              onClick={submit}
              disabled={!draft.trim()}
              title={t("ask.send", "发送")}
              aria-label={t("ask.send", "发送")}
              className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
            >
              <SendIcon className="h-4 w-4" aria-hidden="true" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
