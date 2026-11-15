/**
 * aiflow golden 对拍的共用件（非测试文件）。
 *
 * apps/desktop-go/aiflow 是渲染层 AI 编排（services/ai*.ts、services/knowledge-ai）的 Go 无头移植，
 * 要求请求体、提示词和错误文案与 TS 逐字节一致。做法：本目录的 aiflow-golden-*.test.ts 用真实的 TS 实现跑一组
 * 脚本化场景，把「发出的请求 + 得到的结果」写成 JSON；Go 侧 aiflow/*_test.go 用同样的脚本重放并逐项核对。
 *
 *   - 平时（CI）：TS 当前行为必须等于已提交的 golden，否则说明 TS 变了而 Go 没跟上（或反过来）；
 *   - 有意改了行为：UPDATE_GOLDEN=1 pnpm --filter @guizhi/desktop exec vitest run tests/unit/knowledge-ai/aiflow-golden-*.test.ts
 *     重新导出，同时更新 Go 侧移植，一起提交。
 *
 * 比较时去掉不确定的部分：requestId、耗时。其余（URL、请求头、请求体原文、回调序列、错误文案）全部逐项相等。
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, vi } from "vitest";

const GOLDEN_DIR = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../desktop-go/aiflow/testdata/golden",
);

/** 把 actual 写成/比对 aiflow/testdata/golden/<name>.json。 */
export function writeOrCompareGolden(name: string, actual: unknown): void {
  const file = path.join(GOLDEN_DIR, `${name}.json`);
  const text = `${JSON.stringify(actual, null, 2)}\n`;
  if (process.env.UPDATE_GOLDEN === "1") {
    fs.mkdirSync(GOLDEN_DIR, { recursive: true });
    fs.writeFileSync(file, text);
    return;
  }
  const committed = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : "";
  expect(
    JSON.parse(committed || "null"),
    `aiflow golden「${name}」与 TS 当前行为不一致：确认这是有意的改动后，用 UPDATE_GOLDEN=1 重新导出并提交，同时更新 Go 侧移植`,
  ).toEqual(JSON.parse(text));
}

/** 传输层脚本的一步：描述假传输这一次调用怎么响应。 */
export interface ScriptStep {
  /** HTTP 状态，默认 200 */
  status?: number;
  /** 默认按 status 是否 2xx 推断 */
  ok?: boolean;
  statusText?: string;
  body?: string;
  headers?: Record<string, string>;
  /** 传输层失败的原因（超时、被拒绝等），与代理返回的 error 字段一致 */
  error?: string;
  /** 流式：先逐块回调这些文本再返回 */
  chunks?: string[];
  /** 流式：读流阶段的失败，经 onError 回报 */
  streamError?: string;
  /** 直接让这次调用 reject（模拟传输层抛错） */
  throws?: string;
  /** 这次调用进行中触发取消（signal.abort），再按代理的做法返回一个 error 响应 */
  abortDuring?: boolean;
}

export interface RecordedRequest {
  kind: "request" | "stream";
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: string;
  timeoutMs?: number;
}

export interface UsageRecord {
  scenario: string;
  model: string;
  promptTokens: number;
  completionTokens: number;
  failed?: boolean;
}

export interface FakeTransport {
  requests: RecordedRequest[];
  usage: UsageRecord[];
  /** 把取消控制器接进来：abortDuring 与 "before" 都靠它 */
  controller: AbortController;
}

interface AiBridge {
  createRequestId: () => string;
  cancel: (id: string) => void;
  request: (request: Record<string, unknown>) => Promise<unknown>;
  requestStream: (
    request: Record<string, unknown>,
    handlers: { onChunk: (chunk: string) => void; onError: (message: string) => void },
  ) => Promise<unknown>;
  recordUsage: (entry: UsageRecord) => Promise<void>;
}

function response(step: ScriptStep): Record<string, unknown> {
  const status = step.status ?? 200;
  const out: Record<string, unknown> = {
    ok: step.ok ?? (status >= 200 && status < 300),
    status,
    statusText: step.statusText ?? "",
    body: step.body ?? "",
    headers: step.headers ?? {},
  };
  if (step.error !== undefined) out.error = step.error;
  return out;
}

/**
 * 对齐生产环境的 DOMException：Chromium 里 DOMException 继承 Error（`new DOMException() instanceof Error`），
 * ai.ts 的 catch 据此原样重抛 AbortError；jsdom 的实现不继承，会让取消被误包装成「网络请求失败」，
 * 导出的 golden 就不是生产行为了。
 */
export function alignDomExceptionWithChromium(): void {
  if (!(new DOMException("probe", "AbortError") instanceof Error)) {
    Object.setPrototypeOf(DOMException.prototype, Error.prototype);
  }
}

/** 安装假的 window.api.ai；返回记录器。每个场景调用一次，场景之间互不影响。 */
export function installFakeTransport(script: ScriptStep[]): FakeTransport {
  alignDomExceptionWithChromium();
  const steps = [...script];
  const fake: FakeTransport = {
    requests: [],
    usage: [],
    controller: new AbortController(),
  };
  let counter = 0;

  const take = (): ScriptStep => {
    const step = steps.shift();
    if (!step) throw new Error("场景脚本已用尽：请求次数比预期多");
    return step;
  };
  const record = (kind: RecordedRequest["kind"], request: Record<string, unknown>): void => {
    const entry: RecordedRequest = {
      kind,
      method: String(request.method),
      url: String(request.url),
      headers: (request.headers ?? {}) as Record<string, string>,
    };
    if (request.body !== undefined) entry.body = String(request.body);
    if (request.timeoutMs !== undefined) entry.timeoutMs = Number(request.timeoutMs);
    fake.requests.push(entry);
  };

  const bridge: AiBridge = {
    createRequestId: () => `req-${++counter}`,
    cancel: () => undefined,
    request: async (request) => {
      record("request", request);
      const step = take();
      if (step.throws !== undefined) throw new Error(step.throws);
      if (step.abortDuring) fake.controller.abort();
      return response(step);
    },
    requestStream: async (request, handlers) => {
      record("stream", request);
      const step = take();
      if (step.throws !== undefined) throw new Error(step.throws);
      for (const chunk of step.chunks ?? []) handlers.onChunk(chunk);
      if (step.streamError !== undefined) handlers.onError(step.streamError);
      if (step.abortDuring) fake.controller.abort();
      return response(step);
    },
    recordUsage: async (entry) => {
      const normalized: UsageRecord = {
        scenario: entry.scenario,
        model: entry.model,
        promptTokens: entry.promptTokens,
        completionTokens: entry.completionTokens,
      };
      if (entry.failed !== undefined) normalized.failed = entry.failed;
      fake.usage.push(normalized);
    },
  };

  const target = globalThis as unknown as { window: { api?: Record<string, unknown> } };
  target.window.api = { ...(target.window.api ?? {}), ai: bridge };
  return fake;
}

/** 把 console.warn 收集起来（日志文案也是对拍的一部分，如截断提示）。返回读取函数与还原函数。 */
export function captureWarnings(): { messages: string[]; restore: () => void } {
  const messages: string[] = [];
  const spy = vi.spyOn(console, "warn").mockImplementation((...args: unknown[]) => {
    messages.push(args.map(String).join(" "));
  });
  return { messages, restore: () => spy.mockRestore() };
}

/** 错误对象 → 对拍用的 { message, aborted }。 */
export function describeError(error: unknown): { message: string; aborted: boolean } {
  if (error instanceof Error) {
    return { message: error.message, aborted: error.name === "AbortError" };
  }
  return { message: String(error), aborted: false };
}
