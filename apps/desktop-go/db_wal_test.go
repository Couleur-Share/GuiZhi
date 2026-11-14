package main

// WAL 模式（db_wal.go）的回归测试：切换与持久、旧库转换、不支持时回落、
// 「库打开时单拷主文件会丢数据」的对照组、两处受影响的拷贝点、合法 WAL 判定。

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func journalModeOf(t *testing.T, path string) string {
	t.Helper()
	db, err := openReadWrite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var m string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func setJournalMode(t *testing.T, path, mode string) {
	t.Helper()
	db, err := openReadWrite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRow("PRAGMA journal_mode=" + mode).Scan(&got); err != nil {
		t.Fatal(err)
	}
}

func titleOf(t *testing.T, path, id string) string {
	t.Helper()
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatalf("只读打开 %s 失败：%v", filepath.Base(path), err)
	}
	defer db.Close()
	var s string
	if err := db.QueryRow("SELECT title FROM knowledge_items WHERE id = ?", id).Scan(&s); err != nil {
		t.Fatalf("读取 %s 失败：%v", filepath.Base(path), err)
	}
	return s
}

// walWithUncheckpointedWrite 建一个 WAL 库，并用插件读写连接做一次写入（写入留在 -wal 里，连接保持打开）。
func walWithUncheckpointedWrite(t *testing.T) (path string, rw *sqlite.DB) {
	t.Helper()
	path = newIntegrityFixtureDBRows(t, 300)
	if got := journalModeOf(t, path); got != "wal" {
		t.Fatalf("夹具应是 WAL 库，实际 %s", got)
	}
	rw, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rw.Close() })
	if _, err := rw.Execute(context.Background(), "UPDATE knowledge_items SET title='NEW' WHERE id='i-000001'"); err != nil {
		t.Fatal(err)
	}
	if !hasLiveWAL(path) {
		t.Fatal("写入后 -wal 应含未合并内容")
	}
	return path, rw
}

// crashImage 复制「库打开中」的现场（主文件 + -wal）到新目录，模拟崩溃后留下的文件，且不再有任何连接。
func crashImage(t *testing.T, path string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "knowledge.db")
	if err := copyDatabaseFiles(path, dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestInitSwitchesToWALAndPersists(t *testing.T) {
	path := newIntegrityFixtureDBRows(t, 10)
	if got := journalModeOf(t, path); got != "wal" {
		t.Fatalf("初始化后应是 WAL，实际 %s", got)
	}
}

func TestInitConvertsExistingRollbackJournalDatabase(t *testing.T) {
	ctx := context.Background()
	path := newIntegrityFixtureDBRows(t, 10)
	setJournalMode(t, path, "DELETE")
	if got := journalModeOf(t, path); got != "delete" {
		t.Fatalf("前置：应已降为回滚日志，实际 %s", got)
	}
	lease, err := initDatabaseFile(ctx, path, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
	if got := journalModeOf(t, path); got != "wal" {
		t.Fatalf("旧库（或从旧备份恢复出的库）下次启动应被转成 WAL，实际 %s", got)
	}
}

func TestEnsureWALModeReportsUnsupportedWithoutError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mode, err := ensureWALMode(context.Background(), conn)
	if err != nil {
		t.Fatalf("不支持 WAL 不应是错误（调用方据此回落）：%v", err)
	}
	if mode == "wal" {
		t.Fatal("内存库不可能是 WAL，测试前提不成立")
	}
}

// 对照组：这是 WAL 的核心陷阱——库打开时只拷主文件，最近的写入会丢。
// 这个测试失败意味着陷阱不存在了（比如换了日志模式），那 db_wal.go 的约束可以重新评估。
func TestCopyingOnlyMainFileLosesWritesInWAL(t *testing.T) {
	path, _ := walWithUncheckpointedWrite(t)
	only := filepath.Join(t.TempDir(), "only.db")
	if err := copyFile(path, only); err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, only, "i-000001"); got == "NEW" {
		t.Fatal("只拷主文件居然带上了 -wal 里的写入：WAL 前提不成立，请重新评估 db_wal.go 的约束")
	}
}

func TestCopyDatabaseFilesKeepsUncheckpointedWrites(t *testing.T) {
	path, _ := walWithUncheckpointedWrite(t)
	dst := filepath.Join(t.TempDir(), "snap.db")
	if err := copyDatabaseFiles(path, dst); err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, dst, "i-000001"); got != "NEW" {
		t.Fatalf("主文件+-wal 一起保存后应读到最新写入，实际 %q", got)
	}
}

// createIntegrityBackup 是启动时修复前保存现场的入口，必须保住 -wal 里的内容。
func TestIntegrityBackupKeepsWALContent(t *testing.T) {
	path, _ := walWithUncheckpointedWrite(t)
	p, err := createIntegrityBackup(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, p, "i-000001"); got != "NEW" {
		t.Fatalf("完整性备份应含 -wal 里的写入，实际 %q", got)
	}
}

func TestCheckpointLiveWALMergesIntoMainFile(t *testing.T) {
	path, _ := walWithUncheckpointedWrite(t)
	img := crashImage(t, path)
	if !hasLiveWAL(img) {
		t.Fatal("崩溃现场应带 -wal")
	}
	if err := checkpointLiveWAL(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	if hasLiveWAL(img) {
		t.Fatal("合并后不应再有未合并的 WAL")
	}
	// 只拷主文件（换库、pre-restore 的做法）现在是安全的
	only := filepath.Join(t.TempDir(), "only.db")
	if err := copyFile(img, only); err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, only, "i-000001"); got != "NEW" {
		t.Fatalf("合并后主文件应含最新写入，实际 %q", got)
	}
}

// 伪造的旁车文件（黄金对拍里的 "side"）和没有 -wal 的库不能被 WAL 处理误伤。
func TestCheckpointLiveWALIgnoresJunkAndMissingSidecars(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "knowledge.db")
	os.WriteFile(db, []byte("OLD"), 0o644) // 不是 SQLite 文件
	if err := checkpointLiveWAL(context.Background(), db); err != nil {
		t.Fatalf("没有旁车文件应是空操作：%v", err)
	}
	os.WriteFile(db+"-wal", []byte("side"), 0o644)
	os.WriteFile(db+"-shm", []byte("side"), 0o644)
	if err := checkpointLiveWAL(context.Background(), db); err != nil {
		t.Fatalf("乱码旁车文件应被忽略：%v", err)
	}
	if b, _ := os.ReadFile(db + "-wal"); string(b) != "side" {
		t.Fatal("乱码旁车文件不应被改动，清理由调用方原有逻辑负责")
	}
}

func TestHasLiveWALRequiresValidHeader(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "x.db")
	if hasLiveWAL(db) {
		t.Fatal("没有文件不是 WAL")
	}
	os.WriteFile(db+"-wal", nil, 0o644)
	if hasLiveWAL(db) {
		t.Fatal("空文件不是 WAL")
	}
	os.WriteFile(db+"-wal", make([]byte, 64), 0o644)
	if hasLiveWAL(db) {
		t.Fatal("全零内容不是 WAL")
	}
	hdr := make([]byte, 64)
	copy(hdr, []byte{0x37, 0x7f, 0x06, 0x82})
	os.WriteFile(db+"-wal", hdr, 0o644)
	if !hasLiveWAL(db) {
		t.Fatal("魔数 0x377f0682 应识别为 WAL")
	}
	hdr[3] = 0x83
	os.WriteFile(db+"-wal", hdr, 0o644)
	if !hasLiveWAL(db) {
		t.Fatal("魔数 0x377f0683 应识别为 WAL")
	}
}

// 本地恢复：崩溃留下 -wal 时，pre-restore 快照必须含最新写入，换库后不留旁车文件。
func TestRestoreSwapPreRestoreKeepsWALContentAndClearsSidecars(t *testing.T) {
	path, rw := walWithUncheckpointedWrite(t)
	live := crashImage(t, path) // 模拟崩溃后的现场
	rw.Close()

	backupSrc := newIntegrityFixtureDBRows(t, 50) // 作为要恢复的备份：里面没有 NEW
	dir := t.TempDir()
	lb := &localBackups{dbPath: live, backupsDir: filepath.Join(dir, "backups")}
	pre, err := lb.performRestoreSwap(backupSrc)
	if err != nil {
		t.Fatal(err)
	}
	if pre == "" {
		t.Fatal("应生成 pre-restore 快照")
	}
	// 先查旁车文件：之后的只读打开本身会重新生成 -wal/-shm
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(live + s); err == nil {
			t.Fatalf("换库后不应残留 %s", s)
		}
	}
	if got := titleOf(t, filepath.Join(lb.backupsDir, pre), "i-000001"); got != "NEW" {
		t.Fatalf("pre-restore 快照应含崩溃现场 -wal 里的写入，实际 %q（快照丢数据）", got)
	}
	if got := titleOf(t, live, "i-000001"); got == "NEW" {
		t.Fatal("换库后主库应是备份内容")
	}
}
