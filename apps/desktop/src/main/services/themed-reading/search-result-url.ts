import { isReadingPublicUrl } from "@guizhi/shared/utils/reading-reconstruction";
import { isBlockedHostname, isPrivateAddress } from "../net-safety";

/** 先过滤显式内网地址；正文抓取仍由 net-safety 解析 DNS 并校验连接。 */
export function isPublicSearchUrl(value: unknown): value is string {
  if (!isReadingPublicUrl(value)) return false;
  const hostname = new URL(value).hostname.replace(/^\[|\]$/g, "");
  return !isBlockedHostname(hostname) && !isPrivateAddress(hostname);
}
