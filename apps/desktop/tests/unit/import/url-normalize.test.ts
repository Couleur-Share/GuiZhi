import { describe, expect, it } from "vitest";
import {
  isHttpUrl,
  normalizeUrl,
} from "../../../src/main/services/import/url-normalize";

describe("isHttpUrl", () => {
  it("接受 http/https 链接", () => {
    expect(isHttpUrl("https://example.com/a")).toBe(true);
    expect(isHttpUrl("http://example.com")).toBe(true);
  });

  it("拒绝非链接输入", () => {
    expect(isHttpUrl("随便一段文本")).toBe(false);
    expect(isHttpUrl("ftp://example.com")).toBe(false);
    expect(isHttpUrl("file:///C:/a.txt")).toBe(false);
    expect(isHttpUrl("")).toBe(false);
  });
});

describe("normalizeUrl", () => {
  it("去 fragment 与跟踪参数", () => {
    expect(
      normalizeUrl(
        "https://Example.com/Article?utm_source=x&utm_medium=y&id=42#section",
      ),
    ).toBe("https://example.com/Article?id=42");
  });

  it("查询参数按键排序，等价 URL 归一到同一形态", () => {
    const left = normalizeUrl("https://example.com/p?b=2&a=1");
    const right = normalizeUrl("https://example.com/p?a=1&b=2");
    expect(left).toBe(right);
  });

  it("去默认端口和尾斜杠", () => {
    expect(normalizeUrl("https://example.com:443/path/")).toBe(
      "https://example.com/path",
    );
    expect(normalizeUrl("http://example.com:80/")).toBe("http://example.com/");
  });

  it("B 站分享参数被剥离", () => {
    expect(
      normalizeUrl(
        "https://www.bilibili.com/video/BV1xx?spm=333.999&vd_source=abc&share_source=copy_web",
      ),
    ).toBe("https://www.bilibili.com/video/BV1xx");
  });

  it("非法输入返回 null", () => {
    expect(normalizeUrl("not a url")).toBeNull();
  });
});
