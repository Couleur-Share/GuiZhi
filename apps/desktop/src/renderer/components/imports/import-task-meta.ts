import type { ImportStage, ImportTask } from "@guizhi/shared/types";
import type { PlatformParseErrorCode } from "@guizhi/shared/utils/platform-parse-error";
import { getPlatformParseCode, splitPlatformParseErrorMessage } from "@guizhi/shared/utils/platform-parse-error";
import { detectPlatformCapturePlatform } from "@guizhi/shared/utils/platform-capture";

const AUTHENTICATED_RETRY_CODES = new Set<PlatformParseErrorCode>([
  "guest_denied",
  "token_invalid",
  "structure_missing",
]);

export function getAuthenticatedRetryPlatform(task: ImportTask) {
  if (
    task.status !== "failed" ||
    task.sourceKind !== "url" ||
    (task.captureStrategy ?? "standard") !== "standard" ||
    !task.error ||
    !AUTHENTICATED_RETRY_CODES.has(getPlatformParseCode(task.error) as PlatformParseErrorCode)
  ) {
    return null;
  }
  return detectPlatformCapturePlatform(task.sourceInput);
}

/**
 * 子阶段文案。视频链路会跑元数据 → 下载 → 转码 → 转写 → 排版 → 总结六步，
 * 全程可达几十分钟，只显示「抓取中」用户无法判断是在推进还是卡死了。
 *
 * 这里刻意不给「第 N 步 / 共 M 步」：实际步数取决于运行时条件（没配转写模型
 * 的视频任务会直接跳过下载与转写四步），渲染进程无从预知，写死的分母只会骗人。
 * 判断卡死靠的是下面的已耗时与本阶段耗时。
 */
export const STAGE_LABELS: Record<
  ImportStage,
  { key: string; fallback: string }
> = {
  "web-preparing": { key: "imports.stageWebPreparing", fallback: "准备网页组件" },
  fetching: { key: "imports.stageFetching", fallback: "抓取中" },
  extracting: { key: "imports.stageExtracting", fallback: "解析中" },
  saving: { key: "imports.stageSaving", fallback: "入库中" },
  "video-metadata": {
    key: "imports.stageVideoMetadata",
    fallback: "解析视频信息",
  },
  "video-captions": {
    key: "imports.stageVideoCaptions",
    fallback: "获取平台字幕",
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
  "browser-capture": {
    key: "imports.stageBrowserCapture",
    fallback: "登录态采集",
  },
  comments: { key: "imports.stageComments", fallback: "采集热门评论" },
};

export function getStageLabel(stage: ImportStage | null | undefined): {
  key: string;
  fallback: string;
} {
  return stage
    ? (STAGE_LABELS[stage] ?? STAGE_LABELS.fetching)
    : STAGE_LABELS.fetching;
}

/**
 * 状态文案。`StatusBadge` 自己按状态挑徽标样式，用不上这张表；
 * 而详情弹窗与诊断文本只需要一个名字，两处共用同一份。
 */
export const STATUS_LABELS: Record<
  ImportTask["status"],
  { key: string; fallback: string }
> = {
  pending: { key: "imports.statusPending", fallback: "等待中" },
  processing: { key: "imports.statusProcessing", fallback: "处理中" },
  completed: { key: "imports.statusCompleted", fallback: "已完成" },
  duplicate: { key: "imports.statusDuplicate", fallback: "重复内容" },
  canceled: { key: "imports.statusCanceled", fallback: "已取消" },
  failed: { key: "imports.statusFailed", fallback: "失败" },
};

/** 单个阶段超过这个时长仍无进展，行内给出「本阶段已 x」提示 */
export const STALL_THRESHOLD_MS = 90_000;

/**
 * 处理中任务的**实际工作**时长，不含排队等待。
 *
 * 不能用 `now - createdAt`：那是入队时刻，而并发只有 2——一次丢进三十条链接，
 * 最后一条能在队列里躺一个钟头，按它算出来的「已用 1:00:00」说的是「你多久以前
 * 点了采集」，不是「这条跑了多久」。而这一行的全部用处就是判断它卡没卡死。
 * 它还会和终态那行「共 X」对不上：一完成数字反而缩水，两个口径自相矛盾。
 *
 * 等式是「已结算的各阶段之和 + 当前阶段的已用」。后半段取 `updatedAt` 成立，
 * 是因为处理期间每一次写库都带着阶段变更（`processTask` 里 8 处
 * `updateAndNotify` 的 patch 全含 `stage`），阶段快照与 `updatedAt` 由同一次
 * UPDATE 落下，当前阶段的起点就是它。
 */
export function resolveWorkElapsed(task: ImportTask, now: number): number {
  const stats = task.stageStats;
  if (!stats?.length) {
    // 加统计之前的老任务没有这一列，退回原口径——不准，但不比以前差
    return Math.max(0, now - task.createdAt);
  }
  const settled = stats.reduce((total, entry) => total + entry.ms, 0);
  return settled + Math.max(0, now - task.updatedAt);
}

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
export function needsCaptureToolSetup(
  error: string | null | undefined,
): boolean {
  if (!error) {
    return false;
  }
  return (
    /yt-dlp|ffmpeg/i.test(error) && /未安装|not installed|尚未安装/i.test(error)
  );
}

/** 与 i18n `imports.parseError.*` 对齐的中文兜底（测试 / 缺 key 时用） */
export const PLATFORM_PARSE_ERROR_LABELS: Record<
  PlatformParseErrorCode,
  string
> = {
  structure_missing: "平台页面结构可能已变更",
  note_unavailable: "内容不可用或已删除",
  token_invalid: "链接缺少访问令牌，请用分享面板复制完整链接",
  guest_denied: "需要登录后才能查看，暂不支持",
  network: "网络或站点暂时不可达",
};

type ErrorTranslate = (
  key: string,
  fallback: string,
  options?: Record<string, unknown>,
) => string;

/**
 * 把任务 `error` 里的 `[structure_missing]` 等前缀翻成可读标签。
 * 没有稳定码时原样返回；诊断报告另附错误码一行，见 formatImportTaskErrorForReport。
 */
export function formatImportTaskError(
  error: string | null | undefined,
  t: ErrorTranslate,
): string {
  if (!error) {
    return "";
  }
  const { code, body } = splitPlatformParseErrorMessage(error);
  if (!code) {
    return error;
  }
  const label = t(
    `imports.parseError.${code}`,
    PLATFORM_PARSE_ERROR_LABELS[code],
  );
  if (!body || body === error) {
    return label;
  }
  if (body.startsWith(label)) {
    return body;
  }
  return `${label}：${body}`;
}

/** 诊断文本用：可读文案 + 稳定错误码（排查要靠后者） */
export function formatImportTaskErrorForReport(
  error: string | null | undefined,
  t: ErrorTranslate,
): string {
  if (!error) {
    return "";
  }
  const { code } = splitPlatformParseErrorMessage(error);
  const display = formatImportTaskError(error, t);
  if (!code) {
    return display;
  }
  const codeLine = t("imports.parseErrorCode", "错误码：{{code}}", { code });
  return `${display}\n${codeLine}`;
}

/**
 * warning 代表条目已经入库、只有一部分内容没拿到。它也要像 task.error 一样
 * 翻译：老版本直接把 `HTTP 403` 放给用户，既看不出是权限、下架还是平台风控，
 * 也不知道下一步该做什么。原始状态码保留在说明里，方便复制诊断信息继续排查。
 */
export function formatImportTaskWarning(
  warning: string | null | undefined,
  t: ErrorTranslate,
): string {
  if (!warning) {
    return "";
  }
  if (!warning.startsWith("文字稿生成失败：")) {
    return warning;
  }
  if (/HTTP\s*403/.test(warning)) {
    return t(
      "imports.transcriptionFailure403",
      "文字稿生成失败：平台拒绝访问音频（HTTP 403）。视频可能仅限登录或私密可见，也可能是平台暂时限制解析；请确认链接能在未登录状态打开后稍后重试。",
    );
  }
  if (/HTTP\s*401/.test(warning)) {
    return t(
      "imports.transcriptionFailure401",
      "文字稿生成失败：平台要求登录才能访问音频（HTTP 401）。请确认视频为公开内容，或改用本地文件导入。",
    );
  }
  if (/HTTP\s*404/.test(warning)) {
    return t(
      "imports.transcriptionFailure404",
      "文字稿生成失败：音频资源已不可用（HTTP 404）。视频可能已删除、下架或链接已失效。",
    );
  }
  if (/HTTP\s*429/.test(warning)) {
    return t(
      "imports.transcriptionFailure429",
      "文字稿生成失败：平台暂时限制了音频访问（HTTP 429）。请稍后重试，避免连续重复提交。",
    );
  }
  return warning;
}
