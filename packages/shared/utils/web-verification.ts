import type { ImportTask } from "../types/import";
import { detectPlatformCapturePlatform } from "./platform-capture";

export const WEB_VERIFICATION_REQUIRED = "web_verification_required";

/** 兼容旧任务；403/限流本身不能证明网站要求人机验证。 */
export function needsWebVerification(task: ImportTask): boolean {
  return (
    task.status === "failed" &&
    task.sourceKind === "url" &&
    !detectPlatformCapturePlatform(task.sourceInput) &&
    (task.error?.includes(WEB_VERIFICATION_REQUIRED) ||
      /^(just a moment|verify you are human|人机验证|安全验证)/i.test(
        task.displayName ?? "",
      ) ||
      /网页要求完成验证码/.test(task.error ?? ""))
  );
}
