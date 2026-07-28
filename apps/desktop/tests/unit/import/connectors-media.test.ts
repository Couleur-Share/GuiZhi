import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { extractContent } from "../../../src/main/services/import/connectors";
import {
  configureRuntimePaths,
  resetRuntimePaths,
} from "../../../src/main/runtime-paths";

let workDir: string;

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-conn-media-"));
  configureRuntimePaths({ userDataPath: workDir });
});

afterEach(() => {
  resetRuntimePaths();
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("媒体文件导入连接器", () => {
  it("图片：资产化拷贝 + local-image 引用 + itemType=image", async () => {
    const sourcePath = path.join(workDir, "截图 2026.png");
    fs.writeFileSync(sourcePath, "png-bytes");

    const extracted = await extractContent("file", sourcePath);
    expect(extracted.itemType).toBe("image");
    expect(extracted.title).toBe("截图 2026.png");
    expect(extracted.sourceUri).toBe(path.resolve(sourcePath));

    const match = extracted.content.match(/local-image:\/\/([A-Za-z0-9_.-]+)/);
    expect(match).toBeTruthy();
    expect(
      fs.existsSync(path.join(workDir, "data", "assets", "images", match![1])),
    ).toBe(true);
  });

  it("同一文件重复导入产出同一份正文，队列据此判重", async () => {
    const sourcePath = path.join(workDir, "photo.png");
    fs.writeFileSync(sourcePath, "png-bytes");

    const first = await extractContent("file", sourcePath);
    const second = await extractContent("file", sourcePath);

    // 资产名改成按内容哈希取之后，同一文件两次抽取的正文逐字相同，
    // 于是 computeContentHash 一致，导入队列会把第二次标成「重复」而不是
    // 再建一条条目——此前资产名是随机的，两次正文必然不同，永远判不出重复
    expect(second.content).toBe(first.content);
    expect(
      fs.readdirSync(path.join(workDir, "data", "assets", "images")),
    ).toHaveLength(1);
  });

  it("视频与音频：local-video 引用 + 对应 itemType", async () => {
    const videoPath = path.join(workDir, "demo.mp4");
    fs.writeFileSync(videoPath, "video-bytes");
    const video = await extractContent("file", videoPath);
    expect(video.itemType).toBe("video");
    expect(video.content).toContain("local-video://");

    const audioPath = path.join(workDir, "voice.m4a");
    fs.writeFileSync(audioPath, "audio-bytes");
    const audio = await extractContent("file", audioPath);
    expect(audio.itemType).toBe("audio");
    expect(audio.content).toContain("local-video://");
  });

  it("超过大小上限的图片被拒绝", async () => {
    const bigPath = path.join(workDir, "big.png");
    fs.writeFileSync(bigPath, Buffer.alloc(21 * 1024 * 1024));
    await expect(extractContent("file", bigPath)).rejects.toThrow("上限");
  });

  it("不支持的扩展名报错并提示支持范围", async () => {
    const exePath = path.join(workDir, "tool.exe");
    fs.writeFileSync(exePath, "bin");
    await expect(extractContent("file", exePath)).rejects.toThrow(
      "暂不支持的文件类型",
    );
  });
});
