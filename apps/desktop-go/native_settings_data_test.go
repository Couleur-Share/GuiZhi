package main

// 设置页「数据与备份」分区的主包适配器集成测试：
// 真实的 IPC 处理器（备份、导出、配置迁移）+ 临时数据目录（不碰 %APPDATA%\GuiZhi）；
// 文件选择、保存对话框与重启都换成测试桩，shell:openPath 只用不存在的路径（绝不真的打开资源管理器）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// 这些口令与密钥只应出现在入参里：任何日志、错误文案、导出文件的明文处都不该看到它们。
const (
	testExportPassword   = "export-pw-ZZ91xx"
	testRecoveryPassword = "recovery-pw-QQ77xx"
	testNextRecoveryPW   = "recovery-next-KK55xx"
	testProxySecret      = "proxy-secret-9x"
	testAIKey            = "sk-secret-ai-4711"
)

var allTestSecrets = []string{testExportPassword, testRecoveryPassword, testNextRecoveryPW, testProxySecret, testAIKey}

// lockedBuffer 收集 log 包与设置服务的日志输出，用来断言口令没有进日志。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) Logf(format string, args ...any) { fmt.Fprintf(l, format+"\n", args...) }

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

type dataEnv struct {
	*settingsEnv
	b          *Bridge
	svc        services.Services
	relaunched chan struct{}
	logs       *lockedBuffer

	pickFile string // 下一次「选择文件」返回的路径，空串是取消
	saveTo   string // 下一次「保存为」返回的路径，空串是取消
	pickDir  string // 下一次「选择目录」返回的路径，空串是取消
}

func newDataEnv(t *testing.T) *dataEnv {
	t.Helper()
	e := &dataEnv{settingsEnv: newSettingsEnv(t), relaunched: make(chan struct{}, 4), logs: &lockedBuffer{}}
	log.SetOutput(e.logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	l := e.lib
	l.configDir = e.paths.ConfigDir()
	if err := os.MkdirAll(l.configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	l.jobs = newBackgroundJobRuntime(l, func(map[string]any) {})
	l.relaunch = func() { e.relaunched <- struct{}{} }
	l.backup.repo = &backupRepository{
		paths: repoPaths{RepositoryDir: filepath.Join(l.backupsDir(), "repository"), DatabasePath: l.dbPath, ConfigDir: l.configDir,
			ImagesDir: l.paths.ImagesDir(), VideosDir: l.paths.VideosDir()},
		schemaVersion: supportedSchemaVersion,
		protector:     fakeProtector{avail: true},
	}
	one := func(p string) ([]string, error) {
		if p == "" {
			return nil, nil
		}
		return []string{p}, nil
	}
	l.pickFiles = func(bool, []dialogFilter) ([]string, error) { return one(e.pickFile) }
	l.saveFileDialog = func(string, string, []dialogFilter) (string, error) { return e.saveTo, nil }
	l.pickDirectory = func(string) (string, error) { return e.pickDir, nil }

	b := newSystemBridge()
	b.db = l.db
	e.b = b
	l.registerAppRuntime(b)
	l.registerExport(b)
	l.registerDataPath(b)
	l.registerConfigTransfer(b)
	l.registerBackup(b, &rendererBackupSettings{})
	l.registerShortcutsIPC(b)
	l.startShortcuts(true)
	l.registerWrite(b)

	e.ns = newNativeSettings(l, e.store, e.logs.Logf)
	e.svc = newNativeServices(b, newPushHub())
	e.svc.Settings, e.svc.Prefs = e.ns, e.store
	e.svc.ConfigTransfer = newConfigAdapter(b, l, e.ns, e.store)
	return e
}

func (e *dataEnv) assertNoSecretsLogged(t *testing.T) {
	t.Helper()
	out := e.logs.String()
	for _, s := range allTestSecrets {
		if strings.Contains(out, s) {
			t.Errorf("口令或密钥 %q 出现在日志里：\n%s", s, out)
		}
	}
}

func assertNoSecretsIn(t *testing.T, what, text string) {
	t.Helper()
	for _, s := range allTestSecrets {
		if strings.Contains(text, s) {
			t.Errorf("%s 含有口令或密钥 %q：%s", what, s, text)
		}
	}
}

func (e *dataEnv) waitRelaunch(t *testing.T) {
	t.Helper()
	select {
	case <-e.relaunched:
	case <-time.After(5 * time.Second):
		t.Fatal("没有等到重启")
	}
}

// ── 数据目录 ──

// 运行时路径来自 app:getRuntimePaths；打开目录只走 shell:openPath 的校验（不存在的路径是失败，不会真的打开）；重启走 app:relaunch。
func TestSettingsDataAdapterRuntimePathsOpenAndRelaunch(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	data := e.svc.Data

	paths, err := data.RuntimePaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := services.RuntimePaths{
		UserDataPath: e.paths.UserData, DataDir: e.paths.DataDir(), DatabasePath: e.paths.DatabasePath(),
		BackupsDir: e.paths.BackupsDir(), LogsDir: e.paths.LogsDir(),
	}
	if paths != want {
		t.Errorf("运行时路径不对：\n got  %+v\n want %+v", paths, want)
	}

	missing := filepath.Join(e.paths.UserData, "no-such-dir")
	if err := data.OpenPath(ctx, missing); err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Errorf("不存在的目录应是带原因的失败：%v", err)
	}
	if err := data.OpenPath(ctx, "  "); err == nil {
		t.Error("空路径应失败")
	}

	if err := data.Relaunch(ctx); err != nil {
		t.Fatalf("重启：%v", err)
	}
	e.waitRelaunch(t)

	e.lib.relaunch = nil
	if err := data.Relaunch(ctx); err == nil {
		t.Error("重启不可用时应返回错误，而不是假装成功")
	}
}

// ── 旧快照 ──

// 创建走 VACUUM INTO（文件落在备份目录、非空），列表新的在前，删除后消失；未知文件名的恢复与删除是带原因的失败。
func TestSettingsDataAdapterLocalBackupLifecycle(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	bk := e.svc.Backup

	if list, err := bk.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("没有备份时应是空列表：%v %v", list, err)
	}
	info, err := bk.Create(ctx)
	if err != nil {
		t.Fatalf("创建备份：%v", err)
	}
	if info.FileName == "" || info.Kind != services.BackupKindManual || info.SizeBytes <= 0 || info.CreatedAt <= 0 {
		t.Errorf("备份信息不全：%+v", info)
	}
	if st, err := os.Stat(filepath.Join(e.paths.BackupsDir(), info.FileName)); err != nil || st.Size() != info.SizeBytes {
		t.Errorf("备份文件应在备份目录里且大小一致：%v", err)
	}
	list, err := bk.List(ctx)
	if err != nil || len(list) != 1 || list[0].FileName != info.FileName {
		t.Fatalf("列表应包含刚创建的备份：%+v %v", list, err)
	}

	if err := bk.Restore(ctx, "nope.db"); err == nil || !strings.Contains(err.Error(), "找不到") {
		t.Errorf("恢复未知备份应说明找不到：%v", err)
	}
	if err := bk.Delete(ctx, "nope.db"); err == nil {
		t.Error("删除未知备份应失败")
	}
	if err := bk.Delete(ctx, info.FileName); err != nil {
		t.Fatalf("删除备份：%v", err)
	}
	if list, _ := bk.List(ctx); len(list) != 0 {
		t.Errorf("删除后列表应为空：%+v", list)
	}
	select {
	case <-e.relaunched:
		t.Error("失败的恢复不该触发重启")
	case <-time.After(100 * time.Millisecond):
	}
}

// 恢复成功：处理器换库并安排重启（这里是重启桩收到信号），数据按备份时的内容回来。
func TestSettingsDataAdapterLocalBackupRestore(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	bk := e.svc.Backup
	e.putRow(t, "marker", `"before-backup"`)
	info, err := bk.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.putRow(t, "marker", `"after-backup"`)

	if err := bk.Restore(ctx, info.FileName); err != nil {
		t.Fatalf("恢复：%v", err)
	}
	e.waitRelaunch(t)
	// 数据库此时已按恢复流程换掉并关闭；重新打开磁盘上的库核对内容。
	raw, err := os.ReadFile(e.paths.DatabasePath())
	if err != nil || !bytes.Contains(raw, []byte("before-backup")) || bytes.Contains(raw, []byte("after-backup")) {
		t.Errorf("恢复后的库应是备份时的内容：%v", err)
	}
}

// ── 完整备份仓库 ──

func TestSettingsDataAdapterRepositoryLifecycle(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	bk := e.svc.Backup

	st, err := bk.RepositoryStatus(ctx)
	if err != nil || st.Initialized {
		t.Fatalf("新库的仓库应未启用：%+v %v", st, err)
	}
	if err := bk.InitRepository(ctx, "short"); err == nil {
		t.Error("过短的恢复口令应被拒绝")
	} else {
		assertNoSecretsIn(t, "启用失败的错误", err.Error())
	}
	if err := bk.InitRepository(ctx, testRecoveryPassword); err != nil {
		t.Fatalf("启用：%v", err)
	}
	if err := bk.InitRepository(ctx, testRecoveryPassword); err == nil {
		t.Error("重复启用应失败")
	}
	if st, _ := bk.RepositoryStatus(ctx); !st.Initialized || !st.AutomaticAccessAvailable {
		t.Errorf("启用后应已初始化且可无人值守访问（假密钥环可用）：%+v", st)
	}

	created, err := bk.CreateSnapshot(ctx, "")
	if err != nil {
		t.Fatalf("完整备份：%v", err)
	}
	id := created.Snapshot.FileName
	if id == "" || created.Snapshot.Kind != services.BackupKindManual || created.CreatedObjects <= 0 {
		t.Errorf("完整备份结果不全：%+v", created)
	}
	snaps, err := bk.ListSnapshots(ctx)
	if err != nil || len(snaps) != 1 || snaps[0].FileName != id || snaps[0].Summary == nil {
		t.Fatalf("快照列表：%+v %v", snaps, err)
	}

	preview, err := bk.PreviewRestore(ctx, id, "")
	if err != nil || preview.Snapshot.FileName != id {
		t.Fatalf("恢复预检应通过：%+v %v", preview, err)
	}
	_, err = bk.PreviewRestore(ctx, "no-such-snapshot", "")
	var pe *services.RestorePreviewError
	if !errors.As(err, &pe) || pe.Message == "" {
		t.Errorf("预检失败应是 *RestorePreviewError：%v", err)
	}

	// 改口令：旧口令错误时失败且不泄露口令；成功后可用新口令导出。
	if err := bk.ChangePassword(ctx, "wrong-current-pw-123", testNextRecoveryPW); err == nil {
		t.Error("当前口令不对应失败")
	} else {
		assertNoSecretsIn(t, "改口令失败的错误", err.Error())
	}
	if err := bk.ChangePassword(ctx, testRecoveryPassword, testNextRecoveryPW); err != nil {
		t.Fatalf("改口令：%v", err)
	}

	// 便携备份：导出到保存路径，取消不是失败；导入要先给口令；再导回同一份是「已存在」。
	e.saveTo = ""
	if r, err := bk.ExportPortable(ctx, id, ""); err != nil || !r.Canceled {
		t.Errorf("取消保存应是 Canceled：%+v %v", r, err)
	}
	e.saveTo = filepath.Join(t.TempDir(), "backup.guizhi-backup")
	exported, err := bk.ExportPortable(ctx, id, "")
	if err != nil || exported.FilePath != e.saveTo {
		t.Fatalf("导出便携备份：%+v %v", exported, err)
	}
	if st, err := os.Stat(exported.FilePath); err != nil || st.Size() == 0 {
		t.Errorf("便携备份文件应存在且非空：%v", err)
	}
	if _, err := bk.ImportPortable(ctx, ""); err == nil {
		t.Error("导入便携备份必须先给口令")
	}
	e.pickFile = ""
	if r, err := bk.ImportPortable(ctx, testNextRecoveryPW); err != nil || !r.Canceled {
		t.Errorf("取消选文件应是 Canceled：%+v %v", r, err)
	}
	e.pickFile = exported.FilePath
	imported, err := bk.ImportPortable(ctx, testNextRecoveryPW)
	if err != nil {
		t.Fatalf("导入便携备份：%v", err)
	}
	if !imported.AlreadyExisted && imported.Snapshot == nil {
		t.Errorf("导回同一份应是已存在或带快照：%+v", imported)
	}

	if err := bk.DeleteSnapshot(ctx, "no-such-snapshot"); err == nil {
		t.Error("删除未知快照应失败")
	}
	if err := bk.DeleteSnapshot(ctx, id); err != nil {
		t.Fatalf("删除快照：%v", err)
	}
	if snaps, _ := bk.ListSnapshots(ctx); len(snaps) != 0 {
		t.Errorf("删除后快照列表应为空：%+v", snaps)
	}
	if err := bk.RestoreSnapshot(ctx, "no-such-snapshot", ""); err == nil {
		t.Error("恢复未知快照应失败")
	}
	e.assertNoSecretsLogged(t)
}

// 设置里的备份开关与间隔变化后，自动备份任务跟着重新调度；保留数量变化不触发。
func TestSettingsDataAdapterBackupScheduleFollowsSettings(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	if err := e.svc.Backup.InitRepository(ctx, testRecoveryPassword); err != nil {
		t.Fatal(err)
	}
	stop := watchBackupSchedule(e.ns, e.lib)
	defer stop()

	// 任务状态：停用是 paused，启用是 scheduled。
	jobState := func() string {
		jobs, err := e.lib.jobs.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range jobs {
			if j["kind"] == "backup" && j["scopeId"] == "repository" {
				return fmt.Sprint(j["state"])
			}
		}
		return ""
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if ok() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("没有等到：%s", what)
	}

	// 仓库已启用，但后台任务还没开：任务是停用的。
	if state := jobState(); state != "paused" {
		t.Fatalf("后台任务关闭时自动备份任务应停用：%q", state)
	}
	update := func(f func(a *settings.App)) {
		t.Helper()
		if _, err := e.ns.Update(func(v *settings.Values) { f(&v.App) }); err != nil {
			t.Fatal(err)
		}
	}
	update(func(a *settings.App) { a.BackgroundTasksEnabled, a.BackupAutoEnabled = true, true })
	waitFor("开启后台任务与自动备份后任务启用", func() bool { return jobState() == "scheduled" })
	update(func(a *settings.App) { a.BackupAutoEnabled = false })
	waitFor("关闭自动备份后任务停用", func() bool { return jobState() == "paused" })
	update(func(a *settings.App) { a.BackupAutoEnabled = true })
	waitFor("重新开启后任务恢复调度", func() bool { return jobState() == "scheduled" })
}

// ── Markdown 导出 ──

func TestSettingsDataAdapterMarkdownExport(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	bk := e.svc.Backup

	e.pickDir = ""
	if r, err := bk.ExportMarkdown(ctx); err != nil || !r.Canceled {
		t.Fatalf("取消选目录应是 Canceled：%+v %v", r, err)
	}

	if _, err := callAs[struct {
		ID string `json:"id"`
	}](ctx, e.b, "knowledge:create", map[string]any{"title": "导出测试", "content": "正文内容"}); err != nil {
		t.Fatal(err)
	}
	e.pickDir = t.TempDir()
	r, err := bk.ExportMarkdown(ctx)
	if err != nil {
		t.Fatalf("导出：%v", err)
	}
	if r.Canceled || r.Count != 1 || filepath.Dir(r.Dir) != e.pickDir {
		t.Fatalf("导出结果不对：%+v", r)
	}
	var found bool
	_ = filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			raw, _ := os.ReadFile(p)
			found = strings.Contains(string(raw), "正文内容")
		}
		return nil
	})
	if !found {
		t.Errorf("导出目录里应有含正文的 .md：%s", r.Dir)
	}

	e.lib.pickDirectory = func(string) (string, error) { return "", errors.New("目录选择器不可用") }
	if _, err := bk.ExportMarkdown(ctx); err == nil || !strings.Contains(err.Error(), "目录选择器不可用") {
		t.Errorf("选择器失败应带原因：%v", err)
	}
}

// ── 配置导出与导入 ──

func seedConfigEnv(t *testing.T, e *dataEnv) {
	t.Helper()
	if _, err := e.ns.Update(func(v *settings.Values) {
		v.UI.ThemeColor = "custom"
		v.UI.CustomThemeHex = "#112233"
		v.App.BackupKeepCount = 7
		v.App.NetworkProxy = settings.NetworkProxy{Mode: "manual", Protocol: "http", Host: "proxy.test", Port: 8080, Username: "proxy-user", Password: testProxySecret, Bypass: "<local>"}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Set(settings.PrefSidebarCollapsed, true); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Set(settings.PrefLibraryViewMode, "list"); err != nil {
		t.Fatal(err)
	}
	ai := fmt.Sprintf(`{"kind":"guizhi-ai-config","version":1,"updatedAt":"2026-01-01T00:00:00.000Z",`+
		`"providers":[{"id":"p1","name":"测试服务商","provider":"openai","apiProtocol":"openai","apiUrl":"https://api.example.test/v1","apiKey":%q}],`+
		`"models":[{"id":"m1","name":"测试模型","provider":"openai","providerId":"p1","model":"test-model","apiProtocol":"openai","apiUrl":"https://api.example.test/v1","apiKey":%q,"isDefault":true,"capabilities":{"chat":true}}],`+
		`"modelRouteDefaults":{}}`, testAIKey, testAIKey)
	if err := os.WriteFile(filepath.Join(e.lib.configDir, aiConfigFileName), []byte(ai), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 导出：取消不是失败；不带机密的文件里没有明文密钥；带机密的文件被口令加密，明文也找不到。
func TestSettingsDataAdapterConfigExport(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	seedConfigEnv(t, e)
	cfg := e.svc.ConfigTransfer

	e.saveTo = ""
	if r, err := cfg.Export(ctx, services.ConfigExportOptions{IncludeUILayout: true}); err != nil || !r.Canceled {
		t.Fatalf("取消保存应是 Canceled：%+v %v", r, err)
	}

	plain := filepath.Join(t.TempDir(), "plain.json")
	e.saveTo = plain
	r, err := cfg.Export(ctx, services.ConfigExportOptions{IncludeUILayout: true, IncludeShortcuts: true})
	if err != nil || r.FilePath != plain {
		t.Fatalf("导出（不带机密）：%+v %v", r, err)
	}
	raw, _ := os.ReadFile(plain)
	assertNoSecretsIn(t, "不带机密的导出文件", string(raw))
	for _, want := range []string{"proxy.test", "#112233", "测试服务商", "测试模型", "ui-storage"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("导出文件应含 %q", want)
		}
	}

	enc := filepath.Join(t.TempDir(), "enc.json")
	e.saveTo = enc
	if _, err := cfg.Export(ctx, services.ConfigExportOptions{IncludeSecrets: true, Password: testExportPassword, IncludeUILayout: true}); err != nil {
		t.Fatalf("导出（带机密）：%v", err)
	}
	raw, _ = os.ReadFile(enc)
	assertNoSecretsIn(t, "加密导出文件", string(raw))
	if !strings.Contains(string(raw), "proxy.test") {
		t.Error("加密导出仍应带非机密字段")
	}

	// 处理器拒绝的组合（带机密但没有密码）：错误带原因；写不出文件也是带原因的失败，都不含口令。
	e.saveTo = filepath.Join(t.TempDir(), "bad.json")
	if _, err := cfg.Export(ctx, services.ConfigExportOptions{IncludeSecrets: true}); err == nil || !strings.Contains(err.Error(), "密码") {
		t.Errorf("带机密导出没有密码应被处理器拒绝并说明原因：%v", err)
	}
	e.saveTo = filepath.Join(t.TempDir(), "no-such-dir", "x", "out.json")
	if _, err := cfg.Export(ctx, services.ConfigExportOptions{IncludeSecrets: true, Password: testExportPassword}); err != nil {
		assertNoSecretsIn(t, "写文件失败的错误", err.Error())
	}
	e.assertNoSecretsLogged(t)
}

// 导入：预览、口令错误、导入成功后设置（含代理口令）、AI 配置与界面布局都回来；不选的部分保持不变。
func TestSettingsDataAdapterConfigImportRoundTrip(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	seedConfigEnv(t, e)
	cfg := e.svc.ConfigTransfer

	file := filepath.Join(t.TempDir(), "enc.json")
	e.saveTo = file
	if _, err := cfg.Export(ctx, services.ConfigExportOptions{IncludeSecrets: true, Password: testExportPassword, IncludeUILayout: true}); err != nil {
		t.Fatal(err)
	}

	// 把本机改得面目全非。
	if _, err := e.ns.Update(func(v *settings.Values) {
		v.UI.ThemeColor = "blue"
		v.App.BackupKeepCount = 30
		v.App.NetworkProxy = settings.DefaultProxy()
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Set(settings.PrefSidebarCollapsed, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.lib.configDir, aiConfigFileName)); err != nil {
		t.Fatal(err)
	}

	// 取消选文件；选到不是配置的文件是带原因的失败。
	e.pickFile = ""
	if r, err := cfg.Read(ctx); err != nil || !r.Canceled {
		t.Fatalf("取消选文件应是 Canceled：%+v %v", r, err)
	}
	junk := filepath.Join(t.TempDir(), "junk.json")
	_ = os.WriteFile(junk, []byte(`{"hello":"world"}`), 0o600)
	e.pickFile = junk
	if _, err := cfg.Read(ctx); err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Errorf("不是配置文件应失败并给出原因：%v", err)
	}

	e.pickFile = file
	read, err := cfg.Read(ctx)
	if err != nil {
		t.Fatalf("读取：%v", err)
	}
	if read.FilePath != file || !read.Preview.Encrypted || read.Preview.ProviderCount != 1 || read.Preview.ModelCount != 1 || read.Preview.UILayoutKeyCount == 0 {
		t.Errorf("预览不对：%+v", read)
	}

	// 口令错误：类型化错误，界面据此在输入框下提示；没有任何改动。
	all := services.ConfigApplySelection{Settings: true, UILayout: true, Shortcuts: true, MCPScope: true}
	_, err = cfg.Apply(ctx, read.FilePath, "wrong-password-123", all)
	var wp *services.ConfigWrongPasswordError
	if !errors.As(err, &wp) {
		t.Fatalf("口令错误应是 *ConfigWrongPasswordError：%v", err)
	}
	assertNoSecretsIn(t, "口令错误的文案", err.Error())
	if v := e.snapshot(t); v.UI.ThemeColor != "blue" || v.App.BackupKeepCount != 30 {
		t.Errorf("口令错误不该改动设置：%+v", v.UI.ThemeColor)
	}

	// 只导入设置，不导入布局：布局保持不变。
	res, err := cfg.Apply(ctx, read.FilePath, testExportPassword, services.ConfigApplySelection{Settings: true})
	if err != nil {
		t.Fatalf("导入：%v", err)
	}
	if res.SnapshotDir == "" {
		t.Error("导入前应留下配置快照")
	}
	v := e.snapshot(t)
	if v.UI.ThemeColor != "custom" || v.UI.CustomThemeHex != "#112233" || v.App.BackupKeepCount != 7 {
		t.Errorf("设置没有按文件还原：%+v %d", v.UI, v.App.BackupKeepCount)
	}
	if p := v.App.NetworkProxy; p.Mode != "manual" || p.Host != "proxy.test" || p.Port != 8080 || p.Password != testProxySecret {
		t.Errorf("代理（含解密后的口令）没有还原：%v", p)
	}
	if raw, ok := e.store.Get(settings.PrefSidebarCollapsed); !ok || string(raw) != "false" {
		t.Errorf("没选布局时布局应保持不变：%s", raw)
	}
	if _, err := os.Stat(filepath.Join(e.lib.configDir, aiConfigFileName)); err != nil {
		t.Errorf("AI 配置应由导入写回 ai-models.json：%v", err)
	} else if raw, _ := os.ReadFile(filepath.Join(e.lib.configDir, aiConfigFileName)); !strings.Contains(string(raw), "测试服务商") {
		t.Errorf("ai-models.json 里应有导入的服务商：%s", raw)
	}

	// 再导入一次，这次带布局。
	if _, err := cfg.Apply(ctx, read.FilePath, testExportPassword, services.ConfigApplySelection{UILayout: true}); err != nil {
		t.Fatalf("导入布局：%v", err)
	}
	if raw, ok := e.store.Get(settings.PrefSidebarCollapsed); !ok || string(raw) != "true" {
		t.Errorf("布局应还原：%s", raw)
	}
	if raw, ok := e.store.Get(settings.PrefLibraryViewMode); !ok || string(raw) != `"list"` {
		t.Errorf("列表视图模式应还原：%s", raw)
	}

	// 没有经 Read 选中的路径不能被导入。
	if _, err := cfg.Apply(ctx, filepath.Join(t.TempDir(), "other.json"), testExportPassword, all); err == nil {
		t.Error("没有读过的路径应被拒绝")
	}
	e.assertNoSecretsLogged(t)
}

// 配置导入：设置服务拒绝导入值（这里用不可写的偏好目录模拟布局写失败）时，设置仍然生效，布局失败变成警告而不是整体失败。
func TestSettingsDataAdapterConfigImportLayoutFailureIsWarning(t *testing.T) {
	e := newDataEnv(t)
	ctx := context.Background()
	seedConfigEnv(t, e)

	file := filepath.Join(t.TempDir(), "plain.json")
	e.saveTo = file
	if _, err := e.svc.ConfigTransfer.Export(ctx, services.ConfigExportOptions{IncludeUILayout: true}); err != nil {
		t.Fatal(err)
	}
	e.pickFile = file
	read, err := e.svc.ConfigTransfer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	adapter := newConfigAdapter(e.b, e.lib, e.ns, failingPrefs{})
	res, err := adapter.Apply(ctx, read.FilePath, "", services.ConfigApplySelection{Settings: true, UILayout: true})
	if err != nil {
		t.Fatalf("布局写失败不该让整次导入失败：%v", err)
	}
	var warned bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "界面布局") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("应有界面布局的警告：%v", res.Warnings)
	}
}

type failingPrefs struct{}

func (failingPrefs) Get(string) (json.RawMessage, bool) { return nil, false }
func (failingPrefs) Set(string, any) error              { return errors.New("磁盘写保护") }
