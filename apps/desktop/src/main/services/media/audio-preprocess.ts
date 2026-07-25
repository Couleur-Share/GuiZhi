/**
 * 转写前音频预处理：用 ffmpeg 把任意音视频文件转码为 16kHz 单声道 mp3。
 *
 * 收益：上传体积大幅缩小（对本地转写服务与云端 API 都更快），
 * 并把 m4a / webm / mp4 等服务端未必支持的容器统一为通用格式。
 * ffmpeg 不可用或转码失败时静默降级为原文件直传（保持原有行为）。
 */
import { spawn } from "child_process";
import { randomUUID } from "crypto";
import fs from "fs";
import os from "os";
import path from "path";

const TRANSCODE_TIMEOUT_MS = 10 * 60 * 1000;

export interface PreparedAudio {
  filePath: string;
  /** 释放转码临时文件（原文件直传时为空操作） */
  cleanup: () => void;
}

function runFfmpegTranscode(
  executable: string,
  inputPath: string,
  outputPath: string,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const child = spawn(
      executable,
      [
        "-y",
        "-loglevel",
        "error",
        "-i",
        inputPath,
        "-vn",
        "-ac",
        "1",
        "-ar",
        "16000",
        "-b:a",
        "48k",
        outputPath,
      ],
      { windowsHide: true },
    );

    let stderr = "";
    let settled = false;
    const finish = (action: () => void) => {
      if (!settled) {
        settled = true;
        cleanup();
        action();
      }
    };

    const timeout = setTimeout(() => {
      child.kill();
      finish(() => reject(new Error("ffmpeg 转码超时")));
    }, TRANSCODE_TIMEOUT_MS);

    const abort = () => {
      child.kill();
      finish(() => reject(new Error("已取消")));
    };
    signal?.addEventListener("abort", abort, { once: true });

    const cleanup = () => {
      clearTimeout(timeout);
      signal?.removeEventListener("abort", abort);
    };

    child.stderr?.on("data", (chunk: Buffer) => {
      if (stderr.length < 4096) {
        stderr += chunk.toString("utf8");
      }
    });
    child.on("error", (error) => {
      finish(() => reject(error));
    });
    child.on("close", (code) => {
      finish(() => {
        if (code === 0) {
          resolve();
        } else {
          reject(
            new Error(`ffmpeg 退出码 ${code}: ${stderr.trim().slice(0, 300)}`),
          );
        }
      });
    });
  });
}

/**
 * 转码到 16kHz 单声道 mp3；失败时返回原文件（cleanup 为空操作）。
 * 「已取消」除外——取消需要向上传播中断整个任务。
 */
export async function prepareAudioForTranscription(
  filePath: string,
  ffmpegExecutable: string,
  signal?: AbortSignal,
): Promise<PreparedAudio> {
  if (signal?.aborted) {
    throw new Error("已取消");
  }
  const dir = path.join(
    os.tmpdir(),
    `guizhi-transcode-${randomUUID().slice(0, 8)}`,
  );
  const outputPath = path.join(dir, "audio-16k.mp3");
  try {
    fs.mkdirSync(dir, { recursive: true });
    await runFfmpegTranscode(ffmpegExecutable, filePath, outputPath, signal);
    return {
      filePath: outputPath,
      cleanup: () => fs.rmSync(dir, { recursive: true, force: true }),
    };
  } catch (error) {
    fs.rmSync(dir, { recursive: true, force: true });
    if (error instanceof Error && error.message === "已取消") {
      throw error;
    }
    console.warn(
      "[media] 音频转码不可用，原文件直传:",
      error instanceof Error ? error.message : error,
    );
    return { filePath, cleanup: () => {} };
  }
}
