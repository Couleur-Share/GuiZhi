/**
 * AI 问答会话类型。
 *
 * 消息体（问题/回答/引用/步骤）由渲染进程定义并整体序列化为 JSON 存储，
 * DB 层解析检索文本和证据快照以构建本地索引、执行脱敏与来源清除。
 */

export interface AskSessionOptions {
  scope?: "knowledge" | "article";
  itemId?: string;
  articleTitle?: string;
  webEnabled?: boolean;
  target?: import("./article-ask").ArticleTarget;
}
export interface AskSessionFilter { scope?: "knowledge" | "article"; itemId?: string; }

export interface AskSessionMeta extends AskSessionOptions {
  pinned?: boolean;
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
}

export interface AskSessionRecord extends AskSessionMeta {
  /** 渲染进程消息数组的 JSON 序列化 */
  messagesJson: string;
}

export interface SaveAskSessionInput extends AskSessionOptions {
  id: string;
  title: string;
  messagesJson: string;
}

export interface AskSessionQuery extends AskSessionFilter { search?: string; from?: number; to?: number; cursor?: string | null; limit?: number; }
export interface AskSessionPage { entries: AskSessionMeta[]; nextCursor: string | null; articles?: { id: string; title: string }[]; }
