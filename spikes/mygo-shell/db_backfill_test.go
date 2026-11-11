package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

// 启动补齐全文索引：只补缺失的行，已有的不动；回收站条目也要补。
func TestBackfillMissingFts(t *testing.T) {
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "knowledge.db")
	lease, err := initDatabaseFile(ctx, work, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
	db, err := sqlite.Open(ctx, work, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := &Library{db: db}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Execute(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO tags (id, name, created_at, updated_at) VALUES ('t1', '标签甲', 1, 1)")
	for _, id := range []string{"a", "b", "c"} {
		exec("INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, 1, 1)", id, "标题"+id, "内容 hello "+id)
	}
	exec("UPDATE knowledge_items SET deleted_at = 5 WHERE id = 'c'") // 回收站条目
	exec("INSERT INTO knowledge_item_tags (item_id, tag_id) VALUES ('a', 't1')")
	// b 已有索引行（旧映射缺失），a、c 缺失
	exec("INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES ('b', ?, ?, '')", SegmentTextForFts("标题b"), SegmentTextForFts("内容 hello b"))
	exec("INSERT INTO wiki_pages (id, title, normalized_title, kind, summary, body, generated_at, created_at, updated_at) VALUES ('w1', '页面', '页面', 'concept', '摘要', '正文内容', 1, 1, 1)")

	n, err := l.backfillMissingKnowledgeFts(ctx)
	if err != nil || n != 2 {
		t.Fatalf("应补 2 条（a 与回收站的 c），实际 %d，err=%v", n, err)
	}
	rows, _ := db.Query(ctx, "SELECT item_id, tags FROM knowledge_fts ORDER BY item_id")
	if len(rows.Values) != 3 {
		t.Fatalf("补齐后应有 3 行索引，实际 %d", len(rows.Values))
	}
	if got := rows.Values[0][1]; got != SegmentTextForFts("标签甲") {
		t.Fatalf("标签应写进 tags 列：%v", got)
	}
	m, _ := db.Query(ctx, "SELECT COUNT(*) FROM knowledge_fts_map")
	if asInt(m.Values[0][0]) != 3 {
		t.Fatalf("映射表应登记 3 条，实际 %v", m.Values[0][0])
	}
	if n, err := l.backfillMissingKnowledgeFts(ctx); err != nil || n != 0 {
		t.Fatalf("再次执行应无事可做：%d %v", n, err)
	}
	w, err := l.backfillMissingWikiFts(ctx)
	if err != nil || w != 1 {
		t.Fatalf("应补 1 个 Wiki 页面，实际 %d err=%v", w, err)
	}
	if w, _ := l.backfillMissingWikiFts(ctx); w != 0 {
		t.Fatal("Wiki 再次执行应无事可做")
	}
	_ = os.Remove(work)
}
