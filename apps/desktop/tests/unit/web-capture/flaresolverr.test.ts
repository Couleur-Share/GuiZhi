// @vitest-environment node
import http from "node:http";
import type { AddressInfo } from "node:net";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { validateFlareSolverrSettings } from "@guizhi/shared/utils/flaresolverr";
const mocks = vi.hoisted(() => ({
  connect: vi.fn(),
  close: vi.fn(),
  log: vi.fn(),
}));
vi.mock("../../../src/main/database", () => ({
  tryGetDatabase: () => undefined,
}));
vi.mock("../../../src/main/diagnostic-log", () => ({ logAppError: mocks.log }));
vi.mock("../../../src/main/services/web-capture/web-network", () => ({
  WEB_RESPONSE_LIMIT: 10 * 1024 * 1024,
}));
vi.mock(
  "../../../src/main/services/web-capture/flaresolverr-connection",
  () => ({ connectFlareSolverr: mocks.connect }),
);
import {
  captureWithFlareSolverr,
  flareSolverrCommand,
} from "../../../src/main/services/web-capture/flaresolverr";
const settings = {
  enabled: true,
  connection: "local" as const,
  port: 8191,
  sshHost: "",
};
const request = {
  taskId: "cf",
  purpose: "import" as const,
  url: "https://example.com/article",
};
beforeEach(() => vi.clearAllMocks());
async function fixture(
  run: (endpoint: string, commands: any[]) => Promise<void>,
  solve: (input: any) => any,
) {
  const commands: any[] = [];
  const server = http.createServer(async (req, res) => {
    let body = "";
    for await (const chunk of req) body += chunk;
    const input = JSON.parse(body);
    commands.push(input);
    const response = solve(input);
    if (response === undefined) return;
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify(response));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const endpoint = `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`;
  mocks.connect.mockResolvedValue({ endpoint, close: mocks.close });
  try {
    await run(endpoint, commands);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}
const success = (
  url = request.url,
  html = '<title>文章</title><article>正文<a href="/next">下一页</a></article>',
) => ({
  status: "ok",
  solution: {
    status: 200,
    url,
    response: html,
    cookies: [{ value: "secret" }],
  },
});
describe("FlareSolverr 回环协议和采集边界", () => {
  it("返回正文后销毁自己的会话，不复用 Cookie 再抓网页", async () => {
    await fixture(
      async (_endpoint, commands) => {
        const page = await captureWithFlareSolverr(
          request,
          new AbortController().signal,
          settings,
        );
        expect(page.html).toContain("正文");
        expect(page.links).toEqual(["https://example.com/next"]);
        expect(page).not.toHaveProperty("cookies");
        expect(commands.map((c) => c.cmd)).toEqual([
          "sessions.create",
          "request.get",
          "sessions.destroy",
        ]);
        expect(new Set(commands.map((c) => c.session)).size).toBe(1);
        expect(commands[1].maxTimeout).toBe(60_000);
        expect(mocks.close).toHaveBeenCalledOnce();
      },
      (input) => (input.cmd === "request.get" ? success() : { status: "ok" }),
    );
  });
  it.each([
    "https://127.0.0.1/x",
    "https://localhost/x",
    "http://example.com/x",
    "https://example.com:8443/x",
  ])("拒绝不安全入口 %s，不创建服务会话", async (url) => {
    await expect(
      captureWithFlareSolverr(
        { ...request, url },
        new AbortController().signal,
        settings,
      ),
    ).rejects.toThrow();
    expect(mocks.connect).not.toHaveBeenCalled();
  });
  it.each([
    "https://127.0.0.1/x",
    "https://other.example/x",
    "https://example.com/outside",
  ])("拒绝非法最终网址 %s，并清理会话", async (url) => {
    await fixture(
      async (_endpoint, commands) => {
        await expect(
          captureWithFlareSolverr(
            {
              ...request,
              scope: { origin: "https://example.com", directory: "/article/" },
            },
            new AbortController().signal,
            settings,
          ),
        ).rejects.toThrow();
        expect(commands.at(-1).cmd).toBe("sessions.destroy");
      },
      (input) =>
        input.cmd === "request.get" ? success(url) : { status: "ok" },
    );
  });
  it("上游称成功但仍为验证页时保持失败，不采集验证 HTML", async () => {
    await fixture(
      async () => {
        await expect(
          captureWithFlareSolverr(
            request,
            new AbortController().signal,
            settings,
          ),
        ).rejects.toThrow("未通过");
      },
      (input) =>
        input.cmd === "request.get"
          ? success(request.url, "<title>Just a moment...</title>")
          : { status: "ok" },
    );
  });
  it("取消正在等待的请求仍销毁会话并关闭连接", async () => {
    const controller = new AbortController();
    await fixture(
      async (_endpoint, commands) => {
        await expect(
          captureWithFlareSolverr(request, controller.signal, settings),
        ).rejects.toThrow("取消");
        expect(commands.at(-1).cmd).toBe("sessions.destroy");
        expect(mocks.close).toHaveBeenCalledOnce();
      },
      (input) => {
        if (input.cmd !== "request.get") return { status: "ok" };
        setTimeout(() => controller.abort(), 10);
        return undefined;
      },
    );
  });
  it("服务错误隐藏上游原文，仍清理会话", async () => {
    await fixture(
      async (_endpoint, commands) => {
        await expect(
          captureWithFlareSolverr(
            request,
            new AbortController().signal,
            settings,
          ),
        ).rejects.toThrow("未能完成请求");
        expect(commands.at(-1).cmd).toBe("sessions.destroy");
      },
      (input) =>
        input.cmd === "request.get"
          ? { status: "error", message: "cookie=secret" }
          : { status: "ok" },
    );
  });
  it("不允许将控制请求发送到任意内网或公网地址", async () => {
    await expect(
      flareSolverrCommand(
        "http://192.168.1.1/v1",
        {},
        new AbortController().signal,
      ),
    ).rejects.toThrow("回环");
  });
  it("SSH 配置不接受命令参数或口令，端口不静默修正", () => {
    expect(
      validateFlareSolverrSettings({
        ...settings,
        connection: "ssh",
        sshHost: "gatewaysentry",
      }).sshHost,
    ).toBe("gatewaysentry");
    for (const sshHost of [
      "-oProxyCommand=evil",
      "root@host",
      "host;echo",
      "host\narg",
    ])
      expect(() =>
        validateFlareSolverrSettings({
          ...settings,
          connection: "ssh",
          sshHost,
        }),
      ).toThrow();
    expect(() =>
      validateFlareSolverrSettings({ ...settings, port: 0 }),
    ).toThrow();
  });
});
