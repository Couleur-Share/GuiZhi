package main

// 由 spikes/mygo-db/gen-schema.mts 自动生成，请勿手改：建表/索引/迁移用到的 SQL 常量，原文取自 packages/db/src。
// 重新生成：pnpm dlx tsx spikes/mygo-db/gen-schema.mts

const schemaTables = `
CREATE TABLE IF NOT EXISTS source_capture_revisions (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 title TEXT NOT NULL, content TEXT NOT NULL, transcript TEXT, review_json TEXT NOT NULL, captured_at INTEGER NOT NULL
); CREATE INDEX IF NOT EXISTS idx_source_capture_item ON source_capture_revisions(item_id,captured_at DESC);

CREATE TABLE IF NOT EXISTS knowledge_selection_sets(id TEXT PRIMARY KEY,ids_json TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS knowledge_batch_results(run_id TEXT NOT NULL,item_id TEXT NOT NULL,command_json TEXT NOT NULL,result_json TEXT NOT NULL,updated_at INTEGER NOT NULL,PRIMARY KEY(run_id,item_id));
CREATE INDEX IF NOT EXISTS idx_knowledge_batch_updated ON knowledge_batch_results(updated_at DESC);


CREATE TABLE IF NOT EXISTS ask_session_meta(id TEXT PRIMARY KEY REFERENCES ask_sessions(id) ON DELETE CASCADE, custom_title TEXT, pinned INTEGER NOT NULL DEFAULT 0);
CREATE VIRTUAL TABLE IF NOT EXISTS ask_session_fts USING fts5(session_id UNINDEXED,title,questions,answers);

CREATE TABLE IF NOT EXISTS ask_evidence_clearances (item_id TEXT PRIMARY KEY, cleared_at INTEGER NOT NULL);

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


CREATE TABLE IF NOT EXISTS crawl_jobs (
 id TEXT PRIMARY KEY, payload TEXT NOT NULL, status TEXT NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS crawl_pages (
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES crawl_jobs(id) ON DELETE CASCADE,
 url TEXT NOT NULL, depth INTEGER NOT NULL, seed_index INTEGER NOT NULL,
 status TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(job_id,url)
);
CREATE INDEX IF NOT EXISTS idx_crawl_pages_queue ON crawl_pages(job_id,status,depth);
CREATE TABLE IF NOT EXISTS web_source_versions (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 payload TEXT NOT NULL, captured_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_web_versions_item ON web_source_versions(item_id,captured_at);
CREATE TABLE IF NOT EXISTS web_source_baselines (
 item_id TEXT PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
 version_id TEXT REFERENCES web_source_versions(id), content_hash TEXT NOT NULL,
 title TEXT NOT NULL, checked_at INTEGER NOT NULL, remote_hash TEXT NOT NULL,
 summary_stale INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS web_snapshot_assets (
 version_id TEXT NOT NULL REFERENCES web_source_versions(id) ON DELETE CASCADE,
 file_name TEXT NOT NULL, PRIMARY KEY(version_id,file_name)
);
CREATE INDEX IF NOT EXISTS idx_web_snapshot_asset ON web_snapshot_assets(file_name);


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


CREATE TABLE IF NOT EXISTS research_documents (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  candidate_id TEXT NOT NULL REFERENCES research_candidates(id) ON DELETE CASCADE,
  payload TEXT NOT NULL,
  UNIQUE(run_id, candidate_id)
);
CREATE INDEX IF NOT EXISTS idx_research_documents_run ON research_documents(run_id);


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
  status TEXT NOT NULL DEFAULT 'active'
    CHECK(status IN ('active','archived')),
  collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
  is_favorite INTEGER NOT NULL DEFAULT 0,
  is_pinned INTEGER NOT NULL DEFAULT 0,
  -- 采集成功但内容有缺失时不阻断入库，而是留下待人工复核标记。
  review_status TEXT NOT NULL DEFAULT 'clear'
    CHECK(review_status IN ('clear','needs_review')),
  review_reasons TEXT,
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
  -- 原始分享入口，可带访问令牌；不参与去重，也不暴露给正文/导出。
  access_uri TEXT,
  normalized_uri TEXT,
  content_hash TEXT,
  platform TEXT,
  captured_at INTEGER NOT NULL
);

-- 平台来源评论：辅助材料，不进入正文、FTS、embedding 或 Wiki。
CREATE TABLE IF NOT EXISTS source_comments (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  platform TEXT NOT NULL CHECK(platform IN ('xiaohongshu','douyin','linuxdo')),
  external_id TEXT NOT NULL,
  author_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  like_count INTEGER NOT NULL DEFAULT 0,
  published_at INTEGER,
  captured_at INTEGER NOT NULL,
  UNIQUE(item_id, platform, external_id)
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
  -- 用户手动改过正文的时刻；非空时编译不再覆盖 body（清空即交回自动编译）
  manual_edited_at INTEGER,
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
  failed_calls INTEGER NOT NULL DEFAULT 0,
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
    scope TEXT NOT NULL DEFAULT 'knowledge',
    item_id TEXT,
    article_title TEXT,
    options_json TEXT NOT NULL DEFAULT '{}',
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
  -- 已入库但内容有缺失的原因（转写失败等）；与 error 互不替代，见 ImportTask.warning
  warning TEXT,
  -- 用户已在处理中心知悉该警告；原文仍保留供导入历史诊断
  warning_acknowledged_at INTEGER,
  -- 抽取出的条目类型，用于列表图标；不加 CHECK，避免新增类型时又要重建表
  item_type TEXT,
  result_item_id TEXT,
  duplicate_item_id TEXT,
  collection_id TEXT,
  -- 来源刷新副本关联的原条目；普通导入为 NULL
  refresh_of_item_id TEXT REFERENCES knowledge_items(id) ON DELETE SET NULL,
  -- 采集时选定的标签（JSON 字符串数组），入库时打到条目上
  tag_names TEXT,
  -- 各阶段耗时与 AI 开销（JSON 数组，见 ImportStageStat）；重试时清空
  stage_stats TEXT,
  capture_strategy TEXT NOT NULL DEFAULT 'standard'
    CHECK(capture_strategy IN ('standard','authenticated')),
  comment_limit INTEGER NOT NULL DEFAULT 0
    CHECK(comment_limit IN (0,10,20,50)),
  force_duplicate INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- 保存的平台发现视图。定时任务只发现候选，不自动入库。
CREATE TABLE IF NOT EXISTS platform_discovery_views (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  platform TEXT NOT NULL CHECK(platform IN ('xiaohongshu','douyin','linuxdo')),
  mode TEXT NOT NULL CHECK(mode IN ('creator','keyword')),
  query TEXT NOT NULL,
  interval_minutes INTEGER NOT NULL DEFAULT 1440,
  enabled INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'ready'
    CHECK(state IN ('ready','running','login_required','backoff','paused')),
  last_run_at INTEGER,
  next_run_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS platform_discovery_runs (
  id TEXT PRIMARY KEY,
  view_id TEXT NOT NULL REFERENCES platform_discovery_views(id) ON DELETE CASCADE,
  state TEXT NOT NULL CHECK(state IN ('running','completed','failed','canceled')),
  cursor TEXT,
  pages_scanned INTEGER NOT NULL DEFAULT 0,
  candidates_found INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  started_at INTEGER NOT NULL,
  finished_at INTEGER,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS platform_discovery_candidates (
  view_id TEXT NOT NULL REFERENCES platform_discovery_views(id) ON DELETE CASCADE,
  platform TEXT NOT NULL,
  external_id TEXT NOT NULL,
  item_json TEXT NOT NULL,
  content_hash TEXT,
  state TEXT NOT NULL DEFAULT 'new'
    CHECK(state IN ('new','dismissed','imported')),
  first_seen_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  PRIMARY KEY (view_id, platform, external_id)
);

-- 手动触发的近期主题研究。候选与报告独立保存，只有用户确认后才进入知识库。
CREATE TABLE IF NOT EXISTS research_runs (
  id TEXT PRIMARY KEY,
  topic TEXT NOT NULL,
  day_range INTEGER NOT NULL CHECK(day_range IN (7,14,30)),
  time_scope TEXT NOT NULL DEFAULT 'recent' CHECK(time_scope IN ('recent','all')),
  range_from INTEGER NOT NULL,
  range_to INTEGER NOT NULL,
  depth TEXT NOT NULL CHECK(depth IN ('quick','deep')),
  sources_json TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('collecting','ready','partial','failed','canceled')),
  report_status TEXT NOT NULL DEFAULT 'none'
    CHECK(report_status IN ('none','generating','ready','failed')),
  report_markdown TEXT,
  report_error TEXT,
  report_prompt_version TEXT,
  saved_item_id TEXT REFERENCES knowledge_items(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  completed_at INTEGER
);

CREATE TABLE IF NOT EXISTS research_source_runs (
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  source TEXT NOT NULL CHECK(source IN ('xiaohongshu','douyin','bilibili','web')),
  status TEXT NOT NULL
    CHECK(status IN ('pending','running','succeeded','partial','login_required','failed','canceled')),
  method TEXT NOT NULL,
  collected_count INTEGER NOT NULL DEFAULT 0,
  error_code TEXT,
  error TEXT,
  started_at INTEGER,
  finished_at INTEGER,
  PRIMARY KEY (run_id, source)
);

CREATE TABLE IF NOT EXISTS research_clusters (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  representative_candidate_id TEXT NOT NULL,
  source_count INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS research_candidates (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  source TEXT NOT NULL CHECK(source IN ('xiaohongshu','douyin','bilibili','web')),
  external_id TEXT NOT NULL,
  url TEXT NOT NULL,
  normalized_url TEXT NOT NULL,
  title TEXT NOT NULL,
  author TEXT NOT NULL DEFAULT '',
  snippet TEXT NOT NULL DEFAULT '',
  published_at INTEGER,
  date_confidence TEXT NOT NULL DEFAULT 'low'
    CHECK(date_confidence IN ('high','medium','low')),
  media_type TEXT NOT NULL CHECK(media_type IN ('image','video','article')),
  engagement_json TEXT NOT NULL DEFAULT '{}',
  discovery_method TEXT NOT NULL,
  relevance_score INTEGER NOT NULL DEFAULT 0,
  recency_score INTEGER NOT NULL DEFAULT 0,
  engagement_score INTEGER NOT NULL DEFAULT 0,
  overall_score INTEGER NOT NULL DEFAULT 0,
  cluster_id TEXT REFERENCES research_clusters(id) ON DELETE SET NULL,
  state TEXT NOT NULL DEFAULT 'available'
    CHECK(state IN ('available','queued','imported','dismissed')),
  import_task_id TEXT,
  imported_item_id TEXT REFERENCES knowledge_items(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(run_id, source, external_id),
  UNIQUE(run_id, normalized_url)
);

-- 主进程持有租约；Renderer 崩溃后 lease_until 到期即可重新领取。
CREATE TABLE IF NOT EXISTS background_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL
    CHECK(kind IN ('backup','platform-discovery','wiki-compile','semantic-index')),
  scope_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'scheduled'
    CHECK(state IN ('scheduled','running','retry_wait','paused','succeeded','failed')),
  payload_json TEXT NOT NULL DEFAULT '{}',
  interval_minutes INTEGER,
  next_run_at INTEGER,
  attempt INTEGER NOT NULL DEFAULT 0,
  lease_owner TEXT,
  lease_until INTEGER,
  last_error TEXT,
  last_success_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(kind, scope_id)
);
`

const schemaIndexes = `

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


CREATE TRIGGER IF NOT EXISTS ask_history_deleted AFTER DELETE ON ask_sessions BEGIN DELETE FROM ask_session_fts WHERE session_id=OLD.id; DELETE FROM ask_session_meta WHERE id=OLD.id; END;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_name_lower ON tags(LOWER(name));
CREATE INDEX IF NOT EXISTS idx_collections_sort ON collections(sort_order);
CREATE INDEX IF NOT EXISTS idx_items_status ON knowledge_items(status);
CREATE INDEX IF NOT EXISTS idx_items_collection ON knowledge_items(collection_id);
CREATE INDEX IF NOT EXISTS idx_items_updated ON knowledge_items(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_deleted ON knowledge_items(deleted_at);
CREATE INDEX IF NOT EXISTS idx_items_favorite ON knowledge_items(is_favorite);
-- 列表总数 COUNT(*) 的覆盖索引。
--
-- 每次列表都要数一次总数，条件恒为 deleted_at IS NULL + status 过滤。
-- 单列的 idx_items_deleted 只能定位行、还得回表读 status，而条目表带着
-- 2KB 正文，两万条就是两万次大页读。把 status 并进索引后整个 COUNT
-- 在索引里跑完（COVERING INDEX），实测 2 万条 180ms → 2.5ms。
CREATE INDEX IF NOT EXISTS idx_items_deleted_status
  ON knowledge_items(deleted_at, status);
-- 列表排序恒以 is_pinned 打头（buildOrderClause），单列 updated_at 索引用不上，
-- 每次列表都要对全部匹配行做一次临时 B 树排序
CREATE INDEX IF NOT EXISTS idx_items_pinned_updated
  ON knowledge_items(is_pinned DESC, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_pinned_created
  ON knowledge_items(is_pinned DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_item_tags_tag ON knowledge_item_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_sources_item ON source_records(item_id);
CREATE INDEX IF NOT EXISTS idx_sources_normalized ON source_records(normalized_uri);
CREATE INDEX IF NOT EXISTS idx_sources_hash ON source_records(content_hash);
-- 侧栏「平台」分区每次刷新都要按平台分组数一遍，且列表过滤走 platform 等值
CREATE INDEX IF NOT EXISTS idx_sources_platform ON source_records(platform);
CREATE INDEX IF NOT EXISTS idx_source_comments_item
  ON source_comments(item_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_import_tasks_status ON import_tasks(status);
CREATE INDEX IF NOT EXISTS idx_import_tasks_created ON import_tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_import_tasks_created_id
  ON import_tasks(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_import_tasks_status_created_id
  ON import_tasks(status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_discovery_views_due
  ON platform_discovery_views(enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_discovery_runs_view
  ON platform_discovery_runs(view_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_state
  ON platform_discovery_candidates(view_id, state, first_seen_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_discovery_candidates_external
  ON platform_discovery_candidates(platform, external_id);
CREATE INDEX IF NOT EXISTS idx_research_runs_updated
  ON research_runs(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_research_sources_run
  ON research_source_runs(run_id, source);
CREATE INDEX IF NOT EXISTS idx_research_candidates_run_score
  ON research_candidates(run_id, overall_score DESC);
CREATE INDEX IF NOT EXISTS idx_research_candidates_cluster
  ON research_candidates(cluster_id, overall_score DESC);
CREATE INDEX IF NOT EXISTS idx_research_candidates_import_task
  ON research_candidates(import_task_id);
CREATE INDEX IF NOT EXISTS idx_background_jobs_due
  ON background_jobs(state, next_run_at);
-- getCatalog 是 Wiki 模块最高频的查询（编译时每个条目都要打一次），
-- 而 wiki_pages 原本只有主键和 normalized_title 两个索引
CREATE INDEX IF NOT EXISTS idx_wiki_pages_updated ON wiki_pages(updated_at DESC);
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

-- item_id → fts5 rowid 的映射。
--
-- fts5 只能按 rowid 或 MATCH 做索引查找，对 UNINDEXED 普通列的等值条件
-- 一律线性扫整张索引表。而按 item_id 删除位于每一次
-- create/update 的写事务里：autoSave 每 800ms 敲一次键就全扫一遍，
-- 批量改 100 条就是 100 次。有了这张表，删除改走 rowid。
CREATE TABLE IF NOT EXISTS knowledge_fts_map (
  item_id TEXT PRIMARY KEY,
  fts_rowid INTEGER NOT NULL
);

-- Wiki 页面全文索引：内容同样需要中文按字分词预处理，由 WikiDB 在写事务内维护。
-- 不建索引的话，中文提问只能靠「问句里逐字包含页面标题」命中，
-- 编译出来的页面网络在问答侧几乎不可达。
CREATE VIRTUAL TABLE IF NOT EXISTS wiki_fts USING fts5(
  page_id UNINDEXED,
  title,
  summary,
  body
);
`

const knowledgeBatchSchema = `
CREATE TABLE IF NOT EXISTS knowledge_selection_sets(id TEXT PRIMARY KEY,ids_json TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS knowledge_batch_results(run_id TEXT NOT NULL,item_id TEXT NOT NULL,command_json TEXT NOT NULL,result_json TEXT NOT NULL,updated_at INTEGER NOT NULL,PRIMARY KEY(run_id,item_id));
CREATE INDEX IF NOT EXISTS idx_knowledge_batch_updated ON knowledge_batch_results(updated_at DESC);
`

const sourceRevisionsSchema = `CREATE TABLE IF NOT EXISTS source_capture_revisions (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 title TEXT NOT NULL, content TEXT NOT NULL, transcript TEXT, review_json TEXT NOT NULL, captured_at INTEGER NOT NULL
); CREATE INDEX IF NOT EXISTS idx_source_capture_item ON source_capture_revisions(item_id,captured_at DESC);`

const askHistorySchema = `
CREATE TABLE IF NOT EXISTS ask_session_meta(id TEXT PRIMARY KEY REFERENCES ask_sessions(id) ON DELETE CASCADE, custom_title TEXT, pinned INTEGER NOT NULL DEFAULT 0);
CREATE VIRTUAL TABLE IF NOT EXISTS ask_session_fts USING fts5(session_id UNINDEXED,title,questions,answers);
`

const askHistoryTriggers = `
CREATE TRIGGER IF NOT EXISTS ask_history_deleted AFTER DELETE ON ask_sessions BEGIN DELETE FROM ask_session_fts WHERE session_id=OLD.id; DELETE FROM ask_session_meta WHERE id=OLD.id; END;
`

const wikiCompilerSchema = `
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
`

const wikiInvalidationTriggers = `
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
`

const askEvidenceSchema = `CREATE TABLE IF NOT EXISTS ask_evidence_clearances (item_id TEXT PRIMARY KEY, cleared_at INTEGER NOT NULL);`

const themedReadingSchema = `
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
`

const mobileCaptureSchema = `
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
`

const webSnapshotSchema = `
CREATE TABLE IF NOT EXISTS web_snapshot_assets (
 version_id TEXT NOT NULL REFERENCES web_source_versions(id) ON DELETE CASCADE,
 file_name TEXT NOT NULL, PRIMARY KEY(version_id,file_name)
);
CREATE INDEX IF NOT EXISTS idx_web_snapshot_asset ON web_snapshot_assets(file_name);
`

const webCaptureSchema = `
CREATE TABLE IF NOT EXISTS crawl_jobs (
 id TEXT PRIMARY KEY, payload TEXT NOT NULL, status TEXT NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS crawl_pages (
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES crawl_jobs(id) ON DELETE CASCADE,
 url TEXT NOT NULL, depth INTEGER NOT NULL, seed_index INTEGER NOT NULL,
 status TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(job_id,url)
);
CREATE INDEX IF NOT EXISTS idx_crawl_pages_queue ON crawl_pages(job_id,status,depth);
CREATE TABLE IF NOT EXISTS web_source_versions (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
 payload TEXT NOT NULL, captured_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_web_versions_item ON web_source_versions(item_id,captured_at);
CREATE TABLE IF NOT EXISTS web_source_baselines (
 item_id TEXT PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
 version_id TEXT REFERENCES web_source_versions(id), content_hash TEXT NOT NULL,
 title TEXT NOT NULL, checked_at INTEGER NOT NULL, remote_hash TEXT NOT NULL,
 summary_stale INTEGER NOT NULL DEFAULT 0
);`

const researchEvidenceSchema = `
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
`

const researchDocumentSchema = `
CREATE TABLE IF NOT EXISTS research_documents (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
  candidate_id TEXT NOT NULL REFERENCES research_candidates(id) ON DELETE CASCADE,
  payload TEXT NOT NULL,
  UNIQUE(run_id, candidate_id)
);
CREATE INDEX IF NOT EXISTS idx_research_documents_run ON research_documents(run_id);
`

const researchSeriesSchema = `
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
`

// migrationNames 是 TS 迁移清单的名称与顺序（schema_version = 条数）。
var migrationNames = []string{
	"0001-baseline",
	"0002-wiki-ingestion-backoff",
	"0003-item-type-forum",
	"0004-import-task-tags",
	"0005-ai-usage-failed-calls",
	"0006-wiki-manual-edit",
	"0007-import-task-item-type",
	"0008-item-status-two-state",
	"0009-source-platform",
	"0010-import-task-warning",
	"0011-import-task-stage-stats",
	"0012-import-task-refresh-origin",
	"0013-knowledge-item-review-status",
	"0014-import-authenticated-capture",
	"0015-source-comments",
	"0016-source-comments-linuxdo",
	"0017-source-platform-refresh",
	"0018-source-platform-refresh",
	"0019-import-task-query-indexes",
	"0020-platform-discovery-workflows",
	"0021-background-jobs",
	"0022-discovery-candidate-identity",
	"0023-import-warning-acknowledgement",
	"0024-research-workflows",
	"0025-research-evidence",
	"0026-research-documents",
	"0027-research-series",
	"0028-source-access-uri",
	"0029-mobile-capture",
	"0030-web-capture",
	"0031-wechat-snapshots",
	"0032-themed-reading",
	"article-ask-sessions",
	"0034-ask-evidence-snapshots",
	"0035-wiki-fulltext-compiler",
	"0036-ask-history-search",
	"0037-source-capture-revisions",
	"0038-knowledge-batch-results",
	"0039-wiki-checkpoint-source-cleanup",
}
