/**
 * 标签 DAO。标签名不区分大小写唯一；删除标签自动解除条目关联
 * （knowledge_item_tags 外键 ON DELETE CASCADE）。
 */
import { randomUUID } from "crypto";
import type Database from "./adapter";
import type {
  CreateTagInput,
  Tag,
  TagColorKey,
  UpdateTagInput,
} from "@guizhi/shared/types";
import { TAG_COLOR_KEYS } from "@guizhi/shared/types";

interface TagRow {
  id: string;
  name: string;
  color_key: TagColorKey;
  created_at: number;
  updated_at: number;
  item_count?: number;
}

function mapRow(row: TagRow): Tag {
  return {
    id: row.id,
    name: row.name,
    colorKey: row.color_key,
    itemCount: row.item_count,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function normalizeColorKey(value: unknown): TagColorKey {
  return TAG_COLOR_KEYS.includes(value as TagColorKey)
    ? (value as TagColorKey)
    : "gray";
}

export class TagDB {
  constructor(private readonly db: Database.Database) {}

  list(): Tag[] {
    const rows = this.db.all(
      `SELECT t.*, (
         SELECT COUNT(*) FROM knowledge_item_tags kit
         JOIN knowledge_items i ON i.id = kit.item_id
         WHERE kit.tag_id = t.id AND i.deleted_at IS NULL
       ) AS item_count
       FROM tags t
       ORDER BY t.name`,
    ) as TagRow[];
    return rows.map(mapRow);
  }

  get(id: string): Tag | null {
    const row = this.db.get("SELECT * FROM tags WHERE id = ?", id) as
      | TagRow
      | undefined;
    return row ? mapRow(row) : null;
  }

  findByName(name: string): Tag | null {
    const row = this.db.get(
      "SELECT * FROM tags WHERE LOWER(name) = LOWER(?)",
      name.trim(),
    ) as TagRow | undefined;
    return row ? mapRow(row) : null;
  }

  /** 创建标签；同名（不区分大小写）已存在时返回已有标签。 */
  create(input: CreateTagInput): Tag {
    const name = input.name.trim();
    if (!name) {
      throw new Error("标签名称不能为空");
    }
    const existing = this.findByName(name);
    if (existing) {
      return existing;
    }
    const now = Date.now();
    const id = randomUUID();
    this.db.run(
      "INSERT INTO tags (id, name, color_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
      id,
      name,
      normalizeColorKey(input.colorKey),
      now,
      now,
    );
    const created = this.get(id);
    if (!created) {
      throw new Error(`Failed to load created tag: ${id}`);
    }
    return created;
  }

  update(id: string, input: UpdateTagInput): Tag | null {
    const existing = this.db.get("SELECT * FROM tags WHERE id = ?", id) as
      | TagRow
      | undefined;
    if (!existing) {
      return null;
    }

    const name = input.name !== undefined ? input.name.trim() : existing.name;
    if (!name) {
      throw new Error("标签名称不能为空");
    }
    const conflict = this.db.get(
      "SELECT id FROM tags WHERE LOWER(name) = LOWER(?) AND id != ?",
      name,
      id,
    ) as { id: string } | undefined;
    if (conflict) {
      throw new Error(`标签「${name}」已存在`);
    }

    this.db.run(
      "UPDATE tags SET name = ?, color_key = ?, updated_at = ? WHERE id = ?",
      name,
      input.colorKey !== undefined
        ? normalizeColorKey(input.colorKey)
        : existing.color_key,
      Date.now(),
      id,
    );
    return this.get(id);
  }

  delete(id: string): boolean {
    const result = this.db.run("DELETE FROM tags WHERE id = ?", id);
    return result.changes > 0;
  }
}
