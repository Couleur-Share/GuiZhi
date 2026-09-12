// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import http from "node:http";
import type { AddressInfo } from "node:net";
import type { AIClientConfig } from "@guizhi/core";

const mock = vi.hoisted(() => ({ config: null as AIClientConfig | null, usage: vi.fn(), agent: vi.fn() }));
vi.mock("../../src/main/services/media/media-summary", () => ({ resolveMediaSummaryConfig: () => mock.config }));
vi.mock("../../src/main/services/ai-usage", () => ({ recordMainAiUsage: mock.usage }));
vi.mock("../../src/main/services/network-proxy", () => ({ getHttpRequestAgent: mock.agent }));
import { nativeSearchAvailability, nativeSearchEndpoint, searchNativeWeb } from "../../src/main/services/themed-reading/native-search";
import { decodeNativeSearchResponse, nativeSearchResults } from "../../src/main/services/themed-reading/native-search-response";

const completed = () => ({ status: "completed", output: [
  { type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "https://example.org/extra" }] } },
  { type: "message", content: [{ type: "output_text", text: "生成的摘要 https://hallucinated.example/", annotations: [{ type: "url_citation", title: "官方资料", url: "https://example.org/doc#one" }] }] },
], usage: { input_tokens: 12, output_tokens: 34 } });

describe("原生搜索实际 HTTP 传输", () => {
  let server: http.Server;
  let handle: (req: http.IncomingMessage, res: http.ServerResponse) => void;
  let received: any;
  beforeEach(async () => {
    mock.usage.mockReset(); mock.agent.mockReset().mockReturnValue(undefined); received = undefined;
    handle = (_req, res) => res.end(JSON.stringify(completed()));
    server = http.createServer((req, res) => {
      let body = ""; req.on("data", data => body += data); req.on("end", () => {
        received = { url: req.url, authorization: req.headers.authorization, body: JSON.parse(body) }; handle(req, res);
      });
    });
    await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
    mock.config = { apiUrl: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, apiKey: "fixture-secret", model: "gpt-6-astra" };
  });
  afterEach(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); });
  it("强制搜索且仅发送关键词，沿用代理并记录模型 token，不把摘要当正文", async () => {
    const results = await searchNativeWeb("  模型发布  ", new AbortController().signal);
    expect(received.url).toBe("/v1/responses"); expect(received.authorization).toBe("Bearer fixture-secret");
    expect(received.body).toMatchObject({ input: "模型发布", model: "gpt-6-astra", stream: true, store: false, tools: [{ type: "web_search" }], tool_choice: "required", include: ["web_search_call.action.sources"] });
    expect(JSON.stringify(received.body)).not.toContain("fixture-secret");
    expect(results).toEqual([{ title: "官方资料", url: "https://example.org/doc" }, { title: "example.org", url: "https://example.org/extra" }]);
    expect(mock.agent).toHaveBeenCalledWith(expect.any(URL));
    expect(mock.usage).toHaveBeenCalledTimes(1);
    expect(mock.usage).toHaveBeenCalledWith({ scenario: "themedReading", model: "gpt-6-astra", promptTokens: 12, completionTokens: 34 });
  });
  it("SSE 分片与多字节字符可完整读取，必须收到 completed 终态", async () => {
    handle = (_req, res) => {
      res.setHeader("Content-Type", "text/event-stream");
      const body = Buffer.from(`: heartbeat\r\n\r\ndata: ${JSON.stringify({ type: "response.created" })}\r\n\r\ndata: ${JSON.stringify({ type: "response.completed", response: completed() })}\r\n\r\ndata: [DONE]\r\n\r\n`);
      for (let i = 0; i < body.length; i += 7) res.write(body.subarray(i, i + 7)); res.end();
    };
    expect((await searchNativeWeb("主题", new AbortController().signal))[0].title).toBe("官方资料");
    handle = (_req, res) => res.end('data: {"type":"response.created"}\n\ndata: [DONE]\n\n');
    await expect(searchNativeWeb("主题", new AbortController().signal)).rejects.toThrow("响应不完整");
  });
  it.each([301, 400, 401, 403, 404, 429, 502])("HTTP %s 不跟随重定向、不回显上游内容，失败计账", async code => {
    handle = (_req, res) => { res.writeHead(code, { Location: "/private" }); res.end("echo fixture-secret"); };
    const error = await searchNativeWeb("主题", new AbortController().signal).catch(error => error);
    expect(error.message).toContain(`HTTP ${code}`); expect(error.message).not.toContain("fixture-secret");
    expect(mock.agent).toHaveBeenCalledTimes(1); expect(mock.usage).toHaveBeenCalledWith(expect.objectContaining({ failed: true }));
  });
  it("已取消请求不联网，执行中的取消和超时中止传输", async () => {
    const controller = new AbortController(); controller.abort();
    await expect(searchNativeWeb("主题", controller.signal)).rejects.toThrow(); expect(received).toBeUndefined();
    handle = () => {};
    const pending = new AbortController();
    const request = searchNativeWeb("主题", pending.signal); setTimeout(() => pending.abort(), 20);
    await expect(request).rejects.toThrow("已取消");
    await expect(searchNativeWeb("主题", AbortSignal.timeout(30))).rejects.toThrow("超时");
  });
  it("响应大小有界且流错误不泄漏上游错误详情", async () => {
    handle = (_req, res) => res.end(" ".repeat(9 * 1024 * 1024));
    await expect(searchNativeWeb("主题", new AbortController().signal)).rejects.toThrow("大小上限");
    handle = (_req, res) => res.end('data: {"type":"response.failed","error":{"message":"fixture-secret"}}\n\n');
    const error = await searchNativeWeb("主题", new AbortController().signal).catch(error => error);
    expect(error.message).toContain("响应不完整"); expect(error.message).not.toContain("fixture-secret");
  });
  it("无模型与协议不兼容时提前失败，模型路由变化立即生效", async () => {
    mock.config = null; expect(nativeSearchAvailability()).toMatchObject({ error: expect.stringContaining("主文本模型") });
    await expect(searchNativeWeb("主题", new AbortController().signal)).rejects.toThrow("配置主文本模型");
    mock.config = { apiUrl: "https://api.anthropic.com", apiKey: "fixture-secret", model: "other", apiProtocol: "anthropic" };
    expect(nativeSearchAvailability()).toMatchObject({ error: expect.stringContaining("协议不支持") });
    await expect(searchNativeWeb("主题", new AbortController().signal)).rejects.toThrow("协议不支持"); expect(received).toBeUndefined();
  });
});

describe("原生搜索来源与端点边界", () => {
  it.each([
    ["https://api.example.org", "https://api.example.org/v1/responses"],
    ["https://api.example.org/v1/", "https://api.example.org/v1/responses"],
    ["https://api.example.org/proxy/v1/chat/completions", "https://api.example.org/proxy/v1/responses"],
    ["https://api.example.org/proxy/responses#", "https://api.example.org/proxy/responses"],
  ])("标准基础地址与显式 Responses 地址 %s", (apiUrl, expected) => {
    expect(nativeSearchEndpoint({ apiUrl, apiKey: "key", model: "model" }).href).toBe(expected);
  });
  it.each(["https://api.example.org/custom#", "https://user:pass@api.example.org/v1", "https://api.example.org/v1?key=secret", "file:///private"]) ("不猜自定义端点且拒绝 URL 凭证 %s", apiUrl => {
    expect(() => nativeSearchEndpoint({ apiUrl, apiKey: "key", model: "model" })).toThrow();
  });
  it("去重、过滤内网和非网页来源，返回最多五条公开来源", () => {
    const response = completed();
    (response.output[0] as any).action.sources = [null, ...["http://127.0.0.1/", "http://192.168.1.1/", "http://[::1]/", "http://localhost/", "file:///secret", "https://user:pass@example.org/", "https://example.org/doc#two", ...Array.from({ length: 8 }, (_, n) => `https://example.org/${n}`)].map(url => ({ type: "url", url }))];
    const results = nativeSearchResults(response);
    expect(results).toHaveLength(5); expect(results.every(r => r.url.startsWith("https://example.org/"))).toBe(true);
    expect(results.filter(r => r.url === "https://example.org/doc")).toHaveLength(1);
    expect(results.every(r => !r.text)).toBe(true);
  });
  it("拒绝模型自称搜索、失败终态及无可用来源", () => {
    const response = completed(); response.output.shift();
    expect(() => nativeSearchResults(response)).toThrow("未完成联网搜索");
    expect(() => nativeSearchResults({ ...completed(), status: "incomplete" })).toThrow("未完成");
    expect(() => nativeSearchResults({ ...completed(), output: [{ type: "web_search_call", status: "completed", action: { type: "search" } }] })).toThrow("未返回有效");
    expect(() => decodeNativeSearchResponse('data: {"type":"error","message":"secret"}\n\n')).toThrow("响应不完整");
  });
});
