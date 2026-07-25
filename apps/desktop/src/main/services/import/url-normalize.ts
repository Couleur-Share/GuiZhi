/**
 * URL 规范化：用于导入去重的等价判定。
 * 规则：协议/主机小写、去默认端口、去 fragment、
 * 去常见跟踪参数（utm_* 等）、剩余查询参数按键排序、去尾斜杠。
 */

const TRACKING_PARAM_PATTERNS = [
  /^utm_/i,
  /^fbclid$/i,
  /^gclid$/i,
  /^spm$/i,
  /^share_source$/i,
  /^share_medium$/i,
  /^share_plat$/i,
  /^share_session_id$/i,
  /^share_tag$/i,
  /^from_source$/i,
  /^vd_source$/i,
  /^ref$/i,
  /^refer$/i,
  /^source$/i,
];

function isTrackingParam(name: string): boolean {
  return TRACKING_PARAM_PATTERNS.some((pattern) => pattern.test(name));
}

export function isHttpUrl(value: string): boolean {
  const trimmed = value.trim();
  if (!/^https?:\/\//i.test(trimmed)) {
    return false;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

export function normalizeUrl(rawUrl: string): string | null {
  const trimmed = rawUrl.trim();
  if (!isHttpUrl(trimmed)) {
    return null;
  }

  const parsed = new URL(trimmed);
  parsed.hash = "";
  parsed.hostname = parsed.hostname.toLowerCase();
  if (
    (parsed.protocol === "http:" && parsed.port === "80") ||
    (parsed.protocol === "https:" && parsed.port === "443")
  ) {
    parsed.port = "";
  }

  const keptParams = [...parsed.searchParams.entries()]
    .filter(([name]) => !isTrackingParam(name))
    .sort(([left], [right]) => left.localeCompare(right));
  parsed.search = "";
  for (const [name, value] of keptParams) {
    parsed.searchParams.append(name, value);
  }

  let normalized = parsed.toString();
  if (parsed.pathname !== "/" && normalized.endsWith("/") && !parsed.search) {
    normalized = normalized.slice(0, -1);
  }
  return normalized;
}
