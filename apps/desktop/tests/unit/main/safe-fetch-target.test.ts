import { describe, expect, it, vi } from "vitest";

// 只验字面 IP 的判定路径，域名会走真实 DNS
vi.mock("../../../src/main/services/network-proxy", () => ({
  getHttpRequestAgent: () => undefined,
  hasAnyProxyConfigured: () => false,
  fetchWithNetworkProxy: vi.fn(),
}));

import { assertSafeTarget } from "../../../src/main/services/import/safe-fetch";

const target = (url: string) => new URL(url);

describe("assertSafeTarget 走代理时", () => {
  it.each([
    "http://192.168.1.1/",
    "http://10.0.0.5:8080/",
    "http://169.254.169.254/latest/meta-data/",
    "http://127.0.0.1:7890/",
    "http://[::1]/",
    "http://[::ffff:127.0.0.1]/",
    "http://[::7f00:1]/",
  ])("仍然拦下字面内网地址 %s", async (url) => {
    // 「域名交给代理解析」不等于「字面 IP 也不查」——
    // URL 里写死的地址与 DNS 无关，配了代理之后照样会被原样转发出去
    await expect(assertSafeTarget(target(url), true)).rejects.toThrow(
      /不允许访问/,
    );
  });

  it.each(["http://example.com/", "https://api.openai.com/v1"])(
    "域名照常放行且不钉扎 %s",
    async (url) => {
      // 钉扎会绕过代理按域名分流的规则，所以这里必须返回 null
      await expect(assertSafeTarget(target(url), true)).resolves.toBeNull();
    },
  );

  it.each(["http://8.8.8.8/", "http://[2606:4700::1111]/"])(
    "公网字面 IP 放行 %s",
    async (url) => {
      await expect(assertSafeTarget(target(url), true)).resolves.toBeNull();
    },
  );

  it("协议白名单仍然生效", async () => {
    await expect(assertSafeTarget(target("file:///etc/passwd"), true)).rejects.toThrow(
      /不支持的协议/,
    );
  });

  it("localhost 类主机名不受代理影响", async () => {
    await expect(assertSafeTarget(target("http://localhost:8080/"), true)).rejects.toThrow(
      /不允许访问本地网络地址/,
    );
  });
});

describe("assertSafeTarget 不走代理时", () => {
  it("字面内网地址被拦下并返回钉扎地址给公网地址", async () => {
    await expect(assertSafeTarget(target("http://10.0.0.1/"), false)).rejects.toThrow();
    await expect(
      assertSafeTarget(target("http://1.1.1.1/"), false),
    ).resolves.toEqual({ address: "1.1.1.1", family: 4 });
  });

  it("没有代理配置时 198.18/15 不再当作 fake-ip 放行", async () => {
    // 该段是 Clash 的合成应答区间；没有代理却解析到它，就是真实内网地址
    await expect(
      assertSafeTarget(target("http://198.18.0.1/"), false),
    ).rejects.toThrow(/不允许访问内网地址/);
  });
});
