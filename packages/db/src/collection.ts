/**
 * 集合（知识库）DAO。删除集合不删条目：外键 ON DELETE SET NULL
 * 会把条目的 collection_id 置空。
 */
import { randomUUID } from "crypto";
import type Database from "./adapter";
import type {
  Collection,
  CreateCollectionInput,
  UpdateCollectionInput,
} from "@guizhi/shared/types";

interface CollectionRow {
  id: string;
  name: string;
  icon: string | null;
  sort_order: number;
  created_at: number;
  updated_at: number;
  item_count?: number;
}

function mapRow(row: CollectionRow): Collection {
  return {
    id: row.id,
    name: row.name,
    icon: row.icon,
    sortOrder: row.sort_order,
    itemCount: row.item_count,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export class CollectionDB {
  constructor(private readonly db: Database.Database) {}

  list(): Collection[] {
    const rows = this.db.all(
      `SELECT c.*, (
         SELECT COUNT(*) FROM knowledge_items i
         WHERE i.collection_id = c.id AND i.deleted_at IS NULL
       ) AS item_count
       FROM collections c
       ORDER BY c.sort_order, c.created_at`,
    ) as CollectionRow[];
    return rows.map(mapRow);
  }

  get(id: string): Collection | null {
    const row = this.db.get(
      "SELECT * FROM collections WHERE id = ?",
      id,
    ) as CollectionRow | undefined;
    return row ? mapRow(row) : null;
  }

  create(input: CreateCollectionInput): Collection {
    const name = input.name.trim();
    if (!name) {
      throw new Error("集合名称不能为空");
    }
    const now = Date.now();
    const id = randomUUID();
    const maxRow = this.db.get(
      "SELECT COALESCE(MAX(sort_order), -1) AS max_order FROM collections",
    ) as { max_order: number } | undefined;
    this.db.run(
      "INSERT INTO collections (id, name, icon, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
      id,
      name,
      input.icon ?? null,
      (maxRow?.max_order ?? -1) + 1,
      now,
      now,
    );
    const created = this.get(id);
    if (!created) {
      throw new Error(`Failed to load created collection: ${id}`);
    }
    return created;
  }

  update(id: string, input: UpdateCollectionInput): Collection | null {
    const existing = this.db.get(
      "SELECT * FROM collections WHERE id = ?",
      id,
    ) as CollectionRow | undefined;
    if (!existing) {
      return null;
    }
    const name =
      input.name !== undefined ? input.name.trim() : existing.name;
    if (!name) {
      throw new Error("集合名称不能为空");
    }
    this.db.run(
      "UPDATE collections SET name = ?, icon = ?, sort_order = ?, updated_at = ? WHERE id = ?",
      name,
      input.icon !== undefined ? input.icon : existing.icon,
      input.sortOrder !== undefined ? input.sortOrder : existing.sort_order,
      Date.now(),
      id,
    );
    return this.get(id);
  }

  delete(id: string): boolean {
    const result = this.db.run("DELETE FROM collections WHERE id = ?", id);
    return result.changes > 0;
  }
}
