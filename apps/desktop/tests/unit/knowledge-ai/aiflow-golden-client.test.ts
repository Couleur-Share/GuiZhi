/**
 * aiflow 对话客户端栈的 golden：services/ai.ts 的 chatCompletion / testAIConnection，
 * 连同 ai-stream、ai-errors 与 shared/utils/ai-protocol。Go 侧重放见 apps/desktop-go/aiflow/client_golden_test.go。
 * 约定与导出方式见 aiflow-golden-kit.ts。
 *
 * 有意不进 golden 的 TS 行为（Go 有各自的测试，差异记录在 docs/agents/native-ui-development.md）：
 *   - Anthropic 路径不挂取消（TS 的 signal 过不去），Go 统一尊重取消；
 *   - 响应体不是合法 JSON 时 JSON.parse 的报错原文；
 *   - topP/topK/惩罚项/responseFormat/streamCallbacks：没有调用方，不移植（下方有守卫）。
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  buildChatEndpointFromBase,
  buildEmbeddingsEndpointFromBase,
  buildHeadersForProtocol,
  buildModelsEndpointFromBase,
  extractTextFromChatResponse,
  extractUsageFromChatResponse,
  getBaseUrl,
  normalizeApiUrlInput,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import { chatCompletion, testAIConnection } from "../../../src/renderer/services/ai";
import type { AIConfig, ChatMessage } from "../../../src/renderer/services/ai";
import { getFormattedErrorMessageFromResponse } from "../../../src/renderer/services/ai-errors";
import { createResponseLike } from "../../../src/renderer/services/ai-transport";
import {
  captureWarnings,
  describeError,
  installFakeTransport,
  writeOrCompareGolden,
  type ScriptStep,
} from "./aiflow-golden-kit";

type Options = {
  temperature?: number;
  maxTokens?: number;
  stream?: boolean;
  enableThinking?: boolean;
  timeoutMs?: number;
  allowEmptyContent?: boolean;
};

interface ChatCase {
  name: string;
  config: AIConfig;
  messages: ChatMessage[];
  options?: Options;
  script: ScriptStep[];
  abort?: "before" | "during";
  /** 依次调用 chatCompletion 的次数，默认 1；一次调用可能消耗多步脚本（重试场景） */
  calls?: number;
}

const config = (over: Partial<AIConfig> = {}): AIConfig => ({
  id: "m1",
  provider: "custom",
  apiProtocol: "openai",
  apiKey: "sk-test-0123456789",
  apiUrl: "https://api.example.com/v1",
  model: "test-model",
  ...over,
});

const OPENAI = config({ id: "m-openai", provider: "openai", apiUrl: "https://api.openai.com/v1", model: "gpt-4o-mini" });
const DEEPSEEK = config({ id: "m-ds", provider: "deepseek", apiUrl: "https://api.deepseek.com", model: "deepseek-chat" });
const QWEN = config({ id: "m-qwen", provider: "qwen", apiUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" });
const GEMINI = config({
  id: "m-gemini",
  provider: "google",
  apiProtocol: "gemini",
  apiUrl: "https://generativelanguage.googleapis.com",
  model: "models/gemini-2.5-flash",
});
const ANTHROPIC = config({
  id: "m-claude",
  provider: "anthropic",
  apiProtocol: "anthropic",
  apiUrl: "https://api.anthropic.com",
  model: "claude-sonnet-4-5",
});

const MESSAGES: ChatMessage[] = [
  { role: "system", content: "你是助手。\n请用中文回答。" },
  { role: "user", content: "总结：<b>天气</b> & \"引号\" \u2028 末尾" },
];

const completion = (
  content: unknown,
  extra: { finish_reason?: string | null; reasoning_content?: string; usage?: unknown } = {},
): string =>
  JSON.stringify({
    id: "chatcmpl-1",
    choices: [
      {
        index: 0,
        message: {
          role: "assistant",
          content,
          ...(extra.reasoning_content !== undefined ? { reasoning_content: extra.reasoning_content } : {}),
        },
        finish_reason: extra.finish_reason === undefined ? "stop" : extra.finish_reason,
      },
    ],
    ...(extra.usage !== undefined ? { usage: extra.usage } : {}),
  });

const sse = (...events: Array<object | string>): string =>
  `${events.map((e) => (typeof e === "string" ? e : `data: ${JSON.stringify(e)}`)).join("\n")}\n`;
const delta = (d: Record<string, unknown>, finish?: string): object => ({
  choices: [{ index: 0, delta: d, ...(finish ? { finish_reason: finish } : {}) }],
});

const json = (status: number, payload: unknown, extra: Partial<ScriptStep> = {}): ScriptStep => ({
  status,
  body: typeof payload === "string" ? payload : JSON.stringify(payload),
  ...extra,
});

const ok = (body: string): ScriptStep => ({ status: 200, body });

const LONG_TEXT = `${"错".repeat(150)}😀${"误".repeat(60)}`;

const CHAT_CASES: ChatCase[] = [
  {
    name: "openai 非流式：max_completion_tokens、转义字符与超时透传",
    config: OPENAI,
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 512, timeoutMs: 120_000 },
    script: [ok(completion("你好", { usage: { prompt_tokens: 12, completion_tokens: 3 } }))],
  },
  {
    name: "temperature 为 0 也要原样带上（?? 而不是 ||）",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { temperature: 0 },
    script: [ok(completion("ok"))],
  },
  {
    name: "deepseek 缺 /v1 自动补全，默认 0.7 / 2048，内容块数组取 text 拼接",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [
      ok(completion([{ type: "text", text: "甲" }, { type: "image_url" }, { type: "text", text: "乙" }, { type: "text" }])),
    ],
  },
  {
    name: "非流式：reasoning_content 与 usage",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [ok(completion("答案", { reasoning_content: "先想一想", usage: { prompt_tokens: 12, completion_tokens: 34 } }))],
  },
  {
    name: "usage 是空对象时按 0 计，usage 为 null 时不返回",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [ok(completion("a", { usage: {} })), ok(completion("b", { usage: null }))],
  },
  {
    name: "gemini：去掉 models/ 前缀并走兼容层端点",
    config: GEMINI,
    messages: MESSAGES,
    options: { maxTokens: 100 },
    script: [ok(completion("你好"))],
  },
  {
    name: "anthropic：system 提到顶层，assistant 保留，其余按 user",
    config: ANTHROPIC,
    messages: [
      { role: "system", content: "系统提示" },
      { role: "system", content: "第二条 system 被忽略" },
      { role: "user", content: "问题一" },
      { role: "assistant", content: "回答一" },
      { role: "user", content: "问题二" },
    ],
    options: { maxTokens: 300, stream: true, timeoutMs: 90_000 },
    script: [
      ok(JSON.stringify({ content: [{ type: "text", text: "你" }, { type: "thinking", thinking: "x" }, { type: "text", text: "好" }], stop_reason: "end_turn" })),
    ],
  },
  {
    name: "anthropic：max_tokens 截断归一成 length，无 system 时不带该字段",
    config: ANTHROPIC,
    messages: [{ role: "user", content: "写长文" }],
    script: [ok(JSON.stringify({ content: [{ type: "text", text: "半截" }], stop_reason: "max_tokens", usage: { input_tokens: 5, output_tokens: 9 } }))],
  },
  {
    name: "anthropic：空正文按失败处理",
    config: ANTHROPIC,
    messages: [{ role: "user", content: "x" }],
    script: [ok(JSON.stringify({ content: [], stop_reason: "end_turn" }))],
  },
  {
    name: "anthropic：结构化错误带 type 前缀",
    config: ANTHROPIC,
    messages: [{ role: "user", content: "x" }],
    script: [json(400, { type: "error", error: { type: "invalid_request_error", message: "max_tokens: required" } })],
  },
  {
    name: "qwen 非流式：enable_thinking 强制 false",
    config: QWEN,
    messages: [{ role: "user", content: "hi" }],
    options: { enableThinking: true },
    script: [ok(completion("ok"))],
  },
  {
    name: "qwen 流式：enable_thinking 可以为 true",
    config: QWEN,
    messages: [{ role: "user", content: "hi" }],
    options: { enableThinking: true, stream: true },
    script: [{ chunks: [sse(delta({ content: "好" }), delta({ content: "的" }, "stop"))] }],
  },
  {
    name: "其他模型 enableThinking 直接带 true（deepseek 类）",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { enableThinking: true },
    script: [ok(completion("ok"))],
  },
  {
    name: "流式：半条事件跨块、CRLF、reasoning、usage 收尾事件与 [DONE]",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true, timeoutMs: 120_000 },
    script: [
      {
        chunks: [
          'data: {"choices":[{"index":0,"delta":{"reasoning_content":"想"}}]}\r\ndata: {"choices":[{"inde',
          'x":0,"delta":{"content":"你"}}]}\r\n\r\n',
          sse(delta({}), delta({ content: "好" }), delta({ content: "", reasoning_content: "" })),
          sse({ choices: [{ index: 0, delta: { content: "！" }, finish_reason: "stop" }] }),
          sse({ choices: [], usage: { prompt_tokens: "7", completion_tokens: 2 } }, "data: [DONE]"),
        ],
      },
    ],
  },
  {
    name: "流式：非 data: 行、无空格的 data:、坏 JSON 全部忽略，末行无换行靠 flush 收尾",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true },
    script: [
      {
        chunks: [
          ": keep-alive\n",
          "event: message\n",
          'data:{"choices":[{"delta":{"content":"忽略"}}]}\n',
          "data: {not json}\n",
          "data: 123\n",
          'data: {"choices":[{"delta":{"content":"保留"}}]}\n',
          'data: {"choices":[{"delta":{"content":"尾巴"},"finish_reason":"stop"}]}',
        ],
      },
    ],
  },
  {
    name: "流式：finish_reason 为 length 与 usage 字符串被数字化",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true, maxTokens: 50 },
    script: [
      {
        chunks: [
          sse(delta({ content: "被截" }), delta({}, "length"), { choices: [{ delta: {}, finish_reason: "" }], usage: { prompt_tokens: "abc", completion_tokens: null } }),
        ],
      },
    ],
  },
  {
    name: "流式：读流失败经 onError 抛出",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true },
    script: [{ chunks: [sse(delta({ content: "半" }))], streamError: "流读取中断" }],
  },
  {
    name: "流式：HTTP 错误响应走统一的错误文案",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true },
    script: [json(401, { error: { message: "Incorrect API key provided", type: "invalid_request_error", code: "invalid_api_key" } })],
  },
  {
    name: "流式：没有正文（只有思考）按失败处理，原因恒为 unknown",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true, maxTokens: 64 },
    script: [{ chunks: [sse(delta({ reasoning_content: "一直在想" }), delta({}, "length"))] }],
  },
  {
    name: "token 参数重试：openai 系先 max_completion_tokens，被拒后换成 max_tokens（键移到末尾）",
    config: OPENAI,
    messages: [{ role: "user", content: "hi" }],
    options: { maxTokens: 256, enableThinking: true },
    script: [
      json(400, { error: { message: "Unsupported parameter: 'max_completion_tokens' is not supported with this model. Use 'max_tokens' instead.", type: "invalid_request_error" } }),
      ok(completion("ok")),
    ],
  },
  {
    name: "token 参数重试：自定义模型先 max_tokens，被拒后换成 max_completion_tokens",
    config: config({ model: "my-reasoner" }),
    messages: [{ role: "user", content: "hi" }],
    options: { maxTokens: 256 },
    script: [
      json(400, { error: { message: "Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.", type: "invalid_request_error", code: "unsupported_parameter" } }),
      ok(completion("ok")),
    ],
  },
  {
    name: "token 参数重试：重试后仍失败，报第二次的错误",
    config: config({ model: "my-reasoner" }),
    messages: [{ role: "user", content: "hi" }],
    script: [
      json(400, { error: { message: "'max_tokens' is not supported" } }),
      json(429, { error: { message: "Rate limit reached", type: "rate_limit_error" } }),
    ],
  },
  {
    name: "token 参数重试：流式请求重试后仍是流式",
    config: config({ model: "my-reasoner" }),
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true },
    script: [
      json(400, { error: { message: "Use 'max_completion_tokens' instead" } }),
      { chunks: [sse(delta({ content: "成功" }, "stop"))] },
    ],
  },
  {
    name: "enable_thinking 重试：qwen 非流式被拒后强制 false 再发",
    config: QWEN,
    messages: [{ role: "user", content: "hi" }],
    options: { enableThinking: true },
    script: [
      json(400, { error: { message: "parameter.enable_thinking must be set to false for non-streaming calls", code: "invalid_parameter_error" } }),
      ok(completion("ok")),
    ],
  },
  {
    name: "enable_thinking 重试：非 qwen 模型的 true 被改成 false，位置不变",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { enableThinking: true },
    script: [
      json(400, { error: { message: "enable_thinking only support stream mode" } }),
      ok(completion("ok")),
    ],
  },
  {
    name: "enable_thinking 重试：不带该键的请求补到末尾",
    config: OPENAI,
    messages: [{ role: "user", content: "hi" }],
    script: [json(400, { error: { message: "enable_thinking must be set to false" } }), ok(completion("ok"))],
  },
  {
    name: "错误文案：带 code 的结构化错误",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [json(401, { error: { message: "Incorrect API key provided", type: "invalid_request_error", code: "invalid_api_key" } })],
  },
  {
    name: "错误文案：type 与 message 相同时不加前缀；只有 status（Gemini 风格）",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [json(400, { error: { type: "overloaded" } }), json(429, { error: { status: "RESOURCE_EXHAUSTED", code: 429 } })],
  },
  {
    name: "错误文案：顶层 message、detail 与字符串 error",
    calls: 3,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [json(500, { message: "boom" }), json(404, { detail: "Not Found" }), json(500, { error: "plain string error" })],
  },
  {
    name: "错误文案：HTML 网关页取 title，没有 title 回落到状态文案",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [
      {
        status: 502,
        headers: { "Content-Type": "text/html; charset=utf-8" },
        body: "<!DOCTYPE html><html><head><title>\n  502   Bad\tGateway </title></head><body>x</body></html>",
      },
      { status: 503, body: "  <HTML><body>no title</body></HTML>" },
    ],
  },
  {
    name: "错误文案：504 网关超时",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [{ status: 504, body: "gateway time-out" }],
  },
  {
    name: "错误文案：纯文本截到 200 个 UTF-16 单元，空体回落",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [{ status: 500, body: LONG_TEXT }, { status: 500, body: "" }],
  },
  {
    name: "错误文案：传输层失败（status 0 + error）原样返回，空 error 回落到状态文案",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [{ status: 0, ok: false, error: "AI 请求超时（120000ms）" }, { status: 0, ok: false, error: "" }],
  },
  {
    name: "响应：choices 为空或缺失",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [ok(JSON.stringify({ choices: [] })), ok(JSON.stringify({ id: "x" }))],
  },
  {
    name: "响应：正文为 null / 纯空白 / 缺 finish_reason 都按空正文失败",
    calls: 3,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [
      ok(completion(null, { finish_reason: "stop" })),
      ok(completion("  \n\t ", { finish_reason: "stop" })),
      ok(completion("", { finish_reason: null })),
    ],
  },
  {
    name: "响应：输出被截断没有正文——区分思考耗尽与普通截断",
    calls: 2,
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { maxTokens: 1000 },
    script: [
      ok(completion("", { finish_reason: "length", reasoning_content: "想了很久" })),
      ok(completion("", { finish_reason: "length" })),
    ],
  },
  {
    name: "响应：allowEmptyContent 放行空正文",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { allowEmptyContent: true },
    script: [ok(completion("", { finish_reason: "length", reasoning_content: "想" }))],
  },
  {
    name: "校验：缺 key / url / model 不发请求",
    config: config({ apiKey: "" }),
    messages: [{ role: "user", content: "hi" }],
    script: [],
  },
  {
    name: "校验：缺 url",
    config: config({ apiUrl: "" }),
    messages: [{ role: "user", content: "hi" }],
    script: [],
  },
  {
    name: "校验：缺 model",
    config: config({ model: "" }),
    messages: [{ role: "user", content: "hi" }],
    script: [],
  },
  {
    name: "取消：请求前已取消",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    abort: "before",
    script: [],
  },
  {
    name: "取消：非流式请求进行中取消",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    abort: "during",
    script: [{ abortDuring: true, status: 0, ok: false, error: "已取消" }],
  },
  {
    name: "取消：流式请求进行中取消",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    options: { stream: true },
    abort: "during",
    script: [{ abortDuring: true, chunks: [sse(delta({ content: "半" }))], status: 0, ok: false, error: "已取消" }],
  },
  {
    name: "传输层抛错：message 原样透出",
    config: DEEPSEEK,
    messages: [{ role: "user", content: "hi" }],
    script: [{ throws: "socket hang up" }],
  },
];

function normalizeResult(result: Awaited<ReturnType<typeof chatCompletion>>): Record<string, unknown> {
  const out: Record<string, unknown> = { content: result.content };
  if (result.thinkingContent) out.thinkingContent = result.thinkingContent;
  if (result.finishReason !== undefined) out.finishReason = result.finishReason;
  if (result.usage) out.usage = result.usage;
  return out;
}

async function runChatCase(c: ChatCase): Promise<Record<string, unknown>> {
  const fake = installFakeTransport(c.script);
  const warnings = captureWarnings();
  if (c.abort === "before") fake.controller.abort();
  const streamed: string[] = [];
  const results: Array<Record<string, unknown> | null> = [];
  const errors: Array<{ message: string; aborted: boolean } | null> = [];
  try {
    for (let round = 0; round < (c.calls ?? 1); round++) {
      try {
        const result = await chatCompletion(c.config, c.messages, {
          ...c.options,
          onStream: c.options?.stream ? (chunk: string) => streamed.push(chunk) : undefined,
          signal: fake.controller.signal,
        });
        results.push(normalizeResult(result));
        errors.push(null);
      } catch (error) {
        results.push(null);
        errors.push(describeError(error));
      }
    }
  } finally {
    warnings.restore();
  }
  return {
    requests: fake.requests,
    streamed,
    warnings: warnings.messages,
    results,
    errors,
  };
}

describe("aiflow golden：对话客户端栈", () => {
  it("chatCompletion 的请求与结果与已提交的 golden 一致", async () => {
    const cases = [];
    for (const c of CHAT_CASES) {
      cases.push({
        name: c.name,
        config: c.config,
        messages: c.messages,
        options: c.options ?? {},
        script: c.script,
        abort: c.abort ?? null,
        calls: c.calls ?? 1,
        expect: await runChatCase(c),
      });
    }

    const connection = [];
    const probes: Array<{ name: string; config: AIConfig; prompt?: string; script: ScriptStep[] }> = [
      { name: "成功", config: OPENAI, script: [ok(completion("OK"))] },
      { name: "成功：思考类模型探针正文为空也算连通", config: DEEPSEEK, script: [ok(completion("", { finish_reason: "length", reasoning_content: "想" }))] },
      { name: "自定义提示词", config: DEEPSEEK, prompt: "ping", script: [ok(completion("pong"))] },
      { name: "失败：401", config: DEEPSEEK, script: [json(401, { error: { message: "bad key", code: "invalid_api_key" } })] },
      { name: "失败：未配置 key", config: config({ apiKey: "" }), script: [] },
      { name: "失败：传输层抛错", config: DEEPSEEK, script: [{ throws: "ECONNRESET" }] },
    ];
    for (const probe of probes) {
      const fake = installFakeTransport(probe.script);
      const result = await testAIConnection(probe.config, probe.prompt);
      connection.push({
        name: probe.name,
        config: probe.config,
        prompt: probe.prompt ?? null,
        script: probe.script,
        expect: {
          requests: fake.requests,
          result: {
            id: result.id,
            success: result.success,
            response: result.response,
            thinkingContent: result.thinkingContent || undefined,
            error: result.error,
            model: result.model,
            provider: result.provider,
          },
        },
      });
    }

    writeOrCompareGolden("client", { chat: cases, testConnection: connection });
  });

  it("没有调用方的选项不移植：一旦渲染层开始使用，这里失败提醒补 Go 版", () => {
    const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../src/renderer");
    const pattern = /\b(topP|topK|frequencyPenalty|presencePenalty|responseFormat|streamCallbacks)\b/;
    // 定义与实现这些选项的文件本身不算调用方
    const owners = new Set(["services/ai.ts", "services/ai-types.ts", "services/ai-stream.ts"]);
    const callers: string[] = [];
    for (const rel of fs.readdirSync(root, { recursive: true }) as string[]) {
      const normalized = rel.split(path.sep).join("/");
      if (!/\.(ts|tsx)$/.test(normalized) || owners.has(normalized)) continue;
      const lines = fs.readFileSync(path.join(root, rel), "utf8").split("\n");
      lines.forEach((line, i) => {
        if (pattern.test(line)) callers.push(`${normalized}:${i + 1}: ${line.trim()}`);
      });
    }
    expect(callers, "渲染层出现了 aiflow 未移植的 chat 选项的调用方：补 Go 版（chat.go）并更新 golden").toEqual([]);
  });
});

describe("aiflow golden：协议与端点", () => {
  it("协议判定、端点拼接、请求头、文本与用量提取与已提交的 golden 一致", () => {
    const protocolConfigs = [
      { apiProtocol: "openai", provider: "anthropic", apiUrl: "" },
      { apiProtocol: "anthropic", provider: "openai", apiUrl: "" },
      { apiProtocol: "gemini" },
      { provider: "Anthropic" },
      { provider: "x", apiUrl: "https://API.ANTHROPIC.com/v1" },
      { provider: "Google" },
      { provider: "GEMINI" },
      { provider: "x", apiUrl: "https://generativelanguage.googleapis.com/v1beta" },
      { provider: "deepseek", apiUrl: "https://api.deepseek.com" },
      {},
      { apiProtocol: "bogus", provider: "gemini" },
    ];
    const urls = [
      "", "  ", "https://api.openai.com", "https://api.openai.com/", "https://api.openai.com/v1", "https://api.openai.com/v1/",
      "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions/", "https://x.com/v1/completions",
      "https://x.com/v1/models", "https://x.com/v1/embeddings", "https://x.com/v1/images/generations", "  https://x.com/v1  ",
      "https://x.com/custom#", "https://x.com/custom/#", "https://x.com/v1/chat/completions#", "#", "  #  ",
      "https://generativelanguage.googleapis.com", "https://generativelanguage.googleapis.com/v1beta",
      "https://generativelanguage.googleapis.com/v1beta/openai", "https://generativelanguage.googleapis.com/v1/",
      "https://api.anthropic.com", "https://api.anthropic.com/v1", "https://api.anthropic.com/v1/messages", "https://gw.example.com/anthropic/messages",
      "https://x.com/v2", "https://x.com/v10/", "https://x.com/v1beta", "https://x.com/openai",
    ];
    const protocols = ["openai", "gemini", "anthropic"] as const;
    const endpoints = urls.flatMap((url) =>
      protocols.map((protocol) => {
        const resolved = resolveProtocolBase(url, protocol);
        return {
          url,
          protocol,
          resolved,
          chat: buildChatEndpointFromBase(resolved),
          embeddings: buildEmbeddingsEndpointFromBase(resolved),
          models: buildModelsEndpointFromBase(resolved),
        };
      }),
    );
    const usagePayloads: unknown[] = [
      null, 5, "x", [], {}, { usage: null }, { usage: 0 }, { usage: {} },
      { usage: { prompt_tokens: 10, completion_tokens: 20 } },
      { usage: { prompt_tokens: "10", completion_tokens: "x" } },
      { usage: { prompt_tokens: null } },
      { usage: { completion_tokens: 7 } },
      { usage: { input_tokens: 3, output_tokens: 4, prompt_tokens: 100 } },
      { usageMetadata: { promptTokenCount: 8, candidatesTokenCount: 9 } },
      { usage: null, usageMetadata: { promptTokenCount: 8 } },
      { usage: { promptTokenCount: 1, candidatesTokenCount: 2 } },
      { usage: [1, 2] },
    ];
    const textPayloads: unknown[] = [
      null, "x", [], {}, { choices: [] }, { choices: "x" }, { choices: [{}] },
      { choices: [{ message: { content: "你好" } }] },
      { choices: [{ message: { content: [{ type: "text", text: "a" }, { text: null }, { text: 5 }, "s", null, { x: 1 }, { text: { o: 1 } }] } }] },
      { choices: [{ message: { content: 5 } }] },
      { choices: [null] },
      { content: [{ type: "text", text: "claude" }, { type: "tool_use" }] },
      { content: "plain" },
      { content: 5 },
    ];
    writeOrCompareGolden("protocol", {
      resolveProtocol: protocolConfigs.map((c) => ({ config: c, expect: resolveAIProtocol(c as never) })),
      baseUrls: urls.map((url) => ({ url, getBaseUrl: getBaseUrl(url), normalizeApiUrlInput: normalizeApiUrlInput(url) })),
      endpoints,
      headers: protocols.flatMap((protocol) =>
        [undefined, { accept: "text/event-stream" }, { contentType: false }, { useNativeGeminiAuth: true, accept: "application/json" }].map((options) => ({
          protocol,
          options: options ?? null,
          expect: buildHeadersForProtocol(protocol, "sk-abc", options),
        })),
      ),
      usage: usagePayloads.flatMap((payload) =>
        ["openai", "anthropic", "gemini"].map((protocol) => ({
          payload: payload === undefined ? null : payload,
          protocol,
          expect: extractUsageFromChatResponse(payload, protocol as never) ?? null,
        })),
      ),
      text: textPayloads.flatMap((payload) =>
        ["openai", "anthropic"].map((protocol) => ({
          payload,
          protocol,
          expect: extractTextFromChatResponse(payload, protocol as never),
        })),
      ),
    });
  });
});

describe("aiflow golden：错误文案", () => {
  it("getFormattedErrorMessageFromResponse 与已提交的 golden 一致", async () => {
    interface ErrorCase {
      name: string;
      response: { status: number; body: string; headers?: Record<string, string>; error?: string };
      options?: { operation?: string; fallback?: string; maxTextLength?: number };
    }
    const cases: ErrorCase[] = [
      { name: "空体", response: { status: 500, body: "" } },
      { name: "自定义 fallback 与 operation", response: { status: 504, body: "x" }, options: { operation: "Embeddings 请求", fallback: "失败了" } },
      { name: "html 取 title", response: { status: 502, body: "<html><TITLE lang='x'>Bad  <b>gw</b></TITLE></html>" }, options: { fallback: "上游错误" } },
      { name: "content-type 声明 html", response: { status: 500, body: "oops", headers: { "CONTENT-TYPE": "Text/HTML" } } },
      { name: "html 没有 title", response: { status: 500, body: "<!doctype html><p>hi</p>" } },
      { name: "文本截断（自定义长度）", response: { status: 500, body: "0123456789" }, options: { maxTextLength: 4 } },
      { name: "结构化：error 是数组", response: { status: 400, body: JSON.stringify({ error: ["a"] }) } },
      { name: "结构化：message 是对象", response: { status: 400, body: JSON.stringify({ error: { message: { a: 1 } } }) } },
      { name: "结构化：数字 code", response: { status: 400, body: JSON.stringify({ error: { message: "m", code: 42 } }) } },
      { name: "结构化：code 为 0 视为没有", response: { status: 400, body: JSON.stringify({ error: { message: "m", code: 0, type: "t" } }) } },
      { name: "结构化：JSON null", response: { status: 400, body: "null" } },
      { name: "结构化：JSON 字符串", response: { status: 400, body: JSON.stringify("just text") } },
      { name: "结构化：error 为空串回落到 message", response: { status: 400, body: JSON.stringify({ error: "", message: "top" }) } },
      { name: "error 字段优先于 body", response: { status: 0, body: "ignored", error: "代理拒绝：目标地址不允许" } },
      { name: "error 为空串也算有值", response: { status: 502, body: "上游错误正文", error: "" } },
    ];
    const expects = [];
    for (const c of cases) {
      const response = createResponseLike({
        ok: false,
        status: c.response.status,
        statusText: "",
        body: c.response.body,
        headers: c.response.headers ?? {},
        ...(c.response.error !== undefined ? { error: c.response.error } : {}),
      } as never);
      // message 是对象时 TS 会把对象原样返回，调用方 new Error(它) 后用户看到的是 String(它)，golden 记这个
      expects.push({ ...c, expect: String(await getFormattedErrorMessageFromResponse(response, c.options)) });
    }
    writeOrCompareGolden("errors", { cases: expects });
  });
});
