import fs from "fs";
import path from "path";
import { describe, expect, it, vi } from "vitest";
import { downloadPlatformCaptions, parseCaptionText } from "../../../src/main/services/import/video-captions";

describe("parseCaptionText", () => {
  it("去掉 VTT cue、样式与连续重复，保留可检索的字幕正文", () => {
    const text = parseCaptionText(`WEBVTT

STYLE
::cue { color: lime; }

1
00:00:00.000 --> 00:00:02.000
<c.green>第一句</c>

2
00:00:02.000 --> 00:00:04.000
第一句

3
00:00:04.000 --> 00:00:06.000
第二句 &amp; 更多`);

    expect(text).toBe("第一句\n第二句 & 更多");
  });

  it("WebVTT 文件头元数据与多行 NOTE 注释不进正文", () => {
    const text = parseCaptionText(`WEBVTT
Kind: captions
Language: en

NOTE 第一行注释
第二行注释

00:00:00.000 --> 00:00:02.000
Hello world`);

    expect(text).toBe("Hello world");
  });

  it("文件头后缺空行时，遇到时间轴即恢复读取", () => {
    expect(parseCaptionText("WEBVTT\n00:00:00.000 --> 00:00:01.000\nfirst line")).toBe("first line");
  });
});

describe("字幕语言选择", () => {
  const URL = "https://www.youtube.com/watch?v=example";
  const vtt = (text: string) => `WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n${text}`;
  const writing = (files: Record<string, string>, automatic: boolean) =>
    vi.fn(async (_executable: string, args: string[]) => {
      if (args.includes("--write-auto-subs") !== automatic) return;
      const dir = path.dirname(args[args.indexOf("-o") + 1]);
      for (const [name, body] of Object.entries(files)) fs.writeFileSync(path.join(dir, name), body);
    });

  it("发布者字幕：中文优先，其次英文，不按字母把德语排在英文前", async () => {
    const english = await downloadPlatformCaptions(
      "yt-dlp",
      URL,
      writing({ "caption.de.vtt": vtt("Hallo"), "caption.en.vtt": vtt("Hello"), "caption.es.vtt": vtt("Hola") }, false),
    );
    expect(english).toMatchObject({ source: "platform-subtitles", language: "en", text: "Hello" });

    const chinese = await downloadPlatformCaptions(
      "yt-dlp",
      URL,
      writing({ "caption.de.vtt": vtt("Hallo"), "caption.en.vtt": vtt("Hello"), "caption.zh-Hans.vtt": vtt("你好") }, false),
    );
    expect(chinese).toMatchObject({ language: "zh-Hans", text: "你好" });
  });

  it("自动字幕：原始语言轨（-orig）优先于机器翻译轨", async () => {
    const result = await downloadPlatformCaptions(
      "yt-dlp",
      URL,
      writing(
        { "caption.de.vtt": vtt("Hallo"), "caption.en-orig.vtt": vtt("Hello original"), "caption.zh-Hans.vtt": vtt("你好") },
        true,
      ),
    );
    expect(result).toMatchObject({ source: "platform-ai-captions", language: "en-orig", text: "Hello original" });
  });
});
