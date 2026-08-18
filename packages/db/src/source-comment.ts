import { randomUUID } from "crypto";
import type Database from "./adapter";
import type {
  PlatformCapturePlatform,
  SourceComment,
} from "@guizhi/shared/types";

interface SourceCommentRow {
  id: string;
  item_id: string;
  platform: PlatformCapturePlatform;
  external_id: string;
  author_name: string;
  content: string;
  like_count: number;
  published_at: number | null;
  captured_at: number;
}

function mapRow(row: SourceCommentRow): SourceComment {
  return {
    id: row.id,
    itemId: row.item_id,
    platform: row.platform,
    externalId: row.external_id,
    authorName: row.author_name,
    content: row.content,
    likeCount: row.like_count,
    publishedAt: row.published_at,
    capturedAt: row.captured_at,
  };
}

export interface UpsertSourceCommentInput {
  itemId: string;
  platform: PlatformCapturePlatform;
  externalId: string;
  authorName: string;
  content: string;
  likeCount?: number;
  publishedAt?: number | null;
}

export class SourceCommentDB {
  constructor(private readonly db: Database.Database) {}

  list(itemId: string): SourceComment[] {
    return (
      this.db.all(
        `SELECT * FROM source_comments
         WHERE item_id = ? ORDER BY like_count DESC, captured_at DESC`,
        itemId,
      ) as SourceCommentRow[]
    ).map(mapRow);
  }

  upsertMany(inputs: UpsertSourceCommentInput[]): number {
    if (inputs.length === 0) return 0;
    const now = Date.now();
    const run = this.db.transaction(() => {
      for (const input of inputs) {
        this.db.run(
          `INSERT INTO source_comments
             (id, item_id, platform, external_id, author_name, content,
              like_count, published_at, captured_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT(item_id, platform, external_id) DO UPDATE SET
             author_name = excluded.author_name,
             content = excluded.content,
             like_count = excluded.like_count,
             published_at = excluded.published_at,
             captured_at = excluded.captured_at`,
          randomUUID(),
          input.itemId,
          input.platform,
          input.externalId,
          input.authorName.slice(0, 200),
          input.content.slice(0, 5000),
          Math.max(0, Math.floor(input.likeCount ?? 0)),
          input.publishedAt ?? null,
          now,
        );
      }
    });
    run();
    return inputs.length;
  }
}
