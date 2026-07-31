/**
 * 小红书采集：不经 yt-dlp。
 *
 * yt-dlp 的 XiaoHongShu 提取器只出视频 formats，而小红书的主体内容是图文
 * 笔记（`type: "normal"`），那条路对图文一律报「No video formats found」；
 * 它的 `_VALID_URL` 也不认 `xhslink.com` 短链，分享口令里的链接直接落空。
 *
 * 笔记页本身是服务端渲染的，`window.__INITIAL_STATE__` 里带着标题、文案、
 * 作者、配图与视频流地址，无需 cookie。三个约束都是实测出来的：
 *
 * 1. 必须用**桌面 UA**。移动 UA 拿到的是个空壳页面（14 万字符、没有
 *    noteDetailMap），与抖音正好相反——抖音是移动 UA 才服务端渲染。
 * 2. 必须**保留 `xsec_token`**。去掉当场 302 到 `/404`，笔记数据一点不给。
 *    所以采集用的链接得是分享面板复制出来的那个，地址栏里手抠的 id 不行。
 * 3. `__INITIAL_STATE__` 不是合法 JSON：里面有几十处裸 `undefined` 字面量。
 *
 * 代价与抖音那条路相同——依赖未公开的页面结构，小红书改版就要跟着修。
 * 解析失败会降级成可读原因，不会静默产出空条目。
 */
import fs from "fs/promises";
import path from "path";
import { PlatformParseError } from "@guizhi/shared/utils/platform-parse-error";
import { IMAGE_EXTENSIONS, MEDIA_SIZE_LIMITS } from "./media-files";
import type { ImageNoteSource } from "./image-note-entry";
import { logPlatformStructureMissing } from "./platform-parse-log";
import { downloadToTempFile, fetchHtml } from "./safe-fetch";

/**
 * 桌面 UA 是这条路的前提（移动 UA 只回壳页面），所以钉在这里而不是沿用
 * safe-fetch 的默认值——那个值改成移动端时，报错会出现在这里而不是那里。
 */
const XHS_DESKTOP_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
const INITIAL_STATE_MARKER = "window.__INITIAL_STATE__";
const UNDEFINED_LITERAL = "undefined";
/** `/explore/<id>` 与 `/discovery/item/<id>` 两种站内路径 */
const NOTE_PATH_RE = /\/(?:explore|discovery\/item)\/([\da-f]{16,})/i;
/** 与 yt-dlp 链路的 --max-filesize 300m 对齐 */
const MEDIA_MAX_BYTES = 300 * 1024 * 1024;
const TITLE_MAX_LENGTH = 120;
export const XIAOHONGSHU_LABEL = "小红书";

export interface XiaohongshuNote {
  noteId: string;
  kind: "video" | "note";
  title: string;
  /** 标题取自笔记的标题字段（作者自己写的），而不是文案首行的兜底 */
  authoredTitle: boolean;
  /** 完整文案；与标题相同时为空，避免正文里重复一遍 */
  description: string;
  author: string;
  durationSeconds: number | null;
  /** 视频的主源与备源地址（逐个降级用）；图文为空数组 */
  playUrls: string[];
  /** 图文逐张图片的镜像地址（视频笔记为空数组，封面不入库） */
  imageMirrors: string[][];
  /** 规范化来源链接，参与去重：带 token 的分享链每次都不同，必须收敛 */
  webpageUrl: string;
}

export interface XiaohongshuFetchDeps {
  /** 测试注入：抓取笔记页 */
  fetchPage?: (
    url: string,
    signal?: AbortSignal,
  ) => Promise<{ html: string; finalUrl: string }>;
}

/** 从站内链接里取笔记 ID；短链等取不到的返回 null */
export function extractXhsNoteId(url: string): string | null {
  let pathname: string;
  try {
    pathname = new URL(url.trim()).pathname;
  } catch {
    return null;
  }
  return NOTE_PATH_RE.exec(pathname)?.[1].toLowerCase() ?? null;
}

/** 笔记的规范链接：分享链带的 token 每次都不同，去重只能认这一个形态 */
export function xiaohongshuNoteUrl(noteId: string): string {
  return `https://www.xiaohongshu.com/explore/${noteId}`;
}

/**
 * 切出 `window.__INITIAL_STATE__ = {…}` 并就地补成合法 JSON。
 *
 * 按花括号配平扫描（跳过字符串字面量），顺手把**字符串外**的 `undefined`
 * 换成 `null`。必须区分串内串外：整段无脑替换会把文案里的 `undefined`
 * 一起改掉，而这是个知识库应用，正文里出现这个词再正常不过。
 */
export function sliceInitialStateJson(html: string): string | null {
  const marker = html.indexOf(INITIAL_STATE_MARKER);
  if (marker < 0) {
    return null;
  }
  const start = html.indexOf("{", marker + INITIAL_STATE_MARKER.length);
  if (start < 0) {
    return null;
  }

  const parts: string[] = [];
  let chunkStart = start;
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let index = start; index < html.length; index++) {
    const char = html[index];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === '"') {
        inString = false;
      }
      continue;
    }
    if (char === '"') {
      inString = true;
    } else if (char === "{") {
      depth++;
    } else if (char === "}") {
      if (--depth === 0) {
        parts.push(html.slice(chunkStart, index + 1));
        return parts.join("");
      }
    } else if (char === "u" && html.startsWith(UNDEFINED_LITERAL, index)) {
      parts.push(html.slice(chunkStart, index), "null");
      index += UNDEFINED_LITERAL.length - 1;
      chunkStart = index + 1;
    }
  }
  return null;
}

interface RawImage {
  urlDefault?: unknown;
  urlPre?: unknown;
  infoList?: { url?: unknown }[] | null;
}

interface RawStream {
  masterUrl?: unknown;
  backupUrls?: unknown;
  duration?: unknown;
}

interface RawNote {
  noteId?: unknown;
  type?: unknown;
  title?: unknown;
  desc?: unknown;
  user?: { nickname?: unknown; nickName?: unknown } | null;
  imageList?: RawImage[] | null;
  video?: {
    media?: { stream?: Record<string, RawStream[]> | null } | null;
    capa?: { duration?: unknown } | null;
  } | null;
}

interface RawState {
  note?: {
    firstNoteId?: unknown;
    noteDetailMap?: Record<string, { note?: RawNote } | null> | null;
  } | null;
}

function readString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** CDN 同时给 http 与 https，统一走 https */
function toHttps(url: unknown): string | null {
  const value = readString(url);
  if (!value) {
    return null;
  }
  return value.startsWith("http://")
    ? `https://${value.slice("http://".length)}`
    : value;
}

function dedupeUrls(candidates: unknown[]): string[] {
  const urls: string[] = [];
  const seen = new Set<string>();
  for (const candidate of candidates) {
    const url = toHttps(candidate);
    if (url && !seen.has(url)) {
      seen.add(url);
      urls.push(url);
    }
  }
  return urls;
}

/**
 * 话题在文案里的原始形态是 `#AI漫剧[话题]#`，界面上显示的是 `#AI漫剧`。
 * 原样留着，一篇笔记结尾会拖着十几个 `[话题]#`，正文没法看。
 */
export function cleanNoteText(text: string): string {
  return text.replace(/#([^#[\]]+)\[话题\]#/g, "#$1");
}

/** 标题兜底取文案首行：小红书的标题字段可以为空，文案却总是有的 */
function fallbackTitle(desc: string, noteId: string): string {
  const firstLine = desc.split(/\r?\n/).find((line) => line.trim())?.trim();
  if (!firstLine) {
    return `${XIAOHONGSHU_LABEL}笔记 ${noteId}`;
  }
  return firstLine.length > TITLE_MAX_LENGTH
    ? `${firstLine.slice(0, TITLE_MAX_LENGTH)}…`
    : firstLine;
}

/** 视频时长：流条目给的是毫秒，capa 给的是秒，两处都可能缺 */
function readDuration(video: RawNote["video"], streams: RawStream[]): number | null {
  for (const stream of streams) {
    if (typeof stream.duration === "number" && Number.isFinite(stream.duration)) {
      return Math.round(stream.duration / 1000);
    }
  }
  const seconds = video?.capa?.duration;
  return typeof seconds === "number" && Number.isFinite(seconds)
    ? Math.round(seconds)
    : null;
}

function throwXhsStructureMissing(html: string, message: string): never {
  logPlatformStructureMissing({
    platform: XIAOHONGSHU_LABEL,
    marker: INITIAL_STATE_MARKER,
    html,
    action: "解析小红书笔记页",
  });
  throw new PlatformParseError("structure_missing", message);
}

/** 解析笔记页里的 `__INITIAL_STATE__`；取不到笔记时抛出可读原因 */
export function parseXiaohongshuNote(
  html: string,
  urlNoteId: string | null,
): XiaohongshuNote {
  const json = sliceInitialStateJson(html);
  if (!json) {
    throwXhsStructureMissing(html, "笔记页未返回数据（小红书可能已改版）");
  }

  let state: RawState;
  try {
    state = JSON.parse(json) as RawState;
  } catch {
    throwXhsStructureMissing(
      html,
      "笔记页数据解析失败（页面结构可能已变化）",
    );
  }

  const detailMap = state.note?.noteDetailMap ?? {};
  const noteId = [
    urlNoteId,
    readString(state.note?.firstNoteId),
    Object.keys(detailMap)[0],
  ].find((candidate) => candidate && detailMap[candidate]?.note);
  const item = noteId ? detailMap[noteId]?.note : undefined;
  if (!noteId || !item) {
    throw new PlatformParseError(
      "note_unavailable",
      "页面里没有笔记内容（链接可能缺少 xsec_token 访问令牌，或笔记已被删除）。" +
        "请在小红书里用「分享 → 复制链接」重新复制完整链接。",
    );
  }

  const desc = cleanNoteText(readString(item.desc));
  const authoredTitle = cleanNoteText(readString(item.title));
  const title = authoredTitle || fallbackTitle(desc, noteId);

  const streams = Object.values(item.video?.media?.stream ?? {})
    .filter((entries): entries is RawStream[] => Array.isArray(entries))
    .flat();
  const playUrls = dedupeUrls(
    streams.flatMap((stream) => [
      stream.masterUrl,
      ...(Array.isArray(stream.backupUrls) ? stream.backupUrls : []),
    ]),
  );
  // 类型按平台自己的标注分，不按「有没有配图」——视频笔记的 imageList 装的
  // 是封面，按图片数判会把每条视频都判成图文
  const kind = readString(item.type) === "video" ? "video" : "note";

  const imageMirrors =
    kind === "video"
      ? []
      : (item.imageList ?? [])
          .map((image) =>
            dedupeUrls([
              image.urlDefault,
              ...(image.infoList ?? []).map((info) => info?.url),
              image.urlPre,
            ]),
          )
          .filter((mirrors) => mirrors.length > 0);

  return {
    noteId,
    kind,
    title,
    authoredTitle: Boolean(authoredTitle),
    description: desc === title ? "" : desc,
    author: readString(item.user?.nickname) || readString(item.user?.nickName),
    durationSeconds: kind === "video" ? readDuration(item.video, streams) : null,
    playUrls: kind === "video" ? playUrls : [],
    imageMirrors,
    webpageUrl: xiaohongshuNoteUrl(noteId),
  };
}

/**
 * 抓取并解析笔记。一次请求即可——短链（xhslink.com）由重定向自然落到
 * 带 token 的站内地址上，落地页本身就是服务端渲染好的笔记页。
 */
export async function fetchXiaohongshuNote(
  url: string,
  signal?: AbortSignal,
  deps: XiaohongshuFetchDeps = {},
): Promise<XiaohongshuNote> {
  const fetchPage =
    deps.fetchPage ??
    ((target: string, targetSignal?: AbortSignal) =>
      fetchHtml(target, targetSignal, { userAgent: XHS_DESKTOP_UA }));

  const page = await fetchPage(url, signal);
  // 缺 token / 笔记已删时小红书不报错，而是 302 到站内 404 页——那个页面同样
  // 带 __INITIAL_STATE__，不先认出来的话，报错会变成含糊的「没有笔记内容」
  if (new URL(page.finalUrl).pathname.startsWith("/404")) {
    throw new PlatformParseError(
      "token_invalid",
      "小红书拒绝了该链接（缺少 xsec_token 访问令牌，或笔记已被删除）。" +
        "请在小红书里用「分享 → 复制链接」重新复制完整链接。",
    );
  }
  return parseXiaohongshuNote(
    page.html,
    extractXhsNoteId(page.finalUrl) ?? extractXhsNoteId(url),
  );
}

/** 逐个镜像降级下载；全挂了才抛，错误里带上每个镜像的原因 */
async function downloadFromMirrors(
  mirrors: string[],
  options: { maxBytes: number; fileName: string },
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  const failures: string[] = [];
  for (const url of mirrors) {
    try {
      return await downloadToTempFile(url, {
        signal,
        userAgent: XHS_DESKTOP_UA,
        referer: "https://www.xiaohongshu.com/",
        ...options,
      });
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      failures.push(error instanceof Error ? error.message : String(error));
    }
  }
  throw new Error(failures.join("；") || "没有可用的下载地址");
}

/** 下载视频到临时目录，供转码转写使用 */
export async function downloadXiaohongshuMedia(
  playUrls: string[],
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  return downloadFromMirrors(
    playUrls,
    { maxBytes: MEDIA_MAX_BYTES, fileName: "xiaohongshu.mp4" },
    signal,
  );
}

/** 文件头 → 扩展名；取值受 IMAGE_EXTENSIONS 约束，写错的格式编译不过 */
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
    matches: (head) => head.subarray(0, 8).toString("hex") === "89504e470d0a1a0a",
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

/**
 * 按文件头判扩展名，不看 URL。
 *
 * 小红书的图片地址结尾是 `!nd_dft_wlteh_jpg_3` 这样的处理指令，**没有扩展名**，
 * 按 URL 猜会一律落到默认值上。而 OCR 的 mime 是 `imageMimeFromFileName` 按
 * 扩展名推出来的，猜错的表现是给视觉模型送一张标着 webp 的 JPEG——
 * Anthropic 会直接判非法。
 */
export async function sniffImageExtension(
  filePath: string,
): Promise<ImageExtension> {
  const handle = await fs.open(filePath, "r");
  try {
    const head = Buffer.alloc(12);
    await handle.read(head, 0, 12, 0);
    const hit = IMAGE_SIGNATURES.find((signature) => signature.matches(head));
    // 认不出来的按 JPEG 存：小红书的图实测全是 JPEG，而扩展名必须给一个
    return hit ? hit.extension : ".jpg";
  } finally {
    await handle.close();
  }
}

/** 下载一张配图并按真实格式定扩展名 */
export async function downloadXiaohongshuImage(
  mirrors: string[],
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  const downloaded = await downloadFromMirrors(
    mirrors,
    { maxBytes: MEDIA_SIZE_LIMITS.image, fileName: "image.bin" },
    signal,
  );
  const extension = await sniffImageExtension(downloaded.filePath);
  const filePath = path.join(downloaded.dir, `image${extension}`);
  await fs.rename(downloaded.filePath, filePath);
  return { dir: downloaded.dir, filePath };
}

/** 图文笔记 → 通用图文条目素材 */
export function xiaohongshuImageNoteSource(
  note: XiaohongshuNote,
): ImageNoteSource {
  return {
    platformLabel: XIAOHONGSHU_LABEL,
    title: note.title,
    // 小红书有独立的标题字段，作者写了就照用：AI 重拟只会丢掉可辨识度
    authoredTitle: note.authoredTitle,
    description: note.description,
    author: note.author,
    imageMirrors: note.imageMirrors,
    webpageUrl: note.webpageUrl,
    downloadImage: downloadXiaohongshuImage,
  };
}
