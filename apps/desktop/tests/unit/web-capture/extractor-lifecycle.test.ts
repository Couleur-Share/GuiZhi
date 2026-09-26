import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EventEmitter } from "node:events";
import { PassThrough, Writable } from "node:stream";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { WebHtmlExtractor } from "../../../src/main/services/web-capture/web-html-extractor";

const mocks = vi.hoisted(() => ({
  spawn: vi.fn(),
  verify: vi.fn(),
  directory: "",
}));
vi.mock("node:child_process", async (original) => {
  const actual = await original<typeof import("node:child_process")>();
  return {
    ...actual,
    spawn: mocks.spawn,
    default: { ...actual, spawn: mocks.spawn },
  };
});
vi.mock("../../../src/main/runtime-paths", () => ({
  getUserDataPath: () => mocks.directory,
}));
vi.mock("../../../src/main/services/web-capture/web-runtime", () => ({
  verifyWebRuntime: mocks.verify,
  webRuntimeRoot: () => "/runtime",
  workerRoot: () => "/worker",
  runtimeFile: (root: string, name: string) => root + "/" + name,
}));
const result = { markdown: "中文正文", complete: true, paragraphs: [] };
class Child extends EventEmitter {
  pid = 123456;
  exitCode: number | null = null;
  killed = false;
  stdout = new PassThrough();
  stderr = new PassThrough();
  messages: Record<string, unknown>[] = [];
  hold = false;
  bad = false;
  stdin = new Writable({
    write: (chunk, _encoding, callback) => {
      const message = JSON.parse(chunk.toString());
      this.messages.push(message);
      callback();
      if (!this.hold) queueMicrotask(() => this.reply(message));
    },
  });
  constructor() {
    super();
    this.stdin.once("finish", () => this.exit());
    queueMicrotask(() => this.stdout.write('{"v":1,"type":"ready"}\n'));
  }
  reply(message = this.messages[0]) {
    const bytes = Buffer.from(
      JSON.stringify({ v: 1, id: message.id, result }) + "\n",
    );
    if (this.bad) return this.stdout.write('{"v":7}\n');
    // 故意在中文 UTF-8 编码中间分帧。
    const split = bytes.indexOf(Buffer.from("中文")) + 1;
    this.stdout.write(bytes.subarray(0, split));
    this.stdout.write(bytes.subarray(split));
  }
  exit() {
    if (this.exitCode === null) {
      this.exitCode = 0;
      this.emit("exit", 0);
    }
  }
  kill() {
    this.killed = true;
    this.exit();
    return true;
  }
}
let extractor: WebHtmlExtractor;
let children: Child[];
beforeEach(async () => {
  mocks.directory = await fs.mkdtemp(
    path.join(os.tmpdir(), "guizhi-extractor-"),
  );
  children = [];
  mocks.verify.mockReset().mockResolvedValue({ python: "python.exe" });
  mocks.spawn.mockReset().mockImplementation(() => {
    const child = new Child();
    children.push(child);
    return child;
  });
  extractor = new WebHtmlExtractor();
});
afterEach(async () => {
  await extractor.close();
  vi.useRealTimers();
  await fs.rm(mocks.directory, { recursive: true, force: true });
});
const capture = (signal = new AbortController().signal) =>
  extractor.extract("<p>正文</p>", "https://example.com/", 200, signal);

describe("正文提取进程生命周期", () => {
  it("复用单一 Python，正确拼接 UTF-8；显式关闭后可重新启动", async () => {
    expect(await capture()).toMatchObject(result);
    await capture();
    expect(mocks.spawn).toHaveBeenCalledTimes(1);
    expect(mocks.spawn.mock.calls[0][1]).toEqual([
      "-B",
      "-s",
      path.join("/worker", "extract-only.py"),
    ]);
    await extractor.close();
    expect(extractor.running).toBe(false);
    await capture();
    expect(mocks.spawn).toHaveBeenCalledTimes(2);
  });
  it("取消排队者不杀正在处理的任务", async () => {
    await capture();
    children[0].hold = true;
    const active = capture();
    await vi.waitFor(() => expect(children[0].messages.length).toBe(2));
    const controller = new AbortController();
    const queued = capture(controller.signal);
    const rejected = expect(queued).rejects.toThrow("取消");
    controller.abort();
    await rejected;
    expect(children[0].exitCode).toBeNull();
    children[0].reply(children[0].messages[1]);
    expect(await active).toMatchObject(result);
    expect(children[0].messages).toHaveLength(2);
  });
  it("取消活跃者回收旧进程，排队者用新进程继续", async () => {
    await capture();
    children[0].hold = true;
    const controller = new AbortController();
    const active = capture(controller.signal);
    const rejected = expect(active).rejects.toThrow("取消");
    await vi.waitFor(() => expect(children[0].messages.length).toBe(2));
    const queued = capture();
    controller.abort();
    await rejected;
    expect(await queued).toMatchObject(result);
    expect(children).toHaveLength(2);
    expect(children[0].exitCode).toBe(0);
  });
  it("校验期间取消后不会迟到地启动进程", async () => {
    let finish!: (value: unknown) => void;
    mocks.verify.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const controller = new AbortController();
    const active = capture(controller.signal);
    const rejected = expect(active).rejects.toThrow("取消");
    await vi.waitFor(() => expect(mocks.verify).toHaveBeenCalled());
    controller.abort();
    await rejected;
    finish({ python: "python.exe" });
    await Promise.resolve();
    expect(mocks.spawn).not.toHaveBeenCalled();
    await capture();
    expect(mocks.spawn).toHaveBeenCalledTimes(1);
  });
  it("空闲 60 秒退出，清理本实例缓存", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await capture();
    await vi.advanceTimersByTimeAsync(60_000);
    await extractor.close();
    expect(extractor.running).toBe(false);
    expect(
      await fs.readdir(path.join(mocks.directory, "cache", "web-capture")),
    ).toEqual([]);
  });
  it("协议错误回收进程，重试不会消费旧响应", async () => {
    await capture();
    children[0].bad = true;
    await expect(capture()).rejects.toThrow("协议");
    expect(await capture()).toMatchObject(result);
    expect(children).toHaveLength(2);
  });
  it("关闭同时取消活跃和排队任务", async () => {
    await capture();
    children[0].hold = true;
    const active = capture(),
      queued = capture();
    const rejected = Promise.all([
      expect(active).rejects.toThrow("取消"),
      expect(queued).rejects.toThrow("取消"),
    ]);
    await extractor.close();
    await rejected;
    expect(extractor.running).toBe(false);
  });
});
