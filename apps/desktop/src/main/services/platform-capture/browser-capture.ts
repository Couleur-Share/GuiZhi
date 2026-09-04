import fs from "fs";
import path from "path";
import type { BrowserWindow } from "electron";
import type {
  DiscoverCreatorInput,
  NetworkProxySettings,
  PlatformCaptureErrorCode,
  PlatformCapturePlatform,
  PlatformDiscoveryItem,
  PlatformDiscoveryPage,
  PlatformSessionStatus,
  SearchPlatformInput,
} from "@guizhi/shared/types";
import {
  isAllowedPlatformUrl,
  platformLoginUrl,
  platformSearchUrl,
} from "@guizhi/shared/utils/platform-capture";
import { normalizeNetworkProxySettings } from "@guizhi/shared/utils/network-proxy";
import { getUserDataPath } from "../../runtime-paths";
import {
  clearElectronCaptureSessions,
  createElectronCaptureContext,
  getElectronCaptureSession,
  type ElectronCaptureContext,
  type ElectronCapturePage,
} from "./electron-capture-runtime";
import {
  LOGIN_COOKIE_NAMES,
  LOGIN_FLOW_DOMAINS,
  RESOURCE_DOMAINS,
} from "./browser-capture-domains";

const LOGIN_TIMEOUT_MS = 5 * 60_000;
const LOGIN_PANEL_AUTO_OPEN_TIMEOUT_MS = 10_000;
const LOGIN_PANEL_CLICK_TIMEOUT_MS = 3_000;
const LOGIN_PANEL_CONFIRM_TIMEOUT_MS = 5_000;
const LOGIN_PANEL_POLL_INTERVAL_MS = 250;
const DOUYIN_FAST_LOGIN_READY_TIMEOUT_MS = 8_000;
const DOUYIN_FAST_LOGIN_URL =
  "https://creator.douyin.com/creator-micro/interactive/comment";
const NAVIGATION_TIMEOUT_MS = 45_000;
const DISCOVERY_TIMEOUT_MS = 90_000;
const PAGE_SIZE = 20;
const MAX_DISCOVERY_ITEMS = 100;
const SESSION_STATUS_VERSION = 3;

function isAllowedSecureProtocol(protocol: string): boolean {
  return protocol === "https:" || protocol === "wss:";
}

function isOfficialLoginFlowUrl(
  platform: PlatformCapturePlatform,
  value: string,
): boolean {
  if (isAllowedPlatformUrl(platform, value)) return true;
  try {
    const url = new URL(value);
    if (!isAllowedSecureProtocol(url.protocol)) return false;
    const host = url.hostname.toLowerCase();
    return LOGIN_FLOW_DOMAINS[platform].some(
      (domain) => host === domain || host.endsWith(`.${domain}`),
    );
  } catch {
    return false;
  }
}

export function isAllowedBrowserResourceUrl(
  platform: PlatformCapturePlatform,
  value: string,
): boolean {
  if (/^(?:about:blank|data:|blob:)/i.test(value)) return true;
  if (platform === "linuxdo") {
    try {
      const url = new URL(value);
      return isAllowedSecureProtocol(url.protocol);
    } catch {
      return false;
    }
  }
  if (isOfficialLoginFlowUrl(platform, value)) return true;
  try {
    const url = new URL(value);
    if (!isAllowedSecureProtocol(url.protocol)) return false;
    const host = url.hostname.toLowerCase();
    return RESOURCE_DOMAINS[platform].some(
      (domain) => host === domain || host.endsWith(`.${domain}`),
    );
  } catch {
    return false;
  }
}

export function shouldBlockLoginPageRequest(
  platform: PlatformCapturePlatform,
  value: string,
  resourceType: string,
): boolean {
  // 登录不需要播放信息流音视频；二维码与验证码图片仍正常加载。
  if (resourceType === "media") return true;
  if (platform !== "douyin") return false;
  try {
    const url = new URL(value);
    const pathname = url.pathname;
    const host = url.hostname.toLowerCase();
    // 推荐流封面是登录页最重的一批无关资源；登录二维码使用 passport
    // 资源和同源接口，不落在 douyinpic.com。
    if (
      resourceType === "image" &&
      (host === "douyinpic.com" || host.endsWith(".douyinpic.com"))
    )
      return true;
    return /^\/aweme\/v1\/web\/(?:tab|follow)\/feed\//i.test(pathname);
  } catch {
    return false;
  }
}

type OperationKind = "login" | "capture" | "discovery" | "comments";

interface ProfileState {
  version?: number;
  xiaohongshu?: boolean;
  douyin?: boolean;
  linuxdo?: boolean;
}

type LoginCookieSnapshot = Readonly<Record<string, string>>;

export function didLoginCookieSnapshotChange(
  before: LoginCookieSnapshot,
  after: LoginCookieSnapshot,
): boolean {
  return Object.entries(after).some(
    ([name, value]) => value.length > 0 && before[name] !== value,
  );
}

interface CapturedPage {
  html: string;
  finalUrl: string;
  jsonPayloads: unknown[];
}

export interface CapturedComment {
  externalId: string;
  authorName: string;
  content: string;
  likeCount: number;
  publishedAt: number | null;
}

export class PlatformCaptureError extends Error {
  constructor(
    readonly code: PlatformCaptureErrorCode,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(`[${code}] ${message}`, options);
    this.name = "PlatformCaptureError";
  }
}

export interface BrowserCaptureServiceOptions {
  userDataPath?: string;
  getNetworkProxy?: () => NetworkProxySettings | null | undefined;
}

function cleanText(value: unknown, max = 5000): string {
  return typeof value === "string"
    ? value
        .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, "")
        .trim()
        .slice(0, max)
    : "";
}

function numeric(value: unknown): number {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function optionalMetric(value: unknown): number | undefined {
  if (value === null || value === undefined || value === "") return undefined;
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? Math.floor(parsed) : undefined;
}

function indicatesLoginRequired(value: unknown, depth = 0): boolean {
  if (depth > 8 || value == null) return false;
  if (Array.isArray(value))
    return value.some((entry) => indicatesLoginRequired(entry, depth + 1));
  if (typeof value !== "object") return false;
  const record = value as Record<string, unknown>;
  const message = cleanText(
    record.message ?? record.msg ?? record.status_msg,
    300,
  );
  const code = String(
    record.code ?? record.status_code ?? record.error_code ?? "",
  );
  if (
    /login[_ -]?required|not[_ -]?login|未登录|登录(?:已)?失效|请.*登录/i.test(
      message,
    )
  )
    return true;
  if (
    ["10001", "10002", "-100", "-101"].includes(code) &&
    /login|登录/i.test(message)
  )
    return true;
  return Object.values(record).some((entry) =>
    indicatesLoginRequired(entry, depth + 1),
  );
}

function isLoginPage(url: string): boolean {
  try {
    return /\/(?:login|passport)(?:\/|$)/i.test(new URL(url).pathname);
  } catch {
    return false;
  }
}

function platformItemFromHref(
  platform: PlatformCapturePlatform,
  href: string,
  title: string,
  coverUrl?: string,
  author = "",
  publishedAt?: number,
  hasVideo = false,
): PlatformDiscoveryItem | null {
  let resolved: URL;
  try {
    resolved = new URL(href, platformLoginUrl(platform));
  } catch {
    return null;
  }
  if (!isAllowedPlatformUrl(platform, resolved.href)) return null;
  const match =
    platform === "xiaohongshu"
      ? /\/(?:explore|discovery\/item)\/([^/?#]+)/i.exec(resolved.pathname)
      : /\/(?:video|note)\/([^/?#]+)/i.exec(resolved.pathname);
  if (!match) return null;
  const mediaType =
    hasVideo || (platform === "douyin" && resolved.pathname.includes("/video/"))
      ? "video"
      : "image";
  return {
    platform,
    externalId: match[1],
    url: resolved.href,
    title:
      cleanText(title, 300) ||
      `${platform === "xiaohongshu" ? "小红书" : "抖音"}作品 ${match[1]}`,
    author: cleanText(author, 200),
    coverUrl:
      coverUrl && isAllowedBrowserResourceUrl(platform, coverUrl)
        ? coverUrl
        : undefined,
    mediaType,
    publishedAt:
      publishedAt && Number.isFinite(publishedAt) ? publishedAt : undefined,
    dateConfidence: publishedAt && Number.isFinite(publishedAt) ? "medium" : "low",
    discoveryMethod: "browser-dom",
  };
}

export function scanPlatformComments(
  payloads: unknown[],
  limit: number,
): CapturedComment[] {
  const comments = new Map<string, CapturedComment>();
  let visited = 0;
  const visit = (
    value: unknown,
    depth: number,
    insideReplies = false,
  ): void => {
    if (
      comments.size >= limit ||
      visited > 20_000 ||
      depth > 12 ||
      value == null
    )
      return;
    visited += 1;
    if (Array.isArray(value)) {
      for (const entry of value) visit(entry, depth + 1, insideReplies);
      return;
    }
    if (typeof value !== "object") return;
    const record = value as Record<string, unknown>;
    const content = cleanText(
      record.content ?? record.text ?? record.comment_text,
      5000,
    );
    const rawId = record.comment_id ?? record.cid ?? record.commentId;
    const parentId =
      record.parent_comment_id ??
      record.parent_id ??
      record.reply_to_comment_id;
    const isTopLevel =
      !insideReplies &&
      (parentId == null ||
        parentId === "" ||
        parentId === 0 ||
        parentId === "0");
    if (
      isTopLevel &&
      content &&
      (typeof rawId === "string" || typeof rawId === "number")
    ) {
      const user = (record.user ?? record.author) as
        Record<string, unknown> | undefined;
      const externalId = String(rawId);
      const rawPublishedAt = numeric(record.create_time ?? record.time);
      comments.set(externalId, {
        externalId,
        authorName: cleanText(
          user?.nickname ?? user?.name ?? record.nickname,
          200,
        ),
        content,
        likeCount: Math.max(
          0,
          Math.floor(
            numeric(record.digg_count ?? record.like_count ?? record.likes),
          ),
        ),
        publishedAt: rawPublishedAt
          ? rawPublishedAt * (rawPublishedAt < 10_000_000_000 ? 1000 : 1)
          : null,
      });
    }
    for (const [key, child] of Object.entries(record)) {
      visit(
        child,
        depth + 1,
        insideReplies || /(?:sub_?comments?|repl(?:y|ies))$/i.test(key),
      );
    }
  };
  for (const payload of payloads) visit(payload, 0);
  return [...comments.values()]
    .sort((a, b) => b.likeCount - a.likeCount)
    .slice(0, limit);
}

function nestedText(
  record: Record<string, unknown> | undefined,
  keys: string[],
): string {
  if (!record) return "";
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

function firstNestedUrl(value: unknown, depth = 0): string | undefined {
  if (depth > 5 || value == null) return undefined;
  if (typeof value === "string" && /^https?:\/\//i.test(value)) return value;
  if (Array.isArray(value)) {
    for (const entry of value) {
      const match = firstNestedUrl(entry, depth + 1);
      if (match) return match;
    }
    return undefined;
  }
  if (typeof value !== "object") return undefined;
  const record = value as Record<string, unknown>;
  for (const key of [
    "url_default",
    "urlDefault",
    "url",
    "url_list",
    "urlList",
    "cover",
    "image_list",
    "imageList",
  ]) {
    if (key in record) {
      const match = firstNestedUrl(record[key], depth + 1);
      if (match) return match;
    }
  }
  return undefined;
}

/** 从平台页面自己的 JSON 响应里提取发现卡片；不发起任何接口请求。 */
export function scanPlatformDiscoveryPayloads(
  platform: PlatformCapturePlatform,
  payloads: unknown[],
  limit = MAX_DISCOVERY_ITEMS,
): PlatformDiscoveryItem[] {
  const found = new Map<string, PlatformDiscoveryItem>();
  let visited = 0;
  const visit = (value: unknown, depth: number): void => {
    if (found.size >= limit || visited > 30_000 || depth > 12 || value == null)
      return;
    visited += 1;
    if (Array.isArray(value)) {
      for (const entry of value) visit(entry, depth + 1);
      return;
    }
    if (typeof value !== "object") return;
    const record = value as Record<string, unknown>;
    const rawId =
      platform === "douyin"
        ? record.aweme_id
        : (record.note_id ?? record.noteId);
    const externalId =
      typeof rawId === "string" || typeof rawId === "number"
        ? String(rawId)
        : "";
    if (externalId) {
      const author = (record.author ?? record.user) as
        Record<string, unknown> | undefined;
      const desc = nestedText(record, [
        "title",
        "display_title",
        "desc",
        "description",
      ]);
      const images = record.images ?? record.image_list ?? record.imageList;
      const hasVideo = Boolean(
        record.video ?? record.video_info ?? record.videoInfo,
      );
      if (desc || images || hasVideo) {
        const statistics = (record.statistics ?? record.stats ?? record.interact_info ?? record.interactInfo) as
          Record<string, unknown> | undefined;
        const route =
          platform === "douyin" ? (hasVideo ? "video" : "note") : "explore";
        const timestamp = numeric(
          record.create_time ?? record.time ?? record.publish_time,
        );
        found.set(externalId, {
          platform,
          externalId,
          url:
            platform === "douyin"
              ? `https://www.douyin.com/${route}/${externalId}`
              : `https://www.xiaohongshu.com/explore/${externalId}`,
          title:
            cleanText(desc, 300) ||
            `${platform === "douyin" ? "抖音" : "小红书"}作品 ${externalId}`,
          author: cleanText(
            nestedText(author, ["nickname", "nick_name", "name"]),
            200,
          ),
          coverUrl: (() => {
            const candidate = firstNestedUrl(
              record.cover ?? images ?? record.video,
            );
            return candidate && isAllowedBrowserResourceUrl(platform, candidate)
              ? candidate
              : undefined;
          })(),
          mediaType: hasVideo ? "video" : "image",
          snippet: cleanText(record.desc ?? record.description ?? desc, 1000),
          engagement: {
            views: optionalMetric(statistics?.play_count ?? statistics?.view_count ?? record.play_count),
            likes: optionalMetric(statistics?.digg_count ?? statistics?.liked_count ?? statistics?.like_count ?? record.liked_count),
            comments: optionalMetric(statistics?.comment_count ?? record.comment_count),
            shares: optionalMetric(statistics?.share_count ?? record.share_count),
            collects: platform === "douyin"
              ? optionalMetric(statistics?.collect_count ?? statistics?.collected_count ?? record.collected_count)
              : undefined,
            favorites: platform === "xiaohongshu"
              ? optionalMetric(statistics?.collect_count ?? statistics?.collected_count ?? record.collected_count)
              : optionalMetric(statistics?.favorite_count ?? record.favorite_count),
          },
          publishedAt: timestamp
            ? timestamp * (timestamp < 10_000_000_000 ? 1000 : 1)
            : undefined,
          dateConfidence: timestamp ? "high" : "low",
          discoveryMethod: "captured-json",
        });
      }
    }
    for (const child of Object.values(record)) visit(child, depth + 1);
  };
  for (const payload of payloads) visit(payload, 0);
  return [...found.values()].slice(0, limit);
}

export class BrowserCaptureService {
  private readonly baseDir: string;
  private readonly profileDir: string;
  private readonly statePath: string;
  private getNetworkProxy?: BrowserCaptureServiceOptions["getNetworkProxy"];
  private operationTail: Promise<void> = Promise.resolve();
  private activeController: AbortController | null = null;
  private activeKind: OperationKind | null = null;
  private activePlatform: PlatformCapturePlatform | null = null;
  private activeContext: ElectronCaptureContext | null = null;
  private browserVersion: string | undefined = process.versions.chrome;

  constructor(options: BrowserCaptureServiceOptions = {}) {
    const root = options.userDataPath ?? getUserDataPath();
    this.baseDir = path.join(root, "browser-capture");
    this.profileDir = path.join(this.baseDir, "electron-session");
    this.statePath = path.join(this.baseDir, "session-status.json");
    this.getNetworkProxy = options.getNetworkProxy;
  }

  getProfileDir(): string {
    return this.profileDir;
  }

  setNetworkProxyProvider(
    provider: BrowserCaptureServiceOptions["getNetworkProxy"],
  ): void {
    this.getNetworkProxy = provider;
  }

  getStatuses(): PlatformSessionStatus[] {
    const state = this.readState();
    return (["xiaohongshu", "douyin", "linuxdo"] as const).map((platform) => ({
      platform,
      browser: "embedded",
      browserVersion: this.browserVersion,
      available: true,
      loggedIn: state[platform] === true,
      busy: this.activePlatform === platform,
    }));
  }

  cancel(kind?: OperationKind): boolean {
    if (!this.activeController || (kind && this.activeKind !== kind))
      return false;
    this.activeController.abort();
    void this.activeContext?.close().catch(() => undefined);
    return true;
  }

  cancelForPlatform(
    kind: OperationKind,
    platform: PlatformCapturePlatform,
  ): boolean {
    if (this.activePlatform !== platform) return false;
    return this.cancel(kind);
  }

  async login(
    platform: PlatformCapturePlatform,
    forceRelogin = false,
    parent?: BrowserWindow | null,
  ): Promise<PlatformSessionStatus> {
    await this.runSerialized(
      platform,
      "login",
      async (context, signal) => {
        const page = await this.preparePage(context, platform);
        await this.optimizeLoginPage(page, platform);
        if (forceRelogin) {
          // 必须在首次导航前清 Cookie，并让页面脚本运行前清空站点存储。
          // 否则页面会先自动弹出登录框，随后 reload 把它刷新掉，再弹第二次。
          await this.prepareForcedRelogin(context, page, platform);
        }
        const loginEntryUrl =
          platform === "douyin"
            ? DOUYIN_FAST_LOGIN_URL
            : platformLoginUrl(platform);
        await page.goto(loginEntryUrl, {
          waitUntil: "domcontentloaded",
          timeout: NAVIGATION_TIMEOUT_MS,
        });
        if (
          !forceRelogin &&
          (await this.hasExistingLoginState(context, page, platform))
        ) {
          this.writeLoginState(platform, true);
          return;
        }
        await this.showLoginLoadingIndicator(page, platform);
        const hasFastLoginSurface = await this.hasReadyFastLoginSurface(
          page,
          platform,
        );
        if (!hasFastLoginSurface) {
          if (platform === "douyin") {
            // 仅抖音存在创作者中心快速入口；入口改版或暂不可用时，
            // 才回退消费端首页。小红书已在官方首页，重复 goto 会刷新掉刚弹出的登录框。
            await page.goto(platformLoginUrl(platform), {
              waitUntil: "domcontentloaded",
              timeout: NAVIGATION_TIMEOUT_MS,
            });
            await this.showLoginLoadingIndicator(page, platform);
          }
          if (platform !== "linuxdo") {
            await this.openLoginPanel(page, platform);
          }
        }
        const loginCookieBaseline =
          platform === "xiaohongshu"
            ? await this.captureXhsGuestCookieBaseline(context, page)
            : undefined;
        const started = Date.now();
        while (Date.now() - started < LOGIN_TIMEOUT_MS) {
          if (signal.aborted)
            throw new PlatformCaptureError("canceled", "已取消登录");
          if (
            await this.hasLoginState(
              context,
              page,
              platform,
              loginCookieBaseline,
            )
          ) {
            this.writeLoginState(platform, true);
            return;
          }
          if (page.isClosed())
            throw new PlatformCaptureError(
              "browser_closed",
              "平台登录窗口已关闭",
            );
          await page.waitForTimeout(1000);
        }
        throw new PlatformCaptureError(
          "login_timeout",
          "登录等待已超过 5 分钟",
        );
      },
      true,
      undefined,
      parent,
    );
    return this.getStatuses().find((status) => status.platform === platform)!;
  }

  async logout(platform: PlatformCapturePlatform): Promise<void> {
    await this.runSerialized(platform, "capture", async (context) => {
      const page = await this.preparePage(context, platform);
      await page.goto(platformLoginUrl(platform), {
        waitUntil: "domcontentloaded",
        timeout: NAVIGATION_TIMEOUT_MS,
      });
      await this.clearPlatformSession(context, page, platform);
      this.writeLoginState(platform, false);
    });
  }

  async clearAllData(): Promise<void> {
    this.cancel();
    await this.activeContext?.close().catch(() => undefined);
    await clearElectronCaptureSessions();
    const resolved = path.resolve(this.profileDir);
    const expected = path.resolve(this.baseDir, "electron-session");
    if (
      resolved !== expected ||
      path.dirname(resolved) !== path.resolve(this.baseDir)
    ) {
      throw new Error("拒绝清理非归知内置平台会话目录");
    }
    fs.rmSync(this.baseDir, { recursive: true, force: true });
  }

  async capturePage(
    platform: PlatformCapturePlatform,
    url: string,
    signal?: AbortSignal,
  ): Promise<CapturedPage> {
    if (!isAllowedPlatformUrl(platform, url))
      throw new Error("不支持的平台地址");
    this.requireKnownLogin(platform);
    return this.runSerialized(
      platform,
      "capture",
      async (context, operationSignal) => {
        this.throwIfAborted(signal, operationSignal);
        await this.requireLogin(context, platform);
        const page = await this.preparePage(context, platform);
        const payloads = this.captureJsonResponses(page, platform);
        await page.goto(url, {
          waitUntil: "domcontentloaded",
          timeout: NAVIGATION_TIMEOUT_MS,
        });
        this.assertFinalUrl(platform, page.url());
        await page.waitForTimeout(1500);
        this.throwIfAborted(signal, operationSignal);
        if (
          isLoginPage(page.url()) ||
          payloads.some((payload) => indicatesLoginRequired(payload))
        ) {
          this.writeLoginState(platform, false);
          throw new PlatformCaptureError(
            "login_required",
            "平台登录状态已失效，请重新登录",
          );
        }
        return {
          html: await page.content(),
          finalUrl: page.url(),
          jsonPayloads: payloads,
        };
      },
      true,
      signal,
    );
  }

  /** 用平台专用 Electron 会话拉取 JSON（LINUX DO 等需过 Cloudflare 的 Discourse 接口） */
  async fetchJsonViaSession<T>(
    platform: PlatformCapturePlatform,
    url: string,
    signal?: AbortSignal,
  ): Promise<T> {
    if (!isAllowedPlatformUrl(platform, url)) {
      throw new Error("不支持的平台地址");
    }
    return this.runSerialized(
      platform,
      "capture",
      async (_context, operationSignal) => {
        this.throwIfAborted(signal, operationSignal);
        const targetSession = getElectronCaptureSession(platform);
        const response = await targetSession.fetch(url, { signal });
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }
        const contentType = response.headers.get("content-type") ?? "";
        if (contentType && !/json|text\/plain/i.test(contentType)) {
          throw new Error(`接口未返回 JSON: ${contentType.split(";")[0]}`);
        }
        const text = await response.text();
        try {
          return JSON.parse(text) as T;
        } catch {
          throw new Error("接口响应不是合法的 JSON");
        }
      },
      true,
      signal,
    );
  }

  async discoverCreator(
    input: DiscoverCreatorInput,
  ): Promise<PlatformDiscoveryPage> {
    if (!isAllowedPlatformUrl(input.platform, input.url))
      throw new Error("作者主页地址不合法");
    this.requireKnownLogin(input.platform);
    return this.discover(input.platform, input.url, input.cursor, input.limit);
  }

  async search(input: SearchPlatformInput, signal?: AbortSignal): Promise<PlatformDiscoveryPage> {
    const keyword = cleanText(input.keyword, 100);
    if (!keyword) throw new Error("搜索关键词不能为空");
    this.requireKnownLogin(input.platform);
    return this.discover(input.platform, platformSearchUrl(input.platform, keyword), input.cursor, input.limit, signal);
  }

  async captureComments(
    platform: PlatformCapturePlatform,
    url: string,
    limit: number,
    signal?: AbortSignal,
  ): Promise<CapturedComment[]> {
    this.requireKnownLogin(platform);
    const capped = [10, 20, 50].includes(limit) ? limit : 20;
    return this.runSerialized(
      platform,
      "comments",
      async (context, operationSignal) => {
        this.throwIfAborted(signal, operationSignal);
        await this.requireLogin(context, platform);
        const page = await this.preparePage(context, platform);
        const payloads = this.captureJsonResponses(page, platform);
        await page.goto(url, {
          waitUntil: "domcontentloaded",
          timeout: NAVIGATION_TIMEOUT_MS,
        });
        this.assertFinalUrl(platform, page.url());
        for (let index = 0; index < 4; index += 1) {
          await page.scrollBy(900);
          await page.waitForTimeout(900);
          this.throwIfAborted(signal, operationSignal);
        }
        if (
          isLoginPage(page.url()) ||
          payloads.some((payload) => indicatesLoginRequired(payload))
        ) {
          this.writeLoginState(platform, false);
          throw new PlatformCaptureError(
            "login_required",
            "平台登录状态已失效，请重新登录",
          );
        }
        return scanPlatformComments(payloads, capped);
      },
      true,
      signal,
    );
  }

  async close(): Promise<void> {
    this.cancel();
    await this.activeContext?.close().catch(() => undefined);
  }

  private async discover(platform: PlatformCapturePlatform, url: string, cursor?: string | null, requestedLimit?: number, callerSignal?: AbortSignal): Promise<PlatformDiscoveryPage> {
    return this.runSerialized(
      platform,
      "discovery",
      async (context, signal) => {
        this.throwIfAborted(signal, callerSignal);
        await this.requireLogin(context, platform);
        const page = await this.preparePage(context, platform);
        const payloads = this.captureJsonResponses(page, platform);
        await page.goto(url, {
          waitUntil: "domcontentloaded",
          timeout: NAVIGATION_TIMEOUT_MS,
        });
        this.assertFinalUrl(platform, page.url());
        const offset = Math.max(0, Number.parseInt(cursor ?? "0", 10) || 0);
        const limit = Math.min(
          PAGE_SIZE,
          Math.max(1, requestedLimit ?? PAGE_SIZE),
        );
        const target = Math.min(MAX_DISCOVERY_ITEMS, offset + limit + 1);
        const found = new Map<string, PlatformDiscoveryItem>();
        const started = Date.now();
        let previousCount = -1;
        let noGrowthRounds = 0;
        while (
          found.size < target &&
          Date.now() - started < DISCOVERY_TIMEOUT_MS
        ) {
          if (signal.aborted)
            throw new PlatformCaptureError("canceled", "已取消平台发现");
          const cards = await page.evaluate(() => {
            const anchors = Array.from(
              document.querySelectorAll<HTMLAnchorElement>("a[href]"),
            );
            return anchors.slice(0, 800).map((anchor) => {
              const image = anchor.querySelector<HTMLImageElement>("img");
              const card =
                anchor.closest<HTMLElement>(
                  "article, li, [class*='card'], [class*='item']",
                ) ?? anchor.parentElement;
              const author = card?.querySelector<HTMLElement>(
                "[class*='author'], [class*='user'], [class*='name']",
              );
              const time = card?.querySelector<HTMLTimeElement>("time");
              const timestamp = time?.dateTime
                ? Date.parse(time.dateTime)
                : Number.NaN;
              return {
                href: anchor.href,
                title:
                  anchor.getAttribute("title") ||
                  anchor.textContent ||
                  image?.alt ||
                  "",
                coverUrl: image?.currentSrc || image?.src || "",
                author: author?.textContent || "",
                publishedAt: Number.isFinite(timestamp) ? timestamp : undefined,
                hasVideo: Boolean(card?.querySelector("video")),
              };
            });
          });
          for (const card of cards) {
            const item = platformItemFromHref(
              platform,
              card.href,
              card.title,
              card.coverUrl,
              card.author,
              card.publishedAt,
              card.hasVideo,
            );
            if (item) found.set(item.externalId, item);
          }
          for (const item of scanPlatformDiscoveryPayloads(
            platform,
            payloads,
          )) {
            found.set(item.externalId, item);
          }
          if (
            isLoginPage(page.url()) ||
            payloads.some((payload) => indicatesLoginRequired(payload))
          ) {
            this.writeLoginState(platform, false);
            throw new PlatformCaptureError(
              "login_required",
              "平台登录状态已失效，请重新登录",
            );
          }
          noGrowthRounds =
            found.size === previousCount ? noGrowthRounds + 1 : 0;
          previousCount = found.size;
          if (noGrowthRounds >= 5) break;
          if (found.size >= target || found.size >= MAX_DISCOVERY_ITEMS) break;
          await page.scrollBy(1200);
          await page.waitForTimeout(1000);
        }
        if (
          isLoginPage(page.url()) ||
          payloads.some((payload) => indicatesLoginRequired(payload))
        ) {
          this.writeLoginState(platform, false);
          throw new PlatformCaptureError(
            "login_required",
            "平台登录状态已失效，请重新登录",
          );
        }
        const all = [...found.values()].slice(0, MAX_DISCOVERY_ITEMS);
        const items = all.slice(offset, offset + limit);
        const nextOffset = offset + items.length;
        const hasMore =
          nextOffset < all.length ||
          (items.length === limit && nextOffset < MAX_DISCOVERY_ITEMS);
        return { items, cursor: hasMore ? String(nextOffset) : null, hasMore };
      },
      true,
      callerSignal,
    );
  }

  private async runSerialized<T>(
    platform: PlatformCapturePlatform,
    kind: OperationKind,
    operation: (
      context: ElectronCaptureContext,
      signal: AbortSignal,
    ) => Promise<T>,
    _requireBrowser = true,
    callerSignal?: AbortSignal,
    parent?: BrowserWindow | null,
  ): Promise<T> {
    let release!: () => void;
    const previous = this.operationTail;
    this.operationTail = new Promise<void>((resolve) => {
      release = resolve;
    });
    await previous;
    if (callerSignal?.aborted) {
      release();
      throw new PlatformCaptureError("canceled", "操作已取消");
    }
    const controller = new AbortController();
    const onCallerAbort = () => {
      controller.abort();
      void this.activeContext?.close().catch(() => undefined);
    };
    callerSignal?.addEventListener("abort", onCallerAbort, { once: true });
    this.activeController = controller;
    this.activeKind = kind;
    this.activePlatform = platform;
    try {
      fs.mkdirSync(this.profileDir, { recursive: true });
      const proxySettings = normalizeNetworkProxySettings(
        this.getNetworkProxy?.() ?? { mode: "system" },
      );
      const context = await createElectronCaptureContext({
        platform,
        visible: kind === "login",
        parent,
        proxy: proxySettings,
        isAllowedResourceUrl: (url) =>
          isAllowedBrowserResourceUrl(platform, url),
        isAllowedNavigationUrl: (url) => isOfficialLoginFlowUrl(platform, url),
        shouldBlockRequest: (url, resourceType) =>
          shouldBlockLoginPageRequest(platform, url, resourceType),
      });
      this.activeContext = context;
      this.browserVersion = context.browserVersion ?? this.browserVersion;
      return await operation(context, controller.signal);
    } catch (error) {
      if (error instanceof PlatformCaptureError) throw error;
      if (controller.signal.aborted)
        throw new PlatformCaptureError("canceled", "操作已取消");
      const message = error instanceof Error ? error.message : String(error);
      if (/页面导航超时/i.test(message)) {
        throw new PlatformCaptureError("platform_changed", "平台搜索页加载超时，请检查网络或代理后重试", { cause: error });
      }
      if (/closed|Target page|browser has been closed/i.test(message)) {
        throw new PlatformCaptureError("browser_closed", "平台登录窗口已关闭", {
          cause: error,
        });
      }
      if (!this.activeContext) {
        throw new PlatformCaptureError(
          "browser_unavailable",
          "归知内置平台窗口无法启动",
          { cause: error },
        );
      }
      throw new PlatformCaptureError(
        "platform_changed",
        "平台页面未能按预期加载或结构可能已变化",
        { cause: error },
      );
    } finally {
      callerSignal?.removeEventListener("abort", onCallerAbort);
      await this.activeContext?.close().catch(() => undefined);
      this.activeContext = null;
      this.activeController = null;
      this.activeKind = null;
      this.activePlatform = null;
      release();
    }
  }

  private async preparePage(
    context: ElectronCaptureContext,
    _platform: PlatformCapturePlatform,
  ): Promise<ElectronCapturePage> {
    const page = context.page;
    page.setDefaultNavigationTimeout(NAVIGATION_TIMEOUT_MS);
    page.setDefaultTimeout(NAVIGATION_TIMEOUT_MS);
    return page;
  }

  private captureJsonResponses(
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): unknown[] {
    return page.startJsonCapture(platform);
  }

  private async hasLoginCookies(
    context: ElectronCaptureContext,
    platform: PlatformCapturePlatform,
  ): Promise<boolean> {
    const checkUrl =
      platform === "linuxdo" ? "https://linux.do/" : platformLoginUrl(platform);
    const cookies = await context.cookies(checkUrl);
    const allCookies = cookies.length > 0 ? cookies : await context.cookies();
    const expected = LOGIN_COOKIE_NAMES[platform];
    return allCookies.some(
      (cookie) => expected.includes(cookie.name) && cookie.value.length > 0,
    );
  }

  private async hasLoginState(
    context: ElectronCaptureContext,
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
    xhsCookieBaseline?: LoginCookieSnapshot,
  ): Promise<boolean> {
    try {
      if (platform === "linuxdo") {
        if (await this.hasLoginCookies(context, platform)) return true;
        return await page.evaluate(() =>
          Boolean(document.querySelector("#current-user, .current-user")),
        );
      }
      if (platform === "douyin") {
        if (await this.hasLoginCookies(context, platform)) return true;
        return await page.evaluate(
          () => window.localStorage.getItem("HasUserLogin") === "1",
        );
      }

      // 任意作者主页链接在未登录信息流里也大量存在，不能据此判定成功。
      // 只认侧栏中指向本人主页且文本为“我”的账号入口。
      const selfProfile = page
        .locator('a[href*="/user/profile/"] span:text-is("我")')
        .first();
      if (await selfProfile.isVisible()) return true;

      if (xhsCookieBaseline) {
        const current = await this.getLoginCookieSnapshot(context, platform);
        return didLoginCookieSnapshotChange(xhsCookieBaseline, current);
      }
    } catch {
      // 页面切换或平台脚本尚未就绪时，下一轮继续检查。
    }
    return false;
  }

  private async hasExistingLoginState(
    context: ElectronCaptureContext,
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): Promise<boolean> {
    if (platform === "douyin" || platform === "linuxdo") {
      return this.hasLoginState(context, page, platform);
    }
    try {
      await page
        .locator('a[href*="/user/profile/"] span:text-is("我")')
        .first()
        .waitFor({ state: "visible", timeout: 2_500 });
      return true;
    } catch {
      return false;
    }
  }

  private async captureXhsGuestCookieBaseline(
    context: ElectronCaptureContext,
    page: ElectronCapturePage,
  ): Promise<LoginCookieSnapshot> {
    // 等二维码或手机号表单就绪后再取基线，避免把页面初始化时刚写入的
    // 访客 web_session 误认为扫码带来的 Cookie 变化。
    await page
      .locator(
        [
          "img.qrcode-img",
          "div.login-container input[placeholder*='手机号']",
          "div.login-container input[placeholder*='验证码']",
        ].join(", "),
      )
      .first()
      .waitFor({ state: "visible", timeout: 10_000 })
      .catch(() => undefined);
    return this.getLoginCookieSnapshot(context, "xiaohongshu");
  }

  private async getLoginCookieSnapshot(
    context: ElectronCaptureContext,
    platform: PlatformCapturePlatform,
  ): Promise<LoginCookieSnapshot> {
    const expected = LOGIN_COOKIE_NAMES[platform];
    const snapshot: Record<string, string> = {};
    for (const cookie of await context.cookies(platformLoginUrl(platform))) {
      if (expected.includes(cookie.name) && cookie.value.length > 0) {
        snapshot[cookie.name] = cookie.value;
      }
    }
    return snapshot;
  }

  private async openLoginPanel(
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): Promise<void> {
    const panel =
      platform === "douyin"
        ? page
            .locator(
              [
                "#login-panel-new",
                "#login-panel",
                "[class*='login-panel']",
              ].join(", "),
            )
            .first()
        : page
            .locator(
              [
                "div.login-container",
                "[class*='login-container']",
                "[class*='login-modal']",
              ].join(", "),
            )
            .first();

    const candidates =
      platform === "douyin"
        ? [
            // 与抖音桌面站当前入口结构一致，也与 MediaCrawler 的定位思路一致。
            page.locator('p:text-is("登录")').first(),
            page.getByRole("button", { name: "登录", exact: true }).first(),
            page.locator('[role="button"]:has-text("登录")').first(),
            page.getByText("登录", { exact: true }).first(),
          ]
        : [
            page.getByRole("button", { name: "登录", exact: true }).first(),
            page.getByText("登录", { exact: true }).first(),
          ];

    // 同时观察面板和首选登录入口：入口一旦挂载就立即点击，不再固定等满
    // 10 秒。这样仍兼容平台自动弹窗，又避免首页推荐流拖慢登录体验。
    const preferredTarget = candidates[0];
    const started = Date.now();
    while (Date.now() - started < LOGIN_PANEL_AUTO_OPEN_TIMEOUT_MS) {
      try {
        if (await panel.isVisible()) return;
        if (await preferredTarget.isVisible()) {
          await preferredTarget.click({
            timeout: LOGIN_PANEL_CLICK_TIMEOUT_MS,
          });
          await panel.waitFor({
            state: "visible",
            timeout: LOGIN_PANEL_CONFIRM_TIMEOUT_MS,
          });
          return;
        }
      } catch {
        // 节点可能正在被前端重建，后面的候选定位仍可继续尝试。
        break;
      }
      await page.waitForTimeout(LOGIN_PANEL_POLL_INTERVAL_MS);
    }

    for (const target of candidates) {
      try {
        await target.waitFor({
          state: "visible",
          timeout: LOGIN_PANEL_CLICK_TIMEOUT_MS,
        });
        await target.click({ timeout: LOGIN_PANEL_CLICK_TIMEOUT_MS });
        await panel.waitFor({
          state: "visible",
          timeout: LOGIN_PANEL_CONFIRM_TIMEOUT_MS,
        });
        return;
      } catch {
        // 入口可能是同名的隐藏节点或平台已切换结构，继续尝试下一个候选。
      }
    }

    // 保持浏览器窗口开启，让用户仍可在官方页面手动操作；登录状态轮询继续运行。
  }

  private async hasReadyFastLoginSurface(
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): Promise<boolean> {
    if (platform !== "douyin") return false;
    try {
      // 创作者中心未登录页直接渲染扫码/手机号登录，不需要加载推荐页再点弹框。
      await page.getByText("扫码登录", { exact: true }).first().waitFor({
        state: "visible",
        timeout: DOUYIN_FAST_LOGIN_READY_TIMEOUT_MS,
      });
      return true;
    } catch {
      return false;
    }
  }

  private async optimizeLoginPage(
    _page: ElectronCapturePage,
    _platform: PlatformCapturePlatform,
  ): Promise<void> {
    // Electron 专用会话在请求发出前统一执行域名白名单与登录页轻量化规则。
  }

  private async showLoginLoadingIndicator(
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): Promise<void> {
    await page
      .evaluate((currentPlatform) => {
        const indicatorId = "guizhi-platform-login-progress";
        document.getElementById(indicatorId)?.remove();

        const indicator = document.createElement("div");
        indicator.id = indicatorId;
        indicator.setAttribute("role", "status");
        indicator.setAttribute("aria-live", "polite");
        const isLinuxDo = currentPlatform === "linuxdo";
        Object.assign(indicator.style, {
          position: "fixed",
          left: "50%",
          top: isLinuxDo ? "24px" : "50%",
          transform: isLinuxDo ? "translateX(-50%)" : "translate(-50%, -50%)",
          zIndex: "2147483647",
          width: "min(460px, calc(100vw - 48px))",
          boxSizing: "border-box",
          padding: "20px 24px",
          borderRadius: "16px",
          background: "rgba(24, 24, 27, 0.94)",
          color: "#fff",
          boxShadow: "0 18px 60px rgba(0, 0, 0, 0.45)",
          fontFamily:
            "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
          textAlign: "center",
          pointerEvents: "none",
          transition: "opacity 0.3s ease",
        });

        const title = document.createElement("div");
        title.textContent =
          currentPlatform === "douyin"
            ? "正在加载抖音官方登录框…"
            : isLinuxDo
              ? "正在加载 LINUX DO 验证页…"
              : "正在加载小红书官方登录框…";
        Object.assign(title.style, { fontSize: "16px", fontWeight: "650" });
        const detail = document.createElement("div");
        detail.textContent = isLinuxDo
          ? "请完成 Cloudflare 验证；若需访问私密版块可顺便登录，完成后提示会自动消失"
          : "登录组件出现后此提示会自动消失，请勿关闭窗口";
        Object.assign(detail.style, {
          marginTop: "8px",
          fontSize: "13px",
          lineHeight: "1.5",
          color: "rgba(255, 255, 255, 0.68)",
        });
        indicator.append(title, detail);
        document.body.appendChild(indicator);

        const readySelectors =
          currentPlatform === "douyin"
            ? [
                "#animate_qrcode_container img",
                "input[placeholder*='手机号']",
                "article.web-login-mobile-code",
              ]
            : isLinuxDo
              ? [
                  "#challenge-stage",
                  "#cf-wrapper",
                  "iframe[src*='challenges.cloudflare.com']",
                  ".cf-turnstile",
                  "#main-outlet",
                  ".d-header",
                  "#site-logo",
                  "button.login-button",
                  "header",
                ]
              : [
                  "div.login-container img",
                  "input[placeholder*='手机号']",
                  "input[placeholder*='验证码']",
                ];
        const isReady = (): boolean =>
          readySelectors.some((selector) => {
            const element = document.querySelector<HTMLElement>(selector);
            if (!element) return false;
            const rect = element.getBoundingClientRect();
            if (rect.width === 0 || rect.height === 0) return false;
            if (element instanceof HTMLImageElement)
              return element.complete && element.naturalWidth > 0;
            return true;
          });
        let pollTimer: number | undefined;
        const removeWhenReady = (): void => {
          if (!isReady()) return;
          observer.disconnect();
          if (pollTimer !== undefined) window.clearInterval(pollTimer);
          indicator.remove();
        };
        const observer = new MutationObserver(removeWhenReady);
        observer.observe(document.documentElement, {
          childList: true,
          subtree: true,
          attributes: true,
          attributeFilter: ["src", "style", "class"],
        });
        window.setTimeout(() => {
          if (!indicator.isConnected) return;
          if (currentPlatform === "douyin") {
            detail.textContent =
              "抖音登录服务响应较慢，仍在等待二维码或手机号表单…";
          } else if (isLinuxDo) {
            detail.textContent =
              "正在等待 Cloudflare 响应，若页面出现验证框请手动完成…";
            window.setTimeout(() => {
              if (indicator.isConnected) indicator.remove();
            }, 3000);
          } else {
            detail.textContent = "小红书登录服务响应较慢，仍在等待登录表单…";
          }
        }, 8_000);
        pollTimer = window.setInterval(removeWhenReady, 500);
        removeWhenReady();
      }, platform)
      .catch(() => undefined);
  }

  private async clearPlatformSession(
    context: ElectronCaptureContext,
    page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): Promise<void> {
    await this.clearPlatformCookies(context, platform);
    try {
      await page.evaluate(() => {
        window.localStorage.clear();
        window.sessionStorage.clear();
      });
    } catch {
      // Cookie 已清除；页面存储不可用时不阻塞退出或重新登录。
    }
    this.writeLoginState(platform, false);
  }

  private async prepareForcedRelogin(
    context: ElectronCaptureContext,
    _page: ElectronCapturePage,
    platform: PlatformCapturePlatform,
  ): Promise<void> {
    await this.clearPlatformCookies(context, platform);
    this.writeLoginState(platform, false);
  }

  private async clearPlatformCookies(
    context: ElectronCaptureContext,
    _platform: PlatformCapturePlatform,
  ): Promise<void> {
    // 每个平台使用独立 partition，可安全清空整份会话，不会影响另一平台或主窗口。
    await context.clearStorageData();
  }

  private async requireLogin(
    context: ElectronCaptureContext,
    platform: PlatformCapturePlatform,
  ): Promise<void> {
    if (!(await this.hasLoginCookies(context, platform))) {
      this.writeLoginState(platform, false);
      throw new PlatformCaptureError(
        "login_required",
        "平台登录状态已失效，请重新登录",
      );
    }
  }

  private requireKnownLogin(platform: PlatformCapturePlatform): void {
    if (this.readState()[platform] !== true) {
      throw new PlatformCaptureError(
        "login_required",
        "请先在设置或平台发现中登录账号",
      );
    }
  }

  private assertFinalUrl(platform: PlatformCapturePlatform, url: string): void {
    if (!isOfficialLoginFlowUrl(platform, url)) {
      throw new PlatformCaptureError(
        "platform_changed",
        "平台跳转到了非预期地址",
      );
    }
    if (!isAllowedPlatformUrl(platform, url)) {
      this.writeLoginState(platform, false);
      throw new PlatformCaptureError(
        "login_required",
        "平台要求重新验证登录状态",
      );
    }
  }

  private throwIfAborted(...signals: Array<AbortSignal | undefined>): void {
    if (signals.some((signal) => signal?.aborted)) {
      throw new PlatformCaptureError("canceled", "操作已取消");
    }
  }

  private readState(): ProfileState {
    try {
      const parsed = JSON.parse(
        fs.readFileSync(this.statePath, "utf8"),
      ) as ProfileState;
      if (parsed.version !== SESSION_STATUS_VERSION) {
        // v3 从外部 Chrome/Edge 档案迁到 Electron 独立会话；旧登录标记不能继承，
        // 两个平台都安全地要求重新登录一次。
        return {
          version: SESSION_STATUS_VERSION,
          xiaohongshu: false,
          douyin: false,
          linuxdo: false,
        };
      }
      return parsed;
    } catch {
      return { version: SESSION_STATUS_VERSION };
    }
  }

  private writeLoginState(
    platform: PlatformCapturePlatform,
    loggedIn: boolean,
  ): void {
    fs.mkdirSync(this.baseDir, { recursive: true });
    const state = {
      ...this.readState(),
      version: SESSION_STATUS_VERSION,
      [platform]: loggedIn,
    };
    fs.writeFileSync(this.statePath, JSON.stringify(state), {
      encoding: "utf8",
      mode: 0o600,
    });
  }
}

let singleton: BrowserCaptureService | null = null;

export function getBrowserCaptureService(
  options?: BrowserCaptureServiceOptions,
): BrowserCaptureService {
  if (!singleton) singleton = new BrowserCaptureService(options);
  else if (options?.getNetworkProxy) {
    singleton.setNetworkProxyProvider(options.getNetworkProxy);
  }
  return singleton;
}

export async function closeBrowserCaptureService(): Promise<void> {
  await singleton?.close();
}

export function resetBrowserCaptureServiceForTests(): void {
  singleton = null;
}
