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
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import type {
  BulkUpdateKnowledgeItemsInput,
  CreateKnowledgeItemInput,
  KnowledgeCounts,
  KnowledgeFacetCountsQuery,
  KnowledgeItem,
  KnowledgeItemListEntry,
  KnowledgeItemListResult,
  KnowledgeItemQuery,
  KnowledgeReviewStatus,
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
  review_status: KnowledgeReviewStatus;
  review_reasons: string | null;
  /** 仅 get() 的联查携带；其余查询为 undefined */
  source_uri?: string | null;
  /** 仅 list() 的联查携带（来源平台列） */
  platform?: string | null;
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
/** SQLite 参数上限在不同运行时不完全一致，批量写统一留出余量。 */
const BULK_WRITE_CHUNK_SIZE = 400;
const SNIPPET_MAX_LENGTH = 160;
/**
 * 列表只取正文前这么多字用来生成摘要。
 *
 * 原来是 `SELECT i.*`：一页 20 条长转写（每条几万字）就要从磁盘读出并跨进程
 * 序列化上百万字符，最终只用掉 3200 个。留足余量是因为 makeSnippet 会先剥掉
 * 代码块与 Markdown 语法，再剥掉开头的元数据引用块（连简介可达数百字），
 * 余量不足就真的不剩几个字了。
 */
const SNIPPET_SOURCE_LENGTH = 2000;

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

/**
 * 去掉常见 Markdown 语法，生成列表用纯文本摘要。
 *
 * 采集条目开头的元数据引用块（`> 平台：… · 作者：… · 时长：…`）要先剥掉：
 * 它在列表里是重复信息——平台与类型各有专列——却能连同那条最长 300 字的简介
 * 一起占满整段摘要，正文一个字都露不出来。剥在压平**之前**：这时换行还在，
 * 边界交给现成的解析器判断，不必在压平后的文本里猜（猜错会吃掉正文开头）。
 */
export function makeSnippet(content: string): string {
  const meta = parseVideoMetaBlock(content ?? "");
  // 剥完不剩东西的（只采到元数据、正文还没生成）回退到原文：
  // 列表上显示「平台：抖音 · 作者：…」也好过一片空白
  const source = meta?.body.trim() ? meta.body : (content ?? "");
  const plain = source
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

/** 复核原因是观测信息；旧库或手工编辑出的坏 JSON 都不能拖垮条目详情。 */
function parseReviewReasons(raw: string | null): string[] {
  if (!raw) {
    return [];
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((reason): reason is string => typeof reason === "string")
      : [];
  } catch {
    return [];
  }
}

function normalizeReviewReasons(reasons: readonly string[] | undefined): string[] {
  return [...new Set((reasons ?? []).map((reason) => reason.trim()).filter(Boolean))];
}

type FacetGroup = "collection" | "tag" | "platform";

interface FacetCountSql {
  joinClause: string;
  whereClause: string;
  params: unknown[];
}

/**
 * 为一个分面组建立计数查询。
 *
 * 这是标准的 disjunctive faceting：统计「平台」时去掉平台本身的选中值，
 * 仍保留知识库、标签、范围和搜索；这样一行数字表示再点该行后会得到多少条，
 * 而不会因为自己已选就把同组其它值全部压成 0。
 */
function buildFacetCountSql(
  query: KnowledgeFacetCountsQuery,
  omit: FacetGroup,
): FacetCountSql {
  const conditions: string[] = [];
  const params: unknown[] = [];

  switch (query.scope) {
    case "uncategorized":
      conditions.push(
        "i.deleted_at IS NULL",
        "i.collection_id IS NULL",
        "i.status != 'archived'",
      );
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

  if (omit !== "collection" && query.collectionId) {
    conditions.push("i.collection_id = ?");
    params.push(query.collectionId);
  }
  if (omit !== "tag" && query.tagId) {
    conditions.push(
      "EXISTS (SELECT 1 FROM knowledge_item_tags kit WHERE kit.item_id = i.id AND kit.tag_id = ?)",
    );
    params.push(query.tagId);
  }
  if (omit !== "platform" && query.platform) {
    conditions.push(
      "EXISTS (SELECT 1 FROM source_records sr WHERE sr.item_id = i.id AND sr.platform = ?)",
    );
    params.push(query.platform);
  }

  const searchTerm = query.search?.trim() || "";
  const matchQuery = searchTerm
    ? buildFtsMatchQuery(searchTerm, query.searchMode ?? "phrase")
    : null;
  if (searchTerm && !matchQuery) {
    conditions.push("0");
  }

  const joinClause = matchQuery
    ? `JOIN (
        SELECT item_id FROM knowledge_fts
        WHERE knowledge_fts MATCH ?
      ) f ON f.item_id = i.id`
    : "";
  if (matchQuery) {
    params.unshift(matchQuery);
  }

  return {
    joinClause,
    whereClause: `WHERE ${conditions.join(" AND ")}`,
    params,
  };
}

export class KnowledgeItemDB {
  constructor(private readonly db: Database.Database) {}

  // ── 查询 ──────────────────────────────────────────────────────────────────

  list(query: KnowledgeItemQuery): KnowledgeItemListResult {
    const conditions: string[] = [];
    const params: unknown[] = [];

    switch (query.scope) {
      case "uncategorized":
        // 待整理队列：还没归入任何知识库的条目。归档的不算——归档本身就是
        // 「处理完了，别再来烦我」，再塞回待整理队列只会让它永远清不空
        conditions.push(
          "i.deleted_at IS NULL",
          "i.collection_id IS NULL",
          "i.status != 'archived'",
        );
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
    // 可访问范围（MCP 用）。与 collectionId 叠加而不是二选一：
    // 前者是「在看哪个库」，后者是「最多能看见哪些库」。
    if (query.collectionScope) {
      const { ids, includeUncategorized } = query.collectionScope;
      const branches: string[] = [];
      if (ids.length > 0) {
        branches.push(`i.collection_id IN (${ids.map(() => "?").join(", ")})`);
        params.push(...ids);
      }
      if (includeUncategorized) {
        branches.push("i.collection_id IS NULL");
      }
      // 一个都没选就是「一条都不给看」，不能退化成不过滤——那正好反了
      conditions.push(branches.length > 0 ? `(${branches.join(" OR ")})` : "0");
    }
    if (query.tagId) {
      conditions.push(
        "EXISTS (SELECT 1 FROM knowledge_item_tags kit WHERE kit.item_id = i.id AND kit.tag_id = ?)",
      );
      params.push(query.tagId);
    }
    // 来源是 1:N（旧版迁移可能给同一条目带进多条记录），用 EXISTS 而不是
    // JOIN，否则一条目会在列表里重复出现、总数也跟着虚高
    if (query.platform) {
      conditions.push(
        "EXISTS (SELECT 1 FROM source_records sr WHERE sr.item_id = i.id AND sr.platform = ?)",
      );
      params.push(query.platform);
    }

    const searchTerm = query.search?.trim() || "";
    const matchQuery = searchTerm
      ? buildFtsMatchQuery(searchTerm, query.searchMode ?? "phrase")
      : null;

    // 搜索串编译不出任何可检索内容（例如全是标点）：这是「没有命中」，
    // 不是「没有搜索」。把 null 当成后者会静默列出全库，
    // 而界面此时还显示着「按相关度排序」，用户以为这就是搜索结果。
    if (searchTerm && !matchQuery) {
      return { entries: [], total: 0 };
    }

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

    // 平台筛选是「任一来源命中」，而列表列若仍一律拿最新来源，旧库中
    // 同一条目的多来源记录会出现“筛选抖音却显示网页”的自相矛盾。筛选态
    // 优先投影实际命中的平台；未筛选时保留“最新来源”的日常浏览语义。
    const platformProjection = query.platform
      ? `(SELECT s.platform FROM source_records s
          WHERE s.item_id = i.id AND s.platform = ?
          ORDER BY s.captured_at DESC LIMIT 1)`
      : `(SELECT s.platform FROM source_records s
          WHERE s.item_id = i.id
          ORDER BY s.captured_at DESC LIMIT 1)`;
    const projectionParams = query.platform ? [query.platform] : [];

    const rows = this.db.all(
      `SELECT i.id, i.title, i.item_type, i.status, i.collection_id,
              i.is_favorite, i.is_pinned, i.deleted_at, i.created_at, i.updated_at,
              substr(i.content, 1, ${SNIPPET_SOURCE_LENGTH}) AS content,
              ${platformProjection} AS platform
       FROM knowledge_items i ${joinClause} ${whereClause} ${orderClause} LIMIT ? OFFSET ?`,
      ...projectionParams,
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
      platform: row.platform ?? null,
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

  counts(query: KnowledgeFacetCountsQuery = { scope: "all" }): KnowledgeCounts {
    const scopeRow = this.db.get(
      `SELECT
         SUM(CASE WHEN deleted_at IS NULL AND collection_id IS NULL AND status != 'archived' THEN 1 ELSE 0 END) AS uncategorized,
         SUM(CASE WHEN deleted_at IS NULL AND status != 'archived' THEN 1 ELSE 0 END) AS all_count,
         SUM(CASE WHEN deleted_at IS NULL AND is_favorite = 1 THEN 1 ELSE 0 END) AS favorites,
         SUM(CASE WHEN deleted_at IS NULL AND status = 'archived' THEN 1 ELSE 0 END) AS archived,
         SUM(CASE WHEN deleted_at IS NOT NULL THEN 1 ELSE 0 END) AS trash
       FROM knowledge_items`,
    ) as
      | {
          uncategorized: number | null;
          all_count: number | null;
          favorites: number | null;
          archived: number | null;
          trash: number | null;
        }
      | undefined;

    const collectionSql = buildFacetCountSql(query, "collection");
    const byCollectionRows = this.db.all(
      `SELECT i.collection_id, COUNT(*) AS count FROM knowledge_items i
       ${collectionSql.joinClause} ${collectionSql.whereClause}
       AND collection_id IS NOT NULL
       GROUP BY collection_id`,
      ...collectionSql.params,
    ) as Array<{ collection_id: string; count: number }>;

    const tagSql = buildFacetCountSql(query, "tag");
    const byTagRows = this.db.all(
      `SELECT facet_tag.tag_id AS tag_id, COUNT(*) AS count
       FROM knowledge_items i
       JOIN knowledge_item_tags facet_tag ON facet_tag.item_id = i.id
       ${tagSql.joinClause} ${tagSql.whereClause}
       GROUP BY facet_tag.tag_id`,
      ...tagSql.params,
    ) as Array<{ tag_id: string; count: number }>;

    const platformSql = buildFacetCountSql(query, "platform");
    const byPlatformRows = this.db.all(
      `SELECT s.platform AS platform, COUNT(DISTINCT s.item_id) AS count
       FROM source_records s
       JOIN knowledge_items i ON i.id = s.item_id
       ${platformSql.joinClause} ${platformSql.whereClause}
       AND s.platform IS NOT NULL
       GROUP BY s.platform`,
      ...platformSql.params,
    ) as Array<{ platform: string; count: number }>;

    return {
      uncategorized: scopeRow?.uncategorized ?? 0,
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
      byPlatform: Object.fromEntries(
        byPlatformRows.map((row) => [row.platform, row.count]),
      ),
    };
  }

  // ── 写入 ──────────────────────────────────────────────────────────────────

  create(input: CreateKnowledgeItemInput): KnowledgeItem {
    const now = Date.now();
    const id = randomUUID();
    const tagNames = normalizeTagNames(input.tagNames);
    const reviewReasons = normalizeReviewReasons(input.reviewReasons);

    const run = this.db.transaction(() => {
      this.db.run(
        `INSERT INTO knowledge_items
           (id, title, content, transcript, item_type, status, collection_id, is_favorite, is_pinned, review_status, review_reasons, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?, ?)`,
        id,
        input.title ?? "",
        input.content ?? "",
        input.transcript ?? null,
        input.itemType ?? "note",
        input.status ?? "active",
        input.collectionId ?? null,
        input.reviewStatus ?? (reviewReasons.length > 0 ? "needs_review" : "clear"),
        reviewReasons.length > 0 ? JSON.stringify(reviewReasons) : null,
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
    const reviewReasons =
      input.reviewReasons !== undefined
        ? normalizeReviewReasons(input.reviewReasons)
        : parseReviewReasons(existing.review_reasons);
    const next = {
      title: input.title ?? existing.title,
      content: input.content ?? existing.content,
      summary: input.summary !== undefined ? input.summary : existing.summary,
      transcript:
        input.transcript !== undefined ? input.transcript : existing.transcript,
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
      reviewStatus:
        input.reviewStatus ??
        (input.reviewReasons !== undefined && reviewReasons.length > 0
          ? "needs_review"
          : existing.review_status),
      reviewReasons,
    };

    const run = this.db.transaction(() => {
      this.db.run(
        `UPDATE knowledge_items SET
           title = ?, content = ?, summary = ?, transcript = ?,
           item_type = ?, status = ?, collection_id = ?,
           is_favorite = ?, is_pinned = ?, review_status = ?, review_reasons = ?, updated_at = ?
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
        next.reviewStatus,
        next.reviewReasons.length > 0 ? JSON.stringify(next.reviewReasons) : null,
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

  /**
   * 一个事务里改一批条目，返回实际改动的条数。
   *
   * 渲染层原来是 for 循环逐条打 IPC：100 条就是 100 次往返、100 个独立事务，
   * 每次还要重写一遍 FTS 行；中途失败没有回滚也没有提示。
   */
  bulkUpdate(ids: string[], input: BulkUpdateKnowledgeItemsInput): number {
    if (ids.length === 0) {
      return 0;
    }
    const addNames = normalizeTagNames(input.addTagNames);
    const removeNames = new Set(
      normalizeTagNames(input.removeTagNames).map((name) => name.toLowerCase()),
    );
    const touchesTags = addNames.length > 0 || removeNames.size > 0;

    const targetIds = [...new Set(ids)];
    const now = Date.now();
    let changed = 0;
    const run = this.db.transaction(() => {
      // 不涉及标签时，旧实现会为每个 id 做一次 SELECT + UPDATE。整理数百条
      // 导入内容时主进程会被同步 wasm 往返占住；普通字段可安全地集合更新。
      const assignments = ["updated_at = ?"];
      const assignmentParams: unknown[] = [now];
      if (input.collectionId !== undefined) {
        assignments.push("collection_id = ?");
        assignmentParams.push(input.collectionId);
      }
      if (input.status !== undefined) {
        assignments.push("status = ?");
        assignmentParams.push(input.status);
      }
      if (input.isFavorite !== undefined) {
        assignments.push("is_favorite = ?");
        assignmentParams.push(input.isFavorite ? 1 : 0);
      }
      if (input.isPinned !== undefined) {
        assignments.push("is_pinned = ?");
        assignmentParams.push(input.isPinned ? 1 : 0);
      }
      for (
        let start = 0;
        start < targetIds.length;
        start += BULK_WRITE_CHUNK_SIZE
      ) {
        const batch = targetIds.slice(start, start + BULK_WRITE_CHUNK_SIZE);
        changed += this.db.run(
          `UPDATE knowledge_items SET ${assignments.join(", ")}
           WHERE id IN (${batch.map(() => "?").join(", ")})`,
          ...assignmentParams,
          ...batch,
        ).changes;
      }

      if (!touchesTags) {
        return;
      }
      for (const id of targetIds) {
        const existing = this.db.get(
          "SELECT id, title, content FROM knowledge_items WHERE id = ?",
          id,
        ) as Pick<ItemRow, "id" | "title" | "content"> | undefined;
        if (!existing) {
          continue;
        }
        const current = (this.loadTagsFor([id]).get(id) ?? []).map(
          (tag) => tag.name,
        );
        const kept = current.filter(
          (name) => !removeNames.has(name.toLowerCase()),
        );
        const tags = this.replaceTags(
          id,
          normalizeTagNames([...kept, ...addNames]),
          now,
        );
        this.writeFts(id, existing.title, existing.content, tags);
      }
    });
    run();
    return changed;
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
        this.deleteFtsRow(id);
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
    return this.listReferencedAssets().has(fileName);
  }

  /**
   * 全库仍在引用的资产文件名。
   *
   * 逐个资产打一次 `content LIKE '%name%'` 会做 N 次全表扫描——删一条带 9 张
   * 配图的图文条目就是 9 次。这里换成一次带前缀条件的扫描：任何引用都必然
   * 含有协议前缀，SQL 只筛出这批正文，具体文件名交给已有的解析函数在内存里比。
   */
  listReferencedAssets(): Set<string> {
    const rows = this.db.all(
      `SELECT content FROM knowledge_items
       WHERE content LIKE '%local-image://%' OR content LIKE '%local-video://%'`,
    ) as Array<{ content: string }>;
    const referenced = new Set<string>();
    for (const row of rows) {
      for (const ref of extractAllLocalAssetRefs(row.content)) {
        referenced.add(ref);
      }
    }
    return referenced;
  }

  /**
   * 补齐缺失的 FTS 行，返回补写条数。
   *
   * v0.4.1 之前软删会把条目移出索引，那些库里的回收站条目至今没有索引行，
   * 回收站范围内搜不到。全量重建对大库太贵，这里只补缺失的。
   */
  backfillMissingFtsRows(): number {
    // 先把 rowid 映射补齐：映射表是后加的，老库里已有的索引行一条都没登记，
    // 不补的话写路径会一直走「按 item_id 全表扫」那条退路
    this.db.run(
      `INSERT OR IGNORE INTO knowledge_fts_map (item_id, fts_rowid)
       SELECT item_id, rowid FROM knowledge_fts`,
    );

    // 缺失以索引本身为准而不是映射表：映射可能指向一条已经不在的索引行
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
      this.db.run("DELETE FROM knowledge_fts_map");
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
      reviewStatus: row.review_status ?? "clear",
      reviewReasons: parseReviewReasons(row.review_reasons),
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

  /**
   * 按 rowid 删掉某条目的索引行。
   *
   * fts5 对 UNINDEXED 列的等值条件只能线性扫全表，而这是每次写入的必经步骤；
   * 映射表里没有记录时（老库尚未回填）退回按 item_id 删，保证正确性。
   */
  private deleteFtsRow(itemId: string): void {
    const mapped = this.db.get(
      "SELECT fts_rowid FROM knowledge_fts_map WHERE item_id = ?",
      itemId,
    ) as { fts_rowid: number } | undefined | null;
    if (mapped) {
      this.db.run(
        "DELETE FROM knowledge_fts WHERE rowid = ?",
        mapped.fts_rowid,
      );
      this.db.run("DELETE FROM knowledge_fts_map WHERE item_id = ?", itemId);
      return;
    }
    this.db.run("DELETE FROM knowledge_fts WHERE item_id = ?", itemId);
  }

  private writeFts(
    itemId: string,
    title: string,
    content: string,
    tags: Tag[],
  ): void {
    this.deleteFtsRow(itemId);
    const inserted = this.db.run(
      "INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, ?)",
      itemId,
      segmentTextForFts(title),
      segmentTextForFts(content),
      segmentTextForFts(tags.map((tag) => tag.name).join(" ")),
    );
    this.db.run(
      "INSERT OR REPLACE INTO knowledge_fts_map (item_id, fts_rowid) VALUES (?, ?)",
      itemId,
      Number(inserted.lastInsertRowid),
    );
  }
}
