// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  NOOP_WEB_TRACE,
  WEB_TRACE_ENV,
  classifyAccept,
  createWebTrace,
  type WebTraceEgressSample,
} from "../../../src/main/services/web-capture/web-trace";

const mocks = vi.hoisted(() => ({ log: vi.fn() }));
vi.mock("../../../src/main/diagnostic-log", () => ({ logAppError: mocks.log }));

let dir: string;
beforeEach(async () => {
  vi.clearAllMocks();
  delete process.env[WEB_TRACE_ENV];
  dir = await mkdtemp(path.join(tmpdir(), "guizhi-web-trace-"));
});
afterEach(async () => {
  delete process.env[WEB_TRACE_ENV];
  await rm(dir, { recursive: true, force: true });
});

const sample = (patch: Partial<WebTraceEgressSample> = {}): WebTraceEgressSample => ({
  type: "script",
  method: "GET",
  origin: "https://cdn.example.com",
  responded: true,
  prepMs: 1,
  gateWaitMs: 0,
  socketMs: 10,
  requestMs: 100,
  reused: false,
  retried: false,
  failed: false,
  bytes: 1000,
  ...patch,
});
const rows = async (file: string) =>
  (await readFile(file, "utf8"))
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));

describe("网页采集诊断追踪", () => {
  it("未设置环境变量时返回共享空实现，不创建文件", async () => {
    const trace = createWebTrace("https://example.com/a", "import");
    expect(trace).toBe(NOOP_WEB_TRACE);
    expect(trace.enabled).toBe(false);
    trace.mark("queued");
    trace.set({ engine: "static" });
    trace.egress(sample());
    await expect(trace.flush()).resolves.toBeUndefined();
    expect(await readdir(dir)).toEqual([]);
  });

  it("读取环境变量启用，flush 追加一行 JSON", async () => {
    const file = path.join(dir, "trace.jsonl");
    process.env[WEB_TRACE_ENV] = file;
    const trace = createWebTrace("https://example.com/a", "documents");
    expect(trace.enabled).toBe(true);
    await trace.flush();
    const [row] = await rows(file);
    expect(row).toMatchObject({
      v: 1,
      origin: "https://example.com",
      purpose: "documents",
      engine: null,
      proxy: null,
      outcome: "unknown",
    });
    expect(row).not.toHaveProperty("route");
    expect(row).not.toHaveProperty("fallback");
    expect(row).not.toHaveProperty("egress");
  });

  it("阶段耗时按标记累加，总耗时与尾部耗时由收尾时间计算", async () => {
    const file = path.join(dir, "trace.jsonl");
    let t = 1000;
    const trace = createWebTrace("https://example.com/", "import", {
      file,
      now: () => t,
    });
    t = 1100;
    trace.mark("queued");
    t = 1400;
    trace.mark("static");
    t = 1500;
    trace.mark("render");
    t = 1900;
    trace.mark("render"); // 同名阶段累加（如静态为空后回退渲染）
    t = 2000;
    await trace.flush();
    const [row] = await rows(file);
    expect(row.ms).toEqual({
      total: 1000,
      queued: 100,
      static: 300,
      render: 500,
      tail: 100,
    });
  });

  it("只写入 origin 与网址哈希，不含路径、查询串、凭据或代理地址", async () => {
    const file = path.join(dir, "trace.jsonl");
    const url = "https://user:pa55word@example.com:8443/secret/path?token=abc123#frag";
    const trace = createWebTrace(url, "import", { file });
    trace.set({ proxy: "manual", route: { decision: "render", reasons: ["静态页面无正文"], textChars: 0, semanticRegions: 0, hasScripts: true } });
    await trace.flush();
    const text = await readFile(file, "utf8");
    for (const secret of ["pa55word", "user", "secret", "path", "token", "abc123", "frag"])
      expect(text).not.toContain(secret);
    const [row] = await rows(file);
    expect(row.origin).toBe("https://example.com:8443");
    expect(row.urlHash).toMatch(/^[0-9a-f]{12}$/);
    expect(row.proxy).toBe("manual");
  });

  it("出口统计：分位数采用最近秩法，区分新建与复用连接", async () => {
    const file = path.join(dir, "trace.jsonl");
    const trace = createWebTrace("https://example.com/", "import", { file });
    // requestMs 100..1000，偶数下标新建、奇数下标复用
    for (let i = 0; i < 10; i++)
      trace.egress(
        sample({
          type: i < 6 ? "script" : "image",
          requestMs: (i + 1) * 100,
          reused: i % 2 === 1,
          retried: i === 3,
          failed: i >= 8,
          error: i === 8 ? "ABORT_ERR" : i === 9 ? "ECONNRESET" : undefined,
          bytes: 10,
        }),
      );
    await trace.flush();
    const [{ egress }] = await rows(file);
    expect(egress).toMatchObject({
      requests: 10,
      bytes: 100,
      failed: 2,
      errors: { ABORT_ERR: 1, ECONNRESET: 1 },
      reusedSocket: 5,
      retried: 1,
      requestMs: { sum: 5500, p50: 500, p95: 1000, max: 1000 },
      freshMs: { sum: 2500, p50: 500, p95: 900, max: 900 },
      reusedMs: { sum: 3000, p50: 600, p95: 1000, max: 1000 },
    });
    expect(egress.byType).toEqual({
      script: { n: 6, bytes: 60, ms: 2100 },
      image: { n: 4, bytes: 40, ms: 3400 },
    });
  });

  it("失败明细只保留前 8 条，含错误码、目标 origin、复用与是否已响应", async () => {
    const file = path.join(dir, "trace.jsonl");
    const trace = createWebTrace("https://example.com/", "import", { file });
    trace.egress(sample()); // 成功的请求不进入明细
    for (let i = 0; i < 10; i++)
      trace.egress(
        sample({
          failed: true,
          error: i === 0 ? "ECONNRESET" : "ABORT_ERR",
          type: "image",
          method: i === 0 ? "POST" : "GET",
          origin: `https://t${i}.example.net`,
          reused: i === 0,
          responded: i === 0,
          requestMs: 123.6,
        }),
      );
    await trace.flush();
    const [{ egress }] = await rows(file);
    expect(egress.failed).toBe(10);
    expect(egress.errors).toEqual({ ECONNRESET: 1, ABORT_ERR: 9 });
    expect(egress.failures).toHaveLength(8);
    expect(egress.failures[0]).toEqual({
      error: "ECONNRESET",
      type: "image",
      method: "POST",
      origin: "https://t0.example.net",
      reused: true,
      responded: true,
      ms: 124,
    });
    expect(egress.failures[7].origin).toBe("https://t7.example.net");
  });

  it("快照展开 shadow root 的宿主数有值才写入", async () => {
    const file = path.join(dir, "trace.jsonl");
    const flattened = createWebTrace("https://example.com/a", "import", { file });
    flattened.set({ shadowRoots: 24 });
    await flattened.flush();
    const plain = createWebTrace("https://example.com/b", "import", { file });
    await plain.flush();
    const [first, second] = await rows(file);
    expect(first.shadowRoots).toBe(24);
    expect(second).not.toHaveProperty("shadowRoots");
  });

  it("沉降等待结果（结束原因与首段用时）有值才写入", async () => {
    const file = path.join(dir, "trace.jsonl");
    const settled = createWebTrace("https://example.com/a", "import", { file });
    settled.set({ settle: { reason: "quiet", initialMs: 300 } });
    await settled.flush();
    const plain = createWebTrace("https://example.com/b", "import", { file });
    await plain.flush();
    const [first, second] = await rows(file);
    expect(first.settle).toEqual({ reason: "quiet", initialMs: 300 });
    expect(second).not.toHaveProperty("settle");
  });

  it("没有失败时失败明细为空数组", async () => {
    const file = path.join(dir, "trace.jsonl");
    const trace = createWebTrace("https://example.com/", "import", { file });
    trace.egress(sample());
    await trace.flush();
    const [{ egress }] = await rows(file);
    expect(egress.failures).toEqual([]);
  });

  it("按 Accept 粗分资源类型：文档、样式、图片，其余归 other", () => {
    expect(classifyAccept("text/html,application/xhtml+xml,*/*;q=0.8")).toBe("document");
    expect(classifyAccept("  Text/HTML,application/xhtml+xml")).toBe("document");
    // Chromium 导航（含 <link rel=prefetch>）的完整 Accept。
    expect(
      classifyAccept(
        "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
      ),
    ).toBe("document");
    // 库发的 XHR/fetch 即使要 HTML，也是数据请求，不是文档。
    expect(classifyAccept("text/html, */*; q=0.01")).toBe("other");
    expect(classifyAccept("text/html")).toBe("other");
    expect(classifyAccept("text/css,*/*;q=0.1")).toBe("style");
    expect(classifyAccept("image/avif,image/webp,image/apng,*/*;q=0.8")).toBe("image");
    expect(classifyAccept("*/*")).toBe("other");
    expect(classifyAccept("application/json")).toBe("other");
    expect(classifyAccept("")).toBe("other");
    expect(classifyAccept(null)).toBe("other");
    expect(classifyAccept(undefined)).toBe("other");
  });

  it("flush 幂等，重复调用只写一行", async () => {
    const file = path.join(dir, "trace.jsonl");
    const trace = createWebTrace("https://example.com/", "import", { file });
    await trace.flush();
    await trace.flush();
    expect(await rows(file)).toHaveLength(1);
  });

  it("无法解析的网址仍写入一行，origin 标记为 invalid", async () => {
    const file = path.join(dir, "trace.jsonl");
    const trace = createWebTrace("not a url", "import", { file });
    await trace.flush();
    const [row] = await rows(file);
    expect(row.origin).toBe("invalid");
  });

  it("写入失败不抛出，同一文件的同一错误只记一次日志", async () => {
    const file = path.join(dir, "missing-dir", "trace.jsonl");
    for (let i = 0; i < 3; i++) {
      const trace = createWebTrace("https://example.com/", "import", { file });
      await expect(trace.flush()).resolves.toBeUndefined();
    }
    expect(mocks.log).toHaveBeenCalledTimes(1);
    expect(mocks.log).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: "main",
        action: "网页采集追踪",
        message: expect.stringContaining("ENOENT"),
      }),
    );
  });

  it("相对路径被忽略并提示一次", async () => {
    process.env[WEB_TRACE_ENV] = "relative/trace.jsonl";
    expect(createWebTrace("https://example.com/", "import")).toBe(NOOP_WEB_TRACE);
    expect(createWebTrace("https://example.com/", "import")).toBe(NOOP_WEB_TRACE);
    expect(mocks.log).toHaveBeenCalledTimes(1);
  });
});
