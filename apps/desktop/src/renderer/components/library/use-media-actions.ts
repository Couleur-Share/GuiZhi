import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
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
  transcribe: () => Promise<void>;
  format: () => Promise<void>;
}

/**
 * 音视频转写与文字稿 AI 排版。
 * 状态提到 hook 里，让面板头部的按钮和内容区共用同一份运行状态。
 */
export function useTranscriptActions(item: KnowledgeItem): TranscriptActions {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [isRunning, setIsRunning] = useState(false);
  const [isFormatting, setIsFormatting] = useState(false);

  const itemId = item.id;
  useEffect(() => {
    setIsRunning(false);
    setIsFormatting(false);
  }, [itemId]);

  const hasLocalAsset =
    extractLocalAssetRef(item.content, "local-video") !== null;
  // 图文条目的来源链接同样是抖音，但它没有音轨，不能据此提供转写入口
  const isOnlineVideo =
    item.itemType !== "image" &&
    !hasLocalAsset &&
    detectVideoPlatform(item.sourceUri?.trim() ?? "") !== null;

  const transcribe = useCallback(async () => {
    if (isRunning) {
      return;
    }
    setIsRunning(true);
    try {
      // 主进程基于库中正文写回（清注记/总结/标题），先落盘本地编辑
      await flushPendingSave();
      const result = await window.api.media.transcribe(itemId);
      if (result.success && result.item) {
        applyServerItem(result.item);
        showToast(t("library.transcribeDone", "文字稿已生成"), "success");
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

  // 已有文字稿的 AI 排版：补标点/分段，不重新转写
  const format = useCallback(async () => {
    if (isRunning || isFormatting) {
      return;
    }
    setIsFormatting(true);
    try {
      const result = await window.api.media.formatTranscript(itemId);
      if (result.success && result.item) {
        applyServerItem(result.item);
        showToast(
          t("library.transcriptFormatDone", "文字稿排版完成"),
          "success",
        );
      } else if (result.notConfigured) {
        showToast(
          t("library.transcriptFormatNotConfigured", "尚未配置可用的文本模型"),
          "error",
        );
        requestSettingsSection("ai");
      } else {
        showToast(
          t("library.transcriptFormatFailed", "排版失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
    } finally {
      setIsFormatting(false);
    }
  }, [
    isRunning,
    isFormatting,
    itemId,
    applyServerItem,
    showToast,
    requestSettingsSection,
    t,
  ]);

  return {
    canTranscribe: hasLocalAsset || isOnlineVideo,
    isOnlineVideo,
    transcript: item.transcript?.trim() ?? "",
    isRunning,
    isFormatting,
    transcribe,
    format,
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
export function useMediaSummaryAction(
  item: KnowledgeItem,
): MediaSummaryAction {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );
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
  const forumSections = isForum
    ? splitForumNoteSections(item.content)
    : null;
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
