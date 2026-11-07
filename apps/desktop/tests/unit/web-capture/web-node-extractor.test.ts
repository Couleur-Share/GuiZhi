// @vitest-environment node
import { EventEmitter } from "node:events";
import http from "node:http";
import type { AddressInfo } from "node:net";
import path from "node:path";
import { PassThrough } from "node:stream";
import { Worker } from "node:worker_threads";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { extractWebArticle } from "../../../src/main/services/web-capture/web-extract-article";
import { WebNodeExtractor } from "../../../src/main/services/web-capture/web-node-extractor";

const result = { markdown: "中文正文", complete: true, paragraphs: [] };

/** 按 worker_threads 接口模拟的提取线程：可挂起、回报协议错误或提取异常。 */
class FakeWorker extends EventEmitter {
  messages: Array<Record<string, unknown>> = [];
  hold = false;
  bad = false;
  failure?: unknown;
  terminated = false;
  stdout = new PassThrough();
  stderr = new PassThrough();
  postMessage(message: Record<string, unknown>) {
    this.messages.push(message);
    if (!this.hold) queueMicrotask(() => this.reply(message));
  }
  reply(message = this.messages.at(-1)!) {
    if (this.bad) return this.emit("message", { v: 7 });
    if (this.failure !== undefined) return this.emit("message", { v: 1, id: message.id, error: this.failure });
    this.emit("message", { v: 1, id: message.id, result });
  }
  ref() {}
  unref() {}
  async terminate() {
    if (!this.terminated) {
      this.terminated = true;
      this.emit("exit", 1);
    }
    return 1;
  }
}

let workers: FakeWorker[];
let create: ReturnType<typeof vi.fn>;
let extractor: WebNodeExtractor;
beforeEach(() => {
  workers = [];
  create = vi.fn(async () => {
    const worker = new FakeWorker();
    workers.push(worker);
    return worker as unknown as Worker;
  });
  extractor = new WebNodeExtractor(create as () => Promise<Worker>);
});
afterEach(async () => {
  await extractor.close();
  vi.useRealTimers();
});
const capture = (signal = new AbortController().signal, html = "<p>正文</p>") =>
  extractor.extract(html, "https://example.com/", 200, signal);

describe("R6 提取线程生命周期", () => {
  it("复用同一线程；显式关闭后可重新启动", async () => {
    expect(await capture()).toMatchObject(result);
    await capture();
    expect(create).toHaveBeenCalledTimes(1);
    expect(workers[0].messages[0]).toMatchObject({ v: 1, html: "<p>正文</p>", url: "https://example.com/", status: 200 });
    await extractor.close();
    expect(extractor.running).toBe(false);
    expect(workers[0].terminated).toBe(true);
    await capture();
    expect(create).toHaveBeenCalledTimes(2);
  });

  it("取消排队者不影响正在处理的任务", async () => {
    await capture();
    workers[0].hold = true;
    const active = capture();
    await vi.waitFor(() => expect(workers[0].messages).toHaveLength(2));
    const controller = new AbortController();
    const queued = capture(controller.signal);
    const rejected = expect(queued).rejects.toThrow("取消");
    controller.abort();
    await rejected;
    expect(workers[0].terminated).toBe(false);
    workers[0].reply();
    expect(await active).toMatchObject(result);
    expect(workers[0].messages).toHaveLength(2);
  });

  it("取消活跃任务回收线程，排队任务用新线程继续", async () => {
    await capture();
    workers[0].hold = true;
    const controller = new AbortController();
    const active = capture(controller.signal);
    const rejected = expect(active).rejects.toThrow("取消");
    await vi.waitFor(() => expect(workers[0].messages).toHaveLength(2));
    const queued = capture();
    controller.abort();
    await rejected;
    expect(await queued).toMatchObject(result);
    expect(workers).toHaveLength(2);
    expect(workers[0].terminated).toBe(true);
  });

  it("线程创建期间取消，创建完成后立即结束，不留下迟到的线程", async () => {
    let finish!: (worker: Worker) => void;
    create.mockImplementationOnce(() => new Promise<Worker>((resolve) => (finish = resolve)));
    const controller = new AbortController();
    const active = capture(controller.signal);
    const rejected = expect(active).rejects.toThrow("取消");
    await vi.waitFor(() => expect(create).toHaveBeenCalled());
    controller.abort();
    await rejected;
    const late = new FakeWorker();
    finish(late as unknown as Worker);
    await vi.waitFor(() => expect(late.terminated).toBe(true));
    expect(extractor.running).toBe(false);
    await capture();
    expect(create).toHaveBeenCalledTimes(2);
  });

  it("45 秒没有结果按超时失败并回收线程", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await capture();
    workers[0].hold = true;
    const pending = capture();
    const rejected = expect(pending).rejects.toThrow("正文提取超时");
    await vi.advanceTimersByTimeAsync(45_000);
    await rejected;
    expect(workers[0].terminated).toBe(true);
    expect(extractor.running).toBe(false);
  });

  it("空闲 60 秒结束线程", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await capture();
    expect(extractor.running).toBe(true);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(workers[0].terminated).toBe(true);
    expect(extractor.running).toBe(false);
  });

  it("输入超过 16 MiB 直接失败，不启动线程", async () => {
    await expect(capture(undefined, "a".repeat(16 * 1024 * 1024 + 1))).rejects.toThrow("正文提取输入超过 16 MiB");
    expect(create).not.toHaveBeenCalled();
  });

  it("协议错误回收线程，重试不会消费旧响应", async () => {
    await capture();
    workers[0].bad = true;
    await expect(capture()).rejects.toThrow("协议");
    expect(workers[0].terminated).toBe(true);
    expect(await capture()).toMatchObject(result);
    expect(workers).toHaveLength(2);
  });

  it("线程回报的页面异常只带出类型名，线程保留供后续任务复用", async () => {
    await capture();
    workers[0].failure = "TypeError";
    const failed = (await capture().catch((reason: Error) => reason)) as Error;
    expect(failed.message).toBe("正文提取失败（TypeError）");
    expect(failed.message).not.toContain("协议");
    expect(workers[0].terminated).toBe(false);
    workers[0].failure = "Type Error: C:\\Users\\secret\\页面内容";
    expect(((await capture().catch((reason: Error) => reason)) as Error).message).toBe("正文提取失败（未知异常）");
    workers[0].failure = undefined;
    expect(await capture()).toMatchObject(result);
    expect(create).toHaveBeenCalledTimes(1);
  });

  it("线程内存超限给出可理解的原因并回收线程", async () => {
    await capture();
    workers[0].hold = true;
    const pending = capture();
    await vi.waitFor(() => expect(workers[0].messages).toHaveLength(2));
    workers[0].emit("error", Object.assign(new Error("heap"), { code: "ERR_WORKER_OUT_OF_MEMORY" }));
    await expect(pending).rejects.toThrow("正文提取超过内存限制");
    expect(workers[0].terminated).toBe(true);
  });

  it("关闭同时取消活跃和排队任务", async () => {
    await capture();
    workers[0].hold = true;
    const active = capture(),
      queued = capture();
    const rejected = Promise.all([expect(active).rejects.toThrow("取消"), expect(queued).rejects.toThrow("取消")]);
    await extractor.close();
    await rejected;
    expect(extractor.running).toBe(false);
  });
});

describe("R6 真实提取线程", () => {
  const bootstrap = path.join(__dirname, "fixtures", "web-extract-worker-bootstrap.cjs");
  const entry = path.resolve(__dirname, "../../../src/main/services/web-capture/web-extract-worker.ts");
  let server: http.Server;
  let port: number;
  let connections: number;
  let real: WebNodeExtractor;
  beforeEach(async () => {
    connections = 0;
    server = http.createServer((_request, response) => response.end("ok"));
    server.on("connection", () => connections++);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    port = (server.address() as AddressInfo).port;
    real = new WebNodeExtractor(async () => new Worker(bootstrap, { workerData: { entry } }));
  });
  afterEach(async () => {
    await real.close();
    await new Promise((resolve) => server.close(resolve));
  });
  const resourcePage = () =>
    `<!doctype html><html><head><title>资源页</title><link rel="stylesheet" href="http://127.0.0.1:${port}/a.css">` +
    `<script src="http://127.0.0.1:${port}/a.js"></script><style>@import url("http://127.0.0.1:${port}/b.css");</style></head><body><article>` +
    Array.from({ length: 4 }, (_, i) => `<p>第 ${i + 1} 段正文，包含足够的文字让提取器把它当作文章内容保留下来。</p>`).join("") +
    `<img src="http://127.0.0.1:${port}/a.png" srcset="http://127.0.0.1:${port}/b.png 2x" alt="图">` +
    `<iframe src="http://127.0.0.1:${port}/frame"></iframe><video poster="http://127.0.0.1:${port}/p.png" src="http://127.0.0.1:${port}/v.mp4"></video>` +
    `<object data="http://127.0.0.1:${port}/o.swf"></object><link rel="prefetch" href="http://127.0.0.1:${port}/next">` +
    `</article></body></html>`;

  it("真实线程的结果与进程内提取一致", async () => {
    const html = resourcePage();
    const output = await real.extract(html, "https://example.com/a", 200, new AbortController().signal);
    expect(output).toEqual(extractWebArticle(html, "https://example.com/a", 200).result);
    expect(output.markdown).toContain(`![图](http://127.0.0.1:${port}/a.png)`);
  });

  it("解析含外部资源的页面不发起任何网络请求（进程内与线程内）", async () => {
    extractWebArticle(resourcePage(), "https://example.com/a", 200);
    await real.extract(resourcePage(), "https://example.com/a", 200, new AbortController().signal);
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(connections).toBe(0);
  });

  it("提取线程内的网络接口全部被禁用", async () => {
    const worker = new Worker(bootstrap, { workerData: { entry, probePort: port } });
    try {
      const probe = await new Promise<Record<string, string>>((resolve, reject) => {
        worker.on("message", (message: { probe?: Record<string, string> }) => message.probe && resolve(message.probe));
        worker.once("error", reject);
      });
      expect(Object.keys(probe)).toHaveLength(9);
      for (const outcome of Object.values(probe)) expect(outcome).toBe("正文提取线程不得发起网络请求");
    } finally {
      await worker.terminate();
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(connections).toBe(0);
  });
});
