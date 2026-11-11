package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// 编排测试：用 golden 里的真实库结构（schema 来自 TS 迁移），换库用桩 relaunch，仓库用假密钥保护器。

type fakeProtector struct{ avail bool }

func (p fakeProtector) Backend() string { return "fake" }
func (p fakeProtector) Available() bool { return p.avail }
func (p fakeProtector) Secure() bool    { return p.avail }
func (p fakeProtector) Wrap(k []byte) ([]byte, error) {
	return append([]byte("W:"), k...), nil
}
func (p fakeProtector) Unwrap(w []byte) ([]byte, error) {
	if len(w) < 2 || string(w[:2]) != "W:" {
		return nil, os.ErrInvalid
	}
	return w[2:], nil
}

func newBackupTestLibrary(t *testing.T) (*Library, chan struct{}) {
	t.Helper()
	_, l0 := loadGolden(t)
	l0.db.Close()
	// loadGolden 的库在临时目录里；重新拷一份拿到可控路径
	dir := t.TempDir()
	userData := filepath.Join(dir, "ud")
	dbPath := filepath.Join(userData, "data", "knowledge.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("GUIZHI_GOLDEN_DIR"), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), dbPath, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l := &Library{db: db, dbPath: dbPath, paths: Paths{UserData: userData}, configDir: filepath.Join(userData, "config")}
	l.jobs = newBackgroundJobRuntime(l, func(map[string]any) {})
	relaunched := make(chan struct{}, 4)
	l.relaunch = func() { relaunched <- struct{}{} }
	l.backup.repo = &backupRepository{
		paths:         repoPaths{RepositoryDir: filepath.Join(l.backupsDir(), "repository"), DatabasePath: dbPath, ConfigDir: l.configDir, ImagesDir: l.paths.ImagesDir(), VideosDir: l.paths.VideosDir()},
		schemaVersion: supportedSchemaVersion,
		protector:     fakeProtector{avail: true},
	}
	t.Cleanup(func() { db.Close() })
	return l, relaunched
}

func TestRestoreLocalBackupOrchestration(t *testing.T) {
	l, relaunched := newBackupTestLibrary(t)
	ctx := context.Background()
	// golden 库里带着未完成的采集任务，先清掉，免得被恢复守卫挡住
	for _, tbl := range []string{"import_tasks", "crawl_jobs"} {
		_, _ = l.db.Execute(ctx, "DELETE FROM "+tbl)
	}
	lb := l.localBackups()
	info, err := lb.create("manual")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.Execute(ctx, "INSERT INTO settings (key, value) VALUES ('marker', '\"after-backup\"')"); err != nil {
		t.Fatal(err)
	}

	// 守卫：主题排版进行中拒绝
	l.hasActiveThemedReading = func() bool { return true }
	if r := l.restoreLocalBackup(ctx, info.FileName); r["success"] != false || r["error"] == nil {
		t.Fatalf("主题排版进行中应拒绝恢复: %v", r)
	}
	l.hasActiveThemedReading = nil
	if r := l.restoreLocalBackup(ctx, "../etc.db"); r["error"] != "找不到指定的备份文件" {
		t.Fatalf("非法文件名应拒绝: %v", r)
	}
	select {
	case <-relaunched:
		t.Fatal("拒绝恢复时不应重启")
	default:
	}

	r := l.restoreLocalBackup(ctx, info.FileName)
	if r["success"] != true || r["relaunching"] != true {
		t.Fatalf("恢复失败: %v", r)
	}
	select {
	case <-relaunched:
	case <-time.After(5 * time.Second):
		t.Fatal("恢复后未触发重启")
	}
	probe, err := openReadOnly(l.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	vals, err := queryStrings(probe, "SELECT value FROM settings WHERE key = 'marker'")
	if err != nil || len(vals) != 0 {
		t.Fatalf("恢复后不应含备份之后写入的数据: %v %v", vals, err)
	}
	pre := 0
	for _, b := range lb.list() {
		if b.Kind == "pre-restore" {
			pre++
		}
	}
	if pre != 1 {
		t.Fatalf("应留下 1 份 pre-restore 快照，实际 %d", pre)
	}
}

func TestRepositoryInitStatusAndJobSync(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	ctx := context.Background()
	repo := l.backupRepository()
	if st := repo.status(); st.Initialized {
		t.Fatal("新目录不应已初始化")
	}
	if err := repo.initialize("short"); err == nil {
		t.Fatal("弱口令应被拒绝")
	}
	if err := repo.initialize("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	st := repo.status()
	if !st.Initialized || !st.AutomaticAccessAvailable || st.KeyStorageBackend == nil || *st.KeyStorageBackend != "fake" {
		t.Fatalf("状态不符: %+v", st)
	}

	job := func() map[string]any {
		jobs, err := l.jobs.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range jobs {
			if j["kind"] == "backup" && j["scopeId"] == "repository" {
				return j
			}
		}
		return nil
	}
	// 后台任务总开关关闭 → 任务被登记为停用
	l.syncRepositoryBackupJob(ctx)
	j := job()
	if j == nil || j["state"] == "scheduled" {
		t.Fatalf("总开关关闭时任务应停用: %v", j)
	}
	for k, v := range map[string]string{"backgroundTasksEnabled": "true", "backupAutoEnabled": "true", "backupIntervalHours": "6"} {
		if _, err := l.db.Execute(ctx, "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", k, v); err != nil {
			t.Fatal(err)
		}
	}
	l.syncRepositoryBackupJob(ctx)
	j = job()
	if j == nil || j["state"] != "scheduled" || asInt(j["intervalMinutes"]) != 360 {
		t.Fatalf("开启后应按 6 小时调度: %v", j)
	}
	next := asInt(j["nextRunAt"])
	l.syncRepositoryBackupJob(ctx)
	if j2 := job(); asInt(j2["nextRunAt"]) != next {
		t.Fatalf("重复同步应保留既有 nextRunAt: %v → %v", next, j2["nextRunAt"])
	}
}

func TestConsumeRendererSettings(t *testing.T) {
	l, _ := newBackupTestLibrary(t)
	b := &Bridge{handlers: map[string]handler{}}
	l.registerBackup(b, &rendererBackupSettings{})
	h := b.handlers["backupRepository:consumeRendererSettings"]
	if v, err := h(context.Background(), nil); v != nil || err != nil {
		t.Fatalf("无待应用文件应返回 nil: %v %v", v, err)
	}
	pending := filepath.Join(l.repositoryConfigDir(), "pending-renderer-settings.json")
	os.MkdirAll(filepath.Dir(pending), 0o755)
	os.WriteFile(pending, []byte("{broken"), 0o644)
	if v, _ := h(context.Background(), nil); v != nil {
		t.Fatal("损坏文件应返回 nil")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("损坏文件应保留供排查")
	}
	os.WriteFile(pending, []byte(`{"theme":"dark"}`), 0o644)
	v, _ := h(context.Background(), nil)
	if raw, ok := v.(json.RawMessage); !ok || string(raw) != `{"theme":"dark"}` {
		t.Fatalf("应原样返回: %v", v)
	}
	if _, err := os.Stat(pending); err == nil {
		t.Fatal("成功消费后应删除文件")
	}
}
