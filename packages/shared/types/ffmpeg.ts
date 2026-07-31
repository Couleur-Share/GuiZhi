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
  /**
   * 当前平台是否提供应用内一键安装。
   * 仅 Windows 为 true；Mac / Linux 靠包管理器或自定义路径。
   */
  installSupported: boolean;
  /**
   * 应用内装不了时的推荐安装命令（如 `brew install ffmpeg`）。
   * 无统一命令的平台（Linux 各发行版不同）不填，由 UI 给通用文案。
   */
  installHintCommand?: string;
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
