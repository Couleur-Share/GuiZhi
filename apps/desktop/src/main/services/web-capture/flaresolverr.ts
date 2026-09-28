import http from "node:http";
import { randomUUID } from "node:crypto";
import { tryGetDatabase } from "../../database";
import {
  normalizeFlareSolverrSettings,
  validateFlareSolverrSettings,
  type FlareSolverrSettings,
} from "@guizhi/shared/utils/flaresolverr";
import { canonicalWebUrl, inWebScope } from "@guizhi/shared/utils/web-scope";
import type { WebCaptureRequest } from "@guizhi/shared/types";
import { assertSafeTarget } from "../import/safe-fetch";
import { connectFlareSolverr } from "./flaresolverr-connection";
import { isWebVerificationPage, staticPageLinks } from "./web-static-route";
import { WebTaskGate, webAbortError } from "./web-task-gate";
import { WEB_RESPONSE_LIMIT } from "./web-network";
import { logAppError } from "../../diagnostic-log";

const gate = new WebTaskGate(1);
export function getFlareSolverrSettings(): FlareSolverrSettings {
  const row = tryGetDatabase()
    ?.prepare("SELECT value FROM settings WHERE key = ?")
    .get("flareSolverr") as { value: string } | undefined;
  if (!row) return normalizeFlareSolverrSettings(undefined);
  try {
    return validateFlareSolverrSettings(JSON.parse(row.value));
  } catch {
    throw new Error("FlareSolverr 设置损坏，请在采集设置中重新保存");
  }
}

/** 回环控制接口直连，不经过全局代理；从不向提取器或日志传递 Cookie。 */
export async function flareSolverrCommand(
  endpoint: string,
  input: object,
  signal: AbortSignal,
): Promise<any> {
  const url = new URL(endpoint);
  if (
    url.protocol !== "http:" ||
    url.hostname !== "127.0.0.1" ||
    url.pathname !== "/v1" ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  )
    throw new Error("FlareSolverr 控制接口只允许回环地址");
  if (signal.aborted) throw webAbortError(signal);
  const body = JSON.stringify(input);
  return new Promise((resolve, reject) => {
    const request = http.request(
      url,
      {
        method: "POST",
        agent: false,
        signal,
        headers: {
          "content-type": "application/json",
          "content-length": Buffer.byteLength(body),
        },
      },
      (response) => {
        const chunks: Buffer[] = [];
        let bytes = 0;
        response.on("data", (chunk: Buffer) => {
          bytes += chunk.length;
          if (bytes > 16 * 1024 * 1024)
            response.destroy(new Error("FlareSolverr 响应超过 16 MiB"));
          else chunks.push(chunk);
        });
        response.once("error", reject);
        response.once("end", () => {
          try {
            const result = JSON.parse(Buffer.concat(chunks).toString("utf8"));
            if (response.statusCode !== 200 || result.status !== "ok") {
              // 上游错误可能携带 Cookie 或完整请求，不能原样展示。
              throw new Error(
                /timeout|timed out/i.test(String(result.message))
                  ? "FlareSolverr 验证超时（60 秒），请稍后重试"
                  : "FlareSolverr 未能完成请求，请检查服务状态或稍后重试",
              );
            }
            resolve(result);
          } catch (error) {
            reject(
              error instanceof SyntaxError
                ? new Error("FlareSolverr 返回了无效响应")
                : error,
            );
          }
        });
      },
    );
    request.once("error", (error) =>
      reject(
        signal.aborted
          ? webAbortError(signal)
          : new Error("无法连接 FlareSolverr，请检查服务是否启动", {
              cause: error,
            }),
      ),
    );
    request.end(body);
  });
}

export async function captureWithFlareSolverr(
  request: WebCaptureRequest,
  signal: AbortSignal,
  settings = getFlareSolverrSettings(),
) {
  return gate.run(signal, async () => {
    const entry = new URL(canonicalWebUrl(request.url));
    if (entry.protocol !== "https:" || (entry.port && entry.port !== "443"))
      throw new Error("FlareSolverr 后备采集只支持 HTTPS 标准端口", {
        cause: { webCaptureCode: "security" },
      });
    await assertSafeTarget(entry, true);
    if (request.scope && !inWebScope(entry.href, request.scope))
      throw new Error("采集入口超出目录范围");
    const connection = await connectFlareSolverr(
      validateFlareSolverrSettings(settings),
      signal,
    );
    const session = `guizhi-${randomUUID()}`;
    try {
      await flareSolverrCommand(
        connection.endpoint,
        { cmd: "sessions.create", session },
        signal,
      );
      const result = await flareSolverrCommand(
        connection.endpoint,
        { cmd: "request.get", session, url: entry.href, maxTimeout: 60_000 },
        signal,
      );
      const solution = result.solution;
      if (
        !solution ||
        typeof solution.url !== "string" ||
        typeof solution.response !== "string" ||
        !Number.isInteger(solution.status)
      )
        throw new Error("FlareSolverr 返回了不完整的网页响应");
      const finalUrl = canonicalWebUrl(solution.url);
      await assertSafeTarget(new URL(finalUrl), true);
      if (
        new URL(finalUrl).origin !== entry.origin ||
        (request.scope && !inWebScope(finalUrl, request.scope))
      )
        throw new Error("FlareSolverr 重定向超出允许范围", {
          cause: { webCaptureCode: "security" },
        });
      if (Buffer.byteLength(solution.response) > WEB_RESPONSE_LIMIT)
        throw new Error("FlareSolverr 网页超过 10 MiB");
      if (solution.status < 200 || solution.status >= 400)
        throw new Error(`FlareSolverr 网页返回 HTTP ${solution.status}`);
      if (isWebVerificationPage(solution.response))
        throw new Error("FlareSolverr 未通过网站的人机验证", {
          cause: { webCaptureCode: "captcha" },
        });
      return {
        html: solution.response,
        url: finalUrl,
        status: solution.status,
        links: staticPageLinks(solution.response, finalUrl),
      };
    } finally {
      // 即使创建超时或用户取消，也清理这个客户端预先生成的唯一会话。
      try {
        await flareSolverrCommand(
          connection.endpoint,
          { cmd: "sessions.destroy", session },
          AbortSignal.timeout(10_000),
        );
      } catch {
        logAppError({
          scope: "main",
          action: "清理 FlareSolverr 会话",
          message: "测试或采集会话清理失败，请检查 FlareSolverr 服务",
        });
      }
      connection.close();
    }
  });
}

export async function checkFlareSolverrConnection(
  settings: FlareSolverrSettings,
) {
  const signal = AbortSignal.timeout(25_000);
  const connection = await connectFlareSolverr(
    validateFlareSolverrSettings(settings),
    signal,
  );
  try {
    await flareSolverrCommand(
      connection.endpoint,
      { cmd: "sessions.list" },
      signal,
    );
    return { connected: true };
  } finally {
    connection.close();
  }
}
