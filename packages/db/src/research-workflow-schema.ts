export const RESEARCH_EVIDENCE_SCHEMA = `
CREATE TABLE IF NOT EXISTS research_contexts (
  run_id TEXT PRIMARY KEY REFERENCES research_runs(id) ON DELETE CASCADE,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS research_attempts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_attempts_run ON research_attempts(run_id);
CREATE TABLE IF NOT EXISTS research_snapshots (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_snapshots_run ON research_snapshots(run_id);
CREATE TABLE IF NOT EXISTS research_authors (
  candidate_id TEXT PRIMARY KEY REFERENCES research_candidates(id) ON DELETE CASCADE,
  author_id TEXT NOT NULL
);
`;
export const RESEARCH_DOCUMENT_SCHEMA = `
CREATE TABLE IF NOT EXISTS research_documents (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  candidate_id TEXT NOT NULL REFERENCES research_candidates(id) ON DELETE CASCADE,
  payload TEXT NOT NULL,
  UNIQUE(run_id, candidate_id)
);
CREATE INDEX IF NOT EXISTS idx_research_documents_run ON research_documents(run_id);
`;
export const RESEARCH_SERIES_SCHEMA = `
CREATE TABLE IF NOT EXISTS research_series (
  run_id TEXT PRIMARY KEY REFERENCES research_runs(id) ON DELETE CASCADE,
  series_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_series_id ON research_series(series_id);
CREATE TABLE IF NOT EXISTS research_saved_reports (
  item_id TEXT PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  series_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_saved_series ON research_saved_reports(series_id);
`;
