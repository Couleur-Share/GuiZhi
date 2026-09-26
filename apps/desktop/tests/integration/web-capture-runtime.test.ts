// @vitest-environment node
import { afterAll, describe, expect, it } from "vitest";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";
import { builtinModules } from "node:module";
import { build } from "vite";
import { _electron, type ElectronApplication } from "playwright";
import type { WebCaptureRequest } from "@guizhi/shared/types";
import type { fixture } from "./fixtures/web-runtime-main";

declare global {
  var runtimeFixture: typeof fixture;
}
const enabled = process.env.GUIZHI_TEST_BUNDLED_CRAWLER === "1";
let directory: string;
let application: ElectronApplication | undefined;
const capture = (request: WebCaptureRequest, cancelAfter?: number) =>
  application!.evaluate(
    (_electron, args) =>
      globalThis.runtimeFixture.capture(args.request, args.cancelAfter),
    { request, cancelAfter },
  );
const calls = () =>
  application!.evaluate(() => globalThis.runtimeFixture.calls);
const running = () =>
  application!.evaluate(() => globalThis.runtimeFixture.running);
const closeCapture = () =>
  application!.evaluate(() => globalThis.runtimeFixture?.close());
afterAll(async () => {
  if (application) {
    try {
      await closeCapture();
    } finally {
      await application.close();
    }
  }
  if (directory) await fs.rm(directory, { recursive: true, force: true });
});
describe.skipIf(!enabled)("真实 Electron 与随包 Python 的离线受控流程", () => {
  it("动态中文、表格、代码、短文与访问失败；所有请求经过主进程", async () => {
    directory = await fs.mkdtemp(
      path.join(os.tmpdir(), "guizhi-web-runtime-test-"),
    );
    const userData = path.join(directory, "user-data");
    await fs.mkdir(userData);
    const fixtures = path.resolve("tests/integration/fixtures");
    await build({
      configFile: false,
      logLevel: "warn",
      resolve: {
        alias: [
          {
            find: /^\.\/web-network$/,
            replacement: path.join(fixtures, "web-runtime-network.ts"),
          },
          {
            find: "@guizhi/shared",
            replacement: path.resolve("../../packages/shared"),
          },
        ],
      },
      build: {
        target: "node22",
        outDir: path.join(directory, "main"),
        minify: false,
        lib: {
          entry: path.join(fixtures, "web-runtime-main.ts"),
          formats: ["cjs"],
          fileName: () => "main.cjs",
        },
        rollupOptions: {
          external: [
            "electron",
            // 与正式主进程构建一致，让 linkedom 自行回退可选 canvas。
            "canvas",
            ...builtinModules,
            ...builtinModules.map((name) => "node:" + name),
          ],
        },
      },
    });
    const env = Object.fromEntries(
      Object.entries(process.env).filter(
        ([key, value]) =>
          value !== undefined &&
          !["ELECTRON_RUN_AS_NODE", "NODE_OPTIONS"].includes(key),
      ),
    ) as Record<string, string>;
    // 启动错误交给测试日志，避免 Electron 弹出主进程错误对话框。
    const entry = path.join(directory, "main/entry.cjs");
    await fs.writeFile(
      entry,
      `process.on("uncaughtException", error => { console.error(error); process.exit(1); });
try { require("./main.cjs"); } catch (error) { console.error(error); process.exit(1); }
`,
    );
    application = await _electron.launch({
      args: [entry],
      cwd: process.cwd(),
      env: {
        ...env,
        GUIZHI_RUNTIME_FIXTURE_DATA: userData,
        GUIZHI_WINDOW_MODE: "offscreen",
      },
    });
    const result = await capture({
      taskId: "dynamic",
      purpose: "import",
      url: "https://fixture.example/docs/start",
    });
    expect(result.complete).toBe(true);
    expect(result.engineVersion).toBe("crawl4ai/0.9.3-electron");
    expect(result.markdown).toContain("动态正文支持中文知识管理");
    expect(result.markdown).toContain("Crawl4AI");
    expect(result.markdown).toContain("| 引擎");
    expect(result.markdown).toContain('print("归知")');
    expect(result.markdown).not.toContain("广告导航");
    expect(result.links).toContain("https://fixture.example/reference");
    const short = await capture({
      taskId: "short",
      purpose: "import",
      url: "https://fixture.example/short",
    });
    expect(short.complete).toBe(true);
    expect(short.engineVersion).toBe("crawl4ai/0.9.3-static");
    expect(short.markdown).toContain("小更新");
    const denied = await capture({
      taskId: "denied",
      purpose: "import",
      url: "https://fixture.example/denied",
    });
    expect(denied.error?.code).toBe("restricted");
    expect(denied.complete).toBe(false);
    const redirected = await capture({
      taskId: "redirect",
      purpose: "import",
      url: "https://fixture.example/redirect",
    });
    expect(redirected.finalUrl).toBe("https://fixture.example/short");
    expect(redirected.complete).toBe(true);
    await expect(
      capture({
        taskId: "outside",
        purpose: "documents",
        url: "https://fixture.example/redirect-outside",
        scope: { origin: "https://fixture.example", directory: "/" },
      }),
    ).rejects.toThrow(/范围/);
    expect(await calls()).not.toContain("https://outside.example/short");
    const framed = await capture({
      taskId: "frames",
      purpose: "import",
      url: "https://fixture.example/frame-root",
    });
    expect(framed.markdown).toContain("动态请求完成");
    expect(await calls()).toContain("https://frame.example/short");
    await expect(
      capture(
        {
          taskId: "cancel",
          purpose: "import",
          url: "https://fixture.example/slow",
        },
        200,
      ),
    ).rejects.toThrow(/取消/);
    expect(await calls()).toContain("https://fixture.example/docs/start");
    // 关闭本夹具的提取进程，确认下一页能够重新初始化。
    await closeCapture();
    expect(await running()).toBe(false);
    const restarted = await capture({
      taskId: "restarted",
      purpose: "import",
      url: "https://fixture.example/short",
    });
    expect(restarted.complete).toBe(true);
    await closeCapture();
    expect(await running()).toBe(false);
  }, 180_000);
});
