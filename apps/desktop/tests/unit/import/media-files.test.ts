import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import {
  buildAssetFileName,
  classifyMediaFile,
  mediaProtocolUrl,
  saveMediaAsset,
} from "../../../src/main/services/import/media-files";
import {
  configureRuntimePaths,
  resetRuntimePaths,
} from "../../../src/main/runtime-paths";

describe("classifyMediaFile", () => {
  it("按扩展名分类，大小写不敏感，未知类型返回 null", () => {
    expect(classifyMediaFile(".png")).toBe("image");
    expect(classifyMediaFile(".JPG")).toBe("image");
    expect(classifyMediaFile(".m4a")).toBe("audio");
    expect(classifyMediaFile(".MP4")).toBe("video");
    expect(classifyMediaFile(".pdf")).toBeNull();
    expect(classifyMediaFile("")).toBeNull();
  });
});

describe("buildAssetFileName / mediaProtocolUrl", () => {
  it("资产名仅含十六进制串与小写扩展名（避免协议 URL 编码问题）", () => {
    const name = buildAssetFileName(".PNG");
    expect(name).toMatch(/^import-[0-9a-f]{16}\.png$/);
  });

  it("给了内容哈希就用它，同一份内容永远得到同一个名字", () => {
    const hash = "a".repeat(64);
    expect(buildAssetFileName(".png", "import-", hash)).toBe(
      `import-${"a".repeat(16)}.png`,
    );
    // 形状与随机版一致，协议解析与清理白名单都不受影响
    expect(buildAssetFileName(".png", "import-", hash)).toMatch(
      /^import-[0-9a-f]{16}\.png$/,
    );
  });

  it("不给哈希则随机：AI 配图每张都是新的，去重无从谈起", () => {
    expect(buildAssetFileName(".png", "gen-")).not.toBe(
      buildAssetFileName(".png", "gen-"),
    );
  });

  it("图片走 local-image，音视频走 local-video", () => {
    expect(mediaProtocolUrl("image", "a.png")).toBe("local-image://a.png");
    expect(mediaProtocolUrl("audio", "a.mp3")).toBe("local-video://a.mp3");
    expect(mediaProtocolUrl("video", "a.mp4")).toBe("local-video://a.mp4");
  });
});

describe("saveMediaAsset", () => {
  let workDir: string;

  beforeEach(() => {
    workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-media-test-"));
    configureRuntimePaths({ userDataPath: workDir });
  });

  afterEach(() => {
    resetRuntimePaths();
    fs.rmSync(workDir, { recursive: true, force: true });
  });

  it("图片拷贝进 assets/images，音视频进 assets/videos", async () => {
    const sourcePath = path.join(workDir, "photo.png");
    fs.writeFileSync(sourcePath, "fake-image-bytes");

    const assetFileName = await saveMediaAsset(sourcePath, "image");
    const savedPath = path.join(workDir, "data", "assets", "images", assetFileName);
    expect(fs.readFileSync(savedPath, "utf8")).toBe("fake-image-bytes");

    const audioPath = path.join(workDir, "voice.mp3");
    fs.writeFileSync(audioPath, "fake-audio");
    const audioAsset = await saveMediaAsset(audioPath, "audio");
    expect(
      fs.existsSync(path.join(workDir, "data", "assets", "videos", audioAsset)),
    ).toBe(true);
  });

  it("同一份文件重复导入只落一份拷贝", async () => {
    const first = path.join(workDir, "photo.png");
    const second = path.join(workDir, "照片副本.png");
    fs.writeFileSync(first, "same-bytes");
    fs.writeFileSync(second, "same-bytes");

    const a = await saveMediaAsset(first, "image");
    const b = await saveMediaAsset(second, "image");

    expect(b).toBe(a);
    const imagesDir = path.join(workDir, "data", "assets", "images");
    expect(fs.readdirSync(imagesDir)).toEqual([a]);
    expect(fs.readFileSync(path.join(imagesDir, a), "utf8")).toBe("same-bytes");
  });

  it("内容不同就是两份资产", async () => {
    const first = path.join(workDir, "a.png");
    const second = path.join(workDir, "b.png");
    fs.writeFileSync(first, "bytes-a");
    fs.writeFileSync(second, "bytes-b");

    const a = await saveMediaAsset(first, "image");
    const b = await saveMediaAsset(second, "image");

    expect(b).not.toBe(a);
    expect(
      fs.readdirSync(path.join(workDir, "data", "assets", "images")).sort(),
    ).toEqual([a, b].sort());
  });

  it("并发落同一份文件不互相踩，内容完整", async () => {
    const source = path.join(workDir, "clip.mp4");
    fs.writeFileSync(source, "video-bytes");

    const names = await Promise.all([
      saveMediaAsset(source, "video"),
      saveMediaAsset(source, "video"),
      saveMediaAsset(source, "video"),
    ]);

    expect(new Set(names).size).toBe(1);
    const videosDir = path.join(workDir, "data", "assets", "videos");
    expect(fs.readdirSync(videosDir)).toHaveLength(1);
    expect(fs.readFileSync(path.join(videosDir, names[0]), "utf8")).toBe(
      "video-bytes",
    );
  });
});
