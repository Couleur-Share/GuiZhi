/**
 * macOS arm64 本地转写：用 Node 内置 http 把 FunASR llama.cpp CLI
 * 包成 OpenAI 兼容的 /v1/audio/transcriptions，端口与 Windows Python
 * 引擎相同（127.0.0.1:8620），transcribe.ts 无需分支。
 *
 * 说话人分离（diarize）在此忽略——cam++ 只存在于 Python 引擎。
 */
import { spawn } from "child_process";
import fs from "fs";
import http from "http";
import os from "os";
import path from "path";
import { FUNASR_MODEL_NAME, FUNASR_PORT } from "./funasr-paths";

const DEFAULT_CLI_TIMEOUT_MS = 10 * 60 * 1000;
const MAX_UPLOAD_BYTES = 512 * 1024 * 1024;

export interface GgufShimConfig {
  cliPath: string;
  modelPath: string;
  vadPath: string;
  port?: number;
  cliTimeoutMs?: number;
  onActivity?: () => void;
}

export interface GgufShimHandle {
  close: () => Promise<void>;
}

/** 从 Content-Type 取出 multipart boundary */
export function parseMultipartBoundary(contentType: string): string | null {
  const match = /boundary=(?:"([^"]+)"|([^;\s]+))/i.exec(contentType);
  return match ? (match[1] ?? match[2] ?? "").trim() || null : null;
}

/**
 * 从 multipart/form-data 体里抽出 name="file" 的那一段。
 * 只认这一个字段——转写客户端不会塞别的二进制。
 */
export function extractMultipartFile(
  contentType: string,
  body: Buffer,
): { data: Buffer; filename: string } {
  const boundary = parseMultipartBoundary(contentType);
  if (!boundary) {
    throw new Error("缺少 multipart boundary");
  }
  const delimiter = Buffer.from(`--${boundary}`);
  let start = indexOfBuffer(body, delimiter);
  if (start < 0) {
    throw new Error("multipart 体无效");
  }
  start += delimiter.length;
  while (start < body.length) {
    if (body[start] === 0x2d && body[start + 1] === 0x2d) {
      break;
    }
    if (body[start] === 0x0d && body[start + 1] === 0x0a) {
      start += 2;
    }
    const next = indexOfBuffer(body, delimiter, start);
    const partEnd = next < 0 ? body.length : next;
    const part = body.subarray(start, partEnd);
    const headerEnd = indexOfBuffer(part, Buffer.from("\r\n\r\n"));
    if (headerEnd >= 0) {
      const headers = part.subarray(0, headerEnd).toString("utf8");
      if (/name="file"/i.test(headers)) {
        const filenameMatch = /filename="([^"]*)"/i.exec(headers);
        let data = part.subarray(headerEnd + 4);
        // 段尾的 CRLF 属于分隔符前缀，不是文件内容
        if (
          data.length >= 2 &&
          data[data.length - 2] === 0x0d &&
          data[data.length - 1] === 0x0a
        ) {
          data = data.subarray(0, data.length - 2);
        }
        if (data.length === 0) {
          throw new Error("上传文件为空");
        }
        return {
          data: Buffer.from(data),
          filename: filenameMatch?.[1]?.trim() || "audio.wav",
        };
      }
    }
    if (next < 0) {
      break;
    }
    start = next + delimiter.length;
  }
  throw new Error("缺少 multipart 字段：file");
}

function indexOfBuffer(haystack: Buffer, needle: Buffer, from = 0): number {
  return haystack.indexOf(needle, from);
}

export function buildGgufCliArgs(
  config: Pick<GgufShimConfig, "cliPath" | "modelPath" | "vadPath">,
  audioPath: string,
): string[] {
  return [
    config.cliPath,
    "-m",
    config.modelPath,
    "--vad",
    config.vadPath,
    "-a",
    audioPath,
  ];
}

/** CLI 把转写结果打在 stdout；去掉首尾空白即可 */
export function extractTranscriptFromCliStdout(stdout: string): string {
  return stdout.replace(/\r\n/g, "\n").trim();
}

function suffixFromFilename(filename: string): string {
  const suffix = path.extname(filename);
  if (!suffix || suffix.length > 16) {
    return ".wav";
  }
  return suffix;
}

function sendJson(
  res: http.ServerResponse,
  status: number,
  payload: unknown,
): void {
  const body = Buffer.from(JSON.stringify(payload), "utf8");
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": body.length,
  });
  res.end(body);
}

function readRequestBody(
  req: http.IncomingMessage,
  maxBytes: number,
): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let total = 0;
    req.on("data", (chunk: Buffer) => {
      total += chunk.length;
      if (total > maxBytes) {
        reject(new Error("上传文件过大"));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}

function runCli(
  config: GgufShimConfig,
  audioPath: string,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const args = buildGgufCliArgs(config, audioPath).slice(1);
    const child = spawn(config.cliPath, args, {
      windowsHide: true,
    });
    let stdout = "";
    let stderr = "";
    let settled = false;
    const finish = (action: () => void) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        action();
      }
    };
    const timer = setTimeout(
      () => {
        child.kill();
        finish(() => reject(new Error("本地转写 CLI 超时")));
      },
      config.cliTimeoutMs ?? DEFAULT_CLI_TIMEOUT_MS,
    );

    // 长音频时 CLI 可能长时间无输出；定期心跳让进度 UI 知道还在跑
    const heartbeat = setInterval(() => {
      config.onActivity?.();
    }, 1000);

    child.stdout?.on("data", (chunk: Buffer) => {
      stdout += chunk.toString("utf8");
      config.onActivity?.();
    });
    child.stderr?.on("data", (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
      config.onActivity?.();
    });
    child.on("error", (error) => {
      clearInterval(heartbeat);
      finish(() => reject(error));
    });
    child.on("close", (code) => {
      clearInterval(heartbeat);
      finish(() => {
        if (code === 0) {
          resolve(extractTranscriptFromCliStdout(stdout));
        } else {
          const detail = (stderr || stdout).trim().slice(-400);
          reject(
            new Error(
              detail
                ? `本地转写 CLI 失败：${detail}`
                : `本地转写 CLI 退出码 ${code}`,
            ),
          );
        }
      });
    });
  });
}

/**
 * 在 127.0.0.1:port 拉起适配层。同一时刻只应有一个实例
 * （由 funasr-service 保证）。
 */
export function startGgufShim(config: GgufShimConfig): Promise<GgufShimHandle> {
  const port = config.port ?? FUNASR_PORT;
  let busy = Promise.resolve();

  const server = http.createServer((req, res) => {
    const url = new URL(req.url ?? "/", `http://127.0.0.1:${port}`);

    if (req.method === "GET" && url.pathname === "/v1/models") {
      sendJson(res, 200, {
        object: "list",
        data: [{ id: FUNASR_MODEL_NAME, object: "model" }],
      });
      return;
    }

    if (req.method === "GET" && url.pathname === "/health") {
      sendJson(res, 200, { status: "ok" });
      return;
    }

    if (req.method === "POST" && url.pathname === "/v1/audio/transcriptions") {
      void (async () => {
        const run = async () => {
          const contentType = req.headers["content-type"] ?? "";
          const body = await readRequestBody(req, MAX_UPLOAD_BYTES);
          const file = extractMultipartFile(contentType, body);
          const tmpPath = path.join(
            os.tmpdir(),
            `guizhi-gguf-${Date.now()}-${Math.random().toString(36).slice(2)}${suffixFromFilename(file.filename)}`,
          );
          try {
            fs.writeFileSync(tmpPath, file.data);
            config.onActivity?.();
            const text = await runCli(config, tmpPath);
            sendJson(res, 200, { text });
          } finally {
            fs.rmSync(tmpPath, { force: true });
          }
        };

        // 串行：CLI 一次只能跑一条；与 runExclusiveLocalTranscription 双保险
        const next = busy.then(run, run);
        busy = next.then(
          () => undefined,
          () => undefined,
        );
        try {
          await next;
        } catch (error) {
          if (!res.headersSent) {
            sendJson(res, 500, {
              error: {
                message:
                  error instanceof Error ? error.message : String(error),
              },
            });
          }
        }
      })();
      return;
    }

    sendJson(res, 404, { error: { message: "not found" } });
  });

  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => {
      server.off("error", reject);
      console.log(`[funasr] GGUF shim 已就绪（127.0.0.1:${port}）`);
      resolve({
        close: () =>
          new Promise((closeResolve, closeReject) => {
            server.close((error) =>
              error ? closeReject(error) : closeResolve(),
            );
          }),
      });
    });
  });
}
