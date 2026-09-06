import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { getUserDataPath } from "../../runtime-paths";
import {
  runtimeFile,
  verifyWebRuntime,
  webRuntimeRoot,
  workerRoot,
} from "./web-runtime";
import { webNetworkRequest, type WebNetworkRequest } from "./web-network";
import { inWebScope } from "@guizhi/shared/utils/web-scope";
import type {
  WebCaptureRequest,
  WebCaptureResult,
  ImportStage,
} from "@guizhi/shared/types";
import { webCaptureError } from "./web-error";
import { cleanAbandonedWebCaches } from "./web-cache";

const FRAME_LIMIT = 16 * 1024 * 1024;
interface Task {
  request: WebCaptureRequest;
  controller: AbortController;
  resolve: (result: WebCaptureResult) => void;
  reject: (e: Error) => void;
  stage?: (s: ImportStage) => void;
  requests: number;
  bytes: number;
}
interface Message {
  v: number;
  type: string;
  taskId?: string;
  id?: string;
  stage?: ImportStage;
  result?: WebCaptureResult;
  error?: string;
  code?: string;
  request?: WebNetworkRequest;
  navigation?: boolean;
  mainFrame?: boolean;
}
export class WebWorker {
  private worker?: ChildProcessWithoutNullStreams;
  private browser?: ChildProcessWithoutNullStreams;
  private tasks = new Map<string, Task>();
  private init?: Promise<void>;
  private closing?: Promise<void>;
  private cache?: string;
  private idle?: ReturnType<typeof setTimeout>;
  private generation = 0;
  private initializationUsers = 0;
  private networkActive = 0;
  private networkWaiters = new Set<() => void>();
  get running(): boolean {
    return !!this.worker;
  }
  private send(value: Record<string, unknown>): void {
    const line = JSON.stringify({ v: 1, ...value }) + "\n";
    if (Buffer.byteLength(line) > FRAME_LIMIT)
      throw new Error("采集协议消息过大");
    if (!this.worker?.stdin.writable) throw new Error("网页组件已退出");
    this.worker.stdin.write(line);
  }
  private async start(): Promise<void> {
    if (this.closing !== undefined) await this.closing;
    if (this.init !== undefined) return this.init;
    const generation = this.generation;
    this.init = this.initialize(generation).catch(async (error) => {
      if (generation === this.generation) await this.close();
      throw error;
    });
    return this.init;
  }
  private async initialize(generation: number): Promise<void> {
    const manifest = await verifyWebRuntime();
    if (generation !== this.generation) throw new Error("网页组件初始化已取消");
    const parent = path.join(getUserDataPath(), "cache", "web-capture");
    await fs.mkdir(parent, { recursive: true });
    await cleanAbandonedWebCaches(parent);
    this.cache = await fs.mkdtemp(path.join(parent, "owned-"));
    await fs.writeFile(
      path.join(this.cache, "owner.json"),
      JSON.stringify({
        component: "guizhi-web-capture",
        id: randomUUID(),
        pid: process.pid,
        createdAt: Date.now(),
      }),
    );
    if (generation !== this.generation) throw new Error("网页组件初始化已取消");
    const args = [
      "--headless=new",
      "--remote-debugging-address=127.0.0.1",
      "--remote-debugging-port=0",
      `--user-data-dir=${path.join(this.cache, "browser")}`,
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-background-networking",
      "--disable-component-update",
      "--disable-sync",
      "--disable-quic",
      "--disk-cache-size=104857600",
      "--media-cache-size=10485760",
      "--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
      "--proxy-server=http://127.0.0.1:9",
      "--proxy-bypass-list=<-loopback>",
      "about:blank",
    ];
    this.browser = spawn(
      runtimeFile(webRuntimeRoot(), manifest.browser),
      args,
      {
        windowsHide: true,
        stdio: "pipe",
        detached: process.platform !== "win32",
      },
    );
    const endpoint = await new Promise<string>((resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error("采集浏览器初始化超时")),
        45_000,
      );
      let buffer = "";
      this.browser!.stderr.on("data", (chunk) => {
        buffer = (buffer + chunk.toString()).slice(-8192);
        const match =
          /DevTools listening on (ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+)/.exec(
            buffer,
          );
        if (match) {
          clearTimeout(timeout);
          resolve(match[1]);
        }
      });
      this.browser!.once("error", (error) => {
        clearTimeout(timeout);
        reject(error);
      });
      this.browser!.once("exit", () => {
        clearTimeout(timeout);
        reject(new Error("采集浏览器提前退出"));
      });
    });
    if (generation !== this.generation) throw new Error("网页组件初始化已取消");
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      CRAWL4_AI_BASE_DIRECTORY: this.cache,
      PYTHONUNBUFFERED: "1",
      PYTHONIOENCODING: "utf-8",
      PYTHONDONTWRITEBYTECODE: "1",
    };
    delete env.PYTHONPATH;
    delete env.PYTHONHOME;
    this.worker = spawn(
      runtimeFile(webRuntimeRoot(), manifest.python),
      ["-s", path.join(workerRoot(), "worker.py"), endpoint],
      {
        windowsHide: true,
        stdio: "pipe",
        env,
        detached: process.platform !== "win32",
      },
    );
    this.worker.stderr.resume(); // 库日志可能包含 URL 查询与凭证，不写入普通日志。
    const ownedWorker = this.worker,
      ownedBrowser = this.browser;
    this.worker.stdin.on("error", () =>
      this.failAll(new Error("网页组件通信中断")),
    );
    this.browser.once("exit", () => {
      if (this.browser === ownedBrowser) void this.close();
    });
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error("网页组件初始化超时")),
        45_000,
      );
      let buffer = Buffer.alloc(0);
      this.worker!.stdout.on("data", (chunk) => {
        buffer = Buffer.concat([buffer, chunk]);
        if (buffer.length > FRAME_LIMIT) {
          reject(new Error("采集组件输出超限"));
          void this.close();
          return;
        }
        let end: number;
        while ((end = buffer.indexOf(10)) >= 0) {
          const line = buffer.subarray(0, end);
          buffer = buffer.subarray(end + 1);
          try {
            const message: Message = JSON.parse(line.toString("utf8"));
            if (message.v !== 1) throw new Error("网页组件协议版本不匹配");
            if (message.type === "ready") {
              clearTimeout(timeout);
              resolve();
            } else
              void this.message(message).catch(() =>
                this.failAll(new Error("网页组件协议处理失败")),
              );
          } catch {
            clearTimeout(timeout);
            reject(new Error("网页组件协议无效"));
            void this.close();
          }
        }
      });
      this.worker!.once("error", (error) => {
        clearTimeout(timeout);
        reject(error);
      });
      this.worker!.once("exit", () => {
        clearTimeout(timeout);
        reject(new Error("网页组件提前退出"));
        if (this.worker === ownedWorker) {
          void this.close();
        }
      });
    });
  }
  private async message(message: Message): Promise<void> {
    const task = message.taskId ? this.tasks.get(message.taskId) : undefined;
    if (!task) return;
    if (message.type === "stage" && message.stage) task.stage?.(message.stage);
    if (message.type === "result" && message.result) {
      const result = message.result;
      if (
        task.request.scope &&
        !inWebScope(result.finalUrl, task.request.scope)
      )
        task.reject(new Error("最终网址超出目录范围"));
      else task.resolve(result);
    }
    if (message.type === "error")
      task.reject(
        new Error(message.error ?? "网页采集失败", {
          cause: { webCaptureCode: message.code },
        }),
      );
    if (message.type === "network" && message.request) {
      try {
        if (++task.requests > 200 || task.bytes > 50 * 1024 * 1024) {
          task.controller.abort();
          throw new Error("页面网络预算已用尽");
        }
        if (
          message.navigation &&
          message.mainFrame &&
          task.request.scope &&
          !inWebScope(message.request.url, task.request.scope)
        )
          throw new Error("导航超出目录范围");
        const response = await this.network(
          message.request,
          task.controller.signal,
        );
        task.bytes += Buffer.byteLength(response.body, "base64");
        if (task.bytes > 50 * 1024 * 1024) {
          task.controller.abort();
          throw new Error("页面网络响应总量超过 50 MiB");
        }
        this.send({ type: "network-result", id: message.id, response });
      } catch (error) {
        this.send({
          type: "network-result",
          id: message.id,
          response: { error: "网络请求被拒绝" },
        });
        // 导航失败必须终止，不能把浏览器错误页当作有效短文。
        if (
          (message.navigation && message.mainFrame) ||
          webCaptureError(error).code === "security" ||
          webCaptureError(error).code === "incomplete" ||
          task.controller.signal.aborted
        )
          task.reject(error instanceof Error ? error : new Error("导航被拒绝"));
      }
    }
  }
  private async network(request: WebNetworkRequest, signal: AbortSignal) {
    while (this.networkActive >= 8)
      await new Promise<void>((resolve, reject) => {
        signal.throwIfAborted();
        const wake = () => {
          this.networkWaiters.delete(wake);
          signal.removeEventListener("abort", abort);
          resolve();
        };
        const abort = () => {
          this.networkWaiters.delete(wake);
          reject(new Error("网络请求已取消"));
        };
        this.networkWaiters.add(wake);
        signal.addEventListener("abort", abort, { once: true });
      });
    signal.throwIfAborted();
    this.networkActive++;
    try {
      return await webNetworkRequest(request, signal);
    } finally {
      this.networkActive--;
      for (const wake of [...this.networkWaiters]) wake();
    }
  }
  async capture(
    request: WebCaptureRequest,
    signal: AbortSignal,
    stage?: (stage: ImportStage) => void,
  ): Promise<WebCaptureResult> {
    clearTimeout(this.idle);
    signal.throwIfAborted();
    this.initializationUsers++;
    try {
      await new Promise<void>((resolve, reject) => {
        const abort = () => reject(new Error("网页组件初始化已取消或超时"));
        signal.addEventListener("abort", abort, { once: true });
        this.start().then(
          () => {
            signal.removeEventListener("abort", abort);
            resolve();
          },
          (error) => {
            signal.removeEventListener("abort", abort);
            reject(error);
          },
        );
      });
    } finally {
      this.initializationUsers--;
      if (signal.aborted && !this.initializationUsers && !this.tasks.size)
        await this.close();
    }
    signal.throwIfAborted();
    const controller = new AbortController();
    try {
      return await new Promise<WebCaptureResult>((resolve, reject) => {
        let abortTimer: ReturnType<typeof setTimeout> | undefined;
        const onAbort = () => {
          controller.abort();
          try {
            this.send({ type: "cancel", taskId: request.taskId });
          } catch {
            reject(new Error("网页采集已取消"));
            return;
          }
          // 等待页面关闭确认后再释放并发槽；无响应的组件必须回收。
          abortTimer = setTimeout(() => {
            void this.close();
            reject(new Error("网页采集已取消或超时"));
          }, 2000);
        };
        signal.addEventListener("abort", onAbort, { once: true });
        this.tasks.set(request.taskId, {
          request,
          controller,
          requests: 0,
          bytes: 0,
          stage,
          resolve: (r) => {
            clearTimeout(abortTimer);
            signal.removeEventListener("abort", onAbort);
            if (signal.aborted) reject(new Error("网页采集已取消或超时"));
            else resolve(r);
          },
          reject: (e) => {
            clearTimeout(abortTimer);
            signal.removeEventListener("abort", onAbort);
            controller.abort();
            try {
              this.send({ type: "cancel", taskId: request.taskId });
            } catch {
              /* 组件退出 */
            }
            reject(e);
          },
        });
        this.send({
          type: "capture",
          taskId: request.taskId,
          url: request.url,
        });
      });
    } finally {
      controller.abort();
      this.tasks.delete(request.taskId);
      if (!this.tasks.size)
        this.idle = setTimeout(() => void this.close(), 60_000);
    }
  }
  private failAll(error: Error): void {
    for (const task of this.tasks.values()) {
      task.controller.abort();
      task.reject(error);
    }
  }
  async close(): Promise<void> {
    if (this.closing !== undefined) return this.closing;
    this.closing = this.closeOwned().finally(() => {
      this.closing = undefined;
    });
    return this.closing;
  }
  private async closeOwned(): Promise<void> {
    this.generation++;
    clearTimeout(this.idle);
    this.failAll(new Error("网页组件已停止"));
    const worker = this.worker,
      browser = this.browser,
      owned = this.cache;
    this.worker = undefined;
    this.browser = undefined;
    this.init = undefined;
    this.cache = undefined;
    if (worker?.stdin.writable)
      worker.stdin.end(JSON.stringify({ v: 1, type: "shutdown" }) + "\n");
    for (const child of [worker, browser]) {
      if (!child || child.exitCode !== null) continue;
      await new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          if (process.platform === "win32" && child.pid) {
            const killer = spawn(
              "taskkill",
              ["/PID", String(child.pid), "/T", "/F"],
              { windowsHide: true, stdio: "ignore" },
            );
            killer.once("exit", () => resolve());
            killer.once("error", () => {
              child.kill();
              resolve();
            });
          } else {
            try {
              if (child.pid) process.kill(-child.pid, "SIGKILL");
            } catch {
              /* 已退出 */
            }
            resolve();
          }
        }, 2000);
        child.once("exit", () => {
          clearTimeout(timer);
          resolve();
        });
      });
    }
    if (owned) {
      const marker = JSON.parse(
        await fs
          .readFile(path.join(owned, "owner.json"), "utf8")
          .catch(() => "{}"),
      );
      const parent =
        path.resolve(getUserDataPath(), "cache", "web-capture") + path.sep;
      if (
        marker.component === "guizhi-web-capture" &&
        path.resolve(owned).startsWith(parent)
      )
        await fs
          .rm(owned, { recursive: true, force: true })
          .catch(() => undefined);
    }
  }
}
