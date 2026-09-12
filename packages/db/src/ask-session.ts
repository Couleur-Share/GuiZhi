/**
 * AI 问答会话 DAO。
 *
 * 消息数组由渲染进程整体序列化为 JSON 存入 messages_json，
 * 列表只投影元数据；保存时清理结构化证据并更新本地问答全文索引。
 */
import { queryAskHistory, writeAskHistory } from "./ask-history";
import { sanitizeSessionEvidence, sanitizeEvidenceTarget } from "./ask-evidence";
import type Database from "./adapter";
import type {
  AskSessionFilter,
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
  scope: "knowledge" | "article";
  item_id: string | null;
  article_title: string | null;
  options_json: string;
}

const LIST_DEFAULT_LIMIT = 100;

export class AskSessionDB {
  constructor(private readonly db: Database.Database) {}

  list(limit = LIST_DEFAULT_LIMIT, filter: AskSessionFilter = {}): AskSessionMeta[] {
    return queryAskHistory(this.db, { ...filter, limit }).entries;
  }
  query(input: import("@guizhi/shared/types/ask").AskSessionQuery) { return queryAskHistory(this.db, input); }
  updateMeta(id: string, patch: { title?: string; pinned?: boolean }): AskSessionRecord {
    return this.db.transaction(() => {
      if (!this.get(id)) throw new Error("会话不存在");
      if (patch.title !== undefined && (!patch.title.trim() || patch.title.length > 200)) throw new Error("会话标题应为 1–200 字");
      this.db.run("INSERT OR IGNORE INTO ask_session_meta(id) VALUES(?)", id);
      if (patch.title !== undefined) this.db.run("UPDATE ask_session_meta SET custom_title=? WHERE id=?", patch.title.trim(), id);
      if (patch.pinned !== undefined) this.db.run("UPDATE ask_session_meta SET pinned=? WHERE id=?", patch.pinned ? 1 : 0, id);
      writeAskHistory(this.db, id); return this.get(id)!;
    })();
  }

  get(id: string): AskSessionRecord | null {
    const row = this.db.get(
      "SELECT * FROM ask_sessions WHERE id = ?",
      id,
    ) as SessionRow | undefined;
    if (!row) {
      return null;
    }
    const meta = this.db.get("SELECT custom_title,pinned FROM ask_session_meta WHERE id=?", id) as { custom_title: string | null; pinned: number } | undefined;
    return {
      id: row.id,
      title: meta?.custom_title ?? row.title, pinned: meta?.pinned === 1,
      messagesJson: row.messages_json,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      ...sessionOptions(row),
    };
  }

  /** 整行 upsert：不存在则创建（保留 created_at），存在则更新标题与消息 */
  save(input: SaveAskSessionInput): AskSessionRecord {
    if (input.scope && !["article", "knowledge"].includes(input.scope)) throw new Error("会话范围无效");
    if (input.scope === "article" && (!input.itemId || input.target?.itemId !== input.itemId)) throw new Error("本文会话缺少有效文章关联");
    return this.db.transaction(() => {
    const now = Date.now();
    const target = input.target && this.db.get("SELECT item_id FROM ask_evidence_clearances WHERE item_id=?", input.target.itemId)
      ? { itemId: input.target.itemId, view: input.target.view } : sanitizeEvidenceTarget(input.target);
    this.db.run(
      `INSERT INTO ask_sessions (id, title, messages_json, created_at, updated_at, scope, item_id, article_title, options_json)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         title = excluded.title,
         messages_json = excluded.messages_json,
         updated_at = excluded.updated_at, scope = excluded.scope, item_id = excluded.item_id,
         article_title = excluded.article_title, options_json = excluded.options_json`,
      input.id,
      input.title,
      sanitizeSessionEvidence(this.db, input.messagesJson),
      now,
      now,
      input.scope ?? "knowledge", input.itemId ?? null, input.articleTitle ?? null,
      JSON.stringify({ webEnabled: input.webEnabled, target }),
    );
    writeAskHistory(this.db, input.id);
    const saved = this.get(input.id);
    if (!saved) {
      throw new Error(`Failed to load saved ask session: ${input.id}`);
    }
    return saved;
    })();
  }

  delete(id: string): boolean {
    return this.db.run("DELETE FROM ask_sessions WHERE id = ?", id).changes > 0;
  }
}

function sessionOptions(row: SessionRow): Partial<AskSessionMeta> {
  if (row.scope !== "article") return {};
  const options = JSON.parse(row.options_json || "{}");
  return { scope: "article", itemId: row.item_id ?? undefined, articleTitle: row.article_title ?? undefined,
    webEnabled: options.webEnabled !== false, target: options.target };
}
