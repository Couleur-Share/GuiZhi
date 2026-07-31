/**
 * 论坛帖子链接识别。主进程采集与渲染进程共用，保持判定一致。
 *
 * 论坛没有跨站通用的开放协议（Discourse 的 /t/{id}.json 在 linux.do 这类
 * 站点会被 Cloudflare 挡掉），只能逐站适配，因此这里是白名单式判定：
 * 认不出来的链接一律返回 null，退回通用网页抓取。
 */

export type ForumPlatform = "v2ex" | "nga";

export interface ForumTarget {
  platform: ForumPlatform;
  /** 平台侧的帖子 id */
  topicId: string;
}

/** V2EX 帖子路径：/t/1227616（可带 #reply107 锚点与查询串） */
const V2EX_TOPIC_PATH = /^\/t\/(\d+)/;

/** NGA 读帖页：/read.php?tid=37194262（可带 fav/rand 等噪音参数） */
const NGA_READ_PATH = /\/read\.php$/i;

function isNgaHostname(hostname: string): boolean {
  return (
    hostname === "bbs.nga.cn" ||
    hostname.endsWith(".bbs.nga.cn") ||
    hostname === "ngabbs.com" ||
    hostname.endsWith(".ngabbs.com") ||
    hostname === "nga.178.com" ||
    hostname.endsWith(".nga.178.com")
  );
}

/** 规范来源链接：去掉 fav/rand 等一次性参数，保证同一帖去重稳定 */
export function ngaCanonicalUrl(topicId: string): string {
  return `https://bbs.nga.cn/read.php?tid=${topicId}`;
}

export function detectForumPlatform(url: string): ForumTarget | null {
  let hostname: string;
  let pathname: string;
  let searchParams: URLSearchParams;
  try {
    const parsed = new URL(url);
    hostname = parsed.hostname.toLowerCase();
    pathname = parsed.pathname;
    searchParams = parsed.searchParams;
  } catch {
    return null;
  }

  // 必须是该域本身或其子域，`fakev2ex.com` 这类后缀碰撞不算
  if (hostname === "v2ex.com" || hostname.endsWith(".v2ex.com")) {
    const topicId = V2EX_TOPIC_PATH.exec(pathname)?.[1];
    return topicId ? { platform: "v2ex", topicId } : null;
  }

  if (isNgaHostname(hostname) && NGA_READ_PATH.test(pathname)) {
    const tid = searchParams.get("tid")?.trim() ?? "";
    if (/^\d+$/.test(tid)) {
      return { platform: "nga", topicId: tid };
    }
  }

  return null;
}
