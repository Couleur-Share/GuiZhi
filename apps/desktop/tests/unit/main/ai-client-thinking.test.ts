import { afterEach, describe, expect, it, vi } from "vitest";

// 直接引 ai-client 而不是包入口：index.ts 还会拉起 runtime-paths / ai-config，
// 那两个在单测里用不上
import { chatCompletion } from "@guizhi/core/ai-client";

const MESSAGES = [
  { role: "system" as const, content: "你是文字稿排版助手。" },
  { role: "user" as const, content: "请补标点。" },
];

const RELAY = {
  apiKey: "sk-test",
  apiUrl: "https://api3.wlai.vip/v1",
};

function okBody(content = "补好标点的正文。") {
  return JSON.stringify({
    choices: [{ message: { content }, finish_reason: "stop" }],
    usage: { prompt_tokens: 10, completion_tokens: 20 },
  });
}

function errorBody(message: string) {
  return JSON.stringify({ error: { message } });
}

/** 打桩 fetch，返回每次请求实际发出的 body */
function stubFetch(...responses: Response[]) {
  const sent: Record<string, unknown>[] = [];
  const queue = [...responses];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init: RequestInit) => {
      sent.push(JSON.parse(init.body as string));
      const next = queue.shift();
      if (!next) {
        throw new Error("桩里没有准备这一次请求的响应");
      }
      return next;
    }),
  );
  return sent;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("chatCompletion 的思考开关", () => {
  it("Qwen 系按模型名认出来，非流式一律关思考", async () => {
    const sent = stubFetch(new Response(okBody(), { status: 200 }));

    await chatCompletion({ ...RELAY, model: "qwen3.5-flash" }, MESSAGES);

    expect(sent[0].enable_thinking).toBe(false);
  });

  it("模型名看不出来时按 provider 认（dashscope 部署常改名）", async () => {
    const sent = stubFetch(new Response(okBody(), { status: 200 }));

    await chatCompletion(
      { ...RELAY, provider: "dashscope", model: "tongyi-turbo" },
      MESSAGES,
    );

    expect(sent[0].enable_thinking).toBe(false);
  });

  it("非 Qwen 模型压根不带这个字段：官方 OpenAI 对未知字段直接回 400", async () => {
    const sent = stubFetch(new Response(okBody(), { status: 200 }));

    await chatCompletion(
      { ...RELAY, provider: "openai", model: "gpt-5.2" },
      MESSAGES,
    );

    expect("enable_thinking" in sent[0]).toBe(false);
  });

  it("anthropic 协议不带：那边是另一套请求体形状", async () => {
    const sent = stubFetch(
      new Response(
        JSON.stringify({
          content: [{ type: "text", text: "补好标点的正文。" }],
          stop_reason: "end_turn",
        }),
        { status: 200 },
      ),
    );

    await chatCompletion(
      {
        apiKey: "sk-test",
        apiUrl: "https://api.anthropic.com",
        apiProtocol: "anthropic",
        // 中转站上挂着 qwen 名字的 anthropic 端点，判定不能只看模型名
        model: "qwen-proxy",
      },
      MESSAGES,
    );

    expect("enable_thinking" in sent[0]).toBe(false);
  });
});

describe("端点不认识 enable_thinking 时", () => {
  it("摘掉该字段重发一次，第二次成功就正常返回", async () => {
    const sent = stubFetch(
      new Response(
        errorBody("Unrecognized request argument supplied: enable_thinking"),
        { status: 400 },
      ),
      new Response(okBody("补好标点的正文。"), { status: 200 }),
    );
    vi.spyOn(console, "warn").mockImplementation(() => {});

    const result = await chatCompletion(
      { ...RELAY, model: "qwen3.5-flash" },
      MESSAGES,
    );

    expect(result.content).toBe("补好标点的正文。");
    expect(sent).toHaveLength(2);
    expect(sent[0].enable_thinking).toBe(false);
    expect("enable_thinking" in sent[1]).toBe(false);
  });

  it("重发仍失败 → 抛出第二次的原话，不粉饰成参数问题", async () => {
    stubFetch(
      new Response(errorBody("parameter.enable_thinking is not supported"), {
        status: 400,
      }),
      new Response(errorBody("模型不存在"), { status: 404 }),
    );
    vi.spyOn(console, "warn").mockImplementation(() => {});

    await expect(
      chatCompletion({ ...RELAY, model: "qwen3.5-flash" }, MESSAGES),
    ).rejects.toThrow("模型不存在");
  });
});

describe("与该参数无关的失败", () => {
  it("不重发：余额不足重来一次也是同一个结果，只会白花一次往返", async () => {
    const sent = stubFetch(
      new Response(errorBody("Insufficient balance"), { status: 402 }),
    );

    await expect(
      chatCompletion({ ...RELAY, model: "qwen3.5-flash" }, MESSAGES),
    ).rejects.toThrow("Insufficient balance");
    expect(sent).toHaveLength(1);
  });

  it("响应体不是 JSON 时回落到状态码，而不是抛出一句空话", async () => {
    stubFetch(new Response("<html>502 Bad Gateway</html>", { status: 502 }));

    await expect(
      chatCompletion({ ...RELAY, model: "qwen3.5-flash" }, MESSAGES),
    ).rejects.toThrow("AI API request failed (502)");
  });
});
