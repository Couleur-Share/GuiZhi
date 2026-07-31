/**
 * 平台页面结构异常时写诊断日志（不写完整 HTML）。
 */
import { createHash } from "crypto";
import { logAppError } from "../../diagnostic-log";

export function logPlatformStructureMissing(options: {
  platform: string;
  marker: string;
  html: string;
  action: string;
}): void {
  const htmlHash8 = createHash("sha256")
    .update(options.html, "utf8")
    .digest("hex")
    .slice(0, 8);
  logAppError({
    scope: "import",
    action: options.action,
    message: `[structure_missing] ${options.platform} 页面结构异常`,
    code: "structure_missing",
    platform: options.platform,
    marker: options.marker,
    markerPresent: options.html.includes(options.marker),
    htmlLength: options.html.length,
    htmlHash8,
  });
}
