import { describe, expect, it } from "vitest";
import {
  SOURCE_PLATFORMS,
  isSourcePlatform,
  resolveSourcePlatform,
} from "@guizhi/shared/utils/source-platforms";

describe("resolveSourcePlatform", () => {
  it("按链接归类专有平台", () => {
    expect(
      resolveSourcePlatform("url", "https://www.douyin.com/video/7412345678"),
    ).toBe("douyin");
    expect(
      resolveSourcePlatform(
        "url",
        "https://www.iesdouyin.com/share/video/7412345678/",
      ),
    ).toBe("douyin");
    expect(
      resolveSourcePlatform("url", "https://www.bilibili.com/video/BV1xx411c7mD"),
    ).toBe("bilibili");
    expect(resolveSourcePlatform("url", "https://b23.tv/abc123")).toBe(
      "bilibili",
    );
    expect(
      resolveSourcePlatform("url", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"),
    ).toBe("youtube");
    expect(
      resolveSourcePlatform(
        "url",
        "https://www.xiaohongshu.com/explore/64f0a1b2",
      ),
    ).toBe("xiaohongshu");
    // 分享短链同样归小红书，采集时走的就是它
    expect(resolveSourcePlatform("url", "http://xhslink.com/a/abc123")).toBe(
      "xiaohongshu",
    );
    expect(resolveSourcePlatform("url", "https://www.v2ex.com/t/1227616")).toBe(
      "v2ex",
    );
  });

  it("认不出的链接归到网页兜底桶", () => {
    expect(resolveSourcePlatform("url", "https://example.com/post/1")).toBe(
      "web",
    );
    // B 站专栏不是视频页，采集时走的就是通用网页抓取，归类必须跟着走
    expect(
      resolveSourcePlatform("url", "https://www.bilibili.com/read/cv123456"),
    ).toBe("web");
    // 后缀碰撞不算该平台
    expect(resolveSourcePlatform("url", "https://fakev2ex.com/t/1")).toBe("web");
    expect(
      resolveSourcePlatform("url", "https://fakexiaohongshu.com/explore/1"),
    ).toBe("web");
  });

  it("文件导入归本地，手工文本没有来源", () => {
    expect(resolveSourcePlatform("file", "D:\\notes\\a.md")).toBe("local");
    expect(resolveSourcePlatform("text", null)).toBeNull();
    // url 类型但抽取失败没留下链接时同样没有来源可归
    expect(resolveSourcePlatform("url", null)).toBeNull();
    expect(resolveSourcePlatform("url", "not a url")).toBeNull();
  });

  it("isSourcePlatform 只认清单内的取值", () => {
    for (const platform of SOURCE_PLATFORMS) {
      expect(isSourcePlatform(platform)).toBe(true);
    }
    expect(isSourcePlatform("weibo")).toBe(false);
    expect(isSourcePlatform(null)).toBe(false);
  });
});
