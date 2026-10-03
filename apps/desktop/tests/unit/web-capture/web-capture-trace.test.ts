// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { captureWebPage } from "../../../src/main/services/web-capture/web-capture";
import { WEB_TRACE_ENV, type WebTrace } from "../../../src/main/services/web-capture/web-trace";

const mocks = vi.hoisted(() => ({ capture: vi.fn(), log: vi.fn() }));
vi.mock("../../../src/main/services/web-capture/wechat", () => ({
  isWechatUrl: () => false,
  captureWechat: vi.fn(),
}));
vi.mock("../../../src/main/services/web-capture/web-runtime", () => ({
  webRuntimeStatus: async () => ({ available: true }),
}));
vi.mock("../../../src/main/services/web-capture/web-electron-capture", () => ({
  ElectronWebCapture: class {
    capture = mocks.capture;
    running = false;
    close = async () => undefined;
  },
}));
vi.mock("../../../src/main/diagnostic-log", () => ({ logAppError: mocks.log }));

const request = (url = "https://example.com/page") => ({
  taskId: "t",
  purpose: "import" as const,
  url,
});
const okResult = (error?: { code: string }) => ({
  taskId: "t",
  entryUrl: "https://example.com/page",
  finalUrl: "https://example.com/page",
  title: "标题",
  author: "",
  publishedAt: null,
  dateConfidence: "unknown",
  markdown: "正文",
  links: [],
  paragraphs: [],
  contentHash: "hash",
  capturedAt: 1,
  engineVersion: "crawl4ai/0.9.3-static",
  complete: !error,
  truncated: false,
  warnings: [],
  ...(error ? { error } : {}),
});

let dir: string;
let file: string;
beforeEach(async () => {
  vi.clearAllMocks();
  mocks.capture.mockReset();
  dir = await mkdtemp(path.join(tmpdir(), "guizhi-capture-trace-"));
  file = path.join(dir, "trace.jsonl");
  process.env[WEB_TRACE_ENV] = file;
});
afterEach(async () => {
  delete process.env[WEB_TRACE_ENV];
  await rm(dir, { recursive: true, force: true });
});
const urlHash = (url: string) =>
  createHash("sha256").update(url).digest("hex").slice(0, 12);
const rows = async () =>
  (await readFile(file, "utf8"))
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));

describe("captureWebPage 诊断追踪", () => {
  it("成功采集写入一行并把追踪对象交给采集器", async () => {
    mocks.capture.mockImplementation(
      async (_request, _signal, _stage, trace: WebTrace) => {
        trace.mark("pageGate");
        trace.set({ engine: "static" });
        return okResult();
      },
    );
    await captureWebPage(request());
    expect(mocks.capture.mock.calls[0][3].enabled).toBe(true);
    const [row] = await rows();
    expect(row).toMatchObject({
      origin: "https://example.com",
      purpose: "import",
      engine: "static",
      outcome: "ok",
    });
    expect(row.ms).toEqual(
      expect.objectContaining({
        total: expect.any(Number),
        queued: expect.any(Number),
        pageGate: expect.any(Number),
        tail: expect.any(Number),
      }),
    );
  });

  it("采集抛错时返回失败结果，追踪 outcome 为同一错误码", async () => {
    mocks.capture.mockRejectedValue(new Error("网络不可达"));
    const result = await captureWebPage(request());
    expect(result.error).toBeDefined();
    const [row] = await rows();
    expect(row.outcome).toBe(result.error!.code);
    expect(row.outcome).not.toBe("ok");
  });

  it("结果自带错误码（如抽取为空）时 outcome 取该错误码", async () => {
    mocks.capture.mockResolvedValue(okResult({ code: "empty" }));
    await captureWebPage(request());
    const [row] = await rows();
    expect(row.outcome).toBe("empty");
  });

  it("未设置环境变量时采集器拿到空追踪，不写文件", async () => {
    delete process.env[WEB_TRACE_ENV];
    mocks.capture.mockResolvedValue(okResult());
    await captureWebPage(request());
    expect(mocks.capture.mock.calls[0][3].enabled).toBe(false);
    expect(await readdir(dir)).toEqual([]);
  });

  it("同源串行的等待时间计入 queued 阶段", async () => {
    let release!: () => void;
    mocks.capture.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = () => resolve(okResult());
        }),
    );
    mocks.capture.mockImplementationOnce(async () => okResult());
    const first = captureWebPage(request("https://example.com/a"));
    await vi.waitFor(() => expect(mocks.capture).toHaveBeenCalledTimes(1));
    const second = captureWebPage(request("https://example.com/b"));
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(mocks.capture).toHaveBeenCalledTimes(1);
    release();
    await Promise.all([first, second]);
    // 两个采集几乎同时落盘，文件里的行序不保证；按网址哈希对号入座。
    const byHash = new Map((await rows()).map((row) => [row.urlHash, row]));
    const firstRow = byHash.get(urlHash("https://example.com/a"));
    const secondRow = byHash.get(urlHash("https://example.com/b"));
    expect(firstRow).toBeDefined();
    expect(secondRow).toBeDefined();
    expect(firstRow.ms.queued).toBeLessThan(40);
    expect(secondRow.ms.queued).toBeGreaterThanOrEqual(70);
  });
});
