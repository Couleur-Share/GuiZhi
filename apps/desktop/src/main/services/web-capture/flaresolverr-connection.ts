import { spawn } from "node:child_process";
import net from "node:net";
import type { FlareSolverrSettings } from "@guizhi/shared/utils/flaresolverr";
import { webPause, webAbortError } from "./web-task-gate";

async function reservePort(): Promise<number> {
  const server = net.createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const port = (server.address() as net.AddressInfo).port;
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return port;
}

async function portReady(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = net.connect({ host: "127.0.0.1", port });
    const finish = (ready: boolean) => {
      socket.destroy();
      resolve(ready);
    };
    socket.setTimeout(300, () => finish(false));
    socket.once("connect", () => finish(true));
    socket.once("error", () => finish(false));
  });
}

/** 只启动自己的回环隧道；不改 SSH 配置、不关闭用户已有连接。 */
export async function connectFlareSolverr(
  settings: FlareSolverrSettings,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  if (settings.connection === "local")
    return {
      endpoint: `http://127.0.0.1:${settings.port}/v1`,
      close: () => undefined,
    };
  if (!settings.sshHost) throw new Error("请填写已有 SSH 配置中的主机别名");
  const port = await reservePort();
  signal.throwIfAborted();
  const child = spawn(
    "ssh",
    [
      "-N",
      "-T",
      "-o",
      "BatchMode=yes",
      "-o",
      "ExitOnForwardFailure=yes",
      "-o",
      "ConnectTimeout=10",
      "-o",
      "ServerAliveInterval=15",
      "-o",
      "ServerAliveCountMax=2",
      "-L",
      `127.0.0.1:${port}:127.0.0.1:${settings.port}`,
      settings.sshHost,
    ],
    { windowsHide: true, stdio: ["ignore", "ignore", "pipe"] },
  );
  let exited = false,
    failed = false,
    closed = false;
  child.once("exit", () => {
    exited = true;
  });
  child.once("error", () => {
    failed = true;
  });
  // 不保留 SSH 原始输出，避免配置中的账号信息进入日志。
  child.stderr.resume();
  const close = () => {
    if (closed) return;
    closed = true;
    signal.removeEventListener("abort", close);
    child.kill();
  };
  signal.addEventListener("abort", close, { once: true });
  try {
    for (let attempt = 0; attempt < 100; attempt++) {
      if (signal.aborted) throw webAbortError(signal);
      if (exited || failed)
        throw new Error(
          "FlareSolverr SSH 连接失败，请检查主机别名、密钥和服务器端口",
        );
      if (await portReady(port)) {
        signal.removeEventListener("abort", close);
        return { endpoint: `http://127.0.0.1:${port}/v1`, close };
      }
      await webPause(100, signal);
    }
    throw new Error("FlareSolverr SSH 连接超时");
  } catch (error) {
    close();
    throw error;
  }
}
