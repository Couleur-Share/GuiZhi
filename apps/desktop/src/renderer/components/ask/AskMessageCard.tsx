import { useMemo, useState } from "react";
import {
  BookOpenIcon,
  CheckIcon,
  ChevronDownIcon,
  CopyIcon,
  Loader2Icon,
  NetworkIcon,
  RotateCcwIcon,
  SettingsIcon,
  SparklesIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAskStore, type AskMessage } from "../../stores/ask.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { MarkdownBody } from "../library/MarkdownPreview";
import { linkifyCitations } from "./qa-citations";
import type { QaSourceRef } from "../../services/knowledge-ai/qa";

/** 打开某条引用来源（Wiki 页面或知识条目） */
function useOpenSource() {
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setAppModule = useUIStore((state) => state.setAppModule);

  return async (source: QaSourceRef) => {
    if (source.kind === "wiki") {
      setAppModule("wiki");
      const { useWikiStore } = await import("../../stores/wiki.store");
      await useWikiStore.getState().selectPage(source.refId);
      return;
    }
    setAppModule("library");
    await selectItem(source.refId);
  };
}

/**
 * 检索过程。运行中只显示当前这一步——早先是把已完成的步骤全堆着，
 * 每多一步卡片就长一截，正文在眼皮底下往下跳。
 */
function AgentSteps({ steps, running }: { steps: string[]; running: boolean }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);

  if (steps.length === 0) {
    return null;
  }

  if (running) {
    return (
      <div className="mb-2 flex items-center gap-1.5 text-xs text-muted-foreground">
        <Loader2Icon className="h-3 w-3 shrink-0 animate-spin" aria-hidden="true" />
        <span className="truncate">{steps[steps.length - 1]}</span>
        {steps.length > 1 ? (
          <span className="shrink-0 tabular-nums text-muted-foreground/50">
            {steps.length}
          </span>
        ) : null}
      </div>
    );
  }

  return (
    <div className="mb-2">
      <button
        type="button"
        onClick={() => setExpanded((value) => !value)}
        aria-expanded={expanded}
        className="flex items-center gap-1 text-xs text-muted-foreground/70 transition-colors hover:text-muted-foreground"
      >
        <ChevronDownIcon
          className={`h-3 w-3 transition-transform duration-quick ${
            expanded ? "" : "-rotate-90"
          }`}
          aria-hidden="true"
        />
        {t("ask.stepsLabel", "检索过程（{{count}} 步）", {
          count: steps.length,
        })}
      </button>
      {expanded ? (
        <ol className="mt-1 space-y-0.5 border-l border-border/60 pl-3">
          {steps.map((step, index) => (
            <li key={index} className="text-xs text-muted-foreground/70">
              {step}
            </li>
          ))}
        </ol>
      ) : null}
    </div>
  );
}

function SourceList({
  sources,
  highlighted,
  onOpen,
}: {
  sources: QaSourceRef[];
  highlighted: number | null;
  onOpen: (source: QaSourceRef) => void;
}) {
  const { t } = useTranslation();
  if (sources.length === 0) {
    return null;
  }

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
            data-citation={source.ordinal}
            onClick={() => onOpen(source)}
            className={`inline-flex max-w-64 items-center gap-1 rounded-lg border px-2 py-1 text-xs text-foreground transition-colors ${
              highlighted === source.ordinal
                ? "border-primary bg-primary/10"
                : "border-border/70 bg-background/60 hover:border-primary/40 hover:bg-primary/5"
            }`}
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

function MessageActions({ message }: { message: AskMessage }) {
  const { t } = useTranslation();
  const retry = useAskStore((state) => state.retry);
  const removeMessage = useAskStore((state) => state.removeMessage);
  const isRunning = useAskStore((state) => state.isRunning);
  const [copied, setCopied] = useState(false);

  const copyAnswer = async () => {
    await navigator.clipboard.writeText(message.answer);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };

  const actionClass =
    "inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground";

  return (
    <div className="mt-2 flex items-center gap-0.5 opacity-0 transition-opacity duration-quick group-hover/turn:opacity-100 group-focus-within/turn:opacity-100">
      {message.answer ? (
        <button type="button" onClick={() => void copyAnswer()} className={actionClass}>
          {copied ? (
            <CheckIcon className="h-3 w-3" aria-hidden="true" />
          ) : (
            <CopyIcon className="h-3 w-3" aria-hidden="true" />
          )}
          {copied ? t("ask.copied", "已复制") : t("ask.copyAnswer", "复制")}
        </button>
      ) : null}
      <button
        type="button"
        onClick={() => void retry(message.id)}
        disabled={isRunning}
        className={`${actionClass} disabled:opacity-40`}
      >
        <RotateCcwIcon className="h-3 w-3" aria-hidden="true" />
        {t("ask.retryTurn", "重新回答")}
      </button>
      <button
        type="button"
        onClick={() => removeMessage(message.id)}
        className={`${actionClass} hover:bg-destructive/10 hover:text-destructive`}
      >
        <Trash2Icon className="h-3 w-3" aria-hidden="true" />
        {t("common.delete", "删除")}
      </button>
    </div>
  );
}

/**
 * 一轮问答。回答正文里的 `[n]` 是可点的锚点：点击滚到底部对应的来源并高亮，
 * 再点来源本身才跳去原文——中间这一步让「这句话依据哪份资料」看得见。
 */
export function AskMessageCard({ message }: { message: AskMessage }) {
  const { t } = useTranslation();
  const openSource = useOpenSource();
  const [highlighted, setHighlighted] = useState<number | null>(null);

  const validOrdinals = useMemo(
    () => new Set(message.sources.map((source) => source.ordinal)),
    [message.sources],
  );
  const body = useMemo(
    () => linkifyCitations(message.answer, validOrdinals),
    [message.answer, validOrdinals],
  );

  const focusCitation = (ordinal: number) => {
    setHighlighted(ordinal);
    window.setTimeout(() => setHighlighted(null), 2000);
  };

  const isRunning = message.status === "running";

  return (
    <div className="group/turn space-y-3">
      <div className="flex justify-end">
        <div className="max-w-[85%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-primary px-4 py-2.5 text-sm text-primary-foreground">
          {message.question}
        </div>
      </div>

      <div className="rounded-2xl rounded-bl-md border border-border/70 bg-background/60 px-4 py-3">
        <AgentSteps steps={message.steps} running={isRunning} />

        {message.status === "error" ? (
          <MessageError message={message} />
        ) : message.answer ? (
          <>
            <MarkdownBody content={body} onCitationClick={focusCitation} />
            {message.truncated ? (
              <p className="mt-2 flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-500">
                <TriangleAlertIcon
                  className="mt-0.5 h-3.5 w-3.5 shrink-0"
                  aria-hidden="true"
                />
                {t(
                  "ask.answerTruncated",
                  "回答达到模型输出长度上限，内容可能不完整。",
                )}
              </p>
            ) : null}
          </>
        ) : isRunning && message.steps.length === 0 ? (
          <Loader2Icon
            className="h-4 w-4 animate-spin text-muted-foreground"
            aria-hidden="true"
          />
        ) : null}

        {message.status === "done" ? (
          <SourceList
            sources={message.sources}
            highlighted={highlighted}
            onOpen={(source) => void openSource(source)}
          />
        ) : null}

        {!isRunning ? (
          <div className="mt-1 flex flex-wrap items-center gap-x-2">
            {/* 走了兜底管线 = Agent 协议没跑通，回答质量与检索深度都会差一档，
                不标出来用户无从解释为什么这次答得浅 */}
            {message.model ? (
              <span className="mt-2 inline-flex items-center gap-1 text-[11px] text-muted-foreground/60">
                <SparklesIcon className="h-3 w-3" aria-hidden="true" />
                {message.model}
              </span>
            ) : null}
            {message.usedFallback ? (
              <span
                title={t(
                  "ask.fallbackHint",
                  "本轮未能走通 Agent 检索协议，改用一次性检索作答",
                )}
                className="mt-2 rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[11px] text-amber-600 dark:text-amber-400"
              >
                {t("ask.fallbackBadge", "简易检索")}
              </span>
            ) : null}
            <span className="min-w-0 flex-1" />
            <MessageActions message={message} />
          </div>
        ) : null}
      </div>
    </div>
  );
}
