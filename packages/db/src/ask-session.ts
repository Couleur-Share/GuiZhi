/**
 * AI 问答会话 DAO。
 *
 * 消息数组由渲染进程整体序列化为 JSON 存入 messages_json，
 * 本层只做元数据投影与整行读写，不解析消息内部结构。
 */
import type Database from "./adapter";
import type {
  AskSessionMeta,
  AskSessionRecord,
  SaveAskSessionInput,
} from "@guizhi/shared/types";

interface SessionRow {
  id: string;
  title: string;
  messages_json: string;
  created_at: number;
  updated_at: number;
}

const LIST_DEFAULT_LIMIT = 100;

export class AskSessionDB {
  constructor(private readonly db: Database.Database) {}

  list(limit = LIST_DEFAULT_LIMIT): AskSessionMeta[] {
    const rows = this.db.all(
      `SELECT id, title, '' AS messages_json, created_at, updated_at
       FROM ask_sessions
       ORDER BY updated_at DESC
       LIMIT ?`,
      Math.max(1, Math.min(limit, 500)),
    ) as SessionRow[];
    return rows.map((row) => ({
      id: row.id,
      title: row.title,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
    }));
  }

  get(id: string): AskSessionRecord | null {
    const row = this.db.get(
      "SELECT * FROM ask_sessions WHERE id = ?",
      id,
    ) as SessionRow | undefined;
    if (!row) {
      return null;
    }
    return {
      id: row.id,
      title: row.title,
      messagesJson: row.messages_json,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
    };
  }

  /** 整行 upsert：不存在则创建（保留 created_at），存在则更新标题与消息 */
  save(input: SaveAskSessionInput): AskSessionRecord {
    const now = Date.now();
    this.db.run(
      `INSERT INTO ask_sessions (id, title, messages_json, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         title = excluded.title,
         messages_json = excluded.messages_json,
         updated_at = excluded.updated_at`,
      input.id,
      input.title,
      input.messagesJson,
      now,
      now,
    );
    const saved = this.get(input.id);
    if (!saved) {
      throw new Error(`Failed to load saved ask session: ${input.id}`);
    }
    return saved;
  }

  delete(id: string): boolean {
    return this.db.run("DELETE FROM ask_sessions WHERE id = ?", id).changes > 0;
  }
}
