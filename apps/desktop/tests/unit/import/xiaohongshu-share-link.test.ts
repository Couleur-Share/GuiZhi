import { describe, expect, it, vi } from "vitest";
import { detectVideoPlatform } from "@guizhi/shared/utils/video-platforms";
import { resolveSourcePlatform } from "@guizhi/shared/utils/source-platforms";
import {
  detectPlatformCapturePlatform,
  isAllowedPlatformUrl,
} from "@guizhi/shared/utils/platform-capture";
import {
  parseCaptureDraft,
  resolveCaptureAction,
} from "../../../src/renderer/components/capture/capture-utils";

vi.mock("../../../src/main/services/platform-capture/browser-capture", () => ({
  BrowserCaptureService: class {},
  PlatformCaptureError: class extends Error {},
}));
vi.mock("electron", () => ({ session: { defaultSession: {} }, app: {} }));

import { platformFromAuthenticatedUrl } from "../../../src/main/services/platform-capture/authenticated-platforms";

const SHARE_URL = "https://xhslink.cn/o/2Vlbpe4bxkO";
const SHARE_TEXT = `如何具体安排碳水蛋白质脂肪的量以及调整 碳水的调整... ${SHARE_URL} 先复制这段口令，再去【小红书】打开笔记~`;

describe("小红书 cn 分享短链", () => {
  it("整段口令默认提交链接，同时保留手动存笔记的选择", () => {
    const draft = parseCaptureDraft(SHARE_TEXT);
    expect(draft).toEqual({
      kind: "mixed",
      urls: [SHARE_URL],
      text: SHARE_TEXT,
      prefer: "urls",
    });
    expect(resolveCaptureAction(draft, null)).toEqual({
      kind: "urls",
      urls: [SHARE_URL],
    });
    expect(resolveCaptureAction(draft, "text")).toEqual({
      kind: "text",
      text: SHARE_TEXT,
    });
  });

  it.each([SHARE_URL, "https://xhslink.com/a/abc123"])(
    "%s 在采集、来源归类与登录态路径中都识别为小红书",
    (url) => {
      expect(detectVideoPlatform(url)).toBe("xiaohongshu");
      expect(resolveSourcePlatform("url", url)).toBe("xiaohongshu");
      expect(isAllowedPlatformUrl("xiaohongshu", url)).toBe(true);
      expect(detectPlatformCapturePlatform(url)).toBe("xiaohongshu");
      expect(platformFromAuthenticatedUrl(url)).toBe("xiaohongshu");
    },
  );

  it.each([
    "https://fakexhslink.cn/o/abc",
    "https://xhslink.cn.evil.test/o/abc",
    "https://xhslink.cn@evil.test/o/abc",
  ])("相似域名不能进入小红书采集路径：%s", (url) => {
    expect(detectVideoPlatform(url)).toBeNull();
    expect(isAllowedPlatformUrl("xiaohongshu", url)).toBe(false);
    expect(platformFromAuthenticatedUrl(url)).toBeNull();
  });

  it("登录态采集继续拒绝 HTTP", () => {
    expect(
      isAllowedPlatformUrl("xiaohongshu", SHARE_URL.replace("https:", "http:")),
    ).toBe(false);
  });
});
