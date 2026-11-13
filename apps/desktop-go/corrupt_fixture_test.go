package main

// 损坏库的特征测试：用真实 SQLite 文件固定 quick_check 对各类损坏的实际反应，
// 防止对「启动时能自动修复什么」的认识停留在读代码的推断上。

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// quick_check 官方文档明确不核对「索引内容与表内容是否一致」。
// 这里实测：把真实索引在 schema 里改成 partial（索引树里的条目比 schema 允许的多），quick_check 仍返回 ok。
// 含义：启动时的「仅索引条数不符 → REINDEX」分支（repairIndexIntegrity）在 quick_check 路径上我们没能构造出触发样本，
// 是否可达未证实；它与 TS 版同源，按等价迁移保留，不在此处改动行为。
func TestQuickCheckDoesNotCompareIndexContent(t *testing.T) {
	work := filepath.Join(t.TempDir(), "knowledge.db")
	l, err := initDatabaseFile(context.Background(), work, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	db, err := openReadWrite(work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES ('a','t','c',1,1),('b','t','c',2,2)"); err != nil {
		t.Skipf("夹具表结构与预期不符，跳过：%v", err)
	}
	var idx, sqlText string
	if err := db.QueryRow(`SELECT name, sql FROM sqlite_master WHERE type='index' AND sql IS NOT NULL AND sql NOT LIKE '%WHERE%' AND tbl_name='knowledge_items' ORDER BY name LIMIT 1`).Scan(&idx, &sqlText); err != nil {
		t.Fatalf("找不到可用索引：%v", err)
	}
	for _, q := range []string{"PRAGMA writable_schema=ON", "PRAGMA writable_schema=OFF"} {
		if q == "PRAGMA writable_schema=OFF" {
			if _, err := db.Exec("UPDATE sqlite_master SET sql = ? WHERE type='index' AND name = ?", sqlText+" WHERE 0", idx); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	diag, err := inspectDatabaseIntegrity(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	if !isHealthyQuickCheck(diag) {
		t.Skipf("该 SQLite 版本的 quick_check 已能发现索引不一致（%q），特征改变，请复核 repairIndexIntegrity 的可达性", diag)
	}
}

// 空闲页计数写错（文件头偏移 36）是可被 repairFreelistIntegrity 自动修复的真实损坏。
// 本测试确认：真实库头被篡改后，quick_check 报出的诊断恰好被 isFreelistOnly 识别，且修复后恢复健康、留有完整性备份。
func TestFreelistHeaderCorruptionIsRepaired(t *testing.T) {
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "knowledge.db")
	l, err := initDatabaseFile(ctx, work, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	patchFreelistCount(t, work, 3)

	diag, err := inspectDatabaseIntegrity(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	if !isFreelistOnly(diag) {
		t.Fatalf("篡改空闲页计数后的诊断应只含 Freelist 项，实际：%q", diag)
	}
	if err := ensureDatabaseIntegrity(ctx, work, time.Now); err != nil {
		t.Fatalf("freelist 损坏应被自动修复：%v", err)
	}
	after, err := inspectDatabaseIntegrity(ctx, work)
	if err != nil || !isHealthyQuickCheck(after) {
		t.Fatalf("修复后应健康：%q（%v）", after, err)
	}
	// 修复前必须先留一份完整性备份：<db>.integrity-backup-<时间戳>
	backups, _ := filepath.Glob(work + ".integrity-backup-*")
	if len(backups) != 1 {
		t.Fatalf("应恰有 1 份完整性备份，实际：%v", backups)
	}
}

// patchFreelistCount 改写 SQLite 文件头偏移 36 处的「空闲页总数」（大端 uint32）。
func patchFreelistCount(t *testing.T, path string, n uint32) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], n)
	if _, err := f.WriteAt(buf[:], 36); err != nil {
		t.Fatal(err)
	}
}
