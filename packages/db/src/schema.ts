/**
 * 数据库表结构定义。
 *
 * 全部使用 CREATE TABLE IF NOT EXISTS，新增表对既有库是安全的增量操作；
 * 列级变更需走 schema_migrations（见 init.ts）。
 */

/**
 * Tables only — run BEFORE migrations so CREATE TABLE IF NOT EXISTS
 * is a safe no-op for existing databases.
 */
export const SCHEMA_TABLES = `
-- 设置表
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- 集合（知识库）
CREATE TABLE IF NOT EXISTS collections (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  icon TEXT,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- 标签
CREATE TABLE IF NOT EXISTS tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  color_key TEXT NOT NULL DEFAULT 'gray',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- 知识条目
CREATE TABLE IF NOT EXISTS knowledge_items (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  summary TEXT,
  transcript TEXT,
  item_type TEXT NOT NULL DEFAULT 'note'
    CHECK(item_type IN ('note','webpage','video','image','audio','document','snippet','forum')),
  status TEXT NOT NULL DEFAULT 'inbox'
    CHECK(status IN ('inbox','ready','archived')),
  collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
  is_favorite INTEGER NOT NULL DEFAULT 0,
  is_pinned INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- 条目-标签 多对多
CREATE TABLE IF NOT EXISTS knowledge_item_tags (
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, tag_id)
);

-- 来源追溯（采集管线 M2 起写入）
CREATE TABLE IF NOT EXISTS source_records (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  source_type TEXT NOT NULL,
  source_uri TEXT,
  normalized_uri TEXT,
  content_hash TEXT,
  platform TEXT,
  captured_at INTEGER NOT NULL
);

-- Wiki 页面（ADR 0023：AI 编译的派生知识页；normalized_title 是链接锚点，禁止改名）
CREATE TABLE IF NOT EXISTS wiki_pages (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  normalized_title TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'topic' CHECK(kind IN ('topic','entity','concept')),
  summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  aliases_json TEXT,
  provider TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  prompt_version TEXT NOT NULL DEFAULT '',
  generated_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- Wiki 页间链接（保存时从正文 [[链接]] 物化；仅存解析成功的）
CREATE TABLE IF NOT EXISTS wiki_page_links (
  from_page_id TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
  to_page_id TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (from_page_id, to_page_id)
);

-- Wiki 页面来源条目（可回溯：页面 → 原文）
CREATE TABLE IF NOT EXISTS wiki_page_sources (
  page_id TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (page_id, item_id)
);

-- Wiki 编译指纹（素材哈希 + 提示词版本，增量编译的失效判定）
-- prompt_version 为空表示上次尝试失败；failure_count / next_attempt_at 用于退避，
-- 避免一条模型始终解析不出来的「毒条目」每轮都白烧两次调用
CREATE TABLE IF NOT EXISTS wiki_ingestions (
  item_id TEXT PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  content_hash TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  prompt_version TEXT NOT NULL DEFAULT '',
  failure_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER,
  updated_at INTEGER NOT NULL
);

-- Wiki 页面历史版本（整体覆盖前的快照，可回滚）
CREATE TABLE IF NOT EXISTS wiki_page_revisions (
  id TEXT PRIMARY KEY,
  page_id TEXT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  kind TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  aliases_json TEXT,
  model TEXT NOT NULL DEFAULT '',
  prompt_version TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

-- AI 用量（按天 × 场景 × 模型聚合；provider 不回报 usage 时 token 记 0）
CREATE TABLE IF NOT EXISTS ai_usage_daily (
  day TEXT NOT NULL,
  scenario TEXT NOT NULL,
  model TEXT NOT NULL,
  calls INTEGER NOT NULL DEFAULT 0,
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (day, scenario, model)
);

-- 语义索引（embedding 向量按分块存储；vector 为 L2 归一化的 Float32 BLOB）
CREATE TABLE IF NOT EXISTS knowledge_embeddings (
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  chunk_index INTEGER NOT NULL,
  chunk_text TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  model TEXT NOT NULL,
  dims INTEGER NOT NULL,
  vector BLOB NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (item_id, chunk_index)
);

-- AI 问答会话（消息体由渲染进程序列化为 JSON，DB 层不解析）
CREATE TABLE IF NOT EXISTS ask_sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  messages_json TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- 导入任务队列（持久化，支持重启恢复）
CREATE TABLE IF NOT EXISTS import_tasks (
  id TEXT PRIMARY KEY,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('text','file','url')),
  source_input TEXT NOT NULL,
  display_name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending','processing','completed','failed','canceled','duplicate')),
  stage TEXT,
  error TEXT,
  result_item_id TEXT,
  duplicate_item_id TEXT,
  collection_id TEXT,
  force_duplicate INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
`;

/**
 * Indexes, FTS, and triggers — run AFTER migrations so all columns exist.
 *
 * knowledge_fts 的内容由 KnowledgeItemDB 在同一事务内维护（写入前需做
 * 中文按字分词预处理，无法用纯 SQL 触发器实现）。
 */
export const SCHEMA_INDEXES = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_name_lower ON tags(LOWER(name));
CREATE INDEX IF NOT EXISTS idx_collections_sort ON collections(sort_order);
CREATE INDEX IF NOT EXISTS idx_items_status ON knowledge_items(status);
CREATE INDEX IF NOT EXISTS idx_items_collection ON knowledge_items(collection_id);
CREATE INDEX IF NOT EXISTS idx_items_updated ON knowledge_items(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_deleted ON knowledge_items(deleted_at);
CREATE INDEX IF NOT EXISTS idx_items_favorite ON knowledge_items(is_favorite);
CREATE INDEX IF NOT EXISTS idx_item_tags_tag ON knowledge_item_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_sources_item ON source_records(item_id);
CREATE INDEX IF NOT EXISTS idx_sources_normalized ON source_records(normalized_uri);
CREATE INDEX IF NOT EXISTS idx_sources_hash ON source_records(content_hash);
CREATE INDEX IF NOT EXISTS idx_import_tasks_status ON import_tasks(status);
CREATE INDEX IF NOT EXISTS idx_import_tasks_created ON import_tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_wiki_links_to ON wiki_page_links(to_page_id);
CREATE INDEX IF NOT EXISTS idx_wiki_sources_item ON wiki_page_sources(item_id);
CREATE INDEX IF NOT EXISTS idx_wiki_revisions_page ON wiki_page_revisions(page_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ask_sessions_updated ON ask_sessions(updated_at DESC);
-- 语义检索按 model 过滤全部分块；换模型后旧向量仍在表中，没有索引会全表扫
CREATE INDEX IF NOT EXISTS idx_embeddings_model ON knowledge_embeddings(model);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
  item_id UNINDEXED,
  title,
  content,
  tags
);
`;

/** @deprecated Use SCHEMA_TABLES + SCHEMA_INDEXES instead */
export const SCHEMA = SCHEMA_TABLES + SCHEMA_INDEXES;
