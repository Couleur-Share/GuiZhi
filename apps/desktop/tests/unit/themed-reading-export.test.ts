// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import path from "node:path";
import { parseHTML } from "linkedom";
import type { KnowledgeItemDB } from "@guizhi/db";
import { THEME_TEST_PNG, themeTestAsset, themeTestDirectory, themeTestHash, themeTestPage, removeThemeTestDirectory } from "./themed-reading-test-fixtures";

const state = vi.hoisted(() => ({ directory: "" }));
vi.mock("../../src/main/runtime-paths", () => ({ getImagesDir: () => state.directory, getVideosDir: () => state.directory }));
vi.mock("../../src/main/services/import/safe-fetch", () => ({ downloadToTempFile: vi.fn() }));
vi.mock("../../src/main/services/illustration/image-gen", () => ({ generateImage: vi.fn() }));
vi.mock("../../src/main/services/web-capture/snapshot-assets", () => ({ leasedSnapshotAssets: () => new Set() }));
import { exportThemedReadingHtml } from "../../src/main/services/themed-reading/export";
import { cleanupOrphanAssets } from "../../src/main/services/asset-cleanup";

beforeEach(async () => { state.directory = await themeTestDirectory(); });
afterEach(async () => { vi.restoreAllMocks(); await removeThemeTestDirectory(state.directory); });
const readyAsset = () => themeTestAsset({ status: "ready", fileName: "fixture.png", sha256: themeTestHash(), bytes: THEME_TEST_PNG.length });

describe("主题页单文件 HTML 导出", () => {
  it("真实PNG逐字节内嵌且离线产物不携带脚本、实例或本地协议", async () => {
    await fs.writeFile(path.join(state.directory, "fixture.png"), THEME_TEST_PNG);
    const page = themeTestPage({ assets: [readyAsset()] });
    const html = await exportThemedReadingHtml(page);
    const doc = parseHTML(html).document;
    const encoded = doc.querySelector("img").getAttribute("src");
    expect(encoded).toMatch(/^data:image\/png;base64,/);
    expect(Buffer.from(encoded.split(",")[1], "base64")).toEqual(THEME_TEST_PNG);
    expect(doc.querySelectorAll("script")).toHaveLength(0);
    expect(html).not.toContain("data-instance");
    expect(html).not.toContain("local-image:");
    expect(html).not.toContain(state.directory);
    expect(doc.body.textContent).toContain("完整正文与限定条件");
    expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]').getAttribute("content")).toContain("script-src 'none'");
  });

  it("无图导出可处理失败或丢失素材，并移除正文中的本地图片地址", async () => {
    const page = themeTestPage({ assets: [themeTestAsset({ role: "original", fileName: "missing.png", originalUrl: "local-image://missing.png", status: "failed" })] });
    page.source.blocks[1].html += '<img src="local-image://missing.png" alt="原文啤酒图">';
    const html = await exportThemedReadingHtml(page, true);
    expect(html).not.toContain("local-image:");
    expect(html).not.toContain("data:image");
    expect(html).toContain("原文啤酒图");
    expect(html).toContain("完整正文与限定条件");
  });

  it("缺失、待生成和哈希损坏分别阻止带图导出", async () => {
    await expect(exportThemedReadingHtml(themeTestPage())).rejects.toThrow("素材不完整");
    await expect(exportThemedReadingHtml(themeTestPage({ assets: [readyAsset()] }))).rejects.toThrow("ENOENT");
    await fs.writeFile(path.join(state.directory, "fixture.png"), THEME_TEST_PNG);
    await expect(exportThemedReadingHtml(themeTestPage({ assets: [{ ...readyAsset(), sha256: "0".repeat(64) }] }))).rejects.toThrow("图片校验失败");
  });

  it("拒绝编码后超过100MiB的产物并在失败后释放资产租约", async () => {
    const file = path.join(state.directory, "large.png");
    await fs.writeFile(file, THEME_TEST_PNG);
    await fs.truncate(file, 20 * 1024 * 1024);
    const assets = Array.from({ length: 4 }, (_, index) => themeTestAsset({ id: index ? `image-${index}` : "hero", status: "ready", fileName: "large.png" }));
    await expect(exportThemedReadingHtml(themeTestPage({ assets }))).rejects.toThrow("100 MiB");
    const items = { listReferencedAssets: () => new Set<string>() } as unknown as KnowledgeItemDB;
    expect(cleanupOrphanAssets(items, ["large.png"])).toBe(1);
  });
});
