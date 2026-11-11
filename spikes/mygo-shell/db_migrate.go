package main

// 数据库结构迁移执行器，对应 packages/db/src/migrations.ts。
//
// 为什么不用 MyGo 的 sqlite 插件：迁移要在事务内读（PRAGMA table_info、sqlite_master）、
// 要在事务外关外键（重建表类迁移），而插件的 Transaction 是静态语句批、读不了，且连接池下
// PRAGMA 不一定落在同一条连接上。所以启动时用 modernc 单连接把结构迁到位，再交给插件打开。
//
// 迁移清单的名称与顺序必须与 TS 完全一致（schema_migrations 里存的是名称，用户库里已有记录）；
// 建表/索引 SQL 常量由 gen-schema.mts 从 TS 源码原样导出（schema_gen.go），迁移里的 SQL 逐字对应。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// sqlRunner 是 *sql.Conn 事务与 *sql.Tx 的公共面（迁移只需要这几个方法）。
type sqlRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type dbMigration struct {
	name string
	// foreignKeysOff：重建表类迁移必须关外键强制，否则 DROP TABLE 会隐式 DELETE 并级联清空子表。
	// PRAGMA foreign_keys 在事务内是空操作，所以由执行器在 BEGIN 之前关、COMMIT 之后开。
	foreignKeysOff bool
	up             func(ctx context.Context, r sqlRunner) error
}

// ── 通用守卫 ──

func tableInfoNames(ctx context.Context, r sqlRunner, table string) ([]string, error) {
	rows, err := r.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	nameIdx := -1
	for i, c := range cols {
		if c == "name" {
			nameIdx = i
		}
	}
	var names []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if nameIdx >= 0 {
			switch v := vals[nameIdx].(type) {
			case string:
				names = append(names, v)
			case []byte:
				names = append(names, string(v))
			}
		}
	}
	return names, rows.Err()
}

func hasColumn(ctx context.Context, r sqlRunner, table, column string) (bool, error) {
	names, err := tableInfoNames(ctx, r, table)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if n == column {
			return true, nil
		}
	}
	return false, nil
}

// addColumnIfMissing：缺列才加；表不存在也跳过（与 TS 同一约定）。
func addColumnIfMissing(ctx context.Context, r sqlRunner, table, column, definition string) error {
	names, err := tableInfoNames(ctx, r, table)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	for _, n := range names {
		if n == column {
			return nil
		}
	}
	_, err = r.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

// getTableDefinition：建表语句原文；表不存在为空串。
func getTableDefinition(ctx context.Context, r sqlRunner, table string) (string, error) {
	var def sql.NullString
	err := r.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&def)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return def.String, nil
}

func execAll(ctx context.Context, r sqlRunner, stmts ...string) error {
	for _, s := range stmts {
		if _, err := r.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func queryAllStrings(ctx context.Context, r sqlRunner, query string, args ...any) ([][]sql.NullString, error) {
	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]sql.NullString
	for rows.Next() {
		row := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ── 具体迁移 ──

var knowledgeItemsColumns = []string{"id", "title", "content", "summary", "transcript", "item_type", "status", "collection_id", "is_favorite", "is_pinned", "deleted_at", "created_at", "updated_at"}

// DROP TABLE 会连带删掉表上的索引，重建后要一并补回。
var knowledgeItemsIndexes = []string{
	"CREATE INDEX IF NOT EXISTS idx_items_status ON knowledge_items(status)",
	"CREATE INDEX IF NOT EXISTS idx_items_collection ON knowledge_items(collection_id)",
	"CREATE INDEX IF NOT EXISTS idx_items_updated ON knowledge_items(updated_at DESC)",
	"CREATE INDEX IF NOT EXISTS idx_items_deleted ON knowledge_items(deleted_at)",
	"CREATE INDEX IF NOT EXISTS idx_items_favorite ON knowledge_items(is_favorite)",
}

func rebuildKnowledgeItems(ctx context.Context, r sqlRunner, statusDefault, statusCheck, selectedStatus string) error {
	columns := strings.Join(knowledgeItemsColumns, ", ")
	selected := make([]string, len(knowledgeItemsColumns))
	for i, c := range knowledgeItemsColumns {
		selected[i] = c
		if c == "status" && selectedStatus != "" {
			selected[i] = selectedStatus
		}
	}
	script := fmt.Sprintf(`
        CREATE TABLE knowledge_items_migrate (
          id TEXT PRIMARY KEY,
          title TEXT NOT NULL DEFAULT '',
          content TEXT NOT NULL DEFAULT '',
          summary TEXT,
          transcript TEXT,
          item_type TEXT NOT NULL DEFAULT 'note'
            CHECK(item_type IN ('note','webpage','video','image','audio','document','snippet','forum')),
          status TEXT NOT NULL DEFAULT '%s'
            CHECK(status IN (%s)),
          collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL,
          is_favorite INTEGER NOT NULL DEFAULT 0,
          is_pinned INTEGER NOT NULL DEFAULT 0,
          deleted_at INTEGER,
          created_at INTEGER NOT NULL,
          updated_at INTEGER NOT NULL
        );
        INSERT INTO knowledge_items_migrate (%s)
          SELECT %s FROM knowledge_items;
        DROP TABLE knowledge_items;
        ALTER TABLE knowledge_items_migrate RENAME TO knowledge_items;
        %s;
      `, statusDefault, statusCheck, columns, strings.Join(selected, ", "), strings.Join(knowledgeItemsIndexes, ";\n        "))
	_, err := r.ExecContext(ctx, script)
	return err
}

// refreshSourcePlatforms：按当前连接器白名单重算 source_records.platform。
func refreshSourcePlatforms(ctx context.Context, r sqlRunner) error {
	def, err := getTableDefinition(ctx, r, "source_records")
	if err != nil || def == "" {
		return err
	}
	type rec struct {
		id, kind string
		uri      *string
	}
	var recs []rec
	rows, err := r.QueryContext(ctx, "SELECT id, source_type, source_uri FROM source_records")
	if err != nil {
		return err
	}
	for rows.Next() {
		var x rec
		var uri sql.NullString
		if err := rows.Scan(&x.id, &x.kind, &uri); err != nil {
			rows.Close()
			return err
		}
		if uri.Valid {
			s := uri.String
			x.uri = &s
		}
		recs = append(recs, x)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, x := range recs {
		var platform any
		if p, ok := resolveSourcePlatform(x.kind, x.uri); ok {
			platform = p
		}
		if _, err := r.ExecContext(ctx, "UPDATE source_records SET platform = ? WHERE id = ?", platform, x.id); err != nil {
			return err
		}
	}
	return nil
}

const backfillSourceAccessSQL = `
    WITH single_source AS (
      SELECT item_id, MIN(id) AS source_id FROM source_records
      GROUP BY item_id HAVING COUNT(*) = 1
    ), candidates AS (
      SELECT s.source_id, t.source_input,
        ROW_NUMBER() OVER (
          PARTITION BY s.source_id ORDER BY t.updated_at DESC, t.created_at DESC, t.id DESC
        ) AS priority
      FROM single_source s JOIN import_tasks t ON t.result_item_id = s.item_id
      WHERE t.source_kind = 'url' AND t.status = 'completed'
        AND (t.source_input LIKE 'https://%' OR t.source_input LIKE 'http://%')
    )
    UPDATE source_records SET access_uri = (
      SELECT source_input FROM candidates WHERE source_id = source_records.id AND priority = 1
    )
    WHERE source_type = 'url' AND (access_uri IS NULL OR trim(access_uri) = '')
      AND id IN (SELECT source_id FROM candidates WHERE priority = 1);
  `

var (
	webCaptureConstraint = regexp.MustCompile(`'xiaohongshu'\s*,\s*'douyin'\s*,\s*'bilibili'`)
)

// migrateWebCapture 对应 web-capture-schema.ts：在关外键的事务里重建 CHECK，保留真实表定义、全部列与索引。
func migrateWebCapture(ctx context.Context, r sqlRunner) error {
	if _, err := r.ExecContext(ctx, webCaptureSchema); err != nil {
		return err
	}
	for _, name := range []string{"research_source_runs", "research_candidates"} {
		var tableSQL sql.NullString
		err := r.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&tableSQL)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !tableSQL.Valid) {
			continue
		}
		if err != nil {
			return err
		}
		if strings.Contains(tableSQL.String, "'web'") {
			continue
		}
		expanded := webCaptureConstraint.ReplaceAllString(tableSQL.String, "'xiaohongshu','douyin','bilibili','web'")
		if expanded == tableSQL.String {
			return fmt.Errorf("无法识别 %s 的来源约束", name)
		}
		idxRows, err := queryAllStrings(ctx, r, "SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL", name)
		if err != nil {
			return err
		}
		names, err := tableInfoNames(ctx, r, name)
		if err != nil {
			return err
		}
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = `"` + n + `"`
		}
		columns := strings.Join(quoted, ",")
		var before, after int64
		if err := r.QueryRowContext(ctx, "SELECT COUNT(*) AS count FROM "+name).Scan(&before); err != nil {
			return err
		}
		// 仅替换第一处表名（JS 的 replace(RegExp) 无 g 标志）
		nameRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if loc := nameRe.FindStringIndex(expanded); loc != nil {
			expanded = expanded[:loc[0]] + name + "_web_new" + expanded[loc[1]:]
		}
		if _, err := r.ExecContext(ctx, expanded); err != nil {
			return err
		}
		if _, err := r.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s_web_new (%s) SELECT %s FROM %s; DROP TABLE %s; ALTER TABLE %s_web_new RENAME TO %s;", name, columns, columns, name, name, name, name)); err != nil {
			return err
		}
		for _, ix := range idxRows {
			if _, err := r.ExecContext(ctx, ix[0].String); err != nil {
				return err
			}
		}
		if err := r.QueryRowContext(ctx, "SELECT COUNT(*) AS count FROM "+name).Scan(&after); err != nil {
			return err
		}
		if before != after {
			return fmt.Errorf("迁移 %s 行数不一致", name)
		}
	}
	cols, err := tableInfoNames(ctx, r, "research_runs")
	if err != nil {
		return err
	}
	if len(cols) > 0 {
		found := false
		for _, c := range cols {
			if c == "time_scope" {
				found = true
			}
		}
		if !found {
			_, err = r.ExecContext(ctx, "ALTER TABLE research_runs ADD COLUMN time_scope TEXT NOT NULL DEFAULT 'recent' CHECK(time_scope IN ('recent','all'))")
			return err
		}
	}
	return nil
}

// writeAskHistory 对应 ask-history.ts：按会话 id 重写搜索索引行。
func writeAskHistory(ctx context.Context, r sqlRunner, id string) error {
	var title, messagesJSON string
	err := r.QueryRowContext(ctx, "SELECT COALESCE(m.custom_title,s.title) AS title,s.messages_json FROM ask_sessions s LEFT JOIN ask_session_meta m ON m.id=s.id WHERE s.id=?", id).Scan(&title, &messagesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var questions, answers []string
	var parsed any
	if json.Unmarshal([]byte(messagesJSON), &parsed) == nil { // 损坏的老消息仍可按标题检索
		if list, ok := parsed.([]any); ok {
			field := func(m any, k string) string {
				obj, ok := m.(map[string]any)
				if !ok {
					return ""
				}
				v, ok := obj[k]
				if !ok || v == nil {
					return ""
				}
				return jsToString(v)
			}
			for _, m := range list {
				questions = append(questions, field(m, "question"))
				answers = append(answers, field(m, "answer"))
			}
		}
	}
	if _, err := r.ExecContext(ctx, "DELETE FROM ask_session_fts WHERE session_id=?", id); err != nil {
		return err
	}
	_, err = r.ExecContext(ctx, "INSERT INTO ask_session_fts(session_id,title,questions,answers) VALUES(?,?,?,?)",
		id, SegmentTextForFts(title), SegmentTextForFts(strings.Join(questions, "\n")), SegmentTextForFts(strings.Join(answers, "\n")))
	return err
}

func migrateAskHistory(ctx context.Context, r sqlRunner) error {
	if _, err := r.ExecContext(ctx, askHistorySchema); err != nil {
		return err
	}
	def, err := getTableDefinition(ctx, r, "ask_sessions")
	if err != nil || def == "" {
		return err
	}
	if _, err := r.ExecContext(ctx, askHistoryTriggers); err != nil {
		return err
	}
	rows, err := queryAllStrings(ctx, r, "SELECT id FROM ask_sessions")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeAskHistory(ctx, r, row[0].String); err != nil {
			return err
		}
	}
	return nil
}

func execSchema(schema *string) func(context.Context, sqlRunner) error {
	return func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, *schema)
		return err
	}
}

func addColumn(table, column, definition string) func(context.Context, sqlRunner) error {
	return func(ctx context.Context, r sqlRunner) error {
		return addColumnIfMissing(ctx, r, table, column, definition)
	}
}

const (
	sourcePlatformIndexSQL = `CREATE INDEX IF NOT EXISTS idx_sources_platform ON source_records(platform)`
	sourceCommentsLinuxdo  = `
        CREATE TABLE source_comments_migrate (
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
        INSERT INTO source_comments_migrate
          (id, item_id, platform, external_id, author_name, content,
           like_count, published_at, captured_at)
          SELECT id, item_id, platform, external_id, author_name, content,
                 like_count, published_at, captured_at
          FROM source_comments;
        DROP TABLE source_comments;
        ALTER TABLE source_comments_migrate RENAME TO source_comments;
        CREATE INDEX IF NOT EXISTS idx_source_comments_item
          ON source_comments(item_id, captured_at DESC);
      `
	sourceCommentsSchema = `
        CREATE TABLE IF NOT EXISTS source_comments (
          id TEXT PRIMARY KEY,
          item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
          platform TEXT NOT NULL CHECK(platform IN ('xiaohongshu','douyin')),
          external_id TEXT NOT NULL,
          author_name TEXT NOT NULL DEFAULT '',
          content TEXT NOT NULL DEFAULT '',
          like_count INTEGER NOT NULL DEFAULT 0,
          published_at INTEGER,
          captured_at INTEGER NOT NULL,
          UNIQUE(item_id, platform, external_id)
        );
        CREATE INDEX IF NOT EXISTS idx_source_comments_item
          ON source_comments(item_id, captured_at DESC);
      `
	importTaskQueryIndexes = `
        CREATE INDEX IF NOT EXISTS idx_import_tasks_created_id
          ON import_tasks(created_at DESC, id DESC);
        CREATE INDEX IF NOT EXISTS idx_import_tasks_status_created_id
          ON import_tasks(status, created_at DESC, id DESC);
      `
	platformDiscoverySchema = `
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
        CREATE INDEX IF NOT EXISTS idx_discovery_views_due
          ON platform_discovery_views(enabled, next_run_at);
        CREATE INDEX IF NOT EXISTS idx_discovery_runs_view
          ON platform_discovery_runs(view_id, started_at DESC);
        CREATE INDEX IF NOT EXISTS idx_discovery_candidates_state
          ON platform_discovery_candidates(view_id, state, first_seen_at DESC);
        CREATE UNIQUE INDEX IF NOT EXISTS idx_discovery_candidates_external
          ON platform_discovery_candidates(platform, external_id);
      `
	backgroundJobsSchema = `
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
        CREATE INDEX IF NOT EXISTS idx_background_jobs_due
          ON background_jobs(state, next_run_at);
      `
	discoveryCandidateIdentitySQL = `
        DELETE FROM platform_discovery_candidates
        WHERE rowid NOT IN (
          SELECT MAX(rowid) FROM platform_discovery_candidates
          GROUP BY platform, external_id
        );
        CREATE UNIQUE INDEX IF NOT EXISTS idx_discovery_candidates_external
          ON platform_discovery_candidates(platform, external_id);
      `
	researchWorkflowsSchema = `
        CREATE TABLE IF NOT EXISTS research_runs (
          id TEXT PRIMARY KEY,
          topic TEXT NOT NULL,
          day_range INTEGER NOT NULL CHECK(day_range IN (7,14,30)),
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
          source TEXT NOT NULL CHECK(source IN ('xiaohongshu','douyin','bilibili')),
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
          source TEXT NOT NULL CHECK(source IN ('xiaohongshu','douyin','bilibili')),
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
      `
	wikiCheckpointCleanupTrigger = `CREATE TRIGGER IF NOT EXISTS wiki_checkpoint_source_deleted AFTER DELETE ON knowledge_items
      BEGIN DELETE FROM wiki_compile_blocks WHERE item_id=OLD.id; DELETE FROM wiki_compile_items WHERE item_id=OLD.id; UPDATE wiki_compile_jobs SET status='cancelled',error='编译来源已彻底删除' WHERE status IN ('running','paused','interrupted') AND NOT EXISTS(SELECT 1 FROM wiki_compile_items i WHERE i.job_id=wiki_compile_jobs.id); END;`
	wikiCheckpointCleanupDelete = `DELETE FROM wiki_compile_blocks WHERE item_id NOT IN (SELECT id FROM knowledge_items); DELETE FROM wiki_compile_items WHERE item_id NOT IN (SELECT id FROM knowledge_items)`
)

// 与 TS 一致的前置检查：表不存在（只建了半个库）就跳过。
func whenTable(table string, fn func(context.Context, sqlRunner) error) func(context.Context, sqlRunner) error {
	return func(ctx context.Context, r sqlRunner) error {
		def, err := getTableDefinition(ctx, r, table)
		if err != nil || def == "" {
			return err
		}
		return fn(ctx, r)
	}
}

var dbMigrations = []dbMigration{
	{name: "0001-baseline", up: func(context.Context, sqlRunner) error { return nil }},
	{name: "0002-wiki-ingestion-backoff", up: func(ctx context.Context, r sqlRunner) error {
		if err := addColumnIfMissing(ctx, r, "wiki_ingestions", "failure_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
		return addColumnIfMissing(ctx, r, "wiki_ingestions", "next_attempt_at", "INTEGER")
	}},
	{name: "0003-item-type-forum", foreignKeysOff: true, up: func(ctx context.Context, r sqlRunner) error {
		def, err := getTableDefinition(ctx, r, "knowledge_items")
		if err != nil || def == "" || strings.Contains(def, "'forum'") {
			return err
		}
		return rebuildKnowledgeItems(ctx, r, "inbox", "'inbox','ready','archived'", "")
	}},
	{name: "0004-import-task-tags", up: addColumn("import_tasks", "tag_names", "TEXT")},
	{name: "0005-ai-usage-failed-calls", up: addColumn("ai_usage_daily", "failed_calls", "INTEGER NOT NULL DEFAULT 0")},
	{name: "0006-wiki-manual-edit", up: addColumn("wiki_pages", "manual_edited_at", "INTEGER")},
	{name: "0007-import-task-item-type", up: addColumn("import_tasks", "item_type", "TEXT")},
	{name: "0008-item-status-two-state", foreignKeysOff: true, up: func(ctx context.Context, r sqlRunner) error {
		def, err := getTableDefinition(ctx, r, "knowledge_items")
		if err != nil || def == "" || strings.Contains(def, "'active'") {
			return err
		}
		// 旧 CHECK 不放行 'active'，没法先 UPDATE 再重建，只能在拷贝时就地折叠
		return rebuildKnowledgeItems(ctx, r, "active", "'active','archived'", "CASE WHEN status = 'archived' THEN 'archived' ELSE 'active' END")
	}},
	{name: "0009-source-platform", up: func(ctx context.Context, r sqlRunner) error {
		def, err := getTableDefinition(ctx, r, "source_records")
		if err != nil || def == "" {
			return err
		}
		if _, err := r.ExecContext(ctx, sourcePlatformIndexSQL); err != nil {
			return err
		}
		return refreshSourcePlatforms(ctx, r)
	}},
	{name: "0010-import-task-warning", up: addColumn("import_tasks", "warning", "TEXT")},
	{name: "0011-import-task-stage-stats", up: addColumn("import_tasks", "stage_stats", "TEXT")},
	{name: "0012-import-task-refresh-origin", up: addColumn("import_tasks", "refresh_of_item_id", "TEXT")},
	{name: "0013-knowledge-item-review-status", up: func(ctx context.Context, r sqlRunner) error {
		if err := addColumnIfMissing(ctx, r, "knowledge_items", "review_status", "TEXT NOT NULL DEFAULT 'clear'"); err != nil {
			return err
		}
		return addColumnIfMissing(ctx, r, "knowledge_items", "review_reasons", "TEXT")
	}},
	{name: "0014-import-authenticated-capture", up: func(ctx context.Context, r sqlRunner) error {
		if err := addColumnIfMissing(ctx, r, "import_tasks", "capture_strategy", "TEXT NOT NULL DEFAULT 'standard'"); err != nil {
			return err
		}
		return addColumnIfMissing(ctx, r, "import_tasks", "comment_limit", "INTEGER NOT NULL DEFAULT 0")
	}},
	{name: "0015-source-comments", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, sourceCommentsSchema)
		return err
	}},
	{name: "0016-source-comments-linuxdo", up: func(ctx context.Context, r sqlRunner) error {
		def, err := getTableDefinition(ctx, r, "source_comments")
		if err != nil || def == "" || strings.Contains(def, "'linuxdo'") {
			return err
		}
		if k, err := getTableDefinition(ctx, r, "knowledge_items"); err != nil || k == "" {
			return err
		}
		_, err = r.ExecContext(ctx, sourceCommentsLinuxdo)
		return err
	}},
	{name: "0017-source-platform-refresh", up: refreshSourcePlatforms},
	{name: "0018-source-platform-refresh", up: refreshSourcePlatforms},
	{name: "0019-import-task-query-indexes", up: whenTable("import_tasks", func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, importTaskQueryIndexes)
		return err
	})},
	{name: "0020-platform-discovery-workflows", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, platformDiscoverySchema)
		return err
	}},
	{name: "0021-background-jobs", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, backgroundJobsSchema)
		return err
	}},
	{name: "0022-discovery-candidate-identity", up: whenTable("platform_discovery_candidates", func(ctx context.Context, r sqlRunner) error {
		// 早期开发库按 view_id 分隔同一平台内容；正式契约要求平台内容全局唯一。
		_, err := r.ExecContext(ctx, discoveryCandidateIdentitySQL)
		return err
	})},
	{name: "0023-import-warning-acknowledgement", up: addColumn("import_tasks", "warning_acknowledged_at", "INTEGER")},
	{name: "0024-research-workflows", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, researchWorkflowsSchema)
		return err
	}},
	{name: "0025-research-evidence", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, researchEvidenceSchema)
		return err
	}},
	{name: "0026-research-documents", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, researchDocumentSchema)
		return err
	}},
	{name: "0027-research-series", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, researchSeriesSchema)
		return err
	}},
	{name: "0028-source-access-uri", up: func(ctx context.Context, r sqlRunner) error {
		if err := addColumnIfMissing(ctx, r, "source_records", "access_uri", "TEXT"); err != nil {
			return err
		}
		a, err := hasColumn(ctx, r, "source_records", "access_uri")
		if err != nil {
			return err
		}
		b, err := hasColumn(ctx, r, "import_tasks", "result_item_id")
		if err != nil {
			return err
		}
		if a && b {
			_, err = r.ExecContext(ctx, backfillSourceAccessSQL)
		}
		return err
	}},
	{name: "0029-mobile-capture", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, mobileCaptureSchema)
		return err
	}},
	{name: "0030-web-capture", foreignKeysOff: true, up: migrateWebCapture},
	{name: "0031-wechat-snapshots", up: func(ctx context.Context, r sqlRunner) error {
		if _, err := r.ExecContext(ctx, webSnapshotSchema); err != nil {
			return err
		}
		def, err := getTableDefinition(ctx, r, "source_records")
		if err != nil || def == "" {
			return err
		}
		rows, err := queryAllStrings(ctx, r, "SELECT id,source_uri FROM source_records WHERE source_type='url'")
		if err != nil {
			return err
		}
		for _, row := range rows {
			var uri *string
			if row[1].Valid {
				s := row[1].String
				uri = &s
			}
			if p, ok := resolveSourcePlatform("url", uri); ok && p == "wechat" {
				if _, err := r.ExecContext(ctx, "UPDATE source_records SET platform='wechat' WHERE id=?", row[0].String); err != nil {
					return err
				}
			}
		}
		return nil
	}},
	{name: "0032-themed-reading", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, themedReadingSchema)
		return err
	}},
	{name: "article-ask-sessions", up: func(ctx context.Context, r sqlRunner) error {
		for _, c := range [][2]string{
			{"scope", "TEXT NOT NULL DEFAULT 'knowledge'"}, {"item_id", "TEXT"}, {"article_title", "TEXT"}, {"options_json", "TEXT NOT NULL DEFAULT '{}'"},
		} {
			if err := addColumnIfMissing(ctx, r, "ask_sessions", c[0], c[1]); err != nil {
				return err
			}
		}
		ok, err := hasColumn(ctx, r, "ask_sessions", "scope")
		if err != nil || !ok {
			return err
		}
		_, err = r.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_ask_sessions_article ON ask_sessions(scope,item_id,updated_at DESC)")
		return err
	}},
	{name: "0034-ask-evidence-snapshots", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, askEvidenceSchema)
		return err
	}},
	{name: "0035-wiki-fulltext-compiler", up: func(ctx context.Context, r sqlRunner) error {
		if _, err := r.ExecContext(ctx, wikiCompilerSchema); err != nil {
			return err
		}
		def, err := getTableDefinition(ctx, r, "knowledge_items")
		if err != nil || def == "" {
			return err
		}
		_, err = r.ExecContext(ctx, wikiInvalidationTriggers)
		return err
	}},
	{name: "0036-ask-history-search", up: migrateAskHistory},
	{name: "0037-source-capture-revisions", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, sourceRevisionsSchema)
		return err
	}},
	{name: "0038-knowledge-batch-results", up: func(ctx context.Context, r sqlRunner) error {
		_, err := r.ExecContext(ctx, knowledgeBatchSchema)
		return err
	}},
	{name: "0039-wiki-checkpoint-source-cleanup", up: whenTable("knowledge_items", func(ctx context.Context, r sqlRunner) error {
		if _, err := r.ExecContext(ctx, wikiCheckpointCleanupTrigger); err != nil {
			return err
		}
		_, err := r.ExecContext(ctx, wikiCheckpointCleanupDelete)
		return err
	})},
}

// schemaVersion 对应 SCHEMA_VERSION（迁移条数，写入 PRAGMA user_version）。
func schemaVersion() int { return len(dbMigrations) }

func assertNoForeignKeyViolations(ctx context.Context, r sqlRunner) error {
	rows, err := r.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	count := 0
	tables := map[string]bool{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		count++
		name := "?"
		if len(vals) > 0 {
			if s, ok := vals[0].(string); ok {
				name = s
			}
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var names []string
	for t := range tables {
		names = append(names, t)
	}
	sort.Strings(names)
	return fmt.Errorf("迁移后存在外键孤儿行（%d 行，涉及 %s）", count, strings.Join(names, ", "))
}

// applyMigration 在单独的事务里执行一条迁移并登记；foreignKeysOff 的迁移在事务外关/开外键。
func applyMigration(ctx context.Context, conn *sql.Conn, m dbMigration) (err error) {
	if m.foreignKeysOff {
		if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return err
		}
		defer func() {
			if _, e := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); e != nil && err == nil {
				err = e
			}
		}()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = m.up(ctx, tx); err == nil && m.foreignKeysOff {
		err = assertNoForeignKeyViolations(ctx, tx)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)", m.name, time.Now().UnixMilli())
	}
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("迁移 %s 失败：%w", m.name, err)
	}
	return tx.Commit()
}

// runMigrations 执行所有未应用的迁移（每条独立事务：失败时前面成功的保持已应用，重启后从失败那条继续），返回本次执行的名称。
func runMigrations(ctx context.Context, conn *sql.Conn) ([]string, error) {
	if _, err := conn.ExecContext(ctx, `
    CREATE TABLE IF NOT EXISTS schema_migrations (
      name TEXT PRIMARY KEY,
      applied_at INTEGER NOT NULL
    )
  `); err != nil {
		return nil, err
	}
	applied := map[string]bool{}
	rows, err := queryAllStrings(ctx, conn, "SELECT name FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		applied[r[0].String] = true
	}
	var executed []string
	for _, m := range dbMigrations {
		if applied[m.name] {
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return executed, err
		}
		executed = append(executed, m.name)
	}
	// user_version 是备份/恢复做版本比对的依据
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion())); err != nil {
		return executed, err
	}
	return executed, nil
}
