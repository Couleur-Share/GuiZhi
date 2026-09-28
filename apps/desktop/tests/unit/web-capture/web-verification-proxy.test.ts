// @vitest-environment node
import net from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createWebVerificationProxy } from "../../../src/main/services/web-capture/web-verification-proxy";
const mocks = vi.hoisted(() => ({
  agent: vi.fn(),
  safe: vi.fn(),
  resolve: vi.fn(),
  dial: vi.fn(),
  fixturePort: 0,
}));
vi.mock("node:net", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:net")>();
  const connect = (...args: Parameters<typeof net.connect>) => {
    if (
      typeof args[0] === "object" &&
      "port" in args[0] &&
      args[0].port === 443
    ) {
      mocks.dial(args[0]);
      return actual.connect({ host: "127.0.0.1", port: mocks.fixturePort });
    }
    return actual.connect(...args);
  };
  return { ...actual, default: { ...actual, connect }, connect };
});
vi.mock("../../../src/main/services/web-capture/web-network", () => ({
  proxyAgent: mocks.agent,
}));
vi.mock("../../../src/main/services/import/safe-fetch", () => ({
  assertSafeTarget: mocks.safe,
}));
vi.mock("../../../src/main/services/net-safety", () => ({
  resolvePublicAddress: mocks.resolve,
}));
const cleanup: (() => void)[] = [];
afterEach(() => {
  for (const close of cleanup.splice(0)) close();
  vi.resetAllMocks();
});
function open(port: number) {
  const socket = net.connect(port, "127.0.0.1");
  cleanup.push(() => socket.destroy());
  return socket;
}
describe("验证的安全 CONNECT 隧道", () => {
  it("直连只拨已验证的公网 IP，不交给 Chromium 重新解析域名", async () => {
    const server = net.createServer((socket) => socket.pipe(socket));
    await new Promise<void>((resolve) =>
      server.listen(0, "127.0.0.1", resolve),
    );
    cleanup.push(() => server.close());
    mocks.fixturePort = (server.address() as net.AddressInfo).port;
    mocks.agent.mockResolvedValue(undefined);
    mocks.resolve.mockResolvedValue({ address: "93.184.216.34", family: 4 });
    const proxy = await createWebVerificationProxy(
      new AbortController().signal,
    );
    cleanup.push(proxy.close);
    const client = open(proxy.port);
    const received = new Promise<void>((resolve) =>
      client.once("data", () => resolve()),
    );
    client.write(
      "CONNECT public.example:443 HTTP/1.1\r\nHost: public.example\r\n\r\n",
    );
    await received;
    expect(mocks.dial).toHaveBeenCalledWith({
      host: "93.184.216.34",
      family: 4,
      port: 443,
    });
    expect(mocks.safe).toHaveBeenCalledWith(
      new URL("https://public.example/"),
      false,
    );
  });
  it("原样转发 TLS 字节，沿用上游代理且不销毁共享代理", async () => {
    const server = net.createServer((socket) => socket.pipe(socket));
    await new Promise<void>((resolve) =>
      server.listen(0, "127.0.0.1", resolve),
    );
    cleanup.push(() => server.close());
    const port = (server.address() as net.AddressInfo).port;
    const connect = vi.fn(async () => {
      const socket = open(port);
      await new Promise<void>((resolve) => socket.once("connect", resolve));
      return socket;
    });
    const destroy = vi.fn();
    mocks.agent.mockResolvedValue({ connect, destroy });
    const controller = new AbortController();
    const proxy = await createWebVerificationProxy(controller.signal);
    cleanup.push(proxy.close);
    const client = open(proxy.port);
    const tls = Buffer.from([0x16, 0x03, 0x01, 0x00, 0x04, 1, 2, 3, 4]);
    const received = await new Promise<Buffer>((resolve, reject) => {
      let data = Buffer.alloc(0);
      client.on("error", reject);
      client.on("data", (chunk) => {
        data = Buffer.concat([data, chunk]);
        const end = data.indexOf("\r\n\r\n");
        if (end >= 0 && data.length >= end + 4 + tls.length)
          resolve(data.subarray(end + 4));
      });
      client.write(
        Buffer.concat([
          Buffer.from(
            "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com\r\n\r\n",
          ),
          tls,
        ]),
      );
    });
    expect(received).toEqual(tls);
    expect(connect).toHaveBeenCalledWith(expect.anything(), {
      host: "example.com",
      port: 443,
      secureEndpoint: false,
    });
    expect(mocks.safe).toHaveBeenCalledWith(
      new URL("https://example.com/"),
      true,
    );
    controller.abort();
    expect(destroy).not.toHaveBeenCalled();
  });
  it("内网目标在建立隧道之前拒绝", async () => {
    const connect = vi.fn();
    mocks.agent.mockResolvedValue({ connect });
    mocks.safe.mockRejectedValue(new Error("不允许访问内网地址"));
    const proxy = await createWebVerificationProxy(
      new AbortController().signal,
    );
    cleanup.push(proxy.close);
    const client = open(proxy.port);
    const closed = new Promise<void>((resolve) =>
      client.once("close", () => resolve()),
    );
    client.write("CONNECT 127.0.0.1:443 HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n");
    await closed;
    expect(connect).not.toHaveBeenCalled();
    expect(mocks.resolve).not.toHaveBeenCalled();
  });
  it("非标准端口在代理和 DNS 之前拒绝", async () => {
    const proxy = await createWebVerificationProxy(
      new AbortController().signal,
    );
    cleanup.push(proxy.close);
    const client = open(proxy.port);
    const closed = new Promise<void>((resolve) =>
      client.once("close", () => resolve()),
    );
    client.write(
      "CONNECT example.com:22 HTTP/1.1\r\nHost: example.com\r\n\r\n",
    );
    await closed;
    expect(mocks.agent).not.toHaveBeenCalled();
  });
});
