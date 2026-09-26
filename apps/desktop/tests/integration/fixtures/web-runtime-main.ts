import { app } from "electron";
import { ElectronWebCapture } from "../../../src/main/services/web-capture/web-electron-capture";
import { configureRuntimePaths } from "../../../src/main/runtime-paths";
import type { WebCaptureRequest } from "@guizhi/shared/types";
import { calls } from "./web-runtime-network";

const userData = process.env.GUIZHI_RUNTIME_FIXTURE_DATA;
if (!userData) throw new Error("运行夹具必须指定独立数据目录");
app.setPath("userData", userData);
app.setAppPath(process.cwd());
configureRuntimePaths({ userDataPath: userData });
if (process.env.GUIZHI_TEST_RUNTIME_RESOURCES) {
  Object.defineProperty(process, "resourcesPath", {
    value: process.env.GUIZHI_TEST_RUNTIME_RESOURCES,
  });
  Object.defineProperty(app, "isPackaged", { value: true });
}
// 每页采集都会销毁窗口；夹具在断言完成后由 Playwright 关闭。
app.on("window-all-closed", () => undefined);
const worker = new ElectronWebCapture();
export const fixture = {
  calls,
  get running() {
    return worker.running;
  },
  close: () => worker.close(),
  async capture(request: WebCaptureRequest, cancelAfter?: number) {
    await app.whenReady();
    const controller = new AbortController();
    const timer =
      cancelAfter === undefined
        ? undefined
        : setTimeout(() => controller.abort(), cancelAfter);
    try {
      return await worker.capture(
        request,
        AbortSignal.any([controller.signal, AbortSignal.timeout(60_000)]),
      );
    } finally {
      clearTimeout(timer);
    }
  },
};
Object.assign(globalThis, { runtimeFixture: fixture });
