// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import path from "node:path";
import type { KnowledgeItemDB } from "@guizhi/db";
import { THEME_TEST_PNG, themeTestAsset, themeTestDirectory, themeTestHash, removeThemeTestDirectory } from "./themed-reading-test-fixtures";

const mocks = vi.hoisted(() => ({ directory: "", generate: vi.fn(), download: vi.fn() }));
vi.mock("../../src/main/runtime-paths", () => ({ getImagesDir: () => mocks.directory, getVideosDir: () => mocks.directory }));
vi.mock("../../src/main/services/import/safe-fetch", () => ({ downloadToTempFile: mocks.download }));
vi.mock("../../src/main/services/illustration/image-gen", () => ({ generateImage: mocks.generate }));
vi.mock("../../src/main/services/web-capture/snapshot-assets", () => ({ leasedSnapshotAssets: () => new Set() }));
import { generateThemeAsset, imageExtension, localThemeAssetPath, publishThemeImage, THEMED_IMAGE_TIMEOUT_MS } from "../../src/main/services/themed-reading/assets";
import { cleanupOrphanAssets, retainAssetFiles } from "../../src/main/services/asset-cleanup";

beforeEach(async () => { mocks.directory = await themeTestDirectory(); vi.clearAllMocks(); });
afterEach(async () => { await removeThemeTestDirectory(mocks.directory); });
const hashName = () => `theme-${themeTestHash()}.png`;
const db = (references: Set<string>) => ({ listReferencedAssets: () => new Set(references) }) as unknown as KnowledgeItemDB;

describe("主题图片原子发布与失败回收", () => {
  it("检查点执行时图片已经落盘且租约仍保护资源，成功后不运行失败清理", async () => {
    const asset = themeTestAsset(), cleanup = vi.fn();
    const checkpoint = vi.fn(() => {
      expect(asset.status).toBe("ready");
      expect(cleanupOrphanAssets(db(new Set()), [hashName()])).toBe(0);
    });
    await publishThemeImage(THEME_TEST_PNG, asset, new AbortController().signal, checkpoint, cleanup);
    expect(checkpoint).toHaveBeenCalledOnce();
    expect(cleanup).not.toHaveBeenCalled();
    expect(await fs.readFile(localThemeAssetPath(hashName()))).toEqual(THEME_TEST_PNG);
    expect(await fs.readdir(mocks.directory)).toEqual([hashName()]);
    expect(asset).toMatchObject({ sha256: themeTestHash(), bytes: THEME_TEST_PNG.length, status: "ready" });
  });

  it("检查点失败恢复素材状态，通过全库引用清理已发布但无引用的文件", async () => {
    const asset = themeTestAsset({ error: "上次失败" }), previous = { ...asset };
    const cleanup = vi.fn((fileName: string) => cleanupOrphanAssets(db(new Set()), [fileName]));
    await expect(publishThemeImage(THEME_TEST_PNG, asset, new AbortController().signal, () => { throw new Error("数据库提交失败"); }, cleanup)).rejects.toThrow("数据库提交失败");
    expect(asset).toEqual(previous);
    expect(cleanup).toHaveBeenCalledWith(hashName());
    expect(cleanup).toHaveReturnedWith(1);
    expect(await fs.readdir(mocks.directory)).toEqual([]);
  });

  it("失败清理保留被其他条目或另一个工作租约引用的同一图片", async () => {
    await fs.writeFile(localThemeAssetPath(hashName()), THEME_TEST_PNG);
    const references = new Set([hashName()]);
    const cleanup = vi.fn((fileName: string) => cleanupOrphanAssets(db(references), [fileName]));
    const release = retainAssetFiles([hashName()]);
    try {
      await expect(publishThemeImage(THEME_TEST_PNG, themeTestAsset(), new AbortController().signal, () => { throw new Error("失败"); }, cleanup)).rejects.toThrow("失败");
      expect(cleanup).toHaveReturnedWith(0);
      references.clear();
      expect(cleanupOrphanAssets(db(references), [hashName()])).toBe(0);
      expect(await fs.readFile(localThemeAssetPath(hashName()))).toEqual(THEME_TEST_PNG);
    } finally { release(); }
    expect(cleanupOrphanAssets(db(references), [hashName()])).toBe(1);
  });

  it("取消不能提交图片且必须清理暂存文件", async () => {
    const controller = new AbortController(); controller.abort();
    const checkpoint = vi.fn(), cleanup = vi.fn((fileName: string) => cleanupOrphanAssets(db(new Set()), [fileName]));
    await expect(publishThemeImage(THEME_TEST_PNG, themeTestAsset(), controller.signal, checkpoint, cleanup)).rejects.toThrow();
    expect(checkpoint).not.toHaveBeenCalled();
    expect(await fs.readdir(mocks.directory)).toEqual([]);
  });

  it("已有同名但损坏的资源不能被发布为成功，也不能覆盖共享数据", async () => {
    const corrupt = Buffer.from("corrupted existing shared image");
    await fs.writeFile(localThemeAssetPath(hashName()), corrupt);
    const asset = themeTestAsset(), checkpoint = vi.fn();
    const cleanup = vi.fn((fileName: string) => cleanupOrphanAssets(db(new Set([hashName()])), [fileName]));
    await expect(publishThemeImage(THEME_TEST_PNG, asset, new AbortController().signal, checkpoint, cleanup)).rejects.toThrow();
    expect(checkpoint).not.toHaveBeenCalled();
    expect(asset.status).toBe("pending");
    expect(await fs.readFile(localThemeAssetPath(hashName()))).toEqual(corrupt);
  });

  it("调用生图明确关闭隐式重发并沿用主题用量场景", async () => {
    mocks.generate.mockResolvedValue({ data: THEME_TEST_PNG, extension: ".png" });
    const signal = new AbortController().signal;
    const config = { apiUrl: "https://model.example", apiKey: "fixture-only", model: "fixture-image" };
    await generateThemeAsset(themeTestAsset(), "统一麦穗画风", config, signal, () => undefined, () => undefined);
    expect(mocks.generate).toHaveBeenCalledWith(expect.stringContaining("Do not render text"), "16:9", config, { signal, retryDelaysMs: [], scenario: "themedReading", timeoutMs: THEMED_IMAGE_TIMEOUT_MS });
    expect(mocks.generate).toHaveBeenCalledOnce();
  });

  it("主题请求超时明确结果未知，仍只调用一次且不发布文件", async () => {
    mocks.generate.mockRejectedValue(new DOMException("The operation was aborted due to timeout", "TimeoutError"));
    const checkpoint = vi.fn();
    await expect(generateThemeAsset(themeTestAsset(), "麦穗", { apiUrl: "https://model.example", apiKey: "fixture-only", model: "fixture-image" }, new AbortController().signal, checkpoint, () => undefined)).rejects.toThrow("服务端结果未知；手动继续会再次调用");
    expect(mocks.generate).toHaveBeenCalledOnce();
    expect(checkpoint).not.toHaveBeenCalled();
    expect(await fs.readdir(mocks.directory)).toEqual([]);
  });

  it("拒绝越界路径、伪造格式和超过图片上限的字节", () => {
    expect(() => localThemeAssetPath("../private.png")).toThrow("路径");
    expect(() => imageExtension(Buffer.from("<svg>unsafe</svg>"))).toThrow("格式");
    expect(() => imageExtension(Buffer.alloc(20 * 1024 * 1024 + 1))).toThrow("20 MiB");
    expect(imageExtension(THEME_TEST_PNG)).toBe("png");
    expect(path.dirname(localThemeAssetPath("safe.png"))).toBe(mocks.directory);
  });
});
