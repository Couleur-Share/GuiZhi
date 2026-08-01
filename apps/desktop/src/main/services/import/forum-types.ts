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
  /**
   * 本楼回复的对象（NGA 楼主精选讨论用）。
   * 有摘要时讨论卡片上方展示上下文，避免只看见楼主自说自话。
   */
  replyTo?: {
    author: string;
    floor?: number;
    snippet: string;
  };
}

/** 归一化 replyTo：floor 缺省时不写进对象（避免 TS 把 `floor: undefined` 当成必填键） */
export function asForumReplyTo(r: {
  author: string;
  floor?: number;
  snippet: string;
}): NonNullable<ForumReply["replyTo"]> {
  return {
    author: r.author,
    snippet: r.snippet,
    ...(r.floor != null ? { floor: r.floor } : {}),
  };
}

/**
 * 讨论区入库策略。
 * - all：短帖（V2EX）逐楼全留
 * - op-only：长帖（NGA）正文只留楼主回复，避免镜像水楼
 */
export type ForumReplyRetention = "all" | "op-only";

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
  /**
   * 写入条目「## 讨论」的回复。
   * NGA 为楼主回复；V2EX 为全部回复。
   */
  replies: ForumReply[];
  /**
   * 交给讨论总结模型的素材；缺省则用 replies。
   * NGA 会塞入分页采样（含他人提问），比入库的楼主回复更广。
   */
  summaryReplies?: ForumReply[];
  /** 讨论区保留策略；缺省按 all */
  replyRetention?: ForumReplyRetention;
  webpageUrl: string;
  /**
   * 非致命缺口（例如部分附件图下载失败或触达上限）。
   * 条目照常入库，导入任务标成「完成（有缺失）」。
   */
  warningReason?: string;
}
