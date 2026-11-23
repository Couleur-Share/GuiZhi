package main

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"

	"guizhi-mygo-shell/native/services"
)

// 旧版（.NET）库的最小表结构：只含迁移读取的列。
var nativeLegacySchema = []string{
	`CREATE TABLE Collections (Id TEXT PRIMARY KEY, Name TEXT, Icon TEXT, SortOrder INTEGER, IsDeleted INTEGER, CreatedAtUtc TEXT, UpdatedAtUtc TEXT)`,
	`CREATE TABLE Tags (Id TEXT PRIMARY KEY, Name TEXT, ColorKey INTEGER, CreatedAtUtc TEXT, UpdatedAtUtc TEXT)`,
	`CREATE TABLE KnowledgeItems (Id TEXT PRIMARY KEY, Title TEXT, Content TEXT, Summary TEXT, Transcript TEXT, ItemType INTEGER, Status INTEGER,
		CollectionId TEXT, IsFavorite INTEGER, IsPinned INTEGER, IsDeleted INTEGER, DeletedAtUtc TEXT, SourceId TEXT, CreatedAtUtc TEXT, UpdatedAtUtc TEXT)`,
	`CREATE TABLE KnowledgeItemTags (KnowledgeItemId TEXT, TagId TEXT)`,
	`CREATE TABLE SourceRecords (Id TEXT PRIMARY KEY, SourceType INTEGER, OriginalUri TEXT, CanonicalUri TEXT, ContentHash TEXT, Platform TEXT, CapturedAtUtc TEXT)`,
}

var nativeLegacyData = []string{
	`INSERT INTO Collections VALUES ('c1', '旧合集', NULL, 0, 0, '2025-01-02T03:04:05', '2025-01-02T03:04:05')`,
	`INSERT INTO Tags VALUES ('t1', '旧标签', 1, '2025-01-02T03:04:05', '2025-01-02T03:04:05')`,
	`INSERT INTO SourceRecords VALUES ('s1', 3, 'https://example.com/a', 'https://example.com/a', 'h1', 'web', '2025-01-02T03:04:05')`,
	`INSERT INTO KnowledgeItems VALUES ('i1', '旧条目一', '正文一', NULL, NULL, 0, 1, 'c1', 1, 0, 0, NULL, 's1', '2025-01-02T03:04:05', '2025-01-02T03:04:05')`,
	`INSERT INTO KnowledgeItems VALUES ('i2', '旧条目二', '正文二', NULL, NULL, 0, 1, NULL, 0, 1, 0, NULL, NULL, '2025-01-02T03:04:05', '2025-01-02T03:04:05')`,
	`INSERT INTO KnowledgeItems VALUES ('i3', '已删除条目', '正文三', NULL, NULL, 0, 1, NULL, 0, 0, 1, '2025-02-01T00:00:00', NULL, '2025-01-02T03:04:05', '2025-01-02T03:04:05')`,
	`INSERT INTO KnowledgeItemTags VALUES ('i1', 't1')`,
}

// newMigrationFixture 在真实的临时库上登记旧库迁移处理器，返回接到它们的适配器、当前库与默认旧库路径。
func newMigrationFixture(t *testing.T) (services.Migration, *sqlite.DB, string) {
	t.Helper()
	_, paths, db := openAdapterTestDB(t)
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	l := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths}
	l.registerLegacyMigration(b)
	return migrationAdapter{b: b}, db, l.defaultLegacyDBPath()
}

func fileDigest(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

// 探测与迁移经真实处理器与真实 SQLite 往返：没有旧库是 found=false 而不是错误；
// 探测数的是未删除条目；迁移写入全部记录（含回收站条目）、统计与处理器一致、旧库文件字节不变；
// 迁移后当前库非空，不再探测到旧库，再迁移被拒绝（防覆盖）。
func TestMigrationAdapterDetectAndRunAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, db, legacy := newMigrationFixture(t)

	if _, found, err := svc.DetectLegacy(ctx); found || err != nil {
		t.Fatalf("没有旧库：%v %v", found, err)
	}

	writeLegacyDB(t, legacy, nativeLegacySchema, nil, nativeLegacyData, false, nil)
	before := fileDigest(t, legacy)

	det, found, err := svc.DetectLegacy(ctx)
	if err != nil || !found || det.Path != legacy || det.ItemCount != 2 {
		t.Fatalf("探测：%+v found=%v err=%v", det, found, err)
	}

	stats, err := svc.RunLegacy(ctx, "")
	if err != nil {
		t.Fatalf("迁移：%v", err)
	}
	want := services.LegacyMigrationStats{Collections: 1, Tags: 1, Items: 3, ItemTags: 1, Sources: 1}
	if stats != want {
		t.Fatalf("统计：%+v，应为 %+v", stats, want)
	}
	rows, err := db.Query(ctx, "SELECT id, title, deleted_at IS NOT NULL FROM knowledge_items ORDER BY id")
	if err != nil || len(rows.Values) != 3 {
		t.Fatalf("写入的条目：%v %v", rows, err)
	}
	if asInt(rows.Values[0][2]) != 0 || asInt(rows.Values[2][2]) != 1 || rows.Values[2][1] != "已删除条目" {
		t.Fatalf("已删除条目应进回收站：%v", rows.Values)
	}
	if fileDigest(t, legacy) != before {
		t.Fatal("迁移不应改动旧库文件")
	}

	if _, found, err := svc.DetectLegacy(ctx); found || err != nil {
		t.Fatalf("迁移后当前库非空，不应再探测到旧库：%v %v", found, err)
	}
	if _, err := svc.RunLegacy(ctx, ""); err == nil || !strings.Contains(err.Error(), "不是空的") {
		t.Fatalf("再次迁移应因当前库非空被拒绝：%v", err)
	}
}

// 迁移失败带回原因，并且不改动当前库：指定了不存在的文件、文件不是旧版数据库、旧库留有未合并的写入日志。
func TestMigrationAdapterRunFailuresAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, db, legacy := newMigrationFixture(t)
	dir := filepath.Dir(legacy)
	assertTargetEmpty := func() {
		t.Helper()
		rows, err := db.Query(ctx, "SELECT COUNT(*) FROM knowledge_items")
		if err != nil || asInt(rows.Values[0][0]) != 0 {
			t.Fatalf("失败的迁移不应写入任何记录：%v %v", rows, err)
		}
	}

	if _, err := svc.RunLegacy(ctx, filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("文件不存在应报错")
	}

	other := filepath.Join(dir, "other.db")
	writeLegacyDB(t, other, []string{`CREATE TABLE Other (x)`}, nil, nil, false, nil)
	if _, err := svc.RunLegacy(ctx, other); err == nil || !strings.Contains(err.Error(), "KnowledgeItems") {
		t.Fatalf("不是旧版数据库：%v", err)
	}

	writeLegacyDB(t, legacy, nativeLegacySchema, nil, nativeLegacyData, false, nil)
	if err := os.WriteFile(legacy+"-wal", []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunLegacy(ctx, ""); err == nil || !strings.Contains(err.Error(), "写入日志") {
		t.Fatalf("残留日志应拒绝迁移：%v", err)
	}
	// 探测对读不了的旧库不报错（处理器把它当作没有）。
	if _, found, err := svc.DetectLegacy(ctx); found || err != nil {
		t.Fatalf("探测：found=%v err=%v", found, err)
	}
	assertTargetEmpty()
}

// 显式指定路径迁移：用传入的文件而不是默认旧库。
func TestMigrationAdapterRunExplicitPath(t *testing.T) {
	ctx := t.Context()
	svc, db, legacy := newMigrationFixture(t)
	explicit := filepath.Join(filepath.Dir(legacy), "chosen.db")
	writeLegacyDB(t, explicit, nativeLegacySchema, nil, nativeLegacyData[:5], false, nil) // 只有集合、标签、来源与前两条目
	stats, err := svc.RunLegacy(ctx, explicit)
	if err != nil || stats.Items != 2 {
		t.Fatalf("显式路径迁移：%+v %v", stats, err)
	}
	rows, err := db.Query(ctx, "SELECT COUNT(*) FROM knowledge_items")
	if err != nil || asInt(rows.Values[0][0]) != 2 {
		t.Fatalf("写入的条目数：%v %v", rows, err)
	}
}
