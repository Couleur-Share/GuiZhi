package overlays

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

type migrationRig struct {
	*rig
	m     *Migration
	svc   *servicestest.Migration
	prefs *servicestest.Prefs
	// 钩子计数
	migrated, closed int
}

func newMigrationRig(t *testing.T) *migrationRig {
	t.Helper()
	mr := &migrationRig{rig: newRig(t), svc: &servicestest.Migration{}, prefs: &servicestest.Prefs{}}
	m := NewMigration(mr.guard, mr.invalidate)
	m.Env = mr.env()
	m.Service, m.Prefs = mr.svc, mr.prefs
	m.OnMigrated = func() { mr.migrated++ }
	m.OnClose = func() { mr.closed++ }
	mr.m = m
	return mr
}

func (mr *migrationRig) legacyPresent() {
	mr.svc.SetDetection(services.LegacyDetection{Path: `C:\legacy\knowledge.db`, ItemCount: 42}, true)
}

func TestMigrationDetectOpensConfirm(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.m.Detect()
	v := mr.m.View()
	if !v.Open || v.Phase != MigrationConfirm || v.ItemCount != 42 || v.Path != `C:\legacy\knowledge.db` {
		t.Fatalf("应打开确认页：%+v", v)
	}
	if !mr.m.IsOpen() {
		t.Error("IsOpen 应为真")
	}
}

func TestMigrationDetectNothingFoundStaysClosed(t *testing.T) {
	mr := newMigrationRig(t)
	mr.m.Detect()
	if mr.m.IsOpen() || mr.svc.Detects() != 1 {
		t.Errorf("没有旧库不应弹出：open=%v detects=%d", mr.m.IsOpen(), mr.svc.Detects())
	}
}

func TestMigrationDetectSkippedWhenDismissed(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.prefs.Put(settings.PrefMigrationDismissed, "true")
	mr.m.Detect()
	if mr.m.IsOpen() || mr.svc.Detects() != 0 {
		t.Errorf("选过「暂不迁移」不应再探测或弹出：detects=%d", mr.svc.Detects())
	}
}

func TestMigrationDetectFailureIsLoggedNotShown(t *testing.T) {
	mr := newMigrationRig(t)
	mr.svc.FailDetect(errors.New("legacy file is locked"))
	mr.m.Detect()
	if mr.m.IsOpen() || len(mr.toasts()) != 0 {
		t.Error("探测失败不应打扰用户")
	}
	if logs := mr.log.entries(); len(logs) != 1 || !strings.HasPrefix(logs[0], "migration.detect|") || !strings.Contains(logs[0], "legacy file is locked") {
		t.Errorf("探测失败必须留日志：%v", logs)
	}
}

func TestMigrationNilServiceIsNoop(t *testing.T) {
	r := newRig(t)
	m := NewMigration(r.guard, r.invalidate)
	m.Env = r.env()
	m.Detect()
	if m.IsOpen() {
		t.Error("没接服务不应弹出")
	}
	m.Run() // 对话框没打开：无效
}

func TestMigrationRunSuccessShowsStatsAndNotifies(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.svc.SetStats(services.LegacyMigrationStats{Items: 42, Collections: 3, Tags: 5, Sources: 40, WikiPages: 7})
	mr.m.Detect()
	mr.m.Run()
	v := mr.m.View()
	if v.Phase != MigrationDone || v.Stats.Items != 42 || v.Stats.WikiPages != 7 || v.Error != "" {
		t.Fatalf("应进入完成页：%+v", v)
	}
	if mr.migrated != 1 {
		t.Errorf("迁移成功应通知刷新视图：%d", mr.migrated)
	}
	if runs := mr.svc.Runs(); !slices.Equal(runs, []string{""}) {
		t.Errorf("默认路径应传空串：%v", runs)
	}
	if len(mr.toasts()) != 0 {
		t.Error("完成页已有结果，不另弹提示")
	}
}

func TestMigrationCustomSourcePath(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.m.Detect()
	mr.m.SetSourcePath(`D:\old\knowledge.db`)
	if mr.m.View().SourcePath != `D:\old\knowledge.db` {
		t.Fatal("应记下另选的路径")
	}
	mr.m.Run()
	if runs := mr.svc.Runs(); !slices.Equal(runs, []string{`D:\old\knowledge.db`}) {
		t.Errorf("应使用另选的路径：%v", runs)
	}
	mr.m.SetSourcePath("ignored") // 完成页不可改
	if mr.m.View().SourcePath != `D:\old\knowledge.db` {
		t.Error("完成页不应再改路径")
	}
}

func TestMigrationFailureShowsReasonAndRetries(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.svc.FailRun(errors.New("schema version 3 is not supported"))
	mr.m.Detect()
	mr.m.Run()
	v := mr.m.View()
	if v.Phase != MigrationFailed || v.Error != "schema version 3 is not supported" {
		t.Fatalf("应进入失败页并带原因：%+v", v)
	}
	if mr.migrated != 0 {
		t.Error("失败不应通知刷新")
	}
	if logs := mr.log.entries(); len(logs) != 1 || !strings.HasPrefix(logs[0], "migration.run|") {
		t.Errorf("应留日志：%v", logs)
	}

	mr.svc.FailRun(nil)
	mr.m.Run() // 重试
	if v := mr.m.View(); v.Phase != MigrationDone || v.Error != "" {
		t.Errorf("重试成功应进入完成页并清掉错误：%+v", v)
	}
	if len(mr.svc.Runs()) != 2 || mr.migrated != 1 {
		t.Errorf("应迁移两次、通知一次：runs=%d migrated=%d", len(mr.svc.Runs()), mr.migrated)
	}
}

func TestMigrationRunningIgnoresCloseAndRerun(t *testing.T) {
	mr := newMigrationRig(t)
	mr.m.Env = mr.asyncEnv()
	mr.legacyPresent()
	mr.svc.RunGate = make(chan struct{})
	mr.m.Detect()
	mr.wait()
	mr.m.Run()
	if mr.m.View().Phase != MigrationRunning {
		t.Fatal("应进入迁移中")
	}
	mr.m.Close()
	mr.m.Run()
	if !mr.m.IsOpen() || mr.closed != 0 {
		t.Error("迁移中关闭应被忽略")
	}
	close(mr.svc.RunGate)
	mr.wait()
	if n := len(mr.svc.Runs()); n != 1 {
		t.Errorf("迁移中重复触发不应再迁移：%d", n)
	}
	if mr.m.View().Phase != MigrationDone {
		t.Error("应完成")
	}
}

func TestMigrationSkipPersistsDismissal(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.m.Detect()
	mr.m.Close() // 确认页关闭 = 暂不迁移
	if mr.m.IsOpen() || mr.closed != 1 {
		t.Errorf("应关闭并通知：closed=%d", mr.closed)
	}
	if !prefTrue(mr.prefs, settings.PrefMigrationDismissed) {
		t.Error("暂不迁移应记住选择")
	}
}

func TestMigrationSkipPersistFailureStillClosesAndReports(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.prefs.FailSetWith(errors.New("prefs file is read-only"))
	mr.m.Detect()
	mr.m.Close()
	if mr.m.IsOpen() {
		t.Error("记录写不进去也要关闭")
	}
	if tt := mr.lastToast(); tt.Kind != kit.ToastError || !strings.Contains(tt.Message, locales.MigrationSkip()) || !strings.Contains(tt.Detail, "read-only") {
		t.Errorf("应如实提示：%+v", tt)
	}
}

func TestMigrationCloseAfterFailureOrDoneDoesNotPersist(t *testing.T) {
	// 失败页关闭：下次启动仍会提示
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.svc.FailRun(errors.New("disk read error"))
	mr.m.Detect()
	mr.m.Run()
	mr.m.Close()
	if prefTrue(mr.prefs, settings.PrefMigrationDismissed) || mr.closed != 1 {
		t.Error("失败页关闭不应记住「暂不迁移」")
	}

	// 完成页关闭：不写偏好（探测本身已不再发现旧库）
	mr2 := newMigrationRig(t)
	mr2.legacyPresent()
	mr2.m.Detect()
	mr2.m.Run()
	mr2.m.Close()
	if len(mr2.prefs.SetKeys()) != 0 || mr2.closed != 1 {
		t.Errorf("完成页关闭不应写偏好：%v", mr2.prefs.SetKeys())
	}
	mr2.m.Detect() // 迁移后不再发现
	if mr2.m.IsOpen() {
		t.Error("迁移成功后重新探测不应再弹出")
	}
}

func TestMigrationCloseWhenNotOpenDoesNothing(t *testing.T) {
	mr := newMigrationRig(t)
	mr.m.Close()
	if mr.closed != 0 || len(mr.prefs.SetKeys()) != 0 {
		t.Error("没打开时关闭应无事发生")
	}
}

func TestMigrationDetectDoesNotInterruptOpenDialog(t *testing.T) {
	mr := newMigrationRig(t)
	mr.legacyPresent()
	mr.m.Detect()
	mr.m.SetSourcePath("custom.db")
	mr.m.Detect()
	if mr.m.View().SourcePath != "custom.db" {
		t.Error("再次探测不应重置已打开的对话框")
	}
}
