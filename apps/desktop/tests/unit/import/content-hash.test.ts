import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import {
  computeContentHash,
  computeFileHash,
} from "../../../src/main/services/import/content-hash";

describe("computeContentHash", () => {
  it("相同内容哈希一致", () => {
    expect(computeContentHash("你好，世界")).toBe(
      computeContentHash("你好，世界"),
    );
  });

  it("排版差异（换行/空白/大小写）不影响哈希", () => {
    const base = computeContentHash("Hello World\n第二行");
    expect(computeContentHash("hello   world\r\n\r\n第二行")).toBe(base);
    expect(computeContentHash("  Hello World\n第二行  ")).toBe(base);
  });

  it("内容不同哈希不同", () => {
    expect(computeContentHash("内容甲")).not.toBe(computeContentHash("内容乙"));
  });
});

describe("computeFileHash", () => {
  let workDir: string;

  beforeEach(() => {
    workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-filehash-test-"));
  });

  afterEach(() => {
    fs.rmSync(workDir, { recursive: true, force: true });
  });

  async function hashOf(name: string, bytes: Buffer | string) {
    const filePath = path.join(workDir, name);
    fs.writeFileSync(filePath, bytes);
    return computeFileHash(filePath);
  }

  it("字节相同则哈希相同，与文件名无关", async () => {
    expect(await hashOf("a.bin", "same")).toBe(await hashOf("b.bin", "same"));
  });

  it("字节不同则哈希不同", async () => {
    expect(await hashOf("a.bin", "x")).not.toBe(await hashOf("b.bin", "y"));
  });

  it("按原始字节算，不做文本标准化", async () => {
    // 文本版会压空白并小写化，这两份在它眼里是同一个东西；
    // 二进制不能这么处理，否则改坏字节还认成同一份资产
    const spaced = await hashOf("a.bin", "Hello   World");
    const plain = await hashOf("b.bin", "hello world");
    expect(spaced).not.toBe(plain);
    expect(computeContentHash("Hello   World")).toBe(
      computeContentHash("hello world"),
    );
  });

  it("二进制内容不被破坏（含 0 字节与非 UTF-8 序列）", async () => {
    const bytes = Buffer.from([0x00, 0xff, 0xfe, 0x41, 0x00, 0x80]);
    expect(await hashOf("a.bin", bytes)).toBe(await hashOf("b.bin", bytes));
    expect(await hashOf("c.bin", Buffer.from([0x00, 0xff]))).not.toBe(
      await hashOf("d.bin", bytes),
    );
  });
});
