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
  ToolUpdateCheck,
  YtDlpInstallResult,
  YtDlpStatus,
} from "@guizhi/shared/types";
import {
  parseForumReplies,
  splitForumNoteSections,
  upsertForumSummarySection,
} from "@guizhi/shared/utils/forum-note";
import { extractLocalAssetRef } from "@guizhi/shared/utils/media-refs";
import {
  appendOriginalTitleNote,
  mediaSummaryHeading,
  upsertMediaSummarySection,
} from "@guizhi/shared/utils/media-summary";
import { detectVideoPlatform } from "@guizhi/shared/utils/video-platforms";
import { KnowledgeItemDB } from "@guizhi/db";
import Database from "../database/sqlite";
import { getVideosDir } from "../runtime-paths";
import {
  readFfmpegPathSetting,
  readYtDlpPathSetting,
} from "../services/import/import-service";
import {
  downloadDouyinMedia,
  fetchDouyinAweme,
} from "../services/import/douyin";
import {
  downloadBestAudio,
  runCommand,
  stripTranscriptionNote,
  YtDlpNotFoundError,
} from "../services/import/video-url";
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
  stopFunasrService,
} from "../services/media/funasr-service";
import {
  resolveTranscriptionConfig,
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
  formatTranscript,
  resolveTranscriptFormatterConfig,
} from "../services/media/transcript-format";
import {
  checkYtDlpUpdate,
  getYtDlpStatus,
  installYtDlp,
  probeYtDlpVersion,
  removeManagedYtDlp,
  resolveYtDlpExecutable,
} from "../services/media/ytdlp-manager";

export interface MediaTranscribeResult {
  success: boolean;
  /** 未配置 audioText 转写模型（UI 引导去设置） */
  notConfigured?: boolean;
  error?: string;
  item?: KnowledgeItem;
}

export interface TranscriptionTestResult {
  success: boolean;
  latency?: number;
  error?: string;
}

/** 转写成功后顺带 AI 排版（补标点/分段）；失败或未配置文本模型时保留原始转写 */
async function formatTranscriptSafely(raw: string): Promise<string> {
  const formatterConfig = resolveTranscriptFormatterConfig();
  if (!formatterConfig) {
    return raw;
  }
  try {
    return await formatTranscript(raw, formatterConfig);
  } catch (error) {
    console.warn("[media] 文字稿 AI 排版失败，保留原始转写:", error);
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
    console.log(`[media] 内容总结完成（item=${item.id}，${summary.length} 字）`);
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
 * 素材取自库里正文已存的逐楼回复，不重新抓网页：原帖可能已被删或又多了几十楼，
 * 用条目自己那份才与用户看到的内容一致，也省掉一次平台请求。
 */
async function regenerateForumSummary(
  items: KnowledgeItemDB,
  item: KnowledgeItem,
): Promise<MediaTranscribeResult> {
  const replies = parseForumReplies(item.content);
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
        content: splitForumNoteSections(item.content).body,
        replies,
      },
      config,
    );
    if (!result) {
      return { success: false, error: "模型未返回有效的讨论总结" };
    }
    const content = upsertForumSummarySection(item.content, result.summary);
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

/**
 * 预处理（ffmpeg 转码，不可用时原样直传）→ 转写 → AI 排版 → 内容总结 → 写回条目。
 * 文字稿存 transcript 字段；总结写入正文总结小节并应用 AI 标题，
 * 历史转写状态注记一并清除。
 */
async function transcribeAndSave(
  items: KnowledgeItemDB,
  item: KnowledgeItem,
  sourceFilePath: string,
  ffmpegExecutable: string,
  config: TranscriptionModelConfig,
): Promise<MediaTranscribeResult> {
  const prepared = await prepareAudioForTranscription(
    sourceFilePath,
    ffmpegExecutable,
  );
  try {
    const rawText = await transcribeMediaFile(prepared.filePath, config);
    const text = await formatTranscriptSafely(rawText);
    const summarized = await applyMediaSummarySafely(
      item,
      stripTranscriptionNote(item.content),
      text,
    );
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
    return { success: true, item: updated ?? undefined };
  } finally {
    prepared.cleanup();
  }
}

/** 抖音条目重转写：分享页取到无水印地址后直接下载 */
async function downloadDouyinAudio(
  sourceUrl: string,
): Promise<{ dir: string; filePath: string }> {
  const aweme = await fetchDouyinAweme(sourceUrl);
  if (aweme.kind === "note") {
    throw new Error("该抖音作品是图文，没有可转写的音轨");
  }
  if (!aweme.playUrl) {
    throw new Error("未能取到视频播放地址（抖音页面结构可能已变化）");
  }
  return downloadDouyinMedia(aweme.playUrl);
}

/** 在线视频条目：按来源链接重新下载音轨并转写（重新生成文字稿） */
async function retranscribeOnlineVideo(
  db: Database.Database,
  items: KnowledgeItemDB,
  item: KnowledgeItem,
  ffmpegExecutable: string,
  config: TranscriptionModelConfig,
): Promise<MediaTranscribeResult> {
  const sourceUrl = item.sourceUri?.trim() ?? "";
  const platform = detectVideoPlatform(sourceUrl);
  if (!platform) {
    return {
      success: false,
      error: "该条目没有本地媒体文件，来源链接也不是可解析的视频平台",
    };
  }

  let tempDir: string | null = null;
  try {
    // 抖音不经 yt-dlp：分享页拿到无水印地址后直接下载（见 import/douyin.ts）
    const audio = await (platform === "douyin"
      ? downloadDouyinAudio(sourceUrl)
      : downloadBestAudio(
          resolveYtDlpExecutable(readYtDlpPathSetting(db)),
          sourceUrl,
          runCommand,
        ));
    tempDir = audio.dir;
    return await transcribeAndSave(
      items,
      item,
      audio.filePath,
      ffmpegExecutable,
      config,
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
    async (_event, itemId: string): Promise<MediaTranscribeResult> => {
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
        );
      } catch (error) {
        return {
          success: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }
    },
  );

  // 已有文字稿的 AI 排版：不重新转写，直接整理 transcript 字段
  ipcMain.handle(
    IPC_CHANNELS.MEDIA_FORMAT_TRANSCRIPT,
    async (_event, itemId: string): Promise<MediaTranscribeResult> => {
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
        const formatted = await formatTranscript(transcript, config);
        const updated = items.update(item.id, { transcript: formatted });
        console.log(
          `[media] 文字稿排版完成（item=${item.id}，${formatted.length} 字）`,
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
        return await regenerateForumSummary(items, item);
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
        current.source === "managed" ? current.version ?? null : null,
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
        current.source === "managed" ? current.version ?? null : null,
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
      funasrStatusCache.read(FUNASR_STATUS_CACHE_KEY, getFunasrStatus, force === true),
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
