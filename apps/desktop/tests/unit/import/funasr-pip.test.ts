import { describe, expect, it, vi } from "vitest";
import { ensureFunasrPip } from "../../../src/main/services/media/funasr-pip";

describe("FunASR pip 自检与恢复", () => {
  it("健康环境只加载安装命令，不重装 pip", async () => {
    const run = vi.fn().mockResolvedValue({ stdout: "help" });
    const progress = vi.fn();
    await ensureFunasrPip("python.exe", run, progress);
    expect(run).toHaveBeenCalledOnce();
    expect(run).toHaveBeenCalledWith("python.exe", [
      "-I",
      "-m",
      "pip",
      "install",
      "--help",
    ]);
    expect(progress).not.toHaveBeenCalled();
  });

  it("安装命令缺依赖时离线修复，并重新验收", async () => {
    const run = vi
      .fn()
      .mockRejectedValueOnce(new Error("SeparateBodyFileCache"))
      .mockResolvedValue({ stdout: "ok" });
    const progress = vi.fn();
    await ensureFunasrPip("python.exe", run, progress);
    expect(progress).toHaveBeenCalledOnce();
    expect(run).toHaveBeenCalledTimes(3);
    const script = run.mock.calls[1][1][2] as string;
    expect(script).toContain('"--no-index"');
    expect(script).toContain('"--no-deps"');
    expect(script).toContain('"--force-reinstall"');
    expect(run.mock.calls[2]).toEqual(run.mock.calls[0]);
  });

  it("修复命令成功但自检仍失败时不得继续", async () => {
    const run = vi
      .fn()
      .mockRejectedValueOnce(new Error("broken"))
      .mockResolvedValueOnce({ stdout: "ok" })
      .mockRejectedValueOnce(new Error("still broken"));
    await expect(ensureFunasrPip("python.exe", run)).rejects.toThrow(
      "pip 离线修复失败",
    );
    expect(run).toHaveBeenCalledTimes(3);
  });
});
