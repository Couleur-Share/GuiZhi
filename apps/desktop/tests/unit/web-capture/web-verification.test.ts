import { describe, expect, it } from "vitest";
import { needsWebVerification } from "@guizhi/shared/utils/web-verification";
import type { ImportTask } from "@guizhi/shared/types";
import { isWebVerificationPage } from "../../../src/main/services/web-capture/web-static-route";

const task = {
  status: "failed",
  sourceKind: "url",
  sourceInput: "https://blog.thevernaldawn.com/p/19",
} as ImportTask;
describe("通用网页验证识别", () => {
  it("旧失败任务仍提供验证入口", () => {
    expect(
      needsWebVerification({
        ...task,
        displayName: "Just a moment...",
        error: "网站拒绝访问或限制请求频率",
      }),
    ).toBe(true);
    expect(
      needsWebVerification({
        ...task,
        error: "web_verification_required：需要网页验证",
      }),
    ).toBe(true);
  });
  it("普通403和429、运行中任务、专属平台不误提供入口", () => {
    expect(needsWebVerification({ ...task, error: "HTTP 403" })).toBe(false);
    expect(needsWebVerification({ ...task, error: "HTTP 429" })).toBe(false);
    expect(
      needsWebVerification({
        ...task,
        status: "processing",
        displayName: "Just a moment...",
      }),
    ).toBe(false);
    expect(
      needsWebVerification({
        ...task,
        sourceInput: "https://www.nodeseek.com/post-123-1",
        displayName: "Just a moment...",
      }),
    ).toBe(false);
  });
  it("识别挑战标记，不把文章里引用的文字或普通验证控件当验证页", () => {
    expect(isWebVerificationPage("<title>Just a moment...</title>")).toBe(true);
    expect(
      isWebVerificationPage(
        '<title>博客</title><div id="challenge-stage"></div>',
      ),
    ).toBe(true);
    expect(
      isWebVerificationPage(
        '<title>Cloudflare教程</title><article>Just a moment</article><div class="cf-turnstile"></div>',
      ),
    ).toBe(false);
  });
});
