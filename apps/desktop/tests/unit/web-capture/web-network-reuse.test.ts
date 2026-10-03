// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as http from "node:http";
import type * as net from "node:net";
import { HttpProxyAgent } from "http-proxy-agent";
import {
  getActiveNetworkProxySettings,
  getHttpRequestAgent,
  resolveManualProxy,
} from "../../../src/main/services/network-proxy";
import {
  WEB_CONNECTION_REUSE_ENV,
  WebConnectionPool,
} from "../../../src/main/services/web-capture/web-connection-pool";
import {
  proxyAgent,
  webNetworkRequest,
} from "../../../src/main/services/web-capture/web-network";
import type {
  WebTrace,
  WebTraceEgressSample,
} from "../../../src/main/services/web-capture/web-trace";

vi.mock("../../../src/main/services/network-proxy", () => ({
  getActiveNetworkProxySettings: vi.fn(),
  getHttpRequestAgent: vi.fn(),
  resolveManualProxy: vi.fn(),
  hasAnyProxyConfigured: () => true,
}));
const resolveSystemProxy = vi.hoisted(() => vi.fn());
vi.mock("electron", () => ({
  session: { defaultSession: { resolveProxy: resolveSystemProxy } },
}));

/** 本机 HTTP 转发代理：既当代理又当源站，统计连接数与请求数，并可按连接内序号注入重置。 */
type ResetPolicy = "never" | "first" | "second" | "truncate-second" | "hang";
async function startProxy(reset: ResetPolicy = "never") {
  const sockets = new Set<net.Socket>();
  const perSocket = new WeakMap<net.Socket, number>();
  const state = { connections: 0, requests: 0 };
  const server = http.createServer((req, res) => {
    state.requests++;
    const nth = (perSocket.get(req.socket) ?? 0) + 1;
    perSocket.set(req.socket, nth);
    // 不读取也不应答，直接掐断连接，模拟服务端关闭了空闲连接或中途重置。
    if ((reset === "first" && nth === 1) || (reset === "second" && nth === 2)) {
      req.socket.destroy();
      return;
    }
    // 收到请求但永不应答，用于验证取消。
    if (reset === "hang") return;
    // 已发出响应头与部分正文后掐断：此时不能再按“尚无响应”的口径重试。
    if (reset === "truncate-second" && nth === 2) {
      req.resume();
      res.writeHead(200, { "content-length": "100" });
      res.write("partial");
      setTimeout(() => req.socket.destroy(), 20);
      return;
    }
    req.resume();
    res.setHeader("content-type", "text/plain");
    res.end(`ok ${req.method} ${req.url}`);
  });
  server.on("connection", (socket) => {
    state.connections++;
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
  });
  await new Promise<void>((resolve) =>
    server.listen(0, "127.0.0.1", () => resolve()),
  );
  const port = (server.address() as net.AddressInfo).port;
  return {
    state,
    sockets,
    proxyUrl: `http://127.0.0.1:${port}`,
    close: () =>
      new Promise<void>((resolve) => {
        for (const socket of sockets) socket.destroy();
        server.close(() => resolve());
      }),
  };
}

function recorder() {
  const samples: WebTraceEgressSample[] = [];
  const trace: WebTrace = {
    enabled: true,
    mark: () => undefined,
    set: () => undefined,
    egress: (sample) => samples.push(sample),
    flush: async () => undefined,
  };
  return { trace, samples };
}

const get = (
  opts: Parameters<typeof webNetworkRequest>[2],
  path = "/page",
  method = "GET",
) =>
  webNetworkRequest(
    {
      url: `http://public.example${path}`,
      method,
      body: method === "POST" ? Buffer.from("x").toString("base64") : undefined,
    },
    AbortSignal.timeout(5000),
    opts,
  );
const bodyOf = (response: { body: string }) =>
  Buffer.from(response.body, "base64").toString("utf8");

let proxy: Awaited<ReturnType<typeof startProxy>>;
const pools: WebConnectionPool[] = [];
const newPool = () => {
  const pool = new WebConnectionPool();
  pools.push(pool);
  return pool;
};
async function useProxy(reset: ResetPolicy = "never") {
  proxy = await startProxy(reset);
  vi.mocked(getActiveNetworkProxySettings).mockReturnValue({
    mode: "manual",
  } as never);
  vi.mocked(resolveManualProxy).mockReturnValue({
    proxyUrl: proxy.proxyUrl,
    socks: false,
  });
  // 原有共享 agent 的行为：不带 keepAlive，每个请求新建连接。
  vi.mocked(getHttpRequestAgent).mockImplementation(
    () => new HttpProxyAgent(proxy.proxyUrl) as never,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  delete process.env[WEB_CONNECTION_REUSE_ENV];
});
afterEach(async () => {
  for (const pool of pools.splice(0)) pool.close();
  await proxy?.close();
});

describe("单次采集内的代理连接复用（本机真实套接字）", () => {
  it("顺序请求复用到代理的同一条连接，追踪标出新建与复用", async () => {
    await useProxy();
    const pool = newPool();
    const { trace, samples } = recorder();
    for (let i = 0; i < 5; i++) {
      const response = await get({ pool, trace }, `/page-${i}`);
      expect(response.status).toBe(200);
      expect(bodyOf(response)).toBe(`ok GET http://public.example/page-${i}`);
    }
    expect(proxy.state).toEqual({ connections: 1, requests: 5 });
    expect(samples.map((item) => item.reused)).toEqual([
      false,
      true,
      true,
      true,
      true,
    ]);
    expect(samples.every((item) => !item.failed && !item.retried)).toBe(true);
    expect(samples.every((item) => item.bytes > 0)).toBe(true);
    expect(
      samples.every(
        (item) =>
          item.method === "GET" &&
          item.origin === "http://public.example" &&
          item.responded,
      ),
    ).toBe(true);
  });

  it("并发请求按 maxSockets 上限建连，之后的请求复用空闲连接", async () => {
    await useProxy();
    const pool = newPool();
    await Promise.all(
      Array.from({ length: 6 }, (_, i) => get({ pool }, `/a-${i}`)),
    );
    const connectionsAfterBurst = proxy.state.connections;
    expect(connectionsAfterBurst).toBeGreaterThan(1);
    expect(connectionsAfterBurst).toBeLessThanOrEqual(6);
    await get({ pool }, "/again");
    expect(proxy.state.connections).toBe(connectionsAfterBurst);
  });

  it("不提供连接范围时每个请求新建连接（原行为）", async () => {
    await useProxy();
    for (let i = 0; i < 5; i++) await get({}, `/page-${i}`);
    expect(proxy.state).toEqual({ connections: 5, requests: 5 });
  });

  it("GUIZHI_WEB_CONNECTION_REUSE=0 时忽略连接范围，行为与原来一致", async () => {
    await useProxy();
    process.env[WEB_CONNECTION_REUSE_ENV] = "0";
    const pool = newPool();
    const agentFor = vi.spyOn(pool, "agentFor");
    for (let i = 0; i < 5; i++) await get({ pool }, `/page-${i}`);
    expect(agentFor).not.toHaveBeenCalled();
    expect(proxy.state).toEqual({ connections: 5, requests: 5 });
  });

  it("close 后代理侧不留下残余连接", async () => {
    await useProxy();
    const pool = newPool();
    await get({ pool });
    await get({ pool }, "/second");
    expect(proxy.sockets.size).toBe(1);
    pool.close();
    await vi.waitFor(() => expect(proxy.sockets.size).toBe(0));
  });

  it("复用的连接被服务端掐断：GET 重试一次并成功", async () => {
    await useProxy("second");
    const pool = newPool();
    const { trace, samples } = recorder();
    await get({ pool, trace }, "/first");
    const response = await get({ pool, trace }, "/second");
    expect(response.status).toBe(200);
    expect(bodyOf(response)).toBe("ok GET http://public.example/second");
    // 第 1 条连接上的第 2 个请求被掐断；重试使用新连接。
    expect(proxy.state).toEqual({ connections: 2, requests: 3 });
    expect(samples).toHaveLength(2);
    expect(samples[1]).toMatchObject({
      retried: true,
      reused: false,
      failed: false,
    });
  });

  it("复用的连接被掐断时 POST 不重试，错误原样抛出", async () => {
    await useProxy("second");
    const pool = newPool();
    const { trace, samples } = recorder();
    await get({ pool, trace }, "/first", "POST");
    await expect(get({ pool, trace }, "/second", "POST")).rejects.toMatchObject(
      { code: "ECONNRESET" },
    );
    expect(proxy.state).toEqual({ connections: 1, requests: 2 });
    expect(samples[1]).toMatchObject({
      method: "POST",
      reused: true,
      responded: false,
      retried: false,
      failed: true,
      error: "ECONNRESET",
    });
  });

  it("响应已开始后被掐断：即使是 GET 也不重试", async () => {
    await useProxy("truncate-second");
    const pool = newPool();
    const { trace, samples } = recorder();
    await get({ pool, trace }, "/first");
    await expect(get({ pool, trace }, "/second")).rejects.toThrow();
    expect(proxy.state).toEqual({ connections: 1, requests: 2 });
    expect(samples[1]).toMatchObject({
      reused: true,
      responded: true,
      retried: false,
      failed: true,
    });
  });
  it("新建连接上的重置不重试", async () => {
    await useProxy("first");
    const pool = newPool();
    const { trace, samples } = recorder();
    await expect(get({ pool, trace })).rejects.toMatchObject({
      code: "ECONNRESET",
    });
    expect(proxy.state).toEqual({ connections: 1, requests: 1 });
    expect(samples).toHaveLength(1);
    expect(samples[0]).toMatchObject({
      reused: false,
      responded: false,
      retried: false,
      failed: true,
      error: "ECONNRESET",
    });
  });

  it("请求被取消时不重试，追踪记为 ABORT_ERR", async () => {
    await useProxy("hang");
    const pool = newPool();
    const { trace, samples } = recorder();
    const controller = new AbortController();
    const pending = webNetworkRequest(
      { url: "http://public.example/slow" },
      controller.signal,
      { pool, trace },
    );
    await vi.waitFor(() => expect(proxy.state.requests).toBe(1));
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(proxy.state).toEqual({ connections: 1, requests: 1 });
    expect(samples).toHaveLength(1);
    expect(samples[0]).toMatchObject({ failed: true, retried: false, error: "ABORT_ERR" });
  });

  it("追踪未启用时不产生出口样本", async () => {
    await useProxy();
    const { trace, samples } = recorder();
    const disabled = { ...trace, enabled: false };
    await get({ trace: disabled });
    expect(samples).toEqual([]);
  });
});

describe("proxyAgent 的 agent 选择", () => {
  beforeEach(() => {
    vi.mocked(getActiveNetworkProxySettings).mockReturnValue({
      mode: "manual",
    } as never);
  });
  const sentinel = { sentinel: true } as never;

  it("手动 HTTP 代理 + 连接范围：交给连接范围，按目标协议区分", async () => {
    vi.mocked(resolveManualProxy).mockReturnValue({
      proxyUrl: "http://127.0.0.1:7900",
      socks: false,
    });
    const pool = newPool();
    const agentFor = vi.spyOn(pool, "agentFor");
    const secure = await proxyAgent(new URL("https://a.example/"), pool);
    const plain = await proxyAgent(new URL("http://a.example/"), pool);
    expect(agentFor).toHaveBeenNthCalledWith(1, "http://127.0.0.1:7900", true);
    expect(agentFor).toHaveBeenNthCalledWith(2, "http://127.0.0.1:7900", false);
    expect(secure).not.toBe(plain);
    expect(getHttpRequestAgent).not.toHaveBeenCalled();
  });

  it("手动 SOCKS5 代理沿用共享 agent，不进入连接范围", async () => {
    vi.mocked(resolveManualProxy).mockReturnValue({
      proxyUrl: "socks5h://127.0.0.1:1080",
      socks: true,
    });
    vi.mocked(getHttpRequestAgent).mockReturnValue(sentinel);
    const pool = newPool();
    const agentFor = vi.spyOn(pool, "agentFor");
    expect(await proxyAgent(new URL("https://a.example/"), pool)).toBe(sentinel);
    expect(agentFor).not.toHaveBeenCalled();
  });

  it("目标命中绕过规则（无手动代理）时沿用共享 agent 的结果", async () => {
    vi.mocked(resolveManualProxy).mockReturnValue(null);
    vi.mocked(getHttpRequestAgent).mockReturnValue(undefined);
    const pool = newPool();
    const agentFor = vi.spyOn(pool, "agentFor");
    expect(await proxyAgent(new URL("https://a.example/"), pool)).toBeUndefined();
    expect(agentFor).not.toHaveBeenCalled();
  });

  it("未提供连接范围时不解析手动代理，直接沿用共享 agent", async () => {
    vi.mocked(getHttpRequestAgent).mockReturnValue(sentinel);
    expect(await proxyAgent(new URL("https://a.example/"))).toBe(sentinel);
    expect(resolveManualProxy).not.toHaveBeenCalled();
  });

  it("系统代理：有连接范围时同一代理地址复用 agent，没有时每次新建", async () => {
    vi.mocked(getActiveNetworkProxySettings).mockReturnValue({
      mode: "system",
    } as never);
    resolveSystemProxy.mockResolvedValue("PROXY 127.0.0.1:7900");
    const pool = newPool();
    const agentFor = vi.spyOn(pool, "agentFor");
    const url = new URL("https://a.example/");
    const first = await proxyAgent(url, pool);
    expect(await proxyAgent(url, pool)).toBe(first);
    expect(agentFor).toHaveBeenCalledWith("http://127.0.0.1:7900", true);
    expect(await proxyAgent(url)).not.toBe(await proxyAgent(url));
  });
});
