import { isAllowedPlatformUrl } from "@guizhi/shared/utils/platform-capture";
import type { ElectronCapturePage } from "./electron-capture-runtime";
import { PlatformCaptureError } from "./capture-error";

/** 只消费目标作品，忽略同页推荐流和不含详情的 loader；限制遍历规模。 */
export function douyinDetailResponseHtml(
  payloads: unknown[],
  awemeId: string,
): string | null {
  let remaining = 4000;
  function find(value: unknown, depth = 0): Record<string, unknown> | null {
    if (--remaining < 0 || depth > 10 || !value || typeof value !== "object")
      return null;
    const record = value as Record<string, unknown>;
    if (record.aweme_id === awemeId && (record.video || record.images))
      return record;
    for (const child of Object.values(record)) {
      const item = find(child, depth + 1);
      if (item) return item;
    }
    return null;
  }
  // 最新的详情响应优先，避免早期空 loader 或推荐结果覆盖实际作品。
  for (const payload of [...payloads].reverse()) {
    remaining = 4000;
    const item = find(payload);
    if (item) {
      const data = {
        loaderData: { capture: { videoInfoRes: { item_list: [item] } } },
      };
      return `<script>window._ROUTER_DATA = ${JSON.stringify(data)}</script>`;
    }
  }
  return null;
}

/** 由官方页面自行完成加载，不拼装签名接口或导出会话凭证。 */
export async function captureDouyinDetailPage(
  page: ElectronCapturePage,
  awemeId: string,
  signal: AbortSignal,
): Promise<string> {
  const checkCanceled = () => {
    if (signal.aborted) throw new PlatformCaptureError("canceled", "已取消");
  };
  checkCanceled();
  const payloads = page.startJsonCapture("douyin");
  try {
    await page.goto(`https://www.douyin.com/video/${awemeId}`, {
      waitUntil: "domcontentloaded",
      timeout: 30_000,
    });
    // 详情常晚于 DOMContentLoaded 到达，等目标响应而不是固定睡眠后取首条 JSON。
    for (let attempt = 0; attempt < 40; attempt++) {
      checkCanceled();
      if (!isAllowedPlatformUrl("douyin", page.url())) {
        throw new PlatformCaptureError(
          "platform_changed",
          "抖音作品页跳转到了不支持的地址",
        );
      }
      const html = douyinDetailResponseHtml(payloads, awemeId);
      if (html) return html;
      await page.waitForTimeout(500);
    }
    checkCanceled();
    const verification = await page.evaluate(() => {
      const text = document.body?.innerText ?? "";
      return /请完成下方验证|拖动滑块|安全验证|验证码/.test(text);
    });
    throw new PlatformCaptureError(
      verification ? "verification_required" : "platform_changed",
      verification
        ? "抖音要求安全验证，请在平台登录窗口完成验证后重试"
        : "抖音页面未返回目标作品详情，请检查网络或在平台登录窗口检查访问状态后重试",
    );
  } finally {
    page.stopJsonCapture();
  }
}
