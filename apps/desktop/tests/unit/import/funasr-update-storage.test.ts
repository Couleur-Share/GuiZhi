import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  assertFunasrUpdateSpace,
  measureFunasrEnvironment,
  planFunasrUpdateSpace,
} from "../../../src/main/services/media/funasr-update-storage";

let root: string;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-space-test-"));
});
afterEach(() => {
  vi.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
});
function disk(available: number) {
  return vi
    .spyOn(fs.promises, "statfs")
    .mockResolvedValue({ bsize: 4096, bavail: available / 4096 } as fs.StatsFs);
}

describe("FunASR 更新空间预算", () => {
  it("按目标卷块大小统计嵌套环境，模型不计入备份", async () => {
    const env = path.join(root, "env");
    fs.mkdirSync(path.join(env, "nested"), { recursive: true });
    fs.writeFileSync(path.join(env, "one"), "x");
    fs.writeFileSync(path.join(env, "nested/two"), Buffer.alloc(4097));
    fs.writeFileSync(path.join(root, "model"), Buffer.alloc(10000));
    disk(10 * 1024 ** 3);
    await expect(planFunasrUpdateSpace(root, env)).resolves.toEqual({
      backupBytes: 5 * 4096,
      workBytes: 1.5 * 1024 ** 3,
    });
  });
  it("空间不足展示所需和可用容量，不创建文件", async () => {
    disk(1024 ** 3);
    await expect(assertFunasrUpdateSpace(root, 2 * 1024 ** 3)).rejects.toThrow(
      "预计需要 2.00 GiB，当前可用 1.00 GiB",
    );
    expect(fs.readdirSync(root)).toEqual([]);
  });
  it("无法查询磁盘时阻止升级而不是跳过", async () => {
    vi.spyOn(fs.promises, "statfs").mockRejectedValue(new Error("EIO"));
    await expect(planFunasrUpdateSpace(root, root)).rejects.toThrow(
      "无法检查引擎磁盘空间",
    );
  });
  it("目录链接不能把统计引向其他卷或无限递归", async () => {
    fs.symlinkSync(root, path.join(root, "loop"), "junction");
    await expect(measureFunasrEnvironment(root, 4096)).rejects.toThrow(
      "包含链接",
    );
  });
});
