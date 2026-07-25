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
  it("资产名仅含 UUID 与小写扩展名（避免协议 URL 编码问题）", () => {
    const name = buildAssetFileName(".PNG");
    expect(name).toMatch(/^import-[0-9a-f]{16}\.png$/);
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
});
