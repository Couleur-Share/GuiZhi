/**
 * 论坛帖子链接识别。主进程采集与渲染进程共用，保持判定一致。
 *
 * 论坛没有跨站通用的开放协议（Discourse 的 /t/{id}.json 在 linux.do 这类
 * 站点会被 Cloudflare 挡掉），只能逐站适配，因此这里是白名单式判定：
 * 认不出来的链接一律返回 null，退回通用网页抓取。
 */

export type ForumPlatform = "v2ex";

export interface ForumTarget {
  platform: ForumPlatform;
  /** 平台侧的帖子 id */
  topicId: string;
}

/** V2EX 帖子路径：/t/1227616（可带 #reply107 锚点与查询串） */
const V2EX_TOPIC_PATH = /^\/t\/(\d+)/;

export function detectForumPlatform(url: string): ForumTarget | null {
  let hostname: string;
  let pathname: string;
  try {
    const parsed = new URL(url);
    hostname = parsed.hostname.toLowerCase();
    pathname = parsed.pathname;
  } catch {
    return null;
  }

  // 必须是该域本身或其子域，`fakev2ex.com` 这类后缀碰撞不算
  if (hostname === "v2ex.com" || hostname.endsWith(".v2ex.com")) {
    const topicId = V2EX_TOPIC_PATH.exec(pathname)?.[1];
    return topicId ? { platform: "v2ex", topicId } : null;
  }

  return null;
}
