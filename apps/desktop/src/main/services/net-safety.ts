/**
 * Network-level SSRF protection utilities.
 *
 * DNS 解析 + 私网地址检测，防止渲染进程发起的远程抓取
 * （图片下载、网页采集）被用于访问内网资源。
 */
import * as dns from "dns/promises";
import * as nodeNet from "net";

export interface ResolvedAddress {
  address: string;
  family: 4 | 6;
}

export interface ResolvePublicAddressOptions {
  allowPrivateNetwork?: boolean;
  /**
   * Allows synthetic 198.18/15 DNS answers used by a configured local proxy.
   * Real private addresses remain blocked unless allowPrivateNetwork is set.
   */
  allowProxyCompatibilityAddress?: boolean;
}

export function isBlockedHostname(hostname: string): boolean {
  const normalized = hostname.toLowerCase();
  return (
    normalized === "localhost" ||
    normalized.endsWith(".localhost") ||
    normalized === "localhost.localdomain" ||
    normalized.endsWith(".localdomain")
  );
}

export function isPrivateIPv4(address: string): boolean {
  const parts = address.split(".").map((part) => Number(part));
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part))) {
    return false;
  }

  const [a, b] = parts;
  return (
    a === 0 ||
    a === 10 ||
    a === 127 ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168) ||
    // CGNAT (Carrier-grade NAT)
    (a === 100 && b >= 64 && b <= 127) ||
    // Multicast
    (a >= 224 && a <= 239) ||
    // Reserved for future use
    a >= 240 ||
    // Benchmark testing
    (a === 198 && (b === 18 || b === 19)) ||
    // Documentation ranges (TEST-NET-1, TEST-NET-2, TEST-NET-3)
    (a === 192 && b === 0 && parts[2] === 2) ||
    (a === 198 && b === 51 && parts[2] === 100) ||
    (a === 203 && b === 0 && parts[2] === 113)
  );
}

export function isPrivateIPv6(address: string): boolean {
  const normalized = address.toLowerCase().split("%")[0];
  if (normalized === "::" || normalized === "::1") {
    return true;
  }

  // 点分写法的映射地址（::ffff:127.0.0.1）。不是合法 IPv4 时不能直接返回
  // false——Node 会把它规范化成十六进制的 ::ffff:7f00:1，那种形态交给下面处理
  if (normalized.startsWith("::ffff:")) {
    const mappedAddress = normalized.slice("::ffff:".length);
    if (nodeNet.isIP(mappedAddress) === 4) {
      return isPrivateIPv4(mappedAddress);
    }
  }

  // 十六进制写法的内嵌 IPv4：`::7f00:1` 展开就是 127.0.0.1，
  // 而它的首个 hextet 是 0，下面所有掩码都不命中，会被当成公网地址放行
  const decodedIPv4 = decodeEmbeddedIPv4(normalized, ALL_EMBEDDED_PREFIXES);
  if (decodedIPv4) {
    return isPrivateIPv4(decodedIPv4);
  }

  // Expand :: into the correct number of zero groups to get all 8 hextets
  const halves = normalized.split("::");
  let segments: string[];
  if (halves.length === 2) {
    const left = halves[0] ? halves[0].split(":") : [];
    const right = halves[1] ? halves[1].split(":") : [];
    const missing = 8 - left.length - right.length;
    segments = [...left, ...Array(missing).fill("0"), ...right];
  } else {
    segments = normalized.split(":");
  }

  if (segments.length < 2) {
    return false;
  }

  const firstHextet = Number.parseInt(segments[0], 16);
  if (Number.isNaN(firstHextet)) {
    return false;
  }
  const secondHextet = Number.parseInt(segments[1], 16) || 0;

  return (
    // ULA (Unique Local Address)
    (firstHextet & 0xfe00) === 0xfc00 ||
    // IPv6 组播不是可抓取的公网单播目标。
    (firstHextet & 0xff00) === 0xff00 ||
    // Link-local
    (firstHextet & 0xffc0) === 0xfe80 ||
    // 6to4 relay
    firstHextet === 0x2002 ||
    // Teredo tunneling
    (firstHextet === 0x2001 && secondHextet === 0x0000) ||
    // Documentation
    (firstHextet === 0x2001 && secondHextet === 0x0db8) ||
    // Discard prefix
    firstHextet === 0x0100 ||
    // NAT64
    (firstHextet === 0x0064 && secondHextet === 0xff9b)
  );
}

/**
 * AI 接口地址的禁止目标。
 *
 * AI 端点必须允许回环与局域网——本地 Ollama / LM Studio、局域网推理服务
 * 都是常规用法，套用抓取那套「一律禁私网」会直接废掉这些场景。
 * 这里只挡没有任何合法 AI 用途、却是经典攻击目标的地址段：
 * link-local（含云元数据 169.254.169.254）、组播、保留段。
 */
export function isForbiddenAIEndpointAddress(address: string): boolean {
  const family = nodeNet.isIP(address);
  if (family === 4) {
    const parts = address.split(".").map(Number);
    const [a, b] = parts;
    return (
      (a === 169 && b === 254) ||
      (a >= 224 && a <= 239) ||
      a >= 240 ||
      a === 0
    );
  }
  if (family === 6) {
    const normalized = address.toLowerCase().split("%")[0];
    if (normalized.startsWith("::ffff:")) {
      const mapped = normalized.slice("::ffff:".length);
      if (nodeNet.isIP(mapped) === 4) {
        return isForbiddenAIEndpointAddress(mapped);
      }
    }
    // 兼容写法也要解：`::a9fe:a9fe` 展开就是 169.254.169.254（云元数据服务）
    const decoded = decodeEmbeddedIPv4(normalized, ALL_EMBEDDED_PREFIXES);
    if (decoded) {
      return isForbiddenAIEndpointAddress(decoded);
    }
    const firstHextet = Number.parseInt(normalized.split(":")[0], 16);
    if (Number.isNaN(firstHextet)) {
      return false;
    }
    return (
      // Link-local fe80::/10
      (firstHextet & 0xffc0) === 0xfe80 ||
      // Multicast ff00::/8
      (firstHextet & 0xff00) === 0xff00
    );
  }
  return false;
}

export function isPrivateAddress(address: string): boolean {
  const family = nodeNet.isIP(address);
  if (family === 4) {
    return isPrivateIPv4(address);
  }
  if (family === 6) {
    return isPrivateIPv6(address);
  }
  return false;
}

function expandIPv6Segments(address: string): string[] | null {
  const normalized = address.toLowerCase().split("%")[0];
  if (nodeNet.isIP(normalized) !== 6) {
    return null;
  }

  const halves = normalized.split("::");
  let segments: string[];
  if (halves.length === 2) {
    const left = halves[0] ? halves[0].split(":") : [];
    const right = halves[1] ? halves[1].split(":") : [];
    const missing = 8 - left.length - right.length;
    segments = [...left, ...Array(missing).fill("0"), ...right];
  } else {
    segments = normalized.split(":");
  }

  if (segments.length !== 8) {
    return null;
  }

  return segments.map((segment) => segment.padStart(4, "0"));
}

/** ::ffff:a.b.c.d —— IPv4 映射地址 */
const MAPPED_PREFIX = ["0000", "0000", "0000", "0000", "0000", "ffff"];
/** ::ffff:0:a.b.c.d —— IPv4 翻译地址（RFC 2765） */
const TRANSLATED_PREFIX = ["0000", "0000", "0000", "0000", "ffff", "0000"];
/** ::a.b.c.d —— IPv4 兼容地址，已废弃但栈仍然认，攻击面照旧 */
const COMPATIBLE_PREFIX = ["0000", "0000", "0000", "0000", "0000", "0000"];

const TRUSTED_COMPATIBILITY_PREFIXES = [MAPPED_PREFIX, TRANSLATED_PREFIX];
const ALL_EMBEDDED_PREFIXES = [
  MAPPED_PREFIX,
  TRANSLATED_PREFIX,
  COMPATIBLE_PREFIX,
];

/** 解出 IPv6 里内嵌的 IPv4；前缀不在给定集合内时返回 null */
function decodeEmbeddedIPv4(
  address: string,
  prefixes: string[][],
): string | null {
  const segments = expandIPv6Segments(address);
  if (!segments) {
    return null;
  }

  const prefix = segments.slice(0, 6);
  const matched = prefixes.find((candidate) =>
    prefix.every((segment, index) => segment === candidate[index]),
  );
  if (!matched) {
    return null;
  }

  const high = Number.parseInt(segments[6], 16);
  const low = Number.parseInt(segments[7], 16);
  if (Number.isNaN(high) || Number.isNaN(low)) {
    return null;
  }

  // `::` 与 `::1` 是未指定地址和回环，不是「内嵌 0.0.0.0 / 0.0.0.1 的 IPv4」。
  // 不排除的话回环会被解成 0.0.0.1，而那个地址在 AI 端点判定里是禁止的——
  // 本地 Ollama 就连不上了。全零前缀下 high 为 0 的一律不算内嵌 IPv4。
  if (matched === COMPATIBLE_PREFIX && high === 0) {
    return null;
  }

  return `${high >> 8}.${high & 0xff}.${low >> 8}.${low & 0xff}`;
}

/** 代理 fake-ip 的判定只认映射/翻译两种写法（真代理不会回废弃的兼容形式） */
function decodeTrustedCompatibilityIPv6(address: string): string | null {
  return decodeEmbeddedIPv4(address, TRUSTED_COMPATIBILITY_PREFIXES);
}

function isProxyCompatibilityAddress(address: string): boolean {
  if (address.startsWith("198.18.") || address.startsWith("198.19.")) {
    return true;
  }

  const decodedIPv4 = decodeTrustedCompatibilityIPv6(address);
  return decodedIPv4 !== null && isProxyCompatibilityAddress(decodedIPv4);
}

export async function resolvePublicAddress(
  hostname: string,
  options: ResolvePublicAddressOptions = {},
): Promise<ResolvedAddress> {
  if (isBlockedHostname(hostname)) {
    throw new Error("Access to local network addresses is not allowed");
  }

  if (nodeNet.isIP(hostname)) {
    if (isPrivateAddress(hostname)) {
      if (options.allowPrivateNetwork) {
        return { address: hostname, family: nodeNet.isIP(hostname) as 4 | 6 };
      }
      if (
        isProxyCompatibilityAddress(hostname) &&
        options.allowProxyCompatibilityAddress
      ) {
        return { address: hostname, family: nodeNet.isIP(hostname) as 4 | 6 };
      }
      throw new Error("Access to internal network addresses is not allowed");
    }
    return { address: hostname, family: nodeNet.isIP(hostname) as 4 | 6 };
  }

  const addresses = await dns.lookup(hostname, { all: true, verbatim: true });
  if (addresses.length === 0) {
    throw new Error("Failed to resolve remote host");
  }

  const trustedCompatibilityAddresses = addresses.filter((entry) =>
    isProxyCompatibilityAddress(entry.address),
  );
  if (
    trustedCompatibilityAddresses.length > 0 &&
    trustedCompatibilityAddresses.length === addresses.length &&
    options.allowProxyCompatibilityAddress
  ) {
    const firstTrustedAddress = trustedCompatibilityAddresses[0];
    return {
      address: firstTrustedAddress.address,
      family: firstTrustedAddress.family === 6 ? 6 : 4,
    };
  }

  if (
    !options.allowPrivateNetwork &&
    addresses.some((entry) => isPrivateAddress(entry.address))
  ) {
    throw new Error("Access to internal network addresses is not allowed");
  }

  const firstAddress = addresses[0];
  return {
    address: firstAddress.address,
    family: firstAddress.family === 6 ? 6 : 4,
  };
}
