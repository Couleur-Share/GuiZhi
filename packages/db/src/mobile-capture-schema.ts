export const MOBILE_CAPTURE_SCHEMA = `
CREATE TABLE IF NOT EXISTS mobile_capture_receipts (
  relay_key TEXT NOT NULL, delivery_id TEXT NOT NULL, original_input TEXT NOT NULL,
  task_ids TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, progress TEXT NOT NULL,
  acked INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
  PRIMARY KEY(relay_key,delivery_id)
);
CREATE TABLE IF NOT EXISTS mobile_capture_tasks (
  task_id TEXT PRIMARY KEY, relay_key TEXT NOT NULL, delivery_id TEXT NOT NULL,
  FOREIGN KEY(relay_key,delivery_id) REFERENCES mobile_capture_receipts(relay_key,delivery_id)
);
CREATE TABLE IF NOT EXISTS mobile_capture_outbox (
  relay_key TEXT NOT NULL, delivery_id TEXT NOT NULL, version INTEGER NOT NULL,
  PRIMARY KEY(relay_key,delivery_id),
  FOREIGN KEY(relay_key,delivery_id) REFERENCES mobile_capture_receipts(relay_key,delivery_id)
);
`;
