import { beforeEach, describe, expect, it, vi } from "vitest";
import { EventEmitter } from "node:events";
import { Readable } from "node:stream";
import { gzipSync } from "node:zlib";
import * as https from "node:https";
import * as dns from "dns/promises";
import {
  getActiveNetworkProxySettings,
  getHttpRequestAgent,
} from "../../../src/main/services/network-proxy";
import {
  webNetworkRequest,
  WEB_RESPONSE_LIMIT,
} from "../../../src/main/services/web-capture/web-network";

vi.mock("node:https", () => ({ request: vi.fn() }));
vi.mock("dns/promises", () => ({ lookup: vi.fn() }));
vi.mock("../../../src/main/services/network-proxy", () => ({
  getActiveNetworkProxySettings: vi.fn(),
  getHttpRequestAgent: vi.fn(),
  hasAnyProxyConfigured: () => false,
}));
const resolveProxy = vi.hoisted(() => vi.fn());
vi.mock("electron", () => ({ session: { defaultSession: { resolveProxy } } }));
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getActiveNetworkProxySettings).mockReturnValue({
    mode: "none",
  } as any);
  vi.mocked(getHttpRequestAgent).mockReturnValue(undefined);
  vi.mocked(dns.lookup).mockResolvedValue([
    { address: "93.184.216.34", family: 4 },
  ] as any);
});
function response(body: Buffer, headers: Record<string, string> = {}) {
  vi.mocked(https.request).mockImplementation(((
    _options: unknown,
    callback: (stream: unknown) => void,
  ) => {
    const request = new EventEmitter() as any;
    request.end = () =>
      queueMicrotask(() => {
        const stream = Readable.from([body]);
        Object.assign(stream, { statusCode: 200, headers });
        callback(stream);
      });
    request.destroy = (error: Error) => request.emit("error", error);
    return request;
  }) as any);
}
describe("主进程网页安全出口", () => {
  it.each([
    "https://127.0.0.1/",
    "https://[::ffff:7f00:1]/",
    "https://[64:ff9b::7f00:1]/",
    "https://[ff02::1]/",
  ])("私网与嵌入 IPv4 不发请求：%s", async (url) => {
    await expect(
      webNetworkRequest({ url }, AbortSignal.timeout(1000)),
    ).rejects.toThrow();
    expect(https.request).not.toHaveBeenCalled();
  });
  it("公网 IP 钉扎，保留 Host 与 TLS servername；解析结果混入私网则拒绝", async () => {
    response(Buffer.from("正文"));
    await webNetworkRequest(
      { url: "https://public.example/path" },
      AbortSignal.timeout(1000),
    );
    expect(https.request).toHaveBeenCalledWith(
      expect.objectContaining({
        hostname: "93.184.216.34",
        servername: "public.example",
        headers: expect.objectContaining({ host: "public.example" }),
      }),
      expect.any(Function),
    );
    vi.mocked(dns.lookup).mockResolvedValue([
      { address: "93.184.216.34", family: 4 },
      { address: "10.0.0.1", family: 4 },
    ] as any);
    await expect(
      webNetworkRequest(
        { url: "https://rebound.example/" },
        AbortSignal.timeout(1000),
      ),
    ).rejects.toThrow();
    expect(https.request).toHaveBeenCalledTimes(1);
  });
  it("系统规则 DIRECT 不允许 fake-IP；无法解析代理规则不静默直连", async () => {
    vi.mocked(getActiveNetworkProxySettings).mockReturnValue({
      mode: "system",
    } as any);
    resolveProxy.mockResolvedValue("DIRECT");
    vi.mocked(dns.lookup).mockResolvedValue([
      { address: "198.18.0.1", family: 4 },
    ] as any);
    await expect(
      webNetworkRequest(
        { url: "https://public.example/" },
        AbortSignal.timeout(1000),
      ),
    ).rejects.toThrow();
    resolveProxy.mockResolvedValue("INVALID proxy");
    await expect(
      webNetworkRequest(
        { url: "https://public.example/" },
        AbortSignal.timeout(1000),
      ),
    ).rejects.toThrow(/未回退直连/);
    expect(https.request).not.toHaveBeenCalled();
  });
  it("压缩响应解压后仍执行 10 MiB 上限", async () => {
    response(gzipSync(Buffer.alloc(WEB_RESPONSE_LIMIT + 1)), {
      "content-encoding": "gzip",
    });
    await expect(
      webNetworkRequest(
        { url: "https://public.example/" },
        AbortSignal.timeout(1000),
      ),
    ).rejects.toThrow();
  });
  it("代理错误只返回失败，不能再次发起直连", async () => {
    const agent = {} as any;
    vi.mocked(getHttpRequestAgent).mockReturnValue(agent);
    vi.mocked(https.request).mockImplementation((() => {
      const request = new EventEmitter() as any;
      request.end = () =>
        queueMicrotask(() => request.emit("error", new Error("代理认证失败")));
      return request;
    }) as any);
    await expect(
      webNetworkRequest(
        { url: "https://public.example/" },
        AbortSignal.timeout(1000),
      ),
    ).rejects.toThrow(/代理认证/);
    expect(https.request).toHaveBeenCalledTimes(1);
    expect(dns.lookup).not.toHaveBeenCalled();
  });
});
