package main

// 0 字节数据库文件：磁盘写满或写入被中断后会留下空文件，原先被静默当成「全新的空库」，用户看到的是资料全没了。
// 设计（用户已定）：存在可恢复的备份时阻断启动并给出可操作的说明（走启动失败的对话框通道），
// 没有任何备份时仍按空库启动；两种情况都不改动该文件。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func emptyDBFixture(t *testing.T) (dbPath, backups string) {
	t.Helper()
	root := t.TempDir()
	dbPath = filepath.Join(root, "data", "knowledge.db")
	backups = filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

func TestEmptyDatabaseWithBackupsBlocksStartup(t *testing.T) {
	cases := map[string]func(dbPath, backups string){
		"pre-restore 备份": func(_, backups string) {
			os.MkdirAll(backups, 0o755)
			os.WriteFile(filepath.Join(backups, "knowledge-pre-restore-20250101-000000.db"), []byte("SQLite format 3\x00data"), 0o644)
		},
		"手动备份": func(_, backups string) {
			os.MkdirAll(backups, 0o755)
			os.WriteFile(filepath.Join(backups, "knowledge-manual-20250101-000000.db"), []byte("SQLite format 3\x00data"), 0o644)
		},
		"完整性备份": func(dbPath, _ string) {
			os.WriteFile(dbPath+".integrity-backup-20250101-000000", []byte("SQLite format 3\x00data"), 0o644)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dbPath, backups := emptyDBFixture(t)
			setup(dbPath, backups)
			lease, err := initDatabaseFile(context.Background(), dbPath, dbInitOptions{BackupsDir: backups})
			if err == nil {
				lease.release()
				t.Fatal("存在备份时，0 字节数据库应阻断启动")
			}
			for _, want := range []string{"0 字节", dbPath, "备份"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("错误说明缺少 %q：%v", want, err)
				}
			}
			if st, e := os.Stat(dbPath); e != nil || st.Size() != 0 {
				t.Errorf("阻断时不得改动空文件：%v %v", st, e)
			}
		})
	}
}

func TestEmptyDatabaseWithoutUsableBackupStartsAsEmpty(t *testing.T) {
	dbPath, backups := emptyDBFixture(t)
	os.MkdirAll(backups, 0o755)
	// 0 字节的备份文件不可恢复，不算备份
	os.WriteFile(filepath.Join(backups, "knowledge-pre-restore-20250101-000000.db"), nil, 0o644)
	lease, err := initDatabaseFile(context.Background(), dbPath, dbInitOptions{BackupsDir: backups})
	if err != nil {
		t.Fatalf("没有可用备份时应按空库启动：%v", err)
	}
	lease.release()
	if st, _ := os.Stat(dbPath); st == nil || st.Size() == 0 {
		t.Error("应已建好数据库结构")
	}
}

func TestEmptyDatabaseWithNoBackupDirStartsAsEmpty(t *testing.T) {
	dbPath, backups := emptyDBFixture(t) // backups 目录不存在
	lease, err := initDatabaseFile(context.Background(), dbPath, dbInitOptions{BackupsDir: backups})
	if err != nil {
		t.Fatalf("没有备份目录时应按空库启动：%v", err)
	}
	lease.release()
}

// 用户按提示删除空文件后，下一次启动要能正常新建，不再被拦。
func TestEmptyDatabaseBlockClearsAfterUserRemovesFile(t *testing.T) {
	dbPath, backups := emptyDBFixture(t)
	os.MkdirAll(backups, 0o755)
	os.WriteFile(filepath.Join(backups, "knowledge-pre-restore-20250101-000000.db"), []byte("SQLite format 3\x00data"), 0o644)
	if lease, err := initDatabaseFile(context.Background(), dbPath, dbInitOptions{BackupsDir: backups}); err == nil {
		lease.release()
		t.Fatal("应先被阻断")
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	lease, err := initDatabaseFile(context.Background(), dbPath, dbInitOptions{BackupsDir: backups})
	if err != nil {
		t.Fatalf("删除空文件后应能新建：%v", err)
	}
	lease.release()
}
