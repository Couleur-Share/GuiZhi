package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importTestPassword = "correct horse battery staple"

// 源机器：golden 库 + 已初始化仓库 + 一份手动快照 + 导出的便携包。
func exportPortableForTest(t *testing.T, l *Library, name string) (archive, snapshotID string) {
	t.Helper()
	repo := l.backupRepository()
	if _, err := os.Stat(repo.headerPath()); err != nil {
		if err := repo.initialize(importTestPassword); err != nil {
			t.Fatal(err)
		}
	}
	res := repo.createSnapshot(snapshotRequest{RecoveryPassword: importTestPassword, Kind: "auto", AppVersion: "0.29.0"})
	if !res.Success || res.Snapshot == nil {
		t.Fatalf("创建快照失败: %+v", res)
	}
	archive = filepath.Join(t.TempDir(), name+".guizhi-backup")
	if err := repo.exportPortable(res.Snapshot.FileName, archive, importTestPassword); err != nil {
		t.Fatal(err)
	}
	return archive, res.Snapshot.FileName
}

func newTargetRepo(t *testing.T) *backupRepository {
	t.Helper()
	base := t.TempDir()
	return &backupRepository{
		paths:         repoPaths{RepositoryDir: filepath.Join(base, "backups", "repository"), DatabasePath: filepath.Join(base, "data", "knowledge.db"), ConfigDir: filepath.Join(base, "config"), ImagesDir: filepath.Join(base, "img"), VideosDir: filepath.Join(base, "vid")},
		schemaVersion: supportedSchemaVersion,
		protector:     fakeProtector{avail: true},
	}
}

func noImportLeftovers(t *testing.T, r *backupRepository) {
	t.Helper()
	entries, _ := os.ReadDir(r.paths.RepositoryDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".import-work-") {
			t.Fatalf("残留临时目录: %s", e.Name())
		}
	}
}

func TestImportPortableIntoEmptyMachine(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	archive, id := exportPortableForTest(t, l, "p1")
	target := newTargetRepo(t)

	res := target.importPortable(archive, importTestPassword)
	if !res.Success || res.ReplacedRepository || res.AlreadyExisted {
		t.Fatalf("导入失败: %+v", res)
	}
	list := target.listSnapshots()
	if len(list) != 1 || list[0].FileName != id || list[0].Kind != "manual" {
		t.Fatalf("快照应入列且记为手动: %+v", list)
	}
	if st := target.status(); !st.Initialized || !st.AutomaticAccessAvailable {
		t.Fatalf("导入后应已启用且可自动访问: %+v", st)
	}
	srcManifest, err := l.backupRepository().readManifest(id, importTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	dstManifest, err := target.readManifest(id, importTestPassword)
	if err != nil || len(dstManifest.Entries) != len(srcManifest.Entries) {
		t.Fatalf("导入后清单应一致: %v", err)
	}
	for _, e := range dstManifest.Entries {
		if _, err := target.readEntry(e); err != nil {
			t.Fatalf("对象 %s 无法读取: %v", e.LogicalPath, err)
		}
	}
	noImportLeftovers(t, target)
}

func TestImportPortableRejectsWrongPasswordAndLeavesNothing(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	archive, _ := exportPortableForTest(t, l, "p1")
	target := newTargetRepo(t)
	if res := target.importPortable(archive, "wrong password value"); res.Success || res.Error == "" {
		t.Fatalf("错口令应拒绝: %+v", res)
	}
	if _, err := os.Stat(target.headerPath()); err == nil {
		t.Fatal("失败后不应留下仓库头")
	}
	noImportLeftovers(t, target)
}

func TestImportPortableSameRepositoryMerges(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	first, id1 := exportPortableForTest(t, l, "p1")
	target := newTargetRepo(t)
	if res := target.importPortable(first, importTestPassword); !res.Success {
		t.Fatalf("%+v", res)
	}
	if res := target.importPortable(first, importTestPassword); !res.Success || !res.AlreadyExisted {
		t.Fatalf("重复导入应标记已存在: %+v", res)
	}
	if len(target.listSnapshots()) != 1 {
		t.Fatal("重复导入不应新增快照")
	}
	second, id2 := exportPortableForTest(t, l, "p2")
	if res := target.importPortable(second, importTestPassword); !res.Success {
		t.Fatalf("同仓库后续快照应并入: %+v", res)
	}
	if got := len(target.listSnapshots()); got != 2 || id1 == id2 {
		t.Fatalf("应有两份快照，实际 %d", got)
	}
}

func TestImportPortableRefusesUnrelatedRepositoryWithSnapshots(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	archive, _ := exportPortableForTest(t, l, "p1")

	other, _ := newBackupTestLibrary(t) // 另一台机器：不同密钥的仓库，且有自己的快照
	repo := other.backupRepository()
	if err := repo.initialize("another long passphrase"); err != nil {
		t.Fatal(err)
	}
	local := repo.createSnapshot(snapshotRequest{RecoveryPassword: "another long passphrase", Kind: "manual", AppVersion: "0.29.0"})
	if !local.Success {
		t.Fatalf("%+v", local)
	}
	res := repo.importPortable(archive, importTestPassword)
	if res.Success || !strings.Contains(res.Error, "无法合并") {
		t.Fatalf("应拒绝合并: %+v", res)
	}
	if list := repo.listSnapshots(); len(list) != 1 || list[0].FileName != local.Snapshot.FileName {
		t.Fatalf("本机快照应保持不变: %+v", list)
	}
}

func TestImportPortableReplacesEmptyLocalRepository(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	archive, id := exportPortableForTest(t, l, "p1")
	target := newTargetRepo(t)
	if err := target.initialize("another long passphrase"); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(target.objectsDir(), strings.Repeat("f", 64))
	if err := os.WriteFile(stale, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := target.importPortable(archive, importTestPassword)
	if !res.Success || !res.ReplacedRepository {
		t.Fatalf("无快照的本机仓库应被换用: %+v", res)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("旧密钥的孤立对象应被清掉")
	}
	if _, err := target.readManifest(id, importTestPassword); err != nil {
		t.Fatalf("应能用便携口令读取: %v", err)
	}
	if _, err := target.readManifest(id, "another long passphrase"); err == nil {
		t.Fatal("旧口令不应再可用")
	}
}

func TestImportPortableRejectsTamperedAndMaliciousArchives(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	archive, _ := exportPortableForTest(t, l, "p1")
	target := newTargetRepo(t)

	// 对象被篡改：STORE 模式下翻转某个对象数据中的一个字节
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	objs, _ := os.ReadDir(l.backupRepository().objectsDir())
	sample, _ := os.ReadFile(filepath.Join(l.backupRepository().objectsDir(), objs[0].Name()))
	at := strings.Index(string(raw), string(sample))
	if at < 0 {
		t.Fatal("在包里找不到对象数据")
	}
	raw[at+len(sample)/2] ^= 0xff
	bad := filepath.Join(t.TempDir(), "bad.guizhi-backup")
	os.WriteFile(bad, raw, 0o644)
	if res := target.importPortable(bad, importTestPassword); res.Success {
		t.Fatal("被篡改的包应被拒绝")
	}
	if _, err := os.Stat(target.headerPath()); err == nil {
		t.Fatal("失败后不应留下仓库头")
	}

	// 越权路径与压缩条目
	for name, mk := range map[string]func(w *zip.Writer){
		"traversal": func(w *zip.Writer) { f, _ := w.Create("../evil.txt"); f.Write([]byte("x")) },
		"deflated": func(w *zip.Writer) {
			f, _ := w.CreateHeader(&zip.FileHeader{Name: "repository-header.json", Method: zip.Deflate})
			f.Write([]byte(strings.Repeat("A", 500)))
		},
	} {
		p := filepath.Join(t.TempDir(), name+".guizhi-backup")
		out, _ := os.Create(p)
		w := zip.NewWriter(out)
		mk(w)
		w.Close()
		out.Close()
		if res := target.importPortable(p, importTestPassword); res.Success {
			t.Fatalf("%s 应被拒绝", name)
		}
	}
	noImportLeftovers(t, target)
}
