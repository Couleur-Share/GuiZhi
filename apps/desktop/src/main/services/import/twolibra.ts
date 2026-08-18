/**
 * 2Libra 帖子抓取。
 *
 * 站点是自研 Next.js 论坛，不属于 Discourse。公开帖子有两条无需登录的只读 API：
 * `/api/posts/{shortId}` 返回主楼，`/api/comments/list/flat` 按真实展示顺序分页
 * 返回全部评论。使用 flat 接口能避免楼中楼分页只按顶层楼数计算，也能让楼层号、
 * 回复对象与正文顺序保持一致。
 */
import {
  twolibraCanonicalUrl,
} from "@guizhi/shared/utils/forum-platforms";
import { normalizeForumSnippet } from "@guizhi/shared/utils/forum-note";
import {
  asForumReplyTo,
  type ForumReply,
  type ForumThread,
} from "./forum-types";
import { fetchJson } from "./safe-fetch";

const API_BASE = "https://2libra.com/api";
const COMMENT_PAGE_SIZE = 50;
/** 防止异常 total_pages 让采集无限请求；正常相当于最多一万条回复。 */
const MAX_COMMENT_PAGES = 200;
const RETRY_DELAYS_MS = [1_500, 4_000];

interface ApiEnvelope<T> {
  c?: number;
  m?: string;
  d?: T;
}

interface TwolibraPost {
  short_id?: string;
  title?: string;
  content?: string;
  author?: { username?: string };
  node?: { name?: string; slug?: string };
  created_at?: string;
  comment_count?: number;
}

interface TwolibraCommentRef {
  content?: string;
  author?: { username?: string };
  floor?: number | null;
  flat_floor?: number | null;
  is_deleted?: boolean;
}

interface TwolibraComment extends TwolibraCommentRef {
  id?: string;
  alias_id?: string | null;
  created_at?: string;
  parent?: TwolibraCommentRef | null;
  reply_comment?: TwolibraCommentRef | null;
}

interface TwolibraCommentPage {
  items?: TwolibraComment[];
  total?: number;
  total_pages?: number;
}

export interface TwolibraDeps {
  fetchJson?: <T>(url: string, signal?: AbortSignal) => Promise<T>;
  retryDelaysMs?: number[];
  commentPageSize?: number;
  maxCommentPages?: number;
}

function normalizeText(value: string | undefined): string {
  return (value ?? "").replace(/\r\n?/g, "\n").trim();
}

function toMillis(value: string | undefined): number {
  const parsed = value ? Date.parse(value) : Number.NaN;
  return Number.isFinite(parsed) ? parsed : Date.now();
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
    /HTTP (?:429|5\d\d)/.test(message) ||
    message === "请求超时" ||
    message === "连接被中断"
  );
}

function describeError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (message === "已取消") return message;
  if (message.includes("HTTP 404")) return "帖子不存在或已被删除";
  if (message.includes("HTTP 403")) return "帖子需要登录或没有访问权限";
  if (message.includes("HTTP 429")) return "2Libra 请求过于频繁，稍后重试";
  if (/HTTP 5\d\d/.test(message) || message === "请求超时") {
    return "2Libra 服务器暂时无响应，已自动重试仍未成功，稍后再试";
  }
  return message;
}

async function request<T>(
  url: string,
  deps: TwolibraDeps,
  signal?: AbortSignal,
): Promise<T> {
  const get = deps.fetchJson ?? fetchJson;
  const delays = deps.retryDelaysMs ?? RETRY_DELAYS_MS;
  let lastError: unknown;
  for (let attempt = 0; attempt <= delays.length; attempt++) {
    if (attempt > 0) await wait(delays[attempt - 1], signal);
    try {
      return await get<T>(url, signal);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message === "已取消" || !isTransientFailure(message)) {
        throw new Error(describeError(error), { cause: error });
      }
      lastError = error;
      console.warn(
        `[import] 2Libra 接口第 ${attempt + 1} 次请求失败（${message}）`,
      );
    }
  }
  throw new Error(describeError(lastError));
}

function unwrap<T>(payload: ApiEnvelope<T>, unavailable: string): T {
  if (payload?.c !== 0 || payload.d == null) {
    throw new Error(normalizeText(payload?.m) || unavailable);
  }
  return payload.d;
}

function commentAuthor(comment: TwolibraComment): string {
  return comment.author?.username?.trim() ||
    (comment.alias_id ? "匿名用户" : "未知用户");
}

function commentReplyTo(
  comment: TwolibraComment,
): ForumReply["replyTo"] {
  const parent = comment.reply_comment ?? comment.parent;
  if (!parent || parent.is_deleted) return undefined;
  const snippet = normalizeForumSnippet(normalizeText(parent.content));
  const author = parent.author?.username?.trim() || "";
  if (!snippet && !author) return undefined;
  const floor = parent.flat_floor ?? parent.floor ?? undefined;
  return asForumReplyTo({
    author: author || "未知用户",
    snippet,
    ...(floor != null ? { floor } : {}),
  });
}

function toReply(comment: TwolibraComment, fallbackFloor: number): ForumReply | null {
  if (comment.is_deleted) return null;
  const content = normalizeText(comment.content);
  if (!content) return null;
  const floor = comment.flat_floor ?? comment.floor ?? fallbackFloor;
  const replyTo = commentReplyTo(comment);
  return {
    floor,
    author: commentAuthor(comment),
    content,
    createdAt: toMillis(comment.created_at),
    ...(replyTo ? { replyTo } : {}),
  };
}

/** 抓取公开主楼和全部平铺评论；分页缺失时保留已取得内容并给出 warning。 */
export async function fetchTwolibraThread(
  topicId: string,
  deps: TwolibraDeps = {},
  signal?: AbortSignal,
): Promise<ForumThread> {
  const post = unwrap(
    await request<ApiEnvelope<TwolibraPost>>(
      `${API_BASE}/posts/${encodeURIComponent(topicId)}`,
      deps,
      signal,
    ),
    "帖子不存在、已被删除或需要登录后查看",
  );
  if (!post.title?.trim() || !post.node?.slug?.trim()) {
    throw new Error("2Libra 帖子响应缺少标题或节点信息");
  }

  const pageSize = deps.commentPageSize ?? COMMENT_PAGE_SIZE;
  const pageLimit = deps.maxCommentPages ?? MAX_COMMENT_PAGES;
  const replies: ForumReply[] = [];
  const seen = new Set<string>();
  const warnings: string[] = [];
  let totalPages = 1;
  let reportedTotal: number | undefined;

  for (let page = 1; page <= Math.min(totalPages, pageLimit); page++) {
    signal?.throwIfAborted();
    try {
      const data = unwrap(
        await request<ApiEnvelope<TwolibraCommentPage>>(
          `${API_BASE}/comments/list/flat?post_short_id=${encodeURIComponent(topicId)}&page=${page}&limit=${pageSize}`,
          deps,
          signal,
        ),
        "评论列表不可用",
      );
      totalPages = Math.max(1, data.total_pages ?? 1);
      reportedTotal = data.total ?? reportedTotal;
      for (const comment of data.items ?? []) {
        const dedupeKey = comment.id?.trim();
        if (dedupeKey && seen.has(dedupeKey)) continue;
        if (dedupeKey) seen.add(dedupeKey);
        const reply = toReply(comment, replies.length + 1);
        if (reply) replies.push(reply);
      }
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") throw error;
      const reason = error instanceof Error ? error.message : String(error);
      warnings.push(`评论第 ${page} 页抓取失败（${reason}），讨论可能不完整`);
      break;
    }
  }
  if (totalPages > pageLimit) {
    warnings.push(`评论超过 ${pageLimit * pageSize} 条安全上限，超出部分未入库`);
  }

  const shortId = post.short_id?.trim() || topicId;
  return {
    platform: "twolibra",
    topicId: shortId,
    title: post.title.trim(),
    author: post.author?.username?.trim() || "",
    node: post.node.name?.trim() || post.node.slug.trim(),
    createdAt: toMillis(post.created_at),
    replyCount: post.comment_count ?? reportedTotal ?? replies.length,
    content: normalizeText(post.content),
    replies,
    replyRetention: "all",
    webpageUrl: twolibraCanonicalUrl(post.node.slug.trim(), shortId),
    ...(warnings.length > 0 ? { warningReason: warnings.join("；") } : {}),
  };
}
