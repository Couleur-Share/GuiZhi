import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  root: "",
  manualProxy: vi.fn(),
  stop: vi.fn(),
  download: vi.fn(),
}));
vi.mock("electron", () => ({ session: { defaultSession: {} }, app: {} }));
vi.mock("../../../src/main/services/network-proxy", () => ({
  resolveManualProxy: mocks.manualProxy,
}));
// 工具目录指向临时目录：预检一旦失效，安装流程也只会清理这里。
vi.mock("../../../src/main/runtime-paths", () => ({
  getToolsDir: () => mocks.root,
}));
vi.mock("../../../src/main/services/media/funasr-service", () => ({
  ensureFunasrService: vi.fn(),
  probeFunasrHealth: vi.fn(),
  stopFunasrService: mocks.stop,
}));
vi.mock("../../../src/main/services/media/tool-download", () => ({
  downloadToFile: mocks.download,
  fetchExpectedSha256: mocks.download,
  sha256File: mocks.download,
}));

import { getFunasrPaths } from "../../../src/main/services/media/funasr-paths";
import { installFunasr } from "../../../src/main/services/media/funasr-manager";
import { FUNASR_SOCKS_PROXY_HINT } from "../../../src/main/services/media/funasr-proxy";

const platform = process.platform;
beforeEach(() => {
  Object.defineProperty(process, "platform", { value: "win32" });
  mocks.root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-funasr-install-"));
});
afterEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(process, "platform", { value: platform });
  fs.rmSync(mocks.root, { recursive: true, force: true });
});

describe("本地转写引擎安装的代理预检", () => {
  it("手动 SOCKS5 代理下不停服务、不清理已有环境、不下载运行时", async () => {
    mocks.manualProxy.mockReturnValue({
      proxyUrl: "socks5://127.0.0.1:1080",
      socks: true,
    });
    const existing = path.join(getFunasrPaths().venvDir, "existing");
    fs.mkdirSync(path.dirname(existing), { recursive: true });
    fs.writeFileSync(existing, "engine");

    await expect(installFunasr()).rejects.toThrow(FUNASR_SOCKS_PROXY_HINT);
    expect(mocks.manualProxy.mock.calls[0][0].href).toBe(
      "https://pypi.tuna.tsinghua.edu.cn/simple",
    );
    expect(mocks.stop).not.toHaveBeenCalled();
    expect(mocks.download).not.toHaveBeenCalled();
    expect(fs.readFileSync(existing, "utf8")).toBe("engine");
  });
});
