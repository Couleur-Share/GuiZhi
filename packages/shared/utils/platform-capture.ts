import type { PlatformCapturePlatform } from "../types/platform-capture";

const PLATFORM_DOMAINS: Record<PlatformCapturePlatform, readonly string[]> = {
  xiaohongshu: ["xiaohongshu.com", "xhslink.com", "xhslink.cn"],
  douyin: ["douyin.com", "iesdouyin.com"],
  linuxdo: ["linux.do"],
};

export function isHostOrSubdomain(hostname: string, domain: string): boolean {
  const host = hostname.toLowerCase();
  const expected = domain.toLowerCase();
  return host === expected || host.endsWith(`.${expected}`);
}

export function isAllowedPlatformUrl(
  platform: PlatformCapturePlatform,
  value: string,
): boolean {
  try {
    const url = new URL(value);
    return (
      url.protocol === "https:" &&
      PLATFORM_DOMAINS[platform].some((domain) =>
        isHostOrSubdomain(url.hostname, domain),
      )
    );
  } catch {
    return false;
  }
}

export function detectPlatformCapturePlatform(
  value: string,
): PlatformCapturePlatform | null {
  if (isAllowedPlatformUrl("xiaohongshu", value)) return "xiaohongshu";
  if (isAllowedPlatformUrl("douyin", value)) return "douyin";
  if (isAllowedPlatformUrl("linuxdo", value)) return "linuxdo";
  return null;
}

export function isCreatorProfileUrl(
  platform: PlatformCapturePlatform,
  value: string,
): boolean {
  if (!isAllowedPlatformUrl(platform, value)) return false;
  if (platform === "linuxdo") return false;
  const pathname = new URL(value).pathname;
  return platform === "xiaohongshu"
    ? /^\/user\/profile\/[^/?#]+\/?$/i.test(pathname)
    : /^\/user\/[^/?#]+\/?$/i.test(pathname);
}

export function detectPlatformCreatorUrl(value: string): {
  platform: PlatformCapturePlatform;
  url: string;
} | null {
  const platform = detectPlatformCapturePlatform(value);
  if (!platform || !isCreatorProfileUrl(platform, value)) return null;
  return { platform, url: value };
}

export function platformLoginUrl(platform: PlatformCapturePlatform): string {
  if (platform === "xiaohongshu") {
    return "https://www.xiaohongshu.com/explore";
  }
  if (platform === "linuxdo") {
    return "https://linux.do/login";
  }
  return "https://www.douyin.com/";
}

export function platformSearchUrl(
  platform: PlatformCapturePlatform,
  keyword: string,
): string {
  const encoded = encodeURIComponent(keyword);
  if (platform === "xiaohongshu") {
    return `https://www.xiaohongshu.com/search_result?keyword=${encoded}`;
  }
  if (platform === "linuxdo") {
    return `https://linux.do/search?q=${encoded}`;
  }
  return `https://www.douyin.com/search/${encoded}`;
}

/** 独立来源评论只对抖音、小红书开放；论坛回复由讨论区维护。 */
export function detectSourceCommentsPlatform(value: string): "xiaohongshu" | "douyin" | null {
  const platform = detectPlatformCapturePlatform(value);
  return platform === "xiaohongshu" || platform === "douyin" ? platform : null;
}
