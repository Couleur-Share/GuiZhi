import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, expect, it, vi } from "vitest";
const { createContext, capturePage } = vi.hoisted(() => ({
  createContext: vi.fn(),
  capturePage: vi.fn(),
}));
vi.mock(
  "../../../src/main/services/platform-capture/electron-capture-runtime",
  () => ({ createElectronCaptureContext: createContext }),
);
vi.mock(
  "../../../src/main/services/platform-capture/douyin-detail-capture",
  () => ({ captureDouyinDetailPage: capturePage }),
);
import { BrowserCaptureService } from "../../../src/main/services/platform-capture/browser-capture";
const temporary: string[] = [];
afterEach(() => {
  vi.resetAllMocks();
  for (const dir of temporary.splice(0))
    fs.rmSync(dir, { recursive: true, force: true });
});
function service() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-douyin-service-"));
  temporary.push(dir);
  return new BrowserCaptureService({ userDataPath: dir });
}
it("未登录的标准导入使用离屏窗口且成功后关闭上下文", async () => {
  const instance = service();
  const context = { page: {}, close: vi.fn().mockResolvedValue(undefined) };
  createContext.mockResolvedValue(context);
  capturePage.mockResolvedValue("目标详情");
  expect(await instance.captureDouyinDetail("7669754297756737722")).toBe(
    "目标详情",
  );
  expect(createContext).toHaveBeenCalledWith(
    expect.objectContaining({ platform: "douyin", visible: false }),
  );
  expect(context.close).toHaveBeenCalledTimes(1);
});
it("排队期间取消不会再打开第二个窗口，非法 ID 在创建窗口前拒绝", async () => {
  const instance = service();
  let release!: (value: string) => void;
  const context = { page: {}, close: vi.fn().mockResolvedValue(undefined) };
  createContext.mockResolvedValue(context);
  capturePage.mockImplementation(
    () =>
      new Promise<string>((resolve) => {
        release = resolve;
      }),
  );
  const first = instance.captureDouyinDetail("7669754297756737722");
  await vi.waitFor(() => expect(capturePage).toHaveBeenCalledTimes(1));
  const controller = new AbortController();
  const second = instance.captureDouyinDetail(
    "7669754297756737723",
    controller.signal,
  );
  const rejected = expect(second).rejects.toMatchObject({ code: "canceled" });
  controller.abort();
  release("详情");
  await first;
  await rejected;
  await expect(instance.captureDouyinDetail("../secret")).rejects.toThrow(
    "无效",
  );
  expect(createContext).toHaveBeenCalledTimes(1);
});
