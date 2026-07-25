/**
 * 知识条目 DAO。
 *
 * FTS 索引内容需要中文按字分词预处理，无法用 SQL 触发器维护，
 * 因此所有写路径都在本类的同一事务内同步维护 knowledge_fts。
 * 回收站中的条目保留索引（列表查询自带 deleted_at 过滤，
 * 因此不会污染正常检索，同时让回收站范围内的搜索可用），
 * 索引仅在彻底删除时移除。
 */
import { randomUUID } from "crypto";
import type Database from "./adapter";
import { buildFtsMatchQuery, segmentTextForFts } from "./fts";
import { extractAllLocalAssetRefs } from "@guizhi/shared/utils/media-refs";
import type {
  CreateKnowledgeItemInput,
  KnowledgeCounts,
  KnowledgeItem,
  KnowledgeItemListEntry,
  KnowledgeItemListResult,
  KnowledgeItemQuery,
  KnowledgeItemStatus,
  KnowledgeSortField,
  Tag,
  TagColorKey,
  UpdateKnowledgeItemInput,
} from "@guizhi/shared/types";

interface ItemRow {
  id: string;
  title: string;
  content: string;
  summary: string | null;
  transcript: string | null;
  item_type: KnowledgeItem["itemType"];
  status: KnowledgeItemStatus;
  collection_id: string | null;
  is_favorite: number;
  is_pinned: number;
  /** 仅 get() 的联查携带；其余查询为 undefined */
  source_uri?: string | null;
  deleted_at: number | null;
  created_at: number;
  updated_at: number;
}

interface TagRow {
  id: string;
  name: string;
  color_key: TagColorKey;
  created_at: number;
  updated_at: number;
}

const LIST_DEFAULT_LIMIT = 200;
const SNIPPET_MAX_LENGTH = 160;

/** 排序字段白名单：调用方传入的键只能命中这里的固定 SQL 片段 */
const SORT_COLUMNS: Record<KnowledgeSortField, string> = {
  updatedAt: "i.updated_at",
  createdAt: "i.created_at",
  title: "i.title COLLATE NOCASE",
};

function mapTagRow(row: TagRow): Tag {
  return {
    id: row.id,
    name: row.name,
    colorKey: row.color_key,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

/** 去掉常见 Markdown 语法，生成列表用纯文本摘要。 */
export function makeSnippet(content: string): string {
  const plain = (content ?? "")
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/^\s{0,3}#{1,6}\s+/gm, "")
    .replace(/^\s{0,3}>\s?/gm, "")
    .replace(/^\s{0,3}[-*+]\s+/gm, "")
    .replace(/[*_~]{1,3}([^*_~]+)[*_~]{1,3}/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
  return plain.length > SNIPPET_MAX_LENGTH
    ? `${plain.slice(0, SNIPPET_MAX_LENGTH)}…`
    : plain;
}

function buildOrderClause(
  sortBy: KnowledgeSortField | undefined,
  sortOrder: "asc" | "desc" | undefined,
): string {
  const column = SORT_COLUMNS[sortBy ?? "updatedAt"] ?? SORT_COLUMNS.updatedAt;
  const direction = sortOrder === "asc" ? "ASC" : "DESC";
  return `ORDER BY i.is_pinned DESC, ${column} ${direction}`;
}

function normalizeTagNames(names: readonly string[] | undefined): string[] {
  if (!names) {
    return [];
  }
  const seen = new Set<string>();
  const result: string[] = [];
  for (const raw of names) {
    const name = raw.trim();
    if (!name) {
      continue;
    }
    const key = name.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(name);
  }
  return result;
}

export class KnowledgeItemDB {
  constructor(private readonly db: Database.Database) {}

  // ── 查询 ──────────────────────────────────────────────────────────────────

  list(query: KnowledgeItemQuery): KnowledgeItemListResult {
    const conditions: string[] = [];
    const params: unknown[] = [];

    switch (query.scope) {
      case "inbox":
        conditions.push("i.deleted_at IS NULL", "i.status = 'inbox'");
        break;
      case "favorites":
        conditions.push("i.deleted_at IS NULL", "i.is_favorite = 1");
        break;
      case "archived":
        conditions.push("i.deleted_at IS NULL", "i.status = 'archived'");
        break;
      case "trash":
        conditions.push("i.deleted_at IS NOT NULL");
        break;
      case "all":
      default:
        conditions.push("i.deleted_at IS NULL");
        if (!query.includeArchived) {
          conditions.push("i.status != 'archived'");
        }
        break;
    }

    if (query.collectionId) {
      conditions.push("i.collection_id = ?");
      params.push(query.collectionId);
    }
    if (query.tagId) {
      conditions.push(
        "EXISTS (SELECT 1 FROM knowledge_item_tags kit WHERE kit.item_id = i.id AND kit.tag_id = ?)",
      );
      params.push(query.tagId);
    }

    const matchQuery = query.search
      ? buildFtsMatchQuery(query.search)
      : null;

    let joinClause = "";
    let orderClause = buildOrderClause(query.sortBy, query.sortOrder);
    if (matchQuery) {
      // bm25 的权重按列序位置映射，UNINDEXED 的 item_id 也占一位，
      // 首个 0.0 是它的占位，其后依次为 title / content / tags
      joinClause = `JOIN (
        SELECT item_id, bm25(knowledge_fts, 0.0, 10.0, 1.0, 5.0) AS fts_rank
        FROM knowledge_fts
        WHERE knowledge_fts MATCH ?
      ) f ON f.item_id = i.id`;
      // 搜索态下相关度优先，忽略调用方指定的排序
      orderClause = "ORDER BY f.fts_rank ASC, i.updated_at DESC";
      params.unshift(matchQuery);
    }

    const whereClause = `WHERE ${conditions.join(" AND ")}`;
    const limit = Math.max(1, Math.min(query.limit ?? LIST_DEFAULT_LIMIT, 500));
    const offset = Math.max(0, query.offset ?? 0);

    const totalRow = this.db.get(
      `SELECT COUNT(*) AS count FROM knowledge_items i ${joinClause} ${whereClause}`,
      ...params,
    ) as { count: number } | undefined;
    const total = totalRow?.count ?? 0;

    const rows = this.db.all(
      `SELECT i.* FROM knowledge_items i ${joinClause} ${whereClause} ${orderClause} LIMIT ? OFFSET ?`,
      ...params,
      limit,
      offset,
    ) as ItemRow[];

    const tagsByItem = this.loadTagsFor(rows.map((row) => row.id));
    const entries: KnowledgeItemListEntry[] = rows.map((row) => ({
      id: row.id,
      title: row.title,
      snippet: makeSnippet(row.content),
      itemType: row.item_type,
      status: row.status,
      collectionId: row.collection_id,
      isFavorite: row.is_favorite === 1,
      isPinned: row.is_pinned === 1,
      deletedAt: row.deleted_at,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      tags: tagsByItem.get(row.id) ?? [],
    }));

    return { entries, total };
  }

  get(id: string): KnowledgeItem | null {
    const row = this.db.get(
      `SELECT i.*, (
         SELECT s.source_uri FROM source_records s
         WHERE s.item_id = i.id
         ORDER BY s.captured_at DESC LIMIT 1
       ) AS source_uri
       FROM knowledge_items i WHERE i.id = ?`,
      id,
    ) as ItemRow | undefined;
    if (!row) {
      return null;
    }
    const tags = this.loadTagsFor([row.id]).get(row.id) ?? [];
    return this.mapItemRow(row, tags);
  }

  counts(): KnowledgeCounts {
    const scopeRow = this.db.get(
      `SELECT
         SUM(CASE WHEN deleted_at IS NULL AND status = 'inbox' THEN 1 ELSE 0 END) AS inbox,
         SUM(CASE WHEN deleted_at IS NULL AND status != 'archived' THEN 1 ELSE 0 END) AS all_count,
         SUM(CASE WHEN deleted_at IS NULL AND is_favorite = 1 THEN 1 ELSE 0 END) AS favorites,
         SUM(CASE WHEN deleted_at IS NULL AND status = 'archived' THEN 1 ELSE 0 END) AS archived,
         SUM(CASE WHEN deleted_at IS NOT NULL THEN 1 ELSE 0 END) AS trash
       FROM knowledge_items`,
    ) as
      | {
          inbox: number | null;
          all_count: number | null;
          favorites: number | null;
          archived: number | null;
          trash: number | null;
        }
      | undefined;

    const byCollectionRows = this.db.all(
      `SELECT collection_id, COUNT(*) AS count FROM knowledge_items
       WHERE deleted_at IS NULL AND collection_id IS NOT NULL
       GROUP BY collection_id`,
    ) as Array<{ collection_id: string; count: number }>;

    const byTagRows = this.db.all(
      `SELECT kit.tag_id AS tag_id, COUNT(*) AS count
       FROM knowledge_item_tags kit
       JOIN knowledge_items i ON i.id = kit.item_id
       WHERE i.deleted_at IS NULL
       GROUP BY kit.tag_id`,
    ) as Array<{ tag_id: string; count: number }>;

    return {
      inbox: scopeRow?.inbox ?? 0,
      all: scopeRow?.all_count ?? 0,
      favorites: scopeRow?.favorites ?? 0,
      archived: scopeRow?.archived ?? 0,
      trash: scopeRow?.trash ?? 0,
      byCollection: Object.fromEntries(
        byCollectionRows.map((row) => [row.collection_id, row.count]),
      ),
      byTag: Object.fromEntries(
        byTagRows.map((row) => [row.tag_id, row.count]),
      ),
    };
  }

  // ── 写入 ──────────────────────────────────────────────────────────────────

  create(input: CreateKnowledgeItemInput): KnowledgeItem {
    const now = Date.now();
    const id = randomUUID();
    const tagNames = normalizeTagNames(input.tagNames);

    const run = this.db.transaction(() => {
      this.db.run(
        `INSERT INTO knowledge_items
           (id, title, content, transcript, item_type, status, collection_id, is_favorite, is_pinned, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?)`,
        id,
        input.title ?? "",
        input.content ?? "",
        input.transcript ?? null,
        input.itemType ?? "note",
        input.status ?? "inbox",
        input.collectionId ?? null,
        now,
        now,
      );
      const tags = this.replaceTags(id, tagNames, now);
      this.writeFts(id, input.title ?? "", input.content ?? "", tags);
    });
    run();

    const created = this.get(id);
    if (!created) {
      throw new Error(`Failed to load created knowledge item: ${id}`);
    }
    return created;
  }

  update(id: string, input: UpdateKnowledgeItemInput): KnowledgeItem | null {
    const existing = this.db.get(
      "SELECT * FROM knowledge_items WHERE id = ?",
      id,
    ) as ItemRow | undefined;
    if (!existing) {
      return null;
    }

    const now = Date.now();
    const next = {
      title: input.title ?? existing.title,
      content: input.content ?? existing.content,
      summary: input.summary !== undefined ? input.summary : existing.summary,
      transcript:
        input.transcript !== undefined
          ? input.transcript
          : existing.transcript,
      itemType: input.itemType ?? existing.item_type,
      status: input.status ?? existing.status,
      collectionId:
        input.collectionId !== undefined
          ? input.collectionId
          : existing.collection_id,
      isFavorite:
        input.isFavorite !== undefined
          ? input.isFavorite
          : existing.is_favorite === 1,
      isPinned:
        input.isPinned !== undefined
          ? input.isPinned
          : existing.is_pinned === 1,
    };

    const run = this.db.transaction(() => {
      this.db.run(
        `UPDATE knowledge_items SET
           title = ?, content = ?, summary = ?, transcript = ?,
           item_type = ?, status = ?, collection_id = ?,
           is_favorite = ?, is_pinned = ?, updated_at = ?
         WHERE id = ?`,
        next.title,
        next.content,
        next.summary,
        next.transcript,
        next.itemType,
        next.status,
        next.collectionId,
        next.isFavorite ? 1 : 0,
        next.isPinned ? 1 : 0,
        now,
        id,
      );

      let tags: Tag[];
      if (input.tagNames !== undefined) {
        tags = this.replaceTags(id, normalizeTagNames(input.tagNames), now);
      } else {
        tags = this.loadTagsFor([id]).get(id) ?? [];
      }

      this.writeFts(id, next.title, next.content, tags);
    });
    run();

    return this.get(id);
  }

  setStatus(ids: string[], status: KnowledgeItemStatus): number {
    if (ids.length === 0) {
      return 0;
    }
    const now = Date.now();
    let changed = 0;
    const run = this.db.transaction(() => {
      for (const id of ids) {
        changed += this.db.run(
          "UPDATE knowledge_items SET status = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
          status,
          now,
          id,
        ).changes;
      }
    });
    run();
    return changed;
  }

  moveToTrash(ids: string[]): number {
    if (ids.length === 0) {
      return 0;
    }
    const now = Date.now();
    let changed = 0;
    const run = this.db.transaction(() => {
      for (const id of ids) {
        changed += this.db.run(
          "UPDATE knowledge_items SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
          now,
          now,
          id,
        ).changes;
      }
    });
    run();
    return changed;
  }

  restore(ids: string[]): number {
    if (ids.length === 0) {
      return 0;
    }
    const now = Date.now();
    let changed = 0;
    const run = this.db.transaction(() => {
      for (const id of ids) {
        changed += this.db.run(
          "UPDATE knowledge_items SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL",
          now,
          id,
        ).changes;
      }
    });
    run();
    return changed;
  }

  deleteForever(ids: string[]): number {
    if (ids.length === 0) {
      return 0;
    }
    let changed = 0;
    const run = this.db.transaction(() => {
      for (const id of ids) {
        this.db.run("DELETE FROM knowledge_fts WHERE item_id = ?", id);
        changed += this.db.run(
          "DELETE FROM knowledge_items WHERE id = ?",
          id,
        ).changes;
      }
    });
    run();
    return changed;
  }

  /** 回收站里的全部条目 id */
  listTrashedIds(): string[] {
    const rows = this.db.all(
      "SELECT id FROM knowledge_items WHERE deleted_at IS NOT NULL",
    ) as Array<{ id: string }>;
    return rows.map((row) => row.id);
  }

  emptyTrash(): number {
    return this.deleteForever(this.listTrashedIds());
  }

  /** 这些条目正文里引用的资产文件名（彻底删除前取，用于清理磁盘） */
  listAssetRefs(ids: string[]): string[] {
    if (ids.length === 0) {
      return [];
    }
    const placeholders = ids.map(() => "?").join(", ");
    const rows = this.db.all(
      `SELECT content FROM knowledge_items WHERE id IN (${placeholders})`,
      ...ids,
    ) as Array<{ content: string }>;
    const refs = new Set<string>();
    for (const row of rows) {
      for (const ref of extractAllLocalAssetRefs(row.content)) {
        refs.add(ref);
      }
    }
    return [...refs];
  }

  /** 是否还有条目引用该资产（决定删除条目后能否清理磁盘文件） */
  isAssetReferenced(fileName: string): boolean {
    // 适配器无结果时返回 null（不是 undefined），这里只能用宽松判空
    const row = this.db.get(
      "SELECT 1 AS hit FROM knowledge_items WHERE content LIKE ? LIMIT 1",
      `%${fileName}%`,
    ) as { hit: number } | null;
    return row != null;
  }

  /**
   * 补齐缺失的 FTS 行，返回补写条数。
   *
   * v0.4.1 之前软删会把条目移出索引，那些库里的回收站条目至今没有索引行，
   * 回收站范围内搜不到。全量重建对大库太贵，这里只补缺失的。
   */
  backfillMissingFtsRows(): number {
    const rows = this.db.all(
      `SELECT * FROM knowledge_items i
       WHERE NOT EXISTS (SELECT 1 FROM knowledge_fts WHERE item_id = i.id)`,
    ) as ItemRow[];
    if (rows.length === 0) {
      return 0;
    }
    const tagsByItem = this.loadTagsFor(rows.map((row) => row.id));
    const run = this.db.transaction(() => {
      for (const row of rows) {
        this.writeFts(
          row.id,
          row.title,
          row.content,
          tagsByItem.get(row.id) ?? [],
        );
      }
    });
    run();
    return rows.length;
  }

  /** 重建整个 FTS 索引（数据修复 / 迁移后使用）。 */
  rebuildFtsIndex(): number {
    const rows = this.db.all("SELECT * FROM knowledge_items") as ItemRow[];
    const tagsByItem = this.loadTagsFor(rows.map((row) => row.id));
    const run = this.db.transaction(() => {
      this.db.run("DELETE FROM knowledge_fts");
      for (const row of rows) {
        this.writeFts(
          row.id,
          row.title,
          row.content,
          tagsByItem.get(row.id) ?? [],
        );
      }
    });
    run();
    return rows.length;
  }

  // ── 内部 ──────────────────────────────────────────────────────────────────

  private mapItemRow(row: ItemRow, tags: Tag[]): KnowledgeItem {
    return {
      id: row.id,
      title: row.title,
      content: row.content,
      summary: row.summary,
      transcript: row.transcript,
      itemType: row.item_type,
      status: row.status,
      collectionId: row.collection_id,
      isFavorite: row.is_favorite === 1,
      isPinned: row.is_pinned === 1,
      sourceUri: row.source_uri ?? null,
      deletedAt: row.deleted_at,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      tags,
    };
  }

  private loadTagsFor(itemIds: string[]): Map<string, Tag[]> {
    const result = new Map<string, Tag[]>();
    if (itemIds.length === 0) {
      return result;
    }
    const placeholders = itemIds.map(() => "?").join(", ");
    const rows = this.db.all(
      `SELECT kit.item_id AS item_id, t.*
       FROM knowledge_item_tags kit
       JOIN tags t ON t.id = kit.tag_id
       WHERE kit.item_id IN (${placeholders})
       ORDER BY t.name`,
      ...itemIds,
    ) as Array<TagRow & { item_id: string }>;
    for (const row of rows) {
      const list = result.get(row.item_id) ?? [];
      list.push(mapTagRow(row));
      result.set(row.item_id, list);
    }
    return result;
  }

  /** 全量替换条目标签；名称不存在时自动创建。返回排序后的标签列表。 */
  private replaceTags(itemId: string, tagNames: string[], now: number): Tag[] {
    this.db.run("DELETE FROM knowledge_item_tags WHERE item_id = ?", itemId);

    const tags: Tag[] = [];
    for (const name of tagNames) {
      let row = this.db.get(
        "SELECT * FROM tags WHERE LOWER(name) = LOWER(?)",
        name,
      ) as TagRow | undefined;
      if (!row) {
        const id = randomUUID();
        this.db.run(
          "INSERT INTO tags (id, name, color_key, created_at, updated_at) VALUES (?, ?, 'gray', ?, ?)",
          id,
          name,
          now,
          now,
        );
        row = this.db.get("SELECT * FROM tags WHERE id = ?", id) as TagRow;
      }
      this.db.run(
        "INSERT OR IGNORE INTO knowledge_item_tags (item_id, tag_id) VALUES (?, ?)",
        itemId,
        row.id,
      );
      tags.push(mapTagRow(row));
    }
    tags.sort((left, right) => left.name.localeCompare(right.name));
    return tags;
  }

  private writeFts(
    itemId: string,
    title: string,
    content: string,
    tags: Tag[],
  ): void {
    this.db.run("DELETE FROM knowledge_fts WHERE item_id = ?", itemId);
    this.db.run(
      "INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, ?)",
      itemId,
      segmentTextForFts(title),
      segmentTextForFts(content),
      segmentTextForFts(tags.map((tag) => tag.name).join(" ")),
    );
  }
}
