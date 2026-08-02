/**
 * 在线视频采集：平台链接检测 → 元数据 / 音频获取 → 可选远程转写。
 *
 * 元数据与音轨默认由 yt-dlp 提供（外部工具：优先用设置里配置的路径，
 * 否则查 PATH；未安装时降级保存链接并附安装指引，任务可在安装后重试）。
 * 抖音与小红书例外，都不依赖 yt-dlp：抖音的接口要签名 cookie（走
 * `./douyin.ts` 的移动端分享页），小红书的主体内容是 yt-dlp 根本不出的
 * 图文笔记（走 `./xiaohongshu.ts` 的桌面版笔记页）。
 * 命令执行与两个平台的抓取都以接口注入，便于单测用假实现驱动。
 */
import { randomUUID } from "crypto";
import { spawn } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import {
  detectVideoPlatform,
  type VideoPlatform,
} from "@guizhi/shared/utils/video-platforms";
import type { ImportStage } from "@guizhi/shared/types";
import { extractUrlsFromText } from "@guizhi/shared/utils/url-text";
import { logAppError } from "../../diagnostic-log";
import type { ExtractedContent } from "./connectors";
import {
  douyinImageNoteSource,
  downloadDouyinMedia,
  fetchDouyinAweme,
  type DouyinAweme,
} from "./douyin";
import { buildImageNoteEntry, type ImageNoteDeps } from "./image-note-entry";
import {
  downloadXiaohongshuMedia,
  fetchXiaohongshuNote,
  xiaohongshuImageNoteSource,
  type XiaohongshuNote,
} from "./xiaohongshu";
import { prepareAudioForTranscription } from "../media/audio-preprocess";
import { resolveFfmpegExecutable } from "../media/ffmpeg-manager";
import { ensureLocalTranscriptionService } from "../media/funasr-service";
import {
  resolveTranscriptionConfig,
  transcribeMediaFile,
  type TranscriptionModelConfig,
} from "../media/transcribe";
import {
  extractGlossaryTerms,
  formatTranscript,
  resolveTranscriptFormatterConfig,
} from "../media/transcript-format";
import {
  downloadPlatformCaptions,
  type PlatformCaption,
  type PlatformCaptionSource,
} from "./video-captions";
import {
  generateMediaSummary,
  resolveMediaSummaryConfig,
} from "../media/media-summary";
import { resolveYtDlpExecutable } from "../media/ytdlp-manager";
import {
  appendOriginalTitleNote,
  mediaSummaryHeading,
  upsertMediaSummarySection,
} from "@guizhi/shared/utils/media-summary";
import type { AIClientConfig } from "@guizhi/core";

// 平台识别移至 @guizhi/shared（渲染进程转写卡片共用），此处透传给既有调用方
export { detectVideoPlatform, type VideoPlatform };

const METADATA_TIMEOUT_MS = 90 * 1000;
const AUDIO_DOWNLOAD_TIMEOUT_MS = 15 * 60 * 1000;
const AUDIO_MAX_FILESIZE = "300m";
/** 简介压缩为元数据引用块单行展示，取前 300 字足够辨识 */
const DESCRIPTION_MAX_LENGTH = 300;
/** 简介里的链接最多列几条：抖音的推广文案能堆十几个短链，全列进去反成噪音 */
const LINK_MAX_COUNT = 5;
const OUTPUT_MAX_BYTES = 4 * 1024 * 1024;

const PLATFORM_LABELS: Record<VideoPlatform, string> = {
  bilibili: "哔哩哔哩",
  youtube: "YouTube",
  douyin: "抖音",
  xiaohongshu: "小红书",
};

export class YtDlpNotFoundError extends Error {
  constructor() {
    super("YT_DLP_NOT_FOUND");
    this.name = "YtDlpNotFoundError";
  }
}

export interface RunCommandResult {
  stdout: string;
}

export type RunCommand = (
  executable: string,
  args: string[],
  options: { timeoutMs: number; signal?: AbortSignal },
) => Promise<RunCommandResult>;

/** 默认命令执行器：spawn + 超时/取消 kill；找不到可执行文件抛 YtDlpNotFoundError */
export const runCommand: RunCommand = (executable, args, options) => {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, { windowsHide: true });
    let stdout = "";
    let stderr = "";
    let settled = false;

    const finish = (action: () => void) => {
      if (!settled) {
        settled = true;
        cleanup();
        action();
      }
    };

    const timeout = setTimeout(() => {
      child.kill();
      finish(() => reject(new Error("yt-dlp 执行超时")));
    }, options.timeoutMs);

    const abort = () => {
      child.kill();
      finish(() => reject(new Error("已取消")));
    };
    options.signal?.addEventListener("abort", abort, { once: true });

    const cleanup = () => {
      clearTimeout(timeout);
      options.signal?.removeEventListener("abort", abort);
    };

    child.stdout.on("data", (chunk: Buffer) => {
      if (stdout.length < OUTPUT_MAX_BYTES) {
        stdout += chunk.toString("utf8");
      }
    });
    child.stderr.on("data", (chunk: Buffer) => {
      if (stderr.length < OUTPUT_MAX_BYTES) {
        stderr += chunk.toString("utf8");
      }
    });

    child.on("error", (error: NodeJS.ErrnoException) => {
      finish(() =>
        reject(error.code === "ENOENT" ? new YtDlpNotFoundError() : error),
      );
    });

    child.on("close", (code) => {
      finish(() => {
        if (code === 0) {
          resolve({ stdout });
        } else {
          const detail = stderr.trim().split(/\r?\n/).slice(-3).join(" ");
          reject(new Error(`yt-dlp 退出码 ${code}: ${detail.slice(0, 300)}`));
        }
      });
    });
  });
};

export interface YtDlpMetadata {
  title: string;
  uploader: string;
  durationSeconds: number | null;
  description: string;
  webpageUrl: string;
}

/** 解析 --dump-json 输出（单行 JSON；容忍前后噪声行） */
export function parseYtDlpMetadata(stdout: string): YtDlpMetadata {
  const lines = stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("{"));
  const jsonLine = lines[lines.length - 1];
  if (!jsonLine) {
    throw new Error("yt-dlp 未返回视频元数据");
  }
  const parsed = JSON.parse(jsonLine) as Record<string, unknown>;
  return {
    title: typeof parsed.title === "string" ? parsed.title : "",
    uploader:
      typeof parsed.uploader === "string"
        ? parsed.uploader
        : typeof parsed.channel === "string"
          ? parsed.channel
          : "",
    durationSeconds:
      typeof parsed.duration === "number" && Number.isFinite(parsed.duration)
        ? Math.round(parsed.duration)
        : null,
    description:
      typeof parsed.description === "string" ? parsed.description : "",
    webpageUrl:
      typeof parsed.webpage_url === "string" && parsed.webpage_url
        ? parsed.webpage_url
        : "",
  };
}

export function formatDuration(totalSeconds: number): string {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (value: number) => String(value).padStart(2, "0");
  return hours > 0
    ? `${hours}:${pad(minutes)}:${pad(seconds)}`
    : `${minutes}:${pad(seconds)}`;
}

export interface VideoUrlDeps {
  /** 设置里配置的 yt-dlp 路径（空表示查 PATH） */
  getYtDlpPath: () => string | null;
  /** 设置里配置的 ffmpeg 路径（空表示托管版 / PATH），用于转写前音频转码 */
  getFfmpegPath?: () => string | null;
  run?: RunCommand;
  /** 测试注入：抖音分享页解析（默认走 douyin.ts） */
  fetchDouyin?: (url: string, signal?: AbortSignal) => Promise<DouyinAweme>;
  /** 测试注入：抖音无水印视频下载 */
  downloadDouyin?: (
    playUrl: string,
    signal?: AbortSignal,
  ) => Promise<{ dir: string; filePath: string }>;
  /** 测试注入：小红书笔记页解析（默认走 xiaohongshu.ts） */
  fetchXiaohongshu?: (
    url: string,
    signal?: AbortSignal,
  ) => Promise<XiaohongshuNote>;
  /** 测试注入：小红书视频下载（主源挂了逐个降级到备源） */
  downloadXiaohongshu?: (
    playUrls: string[],
    signal?: AbortSignal,
  ) => Promise<{ dir: string; filePath: string }>;
  /** 测试注入：图文作品的配图下载与 OCR（抖音 / 小红书共用） */
  imageNote?: Omit<ImageNoteDeps, "onStage">;
  /** 测试注入：转写配置解析（默认读 ai-config.json 的 audioText 路由） */
  getTranscriptionConfig?: () => TranscriptionModelConfig | null;
  /** 导入时是否区分说话人（读设置，默认关） */
  getDiarize?: () => boolean;
  /** 测试注入：转写执行 */
  transcribe?: (
    filePath: string,
    config: TranscriptionModelConfig,
    signal?: AbortSignal,
    options?: { diarize?: boolean },
  ) => Promise<string>;
  /** 测试注入：平台字幕获取（发布者字幕优先，其次平台自动字幕） */
  getPlatformCaptions?: (
    signal?: AbortSignal,
  ) => Promise<PlatformCaption | null>;
  /** 测试注入：音频预处理（默认 ffmpeg 转码 16kHz 单声道 mp3） */
  prepareAudio?: typeof prepareAudioForTranscription;
  /** 测试注入：排版模型解析（默认读 ai-config.json 的 fastText 路由） */
  getFormatterConfig?: () => AIClientConfig | null;
  /** 测试注入：文字稿 AI 排版 */
  formatTranscript?: typeof formatTranscript;
  /** 测试注入：总结模型解析（默认读 ai-config.json 的 mainText 路由） */
  getSummaryConfig?: () => AIClientConfig | null;
  /** 测试注入：视频总结生成 */
  summarize?: typeof generateMediaSummary;
  /** 上报当前子阶段（元数据 / 下载 / 转码 / 转写 / 排版 / 总结） */
  onStage?: (stage: ImportStage) => void;
}

/**
 * 组装视频条目正文：只写元数据引用块（平台/作者/时长/简介）与可选的转写注记。
 * 简介压缩为引用块内的单行（MediaMetaCard 展示，正文视图不再出现）；
 * 来源链接不写入正文——sourceUri 字段是数据载体，元数据卡片提供跳转。
 */
export function buildVideoContent(
  metadata: YtDlpMetadata,
  platform: VideoPlatform,
  transcriptionNote?: string,
  transcriptionSource?: string,
): string {
  const metaLine = [
    `平台：${PLATFORM_LABELS[platform]}`,
    metadata.uploader ? `作者：${metadata.uploader}` : "",
    metadata.durationSeconds != null
      ? `时长：${formatDuration(metadata.durationSeconds)}`
      : "",
  ]
    .filter(Boolean)
    .join(" · ");

  const quoteLines = [`> ${metaLine}`];
  const description = metadata.description.trim().replace(/\s+/g, " ");
  if (description) {
    const clipped =
      description.length > DESCRIPTION_MAX_LENGTH
        ? `${description.slice(0, DESCRIPTION_MAX_LENGTH)}…`
        : description;
    quoteLines.push(`> 简介：${clipped}`);
  }
  // 链接从**未截断**的简介里抠，单独占一行。
  //
  // 简介被压成单行并截到 300 字，作者放在末尾的仓库地址、文档链接就这么没了
  // ——而那恰恰是「这条视频能不能直接用上」最关键的东西。实测一条讲 Agent 的
  // 视频，作者反复说「源码和配套文档都在下面」，记录里一个链接都没有。
  const links = extractUrlsFromText(metadata.description).slice(
    0,
    LINK_MAX_COUNT,
  );
  if (links.length > 0) {
    quoteLines.push(`> 相关链接：${links.join(" ")}`);
  }
  if (transcriptionSource) {
    quoteLines.push(`> 文字稿来源：${transcriptionSource}`);
  }

  const parts: string[] = [quoteLines.join("\n")];
  if (transcriptionNote) {
    parts.push(`> ${transcriptionNote}`);
  }
  return parts.join("\n\n");
}

/** 转写状态注记的段落前缀（buildVideoContent 写入正文的两种模板） */
const TRANSCRIPTION_NOTE_PREFIXES = [
  "> 文字稿生成失败",
  "> 未配置「语音转写」模型",
  "> 文字稿来源：",
];

/** 重新生成文字稿成功后，从正文中移除历史转写状态注记 */
export function stripTranscriptionNote(content: string): string {
  return content
    .split("\n\n")
    .map((paragraph) =>
      paragraph
        .split("\n")
        .filter(
          (line) =>
            !TRANSCRIPTION_NOTE_PREFIXES.some((prefix) =>
              line.startsWith(prefix),
            ),
        )
        .join("\n"),
    )
    .filter(Boolean)
    .join("\n\n");
}

/**
 * 重转录会替换文字稿，来源也必须同步更新；否则「平台字幕」被手动 ASR 覆盖后，
 * 元数据还显示旧来源，反而比不展示更误导。来源行固定放在连续元数据引用块内，
 * 这样详情、导出与全文检索都读得到。
 */
export function upsertTranscriptionSourceNote(
  content: string,
  source: string,
): string {
  const lines = stripTranscriptionNote(content).split("\n");
  if (!/^>\s*平台[:：]/.test(lines[0] ?? "")) {
    return content;
  }
  let end = 0;
  while (end < lines.length && lines[end].startsWith(">")) {
    end++;
  }
  lines.splice(end, 0, `> 文字稿来源：${source}`);
  return lines.join("\n");
}

function buildNotInstalledReason(platform: VideoPlatform): string {
  return (
    `检测到${PLATFORM_LABELS[platform]}视频链接，但尚未安装 yt-dlp，无法解析视频信息。` +
    "打开「设置 → 应用设置 → 采集」一键安装后，回到本列表点击「重试」。"
  );
}

/** 下载最佳音轨到临时目录（不依赖 ffmpeg），返回音频文件路径 */
export async function downloadBestAudio(
  executable: string,
  url: string,
  run: RunCommand,
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  const dir = path.join(
    os.tmpdir(),
    `guizhi-video-${randomUUID().slice(0, 8)}`,
  );
  fs.mkdirSync(dir, { recursive: true });
  await run(
    executable,
    [
      "--no-warnings",
      "--no-playlist",
      "--max-filesize",
      AUDIO_MAX_FILESIZE,
      "-f",
      "bestaudio/best",
      "-o",
      path.join(dir, "audio.%(ext)s"),
      url,
    ],
    { timeoutMs: AUDIO_DOWNLOAD_TIMEOUT_MS, signal },
  );
  const files = fs.readdirSync(dir);
  if (files.length === 0) {
    throw new Error("音频下载失败（可能超过大小限制）");
  }
  return { dir, filePath: path.join(dir, files[0]) };
}

/** 平台差异收敛点：元数据从哪来、音轨怎么下 */
interface MediaSource {
  metadata: YtDlpMetadata;
  /** 下载可供转码转写的音轨 / 视频到临时目录 */
  downloadAudio: (
    signal?: AbortSignal,
  ) => Promise<{ dir: string; filePath: string }>;
  /** 可选的平台字幕；只有 yt-dlp 平台实现，失败时由调用方转 ASR */
  downloadCaptions?: (signal?: AbortSignal) => Promise<PlatformCaption | null>;
  /** 图文作品（抖音 / 小红书）：没有音轨，直接给出成品笔记条目 */
  note?: ExtractedContent;
}

async function resolveDouyinSource(
  url: string,
  deps: VideoUrlDeps,
  signal?: AbortSignal,
): Promise<MediaSource> {
  const aweme = await (deps.fetchDouyin ?? fetchDouyinAweme)(url, signal);
  const metadata: YtDlpMetadata = {
    title: aweme.title,
    uploader: aweme.author,
    durationSeconds: aweme.durationSeconds,
    description: aweme.description,
    webpageUrl: aweme.webpageUrl,
  };

  if (aweme.kind === "note") {
    return {
      metadata,
      downloadAudio: () => Promise.reject(new Error("图文作品没有音轨")),
      note: await buildImageNoteEntry(
        douyinImageNoteSource(aweme),
        { ...deps.imageNote, onStage: deps.onStage },
        signal,
      ),
    };
  }

  const playUrl = aweme.playUrl;
  if (!playUrl) {
    throw new Error("未能取到视频播放地址（页面结构可能已变化）");
  }

  const download = deps.downloadDouyin ?? downloadDouyinMedia;
  return {
    metadata,
    downloadAudio: (downloadSignal) => download(playUrl, downloadSignal),
  };
}

async function resolveXiaohongshuSource(
  url: string,
  deps: VideoUrlDeps,
  signal?: AbortSignal,
): Promise<MediaSource> {
  const note = await (deps.fetchXiaohongshu ?? fetchXiaohongshuNote)(
    url,
    signal,
  );
  const metadata: YtDlpMetadata = {
    title: note.title,
    uploader: note.author,
    durationSeconds: note.durationSeconds,
    description: note.description,
    webpageUrl: note.webpageUrl,
  };

  if (note.kind === "note") {
    return {
      metadata,
      downloadAudio: () => Promise.reject(new Error("图文笔记没有音轨")),
      note: await buildImageNoteEntry(
        xiaohongshuImageNoteSource(note),
        { ...deps.imageNote, onStage: deps.onStage },
        signal,
      ),
    };
  }

  const playUrls = note.playUrls;
  if (playUrls.length === 0) {
    throw new Error("未能取到视频播放地址（页面结构可能已变化）");
  }

  const download = deps.downloadXiaohongshu ?? downloadXiaohongshuMedia;
  return {
    metadata,
    downloadAudio: (downloadSignal) => download(playUrls, downloadSignal),
  };
}

async function resolveYtDlpSource(
  url: string,
  deps: VideoUrlDeps,
  run: RunCommand,
  signal?: AbortSignal,
): Promise<MediaSource> {
  const executable = resolveYtDlpExecutable(deps.getYtDlpPath());
  const result = await run(
    executable,
    ["--dump-json", "--no-download", "--no-warnings", "--no-playlist", url],
    { timeoutMs: METADATA_TIMEOUT_MS, signal },
  );
  return {
    metadata: parseYtDlpMetadata(result.stdout),
    downloadAudio: (downloadSignal) =>
      downloadBestAudio(executable, url, run, downloadSignal),
    downloadCaptions: (downloadSignal) =>
      downloadPlatformCaptions(executable, url, run, downloadSignal),
  };
}

function resolveMediaSource(
  url: string,
  platform: VideoPlatform,
  deps: VideoUrlDeps,
  run: RunCommand,
  signal?: AbortSignal,
): Promise<MediaSource> {
  switch (platform) {
    case "douyin":
      return resolveDouyinSource(url, deps, signal);
    case "xiaohongshu":
      return resolveXiaohongshuSource(url, deps, signal);
    default:
      return resolveYtDlpSource(url, deps, run, signal);
  }
}

export async function extractVideoUrl(
  url: string,
  platform: VideoPlatform,
  deps: VideoUrlDeps,
  signal?: AbortSignal,
): Promise<ExtractedContent> {
  const run = deps.run ?? runCommand;

  let source: MediaSource;
  deps.onStage?.("video-metadata");
  try {
    source = await resolveMediaSource(url, platform, deps, run, signal);
  } catch (error) {
    if (error instanceof Error && error.message === "已取消") {
      throw error;
    }
    if (error instanceof YtDlpNotFoundError) {
      return {
        title: url,
        content: "",
        itemType: "video",
        sourceUri: null,
        degradedReason: buildNotInstalledReason(platform),
      };
    }
    const message = error instanceof Error ? error.message : String(error);
    return {
      title: url,
      content: "",
      itemType: "video",
      sourceUri: null,
      degradedReason: `视频信息解析失败：${message}`,
    };
  }

  if (source.note) {
    return source.note;
  }
  const metadata = source.metadata;

  // 先试平台字幕。它已带时间轴与原始标点，且无需下载音频或耗费转写额度；
  // 无字幕 / 自动字幕取不到才进入 ASR。抖音、小红书自建连接器暂时没有这一层，
  // 会自然落到既有 ASR 路径。
  let transcript: string | null = null;
  let transcriptionNote: string | undefined;
  let transcriptionSource: PlatformCaptionSource | "asr" | null = null;
  let captionLanguage: string | undefined;
  const getPlatformCaptions =
    deps.getPlatformCaptions ?? source.downloadCaptions;
  if (getPlatformCaptions) {
    deps.onStage?.("video-captions");
    try {
      const captions = await getPlatformCaptions(signal);
      if (captions) {
        transcript = captions.text;
        transcriptionSource = captions.source;
        captionLanguage = captions.language;
      }
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      // 字幕是优化路径，不能因为平台接口波动让整条视频连 ASR 都走不了。
      console.warn("[import] 平台字幕获取失败，改用语音转写:", error);
    }
  }

  const transcriptionConfig = (
    deps.getTranscriptionConfig ?? resolveTranscriptionConfig
  )();
  if (!transcript && transcriptionConfig) {
    const transcribe = deps.transcribe ?? transcribeMediaFile;
    const prepareAudio = deps.prepareAudio ?? prepareAudioForTranscription;
    let tempDir: string | null = null;
    let prepared: { filePath: string; cleanup: () => void } | null = null;
    try {
      // 目标是托管本地引擎时先确保服务已启动
      await ensureLocalTranscriptionService(transcriptionConfig.apiUrl);
      deps.onStage?.("video-audio");
      const audio = await source.downloadAudio(signal);
      tempDir = audio.dir;
      deps.onStage?.("transcoding");
      prepared = await prepareAudio(
        audio.filePath,
        resolveFfmpegExecutable(deps.getFfmpegPath?.() ?? null),
        signal,
      );
      deps.onStage?.("transcribing");
      transcript = await transcribe(
        prepared.filePath,
        transcriptionConfig,
        signal,
        // 目标不是内置引擎时 transcribe 内部会忽略这个字段，不必在这里判
        { diarize: deps.getDiarize?.() === true },
      );
      transcriptionSource = "asr";
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      transcriptionNote = `文字稿生成失败：${error instanceof Error ? error.message : String(error)}`;
      // 导入是后台流程不弹提示，但转写失败会掏空整条条目——不留痕的话，
      // 用户报「怎么没有文字稿」时双方都拿不出任何可查的东西
      console.warn("[import] 语音转写失败:", error);
      logAppError({
        scope: "import",
        action: "语音转写",
        message: transcriptionNote,
        url,
      });
    } finally {
      prepared?.cleanup();
      if (tempDir) {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    }

    // ASR 输出才需要补标点与分段；平台字幕本身已有发布者/平台给出的结构，
    // 再让模型“排版”反而可能改坏原句与时间语义。
    if (transcript && transcriptionSource === "asr") {
      const formatterConfig = (
        deps.getFormatterConfig ?? resolveTranscriptFormatterConfig
      )();
      if (formatterConfig) {
        deps.onStage?.("formatting");
        try {
          const result = await (deps.formatTranscript ?? formatTranscript)(
            transcript,
            formatterConfig,
            {
              signal,
              glossary: extractGlossaryTerms(
                metadata.title,
                metadata.description,
              ),
            },
          );
          transcript = result.text;
          // 导入是后台流程，不弹提示；但跳过/半途而废要留痕，
          // 否则用户只看到一坨没分段的文字，报上来时双方都拿不出数
          const note = result.skippedReason
            ? `跳过排版：${result.skippedReason}`
            : result.partialReason
              ? `部分排版：${result.partialReason}`
              : null;
          if (note) {
            console.warn(`[import] ${note}`);
            logAppError({
              scope: "import",
              action: "文字稿排版",
              message: note,
              url,
            });
          }
        } catch (error) {
          if (signal?.aborted) {
            throw new Error("已取消", { cause: error });
          }
          console.warn("[import] 文字稿 AI 排版失败，保留原始转写:", error);
          logAppError({
            scope: "import",
            action: "文字稿排版",
            message: `排版失败，保留原始转写：${
              error instanceof Error ? error.message : String(error)
            }`,
            url,
          });
        }
      }
    }
  } else if (!transcript) {
    transcriptionNote =
      "未配置「语音转写」模型，本次仅保存视频信息；配置后可在任务列表重试生成文字稿。";
  }

  // 有文字稿则生成结构化视频总结与 AI 标题；失败只保留文字稿，不阻断导入
  let summary: string | null = null;
  let aiTitle: string | null = null;
  if (transcript) {
    const summaryConfig = (
      deps.getSummaryConfig ?? resolveMediaSummaryConfig
    )();
    if (summaryConfig) {
      deps.onStage?.("summarizing");
      try {
        const result = await (deps.summarize ?? generateMediaSummary)(
          {
            title: metadata.title || url,
            context: metadata.description,
            transcript,
          },
          summaryConfig,
          { signal },
        );
        summary = result.summary;
        aiTitle = result.title;
      } catch (error) {
        if (signal?.aborted) {
          throw new Error("已取消", { cause: error });
        }
        console.warn("[import] 视频总结生成失败，保留文字稿:", error);
      }
    }
  }

  let title = metadata.title || url;
  const transcriptionSourceNote =
    transcriptionSource === "platform-subtitles"
      ? `发布者字幕${captionLanguage ? `（${captionLanguage}）` : ""}`
      : transcriptionSource === "platform-ai-captions"
        ? `平台 AI 字幕${captionLanguage ? `（${captionLanguage}）` : ""}`
        : transcriptionSource === "asr"
          ? `音频识别（${transcriptionConfig?.model ?? "已配置模型"}）`
          : undefined;
  let content = buildVideoContent(
    metadata,
    platform,
    transcriptionNote,
    transcriptionSourceNote,
  );
  if (summary) {
    content = upsertMediaSummarySection(
      content,
      mediaSummaryHeading("video"),
      summary,
    );
  }
  // AI 标题替换平台原标题，原标题记入元数据引用块（仍可检索）
  if (aiTitle && aiTitle !== title) {
    if (metadata.title) {
      content = appendOriginalTitleNote(content, metadata.title);
    }
    title = aiTitle;
  }

  return {
    title,
    content,
    itemType: "video",
    sourceUri: metadata.webpageUrl || url,
    transcript,
    // 正文里的那行注记同时上报到任务上，列表才不会只给一枚绿色的「已完成」
    warningReason: transcriptionNote,
  };
}
