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
 * 入库策略与 V2EX 不同：NGA 长帖水楼多，条目只保留主楼 + 楼主回复；
 * 另按页采样他人回复（上限 NGA_SUMMARY_MAX_PAGES）专供讨论总结。
 * 楼主回复走 authorid 过滤拉取，避免为镜像两千楼而翻百页。
 * 附件图只处理主楼与楼主回复里的，软上限 NGA_IMAGE_LIMIT。
 */
import fs from "fs/promises";
import path from "path";
import { ngaCanonicalUrl } from "@guizhi/shared/utils/forum-platforms";
import { normalizeForumSnippet } from "@guizhi/shared/utils/forum-note";
import { PlatformParseError } from "@guizhi/shared/utils/platform-parse-error";
import { asForumReplyTo, type ForumReply, type ForumThread } from "./forum-types";
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
/**
 * 为讨论总结采样的最大页数（含第 1 页）。
 * 约 12×20=240 楼，够 forum-summary 的分块上限消化，又不必镜像整帖。
 */
export const NGA_SUMMARY_MAX_PAGES = 12;
/** 楼主发言按 authorid 拉取时的页数上限（防止异常账号拖死） */
export const NGA_OP_MAX_PAGES = 40;
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
  pid?: number;
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
  /** 测试注入：讨论总结采样页上限 */
  maxSummaryPages?: number;
  /** 测试注入：楼主 authorid 拉取页上限 */
  maxOpPages?: number;
  /** 测试注入：按 pid 补拉父楼次数上限 */
  maxParentFetches?: number;
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
 *
 * 章节标题是可读性的关键：泥潭作者常用
 * `[align=center][b][size=…]标题[/size][/b][/align]` 或 `[h]标题[/h]`，
 * 也常见 `[b][size=…]标题[/size][/b]`（size 包在粗体内）。后者若先套 `**`
 * 再剥 size，会留下 `** 标题 **` 这种 CommonMark 不认的写法。
 * 折叠 → `<details>`，强调色 → 白名单 class，靠 sanitize 放行。
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

  // 折叠：可交互的 details（标题缺省用「展开」）
  text = text.replace(
    /\[collapse(?:=([^\]]+))?\]([\s\S]*?)\[\/collapse\]/gi,
    (_m, title: string | undefined, body: string) => {
      const summary = escapeHtmlText(title?.trim() || "展开");
      const inner = body.trim();
      return `\n\n<details>\n<summary>${summary}</summary>\n\n${inner}\n\n</details>\n\n`;
    },
  );

  // 强调色：白名单 class，不内联任意 CSS
  text = text.replace(
    /\[color=([^\]]+)\]([\s\S]*?)\[\/color\]/gi,
    (_m, color: string, body: string) => {
      const cls = forumColorClass(color);
      const inner = body.trim();
      if (!inner) {
        return "";
      }
      return cls
        ? `<span class="${cls}">${inner}</span>`
        : inner;
    },
  );

  // 引用
  text = text.replace(
    /\[quote\]([\s\S]*?)\[\/quote\]/gi,
    (_m, body: string) => {
      const cleaned = stripReplyHeaderNoise(body);
      return cleaned
        .trim()
        .split("\n")
        .map((line) => `> ${line}`)
        .join("\n");
    },
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

  // 分隔线标题（NGA 的 [h]…[/h]，空内容就是一条线）
  text = text.replace(/\[h\]([\s\S]*?)\[\/h\]/gi, (_m, body: string) => {
    const title = stripInlineBbcode(body).replace(/\n+/g, " ").trim();
    return title ? `\n\n## ${title}\n\n` : "\n\n---\n\n";
  });

  // 居中块：短标题升级为 ##
  text = text.replace(
    /\[align=center\]([\s\S]*?)\[\/align\]/gi,
    (_m, body: string) => {
      const title = extractCenteredHeading(body);
      if (title) {
        return `\n\n## ${title}\n\n`;
      }
      return `\n\n${body}\n\n`;
    },
  );

  // size+粗体短标题（两种嵌套顺序都认），避免剥 size 后留下带空格的 **
  text = text.replace(
    /\[size=(\d+)%\]\s*\[b\]([\s\S]*?)\[\/b\]\s*\[\/size\]/gi,
    (_m, size: string, body: string) => formatSizedBold(Number(size), body),
  );
  text = text.replace(
    /\[b\]\s*\[size=(\d+)%\]([\s\S]*?)\[\/size\]\s*\[\/b\]/gi,
    (_m, size: string, body: string) => formatSizedBold(Number(size), body),
  );

  // 先剥字号/字体，再处理粗体，避免 `**[size]…[/size]**` → `** 标题 **`
  text = text.replace(/\[(?:size|font)(?:=[^\]]*)?\]/gi, "");
  text = text.replace(/\[\/(?:size|font)\]/gi, "");

  text = text.replace(/\[b\]([\s\S]*?)\[\/b\]/gi, (_m, body: string) =>
    formatBoldMarkdown(body),
  );
  text = text.replace(/\[i\]([\s\S]*?)\[\/i\]/gi, (_m, body: string) => {
    const inner = stripInlineBbcode(body).replace(/\n+/g, " ").trim();
    return inner ? `<em>${inner}</em>` : "";
  });
  text = text.replace(/\[u\]([\s\S]*?)\[\/u\]/gi, (_m, body: string) =>
    stripInlineBbcode(body).trim(),
  );
  text = text.replace(/\[del\]([\s\S]*?)\[\/del\]/gi, (_m, body: string) => {
    const inner = stripInlineBbcode(body).replace(/\n+/g, " ").trim();
    return inner ? `~~${inner}~~` : "";
  });
  text = text.replace(
    /\[strikeout\]([\s\S]*?)\[\/strikeout\]/gi,
    (_m, body: string) => {
      const inner = stripInlineBbcode(body).replace(/\n+/g, " ").trim();
      return inner ? `~~${inner}~~` : "";
    },
  );

  // 残留展示标签：剥壳（color 已转 span）
  text = text.replace(/\[(?:align|color|l)(?:=[^\]]*)?\]/gi, "");
  text = text.replace(
    /\[\/(?:align|color|h|l|b|i|u|del|strikeout)\]/gi,
    "",
  );

  text = text.replace(/\[s:[^\]]+\]/gi, "");
  text = text.replace(/\[\/?[a-z0-9*]+(?:=[^\]]*)?\]/gi, "");

  return text
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

const FORUM_COLOR_CLASSES: Record<string, string> = {
  red: "forum-color-red",
  blue: "forum-color-blue",
  green: "forum-color-green",
  orange: "forum-color-orange",
  purple: "forum-color-purple",
  gray: "forum-color-muted",
  grey: "forum-color-muted",
  silver: "forum-color-muted",
};

function forumColorClass(raw: string): string | null {
  const key = raw.trim().toLowerCase().replace(/^#/, "");
  if (FORUM_COLOR_CLASSES[key]) {
    return FORUM_COLOR_CLASSES[key];
  }
  // 泥潭偶发写 skyblue / royalblue 等，归到蓝
  if (key.includes("red") || key === "crimson") {
    return "forum-color-red";
  }
  if (key.includes("blue")) {
    return "forum-color-blue";
  }
  if (key.includes("green")) {
    return "forum-color-green";
  }
  return null;
}

function escapeHtmlText(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function formatSizedBold(sizePercent: number, body: string): string {
  const title = stripInlineBbcode(body).replace(/\n+/g, " ").trim();
  if (!title) {
    return "";
  }
  if (
    Number.isFinite(sizePercent) &&
    sizePercent >= 120 &&
    title.length <= 48 &&
    !/^[-*+>]\s/.test(title)
  ) {
    return `\n\n## ${title}\n\n`;
  }
  return `<strong>${title}</strong>`;
}

/** 剥掉字号/颜色/粗斜体等，只留纯文本，用于判断是不是章节标题 */
function stripInlineBbcode(raw: string): string {
  return raw
    .replace(/\[(?:size|color|font|b|i|u)(?:=[^\]]*)?\]/gi, "")
    .replace(/\[\/(?:size|color|font|b|i|u)\]/gi, "")
    .replace(/<br\s*\/?>/gi, "\n");
}

/**
 * 居中块是否是「章节标题」：剥壳后只有一行、不太长、不像列表/链接。
 * 阈值取 48——泥潭小标题通常几个到十几个字，再长多半是居中段落。
 */
function extractCenteredHeading(body: string): string | null {
  const plain = stripInlineBbcode(body)
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  if (plain.length !== 1) {
    return null;
  }
  const title = plain[0];
  if (title.length === 0 || title.length > 48) {
    return null;
  }
  if (/^[-*+>]\s/.test(title) || /^https?:\/\//i.test(title)) {
    return null;
  }
  return title;
}

/**
 * `[b]…[/b]` → `<strong>`。
 * 不用 `**…**`：CommonMark 在中文/中文标点旁容易判不成对强调，
 * 页面上就会露出字面量星号（「可以说**挑选镜框…**」那种）。
 * 已有 rehype-raw + sanitize，`<strong>` 稳定可渲染。
 */
function formatBoldMarkdown(body: string): string {
  const plain = stripInlineBbcode(body);
  const trimmed = plain.replace(/^\s+|\s+$/g, "");
  if (!trimmed) {
    return "";
  }
  const lines = trimmed
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  if (lines.length === 0) {
    return "";
  }
  if (lines.length === 1) {
    return `<strong>${lines[0]}</strong>`;
  }
  if (trimmed.length > 200) {
    return lines.join("\n\n");
  }
  return lines.map((line) => `<strong>${line}</strong>`).join("\n\n");
}

/** 引用块里的 Reply / Post by 样板行，转引用时清掉，避免讨论区满屏英文噪音 */
function stripReplyHeaderNoise(quoteBody: string): string {
  return quoteBody
    .replace(/\[pid=[^\]]*\][\s\S]*?\[\/pid\]/gi, "")
    .replace(/\[b\]\s*Post by[\s\S]*?\[\/b\]/gi, "")
    .replace(/Reply to Reply Post by[^\n]*/gi, "")
    .replace(/Post by\s*\[[^\]]*\][^\n]*/gi, "")
    .replace(/\[uid[^\]]*\]|\[\/uid\]/gi, "");
}

export const NGA_REPLY_SNIPPET_MAX = 200;
/** 按 pid 补拉父楼的次数上限，避免楼主几百条回复拖垮导入 */
export const NGA_PARENT_FETCH_MAX = 30;

export interface NgaReplyContext {
  replyTo?: {
    author: string;
    floor?: number;
    snippet: string;
    pid?: number;
  };
  /** 去掉首条「回复引用头」后的 BBCode，供转 Markdown */
  content: string;
}

/**
 * 从楼主回复原文抽出被回复楼的上下文。
 * 命中带 pid / Post by 的首条 [quote] 时剥掉它，避免正文与卡片重复。
 */
export function extractNgaReplyContext(raw: string): NgaReplyContext {
  const text = decodeHtmlEntities(raw);
  const match = /^(\s*)\[quote\]([\s\S]*?)\[\/quote\]/i.exec(text);
  if (!match) {
    return { content: text };
  }
  const quoteBody = match[2];
  const looksLikeReplyHeader =
    /\[pid=/i.test(quoteBody) ||
    /Post by/i.test(quoteBody) ||
    /Reply to Reply Post by/i.test(quoteBody);
  if (!looksLikeReplyHeader) {
    return { content: text };
  }

  const pidMatch = /\[pid=(\d+)(?:,[^\]]*)?\]/i.exec(quoteBody);
  const pid = pidMatch ? Number(pidMatch[1]) : undefined;

  let author = "";
  const uidName = /Post by\s*\[uid[^\]]*\]\s*([^[]+?)\s*\[\/uid\]/i.exec(
    quoteBody,
  );
  if (uidName) {
    author = uidName[1].trim();
  } else {
    const plainName =
      /Post by\s+([^\n(#]+)/i.exec(quoteBody) ??
      /Reply to Reply Post by\s+([^\n(#]+)/i.exec(quoteBody);
    if (plainName) {
      author = plainName[1].trim();
    }
  }

  const snippet = normalizeForumSnippet(
    stripReplyHeaderNoise(quoteBody).replace(
      /\[\/?[a-z0-9*]+(?:=[^\]]*)?\]/gi,
      "",
    ),
    NGA_REPLY_SNIPPET_MAX,
  );

  const rest = text.slice(match[0].length).replace(/^\s+/, "");
  if (!author && !snippet && pid == null) {
    return { content: text };
  }

  return {
    replyTo: {
      author: author || "某人",
      snippet,
      pid: Number.isFinite(pid) ? pid : undefined,
    },
    content: rest,
  };
}

function plainSnippetFromBbcode(raw: string): string {
  return normalizeForumSnippet(
    stripInlineBbcode(raw).replace(/\[\/?[a-z0-9*]+(?:=[^\]]*)?\]/gi, ""),
    NGA_REPLY_SNIPPET_MAX,
  );
}

function resolveAuthor(  post: NgaPost,
  users: Record<string, NgaUser> | undefined,
  fallback?: string,
): string {
  const uid = post.authorid;
  // __U 里的昵称优先：lite=js 分页里 post.author 经常缺席，只剩 authorid
  if (uid != null && users) {
    const name = users[String(uid)]?.username?.trim();
    if (name) {
      return name;
    }
  }
  const direct = post.author?.trim();
  if (direct) {
    return direct;
  }
  if (fallback?.trim()) {
    return fallback.trim();
  }
  if (uid != null && uid < 0) {
    return "匿名";
  }
  return uid != null ? `UID:${uid}` : "";
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

function pageUrl(
  topicId: string,
  page: number,
  authorId?: number,
): string {
  const params = new URLSearchParams({
    tid: topicId,
    page: String(page),
    lite: "js",
  });
  if (authorId != null && Number.isFinite(authorId) && authorId !== 0) {
    params.set("authorid", String(authorId));
  }
  return `${NGA_ORIGIN}/read.php?${params.toString()}`;
}

function pidUrl(pid: number): string {
  return `${NGA_ORIGIN}/read.php?pid=${encodeURIComponent(String(pid))}&lite=js`;
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
  authorId?: number,
): Promise<{ store: NgaStore; cookie: string | undefined }> {
  const get = deps.fetchRawText ?? fetchRawText;
  const delays = deps.retryDelaysMs ?? RETRY_DELAYS_MS;
  let lastError: unknown;
  let currentCookie = cookie;
  const url = pageUrl(topicId, page, authorId);

  for (let attempt = 0; attempt <= delays.length; attempt++) {
    if (attempt > 0) {
      await wait(delays[attempt - 1], signal);
    }
    try {
      // 始终接受 403：分页中途 guest 会话失效时要能读挑战正文再握手
      const result = await get(url, {
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
        const retry = await get(url, {
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

/** 按 pid 拉单楼（补楼主回复的父楼上下文） */
async function requestPid(
  pid: number,
  topicId: string,
  cookie: string | undefined,
  deps: NgaDeps,
  signal?: AbortSignal,
): Promise<{ post: NgaPost | null; users: Record<string, NgaUser>; cookie: string | undefined }> {
  const get = deps.fetchRawText ?? fetchRawText;
  let currentCookie = cookie;
  try {
    const result = await get(pidUrl(pid), {
      signal,
      userAgent: NGA_UA,
      referer: ngaCanonicalUrl(topicId),
      cookie: currentCookie,
      acceptStatuses: [403],
    });
    currentCookie =
      mergeNgaCookies(currentCookie, result.setCookies ?? [], result.text) ||
      currentCookie;

    let text = result.text;
    if (result.status === 403) {
      if (!currentCookie?.includes("guestJs=")) {
        return { post: null, users: {}, cookie: currentCookie };
      }
      const retry = await get(pidUrl(pid), {
        signal,
        userAgent: NGA_UA,
        referer: ngaCanonicalUrl(topicId),
        cookie: currentCookie,
      });
      currentCookie =
        mergeNgaCookies(currentCookie, retry.setCookies ?? [], retry.text) ||
        currentCookie;
      text = retry.text;
    }

    const data = assertPageOk(parseNgaStore(text));
    const posts = asPostList(data.__R);
    const post =
      posts.find((p) => p.pid === pid) ??
      posts.find((p) => p.lou !== 0) ??
      posts[0] ??
      null;
    return {
      post,
      users: data.__U ?? {},
      cookie: currentCookie,
    };
  } catch (error) {
    if (error instanceof Error && error.message === "已取消") {
      throw error;
    }
    console.warn(`[import] NGA 按 pid=${pid} 补拉父楼失败:`, error);
    return { post: null, users: {}, cookie: currentCookie };
  }
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
 * 抓取 NGA 帖子：主楼入库，讨论区只留楼主回复；另采样若干页供总结。
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
  const samplePosts = asPostList(page1.__R);

  const rows =
    typeof page1.__ROWS === "number" && page1.__ROWS > 0
      ? page1.__ROWS
      : samplePosts.length;
  const totalPages = Math.max(1, Math.ceil(rows / DEFAULT_PAGE_SIZE));
  const maxSummaryPages = deps.maxSummaryPages ?? NGA_SUMMARY_MAX_PAGES;
  const summaryPageCap = Math.min(totalPages, maxSummaryPages);

  let cookie = first.cookie;
  const extraWarnings: string[] = [];

  for (let page = 2; page <= summaryPageCap; page++) {
    signal?.throwIfAborted();
    try {
      const next = await requestPage(topicId, page, cookie, deps, signal);
      cookie = next.cookie ?? cookie;
      const data = assertPageOk(next.store);
      Object.assign(users, data.__U ?? {});
      samplePosts.push(...asPostList(data.__R));
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      console.warn(`[import] NGA 总结采样第 ${page} 页失败:`, error);
      const reason = error instanceof Error ? error.message : String(error);
      extraWarnings.push(
        `第 ${page} 页及之后采样失败（${reason}），讨论总结素材可能不完整`,
      );
      break;
    }
  }

  if (totalPages > summaryPageCap) {
    extraWarnings.push(
      `原帖约 ${totalPages} 页，讨论总结仅采样前 ${summaryPageCap} 页；讨论区只保留楼主回复`,
    );
  }

  const opFromSample =
    samplePosts.find((p) => p.lou === 0) ?? samplePosts[0];
  const opAuthorId =
    typeof topic.authorid === "number"
      ? topic.authorid
      : typeof opFromSample?.authorid === "number"
        ? opFromSample.authorid
        : undefined;

  const opPosts: NgaPost[] = [];
  const maxOpPages = deps.maxOpPages ?? NGA_OP_MAX_PAGES;

  if (opAuthorId != null && opAuthorId !== 0) {
    for (let page = 1; page <= maxOpPages; page++) {
      signal?.throwIfAborted();
      try {
        const next = await requestPage(
          topicId,
          page,
          cookie,
          deps,
          signal,
          opAuthorId,
        );
        cookie = next.cookie ?? cookie;
        const data = assertPageOk(next.store);
        Object.assign(users, data.__U ?? {});
        const pagePosts = asPostList(data.__R);
        if (pagePosts.length === 0) {
          break;
        }
        opPosts.push(...pagePosts);
        if (pagePosts.length < DEFAULT_PAGE_SIZE) {
          break;
        }
      } catch (error) {
        if (error instanceof Error && error.message === "已取消") {
          throw error;
        }
        console.warn(`[import] NGA 楼主发言第 ${page} 页失败:`, error);
        const reason = error instanceof Error ? error.message : String(error);
        extraWarnings.push(
          `楼主发言第 ${page} 页抓取失败（${reason}），楼主回复可能不完整`,
        );
        break;
      }
    }
  }

  // authorid 过滤失败或不可用时，从采样里挑同作者的楼，总比讨论区全空强
  if (opPosts.length === 0 && opAuthorId != null && opAuthorId !== 0) {
    for (const post of samplePosts) {
      if (post.authorid === opAuthorId) {
        opPosts.push(post);
      }
    }
    if (opPosts.length > 0) {
      extraWarnings.push(
        "未能按楼主筛选拉取完整发言，讨论区仅含采样页内的楼主回复",
      );
    }
  } else if (opPosts.length === 0 && opFromSample) {
    opPosts.push(opFromSample);
    extraWarnings.push("无法识别楼主账号，讨论区未收录后续回复");
  }

  return finalizeThread({
    topicId,
    topic,
    page1,
    samplePosts,
    opPosts,
    users,
    attachBase,
    deps,
    signal,
    cookie,
    extraWarnings,
  });
}

function dedupePostsByFloor(posts: NgaPost[]): Map<number, NgaPost> {
  const byFloor = new Map<number, NgaPost>();
  for (const post of posts) {
    const lou = typeof post.lou === "number" ? post.lou : byFloor.size;
    if (!byFloor.has(lou)) {
      byFloor.set(lou, post);
    }
  }
  return byFloor;
}

function toForumReply(
  lou: number,
  post: NgaPost,
  users: Record<string, NgaUser>,
  formatPost: (bbcode: string, post: NgaPost) => string,
  replyTo?: ForumReply["replyTo"],
  bbcodeOverride?: string,
): ForumReply | null {
  const body = formatPost(bbcodeOverride ?? post.content ?? "", post);
  if (!body && !replyTo) {
    return null;
  }
  return {
    floor: lou,
    author: resolveAuthor(post, users),
    content: body,
    createdAt: toMillis(post.postdatetimestamp),
    replyTo,
  };
}

async function finalizeThread(input: {
  topicId: string;
  topic: NgaTopic;
  page1: NgaPageData;
  samplePosts: NgaPost[];
  opPosts: NgaPost[];
  users: Record<string, NgaUser>;
  attachBase: string;
  deps: NgaDeps;
  signal?: AbortSignal;
  cookie?: string;
  extraWarnings?: string[];
}): Promise<ForumThread> {
  const {
    topicId,
    topic,
    page1,
    samplePosts,
    opPosts,
    users,
    attachBase,
    deps,
    signal,
    extraWarnings = [],
  } = input;
  let cookie = input.cookie;

  const opByFloor = dedupePostsByFloor(opPosts);
  const sampleByFloor = dedupePostsByFloor(samplePosts);

  const opEntry =
    [...opByFloor.entries()].find(([lou]) => lou === 0) ??
    [...sampleByFloor.entries()].find(([lou]) => lou === 0) ??
    [...opByFloor.entries()][0] ??
    [...sampleByFloor.entries()][0];
  if (!opEntry) {
    throw new PlatformParseError("structure_missing", "帖子没有主楼内容");
  }
  const [, opPost] = opEntry;

  // 附件图只处理主楼 + 入库的楼主回复，不给采样里的水楼烧配额
  const imageSourcePosts = [...opByFloor.values()];
  if (!opByFloor.has(0) && opPost) {
    imageSourcePosts.unshift(opPost);
  }
  const allUrls: string[] = [];
  const seenUrls = new Set<string>();
  for (const post of imageSourcePosts) {
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

  const formatPost = (bbcode: string, post: NgaPost): string => {
    const markdown = ngaBbcodeToMarkdown(bbcode, {
      attachBase,
      imageMap,
    });
    return appendOrphanAttachments(markdown, post, attachBase, imageMap);
  };

  const title =
    decodeHtmlEntities(topic.subject ?? opPost.subject ?? "").trim() ||
    `NGA 帖子 ${topicId}`;
  const author = resolveAuthor(opPost, users, topic.author);
  const content = formatPost(opPost.content ?? "", opPost);

  const maxParentFetches = deps.maxParentFetches ?? NGA_PARENT_FETCH_MAX;
  let parentFetches = 0;
  const parentCache = new Map<
    number,
    { author: string; floor?: number; snippet: string }
  >();

  const replies: ForumReply[] = [];
  for (const [lou, post] of [...opByFloor.entries()].sort(
    (a, b) => a[0] - b[0],
  )) {
    if (lou === 0) {
      continue;
    }
    const ctx = extractNgaReplyContext(post.content ?? "");
    let replyTo: ForumReply["replyTo"] = ctx.replyTo
      ? asForumReplyTo(ctx.replyTo)
      : undefined;

    const needParent =
      ctx.replyTo?.pid != null &&
      ctx.replyTo.pid > 0 &&
      !ctx.replyTo.snippet.trim();
    if (needParent && parentFetches < maxParentFetches) {
      const pid = ctx.replyTo!.pid!;
      let cached = parentCache.get(pid);
      if (!cached) {
        parentFetches += 1;
        const fetched = await requestPid(pid, topicId, cookie, deps, signal);
        cookie = fetched.cookie ?? cookie;
        Object.assign(users, fetched.users);
        if (fetched.post) {
          cached = {
            author:
              resolveAuthor(fetched.post, users) || ctx.replyTo!.author,
            floor:
              typeof fetched.post.lou === "number"
                ? fetched.post.lou
                : undefined,
            snippet: plainSnippetFromBbcode(fetched.post.content ?? ""),
          };
          parentCache.set(pid, cached);
        }
      }
      if (cached) {
        replyTo = asForumReplyTo(cached);
      }
    }

    const reply = toForumReply(
      lou,
      post,
      users,
      formatPost,
      replyTo,
      ctx.content,
    );
    if (reply) {
      replies.push(reply);
    }
  }

  const summaryReplies: ForumReply[] = [];
  for (const [lou, post] of [...sampleByFloor.entries()].sort(
    (a, b) => a[0] - b[0],
  )) {
    if (lou === 0) {
      continue;
    }
    const ctx = extractNgaReplyContext(post.content ?? "");
    const reply = toForumReply(
      lou,
      post,
      users,
      formatPost,
      ctx.replyTo ? asForumReplyTo(ctx.replyTo) : undefined,
      ctx.content,
    );
    if (reply) {
      summaryReplies.push(reply);
    }
  }

  // 采样页几乎没有他人回复时，用楼主回复撑总结素材
  const summaryMaterial =
    summaryReplies.length > 0 ? summaryReplies : replies;

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
    summaryReplies: summaryMaterial,
    replyRetention: "op-only",
    webpageUrl: ngaCanonicalUrl(topicId),
    warningReason: warnings.length > 0 ? warnings.join("；") : undefined,
  };
}
