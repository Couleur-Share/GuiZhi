import { beforeEach, describe, expect, it, vi } from "vitest";

type IpcHandler = (event: unknown, payload?: unknown) => unknown;

// vi.mock 的工厂会被提升到文件顶部，引用的变量必须一并提升
const { handlers, logStartupEvent, checkForUpdates } = vi.hoisted(() => ({
  handlers: new Map<string, (event: unknown, payload?: unknown) => unknown>(),
  logStartupEvent: vi.fn(),
  checkForUpdates: vi.fn(),
}));

vi.mock("electron", () => ({
  app: {
    getVersion: () => "0.6.0",
    isPackaged: true,
    getPath: () => "/tmp",
    getAppPath: () => "/tmp",
  },
  ipcMain: {
    handle: (channel: string, handler: IpcHandler) => {
      handlers.set(channel, handler);
    },
    removeHandler: (channel: string) => {
      handlers.delete(channel);
    },
  },
  shell: {
    openExternal: vi.fn(),
    openPath: vi.fn(),
    showItemInFolder: vi.fn(),
  },
  BrowserWindow: class {},
}));

vi.mock("electron-updater", () => ({
  autoUpdater: {
    on: vi.fn(),
    setFeedURL: vi.fn(),
    checkForUpdates,
    downloadUpdate: vi.fn(),
    quitAndInstall: vi.fn(),
    autoDownload: false,
    autoInstallOnAppQuit: false,
    allowPrerelease: false,
    allowDowngrade: false,
  },
}));

vi.mock("../../../src/main/services/backup", () => ({
  createBackupSafe: vi.fn(),
}));

vi.mock("../../../src/main/services/network-proxy", () => ({
  getHttpRequestAgent: () => undefined,
}));

vi.mock("../../../src/main/startup-log", () => ({ logStartupEvent }));

import { registerUpdaterIPC } from "../../../src/main/updater";

async function invoke(channel: string, payload?: unknown): Promise<unknown> {
  const handler = handlers.get(channel);
  expect(handler, `missing IPC handler: ${channel}`).toBeTypeOf("function");
  return await handler!({}, payload);
}

function lastLog(): Record<string, unknown> {
  const call = logStartupEvent.mock.calls.at(-1);
  expect(call, "logStartupEvent was not called").toBeDefined();
  return call![0] as Record<string, unknown>;
}

describe("更新检查的诊断日志", () => {
  beforeEach(() => {
    handlers.clear();
    logStartupEvent.mockClear();
    checkForUpdates.mockReset();
    registerUpdaterIPC();
  });

  it("没有新版本时也留下记录（自动检查在界面上是无声的）", async () => {
    checkForUpdates.mockResolvedValue({
      isUpdateAvailable: false,
      updateInfo: { version: "0.6.0" },
    });

    await invoke("updater:check", { trigger: "startup", channel: "stable" });

    expect(lastLog()).toMatchObject({
      event: "updater:check",
      trigger: "startup",
      result: "not-available",
      version: "0.6.0",
      currentVersion: "0.6.0",
    });
  });

  it("发现新版本时记录版本号与触发来源", async () => {
    checkForUpdates.mockResolvedValue({
      isUpdateAvailable: true,
      updateInfo: { version: "0.7.0" },
    });

    await invoke("updater:check", { trigger: "visibility", channel: "stable" });

    expect(lastLog()).toMatchObject({
      event: "updater:check",
      trigger: "visibility",
      result: "available",
      version: "0.7.0",
    });
  });

  it("检查失败时记录错误并把失败原样返回", async () => {
    checkForUpdates.mockRejectedValue(new Error("net::ERR_CONNECTION_RESET"));

    const result = (await invoke("updater:check", {
      trigger: "interval",
    })) as { success: boolean; error?: string };

    expect(result.success).toBe(false);
    expect(result.error).toContain("net::ERR_CONNECTION_RESET");
    expect(lastLog()).toMatchObject({
      event: "updater:check",
      trigger: "interval",
      result: "error",
    });
    expect(String(lastLog().error)).toContain("net::ERR_CONNECTION_RESET");
  });

  it("缺省 trigger 视为手动检查", async () => {
    checkForUpdates.mockResolvedValue({
      isUpdateAvailable: false,
      updateInfo: { version: "0.6.0" },
    });

    await invoke("updater:check");

    expect(lastLog()).toMatchObject({ trigger: "manual" });
  });
});

describe("自动检查跳过上报", () => {
  beforeEach(() => {
    handlers.clear();
    logStartupEvent.mockClear();
    registerUpdaterIPC();
  });

  it("记录白名单内的触发来源与跳过原因", async () => {
    const result = await invoke("updater:logAutoSkip", {
      trigger: "startup",
      reason: "hidden",
    });

    expect(result).toEqual({ success: true });
    expect(lastLog()).toMatchObject({
      event: "updater:auto_skip",
      trigger: "startup",
      reason: "hidden",
    });
  });

  it("拒绝白名单外的取值，不把渲染进程的自由文本写进日志", async () => {
    const result = await invoke("updater:logAutoSkip", {
      trigger: "startup",
      reason: "<script>whatever</script>",
    });

    expect(result).toEqual({ success: false });
    expect(logStartupEvent).not.toHaveBeenCalled();
  });
});
