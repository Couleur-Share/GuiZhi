import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import {
  runtimeFile,
  verifyRuntimeFiles,
} from "../../../src/main/services/web-capture/web-runtime-integrity";

let directory: string;
let root: string;
const digest = (data: string | Buffer) =>
  createHash("sha256").update(data).digest("hex");
beforeEach(async () => {
  directory = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-integrity-"));
  root = path.join(directory, "runtime");
  await fs.mkdir(root);
  await fs.writeFile(path.join(root, "manifest.json"), "{}");
});
afterEach(async () => {
  await fs.rm(directory, { recursive: true, force: true });
});

describe("有界并发组件完整性校验", () => {
  it("流式验证大文件和多文件，不漏掉任何字节", async () => {
    const files: Record<string, string> = {};
    let bytes = 0;
    for (let i = 0; i < 18; i++) {
      const data = Buffer.alloc(i === 0 ? 1024 * 1024 + 17 : i + 1, i);
      files[`${i}.bin`] = digest(data);
      bytes += data.length;
      await fs.writeFile(path.join(root, `${i}.bin`), data);
    }
    expect(await verifyRuntimeFiles(root, files)).toMatchObject({
      files: 18,
      bytes,
    });
    await fs.writeFile(path.join(root, "17.bin"), "损坏");
    await expect(verifyRuntimeFiles(root, files)).rejects.toThrow(
      "组件校验失败：17.bin",
    );
  });
  it("拒绝额外文件，包括新生成的 Python 缓存", async () => {
    await fs.mkdir(path.join(root, "__pycache__"));
    await fs.writeFile(path.join(root, "__pycache__", "extra.pyc"), "cache");
    await expect(verifyRuntimeFiles(root, {})).rejects.toThrow("未登记文件");
  });
  it("拒绝缺失文件和不合法哈希", async () => {
    await expect(
      verifyRuntimeFiles(root, { "missing.bin": digest("") }),
    ).rejects.toThrow();
    await fs.writeFile(path.join(root, "value.bin"), "value");
    await expect(
      verifyRuntimeFiles(root, { "value.bin": "not-a-hash" }),
    ).rejects.toThrow("校验值无效");
  });
  it("不允许清单读取父目录或绝对路径", async () => {
    await expect(
      verifyRuntimeFiles(root, { "../outside.bin": digest("") }),
    ).rejects.toThrow("路径越界");
    expect(() => runtimeFile(root, directory)).toThrow("路径越界");
    expect(() => runtimeFile(root, "")).toThrow("路径越界");
  });
  it("拒绝指向运行时以外的目录链接", async () => {
    const outside = path.join(directory, "outside");
    await fs.mkdir(outside);
    await fs.writeFile(path.join(outside, "payload"), "value");
    await fs.symlink(
      outside,
      path.join(root, "linked"),
      process.platform === "win32" ? "junction" : "dir",
    );
    await expect(
      verifyRuntimeFiles(root, { "linked/payload": digest("value") }),
    ).rejects.toThrow("越界链接");
  });
});
