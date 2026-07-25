import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

// ytdlp-manager 依赖 network-proxy（引用 electron），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

// 托管路径来自 electron 的 userData，单测里换成临时目录
vi.mock("../../../src/main/runtime-paths", async () => {
  const os = await import("os");
  const path = await import("path");
  return { getToolsDir: () => path.join(os.tmpdir(), "guizhi-ytdlp-unit") };
});

// 单测不真的下载：让每个下载源都失败，以便断言「预检没跳过时确实走到了下载」
vi.mock("../../../src/main/services/media/tool-download", () => ({
  downloadToFile: vi.fn(async () => {
    throw new Error("单测禁用真实下载");
  }),
}));

import {
  checkYtDlpUpdate,
  fetchLatestYtDlpVersion,
  getManagedBinaryName,
  getYtDlpDownloadUrls,
  getYtDlpLatestReleaseUrls,
  getYtDlpStatus,
  installYtDlp,
  parseReleaseTag,
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

/**
 * 「更新内置版」原本是无条件下载 18MB，装完才比版本。现在拆成独立的
 * 「检查更新」——只读 302 的 Location 拿 release tag，查到有新版本才给更新动作。
 */
describe("检查更新（只查版本号，不下载）", () => {
  function redirectTo(location: string): Response {
    return new Response(null, { status: 302, headers: { location } });
  }

  /** 下载源逐个失败会刷屏，断言的是行为不是日志 */
  function silenceLogs(): void {
    vi.spyOn(console, "log").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
  }

  it("从 302 的 Location 解析 release tag", () => {
    expect(
      parseReleaseTag("https://github.com/yt-dlp/yt-dlp/releases/tag/2026.07.04"),
    ).toBe("2026.07.04");
    // 镜像会在前面套一层自己的域名，尾部形态不变
    expect(
      parseReleaseTag("https://ghfast.top/https://github.com/x/y/releases/tag/2026.07.04"),
    ).toBe("2026.07.04");
    expect(parseReleaseTag("https://github.com/yt-dlp/yt-dlp/releases")).toBeNull();
    expect(parseReleaseTag(null)).toBeNull();
  });

  it("latest 地址覆盖官方源与镜像源", () => {
    const urls = getYtDlpLatestReleaseUrls();
    expect(urls[0]).toBe("https://github.com/yt-dlp/yt-dlp/releases/latest");
    expect(urls.length).toBe(getYtDlpDownloadUrls("win32").length);
  });

  it("首个可用源命中即返回，不再试其余源", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        redirectTo("https://github.com/yt-dlp/yt-dlp/releases/tag/2026.07.04"),
      );

    await expect(fetchLatestYtDlpVersion(fetchImpl as never)).resolves.toBe(
      "2026.07.04",
    );
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(fetchImpl.mock.calls[0][1]).toMatchObject({ redirect: "manual" });
  });

  it("镜像自行跟随重定向时退回读终点 URL", async () => {
    const followed = new Response(null, { status: 200 });
    Object.defineProperty(followed, "url", {
      value: "https://gh-proxy.com/https://github.com/x/y/releases/tag/2026.07.04",
    });
    const fetchImpl = vi.fn().mockResolvedValue(followed);

    await expect(fetchLatestYtDlpVersion(fetchImpl as never)).resolves.toBe(
      "2026.07.04",
    );
  });

  it("逐源失败后返回 null（调用方照常下载）", async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new Error("网络不可达"));
    vi.spyOn(console, "warn").mockImplementation(() => {});

    await expect(fetchLatestYtDlpVersion(fetchImpl as never)).resolves.toBeNull();
    expect(fetchImpl).toHaveBeenCalledTimes(getYtDlpLatestReleaseUrls().length);
  });

  it("同版本 → 没有更新", async () => {
    await expect(
      checkYtDlpUpdate("2026.07.04", async () => "2026.07.04"),
    ).resolves.toEqual({
      current: "2026.07.04",
      latest: "2026.07.04",
      updateAvailable: false,
    });
  });

  it("远端更新 → 有更新，并带出目标版本号供按钮展示", async () => {
    await expect(
      checkYtDlpUpdate("2026.07.04", async () => "2026.07.20"),
    ).resolves.toEqual({
      current: "2026.07.04",
      latest: "2026.07.20",
      updateAvailable: true,
    });
  });

  it("本地比远端还新（nightly）→ 不提示更新，避免把用户降级", async () => {
    const result = await checkYtDlpUpdate(
      "2026.07.20",
      async () => "2026.07.04",
    );
    expect(result.updateAvailable).toBe(false);
  });

  it("按数值比较版本段，不是字典序", async () => {
    // 字典序下 "2026.07.4" > "2026.07.20"，数值比较才得出正确结论
    const result = await checkYtDlpUpdate("2026.07.4", async () => "2026.07.20");
    expect(result.updateAvailable).toBe(true);
  });

  it("查不到远端版本 → 不谎称有更新，由 UI 报检查失败", async () => {
    await expect(
      checkYtDlpUpdate("2026.07.04", async () => null),
    ).resolves.toEqual({
      current: "2026.07.04",
      latest: null,
      updateAvailable: false,
    });
  });

  it("来源不是内置版（currentVersion 为 null）→ 无从比较", async () => {
    const result = await checkYtDlpUpdate(null, async () => "2026.07.20");
    expect(result.updateAvailable).toBe(false);
  });

  it("installYtDlp 只负责下载，不再自行判断要不要更新", async () => {
    silenceLogs();
    await expect(installYtDlp()).rejects.toThrow(/所有下载源均失败/);
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
