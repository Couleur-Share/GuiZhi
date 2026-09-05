import { ResearchWorkflowDB } from "./research-workflow";
import { randomUUID } from "node:crypto";
import type Database from "./adapter";
import type {
  CreateResearchRunInput,
  ResearchCandidate,
  ResearchCandidateInput,
  ResearchCluster,
  ResearchRun,
  ResearchRunDetail,
  ResearchSource,
  ResearchSourceRun,
  ResearchSourceStatus,
} from "@guizhi/shared/types";

interface RunRow {
  id: string;
  topic: string;
  day_range: ResearchRun["dayRange"];
  range_from: number;
  range_to: number;
  depth: ResearchRun["depth"];
  sources_json: string;
  status: ResearchRun["status"];
  report_status: ResearchRun["reportStatus"];
  report_markdown: string | null;
  report_error: string | null;
  report_prompt_version: string | null;
  saved_item_id: string | null;
  created_at: number;
  updated_at: number;
  completed_at: number | null;
  candidate_count: number;
  cluster_count: number;
}

interface SourceRow {
  run_id: string;
  source: ResearchSource;
  status: ResearchSourceStatus;
  method: string;
  collected_count: number;
  error_code: string | null;
  error: string | null;
  started_at: number | null;
  finished_at: number | null;
}

interface CandidateRow {
  id: string;
  run_id: string;
  source: ResearchSource;
  external_id: string;
  url: string;
  normalized_url: string;
  title: string;
  author: string;
  author_id?: string;
  snippet: string;
  published_at: number | null;
  date_confidence: ResearchCandidate["dateConfidence"];
  media_type: ResearchCandidate["mediaType"];
  engagement_json: string;
  discovery_method: string;
  relevance_score: number;
  recency_score: number;
  engagement_score: number;
  overall_score: number;
  cluster_id: string | null;
  state: ResearchCandidate["state"];
  import_task_id: string | null;
  imported_item_id: string | null;
  created_at: number;
  updated_at: number;
}

interface ClusterRow {
  id: string;
  run_id: string;
  title: string;
  representative_candidate_id: string;
  source_count: number;
}

const RUN_SELECT = `SELECT r.*,
  (SELECT COUNT(*) FROM research_candidates c WHERE c.run_id=r.id) AS candidate_count,
  (SELECT COUNT(*) FROM research_clusters k WHERE k.run_id=r.id) AS cluster_count
  FROM research_runs r`;

function parseJson<T>(raw: string, fallback: T): T {
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

function mapRun(row: RunRow): ResearchRun {
  return {
    id: row.id,
    topic: row.topic,
    dayRange: row.day_range,
    rangeFrom: row.range_from,
    rangeTo: row.range_to,
    depth: row.depth,
    sources: parseJson<ResearchSource[]>(row.sources_json, []),
    status: row.status,
    reportStatus: row.report_status,
    reportMarkdown: row.report_markdown,
    reportError: row.report_error,
    reportPromptVersion: row.report_prompt_version,
    savedItemId: row.saved_item_id,
    candidateCount: row.candidate_count,
    clusterCount: row.cluster_count,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    completedAt: row.completed_at,
  };
}

function mapSource(row: SourceRow): ResearchSourceRun {
  return {
    runId: row.run_id,
    source: row.source,
    status: row.status,
    method: row.method,
    collectedCount: row.collected_count,
    errorCode: row.error_code,
    error: row.error,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
  };
}

function mapCandidate(row: CandidateRow): ResearchCandidate {
  return {
    id: row.id,
    runId: row.run_id,
    source: row.source,
    externalId: row.external_id,
    url: row.url,
    normalizedUrl: row.normalized_url,
    title: row.title,
    author: row.author,
    authorId: row.author_id,
    snippet: row.snippet,
    publishedAt: row.published_at,
    dateConfidence: row.date_confidence,
    mediaType: row.media_type,
    engagement: parseJson(row.engagement_json, {}),
    discoveryMethod: row.discovery_method,
    relevanceScore: row.relevance_score,
    recencyScore: row.recency_score,
    engagementScore: row.engagement_score,
    overallScore: row.overall_score,
    clusterId: row.cluster_id,
    state: row.state,
    importTaskId: row.import_task_id,
    importedItemId: row.imported_item_id,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export class ResearchDB {
  constructor(private readonly db: Database.Database) {}

  recoverInterrupted(now = Date.now()): void {
    this.db.transaction(() => {
      this.db.run(
        `UPDATE research_runs SET status='canceled', completed_at=COALESCE(completed_at,?), updated_at=?
         WHERE status='collecting'`,
        now,
        now,
      );
      this.db.run(
        `UPDATE research_source_runs SET status='canceled', finished_at=COALESCE(finished_at,?)
         WHERE status IN ('pending','running')`,
        now,
      );
      this.db.run(
        `UPDATE research_runs SET report_status='failed', report_error='应用退出前报告生成未完成', updated_at=?
         WHERE report_status='generating'`,
        now,
      );
    })();
  }

  create(
    input: CreateResearchRunInput,
    rangeFrom: number,
    rangeTo: number,
    now = Date.now(),
  ): ResearchRun {
    const id = randomUUID();
    this.db.transaction(() => {
      this.db.run(
        `INSERT INTO research_runs
         (id,topic,day_range,range_from,range_to,depth,sources_json,status,report_status,created_at,updated_at)
         VALUES (?,?,?,?,?,?,?,'collecting','none',?,?)`,
        id,
        input.topic.trim(),
        input.dayRange,
        rangeFrom,
        rangeTo,
        input.depth,
        JSON.stringify(input.sources),
        now,
        now,
      );
      for (const source of input.sources) {
        this.db.run(
          `INSERT INTO research_source_runs
           (run_id,source,status,method,collected_count)
           VALUES (?,?, 'pending', ?, 0)`,
          id,
          source,
          source === "bilibili" ? "public-api" : "authenticated-browser",
        );
      }
    })();
    return this.get(id)!;
  }

  list(limit = 100): ResearchRun[] {
    return (this.db.all(
      `${RUN_SELECT} ORDER BY r.updated_at DESC LIMIT ?`,
      Math.min(Math.max(limit, 1), 200),
    ) as RunRow[]).map(mapRun);
  }

  get(id: string): ResearchRun | null {
    const row = this.db.get(`${RUN_SELECT} WHERE r.id=?`, id) as RunRow | undefined;
    return row ? { ...mapRun(row), context: new ResearchWorkflowDB(this.db).context(id) } : null;
  }

  getDetail(id: string): ResearchRunDetail | null {
    const run = this.get(id);
    if (!run) return null;
    const candidates = this.listCandidates(id);
    const clusterRows = this.db.all(
      "SELECT * FROM research_clusters WHERE run_id=? ORDER BY source_count DESC, title",
      id,
    ) as ClusterRow[];
    return {
      run,
      sources: (this.db.all(
        "SELECT * FROM research_source_runs WHERE run_id=? ORDER BY source",
        id,
      ) as SourceRow[]).map(mapSource),
      candidates,
      attempts: new ResearchWorkflowDB(this.db).attempts(id),
      documents: new ResearchWorkflowDB(this.db).documents(id),
      clusters: clusterRows.map((row): ResearchCluster => ({
        id: row.id,
        runId: row.run_id,
        title: row.title,
        representativeCandidateId: row.representative_candidate_id,
        sourceCount: row.source_count,
        candidates: candidates.filter((candidate) => candidate.clusterId === row.id),
      })),
    };
  }

  listCandidates(runId: string): ResearchCandidate[] {
    return (this.db.all(
      "SELECT c.*,(SELECT author_id FROM research_authors a WHERE a.candidate_id=c.id) AS author_id FROM research_candidates c WHERE run_id=? ORDER BY overall_score DESC, created_at ASC",
      runId,
    ) as CandidateRow[]).map(mapCandidate);
  }

  getCandidates(runId: string, ids: readonly string[]): ResearchCandidate[] {
    if (ids.length === 0) return [];
    const placeholders = ids.map(() => "?").join(",");
    return (this.db.all(
      `SELECT * FROM research_candidates WHERE run_id=? AND id IN (${placeholders})`,
      runId,
      ...ids,
    ) as CandidateRow[]).map(mapCandidate);
  }

  updateSource(
    runId: string,
    source: ResearchSource,
    fields: Partial<Pick<ResearchSourceRun, "status" | "collectedCount" | "errorCode" | "error" | "startedAt" | "finishedAt">>,
  ): void {
    const current = this.db.get(
      "SELECT * FROM research_source_runs WHERE run_id=? AND source=?",
      runId,
      source,
    ) as SourceRow | undefined;
    if (!current) return;
    this.db.run(
      `UPDATE research_source_runs SET status=?,collected_count=?,error_code=?,error=?,started_at=?,finished_at=?
       WHERE run_id=? AND source=?`,
      fields.status ?? current.status,
      fields.collectedCount ?? current.collected_count,
      fields.errorCode === undefined ? current.error_code : fields.errorCode,
      fields.error === undefined ? current.error : fields.error,
      fields.startedAt === undefined ? current.started_at : fields.startedAt,
      fields.finishedAt === undefined ? current.finished_at : fields.finishedAt,
      runId,
      source,
    );
    this.touch(runId);
  }

  upsertCandidate(
    runId: string,
    item: ResearchCandidateInput,
    normalizedUrl: string,
    now = Date.now(),
  ): boolean {
    const existing = this.db.get(
      `SELECT id FROM research_candidates
       WHERE run_id=? AND (normalized_url=? OR (source=? AND external_id=? AND external_id<>'')) LIMIT 1`,
      runId,
      normalizedUrl,
      item.source,
      item.externalId,
    ) as { id: string } | undefined;
    if (existing) return false;
    this.db.run(
      `INSERT INTO research_candidates
       (id,run_id,source,external_id,url,normalized_url,title,author,snippet,published_at,
        date_confidence,media_type,engagement_json,discovery_method,created_at,updated_at)
       VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
      randomUUID(),
      runId,
      item.source,
      item.externalId,
      item.url,
      normalizedUrl,
      item.title.trim(),
      item.author.trim(),
      item.snippet?.trim() ?? "",
      item.publishedAt ?? null,
      item.dateConfidence ?? "low",
      item.mediaType,
      JSON.stringify(item.engagement ?? {}),
      item.discoveryMethod,
      now,
      now,
    );
    if (item.authorId) {
      this.db.run("INSERT OR REPLACE INTO research_authors(candidate_id,author_id) SELECT id,? FROM research_candidates WHERE run_id=? AND normalized_url=?", item.authorId, runId, normalizedUrl);
    }
    this.touch(runId, now);
    return true;
  }

  replaceAnalysis(
    runId: string,
    candidates: readonly ResearchCandidate[],
    clusters: readonly Omit<ResearchCluster, "runId" | "candidates">[],
    now = Date.now(),
  ): void {
    this.db.transaction(() => {
      this.db.run("UPDATE research_candidates SET cluster_id=NULL WHERE run_id=?", runId);
      this.db.run("DELETE FROM research_clusters WHERE run_id=?", runId);
      for (const cluster of clusters) {
        this.db.run(
          `INSERT INTO research_clusters (id,run_id,title,representative_candidate_id,source_count)
           VALUES (?,?,?,?,?)`,
          cluster.id,
          runId,
          cluster.title,
          cluster.representativeCandidateId,
          cluster.sourceCount,
        );
      }
      for (const candidate of candidates) {
        this.db.run(
          `UPDATE research_candidates SET relevance_score=?,recency_score=?,engagement_score=?,
           overall_score=?,cluster_id=?,updated_at=? WHERE id=? AND run_id=?`,
          candidate.relevanceScore,
          candidate.recencyScore,
          candidate.engagementScore,
          candidate.overallScore,
          candidate.clusterId,
          now,
          candidate.id,
          runId,
        );
      }
      this.touch(runId, now);
    })();
  }

  resumeRun(id: string, now = Date.now()): void {
    this.db.run("UPDATE research_runs SET status='collecting',completed_at=NULL,updated_at=? WHERE id=?", now, id);
  }

  markReportOutdated(id: string, now = Date.now()): void {
    this.db.run(
      "UPDATE research_runs SET report_status='none',report_error='候选已补充，请重新生成报告以包含最新结果',updated_at=? WHERE id=?",
      now,
      id,
    );
  }

  finishRun(id: string, status: ResearchRun["status"], now = Date.now()): void {
    this.db.run(
      "UPDATE research_runs SET status=?,completed_at=?,updated_at=? WHERE id=?",
      status,
      now,
      now,
      id,
    );
  }

  beginReport(id: string, now = Date.now()): void {
    this.db.run(
      "UPDATE research_runs SET report_status='generating',report_error=NULL,updated_at=? WHERE id=?",
      now,
      id,
    );
  }

  saveReport(id: string, markdown: string, version: string, now = Date.now()): void {
    this.db.run(
      `UPDATE research_runs SET report_status='ready',report_markdown=?,report_error=NULL,
       report_prompt_version=?,updated_at=? WHERE id=?`,
      markdown,
      version,
      now,
      id,
    );
  }

  failReport(id: string, error: string, now = Date.now()): void {
    this.db.run(
      "UPDATE research_runs SET report_status='failed',report_error=?,updated_at=? WHERE id=?",
      error,
      now,
      id,
    );
  }

  linkImport(candidateId: string, taskId: string, now = Date.now()): void {
    this.db.run(
      `UPDATE research_candidates SET state='queued',import_task_id=?,updated_at=? WHERE id=?`,
      taskId,
      now,
      candidateId,
    );
  }

  setSavedItem(id: string, itemId: string, now = Date.now()): void {
    this.db.run(
      "UPDATE research_runs SET saved_item_id=?,updated_at=? WHERE id=?",
      itemId,
      now,
      id,
    );
  }

  delete(id: string): boolean {
    return this.db.run("DELETE FROM research_runs WHERE id=?", id).changes > 0;
  }

  private touch(id: string, now = Date.now()): void {
    this.db.run("UPDATE research_runs SET updated_at=? WHERE id=?", now, id);
  }
}
