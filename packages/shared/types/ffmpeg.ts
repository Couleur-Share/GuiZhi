/**
 * ffmpeg 工具管理类型（转写前音频转码引擎的应用内安装与状态）。
 */

/** 当前生效的 ffmpeg 来源 */
export type FfmpegSource = "custom" | "managed" | "path";

export interface FfmpegStatus {
  installed: boolean;
  /** 生效来源；未安装时为 null */
  source: FfmpegSource | null;
  /** `ffmpeg -version` 首行解析出的版本（如 8.1-essentials_build） */
  version?: string;
  /** 生效的可执行文件路径（PATH 来源时为命令名） */
  path?: string;
  /** 内置托管版的目标路径（未安装也返回，供 UI 展示） */
  managedPath: string;
}

export interface FfmpegInstallResult {
  success: boolean;
  version?: string;
  error?: string;
}

export interface FfmpegDownloadProgress {
  /** 已下载字节数 */
  transferred: number;
  /** 总字节数（镜像可能不返回 content-length，此时为 null） */
  total: number | null;
}
