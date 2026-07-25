import { describe, expect, it } from "vitest";
import { formatEngineVersion } from "../../../src/renderer/components/settings/capture/engine-version";

/**
 * 两个引擎的版本标识本质都是日期，状态行统一按 YYYY-MM-DD 展示，
 * 不再各带 v / 构建 之类的装饰词。
 */
describe("formatEngineVersion", () => {
  it("yt-dlp 的点分日期归一成横线日期", () => {
    expect(formatEngineVersion("2026.07.04")).toBe("2026-07-04");
  });

  it("ffmpeg 每日构建取尾部的构建日期", () => {
    expect(formatEngineVersion("N-125753-g6095372a70-20260724")).toBe(
      "2026-07-24",
    );
  });

  it("主进程给出的裸构建日期同样归一", () => {
    expect(formatEngineVersion("20260724")).toBe("2026-07-24");
  });

  it("ffmpeg 发行版没有日期，退回版本号本身", () => {
    expect(formatEngineVersion("8.1-essentials_build-www.gyan.dev")).toBe("8.1");
    expect(formatEngineVersion("7.1.1-full_build")).toBe("7.1.1");
  });

  it("yt-dlp nightly 带时间戳时不截断成日期，避免两个构建显示成同一个", () => {
    expect(formatEngineVersion("2026.07.04.232715")).toBe("2026.07.04.232715");
  });

  it("认不出的版本串原样返回", () => {
    expect(formatEngineVersion("  unknown-build  ")).toBe("unknown-build");
  });
});
