/**
 * V2EX 帖子抓取。
 *
 * 走官方的 v1 只读接口（`/api/topics/show.json` + `/api/replies/show.json`），
 * 不解析 HTML：帖子页里回复混着头像、感谢数与楼层号，Readability 抽出来
 * 三分之一是噪音，还会漏掉一批回复；接口给的是结构化字段，作者、楼层、
 * 时间都能对上。接口无需 token，限额 600 次/小时，一次采集用两次。
 *
 * 回复接口不支持分页，总是一次返回整帖，因此不必翻页。
 */
import { fetchJson } from "./safe-fetch";

const V2EX_API_BASE = "https://www.v2ex.com/api";

/** 接口返回的帖子对象（只列用得上的字段） */
interface V2exTopic {
  id?: number;
  title?: string;
  content?: string;
  created?: number;
  replies?: number;
  url?: string;
  member?: { username?: string };
  node?: { title?: string; name?: string };
}

interface V2exReply {
  content?: string;
  created?: number;
  member?: { username?: string };
}

export interface ForumReply {
  /** 楼层号，从 1 开始 */
  floor: number;
  author: string;
  content: string;
  /** Unix 毫秒 */
  createdAt: number;
}

export interface ForumThread {
  platform: "v2ex";
  topicId: string;
  title: string;
  author: string;
  /** 节点 / 板块名 */
  node: string;
  /** Unix 毫秒 */
  createdAt: number;
  /** 平台声明的回复总数（可能与实际抓到的条数不等） */
  replyCount: number;
  /** 主楼正文 */
  content: string;
  replies: ForumReply[];
  webpageUrl: string;
}

export interface V2exDeps {
  /** 测试注入：JSON 抓取 */
  fetchJson?: <T>(url: string, signal?: AbortSignal) => Promise<T>;
}

/** 秒级时间戳转毫秒；缺失或非法时回退到当前时间 */
function toMillis(seconds: number | undefined): number {
  return typeof seconds === "number" && Number.isFinite(seconds) && seconds > 0
    ? seconds * 1000
    : Date.now();
}

function normalizeText(value: string | undefined): string {
  // 接口给的是 \r\n 混排的原始输入，统一成 \n 再交给 Markdown 组装
  return (value ?? "").replace(/\r\n?/g, "\n").trim();
}

/**
 * 把接口错误翻译成用户能据此行动的说法。
 * 采集失败的原因最终会显示在导入列表里，「HTTP 403」对用户没有意义。
 */
function describeFetchError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (message === "已取消") {
    return message;
  }
  if (message.includes("HTTP 403") || message.includes("HTTP 429")) {
    return "V2EX 接口访问受限（每小时 600 次上限，或触发了风控），稍后重试";
  }
  if (message.includes("HTTP 404")) {
    return "帖子不存在或已被删除";
  }
  return message;
}

async function request<T>(
  url: string,
  deps: V2exDeps,
  signal?: AbortSignal,
): Promise<T> {
  const get = deps.fetchJson ?? fetchJson;
  try {
    return await get<T>(url, signal);
  } catch (error) {
    const described = describeFetchError(error);
    if (described === "已取消") {
      throw error;
    }
    throw new Error(described);
  }
}

/**
 * 抓取帖子与全部回复。
 * 主楼抓不到即抛错（没有主楼的帖子没有采集价值）；回复抓取失败只记日志，
 * 主楼内容仍然值得入库。
 */
export async function fetchV2exThread(
  topicId: string,
  deps: V2exDeps = {},
  signal?: AbortSignal,
): Promise<ForumThread> {
  const topics = await request<V2exTopic[]>(
    `${V2EX_API_BASE}/topics/show.json?id=${encodeURIComponent(topicId)}`,
    deps,
    signal,
  );
  // 帖子不存在时接口返回的是 200 + 空数组，不是 404
  const topic = Array.isArray(topics) ? topics[0] : undefined;
  if (!topic) {
    throw new Error("帖子不存在、已被删除或需要登录后才能查看");
  }

  signal?.throwIfAborted();

  let replies: V2exReply[] = [];
  try {
    const fetched = await request<V2exReply[]>(
      `${V2EX_API_BASE}/replies/show.json?topic_id=${encodeURIComponent(topicId)}`,
      deps,
      signal,
    );
    replies = Array.isArray(fetched) ? fetched : [];
  } catch (error) {
    if (error instanceof Error && error.message === "已取消") {
      throw error;
    }
    // 讨论区是帖子的主要价值，但只有主楼也比整条采集失败强
    console.warn("[import] V2EX 回复抓取失败，仅保留主楼:", error);
  }

  return {
    platform: "v2ex",
    topicId,
    title: normalizeText(topic.title) || `V2EX 帖子 ${topicId}`,
    author: topic.member?.username?.trim() || "",
    node: topic.node?.title?.trim() || topic.node?.name?.trim() || "",
    createdAt: toMillis(topic.created),
    replyCount:
      typeof topic.replies === "number" ? topic.replies : replies.length,
    content: normalizeText(topic.content),
    replies: replies
      .map((reply, index) => ({
        floor: index + 1,
        author: reply.member?.username?.trim() || "",
        content: normalizeText(reply.content),
        createdAt: toMillis(reply.created),
      }))
      // 纯表情/空回复没有信息量，进了正文只会稀释总结素材
      .filter((reply) => reply.content.length > 0),
    webpageUrl: topic.url?.trim() || `https://www.v2ex.com/t/${topicId}`,
  };
}
