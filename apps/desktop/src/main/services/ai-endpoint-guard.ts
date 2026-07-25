/**
 * AI 端点访问收敛。
 *
 * `ai:httpRequest` 天然需要访问用户配置的任意地址，所以无法像网页抓取那样
 * 一律禁私网。剩下的风险是：渲染进程被注入后，用这条通道静默扫内网——
 * 响应体会完整回传，是个好用的探测器。
 *
 * 彻底的解法是把「连接测试」「拉取模型列表」搬进主进程，让 httpRequest
 * 只服务已保存的端点。那需要把整套 provider 适配也搬过去，改动很大。
 * 这里先做一层成本很低但有效的收敛：
 *
 * - 已保存在 ai-models.json 里的 host，以及回环地址（本地模型），不受限
 * - 其余 host 按「每 N 秒一个」限速——正常场景是用户手点测试连接，
 *   一次一个；扫描需要成百上千次尝试，被限速后不再可行
 *
 * 这是缓解不是根治，日志会记录每一次未知目标访问。
 */
import { coreAIConfigService } from "@guizhi/core";

/** 未知 host 的最小请求间隔 */
const UNKNOWN_HOST_INTERVAL_MS = 10_000;

let lastUnknownHostAt = 0;
/** 已放行过的未知 host：同一个端点反复测试不该每次都等 */
const grantedUnknownHosts = new Set<string>();

function normalizeHost(rawUrl: string): string | null {
  try {
    const parsed = new URL(rawUrl);
    return `${parsed.protocol}//${parsed.host}`.toLowerCase();
  } catch {
    return null;
  }
}

/** 配置里出现过的 host（provider 与 model 各自的 apiUrl） */
export function collectConfiguredHosts(): Set<string> {
  const hosts = new Set<string>();
  try {
    const config = coreAIConfigService.read();
    for (const provider of config.providers) {
      const host = normalizeHost(provider.apiUrl ?? "");
      if (host) hosts.add(host);
    }
    for (const model of config.models) {
      const host = normalizeHost(model.apiUrl ?? "");
      if (host) hosts.add(host);
    }
  } catch (error) {
    console.warn("[ai] 读取已配置端点失败:", error);
  }
  return hosts;
}

export function isLoopbackHost(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, "");
  return (
    host === "localhost" ||
    host === "::1" ||
    host === "0:0:0:0:0:0:0:1" ||
    /^127\./.test(host)
  );
}

export interface EndpointGateDecision {
  allowed: boolean;
  reason?: string;
}

/**
 * 判定是否放行。纯函数部分抽出来便于测试：
 * `now` 与 `configuredHosts` 由调用方注入。
 */
export function evaluateEndpointAccess(params: {
  url: string;
  now: number;
  configuredHosts: Set<string>;
  lastUnknownAt: number;
  grantedHosts: Set<string>;
}): EndpointGateDecision & { host: string | null; isKnown: boolean } {
  const host = normalizeHost(params.url);
  if (!host) {
    return { allowed: false, reason: "无效的接口地址", host: null, isKnown: false };
  }

  let hostname = "";
  try {
    hostname = new URL(params.url).hostname;
  } catch {
    hostname = "";
  }

  const isKnown =
    params.configuredHosts.has(host) ||
    params.grantedHosts.has(host) ||
    isLoopbackHost(hostname);
  if (isKnown) {
    return { allowed: true, host, isKnown: true };
  }

  const elapsed = params.now - params.lastUnknownAt;
  if (elapsed < UNKNOWN_HOST_INTERVAL_MS) {
    const waitSeconds = Math.ceil(
      (UNKNOWN_HOST_INTERVAL_MS - elapsed) / 1000,
    );
    return {
      allowed: false,
      reason: `未配置的接口地址请求过于频繁，请 ${waitSeconds} 秒后重试`,
      host,
      isKnown: false,
    };
  }
  return { allowed: true, host, isKnown: false };
}

/** 请求前的准入检查；不通过时抛出可读错误 */
export function assertEndpointAllowed(url: string): void {
  const decision = evaluateEndpointAccess({
    url,
    now: Date.now(),
    configuredHosts: collectConfiguredHosts(),
    lastUnknownAt: lastUnknownHostAt,
    grantedHosts: grantedUnknownHosts,
  });

  if (!decision.allowed) {
    throw new Error(decision.reason ?? "不允许访问该地址");
  }
  if (!decision.isKnown && decision.host) {
    lastUnknownHostAt = Date.now();
    grantedUnknownHosts.add(decision.host);
    console.log(`[ai] 放行未配置端点（限速）: ${decision.host}`);
  }
}

/** 仅供测试重置 */
export function resetEndpointGate(): void {
  lastUnknownHostAt = 0;
  grantedUnknownHosts.clear();
}
