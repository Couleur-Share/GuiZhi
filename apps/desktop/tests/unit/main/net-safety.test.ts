import { describe, expect, it } from "vitest";

import {
  isBlockedHostname,
  isForbiddenAIEndpointAddress,
  isPrivateIPv4,
  isPrivateIPv6,
} from "../../../src/main/services/net-safety";

describe("isPrivateIPv4", () => {
  it.each([
    "0.0.0.0",
    "10.1.2.3",
    "127.0.0.1",
    "169.254.169.254",
    "172.16.0.1",
    "172.31.255.255",
    "192.168.1.1",
    "100.64.0.1",
    "224.0.0.1",
    "255.255.255.255",
    "198.18.0.1",
    "192.0.2.1",
    "198.51.100.1",
    "203.0.113.1",
  ])("拦截 %s", (address) => {
    expect(isPrivateIPv4(address)).toBe(true);
  });

  it.each(["1.1.1.1", "8.8.8.8", "172.15.0.1", "172.32.0.1", "100.63.0.1"])(
    "放行 %s",
    (address) => {
      expect(isPrivateIPv4(address)).toBe(false);
    },
  );
});

describe("isPrivateIPv6", () => {
  it.each([
    "::",
    "::1",
    "fc00::1",
    "fd12:3456::1",
    "fe80::1",
    "2002::1",
    "2001:0:1::1",
    "2001:db8::1",
    "0100::1",
    "64:ff9b::1",
    "::ffff:127.0.0.1",
    "::ffff:192.168.0.1",
    // 十六进制写法的内嵌 IPv4：首个 hextet 是 0，所有掩码都不命中，
    // 不单独解码就会被当成公网地址放行
    "::7f00:1", // 127.0.0.1（IPv4-compatible，已废弃但栈仍然认）
    "::c0a8:1", // 192.168.0.1
    "::a9fe:a9fe", // 169.254.169.254 云元数据
    "::ffff:0:7f00:1", // 127.0.0.1（IPv4-translated）
    "::ffff:7f00:1", // 127.0.0.1（映射地址的十六进制写法）
  ])("拦截 %s", (address) => {
    expect(isPrivateIPv6(address)).toBe(true);
  });

  it.each(["2400:cb00::1", "2606:4700::1111", "::0808:0808"])(
    "放行 %s",
    (address) => {
      expect(isPrivateIPv6(address)).toBe(false);
    },
  );
});

describe("isBlockedHostname", () => {
  it.each([
    "localhost",
    "LOCALHOST",
    "foo.localhost",
    "localhost.localdomain",
    "box.localdomain",
  ])("拦截 %s", (hostname) => {
    expect(isBlockedHostname(hostname)).toBe(true);
  });

  it.each(["example.com", "localhostess.com", "api.openai.com"])(
    "放行 %s",
    (hostname) => {
      expect(isBlockedHostname(hostname)).toBe(false);
    },
  );
});

describe("isForbiddenAIEndpointAddress", () => {
  it.each([
    "169.254.169.254", // 云元数据
    "169.254.1.1",
    "224.0.0.1",
    "240.0.0.1",
    "0.0.0.0",
    "fe80::1",
    "ff02::1",
    "::ffff:169.254.169.254",
    // 兼容写法同样要解开，否则元数据服务可以用它绕过去
    "::a9fe:a9fe",
    "::ffff:0:a9fe:a9fe",
  ])("拦截 %s", (address) => {
    expect(isForbiddenAIEndpointAddress(address)).toBe(true);
  });

  it.each([
    // 本地与局域网推理服务是常规用法，必须放行
    "127.0.0.1",
    "::1",
    "192.168.1.100",
    "10.0.0.5",
    "172.16.3.4",
    "1.1.1.1",
    "2606:4700::1111",
  ])("放行 %s", (address) => {
    expect(isForbiddenAIEndpointAddress(address)).toBe(false);
  });
});
