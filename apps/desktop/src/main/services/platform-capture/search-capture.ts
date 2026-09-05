import type { PlatformCapturePlatform } from "@guizhi/shared/types";
import { isAllowedPlatformUrl } from "@guizhi/shared/utils/platform-capture";

export interface SearchCaptureScope { keyword: string }
export interface CaptureRequest { url: string; postData?: string }

function normalizedKeyword(value: string): string {
  return value.normalize("NFKC").trim().replace(/\s+/g, " ").toLowerCase();
}

/** 搜索页同时请求推荐流、广告与热词；只有搜索接口且关键词一致才属于本次结果。 */
export function matchesSearchRequest(platform: PlatformCapturePlatform, request: CaptureRequest, scope: SearchCaptureScope): boolean {
  if (!isAllowedPlatformUrl(platform, request.url)) return false;
  const url = new URL(request.url);
  const isSearch = platform === "xiaohongshu"
    ? /^\/api\/sns\/web\/v\d+\/search\/notes\/?$/i.test(url.pathname)
    : platform === "douyin" && /^\/aweme\/v\d+\/web\/(?:general\/search\/single|search\/item)\/?$/i.test(url.pathname);
  if (!isSearch) return false;
  let keyword: unknown = url.searchParams.get("keyword");
  if (!keyword && request.postData) {
    try { keyword = (JSON.parse(request.postData) as Record<string, unknown>)?.keyword; }
    catch { keyword = new URLSearchParams(request.postData).get("keyword"); }
  }
  return typeof keyword === "string" && normalizedKeyword(keyword) === normalizedKeyword(scope.keyword);
}

/** 只读取搜索结果数组，绝不递归遍历推荐区、作者信息里的作品或广告附件。 */
export function searchResponseRows(platform: PlatformCapturePlatform, value: unknown): unknown[] | null {
  if (!value || typeof value !== "object") return null;
  const response = value as Record<string, unknown>;
  const code = response.status_code ?? response.code;
  if ((code != null && Number(code) !== 0) || response.success === false) return null;
  const data = response.data as Record<string, unknown> | undefined;
  const rows = platform === "xiaohongshu" ? data?.items : response.aweme_list ?? response.data;
  if (!Array.isArray(rows)) return null;
  const isContent = (row: unknown): row is Record<string, unknown> => Boolean(row && typeof row === "object" && !(row as Record<string, unknown>).is_ads && !(row as Record<string, unknown>).ad_info);
  return rows.filter(isContent)
    .map((row) => platform === "douyin" ? row.aweme_info ?? row : row)
    .filter(isContent);
}
