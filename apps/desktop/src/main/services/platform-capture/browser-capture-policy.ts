import type { PlatformCapturePlatform } from "@guizhi/shared/types";
import { isAllowedPlatformUrl } from "@guizhi/shared/utils/platform-capture";
import {
  LOGIN_FLOW_DOMAINS,
  RESOURCE_DOMAINS,
} from "./browser-capture-domains";

function isAllowedSecureProtocol(protocol: string): boolean {
  return protocol === "https:" || protocol === "wss:";
}

export function isOfficialLoginFlowUrl(
  platform: PlatformCapturePlatform,
  value: string,
): boolean {
  if (platform === "nodeseek") {
    if (isAllowedPlatformUrl(platform, value)) return true;
    try {
      const url = new URL(value);
      return (
        url.protocol === "https:" &&
        url.hostname === "challenges.cloudflare.com" &&
        !url.port &&
        !url.username &&
        !url.password
      );
    } catch {
      return false;
    }
  }
  if (isAllowedPlatformUrl(platform, value)) return true;
  try {
    const url = new URL(value);
    if (!isAllowedSecureProtocol(url.protocol)) return false;
    const host = url.hostname.toLowerCase();
    return LOGIN_FLOW_DOMAINS[platform].some(
      (domain) => host === domain || host.endsWith(`.${domain}`),
    );
  } catch {
    return false;
  }
}

export function isAllowedBrowserResourceUrl(
  platform: PlatformCapturePlatform,
  value: string,
): boolean {
  if (/^(?:about:blank|data:|blob:)/i.test(value)) return true;
  if (platform === "nodeseek") return isOfficialLoginFlowUrl(platform, value);
  if (platform === "linuxdo") {
    try {
      const url = new URL(value);
      return isAllowedSecureProtocol(url.protocol);
    } catch {
      return false;
    }
  }
  if (isOfficialLoginFlowUrl(platform, value)) return true;
  try {
    const url = new URL(value);
    if (!isAllowedSecureProtocol(url.protocol)) return false;
    const host = url.hostname.toLowerCase();
    return RESOURCE_DOMAINS[platform].some(
      (domain) => host === domain || host.endsWith(`.${domain}`),
    );
  } catch {
    return false;
  }
}

export function shouldBlockLoginPageRequest(
  platform: PlatformCapturePlatform,
  value: string,
  resourceType: string,
): boolean {
  // 登录不需要播放信息流音视频；二维码与验证码图片仍正常加载。
  if (resourceType === "media") return true;
  if (platform !== "douyin") return false;
  try {
    const url = new URL(value);
    const pathname = url.pathname;
    const host = url.hostname.toLowerCase();
    // 推荐流封面是登录页最重的一批无关资源；登录二维码使用 passport
    // 资源和同源接口，不落在 douyinpic.com。
    if (
      resourceType === "image" &&
      (host === "douyinpic.com" || host.endsWith(".douyinpic.com"))
    )
      return true;
    return /^\/aweme\/v1\/web\/(?:tab|follow)\/feed\//i.test(pathname);
  } catch {
    return false;
  }
}
