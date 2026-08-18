/**
 * 白名单 Discourse 站点的公共帖子抓取核心。
 *
 * 标准接口是 `/t/topic/{id}.json`；登录用户通常能拿到原生 Markdown `raw`，
 * 匿名响应常常只有 `cooked` HTML，因此两种都要支持。站点识别、规范链接与
 * 登录态降级仍由各站包装器决定，不能把任意域名交给这里拼接口。
 */
import TurndownService from "turndown";
import type { ForumPlatform } from "@guizhi/shared/utils/forum-platforms";
import { normalizeForumSnippet } from "@guizhi/shared/utils/forum-note";
import {
  asForumReplyTo,
  type ForumReply,
  type ForumThread,
} from "./forum-types";
import { fetchJson } from "./safe-fetch";

const RETRY_DELAYS_MS = [1_500, 4_000];
/** 单次批量补拉帖子的 post_ids 上限 */
const POST_IDS_BATCH_SIZE = 30;

export interface DiscourseSite {
  platform: ForumPlatform;
  origin: string;
  label: string;
  canonicalUrl: (topicId: string) => string;
  forbiddenMessage?: string;
  unauthorizedMessage?: string;
}

interface DiscourseUser {
  username?: string;
  name?: string;
}

interface DiscoursePost {
  id?: number;
  username?: string;
  name?: string;
  created_at?: string;
  cooked?: string;
  raw?: string;
  post_number?: number;
  reply_to_post_number?: number | null;
  actions_summary?: Array<{ id?: number; count?: number }>;
}

interface DiscourseTopicDetails {
  created_by?: DiscourseUser;
  category?: { id?: number; name?: string; slug?: string };
}

interface DiscourseTopicJson {
  id?: number;
  title?: string;
  posts_count?: number;
  created_at?: string;
  category_id?: number;
  tags?: string[];
  details?: DiscourseTopicDetails;
  post_stream?: {
    posts?: DiscoursePost[];
    stream?: number[];
  };
  errors?: string[];
}

interface DiscoursePostsBatchJson {
  post_stream?: {
    posts?: DiscoursePost[];
  };
}

interface DiscourseSiteJson {
  categories?: Array<{ id?: number; name?: string }>;
}

export interface DiscourseDeps {
  fetchJson?: <T>(url: string, signal?: AbortSignal) => Promise<T>;
  /** 站点需要验证或登录时，经其专属 Electron 会话重试 */
  fetchAuthenticatedJson?: <T>(url: string, signal?: AbortSignal) => Promise<T>;
  retryDelaysMs?: number[];
}

function normalizeText(value: string | undefined): string {
  return (value ?? "").replace(/\r\n?/g, "\n").trim();
}

function topicJsonUrl(site: DiscourseSite, topicId: string): string {
  return `${site.origin}/t/topic/${encodeURIComponent(topicId)}.json`;
}

function postsBatchUrl(
  site: DiscourseSite,
  topicId: string,
  postIds: number[],
): string {
  const params = postIds
    .map((id) => `post_ids[]=${encodeURIComponent(String(id))}`)
    .join("&");
  return `${site.origin}/t/${encodeURIComponent(topicId)}/posts.json?${params}`;
}

function toMillis(iso: string | undefined): number {
  if (!iso) return Date.now();
  const parsed = Date.parse(iso);
  return Number.isFinite(parsed) ? parsed : Date.now();
}

function createTurndown(): TurndownService {
  const turndown = new TurndownService({
    headingStyle: "atx",
    codeBlockStyle: "fenced",
    bulletListMarker: "-",
  });
  turndown.remove(["script", "style", "noscript", "iframe"]);
  return turndown;
}

const turndown = createTurndown();

function postBody(post: DiscoursePost): string {
  const raw = normalizeText(post.raw);
  if (raw) return raw;
  const cooked = post.cooked?.trim();
  if (!cooked) return "";
  try {
    return normalizeText(turndown.turndown(cooked));
  } catch {
    return normalizeText(cooked.replace(/<[^>]+>/g, " "));
  }
}

function resolveAuthor(post: DiscoursePost): string {
  return post.username?.trim() || post.name?.trim() || "";
}

function isTransientFailure(message: string): boolean {
  return (
    /HTTP 5\d\d/.test(message) ||
    message === "请求超时" ||
    message === "连接被中断"
  );
}

function needsAuthenticatedFetch(message: string): boolean {
  return (
    message.includes("HTTP 403") ||
    message.includes("HTTP 401") ||
    message.includes("接口未返回 JSON")
  );
}

function describeFetchError(error: unknown, site: DiscourseSite): string {
  const message = error instanceof Error ? error.message : String(error);
  if (message === "已取消") {
    return message;
  }
  if (message.includes("HTTP 403")) {
    return (
      site.forbiddenMessage ??
      `${site.label}拒绝访问（HTTP 403），请稍后重试`
    );
  }
  if (message.includes("HTTP 401")) {
    return (
      site.unauthorizedMessage ?? `${site.label}帖子需要登录后才能查看`
    );
  }
  if (message.includes("HTTP 404")) {
    return "帖子不存在、已被删除或无权访问";
  }
  const serverError = /HTTP (5\d\d)/.exec(message);
  if (serverError) {
    return `${site.label}服务器暂时无响应（HTTP ${serverError[1]}），已自动重试仍未成功，稍后再试`;
  }
  if (message === "请求超时") {
    return `连接${site.label}超时，已自动重试仍未成功，稍后再试`;
  }
  if (message === "连接被中断") {
    return `与${site.label}的连接被中断，已自动重试仍未成功，稍后再试`;
  }
  return message;
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

async function requestJson<T>(
  url: string,
  site: DiscourseSite,
  deps: DiscourseDeps,
  signal?: AbortSignal,
): Promise<T> {
  const delays = deps.retryDelaysMs ?? RETRY_DELAYS_MS;
  let lastError: unknown;

  for (let attempt = 0; attempt <= delays.length; attempt++) {
    if (attempt > 0) {
      await wait(delays[attempt - 1], signal);
    }
    try {
      const getter = deps.fetchJson ?? fetchJson;
      return await getter<T>(url, signal);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message === "已取消") {
        throw error;
      }
      if (needsAuthenticatedFetch(message) && deps.fetchAuthenticatedJson) {
        try {
          return await deps.fetchAuthenticatedJson<T>(url, signal);
        } catch (authError) {
          const authMessage =
            authError instanceof Error ? authError.message : String(authError);
          if (authMessage === "已取消") {
            throw authError;
          }
          throw new Error(describeFetchError(authError, site), {
            cause: authError,
          });
        }
      }
      if (!isTransientFailure(message)) {
        throw new Error(describeFetchError(error, site), { cause: error });
      }
      lastError = error;
      console.warn(
        `[import] ${site.label}接口第 ${attempt + 1} 次请求失败（${message}）`,
      );
    }
  }

  throw new Error(describeFetchError(lastError, site));
}

async function loadAllPosts(
  topicId: string,
  topic: DiscourseTopicJson,
  site: DiscourseSite,
  deps: DiscourseDeps,
  signal?: AbortSignal,
): Promise<DiscoursePost[]> {
  const initial = topic.post_stream?.posts ?? [];
  const stream = topic.post_stream?.stream ?? [];
  const byId = new Map<number, DiscoursePost>();
  for (const post of initial) {
    if (typeof post.id === "number") {
      byId.set(post.id, post);
    }
  }

  const missing = stream.filter((id) => !byId.has(id));
  for (let offset = 0; offset < missing.length; offset += POST_IDS_BATCH_SIZE) {
    signal?.throwIfAborted();
    const batch = missing.slice(offset, offset + POST_IDS_BATCH_SIZE);
    if (batch.length === 0) continue;
    try {
      const payload = await requestJson<DiscoursePostsBatchJson>(
        postsBatchUrl(site, topicId, batch),
        site,
        deps,
        signal,
      );
      for (const post of payload.post_stream?.posts ?? []) {
        if (typeof post.id === "number") {
          byId.set(post.id, post);
        }
      }
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      console.warn(
        `[import] ${site.label}补拉楼层失败，已保留已获取部分:`,
        error,
      );
      break;
    }
  }

  const ordered: DiscoursePost[] = [];
  for (const id of stream) {
    const post = byId.get(id);
    if (post) ordered.push(post);
  }
  if (ordered.length === 0) {
    return [...initial].sort(
      (a, b) => (a.post_number ?? 0) - (b.post_number ?? 0),
    );
  }
  return ordered;
}

async function loadTopicWithPosts(
  topicId: string,
  site: DiscourseSite,
  deps: DiscourseDeps,
  signal?: AbortSignal,
): Promise<{ topic: DiscourseTopicJson; posts: DiscoursePost[] }> {
  const topic = await requestJson<DiscourseTopicJson>(
    topicJsonUrl(site, topicId),
    site,
    deps,
    signal,
  );
  if (topic.errors?.length) {
    throw new Error(topic.errors.join("；"));
  }
  const posts = await loadAllPosts(topicId, topic, site, deps, signal);
  if (posts.length === 0) {
    throw new Error("帖子没有可读内容");
  }
  return { topic, posts };
}

function buildReplyTo(
  post: DiscoursePost,
  postsByNumber: Map<number, DiscoursePost>,
): ForumReply["replyTo"] | undefined {
  const targetNumber = post.reply_to_post_number;
  if (targetNumber == null || targetNumber <= 0) return undefined;
  const target = postsByNumber.get(targetNumber);
  if (!target) {
    return asForumReplyTo({
      author: "",
      floor: targetNumber,
      snippet: `（${targetNumber} 楼）`,
    });
  }
  const snippet = normalizeForumSnippet(postBody(target));
  if (!snippet) {
    return asForumReplyTo({
      author: resolveAuthor(target),
      floor: targetNumber,
      snippet: `（${targetNumber} 楼）`,
    });
  }
  return asForumReplyTo({
    author: resolveAuthor(target),
    floor: targetNumber,
    snippet,
  });
}

function toForumReply(
  post: DiscoursePost,
  postsByNumber: Map<number, DiscoursePost>,
): ForumReply | null {
  const content = postBody(post);
  const replyTo = buildReplyTo(post, postsByNumber);
  if (!content && !replyTo) return null;
  return {
    floor: post.post_number ?? 0,
    author: resolveAuthor(post),
    content,
    createdAt: toMillis(post.created_at),
    replyTo,
  };
}

const categoryNamesByOrigin = new Map<string, Map<number, string>>();

async function resolveNode(
  topic: DiscourseTopicJson,
  site: DiscourseSite,
  deps: DiscourseDeps,
  signal?: AbortSignal,
): Promise<string> {
  const category = topic.details?.category?.name?.trim();
  if (category) return category;

  if (typeof topic.category_id === "number") {
    let categories = categoryNamesByOrigin.get(site.origin);
    if (!categories) {
      try {
        const getter = deps.fetchJson ?? fetchJson;
        const payload = await getter<DiscourseSiteJson>(
          `${site.origin}/site.json`,
          signal,
        );
        categories = new Map(
          (payload.categories ?? [])
            .filter(
              (entry): entry is { id: number; name?: string } =>
                typeof entry.id === "number",
            )
            .map((entry) => [entry.id, entry.name?.trim() ?? ""]),
        );
        // 测试注入的数据不写全局缓存，避免用例之间共享伪站点状态。
        if (!deps.fetchJson) {
          categoryNamesByOrigin.set(site.origin, categories);
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        if (message === "已取消") throw error;
        console.warn(`[import] ${site.label}分类信息读取失败，继续保留帖子:`, error);
      }
    }
    const categoryName = categories?.get(topic.category_id)?.trim();
    if (categoryName) return categoryName;
  }

  const tags = topic.tags?.filter(Boolean) ?? [];
  return tags.length > 0 ? tags.join(" · ") : "";
}

export async function fetchDiscourseThread(
  topicId: string,
  site: DiscourseSite,
  deps: DiscourseDeps = {},
  signal?: AbortSignal,
): Promise<ForumThread> {
  const { topic, posts } = await loadTopicWithPosts(
    topicId,
    site,
    deps,
    signal,
  );

  const postsByNumber = new Map<number, DiscoursePost>();
  for (const post of posts) {
    if (typeof post.post_number === "number") {
      postsByNumber.set(post.post_number, post);
    }
  }

  const opPost = posts[0];
  const opAuthor =
    topic.details?.created_by?.name?.trim() ||
    topic.details?.created_by?.username?.trim() ||
    resolveAuthor(opPost);

  const replies: ForumReply[] = [];
  for (const post of posts) {
    if ((post.post_number ?? 0) <= 1) continue;
    const reply = toForumReply(post, postsByNumber);
    if (reply) replies.push(reply);
  }

  const replyCount =
    typeof topic.posts_count === "number"
      ? Math.max(0, topic.posts_count - 1)
      : Math.max(0, posts.length - 1);

  return {
    platform: site.platform,
    topicId,
    title: normalizeText(topic.title) || `${site.label}帖子 ${topicId}`,
    author: opAuthor,
    node: await resolveNode(topic, site, deps, signal),
    createdAt: toMillis(topic.created_at ?? opPost.created_at),
    replyCount,
    content: postBody(opPost),
    replies,
    replyRetention: "all",
    webpageUrl: site.canonicalUrl(topicId),
  };
}
