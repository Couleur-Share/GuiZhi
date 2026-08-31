import { createHash, randomUUID } from "node:crypto";
import type Database from "./adapter";
import type {
  DiscoveryCandidate,
  DiscoveryCandidateState,
  DiscoveryRun,
  DiscoveryView,
  DiscoveryViewState,
  PlatformDiscoveryItem,
  SaveDiscoveryViewInput,
} from "@guizhi/shared/types";

interface ViewRow {
  id: string;
  name: string;
  platform: DiscoveryView["platform"];
  mode: DiscoveryView["mode"];
  query: string;
  interval_minutes: DiscoveryView["intervalMinutes"];
  enabled: number;
  state: DiscoveryViewState;
  last_run_at: number | null;
  next_run_at: number | null;
  created_at: number;
  updated_at: number;
}

interface CandidateRow {
  view_id: string;
  external_id: string;
  item_json: string;
  content_hash: string | null;
  state: DiscoveryCandidateState;
  first_seen_at: number;
  last_seen_at: number;
}

interface RunRow {
  id: string;
  view_id: string;
  state: DiscoveryRun["state"];
  cursor: string | null;
  pages_scanned: number;
  candidates_found: number;
  error: string | null;
  started_at: number;
  finished_at: number | null;
  updated_at: number;
}

function toView(row: ViewRow): DiscoveryView {
  return {
    id: row.id,
    name: row.name,
    platform: row.platform,
    mode: row.mode,
    query: row.query,
    intervalMinutes: row.interval_minutes,
    enabled: row.enabled === 1,
    state: row.state,
    lastRunAt: row.last_run_at,
    nextRunAt: row.next_run_at,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function toCandidate(row: CandidateRow): DiscoveryCandidate {
  return {
    viewId: row.view_id,
    externalId: row.external_id,
    item: JSON.parse(row.item_json) as PlatformDiscoveryItem,
    contentHash: row.content_hash,
    state: row.state,
    firstSeenAt: row.first_seen_at,
    lastSeenAt: row.last_seen_at,
  };
}

function toRun(row: RunRow): DiscoveryRun {
  return {
    id: row.id,
    viewId: row.view_id,
    state: row.state,
    cursor: row.cursor,
    pagesScanned: row.pages_scanned,
    candidatesFound: row.candidates_found,
    error: row.error,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    updatedAt: row.updated_at,
  };
}

export function discoveryContentHash(item: PlatformDiscoveryItem): string {
  return createHash("sha256")
    .update(
      JSON.stringify({
        title: item.title.trim(),
        author: item.author.trim(),
        mediaType: item.mediaType,
        publishedAt: item.publishedAt ?? null,
        coverUrl: item.coverUrl ?? null,
      }),
    )
    .digest("hex");
}

export class DiscoveryDB {
  constructor(private readonly db: Database.Database) {}

  listViews(): DiscoveryView[] {
    return (this.db.all(
      "SELECT * FROM platform_discovery_views ORDER BY created_at DESC",
    ) as ViewRow[]).map(toView);
  }

  getView(id: string): DiscoveryView | null {
    const row = this.db.get(
      "SELECT * FROM platform_discovery_views WHERE id = ?",
      id,
    ) as ViewRow | undefined;
    return row ? toView(row) : null;
  }

  saveView(
    input: SaveDiscoveryViewInput,
    nextRunAt: number | null,
    now = Date.now(),
  ): DiscoveryView {
    const id = input.id ?? randomUUID();
    const enabled = input.enabled === true;
    this.db.run(
      `INSERT INTO platform_discovery_views
       (id,name,platform,mode,query,interval_minutes,enabled,state,last_run_at,next_run_at,created_at,updated_at)
       VALUES (?,?,?,?,?,?,?, ?,NULL,?,?,?)
       ON CONFLICT(id) DO UPDATE SET name=excluded.name, platform=excluded.platform,
         mode=excluded.mode, query=excluded.query, interval_minutes=excluded.interval_minutes,
         enabled=excluded.enabled,
         state=CASE WHEN platform_discovery_views.state='running' THEN 'running' ELSE excluded.state END,
         next_run_at=CASE WHEN platform_discovery_views.state='running'
           THEN platform_discovery_views.next_run_at ELSE excluded.next_run_at END,
         updated_at=excluded.updated_at`,
      id,
      input.name,
      input.platform,
      input.mode,
      input.query,
      input.intervalMinutes ?? 1440,
      enabled ? 1 : 0,
      enabled ? "ready" : "paused",
      enabled ? nextRunAt : null,
      now,
      now,
    );
    return this.getView(id)!;
  }

  deleteView(id: string): boolean {
    return this.db.run("DELETE FROM platform_discovery_views WHERE id = ?", id)
      .changes > 0;
  }

  setViewState(
    id: string,
    state: DiscoveryViewState,
    fields: { enabled?: boolean; lastRunAt?: number | null; nextRunAt?: number | null } = {},
    now = Date.now(),
  ): DiscoveryView | null {
    const current = this.getView(id);
    if (!current) return null;
    this.db.run(
      `UPDATE platform_discovery_views SET state=?, enabled=?, last_run_at=?, next_run_at=?, updated_at=?
       WHERE id=?`,
      state,
      fields.enabled === undefined ? (current.enabled ? 1 : 0) : fields.enabled ? 1 : 0,
      fields.lastRunAt === undefined ? current.lastRunAt : fields.lastRunAt,
      fields.nextRunAt === undefined ? current.nextRunAt : fields.nextRunAt,
      now,
      id,
    );
    return this.getView(id);
  }

  getResumableRun(viewId: string): DiscoveryRun | null {
    const row = this.db.get(
      `SELECT * FROM platform_discovery_runs
       WHERE view_id=? AND state IN ('running','failed','canceled') AND cursor IS NOT NULL
       ORDER BY started_at DESC LIMIT 1`,
      viewId,
    ) as RunRow | undefined;
    return row ? toRun(row) : null;
  }

  beginRun(viewId: string, cursor: string | null, now = Date.now()): DiscoveryRun {
    const id = randomUUID();
    this.db.run(
      `INSERT INTO platform_discovery_runs
       (id,view_id,state,cursor,pages_scanned,candidates_found,error,started_at,finished_at,updated_at)
       VALUES (?,?,'running',?,0,0,NULL,?,NULL,?)`,
      id,
      viewId,
      cursor,
      now,
      now,
    );
    return this.getRun(id)!;
  }

  getRun(id: string): DiscoveryRun | null {
    const row = this.db.get(
      "SELECT * FROM platform_discovery_runs WHERE id=?",
      id,
    ) as RunRow | undefined;
    return row ? toRun(row) : null;
  }

  updateRun(
    id: string,
    input: Partial<Pick<DiscoveryRun, "state" | "cursor" | "pagesScanned" | "candidatesFound" | "error">>,
    finished = false,
    now = Date.now(),
  ): DiscoveryRun {
    const current = this.getRun(id);
    if (!current) throw new Error("发现运行记录不存在");
    this.db.run(
      `UPDATE platform_discovery_runs SET state=?,cursor=?,pages_scanned=?,candidates_found=?,
       error=?,finished_at=?,updated_at=? WHERE id=?`,
      input.state ?? current.state,
      input.cursor === undefined ? current.cursor : input.cursor,
      input.pagesScanned ?? current.pagesScanned,
      input.candidatesFound ?? current.candidatesFound,
      input.error === undefined ? current.error : input.error,
      finished ? now : current.finishedAt,
      now,
      id,
    );
    return this.getRun(id)!;
  }

  listRuns(viewId: string, limit = 100): DiscoveryRun[] {
    return (this.db.all(
      "SELECT * FROM platform_discovery_runs WHERE view_id=? ORDER BY started_at DESC LIMIT ?",
      viewId,
      Math.min(Math.max(limit, 1), 100),
    ) as RunRow[]).map(toRun);
  }

  upsertCandidate(viewId: string, item: PlatformDiscoveryItem, now = Date.now()): boolean {
    const hash = discoveryContentHash(item);
    // 列表卡片缺少稳定时间和封面时，标题 + 作者不足以证明是同一内容；
    // 这时只按平台 ID 去重，避免把同名连载误合并。
    const canMatchHash = item.publishedAt != null || Boolean(item.coverUrl);
    const existing = this.db.get(
      canMatchHash
        ? `SELECT * FROM platform_discovery_candidates
           WHERE platform=? AND (external_id=? OR content_hash=?)
           ORDER BY CASE WHEN external_id=? THEN 0 ELSE 1 END LIMIT 1`
        : `SELECT * FROM platform_discovery_candidates
           WHERE platform=? AND external_id=? LIMIT 1`,
      item.platform,
      item.externalId,
      ...(canMatchHash ? [hash, item.externalId] : []),
    ) as CandidateRow | undefined;
    if (existing) {
      this.db.run(
        `UPDATE platform_discovery_candidates SET view_id=?, external_id=?, item_json=?,
         content_hash=?, last_seen_at=? WHERE view_id=? AND platform=? AND external_id=?`,
        viewId,
        item.externalId,
        JSON.stringify(item),
        hash,
        now,
        existing.view_id,
        item.platform,
        existing.external_id,
      );
      return false;
    }
    this.db.run(
      `INSERT INTO platform_discovery_candidates
       (view_id,platform,external_id,item_json,content_hash,state,first_seen_at,last_seen_at)
       VALUES (?,?,?,?,?,'new',?,?)`,
      viewId,
      item.platform,
      item.externalId,
      JSON.stringify(item),
      hash,
      now,
      now,
    );
    return true;
  }

  listCandidates(viewId: string, state?: DiscoveryCandidateState): DiscoveryCandidate[] {
    const rows = this.db.all(
      `SELECT view_id,external_id,item_json,content_hash,state,first_seen_at,last_seen_at
       FROM platform_discovery_candidates WHERE view_id=?${state ? " AND state=?" : ""}
       ORDER BY first_seen_at DESC`,
      viewId,
      ...(state ? [state] : []),
    ) as CandidateRow[];
    return rows.map(toCandidate);
  }

  setCandidateState(
    platform: PlatformDiscoveryItem["platform"],
    externalId: string,
    state: DiscoveryCandidateState,
  ): boolean {
    return this.db.run(
      "UPDATE platform_discovery_candidates SET state=? WHERE platform=? AND external_id=?",
      state,
      platform,
      externalId,
    ).changes > 0;
  }

  prune(now = Date.now()): void {
    const dismissedBefore = now - 90 * 24 * 60 * 60_000;
    this.db.run(
      "DELETE FROM platform_discovery_candidates WHERE state='dismissed' AND last_seen_at<?",
      dismissedBefore,
    );
    const views = this.db.all("SELECT id FROM platform_discovery_views") as Array<{ id: string }>;
    for (const { id } of views) {
      this.db.run(
        `DELETE FROM platform_discovery_runs WHERE id IN (
          SELECT id FROM platform_discovery_runs WHERE view_id=?
          ORDER BY started_at DESC LIMIT -1 OFFSET 100
        )`,
        id,
      );
    }
  }
}
