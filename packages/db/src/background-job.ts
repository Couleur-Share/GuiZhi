import type {
  BackgroundJob,
  BackgroundJobKind,
  BackgroundJobScheduleInput,
} from "@guizhi/shared/types";
import type Database from "./adapter";

interface BackgroundJobRow {
  id: string;
  kind: BackgroundJobKind;
  scope_id: string;
  state: BackgroundJob["state"];
  payload_json: string;
  interval_minutes: number | null;
  next_run_at: number | null;
  attempt: number;
  lease_owner: string | null;
  lease_until: number | null;
  last_error: string | null;
  last_success_at: number | null;
  created_at: number;
  updated_at: number;
}

function parsePayload(value: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

function toJob(row: BackgroundJobRow): BackgroundJob {
  return {
    id: row.id,
    kind: row.kind,
    scopeId: row.scope_id,
    state: row.state,
    payload: parsePayload(row.payload_json),
    intervalMinutes: row.interval_minutes,
    nextRunAt: row.next_run_at,
    attempt: row.attempt,
    leaseOwner: row.lease_owner,
    leaseUntil: row.lease_until,
    lastError: row.last_error,
    lastSuccessAt: row.last_success_at,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export class BackgroundJobDB {
  constructor(private readonly db: Database.Database) {}

  list(): BackgroundJob[] {
    return (this.db.all(
      "SELECT * FROM background_jobs ORDER BY COALESCE(next_run_at, 9223372036854775807), created_at",
    ) as BackgroundJobRow[]).map(toJob);
  }

  get(id: string): BackgroundJob | null {
    const row = this.db.get(
      "SELECT * FROM background_jobs WHERE id = ?",
      id,
    ) as BackgroundJobRow | undefined;
    return row ? toJob(row) : null;
  }

  upsert(id: string, input: BackgroundJobScheduleInput, now = Date.now()): BackgroundJob {
    const scopeId = input.scopeId ?? "";
    const enabled = input.enabled !== false;
    const state = enabled ? "scheduled" : "paused";
    const nextRunAt = enabled ? (input.nextRunAt ?? now) : null;
    this.db.run(
      `INSERT INTO background_jobs
        (id, kind, scope_id, state, payload_json, interval_minutes, next_run_at,
         attempt, lease_owner, lease_until, last_error, last_success_at, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, 0, NULL, NULL, NULL, NULL, ?, ?)
       ON CONFLICT(kind, scope_id) DO UPDATE SET
         state = CASE WHEN background_jobs.state = 'running' AND ? = 1
                      THEN background_jobs.state ELSE excluded.state END,
         payload_json = excluded.payload_json,
         interval_minutes = excluded.interval_minutes,
         next_run_at = CASE WHEN background_jobs.state = 'running' AND ? = 1
                            THEN background_jobs.next_run_at ELSE excluded.next_run_at END,
         lease_owner = CASE WHEN ? = 1 THEN background_jobs.lease_owner ELSE NULL END,
         lease_until = CASE WHEN ? = 1 THEN background_jobs.lease_until ELSE NULL END,
         updated_at = excluded.updated_at`,
      id,
      input.kind,
      scopeId,
      state,
      JSON.stringify(input.payload ?? {}),
      input.intervalMinutes ?? null,
      nextRunAt,
      now,
      now,
      enabled ? 1 : 0,
      enabled ? 1 : 0,
      enabled ? 1 : 0,
      enabled ? 1 : 0,
    );
    const row = this.db.get(
      "SELECT * FROM background_jobs WHERE kind = ? AND scope_id = ?",
      input.kind,
      scopeId,
    ) as BackgroundJobRow;
    return toJob(row);
  }

  claimDue(
    kinds: BackgroundJobKind[],
    owner: string,
    leaseMs: number,
    now = Date.now(),
  ): BackgroundJob | null {
    if (kinds.length === 0) return null;
    const placeholders = kinds.map(() => "?").join(",");
    const claim = this.db.transaction(() => {
      const row = this.db.get(
        `SELECT * FROM background_jobs
         WHERE kind IN (${placeholders})
           AND ((state IN ('scheduled','retry_wait') AND next_run_at <= ?)
             OR (state = 'running' AND lease_until <= ?))
         ORDER BY COALESCE(next_run_at, 0), created_at
         LIMIT 1`,
        ...kinds,
        now,
        now,
      ) as BackgroundJobRow | undefined;
      if (!row) return null;
      const result = this.db.run(
        `UPDATE background_jobs
         SET state = 'running', lease_owner = ?, lease_until = ?, updated_at = ?
         WHERE id = ? AND ((state IN ('scheduled','retry_wait') AND next_run_at <= ?)
           OR (state = 'running' AND lease_until <= ?))`,
        owner,
        now + leaseMs,
        now,
        row.id,
        now,
        now,
      );
      return result.changes === 1 ? this.get(row.id) : null;
    });
    return claim();
  }

  complete(
    id: string,
    owner: string,
    now = Date.now(),
    nextRunAtOverride?: number,
  ): boolean {
    const row = this.db.get(
      "SELECT interval_minutes FROM background_jobs WHERE id = ? AND state = 'running' AND lease_owner = ?",
      id,
      owner,
    ) as { interval_minutes: number | null } | undefined;
    if (!row) return false;
    const recurring = Number(row.interval_minutes) > 0;
    const nextRunAt = recurring
      ? (nextRunAtOverride ?? now + Number(row.interval_minutes) * 60_000)
      : null;
    return this.db.run(
      `UPDATE background_jobs SET state = ?, next_run_at = ?, attempt = 0,
       lease_owner = NULL, lease_until = NULL, last_error = NULL,
       last_success_at = ?, updated_at = ? WHERE id = ? AND lease_owner = ?`,
      recurring ? "scheduled" : "succeeded",
      nextRunAt,
      now,
      now,
      id,
      owner,
    ).changes === 1;
  }

  renewLease(
    id: string,
    owner: string,
    leaseMs: number,
    now = Date.now(),
  ): boolean {
    return this.db.run(
      `UPDATE background_jobs SET lease_until = ?, updated_at = ?
       WHERE id = ? AND state = 'running' AND lease_owner = ?`,
      now + leaseMs,
      now,
      id,
      owner,
    ).changes === 1;
  }

  fail(
    id: string,
    owner: string,
    error: string,
    delayMs: number | null,
    now = Date.now(),
  ): boolean {
    return this.db.run(
      `UPDATE background_jobs SET state = ?, next_run_at = ?, attempt = attempt + 1,
       lease_owner = NULL, lease_until = NULL, last_error = ?, updated_at = ?
       WHERE id = ? AND state = 'running' AND lease_owner = ?`,
      delayMs === null ? "paused" : "retry_wait",
      delayMs === null ? null : now + delayMs,
      error,
      now,
      id,
      owner,
    ).changes === 1;
  }

  setPaused(id: string, paused: boolean, now = Date.now()): boolean {
    return this.db.run(
      `UPDATE background_jobs SET state = ?, next_run_at = ?, lease_owner = NULL,
       lease_until = NULL, updated_at = ? WHERE id = ?`,
      paused ? "paused" : "scheduled",
      paused ? null : now,
      now,
      id,
    ).changes === 1;
  }

  remove(kind: BackgroundJobKind, scopeId: string): boolean {
    return this.db.run(
      "DELETE FROM background_jobs WHERE kind = ? AND scope_id = ? AND state <> 'running'",
      kind,
      scopeId,
    ).changes > 0;
  }
}
