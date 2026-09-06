import type { WebCaptureError } from "@guizhi/shared/types";
export function webCaptureError(error: unknown): WebCaptureError {
  const text = error instanceof Error ? error.message : String(error);
  let code: WebCaptureError["code"] = /校验|清单|组件文件/.test(text)
    ? "damaged"
    : /timeout|超时/i.test(text)
      ? "timeout"
      : /取消|abort/i.test(text)
        ? "canceled"
        : /内网|本地网络|private|internal network|local network|超出.*范围|不支持.*协议|目录.*越界/i.test(
              text,
            )
          ? "security"
          : /验证码|人机验证/.test(text)
            ? "captcha"
            : /登录/.test(text)
              ? "login"
              : /HTTP (403|429)|拒绝访问|限流/.test(text)
                ? "restricted"
                : /超过.*(MiB|限制)|超限|预算已用尽/.test(text)
                  ? "incomplete"
                  : "network";
  const explicit =
    error instanceof Error
      ? (error.cause as { webCaptureCode?: string })?.webCaptureCode
      : undefined;
  if (
    explicit &&
    [
      "damaged",
      "canceled",
      "timeout",
      "security",
      "captcha",
      "login",
      "restricted",
      "incomplete",
      "network",
      "empty",
    ].includes(explicit)
  )
    code = explicit as WebCaptureError["code"];
  // 用户可复制定位原因；隐藏 URL 中查询参数和账号密码。
  const message = text
    .replace(/https?:\/\/[^\s]+/g, (raw) => {
      try {
        const url = new URL(raw);
        return url.origin + url.pathname;
      } catch {
        return "[网址]";
      }
    })
    .slice(0, 1000);
  return {
    code,
    message,
    retryable: ["network", "timeout", "incomplete"].includes(code),
  };
}
