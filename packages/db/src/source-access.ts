import type Database from "./adapter";

/** 原始访问入口随来源记录保存；规范链接仍用于展示、平台归类与去重。 */
export class SourceAccessDB {
  constructor(private readonly db: Database.Database) {}

  get(itemId: string, sourceUri: string): string | null {
    const row = this.db.get(
      `SELECT access_uri FROM source_records
       WHERE item_id = ? AND source_uri = ? AND source_type = 'url'
         AND access_uri IS NOT NULL AND trim(access_uri) <> ''
       ORDER BY captured_at DESC, id DESC LIMIT 1`,
      itemId,
      sourceUri,
    ) as { access_uri: string } | undefined;
    return row?.access_uri ?? null;
  }

  /** 只刷新规范链接相同的来源，避免内容哈希判重把另一网站的链接写过来。 */
  remember(itemId: string, normalizedUri: string, accessUri: string): void {
    if (!normalizedUri || !accessUri.trim()) return;
    this.db.run(
      `UPDATE source_records SET access_uri = ?
       WHERE item_id = ? AND normalized_uri = ? AND source_type = 'url'`,
      accessUri.trim(),
      itemId,
      normalizedUri,
    );
  }
}

/**
 * 旧库只回填能明确归属的单来源条目，原链接取最近一次成功创建它的任务。
 * 多来源旧数据无法靠 item_id 区分访问入口，不猜；已有新链接也绝不覆盖。
 */
export function backfillSourceAccessUris(db: Database.Database): void {
  db.exec(`
    WITH single_source AS (
      SELECT item_id, MIN(id) AS source_id FROM source_records
      GROUP BY item_id HAVING COUNT(*) = 1
    ), candidates AS (
      SELECT s.source_id, t.source_input,
        ROW_NUMBER() OVER (
          PARTITION BY s.source_id ORDER BY t.updated_at DESC, t.created_at DESC, t.id DESC
        ) AS priority
      FROM single_source s JOIN import_tasks t ON t.result_item_id = s.item_id
      WHERE t.source_kind = 'url' AND t.status = 'completed'
        AND (t.source_input LIKE 'https://%' OR t.source_input LIKE 'http://%')
    )
    UPDATE source_records SET access_uri = (
      SELECT source_input FROM candidates WHERE source_id = source_records.id AND priority = 1
    )
    WHERE source_type = 'url' AND (access_uri IS NULL OR trim(access_uri) = '')
      AND id IN (SELECT source_id FROM candidates WHERE priority = 1);
  `);
}
