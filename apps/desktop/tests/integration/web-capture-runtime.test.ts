import { afterAll, describe, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";
import { execFileSync, type ChildProcess } from "node:child_process";
import { WebWorker } from "../../src/main/services/web-capture/web-worker";
import { configureRuntimePaths } from "../../src/main/runtime-paths";
import { webNetworkRequest } from "../../src/main/services/web-capture/web-network";

vi.mock("electron", () => ({
  app: {
    isPackaged: !!process.env.GUIZHI_TEST_RUNTIME_RESOURCES,
    getAppPath: () => process.cwd(),
  },
  session: { defaultSession: { resolveProxy: async () => "DIRECT" } },
}));
vi.mock("../../src/main/services/web-capture/web-network", () => ({
  webNetworkRequest: vi.fn(),
}));
const enabled = process.env.GUIZHI_TEST_BUNDLED_CRAWLER === "1";
if (process.env.GUIZHI_TEST_RUNTIME_RESOURCES)
  Object.defineProperty(process, "resourcesPath", {
    value: process.env.GUIZHI_TEST_RUNTIME_RESOURCES,
    configurable: true,
  });
let directory: string;
const worker = new WebWorker();
afterAll(async () => {
  await worker.close();
  if (directory) await fs.rm(directory, { recursive: true, force: true });
});
describe.skipIf(!enabled)(
  "完整随包 Python 与独立 Chromium 的离线受控流程",
  () => {
    it("动态中文、表格、代码、短文与访问失败；所有请求经过主进程", async () => {
      directory = await fs.mkdtemp(
        path.join(os.tmpdir(), "guizhi-web-runtime-test-"),
      );
      configureRuntimePaths({ userDataPath: directory });
      const calls: string[] = [];
      vi.mocked(webNetworkRequest).mockImplementation(async (request) => {
        calls.push(request.url);
        const url = new URL(request.url);
        if (url.pathname === "/redirect")
          return {
            status: 302,
            headers: { location: "https://fixture.example/short" },
            body: "",
          };
        if (url.pathname === "/redirect-outside")
          return {
            status: 302,
            headers: { location: "https://outside.example/short" },
            body: "",
          };
        if (url.pathname === "/frame-root")
          return {
            status: 200,
            headers: { "content-type": "text/html; charset=utf-8" },
            body: Buffer.from(
              '<article>主页面正文</article><iframe src="https://frame.example/short"></iframe><script>fetch("/redirect").then(r=>r.text()).then(t=>document.querySelector("article").textContent="动态请求完成："+t)</script>',
            ).toString("base64"),
          };
        if (url.pathname === "/slow")
          return await new Promise((_resolve, reject) => {
            setTimeout(() => reject(new Error("受控慢请求")), 5000);
          });
        const content =
          url.pathname === "/short"
            ? "<title>短文</title><article><p>今天发布了一个小更新。</p></article>"
            : url.pathname === "/denied"
              ? "<title>拒绝访问</title><h1>Forbidden</h1>"
              : '<title>中文技术文档</title><nav>广告导航</nav><main><h1>组件配置</h1><p id="dynamic">加载中</p><table><tr><th>引擎</th><th>并发</th></tr><tr><td>Crawl4AI</td><td>2</td></tr></table><pre><code>print("归知")</code></pre><a href="/reference">参考链接</a></main><script>setTimeout(()=>document.querySelector("#dynamic").textContent="动态正文支持中文知识管理。",100)</script>';
        return {
          status: url.pathname === "/denied" ? 403 : 200,
          headers: { "content-type": "text/html; charset=utf-8" },
          body: Buffer.from(content).toString("base64"),
        };
      });
      const firstStarted = Date.now();
      const result = await worker.capture(
        {
          taskId: "dynamic",
          purpose: "import",
          url: "https://fixture.example/docs/start",
        },
        AbortSignal.timeout(60_000),
      );
      const firstCaptureMs = Date.now() - firstStarted;
      expect(result.complete).toBe(true);
      expect(result.markdown).toContain("动态正文支持中文知识管理");
      expect(result.markdown).toContain("Crawl4AI");
      expect(result.markdown).toContain("| 引擎");
      expect(result.markdown).toContain('print("归知")');
      expect(result.markdown).not.toContain("广告导航");
      expect(result.links).toContain("https://fixture.example/reference");
      const warmStarted = Date.now();
      const short = await worker.capture(
        {
          taskId: "short",
          purpose: "import",
          url: "https://fixture.example/short",
        },
        AbortSignal.timeout(60_000),
      );
      expect(short.complete).toBe(true);
      expect(short.markdown).toContain("小更新");
      const subsequentCaptureMs = Date.now() - warmStarted;
      if (process.platform === "win32") {
        // 只统计本测试拥有的进程树；WorkingSetPrivate 排除共享驻留页，不能用 WorkingSet 相加代替。
        const owned = worker as unknown as {
          worker: ChildProcess;
          browser: ChildProcess;
        };
        const roots = [owned.worker.pid, owned.browser.pid];
        expect(
          roots.every((pid) => Number.isSafeInteger(pid) && pid! > 0),
        ).toBe(true);
        const script = `$ids = [System.Collections.Generic.HashSet[int]]::new(); @(${roots.join(",")}) | ForEach-Object { [void]$ids.Add($_) }; $rows = @(Get-CimInstance Win32_Process); do { $added = $false; foreach ($row in $rows) { if ($ids.Contains([int]$row.ParentProcessId) -and $ids.Add([int]$row.ProcessId)) { $added = $true } } } while ($added); @(Get-CimInstance Win32_PerfFormattedData_PerfProc_Process | Where-Object { $ids.Contains([int]$_.IDProcess) } | Select-Object IDProcess,Name,WorkingSetPrivate) | ConvertTo-Json -Compress`;
        const processes = JSON.parse(
          execFileSync(
            "powershell.exe",
            ["-NoProfile", "-NonInteractive", "-Command", script],
            { encoding: "utf8", timeout: 30_000 },
          ),
        );
        expect(processes.length).toBeGreaterThanOrEqual(2);
        const output = path.resolve(
          "../../artifacts/crawl4ai/runtime-metrics.json",
        );
        await fs.mkdir(path.dirname(output), { recursive: true });
        await fs.writeFile(
          output,
          JSON.stringify(
            {
              platform: process.platform,
              arch: process.arch,
              resources:
                process.env.GUIZHI_TEST_RUNTIME_RESOURCES || "development",
              firstCaptureMs,
              subsequentCaptureMs,
              privateResidentBytes: processes.reduce(
                (sum: number, item: { WorkingSetPrivate: number }) =>
                  sum + Number(item.WorkingSetPrivate),
                0,
              ),
              processes,
              measurement:
                "Windows WorkingSetPrivate snapshot after two pages; excludes shared pages, Electron and peak usage. First capture includes integrity verification and initialization; OS file cache is not cold-controlled.",
            },
            null,
            2,
          ),
        );
      }
      const denied = await worker.capture(
        {
          taskId: "denied",
          purpose: "import",
          url: "https://fixture.example/denied",
        },
        AbortSignal.timeout(60_000),
      );
      expect(denied.error?.code).toBe("restricted");
      expect(denied.complete).toBe(false);
      const redirected = await worker.capture(
        {
          taskId: "redirect",
          purpose: "import",
          url: "https://fixture.example/redirect",
        },
        AbortSignal.timeout(60_000),
      );
      expect(redirected.finalUrl).toBe("https://fixture.example/short");
      expect(redirected.complete).toBe(true);
      await expect(
        worker.capture(
          {
            taskId: "outside",
            purpose: "documents",
            url: "https://fixture.example/redirect-outside",
            scope: { origin: "https://fixture.example", directory: "/" },
          },
          AbortSignal.timeout(60_000),
        ),
      ).rejects.toThrow(/范围/);
      expect(calls).not.toContain("https://outside.example/short");
      const framed = await worker.capture(
        {
          taskId: "frames",
          purpose: "import",
          url: "https://fixture.example/frame-root",
        },
        AbortSignal.timeout(60_000),
      );
      expect(framed.markdown).toContain("动态请求完成");
      expect(calls).toContain("https://frame.example/short");
      const cancel = new AbortController();
      setTimeout(() => cancel.abort(), 200);
      await expect(
        worker.capture(
          {
            taskId: "cancel",
            purpose: "import",
            url: "https://fixture.example/slow",
          },
          cancel.signal,
        ),
      ).rejects.toThrow(/取消/);
      expect(calls).toContain("https://fixture.example/docs/start");
      // 只结束本测试拥有的 worker，验证浏览器一起回收且下一次采集能重新初始化。
      (worker as unknown as { worker: ChildProcess }).worker.kill();
      await vi.waitFor(() => expect(worker.running).toBe(false));
      const restarted = await worker.capture(
        {
          taskId: "restarted",
          purpose: "import",
          url: "https://fixture.example/short",
        },
        AbortSignal.timeout(60_000),
      );
      expect(restarted.complete).toBe(true);
      await worker.close();
      expect(worker.running).toBe(false);
    }, 180_000);
  },
);
