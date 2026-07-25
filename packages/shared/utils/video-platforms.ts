/**
 * 在线视频平台链接识别。主进程（采集 / 重新转写）与渲染进程
 * （转写卡片按钮可见性）共用，保持判定一致。
 */

export type VideoPlatform = "bilibili" | "youtube" | "douyin" | "xiaohongshu";

/** 识别视频平台链接（其余 URL 走网页抓取） */
export function detectVideoPlatform(url: string): VideoPlatform | null {
  let hostname: string;
  let pathname: string;
  try {
    const parsed = new URL(url);
    hostname = parsed.hostname.toLowerCase();
    pathname = parsed.pathname;
  } catch {
    return null;
  }

  if (hostname === "b23.tv") {
    return "bilibili";
  }
  if (hostname.endsWith("bilibili.com")) {
    return pathname.startsWith("/video/") ? "bilibili" : null;
  }
  if (hostname === "youtu.be") {
    return "youtube";
  }
  if (hostname.endsWith("youtube.com")) {
    return pathname.startsWith("/watch") || pathname.startsWith("/shorts/")
      ? "youtube"
      : null;
  }
  if (hostname.endsWith("douyin.com")) {
    return "douyin";
  }
  if (hostname.endsWith("xiaohongshu.com") || hostname === "xhslink.com") {
    return "xiaohongshu";
  }
  return null;
}
