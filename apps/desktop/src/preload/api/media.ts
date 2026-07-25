import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  FfmpegInstallResult,
  FfmpegStatus,
  FunasrInstallResult,
  FunasrOperationResult,
  FunasrStatus,
  KnowledgeItem,
  YtDlpInstallResult,
  YtDlpStatus,
} from "@guizhi/shared/types";

export interface MediaTranscribeResult {
  success: boolean;
  notConfigured?: boolean;
  error?: string;
  item?: KnowledgeItem;
}

export interface TranscriptionTestResult {
  success: boolean;
  latency?: number;
  error?: string;
}

export const mediaApi = {
  transcribe: (itemId: string): Promise<MediaTranscribeResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_TRANSCRIBE, itemId),
  /** 已有文字稿的 AI 排版（补标点/分段，不重新转写） */
  formatTranscript: (itemId: string): Promise<MediaTranscribeResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_FORMAT_TRANSCRIPT, itemId),
  /** 基于文字稿生成结构化「视频/音频总结」并写入正文 */
  summarize: (itemId: string): Promise<MediaTranscribeResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_SUMMARIZE, itemId),
  /** 转写模型连通性测试：主进程用静音样本发起真实转写请求 */
  testTranscription: (config: {
    apiUrl: string;
    apiKey: string;
    model: string;
  }): Promise<TranscriptionTestResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.MEDIA_TEST_TRANSCRIPTION, config),
};

export const ytDlpApi = {
  status: (): Promise<YtDlpStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_STATUS),
  install: (): Promise<YtDlpInstallResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_INSTALL),
  remove: (): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_REMOVE),
  pickBinary: (): Promise<string | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.YTDLP_PICK_BINARY),
};

export const ffmpegApi = {
  status: (): Promise<FfmpegStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_STATUS),
  install: (): Promise<FfmpegInstallResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_INSTALL),
  remove: (): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_REMOVE),
  pickBinary: (): Promise<string | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.FFMPEG_PICK_BINARY),
};

export const funasrApi = {
  status: (): Promise<FunasrStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.FUNASR_STATUS),
  install: (): Promise<FunasrInstallResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.FUNASR_INSTALL),
  uninstall: (): Promise<FunasrOperationResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.FUNASR_UNINSTALL),
};
