import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  IPC_CHANNELS,
  TRANSCRIPT_FORMAT_CHUNK_CHARS,
  TRANSCRIPT_FORMAT_LONG_CHARS,
} from "@guizhi/shared/constants";
import type {
  KnowledgeItem,
  TranscribeProgress,
  TranscriptFormatProgress,
} from "@guizhi/shared/types";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { detectForumPlatform } from "@guizhi/shared/utils/forum-platforms";
import { extractLocalAssetRef } from "@guizhi/shared/utils/media-refs";
import { hasMediaSummarySection } from "@guizhi/shared/utils/media-summary";
import { detectVideoPlatform } from "@guizhi/shared/utils/video-platforms";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";

export interface TranscriptActions {
  /** 具备转写条件（本地媒体资产或可重新下载音轨的在线视频） */
  canTranscribe: boolean;
  isOnlineVideo: boolean;
  transcript: string;
  isRunning: boolean;
  isFormatting: boolean;
  /** 当前「语音转写」路由是否支持区分说话人（仅内置本地引擎） */
  canDiarize: boolean;
  /**
   * 正在跑的是哪一个转写动作。
   * 两个按钮共用 isRunning 的话会一起转圈，看起来像整个界面都在忙。
   */
  runningAction: "transcribe" | "diarize" | null;
  /** 转写进行中的已用/停滞时长；未在转写时为 null */
  transcribeProgress: TranscribeProgress | null;
  /** 排版进行中的逐块进度；未在排版或尚未收到首个事件时为 null */
  formatProgress: TranscriptFormatProgress | null;
  /** 非空表示正等用户确认长稿排版的代价 */
  pendingLongFormat: { chars: number; chunks: number } | null;
  transcribe: (options?: { diarize?: boolean }) => Promise<void>;
  format: () => Promise<void>;
  confirmLongFormat: () => Promise<void>;
  cancelLongFormat: () => void;
}

/**
 * 音视频转写与文字稿 AI 排版。
 * 状态提到 hook 里，让面板头部的按钮和内容区共用同一份运行状态。
 */
export function useTranscriptActions(item: KnowledgeItem): TranscriptActions {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const flushPendingSave = useKnowledgeStore((state) => state.flushPendingSave);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [runningAction, setRunningAction] = useState<
    "transcribe" | "diarize" | null
  >(null);
  const isRunning = runningAction !== null;
  const [isFormatting, setIsFormatting] = useState(false);
  const [formatProgress, setFormatProgress] =
    useState<TranscriptFormatProgress | null>(null);
  const [pendingLongFormat, setPendingLongFormat] = useState<{
    chars: number;
    chunks: number;
  } | null>(null);
  const [canDiarize, setCanDiarize] = useState(false);
  const [transcribeProgress, setTranscribeProgress] =
    useState<TranscribeProgress | null>(null);

  // 只有内置本地引擎支持分离；不支持时不摆入口，而不是摆一个点了必然报错的按钮
  useEffect(() => {
    let cancelled = false;
    void window.api.media
      .capabilities()
      .then((capabilities) => {
        if (!cancelled) {
          setCanDiarize(capabilities.diarization);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setCanDiarize(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const itemId = item.id;
  useEffect(() => {
    setRunningAction(null);
    setIsFormatting(false);
    setFormatProgress(null);
    setPendingLongFormat(null);
    setTranscribeProgress(null);
  }, [itemId]);

  // 排版按块串行请求，长稿可达数十块；不上报进度的话按钮会静默转好几分钟
  useEffect(() => {
    const handleProgress = (payload: TranscriptFormatProgress) => {
      if (payload.itemId === itemId) {
        setFormatProgress(payload);
      }
    };
    window.api.on?.(IPC_CHANNELS.MEDIA_FORMAT_PROGRESS, handleProgress);
    return () => {
      window.api.off?.(IPC_CHANNELS.MEDIA_FORMAT_PROGRESS, handleProgress);
    };
  }, [itemId]);

  // 转写是个不返回中间结果的长请求，20 分钟的访谈要跑好几分钟；
  // 不报已用时长的话，按钮转圈和卡死在界面上长得一模一样
  useEffect(() => {
    const handleProgress = (payload: TranscribeProgress) => {
      if (payload.itemId === itemId) {
        setTranscribeProgress(payload);
      }
    };
    window.api.on?.(IPC_CHANNELS.MEDIA_TRANSCRIBE_PROGRESS, handleProgress);
    return () => {
      window.api.off?.(IPC_CHANNELS.MEDIA_TRANSCRIBE_PROGRESS, handleProgress);
    };
  }, [itemId]);

  const hasLocalAsset =
    extractLocalAssetRef(item.content, "local-video") !== null;
  // 图文条目的来源链接同样是抖音，但它没有音轨，不能据此提供转写入口
  const isOnlineVideo =
    item.itemType !== "image" &&
    !hasLocalAsset &&
    detectVideoPlatform(item.sourceUri?.trim() ?? "") !== null;

  const transcribe = useCallback(
    async (options?: { diarize?: boolean }) => {
      if (isRunning) {
        return;
      }
      setRunningAction(options?.diarize === true ? "diarize" : "transcribe");
      setTranscribeProgress(null);
      try {
        // 主进程基于库中正文写回（清注记/总结/标题），先落盘本地编辑
        await flushPendingSave();
        const result = await window.api.media.transcribe(itemId, options);
        if (result.success && result.item) {
          applyServerItem(result.item);
          // 要了分离却只出来一个说话人时不报「已生成」了事
          if (result.warning) {
            showToast(
              t(
                "library.transcribeOneSpeaker",
                "文字稿已生成，但只识别出一个说话人",
              ),
              "warning",
              { detail: result.warning },
            );
          } else {
            showToast(t("library.transcribeDone", "文字稿已生成"), "success");
          }
        } else if (result.notConfigured) {
          showToast(
            t(
              "library.transcribeNotConfigured",
              "尚未配置语音转写模型（audioText 路由）",
            ),
            "error",
          );
          requestSettingsSection("ai");
        } else {
          showToast(
            t("library.transcribeFailed", "转写失败：{{message}}", {
              message: result.error ?? "",
            }),
            "error",
          );
        }
      } finally {
        setRunningAction(null);
        setTranscribeProgress(null);
      }
    },
    [
      isRunning,
      itemId,
      flushPendingSave,
      applyServerItem,
      showToast,
      requestSettingsSection,
      t,
    ],
  );

  const transcript = item.transcript?.trim() ?? "";

  const runFormat = useCallback(
    async (allowLong: boolean) => {
      setIsFormatting(true);
      setFormatProgress(null);
      try {
        const result = await window.api.media.formatTranscript(itemId, {
          allowLong,
        });
        if (result.success && result.item) {
          applyServerItem(result.item);
          // 只排了一部分也算成功（结果已落库），但不能弹绿色的「完成」
          if (result.warning) {
            showToast(
              t("library.transcriptFormatPartial", "文字稿只排版了一部分"),
              "warning",
              { detail: result.warning },
            );
          } else {
            showToast(
              t("library.transcriptFormatDone", "文字稿排版完成"),
              "success",
            );
          }
        } else if (result.notConfigured) {
          showToast(
            t(
              "library.transcriptFormatNotConfigured",
              "尚未配置可用的文本模型",
            ),
            "error",
          );
          requestSettingsSection("ai");
        } else {
          showToast(t("library.transcriptFormatFailed", "排版失败"), "error", {
            detail: result.error,
          });
        }
      } finally {
        setIsFormatting(false);
        setFormatProgress(null);
      }
    },
    [itemId, applyServerItem, showToast, requestSettingsSection, t],
  );

  /**
   * 已有文字稿的 AI 排版：补标点/分段，不重新转写。
   * 长稿先弹确认——几万字要拆成几十次串行请求，耗时数分钟且真实计费，
   * 不该在用户点一下图标按钮后就默默花掉。
   */
  const format = useCallback(async () => {
    if (isRunning || isFormatting) {
      return;
    }
    if (transcript.length > TRANSCRIPT_FORMAT_LONG_CHARS) {
      setPendingLongFormat({
        chars: transcript.length,
        chunks: Math.ceil(transcript.length / TRANSCRIPT_FORMAT_CHUNK_CHARS),
      });
      return;
    }
    await runFormat(false);
  }, [isRunning, isFormatting, transcript, runFormat]);

  const confirmLongFormat = useCallback(async () => {
    setPendingLongFormat(null);
    await runFormat(true);
  }, [runFormat]);

  const cancelLongFormat = useCallback(() => {
    setPendingLongFormat(null);
  }, []);

  return {
    canTranscribe: hasLocalAsset || isOnlineVideo,
    isOnlineVideo,
    transcript,
    isRunning,
    isFormatting,
    canDiarize,
    runningAction,
    transcribeProgress,
    formatProgress,
    pendingLongFormat,
    transcribe,
    format,
    confirmLongFormat,
    cancelLongFormat,
  };
}

export interface MediaSummaryAction {
  /** 有文字稿才能总结 */
  available: boolean;
  hasSummary: boolean;
  isRunning: boolean;
  label: string;
  summarize: () => Promise<void>;
}

/** 基于文字稿生成结构化总结，写入正文的总结小节。 */
export function useMediaSummaryAction(item: KnowledgeItem): MediaSummaryAction {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const flushPendingSave = useKnowledgeStore((state) => state.flushPendingSave);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [isRunning, setIsRunning] = useState(false);

  const itemId = item.id;
  useEffect(() => {
    setIsRunning(false);
  }, [itemId]);

  const summarize = useCallback(async () => {
    if (isRunning) {
      return;
    }
    setIsRunning(true);
    try {
      // 主进程基于库中正文生成并写回，先把未保存的本地编辑落盘
      await flushPendingSave();
      const result = await window.api.media.summarize(itemId);
      if (result.success && result.item) {
        applyServerItem(result.item);
        showToast(t("library.mediaSummaryDone", "总结已写入正文"), "success");
      } else if (result.notConfigured) {
        showToast(
          t("library.mediaSummaryNotConfigured", "尚未配置可用的文本模型"),
          "error",
        );
        requestSettingsSection("ai");
      } else {
        showToast(
          t("library.mediaSummaryFailed", "总结生成失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
    } finally {
      setIsRunning(false);
    }
  }, [
    isRunning,
    itemId,
    flushPendingSave,
    applyServerItem,
    showToast,
    requestSettingsSection,
    t,
  ]);

  const isForum = item.itemType === "forum";
  const isAudio = item.itemType === "audio";
  // 论坛条目的素材是正文里的逐楼回复，音视频的是文字稿
  const forumSections = isForum ? splitForumNoteSections(item.content) : null;
  const hasSummary = isForum
    ? Boolean(forumSections?.summary)
    : hasMediaSummarySection(item.content);

  const label = isForum
    ? hasSummary
      ? t("library.forumSummaryRegenerate", "重新生成讨论总结")
      : t("library.forumSummaryGenerate", "生成讨论总结")
    : hasSummary
      ? isAudio
        ? t("library.mediaSummaryRegenerateAudio", "重新生成音频总结")
        : t("library.mediaSummaryRegenerateVideo", "重新生成视频总结")
      : isAudio
        ? t("library.mediaSummaryGenerateAudio", "生成音频总结")
        : t("library.mediaSummaryGenerateVideo", "生成视频总结");

  return {
    available: isForum
      ? Boolean(forumSections?.replies)
      : Boolean(item.transcript?.trim()),
    hasSummary,
    isRunning,
    label,
    summarize,
  };
}

export interface ForumDiscussionRefreshAction {
  available: boolean;
  isRunning: boolean;
  label: string;
  refresh: () => Promise<void>;
}

/** 支持站点的讨论就是帖子楼层；刷新后直接替换正文中的讨论小节。 */
export function useForumDiscussionRefreshAction(
  item: KnowledgeItem,
): ForumDiscussionRefreshAction {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const flushPendingSave = useKnowledgeStore((state) => state.flushPendingSave);
  const [isRunning, setIsRunning] = useState(false);
  const itemId = item.id;

  useEffect(() => {
    setIsRunning(false);
  }, [itemId]);

  const target = item.sourceUri ? detectForumPlatform(item.sourceUri) : null;
  const available =
    item.itemType === "forum" &&
    (target?.platform === "linuxdo" ||
      target?.platform === "appinn" ||
      target?.platform === "twolibra");
  const hadSummary = Boolean(
    item.itemType === "forum"
      ? splitForumNoteSections(item.content).summary.trim()
      : "",
  );

  const refresh = useCallback(async () => {
    if (isRunning || !available) {
      return;
    }
    setIsRunning(true);
    try {
      await flushPendingSave();
      const result = await window.api.media.refreshForumDiscussion(itemId);
      if (result.success && result.item) {
        applyServerItem(result.item);
        showToast(
          hadSummary
            ? t(
                "library.forumDiscussionRefreshedSummaryStale",
                "讨论已刷新，原讨论总结已标记为过期",
              )
            : t("library.forumDiscussionRefreshed", "讨论已刷新"),
          "success",
        );
      } else {
        showToast(
          t(
            "library.forumDiscussionRefreshFailed",
            "刷新讨论失败：{{message}}",
            {
              message: result.error ?? "",
            },
          ),
          "error",
        );
      }
    } catch (error) {
      showToast(
        t("library.forumDiscussionRefreshFailed", "刷新讨论失败：{{message}}", {
          message: error instanceof Error ? error.message : String(error),
        }),
        "error",
      );
    } finally {
      setIsRunning(false);
    }
  }, [
    isRunning,
    available,
    flushPendingSave,
    itemId,
    applyServerItem,
    hadSummary,
    showToast,
    t,
  ]);

  return {
    available,
    isRunning,
    label: t("library.forumDiscussionRefresh", "刷新讨论"),
    refresh,
  };
}
