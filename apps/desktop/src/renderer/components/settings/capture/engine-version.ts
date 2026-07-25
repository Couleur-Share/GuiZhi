/**
 * 采集引擎的版本标识统一按日期展示。
 *
 * 两个引擎的标识本质都是日期——yt-dlp 的版本号就是发布日（2026.07.04），
 * ffmpeg 每日构建的版本串尾部带构建日期（N-125753-g6095372a70-20260724）——
 * 所以归一成 YYYY-MM-DD，状态行里两行结构完全一致，不需要 v / 构建 之类的装饰词。
 *
 * 拿不到日期的（gyan.dev 的 8.1 这类发行版、yt-dlp 的 nightly）退回版本号本身。
 */
export function formatEngineVersion(version: string): string {
  const trimmed = version.trim();

  // yt-dlp 稳定版：2026.07.04
  const dotted = trimmed.match(/^(\d{4})\.(\d{2})\.(\d{2})$/);
  if (dotted) {
    return `${dotted[1]}-${dotted[2]}-${dotted[3]}`;
  }

  // ffmpeg 每日构建：…-20260724，或主进程直接给出的 20260724
  const compact = trimmed.match(/(?:^|-)(\d{8})$/);
  if (compact) {
    const date = compact[1];
    return `${date.slice(0, 4)}-${date.slice(4, 6)}-${date.slice(6)}`;
  }

  // ffmpeg 发行版：8.1-essentials_build-www.gyan.dev → 8.1
  const head = trimmed.split("-")[0];
  return /^\d/.test(head) ? head : trimmed;
}
