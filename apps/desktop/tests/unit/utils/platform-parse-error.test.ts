import { describe, expect, it } from "vitest";
import {
  getPlatformParseCode,
  PlatformParseError,
  splitPlatformParseErrorMessage,
} from "@guizhi/shared/utils/platform-parse-error";

describe("PlatformParseError", () => {
  it("message 带稳定 [code] 前缀，可被抽出", () => {
    const error = new PlatformParseError(
      "structure_missing",
      "分享页未返回作品数据",
    );
    expect(error.message).toBe("[structure_missing] 分享页未返回作品数据");
    expect(error.code).toBe("structure_missing");
    expect(getPlatformParseCode(error)).toBe("structure_missing");
  });

  it("从普通 Error 文案前缀也能认出码", () => {
    expect(
      getPlatformParseCode(new Error("[token_invalid] 缺少令牌")),
    ).toBe("token_invalid");
    expect(getPlatformParseCode(new Error("网络错误"))).toBeNull();
  });

  it("split 拆出 code 与正文", () => {
    expect(
      splitPlatformParseErrorMessage(
        "[guest_denied] 需要登录后才能查看该帖",
      ),
    ).toEqual({
      code: "guest_denied",
      body: "需要登录后才能查看该帖",
    });
    expect(splitPlatformParseErrorMessage("普通错误")).toEqual({
      code: null,
      body: "普通错误",
    });
  });
});
