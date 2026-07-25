import { describe, expect, it } from "vitest";
import {
  evaluateEndpointAccess,
  isLoopbackHost,
} from "../../../src/main/services/ai-endpoint-guard";

const CONFIGURED = new Set(["https://api.openai.com"]);
const NOW = 1_000_000;

function evaluate(
  url: string,
  overrides?: { lastUnknownAt?: number; granted?: Set<string> },
) {
  return evaluateEndpointAccess({
    url,
    now: NOW,
    configuredHosts: CONFIGURED,
    lastUnknownAt: overrides?.lastUnknownAt ?? 0,
    grantedHosts: overrides?.granted ?? new Set(),
  });
}

describe("isLoopbackHost", () => {
  it.each(["localhost", "LOCALHOST", "127.0.0.1", "127.1.2.3", "::1", "[::1]"])(
    "识别 %s 为回环",
    (host) => {
      expect(isLoopbackHost(host)).toBe(true);
    },
  );

  it.each(["example.com", "192.168.1.1", "10.0.0.1"])(
    "%s 不是回环",
    (host) => {
      expect(isLoopbackHost(host)).toBe(false);
    },
  );
});

describe("evaluateEndpointAccess", () => {
  it("已配置的 host 无条件放行", () => {
    const decision = evaluate("https://api.openai.com/v1/chat/completions");
    expect(decision).toMatchObject({ allowed: true, isKnown: true });
  });

  it("本地模型（回环）无条件放行", () => {
    // Ollama / LM Studio 是常规用法，不能被限速影响
    expect(evaluate("http://localhost:11434/v1/chat/completions")).toMatchObject(
      { allowed: true, isKnown: true },
    );
    expect(evaluate("http://127.0.0.1:1234/v1/models")).toMatchObject({
      allowed: true,
      isKnown: true,
    });
  });

  it("未配置的 host 首次放行——用户手点测试连接的正常场景", () => {
    const decision = evaluate("https://new-gateway.example.com/v1");
    expect(decision).toMatchObject({ allowed: true, isKnown: false });
  });

  it("未配置的 host 短时间内连续请求被限速", () => {
    // 扫内网需要成百上千次尝试，限速后不再可行
    const decision = evaluate("http://192.168.1.50:8080/v1", {
      lastUnknownAt: NOW - 1_000,
    });
    expect(decision.allowed).toBe(false);
    expect(decision.reason).toContain("秒后重试");
  });

  it("超过间隔后恢复放行", () => {
    const decision = evaluate("http://192.168.1.50:8080/v1", {
      lastUnknownAt: NOW - 20_000,
    });
    expect(decision.allowed).toBe(true);
  });

  it("已放行过的 host 不再受限速影响", () => {
    // 同一个新端点反复测试连接不该每次都等
    const decision = evaluate("https://new-gateway.example.com/v1", {
      lastUnknownAt: NOW - 100,
      granted: new Set(["https://new-gateway.example.com"]),
    });
    expect(decision).toMatchObject({ allowed: true, isKnown: true });
  });

  it("端口不同视为不同 host", () => {
    const decision = evaluate("https://api.openai.com:8443/v1", {
      lastUnknownAt: NOW - 100,
    });
    expect(decision.allowed).toBe(false);
  });

  it("非法 URL 直接拒绝", () => {
    expect(evaluate("not a url").allowed).toBe(false);
  });
});
