import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  AudioLinesIcon,
  ImageIcon,
  Loader2Icon,
  MessagesSquareIcon,
  RotateCcwIcon,
  ScanTextIcon,
  ScrollTextIcon,
  SparklesIcon,
  UsersIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type {
  KnowledgeItem,
  TranscribeProgress,
  TranscribeStage,
} from "@guizhi/shared/types";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { splitImageNoteSections } from "@guizhi/shared/utils/image-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import { useSettingsStore } from "../../stores/settings.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { ImageGallery } from "./ImageGallery";
import { MarkdownEditor } from "./MarkdownEditor";
import { MarkdownPreview } from "./MarkdownPreview";
import {
  useMediaSummaryAction,
  useTranscriptActions,
} from "./use-media-actions";

type PanelTab =
  | "body"
  | "transcript"
  | "images"
  | "recognized"
  | "summary"
  | "replies";

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
    const preview = editorMarkdownPreview && Boolean(item.content.trim());
    setIsPreview(preview);
    // 论坛帖打开先看讨论总结：主楼往往只是提问，结论都在回复里
    const preferSummary =
      preview &&
      item.itemType === "forum" &&
      Boolean(splitForumNoteSections(item.content).summary);
    setTab(preferSummary ? "summary" : "body");
  }, [item.id, item.content, item.itemType, editorMarkdownPreview]);

  const isMediaItem = item.itemType === "audio" || item.itemType === "video";
  const showTranscriptTab =
    !isTrashed &&
    (transcriptActions.canTranscribe || transcriptActions.transcript.length > 0);

  // 图片条目：文案 / 图片 / 图中文字 混在一屏里很难读，渲染视图下拆成三个标签。
  // 「显示原文」是看完整 Markdown 源码，此时不再分段。
  const sections =
    item.itemType === "image" && !isTrashed && isPreview
      ? splitImageNoteSections(item.content)
      : null;
  const showImagesTab = sections !== null;
  const showRecognizedTab = Boolean(sections?.recognized);

  // 论坛条目：讨论总结 / 主楼 / 逐楼回复同样拆开看，一屏滚到一百楼没法读
  const forumSections =
    item.itemType === "forum" && !isTrashed && isPreview
      ? splitForumNoteSections(item.content)
      : null;
  const showForumSummaryTab = Boolean(forumSections?.summary);
  const showRepliesTab = Boolean(forumSections?.replies);

  const availableTabs: Record<PanelTab, boolean> = {
    body: true,
    transcript: showTranscriptTab,
    images: showImagesTab,
    recognized: showRecognizedTab,
    summary: showForumSummaryTab,
    replies: showRepliesTab,
  };
  const activeTab: PanelTab = availableTabs[tab] ? tab : "body";

  // 渲染视图中元数据引用块交给来源 chip 展示，正文只渲染剩余部分
  const previewContent = isMediaItem
    ? parseVideoMetaBlock(item.content)?.body ?? item.content
    : forumSections
      ? forumSections.body
      : (sections?.caption ?? item.content);

  return (
    <div className="flex min-h-0 flex-1 flex-col px-4 pb-4 pt-3">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border app-wallpaper-panel">
        <div className="flex h-9 shrink-0 items-center gap-1 border-b border-border/60 px-2">
          {showForumSummaryTab ? (
            <TabButton
              active={activeTab === "summary"}
              onClick={() => setTab("summary")}
            >
              <ScrollTextIcon className="h-3 w-3" aria-hidden="true" />
              {t("library.forumSummarySection", "讨论总结")}
            </TabButton>
          ) : null}
          <TabButton
            active={activeTab === "body"}
            onClick={() => setTab("body")}
          >
            {sections
              ? t("library.captionSection", "文案")
              : t("library.bodySection", "正文")}
          </TabButton>
          {showRepliesTab ? (
            <TabButton
              active={activeTab === "replies"}
              onClick={() => setTab("replies")}
            >
              <MessagesSquareIcon className="h-3 w-3" aria-hidden="true" />
              {t("library.forumRepliesSection", "讨论")}
            </TabButton>
          ) : null}
          {showImagesTab ? (
            <TabButton
              active={activeTab === "images"}
              onClick={() => setTab("images")}
            >
              <ImageIcon className="h-3 w-3" aria-hidden="true" />
              {t("library.imagesSection", "图片")}
            </TabButton>
          ) : null}
          {showRecognizedTab ? (
            <TabButton
              active={activeTab === "recognized"}
              onClick={() => setTab("recognized")}
            >
              <ScanTextIcon className="h-3 w-3" aria-hidden="true" />
              {t("library.recognizedSection", "图中文字")}
            </TabButton>
          ) : null}
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

          {activeTab !== "transcript" ? (
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
                  label={
                    transcriptActions.formatProgress
                      ? t(
                          "library.transcriptFormatProgress",
                          "正在排版…已完成 {{current}}/{{total}} 块",
                          {
                            current: transcriptActions.formatProgress.current,
                            total: transcriptActions.formatProgress.total,
                          },
                        )
                      : t("library.transcriptFormat", "AI 排版")
                  }
                  busy={transcriptActions.isFormatting}
                  disabled={transcriptActions.isRunning}
                >
                  <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
                </ToolButton>
              ) : null}
              {transcriptActions.canTranscribe &&
              transcriptActions.transcript ? (
                <>
                  {transcriptActions.canDiarize ? (
                    <ToolButton
                      onClick={() =>
                        void transcriptActions.transcribe({ diarize: true })
                      }
                      label={t(
                        "library.transcribeDiarize",
                        "重新生成并区分说话人",
                      )}
                      // 只有被点的那个转圈，其余置灰——两个都转会让人以为
                      // 整个界面都在忙
                      busy={transcriptActions.runningAction === "diarize"}
                      disabled={transcriptActions.isRunning}
                    >
                      <UsersIcon className="h-3.5 w-3.5" aria-hidden="true" />
                    </ToolButton>
                  ) : null}
                  <ToolButton
                    onClick={() => void transcriptActions.transcribe()}
                    label={t("library.transcribeRegenerate", "重新生成文字稿")}
                    busy={transcriptActions.runningAction === "transcribe"}
                    disabled={transcriptActions.isRunning}
                  >
                    <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  </ToolButton>
                </>
              ) : null}
            </>
          )}
        </div>

        <div className="min-h-0 flex-1">
          {activeTab === "transcript" ? (
            <TranscriptPane actions={transcriptActions} />
          ) : activeTab === "images" ? (
            <ImageGallery content={item.content} />
          ) : activeTab === "recognized" ? (
            <MarkdownPreview content={sections?.recognized ?? ""} />
          ) : activeTab === "summary" ? (
            <MarkdownPreview content={forumSections?.summary ?? ""} />
          ) : activeTab === "replies" ? (
            <MarkdownPreview content={forumSections?.replies ?? ""} />
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

      <ConfirmDialog
        isOpen={transcriptActions.pendingLongFormat !== null}
        onClose={transcriptActions.cancelLongFormat}
        onConfirm={() => void transcriptActions.confirmLongFormat()}
        title={t("library.transcriptFormat", "AI 排版")}
        message={t(
          "library.transcriptFormatLongConfirm",
          "这份文字稿约 {{chars}} 字，排版会拆成 {{chunks}} 次请求串行发给模型，可能需要几分钟并产生相应用量。继续？",
          transcriptActions.pendingLongFormat ?? { chars: 0, chunks: 0 },
        )}
        confirmText={t("common.confirm", "确认")}
        cancelText={t("common.cancel", "取消")}
      />
    </div>
  );
}

const STAGE_LABELS: Record<TranscribeStage, { key: string; text: string }> = {
  transcribing: { key: "library.stageTranscribing", text: "正在转写" },
  formatting: { key: "library.stageFormatting", text: "正在排版文字稿" },
  summarizing: { key: "library.stageSummarizing", text: "正在生成总结" },
};

/**
 * 只报当前阶段与已用时长，不报百分比——funasr 给不出可用的分母。
 * 阶段是必须的：三步都以分钟计，光说「正在转写」会让后两步看起来像卡住。
 */
function transcribeStatusText(
  t: TFunction,
  progress: TranscribeProgress | null,
): string {
  if (!progress) {
    return t("library.transcribeRunning", "正在转写（可能需要几分钟）…");
  }
  const label = STAGE_LABELS[progress.stage] ?? STAGE_LABELS.transcribing;
  const stage = t(label.key, label.text);
  const elapsed = formatClock(progress.elapsedMs);
  // 心跳停了一分钟以上才值得说，正常间隔本来就是秒级
  if (progress.stalledMs !== undefined && progress.stalledMs >= 60_000) {
    return t(
      "library.transcribeStalled",
      "{{stage}}…已用 {{elapsed}}，但已有 {{stalled}} 没有进展",
      { stage, elapsed, stalled: formatClock(progress.stalledMs) },
    );
  }
  return t("library.transcribeElapsed", "{{stage}}…已用 {{elapsed}}", {
    stage,
    elapsed,
  });
}

function formatClock(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
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
            ? transcribeStatusText(t, actions.transcribeProgress)
            : actions.isOnlineVideo
              ? t("library.transcribeRegenerateOnline", "重新生成文字稿")
              : t("library.transcribeGenerate", "生成文字稿")}
        </button>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto px-6 py-4">
      {actions.isRunning ? (
        <p className="mb-3 inline-flex items-center gap-1.5 rounded-md bg-accent/50 px-2.5 py-1 text-xs text-muted-foreground">
          <Loader2Icon className="h-3 w-3 animate-spin" aria-hidden="true" />
          {transcribeStatusText(t, actions.transcribeProgress)}
        </p>
      ) : null}
      {actions.isFormatting ? (
        <p className="mb-3 inline-flex items-center gap-1.5 rounded-md bg-accent/50 px-2.5 py-1 text-xs text-muted-foreground">
          <Loader2Icon className="h-3 w-3 animate-spin" aria-hidden="true" />
          {actions.formatProgress
            ? t(
                "library.transcriptFormatProgress",
                "正在排版…已完成 {{current}}/{{total}} 块",
                {
                  current: actions.formatProgress.current,
                  total: actions.formatProgress.total,
                },
              )
            : t("library.transcriptFormatting", "正在排版…")}
        </p>
      ) : null}
      <p className="whitespace-pre-wrap text-sm leading-relaxed text-foreground/85">
        {actions.transcript}
      </p>
    </div>
  );
}
