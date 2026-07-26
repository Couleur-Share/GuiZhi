import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createHash } from "crypto";
import {
  parseSha256Sums,
  sha256File,
} from "../../../src/main/services/media/tool-download";

let workDir: string;

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-tool-download-"));
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("parseSha256Sums", () => {
  // yt-dlp 官方 SHA2-256SUMS 的真实形态
  const manifest = [
    "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd  yt-dlp",
    "52fe3c26dcf71fbdc85b528589020bb0b8e383155cfa81b64dd447bbe35e24b8  yt-dlp.exe",
    "31c32457d1a573a341bb0929386c624fe47339a5338829e6e91111111111aaaa  yt-dlp_linux",
  ].join("\n");

  it("按文件名取出对应的哈希", () => {
    expect(parseSha256Sums(manifest, "yt-dlp.exe")).toBe(
      "52fe3c26dcf71fbdc85b528589020bb0b8e383155cfa81b64dd447bbe35e24b8",
    );
    // 前缀相同的名字不能串（yt-dlp 与 yt-dlp.exe、yt-dlp_linux）
    expect(parseSha256Sums(manifest, "yt-dlp")).toBe(
      "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd",
    );
  });

  it("认二进制模式的星号前缀，也认 CRLF", () => {
    const text = "aa".repeat(32) + " *cpython-3.12.13+20260610.tar.gz\r\n";
    expect(parseSha256Sums(text, "cpython-3.12.13+20260610.tar.gz")).toBe(
      "aa".repeat(32),
    );
  });

  it("找不到或格式不对时返回 null", () => {
    expect(parseSha256Sums(manifest, "ffmpeg.zip")).toBeNull();
    expect(parseSha256Sums("not a checksum file", "yt-dlp.exe")).toBeNull();
    // 长度不足 64 的十六进制串不该被当成哈希
    expect(parseSha256Sums("abc  yt-dlp.exe", "yt-dlp.exe")).toBeNull();
  });
});

describe("sha256File", () => {
  it("与 crypto 直接计算的结果一致", async () => {
    const filePath = path.join(workDir, "payload.bin");
    const bytes = Buffer.from("归知 GuiZhi 工具下载校验", "utf8");
    fs.writeFileSync(filePath, bytes);

    const expected = createHash("sha256").update(bytes).digest("hex");
    await expect(sha256File(filePath)).resolves.toBe(expected);
  });

  it("内容改一个字节，哈希就不同", async () => {
    const a = path.join(workDir, "a.bin");
    const b = path.join(workDir, "b.bin");
    fs.writeFileSync(a, Buffer.from([1, 2, 3, 4]));
    fs.writeFileSync(b, Buffer.from([1, 2, 3, 5]));

    expect(await sha256File(a)).not.toBe(await sha256File(b));
  });
});
