// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpProxyAgent } from "http-proxy-agent";
import { HttpsProxyAgent } from "https-proxy-agent";
import {
  WEB_CONNECTION_REUSE_ENV,
  WebConnectionPool,
} from "../../../src/main/services/web-capture/web-connection-pool";

afterEach(() => {
  delete process.env[WEB_CONNECTION_REUSE_ENV];
});

describe("单次采集连接范围", () => {
  it("同一代理与目标协议共用 agent，协议或代理不同则各自独立", () => {
    const pool = new WebConnectionPool();
    const a = pool.agentFor("http://127.0.0.1:7900", true);
    expect(pool.agentFor("http://127.0.0.1:7900", true)).toBe(a);
    expect(pool.agentFor("http://127.0.0.1:7900", false)).not.toBe(a);
    expect(pool.agentFor("http://127.0.0.1:7901", true)).not.toBe(a);
    pool.close();
  });

  it("https 目标走 CONNECT 隧道 agent，http 目标走转发 agent", () => {
    const pool = new WebConnectionPool();
    expect(pool.agentFor("http://127.0.0.1:7900", true)).toBeInstanceOf(
      HttpsProxyAgent,
    );
    expect(pool.agentFor("http://127.0.0.1:7900", false)).toBeInstanceOf(
      HttpProxyAgent,
    );
    pool.close();
  });

  it("agent 开启 keepAlive，并发上限与网络出口槽位一致", () => {
    const pool = new WebConnectionPool();
    for (const secure of [true, false]) {
      const agent = pool.agentFor("http://127.0.0.1:7900", secure);
      expect(agent.keepAlive).toBe(true);
      expect(agent.maxSockets).toBe(8);
      expect(agent.maxFreeSockets).toBe(4);
    }
    pool.close();
  });

  it("close 销毁全部 agent 且幂等；关闭后不再发放 agent", () => {
    const pool = new WebConnectionPool();
    const secure = pool.agentFor("http://127.0.0.1:7900", true);
    const plain = pool.agentFor("http://127.0.0.1:7900", false);
    const destroySecure = vi.spyOn(secure, "destroy");
    const destroyPlain = vi.spyOn(plain, "destroy");
    expect(pool.closed).toBe(false);
    pool.close();
    pool.close();
    expect(pool.closed).toBe(true);
    expect(destroySecure).toHaveBeenCalledTimes(1);
    expect(destroyPlain).toHaveBeenCalledTimes(1);
    expect(() => pool.agentFor("http://127.0.0.1:7900", true)).toThrow(
      "网页连接范围已关闭",
    );
  });

  it("环境变量 GUIZHI_WEB_CONNECTION_REUSE=0 关闭复用，其它情况默认开启", () => {
    expect(new WebConnectionPool().enabled).toBe(true);
    process.env[WEB_CONNECTION_REUSE_ENV] = "1";
    expect(new WebConnectionPool().enabled).toBe(true);
    process.env[WEB_CONNECTION_REUSE_ENV] = "0";
    expect(new WebConnectionPool().enabled).toBe(false);
  });
});
