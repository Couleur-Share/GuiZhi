package main

// 后台完整性复检（db_integrity_bg.go）的测试：通过 / 检出损坏 / 取消不留结论 / 标记行为 / 表顺序 / 与换库联动。

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// resetIntegrityState 清零包级状态并在测试结束后还原，避免用例之间互相影响。
func resetIntegrityState(t *testing.T) {
	t.Helper()
	prev := atomic.LoadInt64(&lastIntegrityAtMs)
	prevDue, prevProblem := integrityRecheckDue.Load(), integrityProblemFound.Load()
	atomic.StoreInt64(&lastIntegrityAtMs, 0)
	integrityRecheckDue.Store(false)
	integrityProblemFound.Store(false)
	t.Cleanup(func() {
		atomic.StoreInt64(&lastIntegrityAtMs, prev)
		integrityRecheckDue.Store(prevDue)
		integrityProblemFound.Store(prevProblem)
	})
}

// newIntegrityFixtureDB 建一个带数据的库（几百页），用于检出损坏的特征测试。
func newIntegrityFixtureDB(t *testing.T) string {
	return newIntegrityFixtureDBRows(t, 3000)
}

func newIntegrityFixtureDBRows(t *testing.T, rows int) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "knowledge.db")
	lease, err := initDatabaseFile(ctx, path, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
	db, err := openReadWrite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rng := rand.New(rand.NewSource(7))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		var b strings.Builder
		for b.Len() < 1500 {
			b.WriteByte(byte('a' + rng.Intn(26)))
		}
		if _, err := tx.Exec("INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, 1, 1)",
			fmt.Sprintf("i-%06d", i), fmt.Sprintf("t%d", i), b.String()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return path
}

// openNativeReadOnlyForTest 用插件的原生 SQLite 打开只读连接（与后台复检同一种连接）。
func openNativeReadOnlyForTest(t *testing.T, path string) (*sqlite.DB, error) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{ReadOnly: true})
	if err == nil {
		t.Cleanup(func() { db.Close() })
	}
	return db, err
}

// corruptMiddlePage 用确定性的随机字节覆盖文件中间的一页（保持文件头完好，库仍能打开）。
func corruptMiddlePage(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	const page = 4096
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, page)
	rand.New(rand.NewSource(99)).Read(buf)
	if _, err := f.WriteAt(buf, (st.Size()/page/2)*page); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundIntegrityCheckPassesHealthyDatabase(t *testing.T) {
	resetIntegrityState(t)
	path := newIntegrityFixtureDB(t)
	now := time.Now()
	runBackgroundIntegrityCheck(context.Background(), path, func() time.Time { return now })
	if integrityProblemFound.Load() {
		t.Fatal("健康的库不应报告问题")
	}
	if got := atomic.LoadInt64(&lastIntegrityAtMs); got != now.UnixMilli() {
		t.Fatalf("通过后应更新校验时刻为 %d，实际 %d", now.UnixMilli(), got)
	}
}

func TestBackgroundIntegrityCheckDetectsCorruption(t *testing.T) {
	resetIntegrityState(t)
	path := newIntegrityFixtureDB(t)
	corruptMiddlePage(t, path)
	runBackgroundIntegrityCheck(context.Background(), path, time.Now)
	if !integrityProblemFound.Load() {
		t.Fatal("页被覆盖后应发现问题")
	}
	if got := atomic.LoadInt64(&lastIntegrityAtMs); got != 0 {
		t.Fatalf("发现问题时不应更新校验时刻，实际 %d", got)
	}
}

func TestBackgroundIntegrityCheckCancelledLeavesNoConclusion(t *testing.T) {
	resetIntegrityState(t)
	path := newIntegrityFixtureDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runBackgroundIntegrityCheck(ctx, path, time.Now)
	if integrityProblemFound.Load() || atomic.LoadInt64(&lastIntegrityAtMs) != 0 {
		t.Fatal("被取消的复检不应留下任何结论（既不是通过，也不是问题）")
	}
}

func TestBackgroundIntegrityStopIsPromptAndNilSafe(t *testing.T) {
	resetIntegrityState(t)
	(*integrityBackground)(nil).Stop() // nil 安全

	path := newIntegrityFixtureDB(t)
	bg := startBackgroundIntegrityCheck(context.Background(), path, time.Hour, time.Now)
	done := make(chan struct{})
	go func() { bg.Stop(); bg.Stop(); close(done) }() // 可重复调用
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 应在延迟等待期间立即返回")
	}
	if atomic.LoadInt64(&lastIntegrityAtMs) != 0 {
		t.Fatal("还没开始的复检被停止后不应有结论")
	}
}

func TestBackgroundIntegrityCheckRunsAfterDelay(t *testing.T) {
	resetIntegrityState(t)
	path := newIntegrityFixtureDB(t)
	bg := startBackgroundIntegrityCheck(context.Background(), path, 10*time.Millisecond, time.Now)
	defer bg.Stop()
	deadline := time.Now().Add(20 * time.Second)
	for atomic.LoadInt64(&lastIntegrityAtMs) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("延迟到期后应完成复检并更新校验时刻")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCleanShutdownMarkerSkippedWhenBackgroundCheckFoundProblem(t *testing.T) {
	resetIntegrityState(t)
	db := filepath.Join(t.TempDir(), "knowledge.db")
	atomic.StoreInt64(&lastIntegrityAtMs, 12345)

	writeCleanShutdownMarker(db)
	raw, err := os.ReadFile(db + ".clean")
	if err != nil || string(raw) != "12345" {
		t.Fatalf("没有问题时应写入标记：%q %v", raw, err)
	}
	os.Remove(db + ".clean")

	integrityProblemFound.Store(true)
	writeCleanShutdownMarker(db)
	if _, err := os.Stat(db + ".clean"); err == nil {
		t.Fatal("后台复检发现问题时不应写干净关闭标记（让下次启动做完整校验）")
	}
}

func TestIntegrityTablesPutFullTextLast(t *testing.T) {
	path := newIntegrityFixtureDB(t)
	db, err := openNativeReadOnlyForTest(t, path)
	if err != nil {
		t.Fatal(err)
	}
	names, err := integrityTablesInOrder(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	lastBusiness, firstFts := -1, len(names)
	for i, n := range names {
		if strings.HasPrefix(n, "knowledge_fts") || strings.HasPrefix(n, "wiki_fts") {
			if i < firstFts {
				firstFts = i
			}
		} else {
			lastBusiness = i
		}
	}
	if firstFts == len(names) {
		t.Fatalf("没找到全文索引表，夹具与预期不符：%v", names)
	}
	if lastBusiness > firstFts {
		t.Fatalf("全文索引表应排在业务表之后：%v", names)
	}
	found := false
	for _, n := range names {
		if n == "knowledge_items" {
			found = true
		}
	}
	if !found {
		t.Fatalf("业务表 knowledge_items 应在清单里：%v", names)
	}
}

func TestCloseDatabasesStopsBackgroundIntegrityCheckFirst(t *testing.T) {
	ctx, rw, ro := openRoPair(t)
	var order []string
	l := &Library{db: rw, ro: ro, integrityStop: func() {
		// 此刻连接必须还开着：后台复检要先停，才能安全关闭数据库
		if _, err := rw.Query(ctx, "SELECT 1"); err != nil {
			t.Errorf("integrityStop 调用时主连接应仍可用：%v", err)
		}
		order = append(order, "stop")
	}}
	if err := l.closeDatabases(); err != nil {
		t.Fatal(err)
	}
	if len(order) != 1 {
		t.Fatalf("integrityStop 应恰好被调用一次，实际 %v", order)
	}
	if _, err := rw.Query(ctx, "SELECT 1"); err == nil {
		t.Fatal("closeDatabases 之后主连接应已关闭")
	}
}

// TestWritesDuringBackgroundIntegrityCheck 度量并守住「后台复检期间写入不失败」：
// 夹具默认是 WAL 库（读写互不阻塞）；若回落到回滚日志模式，读连接持 SHARED 锁、写提交要等它放开，靠 dbWriteBusyTimeout 兜底。
// 规模可用 GUIZHI_BG_ROWS 调大（默认 20000 行 ≈ 35 MB）。
func TestWritesDuringBackgroundIntegrityCheck(t *testing.T) {
	resetIntegrityState(t)
	rows := 20000
	if v := os.Getenv("GUIZHI_BG_ROWS"); v != "" {
		fmt.Sscanf(v, "%d", &rows)
	}
	path := newIntegrityFixtureDBRows(t, rows)
	ctx := context.Background()
	rw, err := sqlite.Open(ctx, path, sqlite.OpenOptions{BusyTimeout: dbWriteBusyTimeout})
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	writeLoop := func(stop <-chan struct{}) (n int, maxLat time.Duration, errs []error) {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			start := time.Now()
			_, err := rw.Execute(ctx, "UPDATE knowledge_items SET title = ? WHERE id = ?",
				fmt.Sprintf("w%d", i), fmt.Sprintf("i-%06d", i%rows))
			if lat := time.Since(start); lat > maxLat {
				maxLat = lat
			}
			if err != nil {
				errs = append(errs, err)
			}
			n++
			time.Sleep(5 * time.Millisecond)
		}
	}

	// 基线：无复检
	stop := make(chan struct{})
	time.AfterFunc(time.Second, func() { close(stop) })
	n0, max0, errs0 := writeLoop(stop)

	// 复检期间
	stop = make(chan struct{})
	checkDur := make(chan time.Duration, 1)
	go func() {
		s := time.Now()
		runBackgroundIntegrityCheck(ctx, path, time.Now)
		checkDur <- time.Since(s)
		close(stop)
	}()
	n1, max1, errs1 := writeLoop(stop)
	dur := <-checkDur

	t.Logf("复检 %v；基线 %d 次写 max=%v；复检期间 %d 次写 max=%v", dur, n0, max0, n1, max1)
	if len(errs0) > 0 || len(errs1) > 0 {
		t.Fatalf("写入出现错误：基线 %v；复检期间 %v", errs0, errs1)
	}
	if integrityProblemFound.Load() {
		t.Fatal("复检期间有写入，不应误报损坏")
	}
	if atomic.LoadInt64(&lastIntegrityAtMs) == 0 {
		t.Fatal("复检应在写入并发下仍然完成")
	}
}
