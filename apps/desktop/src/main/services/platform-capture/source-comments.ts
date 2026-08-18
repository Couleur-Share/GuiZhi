import type { PlatformCapturePlatform } from "@guizhi/shared/types";
import type { BrowserCaptureService, CapturedComment } from "./browser-capture";

/**
 * 各平台的评论补采统一入口。
 *
 * 来源评论只服务于没有独立讨论区的小红书 / 抖音。LINUX DO 楼层直接写入
 * 论坛条目的「讨论」小节，由论坛刷新入口维护，避免重复存储。
 */
export async function captureSourceComments(
  service: BrowserCaptureService,
  platform: PlatformCapturePlatform,
  url: string,
  limit: number,
  signal?: AbortSignal,
): Promise<CapturedComment[]> {
  if (platform === "linuxdo") {
    throw new Error("LINUX DO 楼层请在条目的讨论页刷新");
  }
  return service.captureComments(platform, url, limit, signal);
}
