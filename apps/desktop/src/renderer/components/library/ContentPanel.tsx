import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import {
  AudioLinesIcon,
  ImageIcon,
  Loader2Icon,
  MessagesSquareIcon,
  RotateCcwIcon,
  ScanTextIcon,
  SearchIcon,
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
import {
  filterForumReplies,
  parseForumReplySection,
  resolveReplyTargetFloor,
  splitForumNoteSections,
  type ForumReplyEntry,
} from "@guizhi/shared/utils/forum-note";
import { splitImageNoteSections } from "@guizhi/shared/utils/image-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import { useSettingsStore } from "../../stores/settings.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { useToast } from "../ui/Toast";
import { ImageGallery } from "./ImageGallery";
import {
  ForumDiscussionView,
  type ForumDiscussionHandle,
} from "./ForumDiscussionView";
import { defaultCatalogOpen, ForumFloorCatalog } from "./ForumFloorCatalog";
import { highlightText } from "./highlight-text";
import { MarkdownEditor } from "./MarkdownEditor";
import { MarkdownPreview } from "./MarkdownPreview";
import { PanelFindBar } from "./PanelFindBar";
import { ReviewRequiredNotice } from "./ReviewRequiredNotice";
import {
  loadContentReadingMemory,
  patchContentReadingMemory,
  type ReadingPanelTab,
} from "./reading-memory";
import { useMarkFindNavigation } from "./use-mark-find";
import {
  useMediaSummaryAction,
  useTranscriptActions,
} from "./use-media-actions";

type PanelTab = ReadingPanelTab;

const FINDABLE_TABS: PanelTab[] = ["summary", "body", "transcript", "replies"];

function noopMatchCount(_count: number): void {
  /* 讨论区在非激活标签时仍挂载（保滚动），勿回写查找计数 */
}

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

function MarkdownTabPane({
  active,
  content,
  centeredHeadings,
  highlightQuery,
  scrollRef,
}: {
  active: boolean;
  content: string;
  centeredHeadings?: boolean;
  highlightQuery?: string;
  scrollRef?: RefObject<HTMLDivElement | null>;
}) {
  return (
    <div className={active ? "h-full" : "hidden"} aria-hidden={!active}>
      <MarkdownPreview
        ref={scrollRef}
        content={content}
        centeredHeadings={centeredHeadings}
        highlightQuery={highlightQuery}
      />
    </div>
  );
}

function useDebouncedScrollSave(
  itemId: string,
  tab: PanelTab,
  getScrollTop: () => number | null,
) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flush = useCallback(() => {
    const top = getScrollTop();
    if (top == null) {
      return;
    }
    patchContentReadingMemory(itemId, {
      tab,
      scrollTopByTab: { [tab]: top },
    });
  }, [getScrollTop, itemId, tab]);

  const onScroll = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }
    timerRef.current = setTimeout(flush, 200);
  }, [flush]);

  useEffect(() => {
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
      flush();
    };
  }, [flush, itemId]);

  return onScroll;
}

/**
 * 正文面板：正文与文字稿以标签页并列，各自的操作放在面板头部右侧。
 */
export function ContentPanel({
  item,
  isTrashed,
}: {
  item: KnowledgeItem;
  isTrashed: boolean;
}) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const updateSelected = useKnowledgeStore((state) => state.updateSelected);
  const showLineNumbers = useSettingsStore((state) => state.showLineNumbers);
  const editorMarkdownPreview = useSettingsStore(
    (state) => state.editorMarkdownPreview,
  );
  const transcriptActions = useTranscriptActions(item);
  const summaryAction = useMediaSummaryAction(item);

  const [tab, setTab] = useState<PanelTab>("body");
  const [isPreview, setIsPreview] = useState(editorMarkdownPreview);
  const [findQuery, setFindQuery] = useState("");
  const [isFindOpen, setIsFindOpen] = useState(false);
  const [findActiveIndex, setFindActiveIndex] = useState(0);
  const [findMatchCount, setFindMatchCount] = useState(0);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const [catalogActiveFloor, setCatalogActiveFloor] = useState<number | null>(
    null,
  );
  const lastViewItemIdRef = useRef<string | null>(null);
  const restoredRef = useRef(false);

  const summaryScrollRef = useRef<HTMLDivElement>(null);
  const bodyScrollRef = useRef<HTMLDivElement>(null);
  const recognizedScrollRef = useRef<HTMLDivElement>(null);
  const transcriptScrollRef = useRef<HTMLDivElement>(null);
  const emptyScrollRef = useRef<HTMLDivElement>(null);
  const discussionRef = useRef<ForumDiscussionHandle>(null);
  const findInputRef = useRef<HTMLInputElement>(null);

  const isMediaItem = item.itemType === "audio" || item.itemType === "video";
  const showTranscriptTab =
    !isTrashed &&
    (transcriptActions.canTranscribe ||
      transcriptActions.transcript.length > 0);

  const sections =
    item.itemType === "image" && !isTrashed && isPreview
      ? splitImageNoteSections(item.content)
      : null;
  const showImagesTab = sections !== null;
  const showRecognizedTab = Boolean(sections?.recognized);

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

  const replies = useMemo(
    () =>
      forumSections?.replies
        ? parseForumReplySection(forumSections.replies)
        : [],
    [forumSections?.replies],
  );

  const getScrollTop = useCallback((): number | null => {
    if (activeTab === "summary") {
      return summaryScrollRef.current?.scrollTop ?? null;
    }
    if (activeTab === "body") {
      return bodyScrollRef.current?.scrollTop ?? null;
    }
    if (activeTab === "recognized") {
      return recognizedScrollRef.current?.scrollTop ?? null;
    }
    if (activeTab === "transcript") {
      return transcriptScrollRef.current?.scrollTop ?? null;
    }
    if (activeTab === "replies") {
      return discussionRef.current?.getScrollElement()?.scrollTop ?? null;
    }
    return null;
  }, [activeTab]);

  const onPaneScroll = useDebouncedScrollSave(item.id, activeTab, getScrollTop);

  // 切换条目：恢复记忆或默认视图
  useEffect(() => {
    if (lastViewItemIdRef.current === item.id) {
      return;
    }
    lastViewItemIdRef.current = item.id;
    restoredRef.current = false;
    const preview = editorMarkdownPreview && Boolean(item.content.trim());
    setIsPreview(preview);
    setFindQuery("");
    setIsFindOpen(false);
    setFindActiveIndex(0);
    setFindMatchCount(0);

    const memory = loadContentReadingMemory(item.id);
    const forum =
      item.itemType === "forum" && preview
        ? splitForumNoteSections(item.content)
        : null;
    const replyCount = forum?.replies
      ? parseForumReplySection(forum.replies).length
      : 0;

    if (
      memory &&
      memory.tab &&
      (memory.tab === "body" ||
        (memory.tab === "summary" && forum?.summary) ||
        (memory.tab === "replies" && forum?.replies) ||
        (memory.tab === "transcript" && showTranscriptTab) ||
        (memory.tab === "images" && item.itemType === "image") ||
        (memory.tab === "recognized" && item.itemType === "image"))
    ) {
      setTab(memory.tab);
      setFindQuery(memory.tab === "replies" ? (memory.repliesQuery ?? "") : "");
      setCatalogOpen(memory.catalogOpen ?? defaultCatalogOpen(replyCount));
    } else {
      const preferSummary =
        preview && item.itemType === "forum" && Boolean(forum?.summary);
      setTab(preferSummary ? "summary" : "body");
      setCatalogOpen(defaultCatalogOpen(replyCount));
    }
  }, [
    item.id,
    item.content,
    item.itemType,
    editorMarkdownPreview,
    showTranscriptTab,
  ]);

  // 恢复滚动位置（等 pane 挂好）
  useLayoutEffect(() => {
    if (restoredRef.current) {
      return;
    }
    const memory = loadContentReadingMemory(item.id);
    if (!memory) {
      restoredRef.current = true;
      return;
    }
    const top = memory.scrollTopByTab[activeTab];
    if (top == null) {
      restoredRef.current = true;
      return;
    }
    const apply = () => {
      if (activeTab === "summary" && summaryScrollRef.current) {
        summaryScrollRef.current.scrollTop = top;
      } else if (activeTab === "body" && bodyScrollRef.current) {
        bodyScrollRef.current.scrollTop = top;
      } else if (activeTab === "recognized" && recognizedScrollRef.current) {
        recognizedScrollRef.current.scrollTop = top;
      } else if (activeTab === "transcript" && transcriptScrollRef.current) {
        transcriptScrollRef.current.scrollTop = top;
      } else if (activeTab === "replies") {
        const el = discussionRef.current?.getScrollElement();
        if (el) {
          el.scrollTop = top;
        }
      }
      restoredRef.current = true;
    };
    requestAnimationFrame(apply);
  }, [activeTab, item.id]);

  useEffect(() => {
    patchContentReadingMemory(item.id, { tab: activeTab });
  }, [activeTab, item.id]);

  useEffect(() => {
    if (activeTab === "replies") {
      patchContentReadingMemory(item.id, {
        repliesQuery: findQuery,
        catalogOpen,
      });
    }
  }, [activeTab, catalogOpen, findQuery, item.id]);

  const previewContent = isMediaItem
    ? (parseVideoMetaBlock(item.content)?.body ?? item.content)
    : forumSections
      ? forumSections.body
      : (sections?.caption ?? item.content);

  const markContainerRef =
    activeTab === "summary"
      ? summaryScrollRef
      : activeTab === "body"
        ? bodyScrollRef
        : activeTab === "recognized"
          ? recognizedScrollRef
          : activeTab === "transcript"
            ? transcriptScrollRef
            : emptyScrollRef;

  const markContentKey =
    activeTab === "summary"
      ? (forumSections?.summary ?? "")
      : activeTab === "body"
        ? (forumSections?.body ?? sections?.caption ?? previewContent)
        : activeTab === "recognized"
          ? (sections?.recognized ?? "")
          : activeTab === "transcript"
            ? transcriptActions.transcript
            : "";

  useMarkFindNavigation({
    containerRef: markContainerRef,
    query: activeTab === "replies" ? "" : findQuery,
    activeIndex: findActiveIndex,
    onMatchCountChange:
      activeTab === "replies" ? noopMatchCount : setFindMatchCount,
    contentKey: `${item.id}:${activeTab}:${markContentKey.length}`,
  });

  const showFindBar =
    isPreview &&
    !isTrashed &&
    FINDABLE_TABS.includes(activeTab) &&
    (activeTab !== "transcript" || Boolean(transcriptActions.transcript)) &&
    (activeTab !== "replies" || showRepliesTab) &&
    (activeTab !== "summary" || showForumSummaryTab);

  const findPlaceholder =
    activeTab === "replies"
      ? t("library.forumRepliesFilterPlaceholder", "搜索楼层、作者或内容…")
      : t("library.panelFindPlaceholder", "在当前页查找…");

  // 正文查找不常驻工具栏：详情栏在非全屏窗口下较窄，固定输入框会挤掉
  // 标签与正文操作。保留搜索按钮，并使用浏览器/编辑器通用的 Ctrl/Cmd+F 打开浮层。
  useEffect(() => {
    if (!showFindBar) {
      setIsFindOpen(false);
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        event.preventDefault();
        setIsFindOpen(true);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [showFindBar]);

  useEffect(() => {
    if (!isFindOpen) {
      return;
    }
    const frame = requestAnimationFrame(() => findInputRef.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, [isFindOpen]);

  const handleSelectFloor = useCallback(
    (floor: number) => {
      const inFiltered =
        !findQuery.trim() || filterIncludesFloor(replies, findQuery, floor);
      if (!inFiltered) {
        setFindQuery("");
        setFindActiveIndex(0);
      }
      setCatalogActiveFloor(floor);
      requestAnimationFrame(() => {
        discussionRef.current?.scrollToFloor(floor);
      });
    },
    [findQuery, replies],
  );

  const handleReplyToClick = useCallback(
    (replyTo: NonNullable<ForumReplyEntry["replyTo"]>) => {
      const target = resolveReplyTargetFloor(replies, replyTo);
      if (target == null) {
        showToast(
          t("library.forumReplyJumpMiss", "该楼未入库或无法定位"),
          "warning",
        );
        return;
      }
      handleSelectFloor(target);
    },
    [handleSelectFloor, replies, showToast, t],
  );

  const changeTab = (next: PanelTab) => {
    const top = getScrollTop();
    if (top != null) {
      patchContentReadingMemory(item.id, {
        tab: activeTab,
        scrollTopByTab: { [activeTab]: top },
      });
    }
    setTab(next);
    setFindActiveIndex(0);
    restoredRef.current = true;
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col px-4 pb-4 pt-3">
      <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border app-wallpaper-panel">
        <div className="flex h-9 shrink-0 items-center gap-1 border-b border-border/60 px-2">
          <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto overscroll-x-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            {showForumSummaryTab ? (
              <TabButton
                active={activeTab === "summary"}
                onClick={() => changeTab("summary")}
              >
                <ScrollTextIcon className="h-3 w-3" aria-hidden="true" />
                {t("library.forumSummarySection", "讨论总结")}
              </TabButton>
            ) : null}
            <TabButton
              active={activeTab === "body"}
              onClick={() => changeTab("body")}
            >
              {sections
                ? t("library.captionSection", "文案")
                : t("library.bodySection", "正文")}
            </TabButton>
            {showRepliesTab ? (
              <TabButton
                active={activeTab === "replies"}
                onClick={() => changeTab("replies")}
              >
                <MessagesSquareIcon className="h-3 w-3" aria-hidden="true" />
                {t("library.forumRepliesSection", "讨论")}
              </TabButton>
            ) : null}
            {showImagesTab ? (
              <TabButton
                active={activeTab === "images"}
                onClick={() => changeTab("images")}
              >
                <ImageIcon className="h-3 w-3" aria-hidden="true" />
                {t("library.imagesSection", "图片")}
              </TabButton>
            ) : null}
            {showRecognizedTab ? (
              <TabButton
                active={activeTab === "recognized"}
                onClick={() => changeTab("recognized")}
              >
                <ScanTextIcon className="h-3 w-3" aria-hidden="true" />
                {t("library.recognizedSection", "图中文字")}
              </TabButton>
            ) : null}
            {showTranscriptTab ? (
              <TabButton
                active={activeTab === "transcript"}
                onClick={() => changeTab("transcript")}
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
          </div>

          <div className="ml-1 flex shrink-0 items-center gap-1">
            {showFindBar ? (
              <ToolButton
                onClick={() => setIsFindOpen(true)}
                label={t("library.panelFindOpen", "在当前页查找 (Ctrl+F)")}
              >
                <SearchIcon className="h-3.5 w-3.5" aria-hidden="true" />
              </ToolButton>
            ) : null}

            {activeTab !== "transcript" ? (
              <>
                {!isTrashed && summaryAction.available ? (
                  <ToolButton
                    onClick={() => void summaryAction.summarize()}
                    label={summaryAction.label}
                    busy={summaryAction.isRunning}
                  >
                    <ScrollTextIcon
                      className="h-3.5 w-3.5"
                      aria-hidden="true"
                    />
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
                        busy={transcriptActions.runningAction === "diarize"}
                        disabled={transcriptActions.isRunning}
                      >
                        <UsersIcon className="h-3.5 w-3.5" aria-hidden="true" />
                      </ToolButton>
                    ) : null}
                    <ToolButton
                      onClick={() => void transcriptActions.transcribe()}
                      label={t(
                        "library.transcribeRegenerate",
                        "重新生成文字稿",
                      )}
                      busy={transcriptActions.runningAction === "transcribe"}
                      disabled={transcriptActions.isRunning}
                    >
                      <RotateCcwIcon
                        className="h-3.5 w-3.5"
                        aria-hidden="true"
                      />
                    </ToolButton>
                  </>
                ) : null}
              </>
            )}
          </div>
        </div>

        <ReviewRequiredNotice item={item} disabled={isTrashed} />

        {isFindOpen && showFindBar ? (
          <div className="absolute right-2 top-11 z-20 flex w-full max-w-[calc(100%_-_1rem)] justify-end">
            <PanelFindBar
              query={findQuery}
              onQueryChange={(next) => {
                setFindQuery(next);
                setFindActiveIndex(0);
              }}
              activeIndex={findActiveIndex}
              matchCount={findMatchCount}
              onActiveIndexChange={setFindActiveIndex}
              placeholder={findPlaceholder}
              inputRef={findInputRef}
              onClose={() => setIsFindOpen(false)}
            />
          </div>
        ) : null}

        <div className="min-h-0 flex-1" onScrollCapture={onPaneScroll}>
          {activeTab === "transcript" ? (
            <TranscriptPane
              actions={transcriptActions}
              scrollRef={transcriptScrollRef}
              highlightQuery={findQuery}
            />
          ) : activeTab === "images" ? (
            <ImageGallery content={item.content} />
          ) : forumSections ? (
            <div key={item.id} className="h-full">
              {showForumSummaryTab ? (
                <MarkdownTabPane
                  active={activeTab === "summary"}
                  content={forumSections.summary}
                  highlightQuery={
                    activeTab === "summary" ? findQuery : undefined
                  }
                  scrollRef={summaryScrollRef}
                />
              ) : null}
              <MarkdownTabPane
                active={activeTab === "body"}
                content={forumSections.body}
                centeredHeadings
                highlightQuery={activeTab === "body" ? findQuery : undefined}
                scrollRef={bodyScrollRef}
              />
              {showRepliesTab ? (
                <div
                  className={activeTab === "replies" ? "h-full" : "hidden"}
                  aria-hidden={activeTab !== "replies"}
                >
                  <ForumDiscussionView
                    ref={discussionRef}
                    content={forumSections.replies}
                    query={findQuery}
                    activeIndex={findActiveIndex}
                    findNavEnabled={activeTab === "replies"}
                    onMatchCountChange={setFindMatchCount}
                    onReplyToClick={handleReplyToClick}
                    catalog={
                      <ForumFloorCatalog
                        replies={replies}
                        open={catalogOpen}
                        onOpenChange={setCatalogOpen}
                        activeFloor={catalogActiveFloor}
                        onSelectFloor={handleSelectFloor}
                      />
                    }
                  />
                </div>
              ) : null}
            </div>
          ) : sections ? (
            <div key={item.id} className="h-full">
              <MarkdownTabPane
                active={activeTab === "body"}
                content={sections.caption}
                highlightQuery={activeTab === "body" ? findQuery : undefined}
                scrollRef={bodyScrollRef}
              />
              {showRecognizedTab ? (
                <MarkdownTabPane
                  active={activeTab === "recognized"}
                  content={sections.recognized}
                  highlightQuery={
                    activeTab === "recognized" ? findQuery : undefined
                  }
                  scrollRef={recognizedScrollRef}
                />
              ) : null}
            </div>
          ) : isTrashed || isPreview ? (
            <MarkdownPreview
              ref={bodyScrollRef}
              content={isTrashed ? item.content : previewContent}
              highlightQuery={findQuery}
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

function filterIncludesFloor(
  replies: ForumReplyEntry[],
  query: string,
  floor: number,
): boolean {
  const reply = replies.find((r) => r.floor === floor);
  if (!reply) {
    return false;
  }
  return filterForumReplies([reply], query).length > 0;
}

const STAGE_LABELS: Record<TranscribeStage, { key: string; text: string }> = {
  transcribing: { key: "library.stageTranscribing", text: "正在转写" },
  formatting: { key: "library.stageFormatting", text: "正在排版文字稿" },
  summarizing: { key: "library.stageSummarizing", text: "正在生成总结" },
};

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

function TranscriptPane({
  actions,
  scrollRef,
  highlightQuery,
}: {
  actions: ReturnType<typeof useTranscriptActions>;
  scrollRef?: RefObject<HTMLDivElement | null>;
  highlightQuery?: string;
}) {
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
    <div ref={scrollRef} className="h-full overflow-y-auto px-6 py-4">
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
        {highlightQuery?.trim()
          ? highlightText(actions.transcript, highlightQuery)
          : actions.transcript}
      </p>
    </div>
  );
}
