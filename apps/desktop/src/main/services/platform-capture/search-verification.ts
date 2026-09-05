import type { ElectronCapturePage } from "./electron-capture-runtime";
import { platformSearchUrl } from "@guizhi/shared/utils/platform-capture";
import { PlatformCaptureError } from "./capture-error";
import { readDiscoveryPage } from "./discovery-page";
import { searchResponseRows } from "./search-capture";

/** 用户在官方页面完成登录或验证；Cookie 存在不能证明搜索已经恢复。 */
export async function verifyDouyinSearch(page: ElectronCapturePage, keyword: string, checkCanceled: () => void): Promise<void> {
  await page.goto(platformSearchUrl("douyin", keyword), { timeout: 45_000 });
  let payloads: unknown[] | undefined;
  const started = Date.now();
  while (Date.now() - started < 5 * 60_000) {
    checkCanceled();
    if (page.isClosed()) throw new PlatformCaptureError("browser_closed", "抖音搜索验证窗口已关闭");
    const snapshot = await page.evaluate(readDiscoveryPage, { searchOnly: true });
    if (snapshot.verification && payloads) {
      page.stopJsonCapture();
      payloads = undefined;
      await page.reload({ timeout: 45_000 });
    } else if (!snapshot.verification && !snapshot.loginRequired) {
      if (payloads?.some((payload) => searchResponseRows("douyin", payload) !== null)) return;
      // 验证阶段不挂调试监听，避免干扰跨域验证码 iframe；进入搜索页后再观察一次实际搜索。
      if (!payloads && await page.evaluate((value) => Array.from(document.querySelectorAll("input")).some((input) => input.value.trim().toLowerCase() === value.trim().toLowerCase()), keyword)) {
        payloads = page.startJsonCapture("douyin", { keyword });
        await page.reload({ timeout: 45_000 });
      }
    }
    await page.waitForTimeout(1000);
  }
  throw new PlatformCaptureError("login_timeout", "尚未收到抖音搜索结果，请完成搜索页的登录或安全验证后重试");
}
