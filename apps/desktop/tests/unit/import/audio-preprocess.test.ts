import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

import { prepareAudioForTranscription } from "../../../src/main/services/media/audio-preprocess";

let workDir: string;

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-preprocess-test-"));
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("prepareAudioForTranscription", () => {
  it("ffmpeg 不存在 → 静默降级为原文件直传", async () => {
    const inputPath = path.join(workDir, "audio.m4a");
    fs.writeFileSync(inputPath, "fake-audio");

    const prepared = await prepareAudioForTranscription(
      inputPath,
      path.join(workDir, "definitely-not-ffmpeg.exe"),
    );
    expect(prepared.filePath).toBe(inputPath);
    // 降级路径的 cleanup 是空操作，不应影响原文件
    prepared.cleanup();
    expect(fs.existsSync(inputPath)).toBe(true);
  });

  it("已取消信号向上传播（不吞掉中断）", async () => {
    const inputPath = path.join(workDir, "audio.m4a");
    fs.writeFileSync(inputPath, "fake-audio");
    const controller = new AbortController();
    controller.abort();

    await expect(
      prepareAudioForTranscription(
        inputPath,
        path.join(workDir, "definitely-not-ffmpeg.exe"),
        controller.signal,
      ),
    ).rejects.toThrow("已取消");
  });
});
