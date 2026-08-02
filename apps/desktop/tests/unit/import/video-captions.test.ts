import { describe, expect, it } from "vitest";
import { parseCaptionText } from "../../../src/main/services/import/video-captions";

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
});
