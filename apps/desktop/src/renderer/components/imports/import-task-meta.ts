import type { ImportStage, ImportTask } from "@guizhi/shared/types";

/**
 * 子阶段文案。视频链路会跑元数据 → 下载 → 转码 → 转写 → 排版 → 总结六步，
 * 全程可达几十分钟，只显示「抓取中」用户无法判断是在推进还是卡死了。
 *
 * 这里刻意不给「第 N 步 / 共 M 步」：实际步数取决于运行时条件（没配转写模型
 * 的视频任务会直接跳过下载与转写四步），渲染进程无从预知，写死的分母只会骗人。
 * 判断卡死靠的是下面的已耗时与本阶段耗时。
 */
export const STAGE_LABELS: Record<ImportStage, { key: string; fallback: string }> =
  {
    fetching: { key: "imports.stageFetching", fallback: "抓取中" },
    extracting: { key: "imports.stageExtracting", fallback: "解析中" },
    saving: { key: "imports.stageSaving", fallback: "入库中" },
    "video-metadata": {
      key: "imports.stageVideoMetadata",
      fallback: "解析视频信息",
    },
    "video-audio": { key: "imports.stageVideoAudio", fallback: "下载音轨" },
    transcoding: { key: "imports.stageTranscoding", fallback: "音频转码" },
    transcribing: { key: "imports.stageTranscribing", fallback: "语音转写" },
    formatting: { key: "imports.stageFormatting", fallback: "文字稿排版" },
    summarizing: { key: "imports.stageSummarizing", fallback: "生成总结" },
    "image-download": {
      key: "imports.stageImageDownload",
      fallback: "下载配图",
    },
    "image-ocr": { key: "imports.stageImageOcr", fallback: "识别图中文字" },
    "forum-replies": {
      key: "imports.stageForumReplies",
      fallback: "整理讨论区",
    },
  };

export function getStageLabel(stage: ImportStage | null | undefined): {
  key: string;
  fallback: string;
} {
  return stage ? (STAGE_LABELS[stage] ?? STAGE_LABELS.fetching) : STAGE_LABELS.fetching;
}

/** 单个阶段超过这个时长仍无进展，行内给出「本阶段已 x」提示 */
export const STALL_THRESHOLD_MS = 90_000;

/**
 * 耗时格式化为 `M:SS` / `H:MM:SS`。
 * 走时钟写法而不是「3 分 7 秒」，是为了免掉一套单复数与中英差异的文案。
 */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const seconds = total % 60;
  const minutes = Math.floor(total / 60) % 60;
  const hours = Math.floor(total / 3600);
  const pad = (value: number) => String(value).padStart(2, "0");
  return hours > 0
    ? `${hours}:${pad(minutes)}:${pad(seconds)}`
    : `${minutes}:${pad(seconds)}`;
}

/**
 * 任务来源域名（去掉 www. 前缀）。
 * 一列 v2ex / 抖音链接混在一起时，这是最省版面的辨识标记。
 */
export function resolveTaskHost(task: ImportTask): string | null {
  if (task.sourceKind !== "url") {
    return null;
  }
  try {
    const { hostname } = new URL(task.sourceInput);
    return hostname.replace(/^www\./, "") || null;
  } catch {
    return null;
  }
}

/** 文件任务的所在目录，作为副标题（列表里只显示文件名，路径靠这里补） */
export function resolveTaskFolder(task: ImportTask): string | null {
  if (task.sourceKind !== "file") {
    return null;
  }
  const segments = task.sourceInput.split(/[\\/]/);
  segments.pop();
  return segments.length > 0 ? segments.join("/") : null;
}

/**
 * 失败原因是否指向未安装的外部工具。
 *
 * 这类失败靠重试永远好不了，得先去设置页装工具；行内因此多给一个直达按钮。
 */
export function needsCaptureToolSetup(error: string | null | undefined): boolean {
  if (!error) {
    return false;
  }
  return /yt-dlp|ffmpeg/i.test(error) && /未安装|not installed|尚未安装/i.test(error);
}
