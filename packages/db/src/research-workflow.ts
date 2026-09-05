import type Database from "./adapter";
import type { ResearchContext, ResearchDocument, ResearchQueryAttempt, ResearchSnapshot } from "@guizhi/shared/types";

type Payload = ResearchDocument | ResearchQueryAttempt | ResearchSnapshot;
type Table = "research_documents" | "research_attempts" | "research_snapshots";

/** Research-owned text only; never writes the user's knowledge or media assets. */
export class ResearchWorkflowDB {
  constructor(private readonly db: Database.Database) {}

  context(runId: string): ResearchContext | undefined {
    const row = this.db.get("SELECT payload FROM research_contexts WHERE run_id=?", runId) as { payload: string } | undefined;
    return row ? JSON.parse(row.payload) : undefined;
  }

  setContext(runId: string, context: ResearchContext): void {
    this.db.run("INSERT INTO research_contexts(run_id,payload) VALUES (?,?) ON CONFLICT(run_id) DO UPDATE SET payload=excluded.payload", runId, JSON.stringify(context));
    this.db.run("INSERT INTO research_series(run_id,series_id) VALUES (?,?) ON CONFLICT(run_id) DO UPDATE SET series_id=excluded.series_id", runId, context.seriesId);
  }

  patchContext(runId: string, patch: Partial<ResearchContext>): void {
    const current = this.context(runId);
    if (current) this.setContext(runId, { ...current, ...patch });
  }

  series(runId: string): string[] {
    return (this.db.all("SELECT run_id FROM research_series WHERE series_id=(SELECT series_id FROM research_series WHERE run_id=?)", runId) as { run_id: string }[]).map((row) => row.run_id);
  }

  linkSavedReport(runId: string, itemId: string): void {
    this.db.run("INSERT INTO research_saved_reports(item_id,series_id) VALUES (?,?) ON CONFLICT(item_id) DO UPDATE SET series_id=excluded.series_id", itemId, this.context(runId)?.seriesId ?? runId);
  }

  savedReportIds(seriesId: string): string[] {
    return (this.db.all("SELECT item_id FROM research_saved_reports WHERE series_id=?", seriesId) as { item_id: string }[]).map((r) => r.item_id);
  }

  private list<T extends Payload>(table: Table, runId: string): T[] {
    return (this.db.all(`SELECT payload FROM ${table} WHERE run_id=? ORDER BY rowid`, runId) as { payload: string }[]).map((row) => JSON.parse(row.payload));
  }

  attempts(runId: string): ResearchQueryAttempt[] { return this.list("research_attempts", runId); }
  documents(runId: string): ResearchDocument[] { return this.list("research_documents", runId); }
  snapshots(runId: string): ResearchSnapshot[] { return this.list("research_snapshots", runId); }

  private put(table: Table, value: Payload): void {
    if (table === "research_documents") {
      const doc = value as ResearchDocument;
      this.db.run("INSERT INTO research_documents(id,run_id,candidate_id,payload) VALUES (?,?,?,?) ON CONFLICT(run_id,candidate_id) DO UPDATE SET payload=excluded.payload", doc.id, doc.runId, doc.candidateId, JSON.stringify(doc));
    } else {
      this.db.run(`INSERT INTO ${table}(id,run_id,payload) VALUES (?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, value.id, value.runId, JSON.stringify(value));
    }
  }

  putAttempt(value: ResearchQueryAttempt): void { this.put("research_attempts", value); }
  putDocument(value: ResearchDocument): void { this.put("research_documents", value); }
  putSnapshot(value: ResearchSnapshot): void { this.put("research_snapshots", value); }

  freeze(snapshot: ResearchSnapshot): void {
    this.db.transaction(() => {
      this.putSnapshot(snapshot);
      this.patchContext(snapshot.runId, { activeReportId: snapshot.id });
    })();
  }

  recover(): void {
    const runs = this.db.all("SELECT run_id FROM research_contexts") as { run_id: string }[];
    this.db.transaction(() => {
      for (const { run_id: id } of runs) {
        this.patchContext(id, { phase: "idle", activeReportId: undefined });
        for (const doc of this.documents(id)) if (doc.status === "reading") this.putDocument({ ...doc, status: "interrupted", error: "应用退出前精读未完成" });
        for (const snapshot of this.snapshots(id)) if (snapshot.status === "generating") this.putSnapshot({ ...snapshot, status: "canceled", error: "应用退出前报告未完成" });
        for (const attempt of this.attempts(id)) if (attempt.finishedAt == null) this.putAttempt({ ...attempt, finishedAt: Date.now(), errorCode: "interrupted", error: "应用退出前采集未完成" });
      }
    })();
  }
}
