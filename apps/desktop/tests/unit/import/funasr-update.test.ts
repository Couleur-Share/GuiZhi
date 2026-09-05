import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  root: "",
  fetch: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  port: vi.fn(),
}));
vi.mock("../../../src/main/services/network-proxy", () => ({
  fetchWithNetworkProxy: mocks.fetch,
  hasAnyProxyConfigured: () => false,
}));
vi.mock("../../../src/main/services/net-safety", () => ({
  resolvePublicAddress: vi.fn(),
}));
vi.mock("../../../src/main/runtime-paths", () => ({
  getToolsDir: () => mocks.root,
}));
vi.mock("../../../src/main/services/media/funasr-service", () => ({
  runFunasrMaintenance: (
    task: (start: () => Promise<void>) => Promise<unknown>,
  ) => task(mocks.start),
  stopFunasrService: mocks.stop,
  isFunasrPortListening: mocks.port,
}));

import {
  getFunasrPaths,
  readFunasrState,
  writeFunasrState,
} from "../../../src/main/services/media/funasr-paths";
import {
  checkFunasrUpdate,
  isNewerFunasrVersion,
  updateWindowsFunasr,
} from "../../../src/main/services/media/funasr-update";

const platform = process.platform;
beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(fs.promises, "statfs").mockResolvedValue({
    bsize: 4096,
    bavail: 1024 ** 3,
  } as fs.StatsFs);
  Object.defineProperty(process, "platform", { value: "win32" });
  mocks.root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-funasr-update-"));
  const paths = getFunasrPaths();
  fs.mkdirSync(path.dirname(paths.funasrPackage), { recursive: true });
  fs.mkdirSync(path.dirname(paths.venvPython), { recursive: true });
  fs.mkdirSync(paths.modelsDir, { recursive: true });
  fs.writeFileSync(paths.funasrPackage, "old package");
  fs.writeFileSync(paths.venvPython, "python");
  fs.writeFileSync(path.join(paths.modelsDir, "model"), "cached model");
  writeFunasrState({
    funasrVersion: "1.3.29",
    installedAt: "2026-09-01",
    flavor: "python",
  });
  mocks.fetch.mockResolvedValue({
    ok: true,
    json: async () => ({
      info: { version: "1.3.30" },
      urls: [{ yanked: false }],
    }),
  });
  mocks.start.mockResolvedValue(undefined);
  mocks.port.mockResolvedValue(false);
});
afterEach(() => {
  vi.restoreAllMocks();
  Object.defineProperty(process, "platform", { value: platform });
  fs.rmSync(mocks.root, { recursive: true, force: true });
});

function updater(failInstall = false) {
  return vi.fn(async (_exe: string, args: string[]) => {
    if (args.includes("install") && !args.includes("--help")) {
      fs.writeFileSync(getFunasrPaths().funasrPackage, "new package");
      if (failInstall) throw new Error("pip failed");
    }
    return { stdout: args.includes("-c") ? "1.3.30\n" : "" };
  });
}

describe("FunASR 版本检查", () => {
  it("按数字比较，不降级，不接收预发布或命令参数", () => {
    expect(isNewerFunasrVersion("1.3.30", "1.3.9")).toBe(true);
    expect(isNewerFunasrVersion("1.3.29", "1.3.30")).toBe(false);
    expect(isNewerFunasrVersion("1.3.29.0", "1.3.29")).toBe(false);
    expect(() => isNewerFunasrVersion("1.4.0rc1", "1.3.29")).toThrow();
    expect(() => isNewerFunasrVersion("--index-url=evil", "1.3.29")).toThrow();
  });
  it("只查版本，不启动、安装或改本地记录", async () => {
    await expect(checkFunasrUpdate()).resolves.toEqual({
      current: "1.3.29",
      latest: "1.3.30",
      updateAvailable: true,
    });
    expect(mocks.start).not.toHaveBeenCalled();
    expect(mocks.stop).not.toHaveBeenCalled();
    expect(readFunasrState()?.funasrVersion).toBe("1.3.29");
  });
  it("网络失败不当作已是最新", async () => {
    mocks.fetch.mockResolvedValue({ ok: false, status: 503 });
    await expect(checkFunasrUpdate()).rejects.toThrow("503");
  });
  it("拒绝已撤回版本", async () => {
    mocks.fetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        info: { version: "1.3.30" },
        urls: [{ yanked: true }],
      }),
    });
    await expect(checkFunasrUpdate()).rejects.toThrow("稳定版本");
  });
  it("非 Windows 引擎不误查 pip 版本", async () => {
    Object.defineProperty(process, "platform", { value: "darwin" });
    await expect(checkFunasrUpdate()).rejects.toThrow("GGUF");
    expect(mocks.fetch).not.toHaveBeenCalled();
  });
});

describe("FunASR 更新事务", () => {
  it("窗口进度通知抛错不能跳过回滚或丢掉恢复副本", async () => {
    const progress = vi.fn((event: { phase: string }) => {
      if (event.phase === "rollback") throw new Error("window closed");
    });
    await expect(
      updateWindowsFunasr("1.3.30", updater(true), progress),
    ).rejects.toThrow("已恢复原版本");
    expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
      "old package",
    );
  });
  it("备份缺失时拒绝回滚搬移，当前环境和模型仍保留", async () => {
    const base = updater();
    const run = vi.fn(async (exe: string, args: string[]) => {
      const result = await base(exe, args);
      if (args.includes("--upgrade")) {
        const root = getFunasrPaths().root;
        const backup = fs
          .readdirSync(root)
          .find((name) => name.startsWith("update-backup-"))!;
        fs.renameSync(
          path.join(root, backup, "env"),
          path.join(root, "saved-backup-env"),
        );
        throw new Error("pip failed");
      }
      return result;
    });
    await expect(updateWindowsFunasr("1.3.30", run)).rejects.toThrow(
      "恢复失败",
    );
    expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
      "new package",
    );
    expect(
      fs.readFileSync(path.join(getFunasrPaths().modelsDir, "model"), "utf8"),
    ).toBe("cached model");
  });
  it("恢复复制失败时仍保留升级后的环境文件", async () => {
    const copy = fs.promises.cp.bind(fs.promises);
    vi.spyOn(fs.promises, "cp")
      .mockImplementationOnce(copy)
      .mockRejectedValueOnce(new Error("restore failed"));
    await expect(updateWindowsFunasr("1.3.30", updater(true))).rejects.toThrow(
      "恢复失败",
    );
    const root = getFunasrPaths().root;
    const backup = fs
      .readdirSync(root)
      .find((name) => name.startsWith("update-backup-"))!;
    expect(
      fs.readFileSync(
        path.join(
          root,
          backup,
          "failed-env/Lib/site-packages/funasr/__init__.py",
        ),
        "utf8",
      ),
    ).toBe("new package");
  });
  it("预检磁盘不足不停止服务、不创建备份、不运行 pip", async () => {
    vi.mocked(fs.promises.statfs).mockResolvedValue({
      bsize: 4096,
      bavail: 0,
    } as fs.StatsFs);
    const run = updater();
    await expect(updateWindowsFunasr("1.3.30", run)).rejects.toThrow(
      "磁盘空间不足",
    );
    expect(mocks.stop).not.toHaveBeenCalled();
    expect(run).not.toHaveBeenCalled();
    expect(
      fs
        .readdirSync(getFunasrPaths().root)
        .some((name) => name.startsWith("update-backup-")),
    ).toBe(false);
  });
  it("备份后空间被占用，恢复服务但不改环境", async () => {
    vi.mocked(fs.promises.statfs)
      .mockResolvedValueOnce({ bsize: 4096, bavail: 1024 ** 3 } as fs.StatsFs)
      .mockResolvedValueOnce({ bsize: 4096, bavail: 1024 ** 3 } as fs.StatsFs)
      .mockResolvedValueOnce({ bsize: 4096, bavail: 0 } as fs.StatsFs);
    const run = updater();
    await expect(updateWindowsFunasr("1.3.30", run)).rejects.toThrow(
      "磁盘空间不足",
    );
    expect(run).not.toHaveBeenCalled();
    expect(mocks.start).toHaveBeenCalledOnce();
    expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
      "old package",
    );
  });
  it("精确更新检查到的版本，验收后记版本，保留模型", async () => {
    const run = updater();
    const progress = vi.fn();
    await expect(updateWindowsFunasr("1.3.30", run, progress)).resolves.toEqual(
      { version: "1.3.30" },
    );
    expect(run.mock.calls[0][1]).toEqual([
      "-I",
      "-m",
      "pip",
      "install",
      "--help",
    ]);
    expect(run.mock.calls[1][1]).toContain("funasr==1.3.30");
    expect(run.mock.calls[1][1]).toContain("only-if-needed");
    expect(readFunasrState()?.funasrVersion).toBe("1.3.30");
    expect(
      fs.readFileSync(path.join(getFunasrPaths().modelsDir, "model"), "utf8"),
    ).toBe("cached model");
    expect(
      fs
        .readdirSync(getFunasrPaths().root)
        .some((name) => name.startsWith("update-backup-")),
    ).toBe(false);
    expect(progress).toHaveBeenCalledWith({ phase: "verify", percent: null });
  });
  it("pip 已改坏环境后失败，恢复旧文件与版本并重新启动", async () => {
    await expect(updateWindowsFunasr("1.3.30", updater(true))).rejects.toThrow(
      "已恢复原版本",
    );
    expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
      "old package",
    );
    expect(readFunasrState()?.funasrVersion).toBe("1.3.29");
    expect(mocks.start).toHaveBeenCalledOnce();
  });
  it("新版本无法启动也恢复旧环境", async () => {
    mocks.start.mockRejectedValueOnce(new Error("model incompatible"));
    await expect(updateWindowsFunasr("1.3.30", updater())).rejects.toThrow(
      "已恢复原版本",
    );
    expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
      "old package",
    );
    expect(mocks.start).toHaveBeenCalledTimes(2);
  });
  it("检查后的版本变动时要求重查，不安装其他版本", async () => {
    const run = updater();
    await expect(updateWindowsFunasr("1.3.31", run)).rejects.toThrow(
      "重新检查",
    );
    expect(run).not.toHaveBeenCalled();
    expect(mocks.stop).not.toHaveBeenCalled();
  });
  it("备份失败不会删除现有环境", async () => {
    const copy = vi
      .spyOn(fs.promises, "cp")
      .mockRejectedValueOnce(new Error("disk full"));
    try {
      await expect(updateWindowsFunasr("1.3.30", updater())).rejects.toThrow(
        "disk full",
      );
      expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
        "old package",
      );
      expect(mocks.start).toHaveBeenCalledOnce();
    } finally {
      copy.mockRestore();
    }
  });
  it("删除失败只给成功结果附警告，下次升级清理该目录", async () => {
    const remove = vi
      .spyOn(fs.promises, "rm")
      .mockRejectedValueOnce(new Error("EPERM"));
    const result = await updateWindowsFunasr("1.3.30", updater());
    expect(result.version).toBe("1.3.30");
    expect(result.warning).toContain("备份清理未完成");
    expect(mocks.start).toHaveBeenCalledOnce();
    expect(readFunasrState()?.funasrVersion).toBe("1.3.30");
    remove.mockRestore();
    const backup = fs
      .readdirSync(getFunasrPaths().root)
      .find((name) => name.startsWith("update-backup-"))!;
    expect(
      JSON.parse(
        fs.readFileSync(
          path.join(getFunasrPaths().root, backup, "update.json"),
          "utf8",
        ),
      ).phase,
    ).toBe("disposable");
  });
  it("回滚失败保留完整备份和原安装记录", async () => {
    mocks.start.mockRejectedValue(new Error("cannot start"));
    await expect(updateWindowsFunasr("1.3.30", updater())).rejects.toThrow(
      "备份保留在",
    );
    const root = getFunasrPaths().root;
    const backup = fs
      .readdirSync(root)
      .find((name) => name.startsWith("update-backup-"))!;
    const manifest = JSON.parse(
      fs.readFileSync(path.join(root, backup, "update.json"), "utf8"),
    );
    expect(manifest.phase).toBe("recovery-required");
    expect(manifest.original.funasrVersion).toBe("1.3.29");
    expect(
      fs.readFileSync(
        path.join(root, backup, "env/Lib/site-packages/funasr/__init__.py"),
        "utf8",
      ),
    ).toBe("old package");
  });
  it("升级不继承系统临时盘，清理时连同临时文件删除", async () => {
    let temporary = "";
    const base = updater();
    const run = vi.fn(
      async (
        exe: string,
        args: string[],
        options?: { env?: NodeJS.ProcessEnv },
      ) => {
        temporary = options?.env?.TEMP ?? "";
        expect(temporary).toContain(getFunasrPaths().root);
        expect(options?.env?.TMP).toBe(temporary);
        expect(options?.env?.TMPDIR).toBe(temporary);
        fs.writeFileSync(path.join(temporary, "download.tmp"), "download");
        if (args.includes("--upgrade"))
          expect(args).toContain("--no-cache-dir");
        return base(exe, args);
      },
    );
    await updateWindowsFunasr("1.3.30", run);
    expect(fs.existsSync(temporary)).toBe(false);
  });
  it("pip 修复失败时恢复备份，不开始升级 FunASR", async () => {
    const run = vi.fn(async (_exe: string, args: string[]) => {
      if (args.includes("--help")) throw new Error("SeparateBodyFileCache");
      fs.writeFileSync(getFunasrPaths().funasrPackage, "partial repair");
      throw new Error("bundled wheel broken");
    });
    await expect(updateWindowsFunasr("1.3.30", run)).rejects.toThrow(
      "pip 离线修复失败",
    );
    expect(
      run.mock.calls.some(([, args]) => args.includes("funasr==1.3.30")),
    ).toBe(false);
    expect(fs.readFileSync(getFunasrPaths().funasrPackage, "utf8")).toBe(
      "old package",
    );
    expect(readFunasrState()?.funasrVersion).toBe("1.3.29");
    expect(mocks.start).toHaveBeenCalledOnce();
  });
});
