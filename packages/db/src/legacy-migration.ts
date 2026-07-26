/**
 * 旧版归知（.NET / EF Core）数据库一次性迁移器。
 *
 * 迁移范围：集合、标签、知识条目（含标签关联与 FTS 重建）、来源记录、
 * Wiki 四表（页面 / 链接 / 来源 / 编译指纹——指纹哈希口径与旧版一致，迁移后不触发重编）。
 * 不迁移：向量嵌入（模型不同的派生数据）、导入任务历史、AI 生成出处记录。
 *
 * 原 GUID 主键原样保留，引用关系无需重映射；目标库必须为空（防覆盖）。
 */
import type Database from "./adapter";
import { segmentTextForFts } from "./fts";

export interface LegacyMigrationStats {
  collections: number;
  tags: number;
  items: number;
  itemTags: number;
  sources: number;
  wikiPages: number;
  wikiLinks: number;
  wikiSources: number;
  wikiIngestions: number;
}

/** EF Core 的 UTC 时间文本（"2026-07-19 16:35:19.7184038"，无时区标记）→ 毫秒。 */
export function parseLegacyUtc(value: unknown): number {
  if (typeof value !== "string" || !value) {
    return Date.now();
  }
  let normalized = value.trim().replace(" ", "T");
  if (!/[zZ]|[+-]\d{2}:?\d{2}$/.test(normalized)) {
    normalized += "Z";
  }
  const parsed = Date.parse(normalized);
  return Number.isFinite(parsed) ? parsed : Date.now();
}

const ITEM_TYPES = [
  "note",
  "webpage",
  "video",
  "image",
  "audio",
  "document",
  "snippet",
] as const;

export function mapLegacyItemType(value: unknown): string {
  const index = typeof value === "number" ? value : Number(value);
  return ITEM_TYPES[index] ?? "note";
}

/**
 * 旧版 Status：0=Inbox 1=Processing 2=Ready 3=Archived 4=Failed → 新版两态。
 *
 * 只有 Archived 需要区分：旧版那几种「没处理完」的状态在新模型里不存在了，
 * 待整理与否改看条目有没有归入知识库（迁移会把旧集合一并带过来）。
 */
export function mapLegacyStatus(value: unknown): string {
  return (typeof value === "number" ? value : Number(value)) === 3
    ? "archived"
    : "active";
}

const TAG_COLORS = ["teal", "blue", "purple", "pink", "amber", "green"] as const;

export function mapLegacyTagColor(value: unknown): string {
  const index = typeof value === "number" ? value : Number(value);
  return TAG_COLORS[index] ?? "gray";
}

/** 旧版 SourceType：0=Manual 1=PlainText 2=LocalFile 3=WebUrl 4=VideoUrl。 */
export function mapLegacySourceType(value: unknown): string {
  switch (typeof value === "number" ? value : Number(value)) {
    case 2:
      return "file";
    case 3:
    case 4:
      return "url";
    default:
      return "text";
  }
}

const WIKI_KINDS = ["topic", "entity", "concept"] as const;

export function mapLegacyWikiKind(value: unknown): string {
  const index = typeof value === "number" ? value : Number(value);
  return WIKI_KINDS[index] ?? "topic";
}

interface LegacyRow {
  [column: string]: unknown;
}

function hasTable(db: Database.Database, name: string): boolean {
  return Boolean(
    db.get(
      "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?",
      name,
    ),
  );
}

function readAll(db: Database.Database, sql: string): LegacyRow[] {
  return db.all(sql) as LegacyRow[];
}

/** 目标库是否已有业务数据（有则拒绝迁移，防覆盖）。 */
export function isTargetDatabaseEmpty(db: Database.Database): boolean {
  const row = db.get("SELECT COUNT(*) AS c FROM knowledge_items") as
    | { c: number }
    | undefined;
  return (row?.c ?? 0) === 0;
}

/** 快速探测旧库条目数（迁移提示用）。 */
export function countLegacyItems(legacyDb: Database.Database): number {
  if (!hasTable(legacyDb, "KnowledgeItems")) {
    return 0;
  }
  const row = legacyDb.get(
    "SELECT COUNT(*) AS c FROM KnowledgeItems WHERE IsDeleted = 0",
  ) as { c: number } | undefined;
  return row?.c ?? 0;
}

export function migrateLegacyDatabase(
  target: Database.Database,
  legacy: Database.Database,
): LegacyMigrationStats {
  if (!hasTable(legacy, "KnowledgeItems")) {
    throw new Error("源数据库不是旧版归知的数据文件（缺少 KnowledgeItems 表）");
  }
  if (!isTargetDatabaseEmpty(target)) {
    throw new Error("当前知识库不是空的，为防止数据覆盖已取消迁移");
  }

  const stats: LegacyMigrationStats = {
    collections: 0,
    tags: 0,
    items: 0,
    itemTags: 0,
    sources: 0,
    wikiPages: 0,
    wikiLinks: 0,
    wikiSources: 0,
    wikiIngestions: 0,
  };

  const run = target.transaction(() => {
    // ── 集合（跳过软删除的） ──
    for (const row of readAll(
      legacy,
      "SELECT * FROM Collections WHERE IsDeleted = 0",
    )) {
      target.run(
        `INSERT INTO collections (id, name, icon, sort_order, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
        row.Id,
        row.Name ?? "",
        row.Icon ?? null,
        row.SortOrder ?? 0,
        parseLegacyUtc(row.CreatedAtUtc),
        parseLegacyUtc(row.UpdatedAtUtc),
      );
      stats.collections++;
    }
    const collectionIds = new Set(
      (target.all("SELECT id FROM collections") as { id: string }[]).map(
        (row) => row.id,
      ),
    );

    // ── 标签 ──
    for (const row of readAll(legacy, "SELECT * FROM Tags")) {
      target.run(
        `INSERT INTO tags (id, name, color_key, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?)`,
        row.Id,
        row.Name ?? "",
        mapLegacyTagColor(row.ColorKey),
        parseLegacyUtc(row.CreatedAtUtc),
        parseLegacyUtc(row.UpdatedAtUtc),
      );
      stats.tags++;
    }
    const tagNamesById = new Map(
      (target.all("SELECT id, name FROM tags") as { id: string; name: string }[]).map(
        (row) => [row.id, row.name],
      ),
    );

    // ── 知识条目 ──
    const itemIds = new Set<string>();
    for (const row of readAll(legacy, "SELECT * FROM KnowledgeItems")) {
      const id = String(row.Id);
      const collectionId =
        row.CollectionId && collectionIds.has(String(row.CollectionId))
          ? String(row.CollectionId)
          : null;
      const deletedAt =
        row.IsDeleted === 1 || row.IsDeleted === true
          ? parseLegacyUtc(row.DeletedAtUtc)
          : null;
      target.run(
        `INSERT INTO knowledge_items
           (id, title, content, summary, transcript, item_type, status, collection_id,
            is_favorite, is_pinned, deleted_at, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        id,
        row.Title ?? "",
        row.Content ?? "",
        row.Summary ?? null,
        row.Transcript ?? null,
        mapLegacyItemType(row.ItemType),
        mapLegacyStatus(row.Status),
        collectionId,
        row.IsFavorite ? 1 : 0,
        row.IsPinned ? 1 : 0,
        deletedAt,
        parseLegacyUtc(row.CreatedAtUtc),
        parseLegacyUtc(row.UpdatedAtUtc),
      );
      itemIds.add(id);
      stats.items++;
    }

    // ── 条目-标签关联 ──
    for (const row of readAll(legacy, "SELECT * FROM KnowledgeItemTags")) {
      const itemId = String(row.KnowledgeItemId);
      const tagId = String(row.TagId);
      if (itemIds.has(itemId) && tagNamesById.has(tagId)) {
        target.run(
          "INSERT OR IGNORE INTO knowledge_item_tags (item_id, tag_id) VALUES (?, ?)",
          itemId,
          tagId,
        );
        stats.itemTags++;
      }
    }

    // ── 来源记录（旧版：条目.SourceId → 源记录；新版：源记录.item_id → 条目） ──
    for (const row of readAll(
      legacy,
      `SELECT s.*, i.Id AS OwnerItemId FROM SourceRecords s
       JOIN KnowledgeItems i ON i.SourceId = s.Id`,
    )) {
      const itemId = String(row.OwnerItemId);
      if (!itemIds.has(itemId)) {
        continue;
      }
      target.run(
        `INSERT INTO source_records
           (id, item_id, source_type, source_uri, normalized_uri, content_hash, platform, captured_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
        row.Id,
        itemId,
        mapLegacySourceType(row.SourceType),
        row.OriginalUri ?? null,
        row.CanonicalUri ?? null,
        row.ContentHash ?? null,
        row.Platform ?? null,
        parseLegacyUtc(row.CapturedAtUtc),
      );
      stats.sources++;
    }

    // ── FTS 重建（含回收站条目，使回收站范围内可搜索） ──
    for (const row of target.all(
      `SELECT i.id, i.title, i.content,
              (SELECT GROUP_CONCAT(t.name, ' ') FROM knowledge_item_tags kit
               JOIN tags t ON t.id = kit.tag_id WHERE kit.item_id = i.id) AS tag_names
       FROM knowledge_items i`,
    ) as { id: string; title: string; content: string; tag_names: string | null }[]) {
      target.run(
        "INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, ?)",
        row.id,
        segmentTextForFts(row.title),
        segmentTextForFts(row.content),
        segmentTextForFts(row.tag_names ?? ""),
      );
    }

    // ── Wiki 四表（旧库可能没有 Wiki 表——版本较早时跳过） ──
    if (hasTable(legacy, "WikiPages")) {
      const pageIds = new Set<string>();
      for (const row of readAll(legacy, "SELECT * FROM WikiPages")) {
        const id = String(row.Id);
        target.run(
          `INSERT INTO wiki_pages
             (id, title, normalized_title, kind, summary, body, aliases_json,
              provider, model, prompt_version, generated_at, created_at, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          id,
          row.Title ?? "",
          row.NormalizedTitle ?? "",
          mapLegacyWikiKind(row.Kind),
          row.Summary ?? "",
          row.Body ?? "",
          row.AliasesJson ?? null,
          row.Provider ?? "",
          row.Model ?? "",
          row.PromptVersion ?? "",
          parseLegacyUtc(row.GeneratedAtUtc),
          parseLegacyUtc(row.CreatedAtUtc),
          parseLegacyUtc(row.UpdatedAtUtc),
        );
        pageIds.add(id);
        stats.wikiPages++;
      }

      for (const row of readAll(legacy, "SELECT * FROM WikiPageLinks")) {
        const fromId = String(row.FromPageId);
        const toId = String(row.ToPageId);
        if (pageIds.has(fromId) && pageIds.has(toId)) {
          target.run(
            "INSERT OR IGNORE INTO wiki_page_links (from_page_id, to_page_id, created_at) VALUES (?, ?, ?)",
            fromId,
            toId,
            parseLegacyUtc(row.CreatedAtUtc),
          );
          stats.wikiLinks++;
        }
      }

      for (const row of readAll(legacy, "SELECT * FROM WikiPageSources")) {
        const pageId = String(row.WikiPageId);
        const itemId = String(row.KnowledgeItemId);
        if (pageIds.has(pageId) && itemIds.has(itemId)) {
          target.run(
            "INSERT OR IGNORE INTO wiki_page_sources (page_id, item_id, created_at) VALUES (?, ?, ?)",
            pageId,
            itemId,
            parseLegacyUtc(row.CreatedAtUtc),
          );
          stats.wikiSources++;
        }
      }

      for (const row of readAll(legacy, "SELECT * FROM WikiIngestions")) {
        const itemId = String(row.KnowledgeItemId);
        if (itemIds.has(itemId)) {
          target.run(
            `INSERT OR REPLACE INTO wiki_ingestions
               (item_id, content_hash, model, prompt_version, updated_at)
             VALUES (?, ?, ?, ?, ?)`,
            itemId,
            row.ContentHash ?? "",
            row.Model ?? "",
            row.PromptVersion ?? "",
            parseLegacyUtc(row.UpdatedAtUtc),
          );
          stats.wikiIngestions++;
        }
      }
    }
  });
  run();

  return stats;
}
