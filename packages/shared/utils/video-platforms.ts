/**
 * 在线视频平台链接识别。主进程（采集 / 重新转写）与渲染进程
 * （转写卡片按钮可见性）共用，保持判定一致。
 */

export type VideoPlatform = "bilibili" | "youtube" | "douyin" | "xiaohongshu";

/** 域名归属判定：必须是该域本身或它的子域，`evildouyin.com` 这类后缀碰撞不算 */
function isHostOrSubdomain(hostname: string, ...domains: string[]): boolean {
  return domains.some(
    (domain) => hostname === domain || hostname.endsWith(`.${domain}`),
  );
}

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
  // iesdouyin.com 是抖音的分享域，采集走它的移动端分享页（见 import/douyin.ts）
  if (isHostOrSubdomain(hostname, "douyin.com", "iesdouyin.com")) {
    return "douyin";
  }
  if (hostname.endsWith("xiaohongshu.com") || hostname === "xhslink.com") {
    return "xiaohongshu";
  }
  return null;
}
