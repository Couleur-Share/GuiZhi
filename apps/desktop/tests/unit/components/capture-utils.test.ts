import { describe, expect, it } from "vitest";
import {
  extractUrlsFromText,
  isHttpUrlLike,
  parseCaptureDraft,
  resolveCaptureAction,
} from "../../../src/renderer/components/capture/capture-utils";

/** 抖音「复制打开抖音」口令原样（尾部那串是分享校验噪音，不是链接） */
const DOUYIN_SHARE_TEXT =
  "0.02 复制打开抖音，看看【六叔ultra的作品】4 个提示词，让 AI 像博士一样帮你调研🔥 用A... https://v.douyin.com/0lZNY93J6Ck/ :3pm t@E.Ul FhB:/ 12/15";

/** 小红书 PC 版分享口令原样：【】包住标题，链接带 xsec_token（含 `=`） */
const XHS_PC_SHARE_TEXT =
  "84 【AI漫剧培训实战课程丨12天🉑独立出片 - 司晨视觉 | 小红书 - 你的生活兴趣社区】 😆 5wtLOclJhAjzJzW 😆 https://www.xiaohongshu.com/discovery/item/6a59e7f3000000000301fc49?source=webshare&xhsshare=pc_web&xsec_token=CBai8aBe1zi_hNlq7nojlsTLX0Z9M9ZhGy22tGMBTxhUI=&xsec_source=pc_share";

const XHS_PC_SHARE_URL =
  "https://www.xiaohongshu.com/discovery/item/6a59e7f3000000000301fc49?source=webshare&xhsshare=pc_web&xsec_token=CBai8aBe1zi_hNlq7nojlsTLX0Z9M9ZhGy22tGMBTxhUI=&xsec_source=pc_share";

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

describe("extractUrlsFromText", () => {
  it("抖音分享口令里的短链", () => {
    expect(extractUrlsFromText(DOUYIN_SHARE_TEXT)).toEqual([
      "https://v.douyin.com/0lZNY93J6Ck/",
    ]);
  });

  it("小红书 PC 版口令：xsec_token 尾部的 = 要留住，去掉就打不开笔记", () => {
    expect(extractUrlsFromText(XHS_PC_SHARE_TEXT)).toEqual([
      XHS_PC_SHARE_URL,
    ]);
  });

  it("小红书口令的全角逗号紧贴链接，不能被吞进 URL", () => {
    expect(
      extractUrlsFromText(
        "78 【标题 - 作者】😆 http://xhslink.com/a/abc123，复制本条信息，打开【小红书】查看",
      ),
    ).toEqual(["http://xhslink.com/a/abc123"]);
  });

  it("句尾的半角句读不算链接的一部分", () => {
    expect(extractUrlsFromText("see https://example.com/a.")).toEqual([
      "https://example.com/a",
    ]);
    expect(extractUrlsFromText("(见 https://example.com/a)")).toEqual([
      "https://example.com/a",
    ]);
  });

  it("链接自带的配平括号要留住", () => {
    expect(
      extractUrlsFromText("参考 https://ex.com/wiki/Foo_(bar) 这页"),
    ).toEqual(["https://ex.com/wiki/Foo_(bar)"]);
  });

  it("按出现顺序去重", () => {
    expect(
      extractUrlsFromText(
        "a https://ex.com/1 b https://ex.com/2 c https://ex.com/1",
      ),
    ).toEqual(["https://ex.com/1", "https://ex.com/2"]);
  });

  it("没有链接时为空", () => {
    expect(extractUrlsFromText("会议纪要 12/15 t@E.Ul FhB:/")).toEqual([]);
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

  it("抖音分享口令默认采集其中的链接", () => {
    expect(parseCaptureDraft(DOUYIN_SHARE_TEXT)).toEqual({
      kind: "mixed",
      urls: ["https://v.douyin.com/0lZNY93J6Ck/"],
      text: DOUYIN_SHARE_TEXT,
      prefer: "urls",
    });
  });

  it("其他有专用连接器的平台同样默认采集", () => {
    expect(parseCaptureDraft("【标题】 https://b23.tv/abc123")).toMatchObject({
      kind: "mixed",
      prefer: "urls",
    });
    expect(
      parseCaptureDraft("这帖有意思 https://www.v2ex.com/t/1227616"),
    ).toMatchObject({ kind: "mixed", prefer: "urls" });
  });

  it("小红书两种分享口令都默认采集链接", () => {
    expect(parseCaptureDraft(XHS_PC_SHARE_TEXT)).toEqual({
      kind: "mixed",
      urls: [XHS_PC_SHARE_URL],
      text: XHS_PC_SHARE_TEXT,
      prefer: "urls",
    });
    expect(
      parseCaptureDraft(
        "78 【标题 - 作者】😆 http://xhslink.com/a/abc123，复制本条信息，打开【小红书】查看",
      ),
    ).toMatchObject({
      kind: "mixed",
      urls: ["http://xhslink.com/a/abc123"],
      prefer: "urls",
    });
  });

  it("普通网页链接混进说明文字时仍默认存文本，不丢上下文", () => {
    const draft = "明天看这个 https://example.com/a";
    expect(parseCaptureDraft(draft)).toEqual({
      kind: "mixed",
      urls: ["https://example.com/a"],
      text: draft,
      prefer: "text",
    });
  });

  it("B 站专栏不是视频页，按普通网页处理", () => {
    expect(
      parseCaptureDraft("看看这个 https://www.bilibili.com/read/cv123"),
    ).toMatchObject({ kind: "mixed", prefer: "text" });
  });

  it("纯文本按文本保存", () => {
    expect(parseCaptureDraft("会议纪要\n第一条")).toEqual({
      kind: "text",
      text: "会议纪要\n第一条",
    });
  });
});

describe("resolveCaptureAction", () => {
  it("非 mixed 的判定原样透传", () => {
    const urls = parseCaptureDraft("https://example.com/a");
    expect(resolveCaptureAction(urls, null)).toEqual(urls);
    expect(resolveCaptureAction({ kind: "empty" }, "urls")).toEqual({
      kind: "empty",
    });
  });

  it("未改判时按 prefer 走", () => {
    expect(
      resolveCaptureAction(parseCaptureDraft(DOUYIN_SHARE_TEXT), null),
    ).toEqual({ kind: "urls", urls: ["https://v.douyin.com/0lZNY93J6Ck/"] });
    expect(
      resolveCaptureAction(
        parseCaptureDraft("明天看 https://example.com/a"),
        null,
      ),
    ).toEqual({ kind: "text", text: "明天看 https://example.com/a" });
  });

  it("改判后按用户选的走", () => {
    expect(
      resolveCaptureAction(parseCaptureDraft(DOUYIN_SHARE_TEXT), "text"),
    ).toEqual({ kind: "text", text: DOUYIN_SHARE_TEXT });
    expect(
      resolveCaptureAction(
        parseCaptureDraft("明天看 https://example.com/a"),
        "urls",
      ),
    ).toEqual({ kind: "urls", urls: ["https://example.com/a"] });
  });
});
