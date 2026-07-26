/**
 * 版本探测的跨启动缓存：yt-dlp 跑一次 `--version` 实测 2.8 秒（PyInstaller
 * 每次都要解压运行时），冷启动后第一次进设置页会整整转圈三秒。
 * 这里锁住「同一个文件只探测一次、文件换了必须重探」这条不变量。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

const mocked = vi.hoisted(() => ({ toolsDir: "" }));

vi.mock("../../../src/main/runtime-paths", () => ({
  getToolsDir: () => mocked.toolsDir,
}));

import {
  getEngineVersionCachePath,
  withCachedVersion,
} from "../../../src/main/services/media/engine-version-store";

let workDir: string;
let executable: string;

/** 覆盖文件内容并把 mtime 推后，模拟「装了个新版本」 */
function replaceBinary(content: string, mtimeSecondsAhead = 60): void {
  fs.writeFileSync(executable, content);
  const future = new Date(Date.now() + mtimeSecondsAhead * 1000);
  fs.utimesSync(executable, future, future);
}

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-engine-version-"));
  mocked.toolsDir = path.join(workDir, "tools");
  executable = path.join(workDir, "yt-dlp.exe");
  fs.writeFileSync(executable, "binary");
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("withCachedVersion", () => {
  it("同一个文件只探测一次，之后直接读缓存", async () => {
    const probe = vi.fn(async () => "2026.07.04");

    await expect(withCachedVersion(probe)(executable)).resolves.toBe(
      "2026.07.04",
    );
    await expect(withCachedVersion(probe)(executable)).resolves.toBe(
      "2026.07.04",
    );

    expect(probe).toHaveBeenCalledTimes(1);
    expect(fs.existsSync(getEngineVersionCachePath())).toBe(true);
  });

  it("文件换了（大小与 mtime 变化）→ 重新探测", async () => {
    const probe = vi
      .fn()
      .mockResolvedValueOnce("2026.07.04")
      .mockResolvedValueOnce("2026.07.20");

    await withCachedVersion(probe)(executable);
    replaceBinary("binary-v2-longer");

    await expect(withCachedVersion(probe)(executable)).resolves.toBe(
      "2026.07.20",
    );
    expect(probe).toHaveBeenCalledTimes(2);
  });

  it("force 绕过缓存，并把新结果写回", async () => {
    const probe = vi
      .fn()
      .mockResolvedValueOnce("2026.07.04")
      .mockResolvedValueOnce("2026.07.20");

    await withCachedVersion(probe)(executable);
    await expect(withCachedVersion(probe, true)(executable)).resolves.toBe(
      "2026.07.20",
    );
    // 回写之后，非 force 的读取拿到的是刷新过的版本
    await expect(withCachedVersion(probe)(executable)).resolves.toBe(
      "2026.07.20",
    );
    expect(probe).toHaveBeenCalledTimes(2);
  });

  it("探测失败不写缓存——文件可能只是被杀软临时锁住", async () => {
    const probe = vi.fn().mockResolvedValueOnce(null).mockResolvedValueOnce("2026.07.04");

    await expect(withCachedVersion(probe)(executable)).resolves.toBeNull();
    await expect(withCachedVersion(probe)(executable)).resolves.toBe(
      "2026.07.04",
    );
    expect(probe).toHaveBeenCalledTimes(2);
  });

  it("目标文件不存在 → 原样交给 probe，不落缓存", async () => {
    const probe = vi.fn(async () => null);
    const missing = path.join(workDir, "not-installed.exe");

    await expect(withCachedVersion(probe)(missing)).resolves.toBeNull();
    expect(probe).toHaveBeenCalledWith(missing);
    expect(fs.existsSync(getEngineVersionCachePath())).toBe(false);
  });

  it("缓存文件损坏时当作空缓存，不影响探测", async () => {
    fs.mkdirSync(mocked.toolsDir, { recursive: true });
    fs.writeFileSync(getEngineVersionCachePath(), "{ 这不是 JSON");
    const probe = vi.fn(async () => "2026.07.04");

    await expect(withCachedVersion(probe)(executable)).resolves.toBe(
      "2026.07.04",
    );
    expect(JSON.parse(fs.readFileSync(getEngineVersionCachePath(), "utf8"))).toHaveProperty(
      path.resolve(executable),
    );
  });

  it("写入时清掉指向已删除文件的旧条目", async () => {
    const removed = path.join(workDir, "ffmpeg.exe");
    fs.writeFileSync(removed, "binary");
    await withCachedVersion(async () => "8.1")(removed);
    fs.rmSync(removed);

    await withCachedVersion(async () => "2026.07.04")(executable);

    const cache = JSON.parse(
      fs.readFileSync(getEngineVersionCachePath(), "utf8"),
    ) as Record<string, unknown>;
    expect(Object.keys(cache)).toEqual([path.resolve(executable)]);
  });

  it("PATH 上的裸命令名也能缓存（未装内置版时走系统安装）", async () => {
    const originalPath = process.env.PATH;
    process.env.PATH = `${workDir}${path.delimiter}${originalPath ?? ""}`;
    fs.writeFileSync(path.join(workDir, "yt-dlp"), "binary");
    const probe = vi.fn(async () => "2026.07.04");

    try {
      await withCachedVersion(probe)("yt-dlp");
      await withCachedVersion(probe)("yt-dlp");
    } finally {
      process.env.PATH = originalPath;
    }

    expect(probe).toHaveBeenCalledTimes(1);
    expect(probe).toHaveBeenCalledWith("yt-dlp");
  });
});
