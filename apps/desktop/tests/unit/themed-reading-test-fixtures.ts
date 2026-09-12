import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import type { ThemedReadingAsset, ThemedReadingVersion } from "@guizhi/shared/types";

/** 实际可解码的 1×1 PNG；测试不访问用户图片目录或模型服务。 */
export const THEME_TEST_PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=", "base64");
export const themeTestHash = (data = THEME_TEST_PNG) => createHash("sha256").update(data).digest("hex");
export function themeTestAsset(patch: Partial<ThemedReadingAsset> = {}): ThemedReadingAsset {
  return { id: "hero", role: "generated", purpose: "顶部插画", prompt: "麦穗与啤酒", alt: "啤酒主题插画", aspectRatio: "16:9", status: "pending", ...patch };
}
export function themeTestPage(patch: Partial<ThemedReadingVersion> = {}): ThemedReadingVersion {
  return {
    id: "page-12345678", itemId: "item-1", sourceKind: "body", role: "current", formatVersion: 1,
    source: { title: "啤酒知识", content: "# 生啤\n\n完整正文与限定条件", fingerprint: "fingerprint", sourceUri: "https://example.com/article", blocks: [
      { id: "b0", markdown: "# 生啤", html: "<h1>生啤</h1>", text: "生啤" },
      { id: "b1", markdown: "完整正文与限定条件", html: "<p>完整正文与限定条件</p>", text: "完整正文与限定条件" },
    ] },
    options: { style: "暖色杂志", generateImages: true, maxImages: 3 },
    design: { direction: "啤酒主题", html: '<img data-theme-asset="hero"><div data-source-block="b0"></div><div data-source-block="b1"></div>', css: "", assets: [] },
    assets: [themeTestAsset()], warnings: [], createdAt: 1, updatedAt: 1, ...patch,
  };
}
export function themeTestDirectory() { return fs.mkdtemp(path.join(os.tmpdir(), "guizhi-theme-test-")); }
export async function removeThemeTestDirectory(directory: string) {
  const relative = path.relative(os.tmpdir(), path.resolve(directory));
  if (!/^guizhi-theme-test-[^\\/]+$/.test(relative)) throw new Error("拒绝清理不属于本测试的目录");
  await fs.rm(directory, { recursive: true, force: true });
}
