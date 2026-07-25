/**
 * Wiki DAO（ADR 0023）：页面 / 链接 / 来源 / 编译指纹四表。
 * 编译结果经 upsertCompilation 单事务落库；页面身份是 normalized_title。
 */
import { randomUUID } from "crypto";
import type Database from "./adapter";
import type {
  WikiApplyCompilationInput,
  WikiCatalogEntry,
  WikiCompilableItem,
  WikiCompilationStatus,
  WikiGraph,
  WikiIngestion,
  WikiPage,
  WikiPageDetail,
  WikiPageKind,
  WikiPageRevision,
  WikiSourceRef,
} from "@guizhi/shared/types";

interface PageRow {
  id: string;
  title: string;
  normalized_title: string;
  kind: WikiPageKind;
  summary: string;
  body: string;
  aliases_json: string | null;
  provider: string;
  model: string;
  prompt_version: string;
  generated_at: number;
  created_at: number;
  updated_at: number;
}

interface CatalogRow {
  id: string;
  title: string;
  normalized_title: string;
  kind: WikiPageKind;
  summary: string;
  aliases_json: string | null;
  updated_at: number;
}

interface RevisionRow {
  id: string;
  page_id: string;
  title: string;
  kind: WikiPageKind;
  summary: string;
  body: string;
  aliases_json: string | null;
  model: string;
  prompt_version: string;
  created_at: number;
}

/** 每个页面保留的历史版本数 */
const REVISION_KEEP_COUNT = 10;

function mapRevision(row: RevisionRow): WikiPageRevision {
  return {
    id: row.id,
    pageId: row.page_id,
    title: row.title,
    kind: row.kind,
    summary: row.summary,
    body: row.body,
    aliasesJson: row.aliases_json,
    model: row.model,
    promptVersion: row.prompt_version,
    createdAt: row.created_at,
  };
}

function mapPage(row: PageRow): WikiPage {
  return {
    id: row.id,
    title: row.title,
    normalizedTitle: row.normalized_title,
    kind: row.kind,
    summary: row.summary,
    body: row.body,
    aliasesJson: row.aliases_json,
    provider: row.provider,
    model: row.model,
    promptVersion: row.prompt_version,
    generatedAt: row.generated_at,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function mapCatalog(row: CatalogRow): WikiCatalogEntry {
  return {
    id: row.id,
    title: row.title,
    normalizedTitle: row.normalized_title,
    kind: row.kind,
    summary: row.summary,
    aliasesJson: row.aliases_json,
    updatedAt: row.updated_at,
  };
}

const CATALOG_COLUMNS =
  "id, title, normalized_title, kind, summary, aliases_json, updated_at";

export class WikiDB {
  constructor(private readonly db: Database.Database) {}

  getCatalog(): WikiCatalogEntry[] {
    const rows = this.db.all(
      `SELECT ${CATALOG_COLUMNS} FROM wiki_pages ORDER BY updated_at DESC`,
    ) as CatalogRow[];
    return rows.map(mapCatalog);
  }

  getPage(id: string): WikiPageDetail | null {
    const row = this.db.get("SELECT * FROM wiki_pages WHERE id = ?", id) as
      | PageRow
      | undefined;
    if (!row) {
      return null;
    }

    const backlinkRows = this.db.all(
      `SELECT ${CATALOG_COLUMNS.split(", ")
        .map((column) => `p.${column}`)
        .join(", ")}
       FROM wiki_page_links l
       JOIN wiki_pages p ON p.id = l.from_page_id
       WHERE l.to_page_id = ?
       ORDER BY p.updated_at DESC`,
      id,
    ) as CatalogRow[];

    // 来源条目：软删除（回收站）中的不展示
    const sourceRows = this.db.all(
      `SELECT i.id AS item_id, i.title AS title
       FROM wiki_page_sources s
       JOIN knowledge_items i ON i.id = s.item_id
       WHERE s.page_id = ? AND i.deleted_at IS NULL
       ORDER BY s.created_at ASC`,
      id,
    ) as { item_id: string; title: string }[];
    const sources: WikiSourceRef[] = sourceRows.map((source) => ({
      itemId: source.item_id,
      title: source.title || "无标题",
    }));

    return {
      page: mapPage(row),
      backlinks: backlinkRows.map(mapCatalog),
      sources,
    };
  }

  /** 关系图谱：全部页面节点 + 页间链接（可视化用轻量投影） */
  getGraph(): WikiGraph {
    const nodes = this.db.all(
      "SELECT id, title, kind FROM wiki_pages ORDER BY updated_at DESC",
    ) as Array<{ id: string; title: string; kind: WikiPageKind }>;
    const links = this.db.all(
      "SELECT from_page_id, to_page_id FROM wiki_page_links",
    ) as Array<{ from_page_id: string; to_page_id: string }>;
    return {
      nodes,
      links: links.map((link) => ({
        source: link.from_page_id,
        target: link.to_page_id,
      })),
    };
  }

  /** 按规范化标题精确定位（[[链接]] 跳转；别名匹配由调用方基于目录完成） */
  findPageIdByNormalizedTitle(normalizedTitle: string): string | null {
    const row = this.db.get(
      "SELECT id FROM wiki_pages WHERE normalized_title = ?",
      normalizedTitle,
    ) as { id: string } | undefined;
    return row?.id ?? null;
  }

  /** 可编译条目：未删除且正文非空（含收件箱与归档） */
  listCompilableItems(): WikiCompilableItem[] {
    const rows = this.db.all(
      "SELECT id, title, content FROM knowledge_items WHERE deleted_at IS NULL AND content != ''",
    ) as { id: string; title: string; content: string }[];
    return rows.map((row) => ({
      id: row.id,
      title: row.title || "无标题",
      content: row.content,
    }));
  }

  listIngestions(): WikiIngestion[] {
    const rows = this.db.all("SELECT * FROM wiki_ingestions") as {
      item_id: string;
      content_hash: string;
      model: string;
      prompt_version: string;
      failure_count: number | null;
      next_attempt_at: number | null;
      updated_at: number;
    }[];
    return rows.map((row) => ({
      itemId: row.item_id,
      contentHash: row.content_hash,
      model: row.model,
      promptVersion: row.prompt_version,
      failureCount: row.failure_count ?? 0,
      nextAttemptAt: row.next_attempt_at ?? null,
      updatedAt: row.updated_at,
    }));
  }

  getStatus(): WikiCompilationStatus {
    const pageCount =
      (
        this.db.get("SELECT COUNT(*) AS c FROM wiki_pages") as
          | { c: number }
          | undefined
      )?.c ?? 0;
    const compiledItemCount =
      (
        this.db.get(
          `SELECT COUNT(*) AS c FROM wiki_ingestions w
           JOIN knowledge_items i ON i.id = w.item_id AND i.deleted_at IS NULL`,
        ) as { c: number } | undefined
      )?.c ?? 0;
    const eligibleItemCount =
      (
        this.db.get(
          "SELECT COUNT(*) AS c FROM knowledge_items WHERE deleted_at IS NULL AND content != ''",
        ) as { c: number } | undefined
      )?.c ?? 0;
    return { pageCount, compiledItemCount, eligibleItemCount };
  }

  /**
   * 单条目编译结果落库（单事务）：
   * 页面按 normalized_title upsert（禁止改名语义天然满足）、
   * 替换出链、补来源引用、刷新指纹。
   */
  applyCompilation(input: WikiApplyCompilationInput): void {
    const now = Date.now();
    const run = this.db.transaction(() => {
      const pageIdByNormalized = new Map<string, string>();
      for (const row of this.db.all(
        "SELECT id, normalized_title FROM wiki_pages",
      ) as { id: string; normalized_title: string }[]) {
        pageIdByNormalized.set(row.normalized_title, row.id);
      }

      // 本次 prompt 里带了完整正文的页面；只有它们才允许整体覆盖 body
      const contextPageIds = new Set(input.contextPageIds ?? []);

      // 页面 upsert
      for (const draft of input.pages) {
        const existingId = pageIdByNormalized.get(draft.normalizedTitle);
        if (existingId) {
          this.saveRevision(existingId, now);
          // 模型只看到目录里的标题和摘要就重写正文，等于凭空编一份覆盖原文。
          // 这类页面保留原 body，只更新摘要与别名，下一轮它进了上下文再重写。
          const canReplaceBody = contextPageIds.has(existingId);
          this.db.run(
            canReplaceBody
              ? `UPDATE wiki_pages SET
                   title = ?, kind = ?, summary = ?, body = ?, aliases_json = ?,
                   provider = ?, model = ?, prompt_version = ?, generated_at = ?, updated_at = ?
                 WHERE id = ?`
              : `UPDATE wiki_pages SET
                   title = ?, kind = ?, summary = ?, aliases_json = ?,
                   provider = ?, model = ?, prompt_version = ?, generated_at = ?, updated_at = ?
                 WHERE id = ?`,
            ...(canReplaceBody
              ? [
                  draft.title,
                  draft.kind,
                  draft.summary,
                  draft.body,
                  draft.aliasesJson,
                ]
              : [draft.title, draft.kind, draft.summary, draft.aliasesJson]),
            input.provider,
            input.model,
            input.promptVersion,
            now,
            now,
            existingId,
          );
        } else {
          const id = randomUUID();
          this.db.run(
            `INSERT INTO wiki_pages
               (id, title, normalized_title, kind, summary, body, aliases_json,
                provider, model, prompt_version, generated_at, created_at, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
            id,
            draft.title,
            draft.normalizedTitle,
            draft.kind,
            draft.summary,
            draft.body,
            draft.aliasesJson,
            input.provider,
            input.model,
            input.promptVersion,
            now,
            now,
            now,
          );
          pageIdByNormalized.set(draft.normalizedTitle, id);
        }
      }

      // 出链替换 + 来源引用
      for (const draft of input.pages) {
        const pageId = pageIdByNormalized.get(draft.normalizedTitle)!;
        this.db.run(
          "DELETE FROM wiki_page_links WHERE from_page_id = ?",
          pageId,
        );
        const targetIds = new Set<string>();
        for (const target of draft.linkTargets) {
          const targetId = pageIdByNormalized.get(target);
          if (targetId && targetId !== pageId) {
            targetIds.add(targetId);
          }
        }
        for (const targetId of targetIds) {
          this.db.run(
            "INSERT INTO wiki_page_links (from_page_id, to_page_id, created_at) VALUES (?, ?, ?)",
            pageId,
            targetId,
            now,
          );
        }
        this.db.run(
          "INSERT OR IGNORE INTO wiki_page_sources (page_id, item_id, created_at) VALUES (?, ?, ?)",
          pageId,
          input.itemId,
          now,
        );
      }

      // 指纹刷新（成功即清零失败计数与退避）
      this.db.run(
        `INSERT INTO wiki_ingestions
           (item_id, content_hash, model, prompt_version, failure_count, next_attempt_at, updated_at)
         VALUES (?, ?, ?, ?, 0, NULL, ?)
         ON CONFLICT(item_id) DO UPDATE SET
           content_hash = excluded.content_hash,
           model = excluded.model,
           prompt_version = excluded.prompt_version,
           failure_count = 0,
           next_attempt_at = NULL,
           updated_at = excluded.updated_at`,
        input.itemId,
        input.contentHash,
        input.model,
        input.promptVersion,
        now,
      );
    });
    run();
  }

  /**
   * 记录一次编译失败并排下次重试时间。
   *
   * prompt_version 留空表示「这条还没成功编译过」，指纹判定因此仍视其为待编译；
   * next_attempt_at 负责把它挡在退避窗口外，避免每轮都白烧两次模型调用。
   */
  recordCompilationFailure(
    itemId: string,
    contentHash: string,
    nextAttemptAt: number | null,
  ): number {
    const now = Date.now();
    const existing = this.db.get(
      "SELECT failure_count FROM wiki_ingestions WHERE item_id = ?",
      itemId,
    ) as { failure_count: number } | undefined;
    const failureCount = (existing?.failure_count ?? 0) + 1;
    this.db.run(
      `INSERT INTO wiki_ingestions
         (item_id, content_hash, model, prompt_version, failure_count, next_attempt_at, updated_at)
       VALUES (?, ?, '', '', ?, ?, ?)
       ON CONFLICT(item_id) DO UPDATE SET
         content_hash = excluded.content_hash,
         prompt_version = '',
         failure_count = excluded.failure_count,
         next_attempt_at = excluded.next_attempt_at,
         updated_at = excluded.updated_at`,
      itemId,
      contentHash,
      failureCount,
      nextAttemptAt,
      now,
    );
    return failureCount;
  }

  /** 覆盖前存一份快照；每页只保留最近 REVISION_KEEP_COUNT 份 */
  private saveRevision(pageId: string, now: number): void {
    const page = this.db.get(
      "SELECT * FROM wiki_pages WHERE id = ?",
      pageId,
    ) as PageRow | undefined;
    if (!page) {
      return;
    }
    this.db.run(
      `INSERT INTO wiki_page_revisions
         (id, page_id, title, kind, summary, body, aliases_json, model, prompt_version, created_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      randomUUID(),
      pageId,
      page.title,
      page.kind,
      page.summary,
      page.body,
      page.aliases_json,
      page.model,
      page.prompt_version,
      now,
    );
    this.db.run(
      `DELETE FROM wiki_page_revisions
       WHERE page_id = ? AND id NOT IN (
         SELECT id FROM wiki_page_revisions
         WHERE page_id = ? ORDER BY created_at DESC LIMIT ?
       )`,
      pageId,
      pageId,
      REVISION_KEEP_COUNT,
    );
  }

  listRevisions(pageId: string): WikiPageRevision[] {
    const rows = this.db.all(
      `SELECT id, page_id, title, kind, summary, body, aliases_json, model, prompt_version, created_at
       FROM wiki_page_revisions WHERE page_id = ? ORDER BY created_at DESC`,
      pageId,
    ) as RevisionRow[];
    return rows.map(mapRevision);
  }

  /** 把页面回滚到指定版本；回滚本身也会先存一份当前快照 */
  restoreRevision(revisionId: string): boolean {
    const revision = this.db.get(
      "SELECT * FROM wiki_page_revisions WHERE id = ?",
      revisionId,
    ) as RevisionRow | undefined;
    if (!revision) {
      return false;
    }
    const now = Date.now();
    const run = this.db.transaction(() => {
      this.saveRevision(revision.page_id, now);
      this.db.run(
        `UPDATE wiki_pages SET
           title = ?, kind = ?, summary = ?, body = ?, aliases_json = ?, updated_at = ?
         WHERE id = ?`,
        revision.title,
        revision.kind,
        revision.summary,
        revision.body,
        revision.aliases_json,
        now,
        revision.page_id,
      );
    });
    run();
    return true;
  }

  /** 清空 Wiki 四表（全量重建的第一步） */
  clearAll(): void {
    const run = this.db.transaction(() => {
      this.db.run("DELETE FROM wiki_page_links");
      this.db.run("DELETE FROM wiki_page_sources");
      this.db.run("DELETE FROM wiki_ingestions");
      this.db.run("DELETE FROM wiki_page_revisions");
      this.db.run("DELETE FROM wiki_pages");
    });
    run();
  }
}
