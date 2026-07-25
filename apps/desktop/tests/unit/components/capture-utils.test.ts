import { describe, expect, it } from "vitest";
import {
  isHttpUrlLike,
  parseCaptureDraft,
} from "../../../src/renderer/components/capture/capture-utils";

describe("isHttpUrlLike", () => {
  it("识别单个 http(s) 链接", () => {
    expect(isHttpUrlLike("https://example.com/a?b=1")).toBe(true);
    expect(isHttpUrlLike("  http://example.com  ")).toBe(true);
  });

  it("拒绝非 http 协议与非链接", () => {
    expect(isHttpUrlLike("file:///C:/x.txt")).toBe(false);
    expect(isHttpUrlLike("ftp://example.com")).toBe(false);
    expect(isHttpUrlLike("随手记一笔")).toBe(false);
    expect(isHttpUrlLike("")).toBe(false);
  });
});

describe("parseCaptureDraft", () => {
  it("空输入", () => {
    expect(parseCaptureDraft("   \n  ")).toEqual({ kind: "empty" });
  });

  it("单个链接", () => {
    expect(parseCaptureDraft(" https://example.com/a ")).toEqual({
      kind: "urls",
      urls: ["https://example.com/a"],
    });
  });

  it("多行链接批量导入（此前整段会被存成一条文本笔记）", () => {
    const draft = `https://example.com/a
https://example.com/b

https://example.com/c`;
    expect(parseCaptureDraft(draft)).toEqual({
      kind: "urls",
      urls: [
        "https://example.com/a",
        "https://example.com/b",
        "https://example.com/c",
      ],
    });
  });

  it("同一行的多个链接也拆开", () => {
    expect(
      parseCaptureDraft("https://example.com/a https://example.com/b"),
    ).toEqual({
      kind: "urls",
      urls: ["https://example.com/a", "https://example.com/b"],
    });
  });

  it("批内重复链接去重", () => {
    expect(
      parseCaptureDraft("https://example.com/a\nhttps://example.com/a"),
    ).toEqual({ kind: "urls", urls: ["https://example.com/a"] });
  });

  it("混进说明文字时整体按文本保存，不丢上下文", () => {
    const draft = "明天看这个 https://example.com/a";
    expect(parseCaptureDraft(draft)).toEqual({
      kind: "text",
      text: draft,
    });
  });

  it("纯文本按文本保存", () => {
    expect(parseCaptureDraft("会议纪要\n第一条")).toEqual({
      kind: "text",
      text: "会议纪要\n第一条",
    });
  });
});
