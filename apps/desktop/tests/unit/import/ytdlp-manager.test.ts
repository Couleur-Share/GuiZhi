import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

// ytdlp-manager 依赖 network-proxy（引用 electron），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  getManagedBinaryName,
  getYtDlpDownloadUrls,
  getYtDlpStatus,
  resolveYtDlpExecutable,
} from "../../../src/main/services/media/ytdlp-manager";

let workDir: string;

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-ytdlp-test-"));
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("getManagedBinaryName / getYtDlpDownloadUrls", () => {
  it("Windows 用 .exe，其余平台无扩展名", () => {
    expect(getManagedBinaryName("win32")).toBe("yt-dlp.exe");
    expect(getManagedBinaryName("linux")).toBe("yt-dlp");
  });

  it("官方源在前，镜像源随后，资产名按平台区分", () => {
    const winUrls = getYtDlpDownloadUrls("win32");
    expect(winUrls[0]).toBe(
      "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe",
    );
    expect(winUrls.length).toBeGreaterThanOrEqual(3);
    expect(winUrls.slice(1).every((url) => url.includes("github.com"))).toBe(
      true,
    );

    const linuxUrls = getYtDlpDownloadUrls("linux");
    expect(linuxUrls[0]).toContain("yt-dlp_linux");
  });
});

describe("resolveYtDlpExecutable（生效顺序）", () => {
  it("自定义路径 > 内置托管版 > 系统 PATH", () => {
    const managedPath = path.join(workDir, "yt-dlp.exe");

    // 三者皆无 → PATH 命令名
    expect(resolveYtDlpExecutable(null, managedPath)).toBe("yt-dlp");

    // 托管版存在 → 托管版
    fs.writeFileSync(managedPath, "binary");
    expect(resolveYtDlpExecutable(null, managedPath)).toBe(managedPath);
    expect(resolveYtDlpExecutable("  ", managedPath)).toBe(managedPath);

    // 自定义路径最优先
    expect(resolveYtDlpExecutable("C:\\tools\\yt-dlp.exe", managedPath)).toBe(
      "C:\\tools\\yt-dlp.exe",
    );
  });
});

describe("getYtDlpStatus", () => {
  it("自定义路径可运行 → source=custom；不可运行 → installed=false", async () => {
    const okStatus = await getYtDlpStatus("D:\\custom\\yt-dlp.exe", {
      probe: async () => "2026.07.15",
      managedPath: path.join(workDir, "yt-dlp.exe"),
    });
    expect(okStatus).toMatchObject({
      installed: true,
      source: "custom",
      version: "2026.07.15",
    });

    const brokenStatus = await getYtDlpStatus("D:\\custom\\broken.exe", {
      probe: async () => null,
      managedPath: path.join(workDir, "yt-dlp.exe"),
    });
    expect(brokenStatus.installed).toBe(false);
    expect(brokenStatus.source).toBeNull();
  });

  it("无自定义路径时：托管版存在且可运行 → managed", async () => {
    const managedPath = path.join(workDir, "yt-dlp.exe");
    fs.writeFileSync(managedPath, "binary");

    const status = await getYtDlpStatus(null, {
      probe: async (executable) =>
        executable === managedPath ? "2026.07.15" : null,
      managedPath,
    });
    expect(status).toMatchObject({
      installed: true,
      source: "managed",
      path: managedPath,
    });
  });

  it("托管版损坏时回退探测系统 PATH", async () => {
    const managedPath = path.join(workDir, "yt-dlp.exe");
    fs.writeFileSync(managedPath, "corrupted");

    const status = await getYtDlpStatus(null, {
      probe: async (executable) =>
        executable === "yt-dlp" ? "2026.06.01" : null,
      managedPath,
    });
    expect(status).toMatchObject({ installed: true, source: "path" });
  });

  it("全部不可用 → 未安装", async () => {
    const status = await getYtDlpStatus(null, {
      probe: async () => null,
      managedPath: path.join(workDir, "yt-dlp.exe"),
    });
    expect(status.installed).toBe(false);
    expect(status.source).toBeNull();
    expect(status.managedPath).toContain("yt-dlp.exe");
  });
});
