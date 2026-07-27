import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  buildGeminiImageEndpoint,
  buildImagesEndpointFromBase,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import {
  buildGeminiImageBody,
  buildOpenAIImageBody,
  describeImageHttpFailure,
  generateImage,
  isRetryableImageStatus,
  openAiImageSize,
  parseGeminiImageResponse,
  parseOpenAIImageResponse,
} from "../../../src/main/services/illustration/image-gen";

describe("生图端点拼接", () => {
  it("OpenAI 基址补 /v1/images/generations", () => {
    expect(
      buildImagesEndpointFromBase(
        resolveProtocolBase("https://api.openai.com", "openai"),
      ),
    ).toBe("https://api.openai.com/v1/images/generations");
  });

  it("已带版本号的基址不再重复补 /v1", () => {
    expect(
      buildImagesEndpointFromBase(
        resolveProtocolBase("https://relay.example.com/v1", "openai"),
      ),
    ).toBe("https://relay.example.com/v1/images/generations");
  });

  it("末尾 # 表示地址已是完整端点，原样使用", () => {
    expect(
      buildImagesEndpointFromBase(
        resolveProtocolBase("https://relay.example.com/custom/draw#", "openai"),
      ),
    ).toBe("https://relay.example.com/custom/draw");
  });

  it("Anthropic 没有文生图接口，返回空串由调用方报错", () => {
    expect(
      buildImagesEndpointFromBase(
        resolveProtocolBase("https://api.anthropic.com", "anthropic"),
      ),
    ).toBe("");
  });

  it("Gemini 走原生 generateContent，模型名进路径", () => {
    expect(
      buildGeminiImageEndpoint(
        resolveProtocolBase(
          "https://generativelanguage.googleapis.com",
          "gemini",
        ),
        "gemini-3-pro-image",
      ),
    ).toBe(
      "https://generativelanguage.googleapis.com/v1beta/models/gemini-3-pro-image:generateContent",
    );
  });

  it("基址带 /openai 兼容层后缀时剥掉再拼原生路径", () => {
    expect(
      buildGeminiImageEndpoint(
        resolveProtocolBase(
          "https://generativelanguage.googleapis.com/v1beta/openai",
          "gemini",
        ),
        "nano-banana",
      ),
    ).toBe(
      "https://generativelanguage.googleapis.com/v1beta/models/nano-banana:generateContent",
    );
  });
});

describe("openAiImageSize", () => {
  it("gpt-image-2 能给出真正的 16:9", () => {
    expect(openAiImageSize("gpt-image-2", "16:9")).toBe("1536x864");
    // 边长须为 16 的倍数、长短边比 ≤3:1
    const [width, height] = openAiImageSize("gpt-image-2", "16:9")
      .split("x")
      .map(Number);
    expect(width % 16).toBe(0);
    expect(height % 16).toBe(0);
    expect(width / height).toBeLessThanOrEqual(3);
  });

  it("其余模型只有三档固定尺寸，横版退到 1536x1024", () => {
    expect(openAiImageSize("gpt-image-1", "16:9")).toBe("1536x1024");
    expect(openAiImageSize("dall-e-3", "4:3")).toBe("1536x1024");
    expect(openAiImageSize("dall-e-3", "1:1")).toBe("1024x1024");
  });
});

describe("buildOpenAIImageBody", () => {
  it("gpt-image 系不传 response_format——传了会被判为非法参数", () => {
    const body = JSON.parse(buildOpenAIImageBody("gpt-image-2", "画一只猫", "16:9"));
    expect(body.response_format).toBeUndefined();
    expect(body).toMatchObject({ model: "gpt-image-2", n: 1, size: "1536x864" });
  });

  it("其余模型必须显式要 b64_json，否则回的是会过期的 URL", () => {
    const body = JSON.parse(buildOpenAIImageBody("dall-e-3", "画一只猫", "16:9"));
    expect(body.response_format).toBe("b64_json");
  });

  it("探测模式用最小方图 + 最低质量档，把这一次调用压到最便宜", () => {
    const body = JSON.parse(
      buildOpenAIImageBody("gpt-image-2", "探测", "16:9", true),
    );
    expect(body.size).toBe("1024x1024");
    expect(body.quality).toBe("low");
  });

  it("quality 只对 gpt-image 系降档——dall-e 的取值是 standard/hd，传 low 会报错", () => {
    const body = JSON.parse(
      buildOpenAIImageBody("dall-e-3", "探测", "16:9", true),
    );
    expect(body.quality).toBeUndefined();
    expect(body.size).toBe("1024x1024");
  });
});

describe("buildGeminiImageBody", () => {
  it("比例走 imageConfig.aspectRatio，并只要图片模态", () => {
    const body = JSON.parse(buildGeminiImageBody("画一只猫", "16:9"));
    expect(body.generationConfig.imageConfig.aspectRatio).toBe("16:9");
    expect(body.generationConfig.responseModalities).toEqual(["IMAGE"]);
    expect(body.contents[0].parts[0].text).toBe("画一只猫");
  });
});

describe("生图响应解析", () => {
  it("OpenAI 的 b64_json", () => {
    expect(
      parseOpenAIImageResponse('{"data":[{"b64_json":"QUJD"}]}'),
    ).toEqual({ kind: "base64", data: "QUJD" });
  });

  it("中转站无视 response_format 回 URL 时也认", () => {
    expect(
      parseOpenAIImageResponse('{"data":[{"url":"https://cdn.example.com/a.png"}]}'),
    ).toEqual({ kind: "url", url: "https://cdn.example.com/a.png" });
  });

  it("OpenAI 的错误信息原样抛出，而不是「没有图片数据」", () => {
    expect(() =>
      parseOpenAIImageResponse('{"error":{"message":"billing hard limit reached"}}'),
    ).toThrow("billing hard limit reached");
  });

  it("Gemini 的 inlineData（REST 的 camelCase）", () => {
    expect(
      parseGeminiImageResponse(
        '{"candidates":[{"content":{"parts":[{"text":"好的"},{"inlineData":{"mimeType":"image/png","data":"QUJD"}}]}}]}',
      ),
    ).toEqual({ kind: "base64", data: "QUJD", mime: "image/png" });
  });

  it("Gemini 的 inline_data（部分中转站的 snake_case）", () => {
    expect(
      parseGeminiImageResponse(
        '{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/jpeg","data":"QUJD"}}]}}]}',
      ),
    ).toEqual({ kind: "base64", data: "QUJD", mime: "image/jpeg" });
  });

  it("Gemini 只回了文字（被安全策略挡下）时报错", () => {
    expect(() =>
      parseGeminiImageResponse(
        '{"candidates":[{"content":{"parts":[{"text":"抱歉"}]}}]}',
      ),
    ).toThrow("没有图片数据");
  });
});

describe("isRetryableImageStatus", () => {
  it("429 与 5xx 值得换个上游再来一次", () => {
    expect(isRetryableImageStatus(429)).toBe(true);
    expect(isRetryableImageStatus(500)).toBe(true);
    expect(isRetryableImageStatus(524)).toBe(true);
  });

  it("其余 4xx 重发也是同一个结果", () => {
    expect(isRetryableImageStatus(400)).toBe(false);
    expect(isRetryableImageStatus(401)).toBe(false);
    expect(isRetryableImageStatus(404)).toBe(false);
  });
});

describe("describeImageHttpFailure", () => {
  it("带上 error.code——它是「重试」与「改提示词」之间唯一的分界", () => {
    const message = describeImageHttpFailure(
      500,
      '{"error":{"message":"upstream down","code":"server_error"}}',
    );
    expect(message).toContain("HTTP 500 server_error");
    expect(message).toContain("upstream down");
  });

  it("内容安全拦截给出该改什么，而不是只报一个状态码", () => {
    const message = describeImageHttpFailure(
      400,
      '{"error":{"message":"Your request was rejected by the safety system.","code":"moderation_blocked"}}',
    );
    expect(message).toContain("内容安全拦截");
    expect(message).toContain("改写图题");
    expect(message).toContain("moderation_blocked");
  });

  it("被 WAF 拦下的整页 HTML 压成一行再截断", () => {
    const html = `<html>\n  <body>${"x".repeat(500)}</body>\n</html>`;
    const message = describeImageHttpFailure(403, html);
    expect(message).not.toContain("\n");
    expect(message.length).toBeLessThan(360);
  });
});

/**
 * 重试的意义全在这里：中转站按成功率在一池上游渠道之间轮询，渠道之间的内容
 * 安全严格程度并不一致，同一份提示词落到严格渠道被拦、落到宽松渠道就出图。
 * 不重试的话，一批五张凭空少一张，用户只能再花一次钱重来。
 */
describe("生图重试", () => {
  const CONFIG = {
    apiUrl: "https://relay.example.com/v1",
    apiKey: "sk-test",
    model: "gpt-image-2",
    apiProtocol: "openai" as const,
  };

  const okResponse = () =>
    new Response(JSON.stringify({ data: [{ b64_json: "iVBORw0KGgo=" }] }), {
      status: 200,
    });

  const failResponse = (status: number, code = "") =>
    new Response(JSON.stringify({ error: { message: "boom", code } }), {
      status,
    });

  const generate = (fetchImpl: unknown, probe = false) =>
    generateImage("画一只猫", "16:9", CONFIG, {
      fetchImpl: fetchImpl as typeof globalThis.fetch,
      retryDelaysMs: probe ? undefined : [0, 0],
      probe,
    });

  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("429 换个上游重发——实测拿到过 429 配 safety 文案，下一次就过了", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(failResponse(429))
      .mockResolvedValueOnce(okResponse());
    const image = await generate(fetchImpl);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(image.extension).toBe(".png");
  });

  it("5xx 同样重发", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(failResponse(503))
      .mockResolvedValueOnce(okResponse());
    await generate(fetchImpl);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it("网络层抖动（连接被重置）也重发", async () => {
    const fetchImpl = vi
      .fn()
      .mockRejectedValueOnce(new Error("fetch failed"))
      .mockResolvedValueOnce(okResponse());
    await generate(fetchImpl);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it("400 moderation_blocked 只发一次：这条重发一万次也是同一个结果", async () => {
    const fetchImpl = vi
      .fn()
      .mockImplementation(() => failResponse(400, "moderation_blocked"));
    await expect(generate(fetchImpl)).rejects.toThrow("内容安全拦截");
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("余额不足这类 4xx 也只发一次", async () => {
    const fetchImpl = vi
      .fn()
      .mockImplementation(() => failResponse(401, "invalid_api_key"));
    await expect(generate(fetchImpl)).rejects.toThrow("HTTP 401");
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("重试用尽后注明已经自动重试过，免得用户再手动点几遍", async () => {
    const fetchImpl = vi.fn().mockImplementation(() => failResponse(503));
    await expect(generate(fetchImpl)).rejects.toThrow("已自动重试 2 次");
    expect(fetchImpl).toHaveBeenCalledTimes(3);
  });

  it("用户点了停止就立刻停手，不再重发", async () => {
    const controller = new AbortController();
    const fetchImpl = vi.fn().mockImplementation(() => {
      controller.abort(new Error("已取消"));
      return Promise.reject(new Error("已取消"));
    });
    await expect(
      generateImage("画一只猫", "16:9", CONFIG, {
        fetchImpl: fetchImpl as typeof globalThis.fetch,
        retryDelaysMs: [0, 0],
        signal: controller.signal,
      }),
    ).rejects.toThrow("已取消");
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("连接测试不重试：它按张真实计费，一次不该悄悄变三次", async () => {
    const fetchImpl = vi.fn().mockImplementation(() => failResponse(503));
    await expect(generate(fetchImpl, true)).rejects.toThrow("HTTP 503");
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });
});
