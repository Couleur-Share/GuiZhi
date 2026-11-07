// @vitest-environment node
import http from "node:http";
import net from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  session: { defaultSession: { setProxy: vi.fn(async () => undefined) } },
}));

import {
  applyNetworkProxySettings,
  fetchWithNetworkProxy,
  getHttpRequestAgent,
} from "../../../src/main/services/network-proxy";

const HOST = "guizhi-socks-probe.invalid";

/** 最小 SOCKS5 服务：记录 CONNECT 中的目标地址，随后以「规则禁止」拒绝。 */
async function startSocksProbe() {
  const targets: string[] = [];
  const sockets = new Set<net.Socket>();
  const server = net.createServer((socket) => {
    sockets.add(socket);
    socket.once("close", () => sockets.delete(socket));
    socket.once("data", () => {
      socket.write(Buffer.from([0x05, 0x00]));
      socket.once("data", (request) => {
        targets.push(
          request[3] === 0x03
            ? request.subarray(5, 5 + request[4]).toString()
            : Array.from(request.subarray(4, 8)).join("."),
        );
        socket.end(Buffer.from([0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0]));
      });
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    port: (server.address() as net.AddressInfo).port,
    targets,
    close: () =>
      new Promise<void>((resolve) => {
        for (const socket of sockets) socket.destroy();
        server.close(() => resolve());
      }),
  };
}

let probe: Awaited<ReturnType<typeof startSocksProbe>> | undefined;

async function useSocksProbe() {
  probe = await startSocksProbe();
  await applyNetworkProxySettings({
    mode: "manual",
    protocol: "socks5",
    host: "127.0.0.1",
    port: probe.port,
    bypass: "<local>,localhost,127.0.0.1,::1",
    username: "",
    password: "",
  });
  return probe;
}

afterEach(async () => {
  await applyNetworkProxySettings({ mode: "direct" });
  await probe?.close();
  probe = undefined;
});

describe("手动 SOCKS5 代理", () => {
  it("把域名交给代理解析，不在本地解析", async () => {
    const { targets } = await useSocksProbe();
    await new Promise<void>((resolve) => {
      http
        .get(`http://${HOST}/`, { agent: getHttpRequestAgent(`http://${HOST}/`) }, (response) => {
          response.resume();
          resolve();
        })
        .on("error", () => resolve());
    });
    expect(targets).toEqual([HOST]);
  });

  it("全局 fetch 经 SOCKS5 时同样由代理解析域名", async () => {
    const { targets } = await useSocksProbe();
    await expect(fetchWithNetworkProxy(`https://${HOST}/`)).rejects.toThrow();
    expect(targets).toEqual([HOST]);
  });
});
