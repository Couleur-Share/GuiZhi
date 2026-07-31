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
  checkFfmpegUpdate,
  extractFfmpegFromZip,
  fetchLatestFfmpegBuildDate,
  getFfmpegDownloadUrls,
  getFfmpegInstallHintCommand,
  getFfmpegStatus,
  getManagedFfmpegBinaryName,
  isFfmpegInstallSupported,
  parseFfmpegBuildDate,
  resolveFfmpegExecutable,
  toBuildDate,
} from "../../../src/main/services/media/ffmpeg-manager";

let workDir: string;

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-ffmpeg-test-"));
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

/**
 * FFmpeg-Builds 的 release tag 恒为 `latest`，没有版本号可比。可比的是构建日期：
 * 版本串尾部的 -YYYYMMDD 与远端资产 Last-Modified 是同一天（已对真实资产核对过）。
 */
describe("检查更新（只读响应头，不下载 160MB 的 zip）", () => {
  it("从版本串尾部取出构建日期", () => {
    expect(parseFfmpegBuildDate("N-125753-g6095372a70-20260724")).toBe(
      "20260724",
    );
    // gyan.dev 等其他来源的版本串没有日期后缀
    expect(parseFfmpegBuildDate("8.1-essentials_build-www.gyan.dev")).toBeNull();
    expect(parseFfmpegBuildDate(undefined)).toBeNull();
  });

  it("Last-Modified 按 UTC 归到 YYYYMMDD", () => {
    expect(toBuildDate("Fri, 24 Jul 2026 15:50:17 GMT")).toBe("20260724");
    expect(toBuildDate("not a date")).toBeNull();
    expect(toBuildDate(null)).toBeNull();
  });

  // 托管版 ffmpeg 只有 Windows 构建，平台必须显式传：
  // 跟着 process.platform 走的话，这两条在 Linux（CI）上下载源列表为空，
  // 循环一次都不执行，测不到任何东西
  it("HEAD 命中即返回，不读正文", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(null, {
        status: 200,
        headers: { "last-modified": "Fri, 24 Jul 2026 15:50:17 GMT" },
      }),
    );

    await expect(
      fetchLatestFfmpegBuildDate(fetchImpl as never, "win32"),
    ).resolves.toBe("20260724");
    expect(fetchImpl.mock.calls[0][1]).toMatchObject({ method: "HEAD" });
  });

  it("逐源失败 → null，UI 报检查失败而不是「已是最新」", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const fetchImpl = vi.fn().mockRejectedValue(new Error("网络不可达"));

    await expect(
      fetchLatestFfmpegBuildDate(fetchImpl as never, "win32"),
    ).resolves.toBeNull();
    expect(fetchImpl).toHaveBeenCalledTimes(
      getFfmpegDownloadUrls("win32").length,
    );
    expect(getFfmpegDownloadUrls("win32").length).toBeGreaterThan(0);
  });

  it("非 Windows 平台没有托管源，不发任何请求", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const fetchImpl = vi.fn();

    await expect(
      fetchLatestFfmpegBuildDate(fetchImpl as never, "linux"),
    ).resolves.toBeNull();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("同一天构建 → 没有更新", async () => {
    await expect(
      checkFfmpegUpdate("N-125753-g6095372a70-20260724", async () => "20260724"),
    ).resolves.toEqual({
      current: "20260724",
      latest: "20260724",
      updateAvailable: false,
    });
  });

  it("远端有更新的构建 → 提示更新", async () => {
    const result = await checkFfmpegUpdate(
      "N-125753-g6095372a70-20260724",
      async () => "20260725",
    );
    expect(result.updateAvailable).toBe(true);
    expect(result.latest).toBe("20260725");
  });

  it("本地版本串没有日期后缀 → 无从比较，不谎称有更新", async () => {
    const result = await checkFfmpegUpdate(
      "8.1-essentials_build-www.gyan.dev",
      async () => "20260725",
    );
    expect(result.current).toBeNull();
    expect(result.updateAvailable).toBe(false);
  });
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

  it("一键安装仅 Windows；Mac 给出 brew 命令", () => {
    expect(isFfmpegInstallSupported("win32")).toBe(true);
    expect(isFfmpegInstallSupported("darwin")).toBe(false);
    expect(isFfmpegInstallSupported("linux")).toBe(false);
    expect(getFfmpegInstallHintCommand("darwin")).toBe("brew install ffmpeg");
    expect(getFfmpegInstallHintCommand("linux")).toBeUndefined();
    expect(getFfmpegInstallHintCommand("win32")).toBeUndefined();
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
      platform: "win32",
    });
    expect(status.installed).toBe(false);
    expect(status.source).toBeNull();
    expect(status.managedPath).toContain("ffmpeg.exe");
    expect(status.installSupported).toBe(true);
  });

  it("darwin 未安装时带回 brew 指引、不支持应用内安装", async () => {
    const status = await getFfmpegStatus(null, {
      probe: async () => null,
      managedPath: path.join(workDir, "ffmpeg"),
      platform: "darwin",
    });
    expect(status.installed).toBe(false);
    expect(status.installSupported).toBe(false);
    expect(status.installHintCommand).toBe("brew install ffmpeg");
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
