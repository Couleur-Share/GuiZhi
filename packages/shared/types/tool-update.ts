/**
 * 托管工具的「检查更新」结果（yt-dlp / ffmpeg 共用）。
 *
 * current / latest 是各引擎自己的可比标识：yt-dlp 用 release tag（就是版本号），
 * ffmpeg 上游是滚动 tag，改用资产的构建日期 YYYYMMDD。
 */
export interface ToolUpdateCheck {
  /** 当前内置版的标识；来源不是内置版时为 null */
  current: string | null;
  /** 远端最新标识；查询失败为 null（UI 据此报「检查失败」而非「已是最新」） */
  latest: string | null;
  /** 远端确实比本地新 */
  updateAvailable: boolean;
}
