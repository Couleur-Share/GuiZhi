package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// seedIndexedItems 写入 n 条「条目 + 全文索引行 + 映射」齐全的数据（与生产写入同形），内容较短以便单测快速。
func seedIndexedItems(t *testing.T, ctx context.Context, db *sqlite.DB, n int) {
	t.Helper()
	const batch = 500
	for start := 0; start < n; start += batch {
		var stmts []sqlite.Statement
		for i := start; i < start+batch && i < n; i++ {
			id := fmt.Sprintf("s-%07d", i)
			title, content := fmt.Sprintf("样本标题 %d", i), fmt.Sprintf("样本内容 hello world %d 知识库 检索 备份", i)
			stmts = append(stmts,
				sqlite.Statement{SQL: "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, 1, 1)", Args: []any{id, title, content}},
				sqlite.Statement{SQL: "INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, '')", Args: []any{id, SegmentTextForFts(title), SegmentTextForFts(content)}},
				sqlite.Statement{SQL: "INSERT OR REPLACE INTO knowledge_fts_map (item_id, fts_rowid) VALUES (?, last_insert_rowid())", Args: []any{id}},
			)
		}
		if _, err := db.Transaction(ctx, stmts); err != nil {
			t.Fatal(err)
		}
	}
}

func openScaleLibrary(t *testing.T, ctx context.Context) (*Library, *sqlite.DB) {
	t.Helper()
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
	t.Cleanup(func() { db.Close() })
	return &Library{db: db}, db
}

// 回归：库里每条条目都已有全文索引行时，启动补齐必须近似线性，而不是对每个条目全表扫一遍虚表（O(n²)）。
// 真实现象：3 万条 / 750 MB 的库启动 10 分钟仍卡在窗口出现之前（TS 版是同一条 SQL）。
func TestBackfillMissingFtsScalesLinearly(t *testing.T) {
	ctx := context.Background()
	l, db := openScaleLibrary(t, ctx)
	seedIndexedItems(t, ctx, db, 12000)
	start := time.Now()
	n, err := l.backfillMissingKnowledgeFts(ctx)
	elapsed := time.Since(start)
	t.Logf("12000 条全部已索引时，补齐耗时 %v", elapsed)
	if err != nil || n != 0 {
		t.Fatalf("不应补写任何行：n=%d err=%v", n, err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("补齐耗时 %v，疑似对每个条目全表扫描虚表（O(n²)）", elapsed)
	}
}
