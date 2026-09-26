import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
import type { WebCaptureResult } from "@guizhi/shared/types";
import { getUserDataPath } from "../../runtime-paths";
import {
  runtimeFile,
  verifyWebRuntime,
  webRuntimeRoot,
  workerRoot,
} from "./web-runtime";
import { cleanAbandonedWebCaches } from "./web-cache";
import { WebTaskGate, withWebAbort, webAbortError } from "./web-task-gate";

export type WebExtracted = Omit<
  WebCaptureResult,
  "taskId" | "entryUrl" | "finalUrl" | "links" | "capturedAt" | "engineVersion"
>;
const FRAME_LIMIT = 16 * 1024 * 1024;

/** 一个离线 Python 进程，串行提取；活跃任务取消后回收进程，下一任务重新启动。 */
export class WebHtmlExtractor {
  private child?: ChildProcessWithoutNullStreams;
  private cache?: string;
  private gate = new WebTaskGate(1);
  private lifetime = new AbortController();
  private operations = new Set<Promise<unknown>>();
  private idle?: ReturnType<typeof setTimeout>;
  private closing?: Promise<void>;
  get running(): boolean {
    return !!this.child && this.child.exitCode === null && !this.child.killed;
  }

  async extract(
    html: string,
    url: string,
    status: number,
    signal: AbortSignal,
  ): Promise<WebExtracted> {
    while (this.closing !== undefined) await withWebAbort(this.closing, signal);
    clearTimeout(this.idle);
    const combined = AbortSignal.any([signal, this.lifetime.signal]);
    const work = this.gate.run(combined, async () => {
      try {
        const id = randomUUID();
        const input = JSON.stringify({ v: 1, id, html, url, status }) + "\n";
        if (Buffer.byteLength(input) > FRAME_LIMIT)
          throw new Error("正文提取输入超过 16 MiB");
        await this.start(combined);
        const message = await this.frame(combined, input);
        if (
          message.v !== 1 ||
          message.id !== id ||
          message.error ||
          !message.result
        )
          throw new Error("正文提取协议无效");
        const result = message.result as WebExtracted;
        if (
          typeof result.markdown !== "string" ||
          typeof result.complete !== "boolean" ||
          !Array.isArray(result.paragraphs)
        )
          throw new Error("正文提取结果无效");
        return result;
      } catch (error) {
        await this.stopChild();
        throw error;
      }
    });
    this.operations.add(work);
    try {
      return await work;
    } finally {
      this.operations.delete(work);
      if (!this.operations.size && this.closing === undefined) {
        this.idle = setTimeout(() => {
          void this.close();
        }, 60_000);
        this.idle.unref();
      }
    }
  }

  private async start(signal: AbortSignal): Promise<void> {
    if (this.running) return;
    await this.stopChild();
    const manifest = await withWebAbort(verifyWebRuntime(), signal);
    if (signal.aborted) throw webAbortError(signal);
    const parent = path.join(getUserDataPath(), "cache", "web-capture");
    await fs.mkdir(parent, { recursive: true });
    await cleanAbandonedWebCaches(parent);
    this.cache = await fs.mkdtemp(path.join(parent, "owned-"));
    await fs.writeFile(
      path.join(this.cache, "owner.json"),
      JSON.stringify({
        component: "guizhi-web-capture",
        pid: process.pid,
        createdAt: Date.now(),
        id: randomUUID(),
      }),
    );
    if (signal.aborted) throw webAbortError(signal);
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      CRAWL4_AI_BASE_DIRECTORY: this.cache,
      PYTHONUNBUFFERED: "1",
      PYTHONIOENCODING: "utf-8",
      PYTHONDONTWRITEBYTECODE: "1",
    };
    delete env.PYTHONPATH;
    delete env.PYTHONHOME;
    this.child = spawn(
      runtimeFile(webRuntimeRoot(), manifest.python),
      ["-B", "-s", path.join(workerRoot(), "extract-only.py")],
      { windowsHide: true, stdio: "pipe", env },
    );
    // 第三方日志可能含网址与凭证；不写入应用日志。监听空闲期的管道错误避免未处理异常。
    this.child.stderr.resume();
    this.child.on("error", () => undefined);
    this.child.stdin.on("error", () => undefined);
    const ready = await this.frame(signal);
    if (ready.v !== 1 || ready.type !== "ready")
      throw new Error("正文提取组件初始化协议无效");
  }

  private frame(
    signal: AbortSignal,
    input?: string,
  ): Promise<Record<string, unknown>> {
    const child = this.child!;
    return new Promise((resolve, reject) => {
      let buffer = Buffer.alloc(0);
      let done = false;
      const finish = (error?: Error, message?: Record<string, unknown>) => {
        if (done) return;
        done = true;
        clearTimeout(timer);
        signal.removeEventListener("abort", abort);
        child.stdout.off("data", data);
        child.off("exit", exit);
        child.off("error", fail);
        child.stdin.off("error", fail);
        if (error) reject(error);
        else resolve(message!);
      };
      const abort = () => finish(webAbortError(signal));
      const exit = () => finish(new Error("正文提取进程提前退出"));
      const fail = () => finish(new Error("正文提取进程通信失败"));
      const data = (chunk: Buffer) => {
        buffer = Buffer.concat([buffer, chunk]);
        if (buffer.length > FRAME_LIMIT)
          return finish(new Error("正文提取输出超过 16 MiB"));
        const end = buffer.indexOf(10);
        if (end < 0) return;
        try {
          if (end !== buffer.length - 1) throw new Error("多余协议帧");
          finish(
            undefined,
            JSON.parse(buffer.subarray(0, end).toString("utf8")),
          );
        } catch {
          finish(new Error("正文提取协议无效"));
        }
      };
      const timer = setTimeout(() => finish(new Error("正文提取超时")), 45_000);
      child.stdout.on("data", data);
      child.once("exit", exit);
      child.once("error", fail);
      child.stdin.once("error", fail);
      signal.addEventListener("abort", abort, { once: true });
      if (signal.aborted) return abort();
      if (child.exitCode !== null || child.killed) return exit();
      if (input) child.stdin.write(input);
    });
  }

  private async stopChild(): Promise<void> {
    const child = this.child,
      owned = this.cache;
    this.child = undefined;
    this.cache = undefined;
    if (child?.pid && child.exitCode === null) {
      await new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          child.kill();
        }, 1500);
        child.once("exit", () => {
          clearTimeout(timer);
          resolve();
        });
        child.once("error", () => {
          clearTimeout(timer);
          resolve();
        });
        child.stdin.end();
      });
    }
    if (
      owned &&
      path.dirname(owned) ===
        path.join(getUserDataPath(), "cache", "web-capture") &&
      path.basename(owned).startsWith("owned-")
    )
      await fs
        .rm(owned, { recursive: true, force: true })
        .catch(() => undefined);
  }

  close(): Promise<void> {
    if (this.closing !== undefined) return this.closing;
    clearTimeout(this.idle);
    this.lifetime.abort();
    this.closing = (async () => {
      await Promise.allSettled([...this.operations]);
      await this.stopChild();
    })().finally(() => {
      this.lifetime = new AbortController();
      this.closing = undefined;
    });
    return this.closing;
  }
}
