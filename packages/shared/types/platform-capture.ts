/** 登录态平台采集的跨进程契约。 */

export const PLATFORM_CAPTURE_PLATFORMS = [
  "xiaohongshu",
  "douyin",
  "linuxdo",
] as const;

export type PlatformCapturePlatform =
  (typeof PLATFORM_CAPTURE_PLATFORMS)[number];

export const IMPORT_CAPTURE_STRATEGIES = ["standard", "authenticated"] as const;

export type ImportCaptureStrategy =
  (typeof IMPORT_CAPTURE_STRATEGIES)[number];

export const COMMENT_LIMITS = [0, 10, 20, 50] as const;

export type CommentLimit = (typeof COMMENT_LIMITS)[number];
export type PlatformCaptureBrowser = "embedded";

export const PLATFORM_CAPTURE_ERROR_CODES = [
  "browser_unavailable",
  "login_required",
  "login_timeout",
  "browser_closed",
  "platform_changed",
  "navigation_timeout",
  "network_error",
  "verification_required",
  "canceled",
] as const;

export type PlatformCaptureErrorCode =
  (typeof PLATFORM_CAPTURE_ERROR_CODES)[number];

export interface PlatformSessionStatus {
  platform: PlatformCapturePlatform;
  browser: PlatformCaptureBrowser | null;
  browserVersion?: string;
  available: boolean;
  loggedIn: boolean;
  busy: boolean;
  errorCode?: PlatformCaptureErrorCode;
}

export type PlatformDiscoveryMediaType = "image" | "video" | "article";

export type DiscoveryDateConfidence = "high" | "medium" | "low";

export interface PlatformDiscoveryEngagement {
  views?: number;
  likes?: number;
  comments?: number;
  shares?: number;
  collects?: number;
  favorites?: number;
  danmaku?: number;
}

export interface PlatformDiscoveryItem {
  platform: PlatformCapturePlatform;
  externalId: string;
  url: string;
  title: string;
  author: string;
  authorId?: string;
  coverUrl?: string;
  mediaType: PlatformDiscoveryMediaType;
  publishedAt?: number;
  snippet?: string;
  engagement?: PlatformDiscoveryEngagement;
  dateConfidence?: DiscoveryDateConfidence;
  discoveryMethod?: string;
  importedItemId?: string;
}

export interface PlatformDiscoveryPage {
  items: PlatformDiscoveryItem[];
  cursor: string | null;
  hasMore: boolean;
}

export interface DiscoverCreatorInput {
  platform: PlatformCapturePlatform;
  url: string;
  cursor?: string | null;
  limit?: number;
}

export interface SearchPlatformInput {
  platform: PlatformCapturePlatform;
  keyword: string;
  cursor?: string | null;
  limit?: number;
}

export interface SourceComment {
  id: string;
  itemId: string;
  platform: PlatformCapturePlatform;
  externalId: string;
  authorName: string;
  content: string;
  likeCount: number;
  publishedAt: number | null;
  capturedAt: number;
}

export interface CaptureCommentsInput {
  itemId: string;
  limit: Exclude<CommentLimit, 0>;
}

export function isPlatformCapturePlatform(
  value: unknown,
): value is PlatformCapturePlatform {
  return PLATFORM_CAPTURE_PLATFORMS.includes(value as PlatformCapturePlatform);
}

export function isImportCaptureStrategy(
  value: unknown,
): value is ImportCaptureStrategy {
  return IMPORT_CAPTURE_STRATEGIES.includes(value as ImportCaptureStrategy);
}

export function isCommentLimit(value: unknown): value is CommentLimit {
  return COMMENT_LIMITS.includes(value as CommentLimit);
}
