import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

vi.mock("electron", () => ({ session: { defaultSession: {} }, app: {} }));

const toolsDir = path.join(os.tmpdir(), "guizhi-ytdlp-checksum");
vi.mock("../../../src/main/runtime-paths", () => ({
  getToolsDir: () => toolsDir,
}));

/** 下载写出一个占位文件；哈希与是否取到校验和由用例逐个设定 */
const downloadToFile = vi.fn(async (_url: string, targetPath: string) => {
  fs.writeFileSync(targetPath, "fake-binary");
});
const fetchExpectedSha256 = vi.fn<() => Promise<string | null>>();
const sha256File = vi.fn<() => Promise<string>>();

vi.mock("../../../src/main/services/media/tool-download", () => ({
  downloadToFile: (...args: [string, string]) => downloadToFile(...args),
  fetchExpectedSha256: () => fetchExpectedSha256(),
  sha256File: () => sha256File(),
}));

import {
  getYtDlpAssetName,
  getYtDlpChecksumUrls,
  installYtDlp,
} from "../../../src/main/services/media/ytdlp-manager";

const EXPECTED = "a".repeat(64);

beforeEach(() => {
  fs.mkdirSync(toolsDir, { recursive: true });
  downloadToFile.mockClear();
  fetchExpectedSha256.mockReset();
  sha256File.mockReset();
});

afterEach(() => {
  fs.rmSync(toolsDir, { recursive: true, force: true });
});

describe("installYtDlp 的哈希校验", () => {
  it("校验和对不上就换源，四个源都对不上才报错", async () => {
    fetchExpectedSha256.mockResolvedValue(EXPECTED);
    sha256File.mockResolvedValue("b".repeat(64));

    await expect(installYtDlp()).rejects.toThrow(/校验和不匹配/);
    // 被替换的文件不该留在磁盘上
    expect(fs.readdirSync(toolsDir)).not.toContain("yt-dlp.partial.exe");
    // 每个镜像都试过一遍
    expect(downloadToFile.mock.calls.length).toBe(getYtDlpChecksumUrls().length);
  });

  it("校验通过后才进到「能不能跑」这一步", async () => {
    fetchExpectedSha256.mockResolvedValue(EXPECTED);
    sha256File.mockResolvedValue(EXPECTED);

    // 占位文件当然跑不起来，但错误换成了运行探测那条，说明哈希这关过了
    await expect(installYtDlp()).rejects.toThrow(/无法运行/);
    expect(sha256File).toHaveBeenCalled();
  });

  it("取不到官方校验和时降级放行，而不是让安装彻底不可用", async () => {
    // 正需要镜像的网络里官方源多半也不通，硬失败等于封死这个功能
    fetchExpectedSha256.mockResolvedValue(null);
    sha256File.mockResolvedValue("whatever");

    await expect(installYtDlp()).rejects.toThrow(/无法运行/);
    expect(sha256File).not.toHaveBeenCalled();
  });

  it("校验清单按平台资产名匹配", () => {
    expect(getYtDlpAssetName("win32")).toBe("yt-dlp.exe");
    expect(getYtDlpAssetName("darwin")).toBe("yt-dlp_macos");
    expect(getYtDlpAssetName("linux")).toBe("yt-dlp_linux");
    // 官方源必须排在镜像前面：校验和与二进制同源就失去意义了
    expect(getYtDlpChecksumUrls()[0]).toMatch(/^https:\/\/github\.com\//);
  });
});
