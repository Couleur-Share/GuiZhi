/** 验证与采集共用平台专用 Electron 会话；Cookie 不经过 IPC。 */
import { isAllowedPlatformUrl } from "@guizhi/shared/utils/platform-capture";
import {
  detectForumPlatform,
  nodeseekCanonicalUrl,
} from "@guizhi/shared/utils/forum-platforms";
import { assertSafeTarget } from "../import/safe-fetch";
import {
  NODESEEK_VERIFICATION_MESSAGE,
  parseNodeseekPage,
} from "../import/nodeseek";
import { PlatformCaptureError } from "./capture-error";
import type { ElectronCapturePage } from "./electron-capture-runtime";

function assertTarget(url: string): string {
  const target = detectForumPlatform(url);
  if (!isAllowedPlatformUrl("nodeseek", url) || target?.platform !== "nodeseek")
    throw new Error("无效的 NodeSeek 帖子地址");
  return target.topicId;
}

export async function readNodeseekPage(
  page: ElectronCapturePage,
  url: string,
  signal: AbortSignal,
  verify = false,
): Promise<string> {
  const topicId = assertTarget(url);
  const pageNumber = Number(new URL(url).pathname.match(/-(\d+)$/)?.[1] ?? 1);
  const verificationMessage = `${NODESEEK_VERIFICATION_MESSAGE}（${url}）`;
  signal.throwIfAborted();
  if (pageNumber > 1) await page.waitForTimeout(800);
  await assertSafeTarget(new URL(url), true);
  await page.goto(url, { waitUntil: "domcontentloaded", timeout: 45_000 });
  const deadline = Date.now() + (verify ? 5 * 60_000 : 12_000);
  let lastError: unknown;
  while (Date.now() < deadline) {
    signal.throwIfAborted();
    if (page.isClosed())
      throw new PlatformCaptureError(
        "browser_closed",
        "NodeSeek 验证窗口已关闭，任务尚未重试",
      );
    const finalUrl = page.url();
    if (!isAllowedPlatformUrl("nodeseek", finalUrl))
      throw new PlatformCaptureError(
        "verification_required",
        verificationMessage,
      );
    let html: string;
    try {
      html = await page.content();
    } catch (error) {
      // 验证完成的导航可能短暂销毁执行上下文，保持窗口等待下一次读取。
      if (!/context.*destroyed|frame.*disposed|navigat/i.test(String(error)))
        throw error;
      await page.waitForTimeout(500);
      continue;
    }
    try {
      if (new URL(finalUrl).pathname !== new URL(url).pathname)
        throw new Error(verificationMessage);
      parseNodeseekPage(html, topicId, pageNumber);
      return html;
    } catch (error) {
      lastError = error;
    }
    await page.waitForTimeout(500);
  }
  if (
    !verify &&
    lastError instanceof Error &&
    !lastError.message.includes("nodeseek_verification_required")
  ) {
    throw new PlatformCaptureError("platform_changed", lastError.message, {
      cause: lastError,
    });
  }
  throw new PlatformCaptureError(
    verify ? "login_timeout" : "verification_required",
    verify ? "NodeSeek 验证等待已超过 5 分钟，请重试" : verificationMessage,
  );
}

export function nodeseekVerificationUrl(value?: string): string {
  if (!value) throw new Error("请从 NodeSeek 失败任务点击「验证并自动采集」");
  const page = Number(new URL(value).pathname.match(/-(\d+)\/?$/)?.[1] ?? 1);
  return nodeseekCanonicalUrl(assertTarget(value), page);
}
