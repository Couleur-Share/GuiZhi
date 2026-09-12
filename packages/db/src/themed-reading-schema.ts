/** 主题阅读页独立于知识正文；角色交换仅在事务内部短暂使用 NULL。 */
export const THEMED_READING_SCHEMA = `
CREATE TABLE IF NOT EXISTS themed_reading_versions (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('body','summary')),
  role TEXT CHECK(role IN ('current','previous','working')),
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(item_id, source_kind, role),
  UNIQUE(id, item_id, source_kind)
);
CREATE TABLE IF NOT EXISTS themed_reading_assets (
  version_id TEXT NOT NULL REFERENCES themed_reading_versions(id) ON DELETE CASCADE,
  file_name TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  bytes INTEGER NOT NULL CHECK(bytes >= 0),
  PRIMARY KEY(version_id, file_name)
);
CREATE INDEX IF NOT EXISTS idx_themed_reading_assets_file
  ON themed_reading_assets(file_name);
CREATE TABLE IF NOT EXISTS themed_reading_tasks (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('body','summary')),
  version_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('queued','running','completed','partial','failed','cancelled','interrupted')),
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY(version_id, item_id, source_kind)
    REFERENCES themed_reading_versions(id, item_id, source_kind) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_themed_reading_active_task
  ON themed_reading_tasks(item_id, source_kind) WHERE state IN ('queued','running');
CREATE INDEX IF NOT EXISTS idx_themed_reading_tasks_updated
  ON themed_reading_tasks(updated_at DESC);
`;
