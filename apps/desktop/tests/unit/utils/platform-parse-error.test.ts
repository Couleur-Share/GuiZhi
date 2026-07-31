import { describe, expect, it } from "vitest";
import {
  getPlatformParseCode,
  PlatformParseError,
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
});
