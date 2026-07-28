import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SOURCE_PLATFORMS } from "@guizhi/shared/utils/source-platforms";
import {
  getSourcePlatformMeta,
  PlatformIcon,
  SOURCE_PLATFORM_META,
} from "../../../src/renderer/components/library/platform-meta";

/** 品牌 mark 的标准色是近黑，写死就会在深色主题里整个消失 */
const NEAR_BLACK_BRANDS = ["douyin", "v2ex"];

function renderIcon(platform: string) {
  const { container } = render(
    <PlatformIcon platform={platform} className="h-4 w-4" />,
  );
  return container.querySelector("svg");
}

describe("平台图标", () => {
  it("每个平台都配了图标，新增平台漏配会在这里挡下", () => {
    // 侧栏直接按 SOURCE_PLATFORMS 索引这张表，漏一个就是渲染时读 undefined
    for (const platform of SOURCE_PLATFORMS) {
      expect(SOURCE_PLATFORM_META[platform]).toBeDefined();
      expect(renderIcon(platform)).not.toBeNull();
    }
  });

  it("旧版本认不出的平台走兜底而不是抛异常", () => {
    // 取值来自数据库，新版本写进去的平台在旧版本里查不到，抛了就是列表白屏
    expect(getSourcePlatformMeta("mastodon").fallback).toBe("未知来源");
    expect(renderIcon("mastodon")).not.toBeNull();
  });

  it("品牌 logo 用 currentColor 填充，颜色才换得动", () => {
    // 写死 fill 的话下面那条主题适配的断言就形同虚设
    expect(renderIcon("bilibili")?.getAttribute("fill")).toBe("currentColor");
    expect(renderIcon("douyin")?.getAttribute("fill")).toBe("currentColor");
  });

  it("近黑色的品牌跟着主题走，不写死品牌色", () => {
    for (const platform of NEAR_BLACK_BRANDS) {
      expect(SOURCE_PLATFORM_META[platform].colorClass).toBe("text-foreground");
    }
  });

  it("尺寸与着色一起落到图标上", () => {
    // 两者分开拼的话，漏掉着色的表现只是那一个平台悄悄变回黑白，不会报错
    const className = renderIcon("youtube")?.getAttribute("class");
    expect(className).toContain("h-4 w-4");
    expect(className).toContain("text-[#FF0000]");
  });
});
