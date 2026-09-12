export const WIKI_COMPILER_SCHEMA = `
CREATE TABLE IF NOT EXISTS wiki_compile_jobs (
 id TEXT PRIMARY KEY, model TEXT NOT NULL, status TEXT NOT NULL, error TEXT, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS wiki_compile_items (
 job_id TEXT NOT NULL REFERENCES wiki_compile_jobs(id) ON DELETE CASCADE,
 item_id TEXT NOT NULL, title TEXT NOT NULL, fingerprint TEXT NOT NULL, published INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(job_id,item_id)
);
CREATE TABLE IF NOT EXISTS wiki_compile_blocks (
 job_id TEXT NOT NULL, item_id TEXT NOT NULL, block_key TEXT NOT NULL, block_json TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', contributions_json TEXT, error TEXT, attempts INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(job_id,item_id,block_key),
 FOREIGN KEY(job_id,item_id) REFERENCES wiki_compile_items(job_id,item_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS wiki_block_cache (
 item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 block_hash TEXT NOT NULL, model TEXT NOT NULL, contributions_json TEXT NOT NULL,
 PRIMARY KEY(item_id,block_hash,model)
);
CREATE TABLE IF NOT EXISTS wiki_contributions (
 item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 block_key TEXT NOT NULL, page_title TEXT NOT NULL, contribution_json TEXT NOT NULL,
 PRIMARY KEY(item_id,block_key,page_title)
);
CREATE INDEX IF NOT EXISTS idx_wiki_contributions_page ON wiki_contributions(page_title);
CREATE TABLE IF NOT EXISTS wiki_page_suggestions (
 page_id TEXT PRIMARY KEY REFERENCES wiki_pages(id) ON DELETE CASCADE,
 draft_json TEXT NOT NULL, reason TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS wiki_invalidated_pages (page_title TEXT PRIMARY KEY);
`;

export const WIKI_INVALIDATION_TRIGGERS = `
CREATE TRIGGER IF NOT EXISTS wiki_source_changed BEFORE UPDATE OF title,content,transcript,deleted_at ON knowledge_items
WHEN OLD.title IS NOT NEW.title OR OLD.content IS NOT NEW.content OR OLD.transcript IS NOT NEW.transcript OR OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
 INSERT OR IGNORE INTO wiki_invalidated_pages SELECT page_title FROM wiki_contributions WHERE item_id=OLD.id;
 DELETE FROM wiki_contributions WHERE item_id=OLD.id;
 UPDATE wiki_ingestions SET content_hash='' WHERE item_id=OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS wiki_checkpoint_source_deleted AFTER DELETE ON knowledge_items
BEGIN
 DELETE FROM wiki_compile_blocks WHERE item_id=OLD.id;
 DELETE FROM wiki_compile_items WHERE item_id=OLD.id;
 UPDATE wiki_compile_jobs SET status='cancelled',error='编译来源已彻底删除' WHERE status IN ('running','paused','interrupted') AND NOT EXISTS(SELECT 1 FROM wiki_compile_items i WHERE i.job_id=wiki_compile_jobs.id);
END;
CREATE TRIGGER IF NOT EXISTS wiki_source_deleted BEFORE DELETE ON knowledge_items
BEGIN
 INSERT OR IGNORE INTO wiki_invalidated_pages SELECT page_title FROM wiki_contributions WHERE item_id=OLD.id;
END;
`;
