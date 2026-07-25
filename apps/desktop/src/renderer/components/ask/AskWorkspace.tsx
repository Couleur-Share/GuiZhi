import { useEffect, useRef, useState } from "react";
import {
  BookOpenIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  Loader2Icon,
  MessageSquarePlusIcon,
  MessagesSquareIcon,
  NetworkIcon,
  SendIcon,
  SettingsIcon,
  SquareIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAskStore, type AskMessage } from "../../stores/ask.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { MarkdownBody } from "../library/MarkdownPreview";
import type { QaSourceRef } from "../../services/knowledge-ai/qa";

function AgentSteps({ steps, running }: { steps: string[]; running: boolean }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);

  if (steps.length === 0) {
    return null;
  }

  // 运行中始终展示最新步骤；完成后折叠为可展开的过程记录
  if (running) {
    return (
      <div className="mb-2 space-y-1">
        {steps.map((step, index) => (
          <div
            key={index}
            className={`flex items-center gap-1.5 text-xs ${
              index === steps.length - 1
                ? "text-muted-foreground"
                : "text-muted-foreground/50"
            }`}
          >
            {index === steps.length - 1 ? (
              <Loader2Icon
                className="h-3 w-3 animate-spin"
                aria-hidden="true"
              />
            ) : (
              <span className="inline-block h-3 w-3 text-center leading-3">
                ·
              </span>
            )}
            <span>{step}</span>
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="mb-2">
      <button
        type="button"
        onClick={() => setExpanded((value) => !value)}
        className="flex items-center gap-1 text-xs text-muted-foreground/70 transition-colors hover:text-muted-foreground"
      >
        {expanded ? (
          <ChevronDownIcon className="h-3 w-3" aria-hidden="true" />
        ) : (
          <ChevronRightIcon className="h-3 w-3" aria-hidden="true" />
        )}
        {t("ask.stepsLabel", "检索过程（{{count}} 步）", {
          count: steps.length,
        })}
      </button>
      {expanded ? (
        <div className="mt-1 space-y-0.5 border-l border-border/60 pl-3">
          {steps.map((step, index) => (
            <p key={index} className="text-xs text-muted-foreground/70">
              {step}
            </p>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function SourceList({ sources }: { sources: QaSourceRef[] }) {
  const { t } = useTranslation();
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setAppModule = useUIStore((state) => state.setAppModule);

  if (sources.length === 0) {
    return null;
  }

  const openSource = async (source: QaSourceRef) => {
    if (source.kind === "wiki") {
      setAppModule("wiki");
      const { useWikiStore } = await import("../../stores/wiki.store");
      await useWikiStore.getState().selectPage(source.refId);
    } else {
      setAppModule("library");
      await selectItem(source.refId);
    }
  };

  return (
    <div className="mt-3 border-t border-border/60 pt-2">
      <p className="mb-1.5 text-xs font-medium text-muted-foreground">
        {t("ask.sources", "引用来源")}
      </p>
      <div className="flex flex-wrap gap-1.5">
        {sources.map((source) => (
          <button
            key={source.ordinal}
            type="button"
            onClick={() => void openSource(source)}
            title={source.title}
            className="inline-flex max-w-64 items-center gap-1 rounded-lg border border-border/70 bg-background/60 px-2 py-1 text-xs text-foreground transition-colors hover:border-primary/40 hover:bg-primary/5"
          >
            <span className="shrink-0 font-mono text-[10px] text-primary">
              [{source.ordinal}]
            </span>
            {source.kind === "wiki" ? (
              <NetworkIcon
                className="h-3 w-3 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
            ) : (
              <BookOpenIcon
                className="h-3 w-3 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
            )}
            <span className="min-w-0 truncate">{source.title}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

function MessageError({ message }: { message: AskMessage }) {
  const { t } = useTranslation();
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );

  if (message.errorKind === "not-configured") {
    return (
      <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        <TriangleAlertIcon
          className="h-4 w-4 text-amber-500"
          aria-hidden="true"
        />
        {t("ask.notConfigured", "尚未配置 AI 服务")}
        <button
          type="button"
          onClick={() => requestSettingsSection("ai")}
          className="inline-flex items-center gap-1 rounded-md bg-primary/10 px-2 py-0.5 text-xs text-primary transition-colors hover:bg-primary/20"
        >
          <SettingsIcon className="h-3 w-3" aria-hidden="true" />
          {t("ask.goToSettings", "前往设置")}
        </button>
      </div>
    );
  }

  if (message.errorKind === "no-source") {
    return (
      <p className="text-sm text-muted-foreground">
        {t(
          "ask.noSource",
          "知识库中没有找到与问题相关的资料。先采集一些内容，或换个问法试试。",
        )}
      </p>
    );
  }

  return (
    <p className="flex items-start gap-1.5 text-sm text-destructive/90">
      <TriangleAlertIcon
        className="mt-0.5 h-4 w-4 shrink-0"
        aria-hidden="true"
      />
      {message.error}
    </p>
  );
}

function MessageCard({ message }: { message: AskMessage }) {
  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-md bg-primary px-4 py-2.5 text-sm text-primary-foreground">
          {message.question}
        </div>
      </div>

      <div className="rounded-2xl rounded-bl-md border border-border/70 bg-background/60 px-4 py-3">
        <AgentSteps
          steps={message.steps}
          running={message.status === "running"}
        />
        {message.status === "error" ? (
          <MessageError message={message} />
        ) : message.answer ? (
          <MarkdownBody content={message.answer} />
        ) : message.status === "running" && message.steps.length === 0 ? (
          <Loader2Icon
            className="h-4 w-4 animate-spin text-muted-foreground"
            aria-hidden="true"
          />
        ) : null}
        {message.status === "done" ? (
          <SourceList sources={message.sources} />
        ) : null}
      </div>
    </div>
  );
}

/**
 * AI 问答页：基于知识库的多轮问答，
 * Agent 检索过程实时展示，回答附可跳转的引用来源。
 */
export function AskWorkspace() {
  const { t } = useTranslation();
  const messages = useAskStore((state) => state.messages);
  const isRunning = useAskStore((state) => state.isRunning);
  const ask = useAskStore((state) => state.ask);
  const stop = useAskStore((state) => state.stop);
  const newSession = useAskStore((state) => state.newSession);
  const initialize = useAskStore((state) => state.initialize);

  const [draft, setDraft] = useState("");
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // 恢复上次会话（会话列表与消息持久化在主进程 SQLite）
  useEffect(() => {
    void initialize();
  }, [initialize]);

  // 新消息 / 步骤更新时滚到底部
  useEffect(() => {
    const container = scrollRef.current;
    if (container) {
      container.scrollTop = container.scrollHeight;
    }
  }, [messages]);

  const submit = () => {
    const question = draft.trim();
    if (!question || isRunning) {
      return;
    }
    setDraft("");
    void ask(question);
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground">
          {t("ask.title", "AI 问答")}
        </h2>
        {messages.length > 0 ? (
          <button
            type="button"
            onClick={newSession}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
          >
            <MessageSquarePlusIcon className="h-4 w-4" aria-hidden="true" />
            {t("ask.newSession", "新对话")}
          </button>
        ) : null}
      </div>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {messages.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
            <MessagesSquareIcon
              className="h-10 w-10 text-muted-foreground/40"
              aria-hidden="true"
            />
            <p className="text-sm text-muted-foreground">
              {t("ask.empty", "向你的知识库提问")}
            </p>
            <p className="max-w-sm text-xs text-muted-foreground/70">
              {t(
                "ask.emptyHint",
                "AI 会检索相关条目并给出带引用的回答，点击引用可跳回原文",
              )}
            </p>
          </div>
        ) : (
          <div className="mx-auto max-w-3xl space-y-6">
            {messages.map((message) => (
              <MessageCard key={message.id} message={message} />
            ))}
          </div>
        )}
      </div>

      <div className="shrink-0 border-t border-border px-5 py-3">
        <div className="mx-auto flex max-w-3xl items-end gap-2">
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                submit();
              }
            }}
            rows={Math.min(4, Math.max(1, draft.split("\n").length))}
            placeholder={t("ask.placeholder", "输入问题，Enter 发送…")}
            className="min-h-9 flex-1 resize-none rounded-xl border border-border bg-background/60 px-3.5 py-2 text-sm text-foreground placeholder:text-muted-foreground/70 focus:border-primary/50 focus:outline-none"
          />
          {isRunning ? (
            <button
              type="button"
              onClick={stop}
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
