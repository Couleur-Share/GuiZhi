import { describe, expect, it } from "vitest";
import {
  extractUrlsFromText,
  isHttpUrlLike,
} from "@guizhi/shared/utils/url-text";

/**
 * 这组规则原本长在采集框里，主进程要从视频简介抠链接才上提到 shared。
 * 尾部修剪的每条都是实测踩出来的，用例跟着一起搬，免得上提时悄悄改了行为。
 */
describe("从文本里抠链接", () => {
  it("按出现顺序去重", () => {
    expect(
      extractUrlsFromText("看 https://a.com/x 和 https://b.com 还有 https://a.com/x"),
    ).toEqual(["https://a.com/x", "https://b.com"]);
  });

  it("全角逗号紧贴链接时不吞进后半句", () => {
    // 小红书手机版口令就是这个形状
    expect(
      extractUrlsFromText("http://xhslink.com/a/xxx，复制本条信息打开看看"),
    ).toEqual(["http://xhslink.com/a/xxx"]);
  });

  it("句末的半角句读要削，配平的括号要留", () => {
    expect(extractUrlsFromText("见 https://example.com/doc.")).toEqual([
      "https://example.com/doc",
    ]);
    expect(extractUrlsFromText("(见 https://example.com/a)")).toEqual([
      "https://example.com/a",
    ]);
    expect(extractUrlsFromText("https://zh.wikipedia.org/wiki/Foo_(bar)")).toEqual([
      "https://zh.wikipedia.org/wiki/Foo_(bar)",
    ]);
  });

  it("token 尾部的 = 不能当句读削掉", () => {
    // 小红书 PC 版口令的 xsec_token 以 = 结尾，削了就打不开笔记
    expect(
      extractUrlsFromText("https://www.xiaohongshu.com/explore/abc?xsec_token=I="),
    ).toEqual(["https://www.xiaohongshu.com/explore/abc?xsec_token=I="]);
  });

  it("裸域名不算链接", () => {
    expect(extractUrlsFromText("v.douyin.com/xxx 12/15")).toEqual([]);
  });

  it("isHttpUrlLike 只认整体是单个链接的输入", () => {
    expect(isHttpUrlLike("https://example.com")).toBe(true);
    expect(isHttpUrlLike("  https://example.com  ")).toBe(true);
    expect(isHttpUrlLike("看 https://example.com")).toBe(false);
    expect(isHttpUrlLike("ftp://example.com")).toBe(false);
    expect(isHttpUrlLike("")).toBe(false);
  });
});
