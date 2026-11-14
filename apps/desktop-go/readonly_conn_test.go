package main

// 只读连接（Library.ro）的回归测试：慢读不挡主连接、读得到已提交写入、拒绝写入、回落与关闭。
// 慢查询用递归 CTE 造出确定的 CPU 耗时，不依赖夹具文件。

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// 约数百毫秒的纯 CPU 查询，用来占住一条连接。
const slowCTE = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 8000000) SELECT count(*) FROM c`

// openRoPair 建一个已初始化的库，返回读写连接与同文件的只读连接。
func openRoPair(t *testing.T) (ctx context.Context, rw, ro *sqlite.DB) {
	t.Helper()
	ctx = context.Background()
	path := filepath.Join(t.TempDir(), "knowledge.db")
	lease, err := initDatabaseFile(ctx, path, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
	rw, err = sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ro, err = sqlite.Open(ctx, path, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		rw.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close(); rw.Close() })
	return ctx, rw, ro
}

// lightFinishesBeforeSlow 在 slowOn 上起慢查询，随后在 lightOn 上跑一条轻查询，
// 返回轻查询结束时慢查询是否仍在运行（即轻查询没有被慢查询挡住）。
func lightFinishesBeforeSlow(t *testing.T, ctx context.Context, slowOn, lightOn *sqlite.DB) bool {
	t.Helper()
	var slowDone atomic.Bool
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if _, err := slowOn.Query(ctx, slowCTE); err != nil {
			t.Errorf("慢查询失败：%v", err)
		}
		slowDone.Store(true)
	}()
	time.Sleep(30 * time.Millisecond) // 让慢查询先拿到连接
	if _, err := lightOn.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("轻查询失败：%v", err)
	}
	stillRunning := !slowDone.Load()
	<-finished
	return stillRunning
}

func TestReadOnlyConnectionDoesNotQueueBehindSlowRead(t *testing.T) {
	ctx, rw, ro := openRoPair(t)

	// 对照组：同一连接上轻查询必须排在慢查询之后；否则这个测试证明不了任何事。
	if lightFinishesBeforeSlow(t, ctx, rw, rw) {
		t.Fatal("对照组失效：同一连接上的轻查询居然没有排队，慢查询可能太快，请加大 slowCTE")
	}
	// 实验组：慢查询在只读连接上，主连接上的轻查询不应排队。
	if !lightFinishesBeforeSlow(t, ctx, ro, rw) {
		t.Fatal("只读连接上的慢查询仍然挡住了主连接")
	}
}

// 回归：knowledgeGet 曾因 loadTagsFor 整体搬到只读连接而排在列表/计数的慢读后面。
// 轻量调用（get、写入后返回结果）必须只用主连接。
func TestKnowledgeGetDoesNotQueueBehindSlowReadOnLibrary(t *testing.T) {
	ctx, rw, ro := openRoPair(t)
	if _, err := rw.Transaction(ctx, []sqlite.Statement{{
		SQL:  "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, 1, 1)",
		Args: []any{"get-1", "t", "c"},
	}}); err != nil {
		t.Fatal(err)
	}
	l := &Library{db: rw, ro: ro}

	var slowDone atomic.Bool
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if _, err := ro.Query(ctx, slowCTE); err != nil {
			t.Errorf("慢查询失败：%v", err)
		}
		slowDone.Store(true)
	}()
	time.Sleep(30 * time.Millisecond) // 让慢查询先占住只读连接
	got, err := l.knowledgeGet(ctx, "get-1")
	if err != nil || got == nil {
		t.Fatalf("knowledgeGet 失败：got=%v err=%v", got, err)
	}
	if slowDone.Load() {
		t.Fatal("knowledgeGet 返回时慢查询已结束：它被排在只读连接的慢读之后，或慢查询太快无法证明")
	}
	<-finished
}

func TestReadOnlyConnectionSeesCommittedWritesAndRejectsWrites(t *testing.T) {
	ctx, rw, ro := openRoPair(t)
	if _, err := rw.Transaction(ctx, []sqlite.Statement{{
		SQL:  "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, 1, 1)",
		Args: []any{"ro-1", "t", "c"},
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := ro.Query(ctx, "SELECT title FROM knowledge_items WHERE id = ?", "ro-1")
	if err != nil || len(rows.Values) != 1 {
		t.Fatalf("只读连接应读到主连接已提交的写入：rows=%v err=%v", rows.Values, err)
	}
	// 只读连接上的写入必须失败：这是"有人误把写语句放进 rdb()"时的安全网。
	if _, err := ro.Execute(ctx, "DELETE FROM knowledge_items WHERE id = ?", "ro-1"); err == nil {
		t.Fatal("只读连接不应允许写入")
	}
	rows, err = rw.Query(ctx, "SELECT count(*) FROM knowledge_items WHERE id = ?", "ro-1")
	if err != nil || asInt(rows.Values[0][0]) != 1 {
		t.Fatalf("被拒绝的写入不应改变数据：rows=%v err=%v", rows.Values, err)
	}
}

func TestLibraryRdbFallbackAndCloseDatabases(t *testing.T) {
	ctx, rw, ro := openRoPair(t)

	noRO := &Library{db: rw}
	if noRO.rdb() != rw {
		t.Fatal("没有只读连接时 rdb() 应回落到主连接")
	}
	withRO := &Library{db: rw, ro: ro}
	if withRO.rdb() != ro {
		t.Fatal("有只读连接时 rdb() 应返回只读连接")
	}

	if err := withRO.closeDatabases(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if _, err := ro.Query(ctx, "SELECT 1"); err == nil {
		t.Fatal("closeDatabases 之后只读连接应已关闭")
	}
	if _, err := rw.Query(ctx, "SELECT 1"); err == nil {
		t.Fatal("closeDatabases 之后主连接应已关闭")
	}
	// 没有只读连接的 Library（MCP 与旧单测）同样能正常关闭。
	_, rw2, ro2 := openRoPair(t)
	ro2.Close()
	if err := (&Library{db: rw2}).closeDatabases(); err != nil {
		t.Fatalf("无只读连接时关闭失败：%v", err)
	}
}
