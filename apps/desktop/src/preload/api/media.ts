import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  FfmpegInstallResult,
  FfmpegStatus,
  FunasrInstallResult,
  FunasrOperationResult,
  FunasrStatus,
  KnowledgeItem,
  MediaCapabilities,
  ToolUpdateCheck,
  YtDlpInstallResult,
  YtDlpStatus,
} from "@guizhi/shared/types";

export interface MediaTranscribeResult {
  success: boolean;
  notConfigured?: boolean;
  error?: string;
  /** 成功但打了折扣（如只排版了一部分） */
  warning?: string;
  item?: KnowledgeItem;
}

export interface TranscriptionTestResult {
  success: boolean;
  latency?: number;
  error?: string;
}

export const mediaApi = {
  /** diarize 区分说话人，仅内置本地转写引擎支持 */
  transcribe: (
    itemId: string,
    options?: { diarize?: boolean },
  ): Promise<MediaTranscribeResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_TRANSCRIBE, itemId, options),
  /**
   * 已有文字稿的 AI 排版（补标点/分段，不重新转写）。
   * allowLong 越过自动排版的长度上限，由调用方先向用户确认代价。
   */
  formatTranscript: (
    itemId: string,
    options?: { allowLong?: boolean },
  ): Promise<MediaTranscribeResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_FORMAT_TRANSCRIPT, itemId, options),
  /** 基于文字稿生成结构化「视频/音频总结」并写入正文 */
  summarize: (itemId: string): Promise<MediaTranscribeResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_SUMMARIZE, itemId),
  /** 当前「语音转写」路由支持哪些可选能力（界面据此决定摆不摆入口） */
  capabilities: (): Promise<MediaCapabilities> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_CAPABILITIES),
  /** 转写模型连通性测试：主进程用静音样本发起真实转写请求 */
  testTranscription: (config: {
    apiUrl: string;
    apiKey: string;
    model: string;
  }): Promise<TranscriptionTestResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_TEST_TRANSCRIPTION, config),
};

export const ytDlpApi = {
  /** force 为 true 时绕过主进程状态缓存，重新 spawn 探测 */
  status: (force?: boolean): Promise<YtDlpStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_STATUS, force),
  /** 只查远端版本号判断有无更新，不下载任何资产 */
  checkUpdate: (): Promise<ToolUpdateCheck> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_CHECK_UPDATE),
  install: (): Promise<YtDlpInstallResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_INSTALL),
  remove: (): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_REMOVE),
  pickBinary: (): Promise<string | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_PICK_BINARY),
};

export const ffmpegApi = {
  /** force 为 true 时绕过主进程状态缓存，重新 spawn 探测 */
  status: (force?: boolean): Promise<FfmpegStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_STATUS, force),
  /** 只读远端构建日期判断有无新构建，不下载那 160 多 MB 的 zip */
  checkUpdate: (): Promise<ToolUpdateCheck> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_CHECK_UPDATE),
  install: (): Promise<FfmpegInstallResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_INSTALL),
  remove: (): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_REMOVE),
  pickBinary: (): Promise<string | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_PICK_BINARY),
};

export const funasrApi = {
  /** force 为 true 时绕过主进程状态缓存，重新发起健康检查 */
  status: (force?: boolean): Promise<FunasrStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.FUNASR_STATUS, force),
  install: (): Promise<FunasrInstallResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.FUNASR_INSTALL),
  uninstall: (): Promise<FunasrOperationResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.FUNASR_UNINSTALL),
};
