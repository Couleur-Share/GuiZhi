import fs from "node:fs";
import promises from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureRuntimePaths, getImagesDir, resetRuntimePaths } from "../../src/main/runtime-paths";
import { assertThemedReadingImagesHealthy, inspectThemedReadingImages, resetThemedReadingImageHealthCache } from "../../src/main/services/themed-reading/image-health";
import { themedAsset, themedVersion } from "./db/themed-reading-fixture";

const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aE0sAAAAASUVORK5CYII=", "base64");
let root: string;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-theme-health-"));
  configureRuntimePaths({userDataPath: root});
  fs.mkdirSync(getImagesDir(), {recursive: true});
  resetThemedReadingImageHealthCache();
});
afterEach(() => {
  vi.restoreAllMocks();
  resetThemedReadingImageHealthCache();
  resetRuntimePaths();
  fs.rmSync(root, {recursive: true, force: true});
});

describe("主题页图片读取健康检查", () => {
  it("缺图只降级返回副本，保留正文并提供可重试原因", async () => {
    const version = themedVersion("item", {assets: [themedAsset("missing.png", png)]});
    const view = await inspectThemedReadingImages(version);
    expect(view.assets[0]).toMatchObject({status: "failed", error: expect.stringContaining("已丢失")});
    expect(view.warnings[0]).toContain("已丢失");
    expect(version.assets[0].status).toBe("ready");
    expect(version.warnings).toEqual([]);
    expect(view.source.content).toBe(version.source.content);
    await expect(assertThemedReadingImagesHealthy(version)).rejects.toThrow("当前页面保持不变");
  });

  it("并行和重复读取复用文件身份+预期哈希缓存，文件修复后重新校验", async () => {
    const asset = themedAsset("cached.png", png), file = path.join(getImagesDir(), asset.fileName!);
    fs.writeFileSync(file, png);
    const version = themedVersion("item", {assets: [asset]});
    const read = vi.spyOn(promises, "readFile");
    const [first, second] = await Promise.all([inspectThemedReadingImages(version), inspectThemedReadingImages(version)]);
    expect(first.assets[0].status).toBe("ready");
    expect(second.assets[0].status).toBe("ready");
    await inspectThemedReadingImages(version);
    expect(read).toHaveBeenCalledTimes(1);
    const changed = Buffer.from(png); changed[changed.length - 1] ^= 1;
    fs.writeFileSync(file, changed);
    fs.utimesSync(file, new Date(), new Date(Date.now() + 5000));
    const damaged = await inspectThemedReadingImages(version);
    expect(damaged.assets[0].status).toBe("failed");
    expect(damaged.assets[0].error).toContain("校验失败");
    expect(read).toHaveBeenCalledTimes(2);
    const fixed = await inspectThemedReadingImages({...version, assets: [themedAsset("cached.png", changed)]});
    expect(fixed.assets[0].status).toBe("ready");
    expect(read).toHaveBeenCalledTimes(3);
  });

  it("完整页可以恢复，持久化的未完成素材仍不能冒充健康", async () => {
    const asset = themedAsset("valid.png", png);
    fs.writeFileSync(path.join(getImagesDir(), asset.fileName!), png);
    await expect(assertThemedReadingImagesHealthy(themedVersion("item", {assets: [asset]}))).resolves.toBeUndefined();
    await expect(assertThemedReadingImagesHealthy(themedVersion("item", {assets: [{...asset, status: "failed", error: "模型请求失败"}]}))).rejects.toThrow("模型请求失败");
    await expect(assertThemedReadingImagesHealthy(themedVersion("item"))).resolves.toBeUndefined();
  });
});
