// @vitest-environment node
// 主进程走 undici fetch，jsdom 环境下的 AbortSignal 会被拒收，必须用 node 环境
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import fs from "fs";
import http from "http";
import os from "os";
import path from "path";
import type { AddressInfo } from "net";

// network-proxy 引用 electron，单测中替换为空实现（无代理配置时直连）
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { recognizeImageFile } from "../../../src/main/services/media/ocr";

interface CapturedRequest {
  url: string;
  headers: http.IncomingHttpHeaders;
  body: string;
}

let server: http.Server;
let baseUrl: string;
let captured: CapturedRequest | null = null;
let respond: (res: http.ServerResponse) => void;
let workDir: string;
let imagePath: string;

beforeAll(async () => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-ocr-test-"));
  imagePath = path.join(workDir, "shot.webp");
  fs.writeFileSync(imagePath, Buffer.from([0x52, 0x49, 0x46, 0x46]));

  server = http.createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => {
      captured = {
        url: req.url ?? "",
        headers: req.headers,
        body: Buffer.concat(chunks).toString("utf8"),
      };
      respond(res);
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  baseUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`;
});

afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
  fs.rmSync(workDir, { recursive: true, force: true });
});

function config(overrides: Record<string, unknown> = {}) {
  return {
    apiUrl: baseUrl,
    apiKey: "sk-test",
    model: "gpt-4o",
    ...overrides,
  };
}

describe("主进程 OCR", () => {
  it("请求打到 chat 端点，图片以 data URL 内联，鉴权头正确", async () => {
    respond = (res) => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({ choices: [{ message: { content: "识别出的文字" } }] }),
      );
    };

    const text = await recognizeImageFile(imagePath, config());
    expect(text).toBe("识别出的文字");

    expect(captured?.url).toBe("/v1/chat/completions");
    expect(captured?.headers.authorization).toBe("Bearer sk-test");
    const body = JSON.parse(captured?.body ?? "{}");
    expect(body.model).toBe("gpt-4o");
    const parts = body.messages[0].content;
    expect(parts[0].type).toBe("text");
    // 扩展名决定 MIME，webp 走错会被部分服务商拒收
    expect(parts[1].image_url.url).toBe(
      `data:image/webp;base64,${fs.readFileSync(imagePath).toString("base64")}`,
    );
  });

  it("Anthropic 协议改走 base64 source 块与 x-api-key", async () => {
    respond = (res) => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ content: [{ type: "text", text: "克劳德识别" }] }));
    };

    const text = await recognizeImageFile(
      imagePath,
      config({ apiProtocol: "anthropic" }),
    );
    expect(text).toBe("克劳德识别");
    expect(captured?.headers["x-api-key"]).toBe("sk-test");
    const body = JSON.parse(captured?.body ?? "{}");
    expect(body.max_tokens).toBeGreaterThan(0);
    expect(body.messages[0].content[1].source.media_type).toBe("image/webp");
  });

  it("HTTP 错误透出状态码与响应体，便于判断是限流还是配额", async () => {
    respond = (res) => {
      res.writeHead(429, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: { code: "insufficient_user_quota" } }));
    };

    await expect(recognizeImageFile(imagePath, config())).rejects.toThrow(
      /HTTP 429.*insufficient_user_quota/,
    );
  });

  it("响应缺少文本内容时报错，不返回空串冒充成功", async () => {
    respond = (res) => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ choices: [{ message: {} }] }));
    };

    await expect(recognizeImageFile(imagePath, config())).rejects.toThrow(
      "缺少文本内容",
    );
  });

  it("外部取消会中断请求", async () => {
    respond = () => {
      // 不应答，等调用方取消
    };
    const controller = new AbortController();
    const pending = recognizeImageFile(imagePath, config(), controller.signal);
    controller.abort();
    await expect(pending).rejects.toThrow();
  });
});
