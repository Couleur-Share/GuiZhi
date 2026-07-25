import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  AudioLinesIcon,
  Loader2Icon,
  RotateCcwIcon,
  ScrollTextIcon,
  SparklesIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import { useSettingsStore } from "../../stores/settings.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { MarkdownEditor } from "./MarkdownEditor";
import { MarkdownPreview } from "./MarkdownPreview";
import {
  useMediaSummaryAction,
  useTranscriptActions,
} from "./use-media-actions";

type PanelTab = "body" | "transcript";

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`inline-flex h-6 items-center gap-1 rounded-md px-2 text-[11px] font-medium transition-colors ${
        active
          ? "bg-primary/15 text-primary"
          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

function ToolButton({
  onClick,
  label,
  disabled,
  busy,
  children,
}: {
  onClick: () => void;
  label: string;
  disabled?: boolean;
  busy?: boolean;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || busy}
      title={label}
      aria-label={label}
      className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
    >
      {busy ? (
        <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
      ) : (
        children
      )}
    </button>
  );
}

/**
 * 正文面板：正文与文字稿以标签页并列，各自的操作放在面板头部右侧。
 * 文字稿从原来的固定卡片改为标签页，避免在正文上方挤占版面。
 */
export function ContentPanel({
  item,
  isTrashed,
}: {
  item: KnowledgeItem;
  isTrashed: boolean;
}) {
  const { t } = useTranslation();
  const updateSelected = useKnowledgeStore((state) => state.updateSelected);
  const showLineNumbers = useSettingsStore((state) => state.showLineNumbers);
  const editorMarkdownPreview = useSettingsStore(
    (state) => state.editorMarkdownPreview,
  );
  const transcriptActions = useTranscriptActions(item);
  const summaryAction = useMediaSummaryAction(item);

  const [tab, setTab] = useState<PanelTab>("body");
  const [isPreview, setIsPreview] = useState(editorMarkdownPreview);
  const lastViewItemIdRef = useRef<string | null>(null);

  // 切换条目回到默认视图：有内容进渲染视图，空内容（新建笔记）直接进编辑
  useEffect(() => {
    if (lastViewItemIdRef.current === item.id) {
      return;
    }
    lastViewItemIdRef.current = item.id;
    setTab("body");
    setIsPreview(editorMarkdownPreview && Boolean(item.content.trim()));
  }, [item.id, item.content, editorMarkdownPreview]);

  const isMediaItem = item.itemType === "audio" || item.itemType === "video";
  const showTranscriptTab =
    !isTrashed &&
    (transcriptActions.canTranscribe || transcriptActions.transcript.length > 0);
  const activeTab: PanelTab =
    tab === "transcript" && !showTranscriptTab ? "body" : tab;

  // 渲染视图中元数据引用块交给来源 chip 展示，正文只渲染剩余部分
  const previewContent = isMediaItem
    ? parseVideoMetaBlock(item.content)?.body ?? item.content
    : item.content;

  return (
    <div className="flex min-h-0 flex-1 flex-col px-4 pb-4 pt-3">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border app-wallpaper-panel">
        <div className="flex h-9 shrink-0 items-center gap-1 border-b border-border/60 px-2">
          <TabButton
            active={activeTab === "body"}
            onClick={() => setTab("body")}
          >
            {t("library.bodySection", "正文")}
          </TabButton>
          {showTranscriptTab ? (
            <TabButton
              active={activeTab === "transcript"}
              onClick={() => setTab("transcript")}
            >
              <AudioLinesIcon className="h-3 w-3" aria-hidden="true" />
              {t("library.transcript", "文字稿")}
              {transcriptActions.transcript ? (
                <span className="text-[10px] opacity-60">
                  {t("library.transcriptLength", "{{count}} 字", {
                    count: transcriptActions.transcript.length,
                  })}
                </span>
              ) : null}
            </TabButton>
          ) : null}

          <span className="min-w-0 flex-1" />

          {activeTab === "body" ? (
            <>
              {!isTrashed && summaryAction.available ? (
                <ToolButton
                  onClick={() => void summaryAction.summarize()}
                  label={summaryAction.label}
                  busy={summaryAction.isRunning}
                >
                  <ScrollTextIcon className="h-3.5 w-3.5" aria-hidden="true" />
                </ToolButton>
              ) : null}
              {!isTrashed ? (
                <button
                  type="button"
                  onClick={() => setIsPreview(!isPreview)}
                  className="inline-flex h-6 shrink-0 items-center rounded-md border border-border/70 px-2 text-[11px] text-muted-foreground transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground"
                >
                  {isPreview
                    ? t("library.showSource", "显示原文")
                    : t("library.renderMarkdown", "Markdown 渲染")}
                </button>
              ) : null}
            </>
          ) : (
            <>
              {transcriptActions.transcript ? (
                <ToolButton
                  onClick={() => void transcriptActions.format()}
                  label={t("library.transcriptFormat", "AI 排版")}
                  busy={transcriptActions.isFormatting}
                  disabled={transcriptActions.isRunning}
                >
                  <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
                </ToolButton>
              ) : null}
              {transcriptActions.canTranscribe &&
              transcriptActions.transcript ? (
                <ToolButton
                  onClick={() => void transcriptActions.transcribe()}
                  label={t("library.transcribeRegenerate", "重新生成文字稿")}
                  busy={transcriptActions.isRunning}
                >
                  <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
                </ToolButton>
              ) : null}
            </>
          )}
        </div>

        <div className="min-h-0 flex-1">
          {activeTab === "transcript" ? (
            <TranscriptPane actions={transcriptActions} />
          ) : isTrashed || isPreview ? (
            // 回收站视图不显示元数据卡片，保留完整原文避免信息丢失
            <MarkdownPreview
              content={isTrashed ? item.content : previewContent}
            />
          ) : (
            <MarkdownEditor
              docId={item.id}
              value={item.content}
              onChange={(content) => updateSelected({ content })}
              showLineNumbers={showLineNumbers}
              placeholderText={t(
                "library.contentPlaceholder",
                "开始输入 Markdown 内容…",
              )}
            />
          )}
        </div>
      </div>
    </div>
  );
}

function TranscriptPane({ actions }: { actions: ReturnType<typeof useTranscriptActions> }) {
  const { t } = useTranslation();

  if (!actions.transcript) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
        <p className="text-sm text-muted-foreground">
          {t("library.transcriptEmpty", "还没有文字稿")}
        </p>
        <button
          type="button"
          onClick={() => void actions.transcribe()}
          disabled={actions.isRunning}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-60"
        >
          {actions.isRunning ? (
            <Loader2Icon
              className="h-3.5 w-3.5 animate-spin"
              aria-hidden="true"
            />
          ) : (
            <AudioLinesIcon className="h-3.5 w-3.5" aria-hidden="true" />
          )}
          {actions.isRunning
            ? t("library.transcribeRunning", "正在转写（可能需要几分钟）…")
            : actions.isOnlineVideo
              ? t("library.transcribeRegenerateOnline", "重新生成文字稿")
              : t("library.transcribeGenerate", "生成文字稿")}
        </button>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto px-6 py-4">
      <p className="whitespace-pre-wrap text-sm leading-relaxed text-foreground/85">
        {actions.transcript}
      </p>
    </div>
  );
}
