/**
 * NGA 帖子抓取。
 *
 * 走 `read.php?tid=&page=&lite=js`：返回
 * `window.script_muti_get_var_store={data:{__T,__R,__U,…}}`（常为 GBK），
 * 比 HTML/Readability 干净——楼层、作者、附件都是结构化字段。
 *
 * 访客首次请求会 403 并在正文里下发 `guestJs=…` 挑战；带上该 cookie
 * 再请求即可读公开帖，全程不必用户登录。需登录的版块仍会报错，提示清楚。
 *
 * 长帖按页顺序拉齐（默认约 20 楼/页）；附件图入库为 local-image://，
 * 全帖软上限 NGA_IMAGE_LIMIT，超出保留外链并记 warning。
 */
import fs from "fs/promises";
import path from "path";
import { ngaCanonicalUrl } from "@guizhi/shared/utils/forum-platforms";
import { PlatformParseError } from "@guizhi/shared/utils/platform-parse-error";
import type { ForumReply, ForumThread } from "./forum-types";
import {
  IMAGE_EXTENSIONS,
  MEDIA_SIZE_LIMITS,
  mediaProtocolUrl,
  saveMediaAsset,
} from "./media-files";
import {
  downloadToTempFile,
  fetchRawText,
  type FetchRawTextOptions,
  type FetchRawTextResult,
} from "./safe-fetch";

const NGA_ORIGIN = "https://bbs.nga.cn";
const NGA_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
/** 与官方默认一致；接口若不报 page size 就用这个估总页数 */
const DEFAULT_PAGE_SIZE = 20;
/** 全帖附件图入库上限；超出保留外链，避免千楼长帖拖死导入 */
export const NGA_IMAGE_LIMIT = 80;
const DEFAULT_ATTACH_BASE = "https://img.nga.cn/attachments";
const RETRY_DELAYS_MS = [1_500, 4_000];

interface NgaUser {
  uid?: number;
  username?: string;
}

interface NgaAttach {
  attachurl?: string;
  type?: string;
  url_utf8_org_name?: string;
  name?: string;
  ext?: string;
}

interface NgaPost {
  content?: string;
  subject?: string;
  author?: string;
  authorid?: number;
  lou?: number;
  postdatetimestamp?: number;
  attachs?: Record<string, NgaAttach> | NgaAttach[];
}

interface NgaTopic {
  tid?: number;
  fid?: number;
  subject?: string;
  author?: string;
  authorid?: number;
  replies?: number;
  postdate?: number;
}

interface NgaPageData {
  __T?: NgaTopic;
  __R?: Record<string, NgaPost> | NgaPost[];
  __U?: Record<string, NgaUser>;
  __F?: Record<string, { name?: string; [key: string]: unknown }>;
  __GLOBAL?: { _ATTACH_BASE_VIEW?: string };
  __ROWS?: number;
  __PAGE?: number;
  __MESSAGE?: { "0"?: number; "1"?: string };
}

interface NgaStore {
  error?: { "0"?: string; "1"?: string };
  data?: NgaPageData;
  encode?: string;
}

export interface NgaDeps {
  fetchRawText?: (
    url: string,
    options?: FetchRawTextOptions,
  ) => Promise<FetchRawTextResult>;
  downloadImage?: (
    url: string,
    signal?: AbortSignal,
  ) => Promise<{ dir: string; filePath: string }>;
  saveImageAsset?: (filePath: string) => Promise<string>;
  /** 测试注入：重试退避间隔，传空数组即关闭重试 */
  retryDelaysMs?: number[];
  /** 测试注入：附件图上限 */
  imageLimit?: number;
}

type ImageExtension = (typeof IMAGE_EXTENSIONS)[number];

const IMAGE_SIGNATURES: {
  extension: ImageExtension;
  matches: (head: Buffer) => boolean;
}[] = [
  {
    extension: ".jpg",
    matches: (head) => head.subarray(0, 3).toString("hex") === "ffd8ff",
  },
  {
    extension: ".png",
    matches: (head) =>
      head.subarray(0, 8).toString("hex") === "89504e470d0a1a0a",
  },
  {
    extension: ".gif",
    matches: (head) => head.subarray(0, 4).toString("latin1") === "GIF8",
  },
  {
    extension: ".webp",
    matches: (head) =>
      head.subarray(0, 4).toString("latin1") === "RIFF" &&
      head.subarray(8, 12).toString("latin1") === "WEBP",
  },
  {
    extension: ".bmp",
    matches: (head) => head.subarray(0, 2).toString("latin1") === "BM",
  },
];

async function sniffImageExtension(filePath: string): Promise<ImageExtension> {
  const handle = await fs.open(filePath, "r");
  try {
    const head = Buffer.alloc(12);
    await handle.read(head, 0, 12, 0);
    const hit = IMAGE_SIGNATURES.find((signature) => signature.matches(head));
    return hit ? hit.extension : ".jpg";
  } finally {
    await handle.close();
  }
}

async function defaultDownloadImage(
  url: string,
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  const downloaded = await downloadToTempFile(url, {
    signal,
    userAgent: NGA_UA,
    referer: `${NGA_ORIGIN}/`,
    maxBytes: MEDIA_SIZE_LIMITS.image,
    fileName: "image.bin",
  });
  const extension = await sniffImageExtension(downloaded.filePath);
  const filePath = path.join(downloaded.dir, `image${extension}`);
  await fs.rename(downloaded.filePath, filePath);
  return { dir: downloaded.dir, filePath };
}

function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new Error("已取消"));
      return;
    }
    let timer: NodeJS.Timeout | undefined;
    const onAbort = () => {
      clearTimeout(timer);
      reject(new Error("已取消"));
    };
    timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

function isTransientFailure(message: string): boolean {
  return (
    /HTTP 5\d\d/.test(message) ||
    message === "请求超时" ||
    message === "连接被中断"
  );
}

function describeNgaError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (message === "已取消") {
    return message;
  }
  if (message.includes("需要登录") || message.includes("访客不能")) {
    return message;
  }
  if (message.includes("HTTP 404")) {
    return "帖子不存在或已被删除";
  }
  const serverError = /HTTP (5\d\d)/.exec(message);
  if (serverError) {
    return `NGA 服务器暂时无响应（HTTP ${serverError[1]}），已自动重试仍未成功，稍后再试`;
  }
  if (message === "请求超时") {
    return "连接 NGA 超时，已自动重试仍未成功，稍后再试";
  }
  if (message === "连接被中断") {
    return "与 NGA 的连接被中断，已自动重试仍未成功，稍后再试";
  }
  return message;
}

/** 从挑战正文里抠 guestJs 值 */
export function extractGuestJs(text: string): string | null {
  const match = /guestJs=([^;'"\s]+)/.exec(text);
  return match?.[1] ?? null;
}

/**
 * NGA 的 lite=js 不是严格 JSON：用户备注等字段里常有未转义的 tab/换行，
 * JSON.parse 会报 Bad control character。只处理字符串字面量内部，避免动到结构。
 */
export function escapeControlCharsInJsonStrings(source: string): string {
  let out = "";
  let inString = false;
  let escaped = false;
  for (let index = 0; index < source.length; index++) {
    const char = source[index];
    const code = char.charCodeAt(0);
    if (inString) {
      if (escaped) {
        out += char;
        escaped = false;
        continue;
      }
      if (char === "\\") {
        out += char;
        escaped = true;
        continue;
      }
      if (char === '"') {
        out += char;
        inString = false;
        continue;
      }
      if (code < 0x20) {
        const named: Record<number, string> = {
          8: "\\b",
          9: "\\t",
          10: "\\n",
          12: "\\f",
          13: "\\r",
        };
        out +=
          named[code] ?? `\\u${code.toString(16).padStart(4, "0")}`;
        continue;
      }
      out += char;
      continue;
    }
    if (char === '"') {
      inString = true;
    }
    out += char;
  }
  return out;
}

/**
 * 剥掉 `window.script_muti_get_var_store=` 前缀并解析。
 * 偶发尾部多一个分号或空白，JSON.parse 前先修剪。
 */
export function parseNgaStore(text: string): NgaStore {
  const marker = "window.script_muti_get_var_store=";
  const start = text.indexOf(marker);
  const jsonText =
    start >= 0 ? text.slice(start + marker.length).trim() : text.trim();
  const trimmed = jsonText.replace(/;+\s*$/, "");
  const sanitized = escapeControlCharsInJsonStrings(trimmed);
  try {
    return JSON.parse(sanitized) as NgaStore;
  } catch {
    throw new PlatformParseError("structure_missing", "NGA 接口响应无法解析");
  }
}

/** 把 Set-Cookie / guestJs 合并进 Cookie 头 */
export function mergeNgaCookies(
  existing: string | undefined,
  setCookies: string[],
  bodyText: string,
): string {
  const jar = new Map<string, string>();
  for (const part of (existing ?? "").split(";")) {
    const trimmed = part.trim();
    const eq = trimmed.indexOf("=");
    if (eq > 0) {
      jar.set(trimmed.slice(0, eq), trimmed.slice(eq + 1));
    }
  }
  for (const part of setCookies) {
    const eq = part.indexOf("=");
    if (eq > 0) {
      jar.set(part.slice(0, eq), part.slice(eq + 1));
    }
  }
  const guestJs = extractGuestJs(bodyText);
  if (guestJs) {
    jar.set("guestJs", guestJs);
  }
  return [...jar.entries()].map(([key, value]) => `${key}=${value}`).join("; ");
}

function decodeHtmlEntities(value: string): string {
  return value
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&nbsp;/g, " ");
}

function asPostList(raw: NgaPageData["__R"]): NgaPost[] {
  if (!raw) {
    return [];
  }
  if (Array.isArray(raw)) {
    return raw;
  }
  return Object.keys(raw)
    .sort((a, b) => Number(a) - Number(b))
    .map((key) => raw[key])
    .filter(Boolean);
}

function asAttachList(
  raw: NgaPost["attachs"],
): NgaAttach[] {
  if (!raw) {
    return [];
  }
  if (Array.isArray(raw)) {
    return raw;
  }
  return Object.values(raw);
}

function resolveAttachBase(data: NgaPageData): string {
  const view = data.__GLOBAL?._ATTACH_BASE_VIEW?.trim();
  if (!view) {
    return DEFAULT_ATTACH_BASE;
  }
  if (/^https?:\/\//i.test(view)) {
    return view.replace(/\/$/, "");
  }
  return `https://${view.replace(/\/$/, "")}`;
}

/** 相对附件路径 / 绝对 URL → 可下载地址 */
export function resolveNgaImageUrl(
  raw: string,
  attachBase: string,
): string | null {
  const value = raw.trim();
  if (!value) {
    return null;
  }
  if (/^https?:\/\//i.test(value)) {
    return value;
  }
  const relative = value.replace(/^\.\//, "").replace(/^\/+/, "");
  if (!relative) {
    return null;
  }
  return `${attachBase.replace(/\/$/, "")}/${relative}`;
}

function collectPostImageUrls(post: NgaPost, attachBase: string): string[] {
  const urls: string[] = [];
  const seen = new Set<string>();
  const push = (raw: string | undefined) => {
    if (!raw) {
      return;
    }
    const resolved = resolveNgaImageUrl(raw, attachBase);
    if (!resolved || seen.has(resolved)) {
      return;
    }
    seen.add(resolved);
    urls.push(resolved);
  };

  for (const attach of asAttachList(post.attachs)) {
    if ((attach.type ?? "img") === "img" && attach.attachurl) {
      push(attach.attachurl);
    }
  }

  const content = post.content ?? "";
  for (const match of content.matchAll(/\[img\]([\s\S]*?)\[\/img\]/gi)) {
    push(match[1]);
  }

  return urls;
}

/**
 * NGA BBCode → Markdown。只覆盖常见标签；未知标签剥壳留文本。
 * imageMap 有命中时把 [img] 换成 local-image://，否则保留 https 外链。
 */
export function ngaBbcodeToMarkdown(
  raw: string,
  options: {
    attachBase?: string;
    imageMap?: Map<string, string>;
  } = {},
): string {
  const attachBase = options.attachBase ?? DEFAULT_ATTACH_BASE;
  const imageMap = options.imageMap ?? new Map<string, string>();

  let text = decodeHtmlEntities(raw).replace(/\r\n?/g, "\n");
  text = text.replace(/<br\s*\/?>/gi, "\n");

  // 折叠：保留标题与正文
  text = text.replace(
    /\[collapse(?:=([^\]]+))?\]([\s\S]*?)\[\/collapse\]/gi,
    (_m, title: string | undefined, body: string) => {
      const heading = title?.trim() ? `**${title.trim()}**\n\n` : "";
      return `${heading}${body.trim()}`;
    },
  );

  // 引用
  text = text.replace(
    /\[quote\]([\s\S]*?)\[\/quote\]/gi,
    (_m, body: string) =>
      body
        .trim()
        .split("\n")
        .map((line) => `> ${line}`)
        .join("\n"),
  );

  // 列表
  text = text.replace(/\[list\]/gi, "\n").replace(/\[\/list\]/gi, "\n");
  text = text.replace(/\[\*\]/gi, "\n- ");

  // 图片
  text = text.replace(/\[img\]([\s\S]*?)\[\/img\]/gi, (_m, src: string) => {
    const resolved = resolveNgaImageUrl(src, attachBase);
    if (!resolved) {
      return "";
    }
    const local = imageMap.get(resolved);
    return `\n\n![图](${local ?? resolved})\n\n`;
  });

  // 链接：[url=href]text[/url] / [url]href[/url]
  text = text.replace(
    /\[url=([^\]]+)\]([\s\S]*?)\[\/url\]/gi,
    (_m, href: string, label: string) => `[${label.trim() || href}](${href.trim()})`,
  );
  text = text.replace(
    /\[url\]([\s\S]*?)\[\/url\]/gi,
    (_m, href: string) => {
      const clean = href.trim();
      return `[${clean}](${clean})`;
    },
  );

  // 行内样式
  text = text.replace(/\[b\]([\s\S]*?)\[\/b\]/gi, "**$1**");
  text = text.replace(/\[i\]([\s\S]*?)\[\/i\]/gi, "*$1*");
  text = text.replace(/\[u\]([\s\S]*?)\[\/u\]/gi, "$1");
  text = text.replace(/\[del\]([\s\S]*?)\[\/del\]/gi, "~~$1~~");
  text = text.replace(/\[strikeout\]([\s\S]*?)\[\/strikeout\]/gi, "~~$1~~");

  // 纯展示标签：剥壳
  text = text.replace(
    /\[(?:align|size|color|font|h|l)(?:=[^\]]*)?\]/gi,
    "",
  );
  text = text.replace(
    /\[\/(?:align|size|color|font|h|l|b|i|u|del|strikeout)\]/gi,
    "",
  );

  // 表情占位：留下可读提示而不是空白
  text = text.replace(/\[s:[^\]]+\]/gi, "");

  // 残留未知标签
  text = text.replace(/\[\/?[a-z0-9*]+(?:=[^\]]*)?\]/gi, "");

  return text
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function resolveAuthor(
  post: NgaPost,
  users: Record<string, NgaUser> | undefined,
  fallback?: string,
): string {
  const direct = post.author?.trim();
  if (direct) {
    return direct;
  }
  if (fallback?.trim()) {
    return fallback.trim();
  }
  const uid = post.authorid;
  if (uid != null && users) {
    const name = users[String(uid)]?.username?.trim();
    if (name) {
      return name;
    }
  }
  return uid != null && uid < 0 ? "匿名" : "";
}

function resolveNodeName(data: NgaPageData, fid: number | undefined): string {
  if (fid == null || !data.__F) {
    return "";
  }
  const entry = data.__F[String(fid)];
  if (entry && typeof entry.name === "string") {
    return entry.name.trim();
  }
  // 有些页 __F 直接是 { name: "…" }
  const rootName = (data.__F as { name?: string }).name;
  return typeof rootName === "string" ? rootName.trim() : "";
}

function toMillis(seconds: number | undefined): number {
  return typeof seconds === "number" && Number.isFinite(seconds) && seconds > 0
    ? seconds * 1000
    : Date.now();
}

function pageUrl(topicId: string, page: number): string {
  return `${NGA_ORIGIN}/read.php?tid=${encodeURIComponent(topicId)}&page=${page}&lite=js`;
}

function assertPageOk(store: NgaStore): NgaPageData {
  const errorCode = store.error?.["0"] ?? "";
  if (errorCode) {
    if (errorCode.startsWith("15")) {
      throw new PlatformParseError(
        "guest_denied",
        "需要登录后才能查看该帖（访客无权访问）",
      );
    }
    if (errorCode.startsWith("5")) {
      throw new PlatformParseError(
        "note_unavailable",
        "帖子过旧或访问受限，暂时无法采集",
      );
    }
    const detail = store.error?.["1"]?.replace(/<[^>]+>/g, "").trim();
    throw new PlatformParseError(
      "note_unavailable",
      detail
        ? `NGA 拒绝访问：${errorCode}${detail ? `（${detail.slice(0, 80)}）` : ""}`
        : `NGA 拒绝访问：${errorCode}`,
    );
  }
  const messageCode = store.data?.__MESSAGE?.["0"];
  if (typeof messageCode === "number" && messageCode !== 0) {
    const detail = store.data?.__MESSAGE?.["1"] ?? "";
    if (messageCode === 15) {
      throw new PlatformParseError(
        "guest_denied",
        "需要登录后才能查看该帖（访客无权访问）",
      );
    }
    throw new PlatformParseError(
      "note_unavailable",
      `NGA 拒绝访问：${messageCode}${detail ? `（${String(detail).slice(0, 80)}）` : ""}`,
    );
  }
  if (!store.data) {
    throw new PlatformParseError(
      "structure_missing",
      "NGA 接口未返回帖子数据",
    );
  }
  return store.data;
}

async function requestPage(
  topicId: string,
  page: number,
  cookie: string | undefined,
  deps: NgaDeps,
  signal?: AbortSignal,
): Promise<{ store: NgaStore; cookie: string | undefined }> {
  const get = deps.fetchRawText ?? fetchRawText;
  const delays = deps.retryDelaysMs ?? RETRY_DELAYS_MS;
  let lastError: unknown;
  let currentCookie = cookie;

  for (let attempt = 0; attempt <= delays.length; attempt++) {
    if (attempt > 0) {
      await wait(delays[attempt - 1], signal);
    }
    try {
      // 始终接受 403：分页中途 guest 会话失效时要能读挑战正文再握手
      const result = await get(pageUrl(topicId, page), {
        signal,
        userAgent: NGA_UA,
        referer: ngaCanonicalUrl(topicId),
        cookie: currentCookie,
        acceptStatuses: [403],
      });

      currentCookie =
        mergeNgaCookies(
          currentCookie,
          result.setCookies ?? [],
          result.text,
        ) || currentCookie;

      if (result.status === 403) {
        if (!currentCookie || !currentCookie.includes("guestJs=")) {
          throw new PlatformParseError(
            "guest_denied",
            "需要登录后才能查看该帖（访客无权访问）",
          );
        }
        const retry = await get(pageUrl(topicId, page), {
          signal,
          userAgent: NGA_UA,
          referer: ngaCanonicalUrl(topicId),
          cookie: currentCookie,
        });
        currentCookie =
          mergeNgaCookies(
            currentCookie,
            retry.setCookies ?? [],
            retry.text,
          ) || currentCookie;
        return { store: parseNgaStore(retry.text), cookie: currentCookie };
      }

      return { store: parseNgaStore(result.text), cookie: currentCookie };
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message === "已取消") {
        throw error;
      }
      if (error instanceof PlatformParseError) {
        throw error;
      }
      if (
        message.includes("需要登录") ||
        message.includes("拒绝访问") ||
        message.includes("无法解析")
      ) {
        const code = message.includes("需要登录")
          ? "guest_denied"
          : message.includes("无法解析")
            ? "structure_missing"
            : "note_unavailable";
        throw new PlatformParseError(code, describeNgaError(error), {
          cause: error,
        });
      }
      if (!isTransientFailure(message)) {
        throw new PlatformParseError("network", describeNgaError(error), {
          cause: error,
        });
      }
      lastError = error;
      console.warn(
        `[import] NGA 第 ${page} 页第 ${attempt + 1} 次请求失败（${message}）`,
      );
    }
  }

  throw new PlatformParseError("network", describeNgaError(lastError));
}

interface LocalizedImages {
  imageMap: Map<string, string>;
  warningParts: string[];
}

async function localizeImages(
  urls: string[],
  deps: NgaDeps,
  signal?: AbortSignal,
): Promise<LocalizedImages> {
  const limit = deps.imageLimit ?? NGA_IMAGE_LIMIT;
  const imageMap = new Map<string, string>();
  const warningParts: string[] = [];
  const download = deps.downloadImage ?? defaultDownloadImage;
  const save = deps.saveImageAsset ?? ((filePath: string) => saveMediaAsset(filePath, "image"));

  let downloaded = 0;
  let skipped = 0;
  let failed = 0;

  for (const url of urls) {
    signal?.throwIfAborted();
    if (downloaded >= limit) {
      skipped += 1;
      continue;
    }
    let temp: { dir: string; filePath: string } | null = null;
    try {
      temp = await download(url, signal);
      const assetFileName = await save(temp.filePath);
      imageMap.set(url, mediaProtocolUrl("image", assetFileName));
      downloaded += 1;
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      failed += 1;
      console.warn(`[import] NGA 附件图下载失败: ${url}`, error);
    } finally {
      if (temp) {
        await fs.rm(temp.dir, { recursive: true, force: true }).catch(() => {});
      }
    }
  }

  if (skipped > 0) {
    warningParts.push(
      `附件图超过 ${limit} 张上限，另有 ${skipped} 张保留外链`,
    );
  }
  if (failed > 0) {
    warningParts.push(`${failed} 张附件图下载失败，已保留外链`);
  }

  return { imageMap, warningParts };
}

function appendOrphanAttachments(
  markdown: string,
  post: NgaPost,
  attachBase: string,
  imageMap: Map<string, string>,
): string {
  const embedded = new Set(
    [...markdown.matchAll(/!\[[^\]]*]\(([^)]+)\)/g)].map((m) => m[1]),
  );
  const extras: string[] = [];
  for (const attach of asAttachList(post.attachs)) {
    if ((attach.type ?? "img") !== "img" || !attach.attachurl) {
      continue;
    }
    const resolved = resolveNgaImageUrl(attach.attachurl, attachBase);
    if (!resolved) {
      continue;
    }
    const target = imageMap.get(resolved) ?? resolved;
    if (embedded.has(target) || embedded.has(resolved)) {
      continue;
    }
    extras.push(`![图](${target})`);
  }
  if (extras.length === 0) {
    return markdown;
  }
  return `${markdown}\n\n${extras.join("\n\n")}`.trim();
}

/**
 * 抓取 NGA 帖子与全部回复，并把附件图尽量入库。
 */
export async function fetchNgaThread(
  topicId: string,
  deps: NgaDeps = {},
  signal?: AbortSignal,
): Promise<ForumThread> {
  const first = await requestPage(topicId, 1, undefined, deps, signal);
  const page1 = assertPageOk(first.store);
  const topic = page1.__T;
  if (!topic) {
    throw new PlatformParseError("note_unavailable", "帖子不存在或已被删除");
  }

  const attachBase = resolveAttachBase(page1);
  const users: Record<string, NgaUser> = { ...(page1.__U ?? {}) };
  const posts = asPostList(page1.__R);

  const rows =
    typeof page1.__ROWS === "number" && page1.__ROWS > 0
      ? page1.__ROWS
      : posts.length;
  const totalPages = Math.max(1, Math.ceil(rows / DEFAULT_PAGE_SIZE));

  let cookie = first.cookie;
  for (let page = 2; page <= totalPages; page++) {
    signal?.throwIfAborted();
    try {
      const next = await requestPage(topicId, page, cookie, deps, signal);
      cookie = next.cookie ?? cookie;
      const data = assertPageOk(next.store);
      Object.assign(users, data.__U ?? {});
      posts.push(...asPostList(data.__R));
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      // 后续页失败：保留已抓到的楼层，记 warning，总比整帖作废强
      console.warn(`[import] NGA 第 ${page} 页抓取失败，保留已抓楼层:`, error);
      const reason = error instanceof Error ? error.message : String(error);
      const partialWarning = `第 ${page} 页及之后抓取失败（${reason}），仅保留部分楼层`;
      return finalizeThread({
        topicId,
        topic,
        page1,
        posts,
        users,
        attachBase,
        deps,
        signal,
        extraWarnings: [partialWarning],
      });
    }
  }

  return finalizeThread({
    topicId,
    topic,
    page1,
    posts,
    users,
    attachBase,
    deps,
    signal,
  });
}

async function finalizeThread(input: {
  topicId: string;
  topic: NgaTopic;
  page1: NgaPageData;
  posts: NgaPost[];
  users: Record<string, NgaUser>;
  attachBase: string;
  deps: NgaDeps;
  signal?: AbortSignal;
  extraWarnings?: string[];
}): Promise<ForumThread> {
  const {
    topicId,
    topic,
    page1,
    posts,
    users,
    attachBase,
    deps,
    signal,
    extraWarnings = [],
  } = input;

  // 按 lou 去重（翻页偶发重叠），主楼与回复分开
  const byFloor = new Map<number, NgaPost>();
  for (const post of posts) {
    const lou = typeof post.lou === "number" ? post.lou : byFloor.size;
    if (!byFloor.has(lou)) {
      byFloor.set(lou, post);
    }
  }
  const ordered = [...byFloor.entries()].sort((a, b) => a[0] - b[0]);
  const opEntry = ordered.find(([lou]) => lou === 0) ?? ordered[0];
  if (!opEntry) {
    throw new PlatformParseError("structure_missing", "帖子没有主楼内容");
  }
  const [, opPost] = opEntry;

  const allUrls: string[] = [];
  const seenUrls = new Set<string>();
  for (const [, post] of ordered) {
    for (const url of collectPostImageUrls(post, attachBase)) {
      if (!seenUrls.has(url)) {
        seenUrls.add(url);
        allUrls.push(url);
      }
    }
  }

  const { imageMap, warningParts } = await localizeImages(
    allUrls,
    deps,
    signal,
  );
  const warnings = [...extraWarnings, ...warningParts];

  const formatPost = (post: NgaPost): string => {
    const markdown = ngaBbcodeToMarkdown(post.content ?? "", {
      attachBase,
      imageMap,
    });
    return appendOrphanAttachments(markdown, post, attachBase, imageMap);
  };

  const title =
    decodeHtmlEntities(topic.subject ?? opPost.subject ?? "").trim() ||
    `NGA 帖子 ${topicId}`;
  const author = resolveAuthor(opPost, users, topic.author);
  const content = formatPost(opPost);

  const replies: ForumReply[] = [];
  for (const [lou, post] of ordered) {
    if (lou === 0) {
      continue;
    }
    const body = formatPost(post);
    if (!body) {
      continue;
    }
    replies.push({
      floor: lou,
      author: resolveAuthor(post, users),
      content: body,
      createdAt: toMillis(post.postdatetimestamp),
    });
  }

  return {
    platform: "nga",
    topicId,
    title,
    author,
    node: resolveNodeName(page1, topic.fid),
    createdAt: toMillis(topic.postdate ?? opPost.postdatetimestamp),
    replyCount:
      typeof topic.replies === "number" ? topic.replies : replies.length,
    content,
    replies,
    webpageUrl: ngaCanonicalUrl(topicId),
    warningReason: warnings.length > 0 ? warnings.join("；") : undefined,
  };
}
