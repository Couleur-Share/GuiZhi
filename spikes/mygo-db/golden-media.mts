// 媒体增强（转写 / 排版 / 总结）与主进程对话客户端的差分基线：
// 纯函数与文本处理逐项由真实 TS 产出；对话客户端与转写请求对本地 http 服务重放，
// 记录 TS 实际发出的请求（头、原始体）与结果，Go 侧对同样的场景逐项比对。不访问真实网络、不用真实密钥。
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";

type Json = Record<string, unknown>;

const PICK_HEADERS = ["content-type", "accept", "authorization", "x-api-key", "anthropic-version", "x-goog-api-key", "user-agent"];

function pickHeaders(h: http.IncomingHttpHeaders): Json {
  const out: Json = {};
  for (const k of PICK_HEADERS) {
    if (h[k] !== undefined) out[k] = k === "content-type" && String(h[k]).startsWith("multipart/form-data") ? "multipart/form-data" : h[k];
  }
  return out;
}

interface ScriptedResponse {
  status: number;
  contentType?: string;
  json?: unknown;
  raw?: string;
}

async function withServer<T>(
  responses: ScriptedResponse[],
  onRequest: (req: http.IncomingMessage, body: Buffer) => void,
  run: (base: string) => Promise<T>,
): Promise<T> {
  let index = 0;
  const server = http.createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      onRequest(req, Buffer.concat(chunks));
      const r = responses[Math.min(index++, responses.length - 1)];
      res.writeHead(r.status, { "content-type": r.contentType ?? "application/json" });
      res.end(r.raw ?? JSON.stringify(r.json));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const port = (server.address() as { port: number }).port;
    return await run(`http://127.0.0.1:${port}`);
  } finally {
    server.closeAllConnections?.();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export async function buildMediaGolden(out: string): Promise<unknown> {
  const proto = await import("../../packages/shared/utils/ai-protocol.ts");
  const { chatCompletion } = await import("@guizhi/core");
  const fmt = await import("../../apps/desktop/src/main/services/media/transcript-format.ts");
  const sum = await import("../../apps/desktop/src/main/services/media/media-summary.ts");
  const tr = await import("../../apps/desktop/src/main/services/media/transcribe.ts");
  const notes = await import("../../packages/shared/utils/media-summary.ts");
  const videoUrl = await import("../../apps/desktop/src/main/services/import/video-url.ts");
  const refs = await import("../../packages/shared/utils/media-refs.ts");
  const speaker = await import("../../packages/shared/utils/speaker-note.ts");

  // ── 协议判定 / 端点 / 头 / 用量 ──
  const resolveCases = [
    { apiProtocol: "openai" }, { apiProtocol: "anthropic", provider: "openai" }, { apiProtocol: "gemini" },
    { apiProtocol: "bogus", provider: "anthropic" }, { provider: "Anthropic" }, { provider: "anthropic" },
    { provider: "claude" }, { provider: "google" }, { provider: "GEMINI" }, { provider: "custom", apiUrl: "https://api.anthropic.com/v1" },
    { provider: "custom", apiUrl: "https://generativelanguage.googleapis.com/v1beta" }, { provider: "custom", apiUrl: "https://x.test" }, {},
  ].map((c) => ({ c, protocol: proto.resolveAIProtocol(c as never) }));
  const urls = [
    "https://api.openai.com/v1", "https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions", "https://x.test",
    "https://x.test/v2", "https://x.test/v1/models", "https://x.test/custom/path#", "  https://x.test/v1  ", "https://x.test/v1/messages",
    "https://x.test/v1beta", "https://x.test/v1beta/openai", "https://x.test/openai", "https://x.test/v1/embeddings/", "", "#",
    "https://x.test/v1/audio/transcriptions", "https://x.test/images/generations",
  ];
  const endpoints = urls.flatMap((apiUrl) =>
    (["openai", "anthropic", "gemini"] as const).map((protocol) => {
      const base = proto.resolveProtocolBase(apiUrl, protocol);
      return {
        apiUrl, protocol, base,
        chat: proto.buildChatEndpointFromBase(base),
        transcriptions: proto.buildTranscriptionsEndpointFromBase(base),
      };
    }),
  );
  const headerCases = (["openai", "anthropic", "gemini"] as const).flatMap((protocol) =>
    [undefined, { accept: "text/event-stream" }, { contentType: false }, { useNativeGeminiAuth: true, accept: "application/json" }].map((o) => ({
      protocol, o: o ?? null, headers: proto.buildHeadersForProtocol(protocol, "KEY", o),
    })),
  );
  const usagePayloads: unknown[] = [
    null, 1, "x", {}, { usage: null }, { usage: {} }, { usage: { prompt_tokens: 3, completion_tokens: 4 } },
    { usage: { prompt_tokens: "5", completion_tokens: "6" } }, { usage: { prompt_tokens: 7 } }, { usage: { completion_tokens: 8 } },
    { usage: { input_tokens: 9, output_tokens: 10 } }, { usageMetadata: { promptTokenCount: 11, candidatesTokenCount: 12 } },
    { usage: { promptTokenCount: 13, candidatesTokenCount: 14 } }, { usage: { prompt_tokens: "abc", completion_tokens: 2 } },
    { usage: { prompt_tokens: null, completion_tokens: null } }, { usage: { prompt_tokens: "", completion_tokens: " 7 " } },
    { usage: { prompt_tokens: true, completion_tokens: 1 } }, { usage: { prompt_tokens: 1.5, completion_tokens: 2.25 } },
    { usage: { prompt_tokens: "0x10", completion_tokens: "1e2" } }, { usage: 0, usageMetadata: { promptTokenCount: 1 } },
    { usage: [1, 2] }, { usage: { prompt_tokens: -3, completion_tokens: 0 } }, { usage: { input_tokens: 4 }, usageMetadata: { promptTokenCount: 9 } },
  ];
  const usages = usagePayloads.flatMap((payload) =>
    (["openai", "anthropic", "gemini"] as const).map((protocol) => ({
      payload, protocol, usage: proto.extractUsageFromChatResponse(payload, protocol) ?? null,
    })),
  );

  // ── 对话客户端：对本地服务重放 ──
  interface ChatSpec {
    name: string;
    config: { provider?: string; apiProtocol?: string; apiKey: string; urlPath: string; model: string };
    messages: { role: string; content: string }[];
    options?: { temperature?: number; maxTokens?: number; responseFormat?: { type: string }; stream?: boolean };
    responses: ScriptedResponse[];
  }
  const sse = (events: unknown[], sep = "\n\n") => events.map((e) => (typeof e === "string" ? `data: ${e}` : `data: ${JSON.stringify(e)}`)).join(sep) + sep;
  const openaiOk = (content: string, extra: Json = {}) => ({ status: 200, json: { choices: [{ message: { content }, finish_reason: "stop" }], usage: { prompt_tokens: 10, completion_tokens: 20 }, ...extra } });
  const std = [{ role: "system", content: "你是助手" }, { role: "user", content: "你好，世界" }];
  const base = { apiKey: "sk-test", model: "gpt-x", urlPath: "/v1" };
  const chatSpecs: ChatSpec[] = [
    { name: "openaiPlain", config: base, messages: std, responses: [openaiOk("你好！")] },
    { name: "openaiOptions", config: base, messages: std, options: { temperature: 0.1, maxTokens: 100, responseFormat: { type: "json_object" } }, responses: [openaiOk("{}")] },
    { name: "openaiLength", config: base, messages: std, responses: [{ status: 200, json: { choices: [{ message: { content: "截断" }, finish_reason: "length" }] } }] },
    {
      name: "anthropic",
      config: { ...base, apiProtocol: "anthropic", model: "claude-x" },
      messages: [{ role: "system", content: "系统提示" }, { role: "user", content: "问" }, { role: "assistant", content: "答" }, { role: "user", content: "再问" }],
      responses: [{ status: 200, json: { content: [{ type: "text", text: "甲" }, { type: "tool_use" }, { type: "text", text: "乙" }], stop_reason: "max_tokens", usage: { input_tokens: 5, output_tokens: 6 } } }],
    },
    { name: "anthropicNoSystem", config: { ...base, apiProtocol: "anthropic" }, messages: [{ role: "user", content: "问" }], responses: [{ status: 200, json: { content: [], stop_reason: "end_turn" } }] },
    { name: "anthropicEmptySystem", config: { ...base, apiProtocol: "anthropic", urlPath: "/v1/messages" }, messages: [{ role: "system", content: "" }, { role: "user", content: "问" }], responses: [{ status: 200, json: { content: [{ type: "text", text: "x" }] } }] },
    { name: "geminiModelPrefix", config: { ...base, apiProtocol: "gemini", model: "models/gemini-2.0", urlPath: "/v1beta" }, messages: std, responses: [openaiOk("g")] },
    { name: "qwenThinking", config: { ...base, model: "Qwen3-32B" }, messages: std, responses: [openaiOk("q")] },
    { name: "qwenProviderThinking", config: { ...base, provider: "dashscope", model: "x" }, messages: std, responses: [openaiOk("q")] },
    {
      name: "qwenRetryWithoutThinking",
      config: { ...base, model: "qwen-plus" },
      messages: std,
      responses: [{ status: 400, json: { error: { message: "Unrecognized request argument supplied: enable_thinking" } } }, openaiOk("重发成功")],
    },
    {
      name: "qwenRetryStillFails",
      config: { ...base, model: "qwen-plus" },
      messages: std,
      responses: [{ status: 400, raw: "bad enable_thinking" }, { status: 429, json: { message: "限流了" } }],
    },
    { name: "errorJsonInner", config: base, messages: std, responses: [{ status: 401, json: { error: { message: "无效密钥" } } }] },
    { name: "errorJsonTop", config: base, messages: std, responses: [{ status: 402, json: { message: "余额不足" } }] },
    { name: "errorJsonNoMessage", config: base, messages: std, responses: [{ status: 403, json: { error: {} } }] },
    { name: "errorJsonEmptyInner", config: base, messages: std, responses: [{ status: 403, json: { error: { message: "" }, message: "顶层" } }] },
    { name: "errorText", config: base, messages: std, responses: [{ status: 500, contentType: "text/plain", raw: "boom" }] },
    { name: "errorArray", config: base, messages: std, responses: [{ status: 502, raw: "[1,2]" }] },
    { name: "unexpectedFormat", config: base, messages: std, responses: [{ status: 200, json: { foo: 1 } }] },
    { name: "contentNotString", config: base, messages: std, responses: [{ status: 200, json: { choices: [{ message: { content: null } }] } }] },
    { name: "emptyChoices", config: base, messages: std, responses: [{ status: 200, json: { choices: [] } }] },
    { name: "explicitEndpoint", config: { ...base, urlPath: "/custom/chat#" }, messages: std, responses: [openaiOk("e")] },
    { name: "missingKey", config: { ...base, apiKey: "" }, messages: std, responses: [openaiOk("x")] },
    { name: "missingModel", config: { ...base, model: "" }, messages: std, responses: [openaiOk("x")] },
    {
      name: "streamOpenai",
      config: base,
      messages: std,
      options: { stream: true },
      responses: [{ status: 200, contentType: "text/event-stream", raw: sse([
        { choices: [{ delta: { content: "你" } }] }, { choices: [{ delta: { content: "好😀" } }] },
        { choices: [{ delta: { content: "！" }, finish_reason: "stop" }], usage: { prompt_tokens: 3, completion_tokens: 4 } }, "[DONE]",
      ]) }],
    },
    {
      name: "streamOpenaiCrlf",
      config: base,
      messages: std,
      options: { stream: true },
      responses: [{ status: 200, contentType: "text/event-stream", raw: sse([{ choices: [{ delta: { content: "甲" } }] }, { choices: [{ delta: { content: "乙" }, finish_reason: "length" }] }], "\r\n\r\n") }],
    },
    {
      name: "streamAnthropic",
      config: { ...base, apiProtocol: "anthropic" },
      messages: std,
      options: { stream: true },
      responses: [{ status: 200, contentType: "text/event-stream", raw: sse([
        { type: "message_start", message: { usage: { input_tokens: 7 } } },
        { type: "content_block_delta", delta: { type: "text_delta", text: "子" } },
        { type: "content_block_delta", delta: { type: "text_delta", text: "丑" } },
        { type: "message_delta", delta: { stop_reason: "max_tokens" }, usage: { output_tokens: 9 } },
        { type: "message_stop" },
      ]) }],
    },
    { name: "streamNoFinish", config: base, messages: std, options: { stream: true }, responses: [{ status: 200, contentType: "text/event-stream", raw: sse([{ choices: [{ delta: { content: "半截" } }] }]) }] },
    { name: "streamEmptyContent", config: base, messages: std, options: { stream: true }, responses: [{ status: 200, contentType: "text/event-stream", raw: sse([{ choices: [{ delta: {}, finish_reason: "stop" }] }]) }] },
    { name: "streamErrorEvent", config: base, messages: std, options: { stream: true }, responses: [{ status: 200, contentType: "text/event-stream", raw: sse([{ error: { message: "x" } }]) }] },
    { name: "streamErrorType", config: base, messages: std, options: { stream: true }, responses: [{ status: 200, contentType: "text/event-stream", raw: sse([{ type: "error" }]) }] },
    { name: "streamLeftover", config: base, messages: std, options: { stream: true }, responses: [{ status: 200, contentType: "text/event-stream", raw: `data: ${JSON.stringify({ choices: [{ delta: { content: "尾" }, finish_reason: "stop" }] })}` }] },
    { name: "streamJsonFallback", config: base, messages: std, options: { stream: true }, responses: [openaiOk("非流式回落")] },
    { name: "streamMultiData", config: base, messages: std, options: { stream: true }, responses: [{ status: 200, contentType: "text/event-stream", raw: 'data: {"choices":[{"delta":\ndata: {"content":"拼"},"finish_reason":"stop"}]}\n\n' }] },
  ];
  const chatCases: unknown[] = [];
  for (const spec of chatSpecs) {
    const requests: Json[] = [];
    const progress: number[] = [];
    const deltas: string[] = [];
    const outcome = await withServer(
      spec.responses,
      (req, body) => requests.push({ method: req.method, path: req.url, headers: pickHeaders(req.headers), rawBody: body.toString("utf8") }),
      async (baseUrl) => {
        try {
          const r = await chatCompletion(
            { provider: spec.config.provider, apiProtocol: spec.config.apiProtocol as never, apiKey: spec.config.apiKey, apiUrl: baseUrl + spec.config.urlPath, model: spec.config.model },
            spec.messages as never,
            { ...spec.options, onProgress: (n: number) => progress.push(n), onDelta: (t: string) => deltas.push(t) } as never,
          );
          return { ok: true, content: r.content, finishReason: r.finishReason ?? null, usage: r.usage ?? null };
        } catch (e) {
          return { ok: false, error: errMsg(e) };
        }
      },
    );
    chatCases.push({ spec, requests, progress, deltas, outcome });
  }

  // ── 文字稿排版 ──
  const glossarySources: (string | null | undefined)[][] = [
    ["使用 Docker 部署 Node.js. 与 GitHub Actions", "TypeScript 与 C"],
    ["", null, undefined],
    ["a b c d", "ab"],
    ["Python3.11 和 python3.11 以及 PYTHON", "x+"],
    ["中文，无拉丁词"],
    ["pi-agent、seaco-paraformer, C++ / C# / .NET"],
    [Array.from({ length: 60 }, (_, i) => `Term${i}`).join(" 的 ")],
    ["  " + "Edge ".repeat(200)],
    ["尾部点. 尾部横杠- 尾部加号+ 尾部井#"],
  ];
  const glossary = glossarySources.map((sources) => ({ sources, terms: fmt.extractGlossaryTerms(...sources) }));

  const longZh = Array.from({ length: 360 }, (_, i) => `这是第${i}句口播内容，讲了一些事情`).join("，");
  const noPunct = "无标点".repeat(900);
  const spaced = Array.from({ length: 400 }, (_, i) => `word${i}`).join(" ");
  const emoji = "😀".repeat(1700);
  const mixed = Array.from({ length: 80 }, (_, i) => `第${i}段。Next sentence ${i}!\n`).join("");
  const splitTexts = ["", "   ", "短文本", longZh, noPunct, spaced, emoji, mixed, "a".repeat(3201), `${"句子。".repeat(534)}尾巴`];
  const splits = splitTexts.map((t) => ({ text: t, chunks: fmt.splitTranscriptChunks(t), small: fmt.splitTranscriptChunks(t, 50) }));

  const rejectCases: { raw: string; content: string; finishReason?: string }[] = [
    { raw: "原始文本".repeat(10), content: "" },
    { raw: "原始文本".repeat(10), content: "  \n " },
    { raw: "原始文本".repeat(10), content: "整理后，的文本。".repeat(5), finishReason: "length" },
    { raw: "a".repeat(100), content: "短。" },
    { raw: "a".repeat(100), content: "长，".repeat(120) },
    { raw: "a".repeat(100), content: "x".repeat(100) },
    { raw: "a".repeat(100), content: "x".repeat(70) + "。" },
    { raw: "a".repeat(400), content: ("x".repeat(100) + "。").repeat(4) },
    { raw: "a".repeat(400), content: "x".repeat(400) + "。" },
    { raw: "说话人 1：你好\n说话人 2：好", content: "说话人 1：你好。\n说话人 2：好。" },
    { raw: "说话人 1：你好\n说话人 2：好", content: "你好。\n好。" },
    { raw: "说话人 1：你好\n说话人 2：好", content: "说话人 1：你好。\n说话人 2：好。\n说话人 3：多。" },
    { raw: "a".repeat(200), content: "好，" + "x".repeat(130) },
    { raw: "a".repeat(3), content: "，。，" },
  ];
  const rejects = rejectCases.map((c) => ({ ...c, reason: fmt.rejectFormattedChunk(c.raw, { content: c.content, finishReason: c.finishReason }) }));

  const speakerTexts = ["说话人 1：你好\n说话人 2：好\n说话人 1：嗯\n", "无标记", "说话人 12：a\n说话人 一：b\n说话人 3：c"];
  const speakers = speakerTexts.map((t) => ({ text: t, count: speaker.countSpeakerPrefixes(t), list: speaker.listSpeakers(t) }));

  // 用确定性的替身对话：回复只取决于提示词里的块内容，并发顺序不影响结果
  const CHUNK_RE = /【待整理文字稿开始】\n([\s\S]*)\n【待整理文字稿结束】/;
  const chunkOf = (messages: { content: string }[]) => CHUNK_RE.exec(messages[1].content)![1];
  const punctuate = (t: string) => t.replace(/(.{20})/g, "$1，") + "。";
  type StubMode = "ok" | "echo" | "throw" | "failChunk2" | "failAt3" | "dropSpeaker" | "lengthFinish" | "emptyThenOk";
  const formatScenarios: { name: string; raw: string; mode: StubMode; opts?: { maxTotalChars?: number; glossary?: string[]; timeBudgetMs?: number } }[] = [
    { name: "okShort", raw: "今天讲一下 dacker 的用法 然后是 us state 的写法", mode: "ok", opts: { glossary: ["Docker"] } },
    { name: "okLong", raw: longZh, mode: "ok" },
    { name: "skipTooLong", raw: longZh, mode: "ok", opts: { maxTotalChars: 100 } },
    { name: "emptyInput", raw: "  ", mode: "ok" },
    { name: "echoRejected", raw: "平平淡淡".repeat(20), mode: "echo" },
    { name: "throwFirst", raw: longZh, mode: "throw" },
    { name: "failChunk2Partial", raw: longZh, mode: "failChunk2" },
    { name: "failAt3Partial", raw: longZh, mode: "failAt3" },
    { name: "speakerOk", raw: "说话人 1：你好 今天天气不错\n说话人 2：是的 适合出去走走", mode: "ok" },
    { name: "speakerDropped", raw: "说话人 1：你好 今天天气不错\n说话人 2：是的 适合出去走走", mode: "dropSpeaker" },
    { name: "lengthFinish", raw: "一些很普通的话".repeat(10), mode: "lengthFinish" },
    { name: "retryThenOk", raw: "一些很普通的话".repeat(10), mode: "emptyThenOk" },
    { name: "budgetExhausted", raw: longZh, mode: "ok", opts: { timeBudgetMs: 0 } },
  ];
  const formatCases: unknown[] = [];
  for (const sc of formatScenarios) {
    const calls: { chunk: string; messages: unknown; options: unknown }[] = [];
    const progress: number[] = [];
    const attempts = new Map<string, number>();
    const chat = async (_cfg: unknown, messages: { role: string; content: string }[], options: Json) => {
      const chunk = chunkOf(messages);
      calls.push({ chunk, messages, options: { temperature: options.temperature, maxTokens: options.maxTokens, timeoutMs: options.timeoutMs } });
      const n = (attempts.get(chunk) ?? 0) + 1;
      attempts.set(chunk, n);
      switch (sc.mode) {
        case "throw": throw new Error("模型报错 401");
        case "echo": return { content: chunk };
        case "lengthFinish": return { content: punctuate(chunk), finishReason: "length" };
        case "emptyThenOk": return n === 1 ? { content: "" } : { content: punctuate(chunk), usage: { promptTokens: 1, completionTokens: 2 } };
        case "failChunk2":
        case "failAt3": {
          const chunks = fmt.splitTranscriptChunks(sc.raw);
          const at = sc.mode === "failAt3" ? 3 : 1;
          if (chunk === chunks[at]) throw new Error(`第 ${at + 1} 块失败`);
          return { content: punctuate(chunk) };
        }
        case "dropSpeaker": return { content: chunk.replace(/说话人 \d+：/g, "") + "。，" };
        default: return { content: chunk.includes("说话人") ? chunk + "。" : punctuate(chunk) };
      }
    };
    let outcome: Json;
    try {
      const r = await fmt.formatTranscript(sc.raw, { apiKey: "k", apiUrl: "u", model: "stub-model" } as never, {
        ...sc.opts, chat: chat as never, onProgress: (c: number, t: number) => progress.push(c * 1000 + t),
      });
      outcome = { ok: true, text: r.text, skippedReason: r.skippedReason ?? null, partialReason: r.partialReason ?? null };
    } catch (e) {
      outcome = { ok: false, error: errMsg(e) };
    }
    calls.sort((a, b) => (a.chunk < b.chunk ? -1 : a.chunk > b.chunk ? 1 : 0));
    formatCases.push({ sc, calls, progress: [...progress].sort((a, b) => a - b), outcome });
  }

  // ── 内容总结 ──
  const sanitizeInputs = [
    "标题：很好的标题\n\n正文",
    "```markdown\n**总结**\n\n# 一级\n正文\n---\n## 二级\n- 点\n```",
    "视频总结\n\n正文一\n\n\n\n正文二",
    "**总结**：\n正文",
    "# 视频总结\n正文",
    "   \n\n  内容总结  \n正文",
    "---\n***\n- - -\n正文",
    "###### 六级\n####### 七级\n#无空格\n",
    "```\n只有围栏\n```",
    "```js\n代码\n``` 后缀",
    "",
  ];
  const sanitized = sanitizeInputs.map((s) => ({ s, out: sum.sanitizeMediaSummary(s) }));
  const titleInputs = [
    "标题：好标题\n正文", "**标题：加粗标题**\n正文", "标题:半角冒号\n正文", "标题：  《书名号》  \n正文", "标题：" + "长".repeat(80) + "\n正文",
    "没有标题\n正文", "标题：\n正文", "标题：“引号”\n", "标题：a   b\t c\n", "  标题：前导空白\n正文", "标题：'单引号'\n正文", "标题：中间《书》名\n",
  ];
  const titles = titleInputs.map((s) => ({ s, out: sum.splitTitleFromSummary(s) }));

  const summaryCalls: Json[] = [];
  const summaryScenarios: { name: string; input: { title: string; context?: string; transcript: string }; replies: string[] | "throw" | "empty" }[] = [
    { name: "short", input: { title: "测试视频", context: "简介 ".repeat(300), transcript: "第一句。第二句。" }, replies: ["标题：AI 拟的标题\n\n总起句。\n\n**小节一**\n- 要点"] },
    { name: "shortNoTitle", input: { title: "", transcript: "文字稿内容".repeat(30) }, replies: ["直接就是总结正文\n- 点"] },
    { name: "emptyTranscript", input: { title: "x", transcript: "   " }, replies: ["x"] },
    { name: "emptyReply", input: { title: "x", transcript: "文字稿" }, replies: "empty" },
    { name: "throw", input: { title: "x", transcript: "文字稿" }, replies: "throw" },
    { name: "onlyTitleReply", input: { title: "x", transcript: "文字稿" }, replies: ["标题：只有标题"] },
    {
      name: "mapReduce",
      input: { title: "长视频", context: "介绍", transcript: Array.from({ length: 700 }, (_, i) => `第${i}句比较长的口播内容讲了这件事。`).join("") },
      replies: ["笔记甲", "笔记乙", "标题：综合标题\n\n综合总结\n- 要点"],
    },
    {
      name: "mapReduceTruncated",
      input: { title: "超长视频", transcript: Array.from({ length: 16000 }, (_, i) => `第${i}句口播。`).join("") },
      replies: Array.from({ length: 13 }, (_, i) => (i < 12 ? `笔记${i}` : "标题：超长标题\n\n总结")),
    },
  ];
  const summaryCases: unknown[] = [];
  for (const sc of summaryScenarios) {
    const calls: Json[] = [];
    let n = 0;
    const chat = async (_c: unknown, messages: unknown, options: Json) => {
      calls.push({ messages, options: { temperature: options.temperature, maxTokens: options.maxTokens, timeoutMs: options.timeoutMs } });
      if (sc.replies === "throw") throw new Error("上游失败");
      if (sc.replies === "empty") return { content: "   " };
      return { content: sc.replies[n++] ?? "兜底" };
    };
    let outcome: Json;
    try {
      const r = await sum.generateMediaSummary(sc.input, { apiKey: "k", apiUrl: "u", model: "m" } as never, { chat: chat as never });
      outcome = { ok: true, summary: r.summary, title: r.title };
    } catch (e) {
      outcome = { ok: false, error: errMsg(e) };
    }
    summaryCases.push({ sc, calls, outcome });
  }
  const contentTitleCases: unknown[] = [];
  for (const [name, source, reply] of [
    ["plain", "文案内容 图中文字", "一个好标题"],
    ["withProtocol", "文案", "标题：协议标题"],
    ["fenced", "文案", "```\n围栏标题\n```"],
    ["multiLine", "文案", "\n\n  首个非空行  \n第二行"],
    ["empty", "   ", "不会被调用"],
    ["emptyReply", "文案", "   "],
    ["long", "x".repeat(5000), "标题"],
  ] as const) {
    const calls: Json[] = [];
    const chat = async (_c: unknown, messages: unknown, options: Json) => {
      calls.push({ messages, options: { temperature: options.temperature, maxTokens: options.maxTokens, timeoutMs: options.timeoutMs } });
      return { content: reply };
    };
    const title = await sum.generateContentTitle(source, { apiKey: "k", apiUrl: "u", model: "m" } as never, { chat: chat as never });
    contentTitleCases.push({ name, source, reply, calls, title });
  }

  // ── 正文小节 / 注记 ──
  const heading = "## 视频总结";
  const upsertContents = [
    "", "   ", "> 平台：抖音 · 作者：甲\n\n正文段落\n\n更多", "> 平台：抖音\n", "> 平台：抖音\n\n", "local-video://a.mp4\n\n简介文字",
    "没有元数据头的正文\n第二行", "## 视频总结\n\n旧总结\n\n---\n后续", "## 音频总结\n旧\n## 其他\n内容", "前言\n## 视频总结\n旧总结尾部",
    "> 平台：B站\n\n## 视频总结\n\n旧\n\n## 讨论\n内容",
  ];
  const upserts = upsertContents.flatMap((content) =>
    [heading, "## 音频总结"].map((h) => ({ content, heading: h, out: notes.upsertMediaSummarySection(content, h, "  新总结第一行\n- 要点  ") })),
  );
  const originalTitleCases = [
    ["> 平台：抖音\n\n正文", "原来的标题"], ["> 平台：抖音\n> 原标题：已有\n\n正文", "新的"], ["正文没有引用块", "标题"],
    ["> a\n> b\n>c\n正文", "标题"], ["> 平台：抖音", "  "], ["> 平台：抖音\n>原标题：无空格\n", "x"], ["", "t"],
  ].map(([content, title]) => ({ content, title, out: notes.appendOriginalTitleNote(content, title) }));
  const sourceNoteContents = [
    "> 平台：抖音 · 作者：甲\n\n正文", "> 平台：抖音\n> 文字稿来源：旧来源\n\n正文", "> 平台：抖音\n\n> 文字稿生成失败：x\n\n正文", "没有平台行\n正文",
    "> 平台：抖音\n> 未配置「语音转写」模型\n> 简介：x\n\n正文", "", "> 平台:半角\n正文", ">平台：无空格\n\n正文",
  ];
  const sourceNotes = sourceNoteContents.map((content) => ({
    content, strip: videoUrl.stripTranscriptionNote(content), upsert: videoUrl.upsertTranscriptionSourceNote(content, "音频识别（sensevoice）"),
  }));
  const assetContents = ["local-video://abc-1.mp4 以及 local-video://def.mp4", "无", "local-video://..", "local-image://x.png", "xlocal-video://a_b.c-d.mkv后缀", "local-video://中文.mp4"];
  const assets = assetContents.map((c) => ({ c, ref: refs.extractLocalAssetRef(c, "local-video") }));

  // ── 转写 ──
  const cleanInputs = [
    "所谓的高手 都是", "持っ ていけない", "hello world  foo", "高手，。", "好，、；啊", "a。！b", "中 文 English 中 文", "，，，，", "，。！", "  前后空白  ",
    "段一 \t 段二", "全角　空格　测试", "한국어 텍스트 입니다", "日本語 と English", "😀 😀 中 😀", "。，", "中文 \n 换行",
  ];
  const cleans = cleanInputs.map((s) => ({ s, out: tr.cleanTranscriptText(s) }));
  const endpointCases = [
    { apiUrl: "https://x.test/v1", apiProtocol: "openai" }, { apiUrl: "https://x.test", provider: "custom" }, { apiUrl: "https://api.anthropic.com/v1" },
    { apiUrl: "https://generativelanguage.googleapis.com/v1beta", provider: "google" }, { apiUrl: "https://x.test/stt#" }, { apiUrl: "https://x.test/v1/audio/transcriptions" },
    { apiUrl: "https://x.test/v2/", apiProtocol: "openai" }, { apiUrl: "http://127.0.0.1:8620/v1" }, { apiUrl: "", apiProtocol: "openai" },
  ].map((c) => ({ c, endpoint: tr.buildTranscriptionsEndpoint(c as never) }));

  const wavDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-golden-wav-"));
  const wavs = [0.5, 0, 1, 0.00001, 2.25].map((seconds) => {
    const file = path.join(wavDir, `s-${seconds}.wav`);
    tr.writeSilentWav(file, seconds);
    const bytes = fs.readFileSync(file);
    return { seconds, size: bytes.length, sha256: crypto.createHash("sha256").update(bytes).digest("hex") };
  });

  // 转写请求：对本地服务重放，记录 TS 发出的请求形态
  const audioFile = path.join(wavDir, "talk.mp3");
  fs.writeFileSync(audioFile, Buffer.from(Array.from({ length: 5000 }, (_, i) => (i * 7) % 256)));
  const audioSha = crypto.createHash("sha256").update(fs.readFileSync(audioFile)).digest("hex");
  const transcribeSpecs: { name: string; diarize?: boolean; responses: ScriptedResponse[]; cfg?: Json }[] = [
    { name: "ok", responses: [{ status: 200, json: { text: "你好 世界，。" } }] },
    { name: "okDiarizeIgnoredForCloud", diarize: true, responses: [{ status: 200, json: { text: "好" } }] },
    { name: "http500", responses: [{ status: 500, contentType: "text/plain", raw: "出错了".repeat(200) }] },
    { name: "http401", responses: [{ status: 401, json: { error: "bad key" } }] },
    { name: "noTextField", responses: [{ status: 200, json: { result: "x" } }] },
    { name: "textNotString", responses: [{ status: 200, json: { text: 5 } }] },
    { name: "emptyText", responses: [{ status: 200, json: { text: "  ，， " } }] },
    { name: "protocolUnsupported", cfg: { apiProtocol: "anthropic" }, responses: [{ status: 200, json: { text: "x" } }] },
  ];
  const transcribeCases: unknown[] = [];
  for (const sc of transcribeSpecs) {
    const requests: Json[] = [];
    const outcome = await withServer(
      sc.responses,
      async (req, body) => {
        const form = await new Response(body, { headers: { "content-type": String(req.headers["content-type"]) } }).formData();
        const file = form.get("file") as File | null;
        requests.push({
          method: req.method, path: req.url, headers: (() => { const h = pickHeaders(req.headers); delete h.accept; return h; })(),
          fields: [...form.keys()].filter((k) => k !== "file").map((k) => [k, form.get(k)]),
          file: file ? { name: file.name, size: file.size, sha256: crypto.createHash("sha256").update(Buffer.from(await file.arrayBuffer())).digest("hex") } : null,
        });
      },
      async (baseUrl) => {
        try {
          const text = await tr.transcribeMediaFile(
            audioFile,
            { apiUrl: baseUrl + "/v1", apiKey: "sk-stt", model: "whisper-x", ...(sc.cfg ?? {}) } as never,
            undefined,
            { diarize: sc.diarize },
          );
          return { ok: true, text };
        } catch (e) {
          return { ok: false, error: errMsg(e) };
        }
      },
    );
    // 请求记录里的 await 在服务端回调里异步完成；给事件循环一个回合
    await new Promise((r) => setTimeout(r, 20));
    transcribeCases.push({ sc, requests, outcome });
  }
  fs.rmSync(wavDir, { recursive: true, force: true });
  void out;

  return {
    protocol: { resolveCases, endpoints, headerCases, usages },
    chatCases,
    glossary, splits, rejects, speakers, formatCases,
    sanitized, titles, summaryCases, contentTitleCases,
    upserts, originalTitleCases, sourceNotes, assets,
    cleans, endpointCases, wavs, audioSha, transcribeCases,
  };
}
