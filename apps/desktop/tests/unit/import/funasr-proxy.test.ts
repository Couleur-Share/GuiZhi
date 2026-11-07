import { describe, expect, it } from "vitest";
import {
  FUNASR_SOCKS_PROXY_HINT,
  explainFunasrProxyFailure,
} from "../../../src/main/services/media/funasr-proxy";

describe("本地转写引擎的 SOCKS 代理提示", () => {
  it("pip 或 requests 报缺少 SOCKS 支持时给出可操作说明，并保留原始原因", () => {
    const raw =
      "python.exe 退出码 2: pip._vendor.requests.exceptions.InvalidSchema: Missing dependencies for SOCKS support.";
    const message = explainFunasrProxyFailure(raw);
    expect(message.startsWith(FUNASR_SOCKS_PROXY_HINT)).toBe(true);
    expect(message).toContain(raw);
  });

  it("其他失败原样返回", () => {
    const raw = "python.exe 退出码 1: No matching distribution found for funasr";
    expect(explainFunasrProxyFailure(raw)).toBe(raw);
  });
});
