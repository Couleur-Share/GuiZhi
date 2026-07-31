/**
 * 论坛帖子的跨平台公共形状。V2EX / NGA 抓取器都产出这一份，
 * forum-post 组装与讨论总结只认这些字段。
 */
import type { ForumPlatform } from "@guizhi/shared/utils/forum-platforms";

export interface ForumReply {
  /** 楼层号，从 1 开始（NGA 主楼是 0，组装时不会进 replies） */
  floor: number;
  author: string;
  content: string;
  /** Unix 毫秒 */
  createdAt: number;
}

export interface ForumThread {
  platform: ForumPlatform;
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
  /**
   * 非致命缺口（例如部分附件图下载失败或触达上限）。
   * 条目照常入库，导入任务标成「完成（有缺失）」。
   */
  warningReason?: string;
}
