// @vitest-environment node
// （adm-zip 的解压在 jsdom 环境下会静默失败；本文件全部是主进程代码，用 node 环境）
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import AdmZip from "adm-zip";

// ffmpeg-manager 依赖 network-proxy（引用 electron），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  extractFfmpegFromZip,
  getFfmpegDownloadUrls,
  getFfmpegStatus,
  getManagedFfmpegBinaryName,
  resolveFfmpegExecutable,
} from "../../../src/main/services/media/ffmpeg-manager";

let workDir: string;

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-ffmpeg-test-"));
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("getManagedFfmpegBinaryName / getFfmpegDownloadUrls", () => {
  it("Windows 用 .exe，其余平台无扩展名", () => {
    expect(getManagedFfmpegBinaryName("win32")).toBe("ffmpeg.exe");
    expect(getManagedFfmpegBinaryName("linux")).toBe("ffmpeg");
  });

  it("Windows 官方源在前、镜像随后；其余平台不提供应用内安装", () => {
    const winUrls = getFfmpegDownloadUrls("win32");
    expect(winUrls[0]).toBe(
      "https://github.com/yt-dlp/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip",
    );
    expect(winUrls.length).toBeGreaterThanOrEqual(3);
    expect(winUrls.slice(1).every((url) => url.includes("github.com"))).toBe(
      true,
    );

    expect(getFfmpegDownloadUrls("linux")).toEqual([]);
    expect(getFfmpegDownloadUrls("darwin")).toEqual([]);
  });
});

describe("resolveFfmpegExecutable（生效顺序）", () => {
  it("自定义路径 > 内置托管版 > 系统 PATH", () => {
    const managedPath = path.join(workDir, "ffmpeg.exe");

    // 三者皆无 → PATH 命令名
    expect(resolveFfmpegExecutable(null, managedPath)).toBe("ffmpeg");

    // 托管版存在 → 托管版
    fs.writeFileSync(managedPath, "binary");
    expect(resolveFfmpegExecutable(null, managedPath)).toBe(managedPath);
    expect(resolveFfmpegExecutable("  ", managedPath)).toBe(managedPath);

    // 自定义路径最优先
    expect(resolveFfmpegExecutable("C:\\tools\\ffmpeg.exe", managedPath)).toBe(
      "C:\\tools\\ffmpeg.exe",
    );
  });
});

describe("getFfmpegStatus", () => {
  it("自定义路径可运行 → source=custom；不可运行 → installed=false", async () => {
    const okStatus = await getFfmpegStatus("D:\\custom\\ffmpeg.exe", {
      probe: async () => "8.1-full_build",
      managedPath: path.join(workDir, "ffmpeg.exe"),
    });
    expect(okStatus).toMatchObject({
      installed: true,
      source: "custom",
      version: "8.1-full_build",
    });

    const brokenStatus = await getFfmpegStatus("D:\\custom\\broken.exe", {
      probe: async () => null,
      managedPath: path.join(workDir, "ffmpeg.exe"),
    });
    expect(brokenStatus.installed).toBe(false);
    expect(brokenStatus.source).toBeNull();
  });

  it("无自定义路径时：托管版存在且可运行 → managed", async () => {
    const managedPath = path.join(workDir, "ffmpeg.exe");
    fs.writeFileSync(managedPath, "binary");

    const status = await getFfmpegStatus(null, {
      probe: async (executable) =>
        executable === managedPath ? "8.1" : null,
      managedPath,
    });
    expect(status).toMatchObject({
      installed: true,
      source: "managed",
      path: managedPath,
    });
  });

  it("托管版损坏时回退探测系统 PATH", async () => {
    const managedPath = path.join(workDir, "ffmpeg.exe");
    fs.writeFileSync(managedPath, "corrupted");

    const status = await getFfmpegStatus(null, {
      probe: async (executable) => (executable === "ffmpeg" ? "7.1" : null),
      managedPath,
    });
    expect(status).toMatchObject({ installed: true, source: "path" });
  });

  it("全部不可用 → 未安装", async () => {
    const status = await getFfmpegStatus(null, {
      probe: async () => null,
      managedPath: path.join(workDir, "ffmpeg.exe"),
    });
    expect(status.installed).toBe(false);
    expect(status.source).toBeNull();
    expect(status.managedPath).toContain("ffmpeg.exe");
  });
});

describe("extractFfmpegFromZip", () => {
  it("从发布包目录结构中解出 bin/ffmpeg.exe", () => {
    const zip = new AdmZip();
    zip.addFile(
      "ffmpeg-master-latest-win64-gpl/bin/ffmpeg.exe",
      Buffer.from("fake-ffmpeg-binary"),
    );
    zip.addFile(
      "ffmpeg-master-latest-win64-gpl/bin/ffprobe.exe",
      Buffer.from("fake-ffprobe"),
    );
    const zipPath = path.join(workDir, "ffmpeg.zip");
    zip.writeZip(zipPath);

    const targetPath = path.join(workDir, "ffmpeg.exe");
    extractFfmpegFromZip(zipPath, targetPath);
    expect(fs.readFileSync(targetPath, "utf8")).toBe("fake-ffmpeg-binary");
  });

  it("压缩包缺少 ffmpeg.exe 时报错", () => {
    const zip = new AdmZip();
    zip.addFile("docs/readme.txt", Buffer.from("no binary here"));
    const zipPath = path.join(workDir, "bad.zip");
    zip.writeZip(zipPath);

    expect(() =>
      extractFfmpegFromZip(zipPath, path.join(workDir, "ffmpeg.exe")),
    ).toThrow("未找到");
  });
});
