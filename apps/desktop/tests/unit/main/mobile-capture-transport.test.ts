import { EventEmitter } from "node:events";
import { afterEach, describe, expect, it, vi } from "vitest";
import { captureRequest } from "../../../src/main/services/mobile-capture/transport";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("node:https", () => ({ default: { request: mocks.request } }));
vi.mock("../../../src/main/services/net-safety", () => ({ resolvePublicAddress: async () => ({ address: "93.184.215.14", family: 4 }) }));
vi.mock("../../../src/main/services/network-proxy", () => ({ getHttpRequestAgent: () => undefined, hasAnyProxyConfigured: () => false }));
afterEach(() => { vi.clearAllMocks(); });
describe("手机收集 HTTP 传输", () => {
  it("DELETE 与中文 JSON 都明确发送字节长度，避免服务端收到空请求体", async () => {
    let payload = "";
    mocks.request.mockImplementation((_url, _options, onResponse) => {
      const request = new EventEmitter() as EventEmitter & { end: (body: string) => void };
      request.end = body => {
        payload = body;
        const response = Object.assign(new EventEmitter(), { statusCode: 200, headers: {} });
        onResponse(response);
        response.emit("data", Buffer.from('{"success":true}'));
        response.emit("end");
      };
      return request;
    });
    const body = { reason: "停用测试" };
    await expect(captureRequest("https://capture.example.com", "/v1/mailbox", "synthetic", body, "DELETE", new AbortController().signal)).resolves.toEqual({ success: true });
    expect(payload).toBe(JSON.stringify(body));
    expect(mocks.request.mock.calls[0][1]).toMatchObject({ method: "DELETE", headers: { "Content-Length": Buffer.byteLength(payload) } });
  });
});
