/**
 * AI 问答会话类型。
 *
 * 消息体（问题/回答/引用/步骤）由渲染进程定义并整体序列化为 JSON 存储，
 * 主进程与 DB 层不解析其内部结构，仅负责持久化。
 */

export interface AskSessionMeta {
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
}

export interface AskSessionRecord extends AskSessionMeta {
  /** 渲染进程消息数组的 JSON 序列化 */
  messagesJson: string;
}

export interface SaveAskSessionInput {
  id: string;
  title: string;
  messagesJson: string;
}
