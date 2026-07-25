/**
 * yt-dlp 工具管理类型（在线视频解析引擎的应用内安装与状态）。
 */

/** 当前生效的 yt-dlp 来源 */
export type YtDlpSource = "custom" | "managed" | "path";

export interface YtDlpStatus {
  installed: boolean;
  /** 生效来源；未安装时为 null */
  source: YtDlpSource | null;
  /** `yt-dlp --version` 输出（如 2026.07.15） */
  version?: string;
  /** 生效的可执行文件路径（PATH 来源时为命令名） */
  path?: string;
  /** 内置托管版的目标路径（未安装也返回，供 UI 展示） */
  managedPath: string;
}

export interface YtDlpInstallResult {
  success: boolean;
  version?: string;
  error?: string;
}

export interface YtDlpDownloadProgress {
  /** 已下载字节数 */
  transferred: number;
  /** 总字节数（镜像可能不返回 content-length，此时为 null） */
  total: number | null;
}
