/**
 * 媒体增强 IPC：音视频条目的远程转写（本地媒体资产 + 在线视频重新生成）
 * 与 yt-dlp / ffmpeg 工具管理。
 */
import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  type IpcMainInvokeEvent,
} from "electron";
import fs from "fs";
import path from "path";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type {
  FfmpegInstallResult,
  FfmpegStatus,
  FunasrInstallResult,
  FunasrOperationResult,
  FunasrStatus,
  KnowledgeItem,
  MediaCapabilities,
  ToolUpdateCheck,
  TranscribeProgress,
  TranscribeStage,
  TranscriptFormatProgress,
  YtDlpInstallResult,
  YtDlpStatus,
} from "@guizhi/shared/types";
import {
  parseForumReplies,
  replaceForumRepliesSection,
  splitForumNoteSections,
  upsertForumSummarySection,
} from "@guizhi/shared/utils/forum-note";
import { extractLocalAssetRef } from "@guizhi/shared/utils/media-refs";
import {
  appendOriginalTitleNote,
  mediaSummaryHeading,
  upsertMediaSummarySection,
} from "@guizhi/shared/utils/media-summary";
import { listSpeakers } from "@guizhi/shared/utils/speaker-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import {
  detectForumPlatform,
  type ForumTarget,
} from "@guizhi/shared/utils/forum-platforms";
import { KnowledgeItemDB } from "@guizhi/db";
import Database from "../database/sqlite";
import { logAppError } from "../diagnostic-log";
import { getVideosDir } from "../runtime-paths";
import {
  readFfmpegPathSetting,
  readNetworkProxySetting,
  readYtDlpPathSetting,
} from "../services/import/import-service";
import { fetchAppinnThread } from "../services/import/appinn";
import { fetchLinuxdoThread } from "../services/import/linuxdo";
import { fetchTwolibraThread } from "../services/import/twolibra";
import { fetchAuthenticatedLinuxdoJson } from "../services/platform-capture/authenticated-platforms";
import { getBrowserCaptureService } from "../services/platform-capture/browser-capture";
import {
  upsertTranscriptionSourceNote,
  YtDlpNotFoundError,
} from "../services/import/video-url";
import { downloadItemVideoAudio } from "../services/media/video-retranscription";
import { prepareAudioForTranscription } from "../services/media/audio-preprocess";
import { rememberPickedBinaryPath } from "../services/picked-binary-paths";
import { createStatusCache } from "../services/media/engine-status-cache";
import { withCachedVersion } from "../services/media/engine-version-store";
import {
  checkFfmpegUpdate,
  getFfmpegStatus,
  installFfmpeg,
  probeFfmpegVersion,
  removeManagedFfmpeg,
  resolveFfmpegExecutable,
} from "../services/media/ffmpeg-manager";
import {
  getFunasrStatus,
  installFunasr,
  uninstallFunasr,
} from "../services/media/funasr-manager";
import {
  ensureLocalTranscriptionService,
  getTranscriptionActivityAt,
  stopFunasrService,
} from "../services/media/funasr-service";
import {
  resolveTranscriptionConfig,
  supportsDiarization,
  testTranscriptionConfig,
  transcribeMediaFile,
  type TranscriptionModelConfig,
} from "../services/media/transcribe";
import {
  generateMediaSummary,
  resolveMediaSummaryConfig,
} from "../services/media/media-summary";
import { generateForumSummary } from "../services/import/forum-summary";
import {
  extractGlossaryTerms,
  formatTranscript,
  resolveTranscriptFormatterConfig,
} from "../services/media/transcript-format";
import {
  checkYtDlpUpdate,
  getYtDlpStatus,
  installYtDlp,
  probeYtDlpVersion,
  removeManagedYtDlp,
} from "../services/media/ytdlp-manager";

export interface MediaTranscribeResult {
  success: boolean;
  /** 未配置 audioText 转写模型（UI 引导去设置） */
  notConfigured?: boolean;
  error?: string;
  /** 成功但打了折扣（如只排版了一部分），UI 用 warning 提示而非绿色的「完成」 */
  warning?: string;
  item?: KnowledgeItem;
}

export interface TranscriptionTestResult {
  success: boolean;
  latency?: number;
  error?: string;
}

/** 条目侧的专名表：标题 + 元数据引用块里的平台简介 */
function resolveItemGlossary(item: KnowledgeItem): string[] {
  return extractGlossaryTerms(
    item.title,
    parseVideoMetaBlock(item.content)?.description,
  );
}

/**
 * 转写成功后顺带 AI 排版（补标点/分段）；失败或未配置文本模型时保留原始转写。
 *
 * 这是后台自动执行的一步，不弹提示，但失败与超长跳过都要写进 error.log：
 * 不然用户看到的只是一坨没分段的文字，报「排版怎么没生效」时双方都拿不出数。
 * 未配置文本模型是常态而非故障，不记。
 */
async function formatTranscriptSafely(
  raw: string,
  item: KnowledgeItem,
): Promise<string> {
  const itemId = item.id;
  const formatterConfig = resolveTranscriptFormatterConfig();
  if (!formatterConfig) {
    return raw;
  }
  try {
    const { text, skippedReason, partialReason } = await formatTranscript(
      raw,
      formatterConfig,
      { glossary: resolveItemGlossary(item) },
    );
    const note = skippedReason
      ? `跳过排版：${skippedReason}`
      : partialReason
        ? `部分排版：${partialReason}`
        : null;
    if (note) {
      console.warn(`[media] ${note}`);
      logAppError({
        scope: "media",
        action: "文字稿排版",
        message: note,
        itemId,
      });
    }
    return text;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.warn("[media] 文字稿 AI 排版失败，保留原始转写:", error);
    logAppError({
      scope: "media",
      action: "文字稿排版",
      message: `排版失败，保留原始转写：${message}`,
      itemId,
    });
    return raw;
  }
}

/**
 * 转写成功后顺带生成内容总结写入正文，并应用 AI 标题（原标题记入元数据引用块）；
 * 失败或未配置文本模型时跳过，只返回原正文。
 */
async function applyMediaSummarySafely(
  item: KnowledgeItem,
  content: string,
  transcript: string,
): Promise<{ content: string; title?: string }> {
  const summaryConfig = resolveMediaSummaryConfig();
  if (!summaryConfig) {
    return { content };
  }
  try {
    const { summary, title } = await generateMediaSummary(
      { title: item.title, transcript },
      summaryConfig,
    );
    console.log(
      `[media] 内容总结完成（item=${item.id}，${summary.length} 字）`,
    );
    let next = upsertMediaSummarySection(
      content,
      mediaSummaryHeading(item.itemType),
      summary,
    );
    if (title && title !== item.title) {
      next = appendOriginalTitleNote(next, item.title);
      return { content: next, title };
    }
    return { content: next };
  } catch (error) {
    console.warn("[media] 内容总结生成失败，保留文字稿:", error);
    return { content };
  }
}

/**
 * 重新生成论坛条目的讨论总结。
 *
 * 支持公开分页接口的站点会在生成前读取最新完整帖子，使总结覆盖主楼和全部楼层；
 * 其他论坛仍使用条目里已经入库的讨论内容。
 */
type RefreshableForumPlatform = "linuxdo" | "appinn" | "twolibra";

function isRefreshableForumTarget(
  target: ForumTarget | null,
): target is ForumTarget & { platform: RefreshableForumPlatform } {
  return (
    target?.platform === "linuxdo" ||
    target?.platform === "appinn" ||
    target?.platform === "twolibra"
  );
}

function refreshableForumLabel(platform: RefreshableForumPlatform): string {
  if (platform === "linuxdo") return "LINUX DO";
  return platform === "appinn" ? "小众软件论坛" : "2Libra";
}

async function fetchLatestForumThread(
  db: Database.Database,
  target: ForumTarget & { platform: RefreshableForumPlatform },
) {
  if (target.platform === "appinn") {
    return fetchAppinnThread(target.topicId);
  }
  if (target.platform === "twolibra") {
    return fetchTwolibraThread(target.topicId);
  }
  const browserCapture = getBrowserCaptureService({
    getNetworkProxy: () => readNetworkProxySetting(db),
  });
  return fetchLinuxdoThread(target.topicId, {
    fetchAuthenticatedJson: (url, signal) =>
      fetchAuthenticatedLinuxdoJson(browserCapture, url, signal),
  });
}

async function regenerateForumSummary(
  db: Database.Database,
  items: KnowledgeItemDB,
  item: KnowledgeItem,
): Promise<MediaTranscribeResult> {
  let baseContent = item.content;
  let body = splitForumNoteSections(item.content).body;
  let replies = parseForumReplies(item.content);
  const forumTarget = item.sourceUri
    ? detectForumPlatform(item.sourceUri)
    : null;
  if (isRefreshableForumTarget(forumTarget)) {
    try {
      const thread = await fetchLatestForumThread(db, forumTarget);
      body = thread.content;
      replies = thread.replies;
      baseContent = updateForumReplyCount(
        replaceForumRepliesSection(item.content, thread.replies),
        thread.replyCount,
      );
    } catch (error) {
      return {
        success: false,
        error: `无法获取完整的${refreshableForumLabel(forumTarget.platform)}讨论：${
          error instanceof Error ? error.message : String(error)
        }`,
      };
    }
  }
  if (replies.length === 0) {
    return { success: false, error: "该条目没有可用于总结的讨论内容" };
  }

  const config = resolveMediaSummaryConfig();
  if (!config) {
    return {
      success: false,
      notConfigured: true,
      error: "未配置可用的文本模型",
    };
  }

  try {
    const result = await generateForumSummary(
      {
        title: item.title,
        // 只喂主楼，别把上一版总结和回复原文重复塞进提示词
        content: body,
        replies,
      },
      config,
    );
    if (!result) {
      return { success: false, error: "模型未返回有效的讨论总结" };
    }
    const content = upsertForumSummarySection(baseContent, result.summary);
    const patch: { content: string; title?: string } = { content };
    // 标题仍是那个说不清内容的原标题时，顺手换成模型拟的
    if (result.title && result.title !== item.title) {
      patch.content = appendOriginalTitleNote(content, item.title);
      patch.title = result.title;
    }
    const updated = items.update(item.id, patch);
    console.log(
      `[import] 讨论总结重新生成完成（item=${item.id}，${result.summary.length} 字）`,
    );
    return { success: true, item: updated ?? undefined };
  } catch (error) {
    return {
      success: false,
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

function updateForumReplyCount(content: string, replyCount: number): string {
  return content.replace(
    /^(>\s*平台：[^\n]*?·\s*)\d+\s*条回复(?=[ \t]*(?:（|$))/m,
    (_line, prefix: string) => `${prefix}${replyCount} 条回复`,
  );
}

/** 抓取支持站点的最新全楼层，替换讨论段并把旧总结标成过期。 */
async function refreshForumDiscussion(
  db: Database.Database,
  items: KnowledgeItemDB,
  item: KnowledgeItem,
): Promise<MediaTranscribeResult> {
  const target = item.sourceUri ? detectForumPlatform(item.sourceUri) : null;
  if (item.itemType !== "forum" || !isRefreshableForumTarget(target)) {
    return {
      success: false,
      error: "仅 LINUX DO、小众软件论坛与 2Libra 条目支持刷新讨论",
    };
  }

  try {
    const thread = await fetchLatestForumThread(db, target);
    const content = updateForumReplyCount(
      replaceForumRepliesSection(item.content, thread.replies),
      thread.replyCount,
    );
    const updated = items.update(item.id, { content });
    if (!updated) {
      return { success: false, error: "刷新后的讨论写入失败" };
    }
    const platformLabel = refreshableForumLabel(target.platform);
    console.log(
      `[import] ${platformLabel}讨论刷新完成（item=${item.id}，${thread.replies.length} 层）`,
    );
    return { success: true, item: updated };
  } catch (error) {
    return {
      success: false,
      error: `刷新${refreshableForumLabel(target.platform)}讨论失败：${
        error instanceof Error ? error.message : String(error)
      }`,
    };
  }
}

/**
 * 预处理（ffmpeg 转码，不可用时原样直传）→ 转写 → AI 排版 → 内容总结 → 写回条目。
 * 文字稿存 transcript 字段；总结写入正文总结小节并应用 AI 标题，
 * 历史转写状态注记一并清除。
 */
/**
 * 按秒上报当前阶段与已用时长。
 *
 * 覆盖整条链路（转写 → 排版 → 总结）而不只是转写：这三步都以分钟计，
 * 只给转写计时的话，后两步期间界面会停在最后一次上报的数字上、文案还写着
 * 「正在转写」——看起来就像卡死了。没有百分比是有意的，见
 * `TranscribeProgress` 的注释。
 */
function startTranscribeProgress(
  itemId: string,
  report?: (payload: TranscribeProgress) => void,
): { setStage: (stage: TranscribeStage) => void; stop: () => void } {
  if (!report) {
    return { setStage: () => {}, stop: () => {} };
  }
  const startedAt = Date.now();
  let stage: TranscribeStage = "transcribing";
  const emit = () => {
    const activityAt = getTranscriptionActivityAt();
    report({
      itemId,
      stage,
      elapsedMs: Date.now() - startedAt,
      // 心跳来自本地 ASR 服务，只有转写阶段有；云端转写也没有，
      // 报出来会变成「一直卡着」
      stalledMs:
        stage === "transcribing" && activityAt > startedAt
          ? Date.now() - activityAt
          : undefined,
    });
  };
  const timer = setInterval(emit, 1000);
  return {
    setStage: (next) => {
      stage = next;
      emit();
    },
    stop: () => clearInterval(timer),
  };
}

async function transcribeAndSave(
  items: KnowledgeItemDB,
  item: KnowledgeItem,
  sourceFilePath: string,
  ffmpegExecutable: string,
  config: TranscriptionModelConfig,
  diarize = false,
  report?: (payload: TranscribeProgress) => void,
): Promise<MediaTranscribeResult> {
  const prepared = await prepareAudioForTranscription(
    sourceFilePath,
    ffmpegExecutable,
  );
  const progress = startTranscribeProgress(item.id, report);
  try {
    const rawText = await transcribeMediaFile(
      prepared.filePath,
      config,
      undefined,
      { diarize },
    );
    progress.setStage("formatting");
    const text = await formatTranscriptSafely(rawText, item);
    progress.setStage("summarizing");
    const sourceContent = upsertTranscriptionSourceNote(
      item.content,
      `音频识别（${config.model}）`,
    );
    const summarized = await applyMediaSummarySafely(item, sourceContent, text);
    const patch: { transcript: string; content?: string; title?: string } = {
      transcript: text,
    };
    if (summarized.content !== item.content) {
      patch.content = summarized.content;
    }
    if (summarized.title) {
      patch.title = summarized.title;
    }
    const updated = items.update(item.id, patch);
    console.log(`[media] 转写完成（item=${item.id}，${text.length} 字）`);
    // 要求了分离却只出来一个说话人：聚类判定「只有一个人」。实测短音频
    // （15 秒 3 轮）会塌成单说话人，而用户会以为功能没生效
    const speakers = diarize ? listSpeakers(text).length : 0;
    return {
      success: true,
      item: updated ?? undefined,
      warning:
        diarize && speakers <= 1
          ? "只识别出一个说话人。音频较短或换人间隔太小时分不出来，全程单人说话时这也是正确结果。"
          : undefined,
    };
  } finally {
    progress.stop();
    prepared.cleanup();
  }
}

/** 在线视频条目：按来源链接重新下载音轨并转写（重新生成文字稿） */
async function retranscribeOnlineVideo(
  db: Database.Database,
  items: KnowledgeItemDB,
  item: KnowledgeItem,
  ffmpegExecutable: string,
  config: TranscriptionModelConfig,
  diarize = false,
  report?: (payload: TranscribeProgress) => void,
): Promise<MediaTranscribeResult> {
  let tempDir: string | null = null;
  try {
    const audio = await downloadItemVideoAudio(db, item, {
      getYtDlpPath: () => readYtDlpPathSetting(db),
    });
    tempDir = audio.dir;
    return await transcribeAndSave(
      items,
      item,
      audio.filePath,
      ffmpegExecutable,
      config,
      diarize,
      report,
    );
  } catch (error) {
    if (error instanceof YtDlpNotFoundError) {
      return {
        success: false,
        error: "尚未安装 yt-dlp——打开「设置 → 应用设置 → 采集」一键安装后重试",
      };
    }
    throw error;
  } finally {
    if (tempDir) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }
}

async function pickExecutable(
  event: IpcMainInvokeEvent,
  title: string,
): Promise<string | null> {
  const owner = BrowserWindow.fromWebContents(event.sender) ?? undefined;
  const options = {
    title,
    properties: ["openFile" as const],
    filters:
      process.platform === "win32"
        ? [
            { name: "可执行文件", extensions: ["exe"] },
            { name: "All Files", extensions: ["*"] },
          ]
        : [{ name: "All Files", extensions: ["*"] }],
  };
  const result = owner
    ? await dialog.showOpenDialog(owner, options)
    : await dialog.showOpenDialog(options);
  if (result.canceled || result.filePaths.length === 0) {
    return null;
  }
  // 登记后 settings:set 才允许把它写进配置（见 picked-binary-paths.ts）
  rememberPickedBinaryPath(result.filePaths[0]);
  return result.filePaths[0];
}

// 采集引擎状态缓存：模块级持有，registerMediaIPC 重入（数据目录切换）不清空——
// 缓存键是各自的配置路径，配置变了自然未命中。
const ytDlpStatusCache = createStatusCache<YtDlpStatus>();
const ffmpegStatusCache = createStatusCache<FfmpegStatus>();
const funasrStatusCache = createStatusCache<FunasrStatus>();
const FUNASR_STATUS_CACHE_KEY = "funasr";

export function registerMediaIPC(db: Database.Database): void {
  ipcMain.handle(
    IPC_CHANNELS.MEDIA_TRANSCRIBE,
    async (
      event,
      itemId: string,
      options?: { diarize?: boolean },
    ): Promise<MediaTranscribeResult> => {
      const report = (payload: TranscribeProgress) => {
        event.sender.send(IPC_CHANNELS.MEDIA_TRANSCRIBE_PROGRESS, payload);
      };
      const items = new KnowledgeItemDB(db);
      const item = items.get(itemId);
      if (!item) {
        return { success: false, error: "条目不存在" };
      }
      if (item.itemType !== "audio" && item.itemType !== "video") {
        return { success: false, error: "仅音频 / 视频条目支持转写" };
      }

      const config = resolveTranscriptionConfig();
      if (!config) {
        return {
          success: false,
          notConfigured: true,
          error: "未配置语音转写模型",
        };
      }
      const diarize = options?.diarize === true;
      // 说话人分离是内置引擎的扩展能力，云端转写接口给不出来——
      // 静默忽略的话用户会以为分了、结果里却一个说话人都没有
      if (diarize && !supportsDiarization(config.apiUrl)) {
        return {
          success: false,
          error:
            "区分说话人只有内置本地转写引擎支持，当前「语音转写」路由指向的是外部接口",
        };
      }

      const ffmpegExecutable = resolveFfmpegExecutable(
        readFfmpegPathSetting(db),
      );

      try {
        // 目标是托管本地引擎时先确保服务已启动（首次约 10-20 秒）
        await ensureLocalTranscriptionService(config.apiUrl);

        const assetFileName = extractLocalAssetRef(item.content, "local-video");
        if (!assetFileName) {
          // 无本地媒体文件：尝试按来源链接重新下载音轨（在线视频）
          return await retranscribeOnlineVideo(
            db,
            items,
            item,
            ffmpegExecutable,
            config,
            diarize,
            report,
          );
        }

        const filePath = path.join(getVideosDir(), assetFileName);
        if (!fs.existsSync(filePath)) {
          return { success: false, error: "媒体文件已不存在" };
        }
        return await transcribeAndSave(
          items,
          item,
          filePath,
          ffmpegExecutable,
          config,
          diarize,
          report,
        );
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  // 「语音转写」路由支持哪些可选能力：界面据此决定要不要摆出入口
  ipcMain.handle(
    IPC_CHANNELS.MEDIA_CAPABILITIES,
    async (): Promise<MediaCapabilities> => {
      const config = resolveTranscriptionConfig();
      return {
        diarization: config ? supportsDiarization(config.apiUrl) : false,
      };
    },
  );

  // 已有文字稿的 AI 排版：不重新转写，直接整理 transcript 字段
  ipcMain.handle(
    IPC_CHANNELS.MEDIA_FORMAT_TRANSCRIPT,
    async (
      event,
      itemId: string,
      options?: { allowLong?: boolean },
    ): Promise<MediaTranscribeResult> => {
      const items = new KnowledgeItemDB(db);
      const item = items.get(itemId);
      if (!item) {
        return { success: false, error: "条目不存在" };
      }
      const transcript = item.transcript?.trim();
      if (!transcript) {
        return { success: false, error: "该条目还没有文字稿" };
      }

      const config = resolveTranscriptFormatterConfig();
      if (!config) {
        return {
          success: false,
          notConfigured: true,
          error: "未配置可用的文本模型",
        };
      }

      try {
        const { text, skippedReason, partialReason } = await formatTranscript(
          transcript,
          config,
          {
            glossary: resolveItemGlossary(item),
            // 用户已在确认框里看过字数与预估请求次数，这里不再拦
            maxTotalChars: options?.allowLong
              ? Number.MAX_SAFE_INTEGER
              : undefined,
            onProgress: (current, total) => {
              const payload: TranscriptFormatProgress = {
                itemId,
                current,
                total,
              };
              event.sender.send(IPC_CHANNELS.MEDIA_FORMAT_PROGRESS, payload);
            },
          },
        );
        // 手动点的操作不能静默返回成功——没排版就得说清为什么
        if (skippedReason) {
          return { success: false, error: skippedReason };
        }
        const updated = items.update(item.id, { transcript: text });
        console.log(
          `[media] 文字稿排版完成（item=${item.id}，${text.length} 字${
            partialReason ? "，部分排版" : ""
          }）`,
        );
        // 排到一半照样落库（这些请求的钱已经花了），但不能报成完整成功
        return {
          success: true,
          item: updated ?? undefined,
          warning: partialReason,
        };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  // 基于文字稿生成结构化「视频/音频总结」，写入正文的总结小节（已有则替换）
  ipcMain.handle(
    IPC_CHANNELS.MEDIA_SUMMARIZE,
    async (_event, itemId: string): Promise<MediaTranscribeResult> => {
      const items = new KnowledgeItemDB(db);
      const item = items.get(itemId);
      if (!item) {
        return { success: false, error: "条目不存在" };
      }
      if (item.itemType === "forum") {
        return await regenerateForumSummary(db, items, item);
      }
      if (item.itemType !== "audio" && item.itemType !== "video") {
        return { success: false, error: "仅音频 / 视频条目支持内容总结" };
      }
      const transcript = item.transcript?.trim();
      if (!transcript) {
        return { success: false, error: "该条目还没有文字稿，请先生成文字稿" };
      }

      const config = resolveMediaSummaryConfig();
      if (!config) {
        return {
          success: false,
          notConfigured: true,
          error: "未配置可用的文本模型",
        };
      }

      try {
        const { summary, title } = await generateMediaSummary(
          { title: item.title, transcript },
          config,
        );
        let content = upsertMediaSummarySection(
          item.content,
          mediaSummaryHeading(item.itemType),
          summary,
        );
        const patch: { content: string; title?: string } = { content };
        if (title && title !== item.title) {
          content = appendOriginalTitleNote(content, item.title);
          patch.content = content;
          patch.title = title;
        }
        const updated = items.update(item.id, patch);
        console.log(
          `[media] 内容总结完成（item=${item.id}，${summary.length} 字）`,
        );
        return { success: true, item: updated ?? undefined };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.MEDIA_REFRESH_FORUM_DISCUSSION,
    async (_event, itemId: string): Promise<MediaTranscribeResult> => {
      const items = new KnowledgeItemDB(db);
      const item = items.get(itemId);
      if (!item) {
        return { success: false, error: "条目不存在" };
      }
      return refreshForumDiscussion(db, items, item);
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.MEDIA_TEST_TRANSCRIPTION,
    async (
      _event,
      config: { apiUrl?: string; apiKey?: string; model?: string },
    ): Promise<TranscriptionTestResult> => {
      const apiUrl = config?.apiUrl?.trim();
      const apiKey = config?.apiKey?.trim();
      const model = config?.model?.trim();
      if (!apiUrl || !apiKey || !model) {
        return { success: false, error: "转写模型配置不完整" };
      }
      try {
        await ensureLocalTranscriptionService(apiUrl);
        const { latency } = await testTranscriptionConfig({
          apiUrl,
          apiKey,
          model,
        });
        return { success: true, latency };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  // ── yt-dlp 工具管理 ──────────────────────────────────────────────────────

  ipcMain.handle(
    IPC_CHANNELS.YTDLP_STATUS,
    (_event, force?: boolean): Promise<YtDlpStatus> => {
      const configuredPath = readYtDlpPathSetting(db);
      return ytDlpStatusCache.read(
        configuredPath ?? "",
        () =>
          getYtDlpStatus(configuredPath, {
            probe: withCachedVersion(probeYtDlpVersion, force === true),
          }),
        force === true,
      );
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.YTDLP_CHECK_UPDATE,
    async (): Promise<ToolUpdateCheck> => {
      // 只有当前就跑在内置版上才谈得上「更新」：来源是 PATH / 自定义路径时
      // 用户面对的动作是「安装内置版」，不存在版本比较。
      const current = await getYtDlpStatus(readYtDlpPathSetting(db), {
        probe: withCachedVersion(probeYtDlpVersion),
      });
      return checkYtDlpUpdate(
        current.source === "managed" ? (current.version ?? null) : null,
      );
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.YTDLP_INSTALL,
    async (event): Promise<YtDlpInstallResult> => {
      try {
        const { version } = await installYtDlp((progress) => {
          if (!event.sender.isDestroyed()) {
            event.sender.send(IPC_CHANNELS.YTDLP_DOWNLOAD_PROGRESS, progress);
          }
        });
        return { success: true, version };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      } finally {
        ytDlpStatusCache.invalidate();
      }
    },
  );

  ipcMain.handle(IPC_CHANNELS.YTDLP_REMOVE, () => {
    ytDlpStatusCache.invalidate();
    return removeManagedYtDlp();
  });

  ipcMain.handle(IPC_CHANNELS.YTDLP_PICK_BINARY, (event) =>
    pickExecutable(event, "选择 yt-dlp 可执行文件"),
  );

  // ── ffmpeg 工具管理 ──────────────────────────────────────────────────────

  ipcMain.handle(
    IPC_CHANNELS.FFMPEG_STATUS,
    (_event, force?: boolean): Promise<FfmpegStatus> => {
      const configuredPath = readFfmpegPathSetting(db);
      return ffmpegStatusCache.read(
        configuredPath ?? "",
        () =>
          getFfmpegStatus(configuredPath, {
            probe: withCachedVersion(probeFfmpegVersion, force === true),
          }),
        force === true,
      );
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.FFMPEG_CHECK_UPDATE,
    async (): Promise<ToolUpdateCheck> => {
      const current = await getFfmpegStatus(readFfmpegPathSetting(db), {
        probe: withCachedVersion(probeFfmpegVersion),
      });
      return checkFfmpegUpdate(
        current.source === "managed" ? (current.version ?? null) : null,
      );
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.FFMPEG_INSTALL,
    async (event): Promise<FfmpegInstallResult> => {
      try {
        const { version } = await installFfmpeg((progress) => {
          if (!event.sender.isDestroyed()) {
            event.sender.send(IPC_CHANNELS.FFMPEG_DOWNLOAD_PROGRESS, progress);
          }
        });
        return { success: true, version };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      } finally {
        ffmpegStatusCache.invalidate();
      }
    },
  );

  ipcMain.handle(IPC_CHANNELS.FFMPEG_REMOVE, () => {
    ffmpegStatusCache.invalidate();
    return removeManagedFfmpeg();
  });

  ipcMain.handle(IPC_CHANNELS.FFMPEG_PICK_BINARY, (event) =>
    pickExecutable(event, "选择 ffmpeg 可执行文件"),
  );

  // ── 本地转写引擎（托管 funasr-server）──────────────────────────────────

  ipcMain.handle(
    IPC_CHANNELS.FUNASR_STATUS,
    (_event, force?: boolean): Promise<FunasrStatus> =>
      // 无配置输入，缓存键固定；running 会随转写按需启动而变，靠 TTL 与手动重新检测收敛
      funasrStatusCache.read(
        FUNASR_STATUS_CACHE_KEY,
        getFunasrStatus,
        force === true,
      ),
  );

  ipcMain.handle(
    IPC_CHANNELS.FUNASR_INSTALL,
    async (event): Promise<FunasrInstallResult> => {
      try {
        const { version } = await installFunasr((progress) => {
          if (!event.sender.isDestroyed()) {
            event.sender.send(IPC_CHANNELS.FUNASR_INSTALL_PROGRESS, progress);
          }
        });
        return { success: true, version };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      } finally {
        funasrStatusCache.invalidate();
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.FUNASR_UNINSTALL,
    async (): Promise<FunasrOperationResult> => {
      try {
        await uninstallFunasr();
        return { success: true };
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      } finally {
        funasrStatusCache.invalidate();
      }
    },
  );

  registerFunasrQuitHook();
}

let funasrQuitHookRegistered = false;

/** 应用退出时停止托管的转写服务（registerMediaIPC 可能因数据目录切换重入） */
function registerFunasrQuitHook(): void {
  if (funasrQuitHookRegistered) {
    return;
  }
  funasrQuitHookRegistered = true;
  app.on("will-quit", () => {
    stopFunasrService();
  });
}
