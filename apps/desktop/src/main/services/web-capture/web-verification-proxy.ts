import http from "node:http";
import net from "node:net";
import type { Duplex } from "node:stream";
import { assertSafeTarget } from "../import/safe-fetch";
import { resolvePublicAddress } from "../net-safety";
import { proxyAgent } from "./web-network";
import { withWebAbort } from "./web-task-gate";

/** CONNECT 只转发加密字节，保留 Chromium TLS；直连固定到已验证的公网 IP。 */
export async function createWebVerificationProxy(signal: AbortSignal) {
  const sockets = new Set<Duplex>();
  let count = 0,
    bytes = 0,
    closed = false;
  let failure: string | undefined;
  const track = (socket: Duplex) => {
    sockets.add(socket);
    socket.on("error", () => socket.destroy());
    socket.once("close", () => sockets.delete(socket));
  };
  const server = http.createServer((_req, res) => {
    res.writeHead(403).end();
  });
  server.on("connection", track);
  server.on("connect", (req, client, head) => {
    void (async () => {
      if (closed || signal.aborted) throw new Error("验证已取消");
      if (++count > 200) {
        failure = "验证网络预算已用尽";
        close();
        throw new Error(failure);
      }
      const url = new URL(`https://${req.url}`);
      if (
        url.username ||
        url.password ||
        url.pathname !== "/" ||
        url.search ||
        url.hash ||
        (url.port && url.port !== "443")
      )
        throw new Error("验证只允许 HTTPS 标准端口");
      const agent = await proxyAgent(url);
      await assertSafeTarget(url, !!agent);
      let upstream: Duplex;
      if (agent) {
        // 上游代理只建立 TCP 隧道；TLS 始终由 Chromium 发起。
        const connection = (
          agent as http.Agent & {
            connect: (
              req: http.IncomingMessage,
              options: object,
            ) => Promise<Duplex>;
          }
        ).connect(req, {
          host: url.hostname,
          port: 443,
          secureEndpoint: false,
        });
        // connect() 不进入共享 Agent 的连接池，不能销毁其他采集正在使用的 Agent。
        void connection.then(
          (socket) => {
            track(socket);
            if (closed || signal.aborted || client.destroyed) socket.destroy();
          },
          () => undefined,
        );
        upstream = await withWebAbort(connection, signal);
        if (!upstream.writable) {
          upstream.destroy();
          throw new Error("上游代理拒绝连接");
        }
      } else {
        const address = await resolvePublicAddress(
          url.hostname.replace(/^\[|\]$/g, ""),
        );
        upstream = await withWebAbort(
          new Promise<net.Socket>((resolve, reject) => {
            const socket = net.connect({
              host: address.address,
              family: address.family,
              port: 443,
            });
            track(socket);
            socket.setTimeout(30_000, () =>
              socket.destroy(new Error("验证连接超时")),
            );
            socket.once("connect", () => resolve(socket));
            socket.once("error", reject);
          }),
          signal,
        );
      }
      track(upstream);
      if (closed || signal.aborted || client.destroyed) {
        upstream.destroy();
        return;
      }
      const budget = (chunk: Buffer) => {
        bytes += chunk.length;
        if (bytes > 50 * 1024 * 1024) {
          failure = "验证网络响应超过 50 MiB";
          close();
        }
      };
      upstream.on("data", budget);
      client.on("data", budget);
      upstream.once("close", () => client.destroy());
      client.once("close", () => upstream.destroy());
      client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length) upstream.write(head);
      client.pipe(upstream).pipe(client);
    })().catch(() => client.destroy());
  });
  const close = () => {
    closed = true;
    signal.removeEventListener("abort", close);
    for (const socket of sockets) socket.destroy();
    server.close();
  };
  signal.addEventListener("abort", close, { once: true });
  try {
    await withWebAbort(
      new Promise<void>((resolve, reject) => {
        server.once("error", reject);
        server.listen(0, "127.0.0.1", resolve);
      }),
      signal,
    );
    const address = server.address() as net.AddressInfo;
    return {
      port: address.port,
      close,
      get error() {
        return failure;
      },
    };
  } catch (error) {
    close();
    throw error;
  }
}
