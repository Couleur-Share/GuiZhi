package main

// 对应 apps/desktop/src/main/ipc/migration.ipc.ts 与 packages/db/src/legacy-migration.ts：
// 旧版 .NET 归知数据库的探测与一次性迁移。
//
// 与 TS 的差异：旧库是 WAL 模式，TS 的 wasm 驱动不支持，所以拷贝后把文件头 18/19 字节降为 1；
// 这里沿用同一做法（拷贝上操作，绝不碰原文件；有残留 -wal 就拒绝），行为保持一致。
// MyGo 事务是一批静态语句，不能穿插读取：全部读取先在旧库拷贝上做完，再拼成一个事务提交。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

const (
	chMigrationDetectLegacy = "migration:detectLegacy"
	chMigrationRunLegacy    = "migration:runLegacy"
)

type legacyMigrationStats struct {
	Collections    int `json:"collections"`
	Tags           int `json:"tags"`
	Items          int `json:"items"`
	ItemTags       int `json:"itemTags"`
	Sources        int `json:"sources"`
	WikiPages      int `json:"wikiPages"`
	WikiLinks      int `json:"wikiLinks"`
	WikiSources    int `json:"wikiSources"`
	WikiIngestions int `json:"wikiIngestions"`
}

// legacyValue 把 SQLite 取出的值换成 JS 语义的值，复用 jsToNumber 等 Number() 规则。
func legacyValue(v any) any {
	switch x := v.(type) {
	case nil:
		return jsNull
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case []byte:
		return string(x)
	}
	return v
}

// legacyNum 对应 `typeof value === "number" ? value : Number(value)`。
func legacyNum(v any) float64 { return jsToNumber(legacyValue(v)) }

func legacyIndex(v any, n int) (int, bool) {
	f := legacyNum(v)
	if math.IsNaN(f) || f != math.Trunc(f) || f < 0 || f >= float64(n) {
		return 0, false
	}
	return int(f), true
}

var legacyItemTypes = []string{"note", "webpage", "video", "image", "audio", "document", "snippet"}
var legacyTagColors = []string{"teal", "blue", "purple", "pink", "amber", "green"}
var legacyWikiKinds = []string{"topic", "entity", "concept"}

func mapLegacyItemType(v any) string {
	if i, ok := legacyIndex(v, len(legacyItemTypes)); ok {
		return legacyItemTypes[i]
	}
	return "note"
}

func mapLegacyStatus(v any) string {
	if legacyNum(v) == 3 {
		return "archived"
	}
	return "active"
}

func mapLegacyTagColor(v any) string {
	if i, ok := legacyIndex(v, len(legacyTagColors)); ok {
		return legacyTagColors[i]
	}
	return "gray"
}

func mapLegacySourceType(v any) string {
	switch legacyNum(v) {
	case 2:
		return "file"
	case 3, 4:
		return "url"
	}
	return "text"
}

func mapLegacyWikiKind(v any) string {
	if i, ok := legacyIndex(v, len(legacyWikiKinds)); ok {
		return legacyWikiKinds[i]
	}
	return "topic"
}

var (
	legacyTZRe  = regexp.MustCompile(`[zZ]|[+-]\d{2}:?\d{2}$`)
	legacyTimeR = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d+))?)?)?(Z|[+-]\d{2}:?\d{2})$`)
)

// parseLegacyUtc 对应 parseLegacyUtc：EF Core 的 UTC 文本（无时区标记）→ 毫秒；解析不了回落到当前时间。
func parseLegacyUtc(v any) int64 {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Now().UnixMilli()
	}
	n := strings.Replace(jsTrim(s), " ", "T", 1)
	if !legacyTZRe.MatchString(n) {
		n += "Z"
	}
	if ms, ok := legacyDateParse(n); ok {
		return ms
	}
	return time.Now().UnixMilli()
}

// legacyDateParse 覆盖 ISO 8601 形态（含 EF Core 的 7 位小数，按 V8 截断到毫秒）。
func legacyDateParse(s string) (int64, bool) {
	m := legacyTimeR.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	frac := 0
	if m[7] != "" {
		f := (m[7] + "000")[:3]
		frac = atoi(f)
	}
	mo, d := atoi(m[2]), atoi(m[3])
	h, mi, sec := atoi(m[4]), atoi(m[5]), atoi(m[6])
	if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 24 || mi > 59 || sec > 59 {
		return 0, false
	}
	loc := time.UTC
	if m[8] != "Z" && m[8] != "z" {
		sign := 1
		if m[8][0] == '-' {
			sign = -1
		}
		digits := strings.ReplaceAll(m[8][1:], ":", "")
		off := sign * (atoi(digits[:2])*3600 + atoi(digits[2:])*60)
		loc = time.FixedZone("", off)
	}
	t := time.Date(atoi(m[1]), time.Month(mo), d, h, mi, sec, frac*int(time.Millisecond), loc)
	if t.Day() != d && h != 24 { // 2月30日这类日期 V8 同样判无效
		return 0, false
	}
	return t.UnixMilli(), true
}

func legacyTargetEmpty(ctx context.Context, db *sqlite.DB) (bool, error) {
	rows, err := db.Query(ctx, "SELECT COUNT(*) AS c FROM knowledge_items")
	if err != nil {
		return false, err
	}
	if len(rows.Values) == 0 {
		return true, nil
	}
	return asInt(rows.Values[0][0]) == 0, nil
}

type legacyDB struct{ db *sqlite.DB }

func (d legacyDB) hasTable(ctx context.Context, name string) bool {
	rows, err := d.db.Query(ctx, "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?", name)
	return err == nil && len(rows.Values) > 0
}

type legacyRow map[string]any

func (d legacyDB) readAll(ctx context.Context, sql string) ([]legacyRow, error) {
	rows, err := d.db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]legacyRow, 0, len(rows.Values))
	for _, vals := range rows.Values {
		r := legacyRow{}
		for i, c := range rows.Columns {
			if _, dup := r[c]; !dup { // 与 SELECT * 的同名列取值一致（先出现者生效）
				r[c] = vals[i]
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (r legacyRow) or(key string, def any) any {
	if v := r[key]; v != nil {
		return v
	}
	return def
}

// str 对应 String(row.X)。
func (r legacyRow) str(key string) string {
	switch x := r[key].(type) {
	case nil:
		return "null"
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return jsNumberString(x)
	case []byte:
		return string(x)
	}
	return fmt.Sprint(r[key])
}

// truthy 对应 JS 真值判断。
func (r legacyRow) truthy(key string) bool {
	switch x := r[key].(type) {
	case nil:
		return false
	case int64:
		return x != 0
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// withLegacyCopy 在旧库的临时拷贝上执行操作（绝不触碰原文件）。
func withLegacyCopy[T any](ctx context.Context, sourcePath string, fn func(legacyDB) (T, error)) (T, error) {
	var zero T
	if st, err := os.Stat(sourcePath + "-wal"); err == nil && st.Size() > 0 {
		return zero, fmt.Errorf("旧版数据库有未合并的写入日志。请先打开一次旧版归知并正常退出，再重试迁移。")
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("guizhi-legacy-migration-%d.db", time.Now().UnixMilli()))
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return zero, err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return zero, err
	}
	defer os.Remove(tmp)
	if len(data) >= 20 && (data[18] == 2 || data[19] == 2) {
		data[18], data[19] = 1, 1
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return zero, err
		}
	}
	db, err := sqlite.Open(ctx, tmp, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer db.Close()
	return fn(legacyDB{db})
}

func (d legacyDB) countItems(ctx context.Context) (int64, error) {
	if !d.hasTable(ctx, "KnowledgeItems") {
		return 0, nil
	}
	rows, err := d.db.Query(ctx, "SELECT COUNT(*) AS c FROM KnowledgeItems WHERE IsDeleted = 0")
	if err != nil {
		return 0, err
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	return asInt(rows.Values[0][0]), nil
}

// build 读出旧库全部需要的行，拼成一个事务的语句批与统计。
func (d legacyDB) build(ctx context.Context) ([]sqlite.Statement, legacyMigrationStats, error) {
	var stats legacyMigrationStats
	var stmts []sqlite.Statement
	add := func(sql string, args ...any) { stmts = append(stmts, sqlite.Statement{SQL: sql, Args: args}) }

	// 集合（跳过软删除的）
	collectionIDs := map[string]bool{}
	rows, err := d.readAll(ctx, "SELECT * FROM Collections WHERE IsDeleted = 0")
	if err != nil {
		return nil, stats, err
	}
	for _, r := range rows {
		add(`INSERT INTO collections (id, name, icon, sort_order, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`, r["Id"], r.or("Name", ""), r["Icon"], r.or("SortOrder", int64(0)), parseLegacyUtc(r["CreatedAtUtc"]), parseLegacyUtc(r["UpdatedAtUtc"]))
		collectionIDs[fmt.Sprint(r["Id"])] = true
		stats.Collections++
	}

	// 标签
	tagNames := map[string]string{}
	if rows, err = d.readAll(ctx, "SELECT * FROM Tags"); err != nil {
		return nil, stats, err
	}
	for _, r := range rows {
		add(`INSERT INTO tags (id, name, color_key, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?)`, r["Id"], r.or("Name", ""), mapLegacyTagColor(r["ColorKey"]), parseLegacyUtc(r["CreatedAtUtc"]), parseLegacyUtc(r["UpdatedAtUtc"]))
		name := r.or("Name", "")
		tagNames[fmt.Sprint(r["Id"])] = fmt.Sprint(name)
		stats.Tags++
	}

	// 知识条目
	itemIDs := map[string]bool{}
	var itemOrder []string
	itemTitles := map[string]string{}
	itemContents := map[string]string{}
	if rows, err = d.readAll(ctx, "SELECT * FROM KnowledgeItems"); err != nil {
		return nil, stats, err
	}
	for _, r := range rows {
		id := r.str("Id")
		var collectionID any
		if r.truthy("CollectionId") && collectionIDs[r.str("CollectionId")] {
			collectionID = r.str("CollectionId")
		}
		var deletedAt any
		if v, ok := r["IsDeleted"].(int64); ok && v == 1 {
			deletedAt = parseLegacyUtc(r["DeletedAtUtc"])
		}
		boolInt := func(k string) int {
			if r.truthy(k) {
				return 1
			}
			return 0
		}
		add(`INSERT INTO knowledge_items
           (id, title, content, summary, transcript, item_type, status, collection_id,
            is_favorite, is_pinned, deleted_at, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, r.or("Title", ""), r.or("Content", ""), r["Summary"], r["Transcript"],
			mapLegacyItemType(r["ItemType"]), mapLegacyStatus(r["Status"]), collectionID,
			boolInt("IsFavorite"), boolInt("IsPinned"), deletedAt,
			parseLegacyUtc(r["CreatedAtUtc"]), parseLegacyUtc(r["UpdatedAtUtc"]))
		if !itemIDs[id] {
			itemOrder = append(itemOrder, id)
		}
		itemIDs[id] = true
		itemTitles[id] = fmt.Sprint(r.or("Title", ""))
		itemContents[id] = fmt.Sprint(r.or("Content", ""))
		stats.Items++
	}

	// 条目-标签关联
	itemTagSet := map[string]map[string]bool{}
	if rows, err = d.readAll(ctx, "SELECT * FROM KnowledgeItemTags"); err != nil {
		return nil, stats, err
	}
	for _, r := range rows {
		itemID, tagID := r.str("KnowledgeItemId"), r.str("TagId")
		if _, ok := tagNames[tagID]; itemIDs[itemID] && ok {
			add("INSERT OR IGNORE INTO knowledge_item_tags (item_id, tag_id) VALUES (?, ?)", itemID, tagID)
			if itemTagSet[itemID] == nil {
				itemTagSet[itemID] = map[string]bool{}
			}
			itemTagSet[itemID][tagID] = true
			stats.ItemTags++
		}
	}

	// 来源记录
	if rows, err = d.readAll(ctx, `SELECT s.*, i.Id AS OwnerItemId FROM SourceRecords s
       JOIN KnowledgeItems i ON i.SourceId = s.Id`); err != nil {
		return nil, stats, err
	}
	for _, r := range rows {
		itemID := r.str("OwnerItemId")
		if !itemIDs[itemID] {
			continue
		}
		add(`INSERT INTO source_records
           (id, item_id, source_type, source_uri, normalized_uri, content_hash, platform, captured_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r["Id"], itemID, mapLegacySourceType(r["SourceType"]), r["OriginalUri"], r["CanonicalUri"], r["ContentHash"], r["Platform"], parseLegacyUtc(r["CapturedAtUtc"]))
		stats.Sources++
	}

	// FTS 重建（含回收站条目）。同一条目的标签名按标签 id 排序（与 SQLite 对主键索引的扫描顺序一致）
	for _, id := range itemOrder {
		var names []string
		var tagIDs []string
		for t := range itemTagSet[id] {
			tagIDs = append(tagIDs, t)
		}
		sort.Strings(tagIDs)
		for _, t := range tagIDs {
			names = append(names, tagNames[t])
		}
		add("INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, ?)",
			id, SegmentTextForFts(itemTitles[id]), SegmentTextForFts(itemContents[id]), SegmentTextForFts(strings.Join(names, " ")))
	}

	// Wiki 四表（旧库可能没有 Wiki 表）
	if d.hasTable(ctx, "WikiPages") {
		pageIDs := map[string]bool{}
		if rows, err = d.readAll(ctx, "SELECT * FROM WikiPages"); err != nil {
			return nil, stats, err
		}
		for _, r := range rows {
			id := r.str("Id")
			add(`INSERT INTO wiki_pages
             (id, title, normalized_title, kind, summary, body, aliases_json,
              provider, model, prompt_version, generated_at, created_at, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, r.or("Title", ""), r.or("NormalizedTitle", ""), mapLegacyWikiKind(r["Kind"]), r.or("Summary", ""), r.or("Body", ""), r["AliasesJson"],
				r.or("Provider", ""), r.or("Model", ""), r.or("PromptVersion", ""),
				parseLegacyUtc(r["GeneratedAtUtc"]), parseLegacyUtc(r["CreatedAtUtc"]), parseLegacyUtc(r["UpdatedAtUtc"]))
			pageIDs[id] = true
			stats.WikiPages++
		}
		if rows, err = d.readAll(ctx, "SELECT * FROM WikiPageLinks"); err != nil {
			return nil, stats, err
		}
		for _, r := range rows {
			from, to := r.str("FromPageId"), r.str("ToPageId")
			if pageIDs[from] && pageIDs[to] {
				add("INSERT OR IGNORE INTO wiki_page_links (from_page_id, to_page_id, created_at) VALUES (?, ?, ?)", from, to, parseLegacyUtc(r["CreatedAtUtc"]))
				stats.WikiLinks++
			}
		}
		if rows, err = d.readAll(ctx, "SELECT * FROM WikiPageSources"); err != nil {
			return nil, stats, err
		}
		for _, r := range rows {
			pageID, itemID := r.str("WikiPageId"), r.str("KnowledgeItemId")
			if pageIDs[pageID] && itemIDs[itemID] {
				add("INSERT OR IGNORE INTO wiki_page_sources (page_id, item_id, created_at) VALUES (?, ?, ?)", pageID, itemID, parseLegacyUtc(r["CreatedAtUtc"]))
				stats.WikiSources++
			}
		}
		if rows, err = d.readAll(ctx, "SELECT * FROM WikiIngestions"); err != nil {
			return nil, stats, err
		}
		for _, r := range rows {
			itemID := r.str("KnowledgeItemId")
			if itemIDs[itemID] {
				add(`INSERT OR REPLACE INTO wiki_ingestions
               (item_id, content_hash, model, prompt_version, updated_at)
             VALUES (?, ?, ?, ?, ?)`, itemID, r.or("ContentHash", ""), r.or("Model", ""), r.or("PromptVersion", ""), parseLegacyUtc(r["UpdatedAtUtc"]))
				stats.WikiIngestions++
			}
		}
	}
	return stmts, stats, nil
}

func (l *Library) defaultLegacyDBPath() string {
	return filepath.Join(l.paths.UserData, "data", "guizhi.db")
}

func (l *Library) runLegacyMigration(ctx context.Context, legacyPath string) (*legacyMigrationStats, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return withLegacyCopy(ctx, legacyPath, func(d legacyDB) (*legacyMigrationStats, error) {
		if !d.hasTable(ctx, "KnowledgeItems") {
			return nil, fmt.Errorf("源数据库不是旧版归知的数据文件（缺少 KnowledgeItems 表）")
		}
		empty, err := legacyTargetEmpty(ctx, l.db)
		if err != nil {
			return nil, err
		}
		if !empty {
			return nil, fmt.Errorf("当前知识库不是空的，为防止数据覆盖已取消迁移")
		}
		stmts, stats, err := d.build(ctx)
		if err != nil {
			return nil, err
		}
		if len(stmts) > 0 {
			if _, err := l.db.Transaction(ctx, stmts); err != nil {
				return nil, err
			}
		}
		return &stats, nil
	})
}

func (l *Library) registerLegacyMigration(b *Bridge) {
	b.handlers[chMigrationDetectLegacy] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		legacyPath := l.defaultLegacyDBPath()
		if _, err := os.Stat(legacyPath); err != nil {
			return nil, nil
		}
		empty, err := legacyTargetEmpty(ctx, l.db)
		if err != nil {
			return nil, err
		}
		if !empty {
			return nil, nil
		}
		count, err := withLegacyCopy(ctx, legacyPath, func(d legacyDB) (int64, error) { return d.countItems(ctx) })
		if err != nil {
			log.Printf("旧版数据库探测失败: %v", err)
			return nil, nil
		}
		if count > 0 {
			return map[string]any{"path": legacyPath, "itemCount": count}, nil
		}
		return nil, nil
	}
	b.handlers[chMigrationRunLegacy] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		legacyPath := ""
		if len(a) > 0 {
			var v any
			if err := json.Unmarshal(a[0], &v); err == nil {
				if s, ok := v.(string); ok {
					legacyPath = s
				} else if jsTruthy(v) {
					return nil, fmt.Errorf("The \"path\" argument must be of type string")
				}
			}
		}
		if legacyPath == "" {
			legacyPath = l.defaultLegacyDBPath()
		}
		if _, err := os.Stat(legacyPath); err != nil {
			return nil, fmt.Errorf("找不到旧版数据库文件：%s", legacyPath)
		}
		stats, err := l.runLegacyMigration(ctx, legacyPath)
		if err != nil {
			return nil, err
		}
		if raw, err := json.Marshal(stats); err == nil {
			log.Printf("旧版数据迁移完成: %s", raw)
		}
		return stats, nil
	}
}
