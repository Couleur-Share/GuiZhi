package overlays

import (
	"errors"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
)

// 旧版数据迁移对话框界面（L4）：确认页、迁移中（不可关闭）、完成页、失败页（重试）。

const legacyPath = `C:\legacy\knowledge.db`

// newMigrationUI 探测到旧库并打开确认页。
func newMigrationUI(t *testing.T) *uiRig {
	t.Helper()
	r := newUIRig(t, withMigration)
	r.migrate.SetDetection(services.LegacyDetection{Path: legacyPath, ItemCount: 42}, true)
	r.migrate.SetStats(services.LegacyMigrationStats{Items: 42, Collections: 3, Tags: 7, Sources: 5, WikiPages: 2})
	r.screen.Migration.Detect()
	r.frames(3)
	return r
}

func (r *uiRig) migrationOpen() bool { return r.has(gfx.RoleGroup, locales.MigrationTitle()) }

func TestUIMigrationClosedDrawsNothing(t *testing.T) {
	r := newUIRig(t, withMigration)
	if r.migrationOpen() {
		t.Fatal("没打开时不应画对话框")
	}
}

// 确认页：探测到的条目数、迁移范围、来源路径，「暂不迁移」与「立即迁移」。
func TestUIMigrationConfirmShowsDetectedLegacy(t *testing.T) {
	r := newMigrationUI(t)
	if !r.migrationOpen() || !r.has(gfx.RoleHeading, locales.MigrationTitle()) {
		t.Fatalf("应显示标题：%q", r.session.Texts())
	}
	for _, want := range []string{locales.MigrationDetected(42), locales.MigrationScope(), locales.NativeMigrationSourcePath(legacyPath)} {
		if !r.hasWrapped(want) {
			t.Errorf("缺少 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if !r.hasButton(locales.MigrationSkip()) || !r.hasButton(locales.MigrationStart()) {
		t.Errorf("应有暂不迁移与立即迁移：%q", r.session.Texts())
	}
}

// 暂不迁移：关闭并永久记下，下次不再提示。
func TestUIMigrationSkipClosesAndRemembers(t *testing.T) {
	r := newMigrationUI(t)
	r.clickButton(locales.MigrationSkip())
	if r.migrationOpen() || r.screen.Migration.IsOpen() {
		t.Fatal("应关闭")
	}
	if !prefTrue(r.prefs, settings.PrefMigrationDismissed) {
		t.Error("应记下「暂不迁移」")
	}
	if got := r.called(); !slices.Equal(got, []string{"closed"}) {
		t.Errorf("关闭应通知宿主：%v", got)
	}
}

// 确认页也能用 Esc、点遮罩、关闭钮关闭（等同暂不迁移）。
func TestUIMigrationConfirmCloseWays(t *testing.T) {
	cases := []struct {
		name string
		do   func(r *uiRig)
	}{
		{"Esc", func(r *uiRig) { r.keys("Escape") }},
		{"关闭钮", func(r *uiRig) { r.clickButton(locales.CommonClose()) }},
		{"点遮罩", func(r *uiRig) { r.clickAt(10, 10) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newMigrationUI(t)
			c.do(r)
			if r.migrationOpen() || r.screen.Migration.IsOpen() {
				t.Error("应关闭")
			}
		})
	}
}

// 迁移中：转圈加说明，没有按钮；Esc、点遮罩、关闭钮都不能关；放行后进入完成页。
func TestUIMigrationRunningCannotBeClosed(t *testing.T) {
	r := newMigrationUI(t)
	gate := make(chan struct{})
	r.migrate.RunGate = gate
	r.screen.Migration.Env = r.asyncEnv()
	r.clickButton(locales.MigrationStart())
	if !r.hasText(locales.MigrationRunning()) {
		t.Fatalf("应显示迁移中：%q", r.session.Texts())
	}
	if r.hasButton(locales.MigrationStart()) || r.hasButton(locales.MigrationSkip()) || r.hasButton(locales.CommonClose()) {
		t.Error("迁移中不应有任何按钮（含关闭钮）")
	}
	r.keys("Escape")
	r.clickAt(10, 10)
	if !r.migrationOpen() || r.screen.Migration.View().Phase != MigrationRunning {
		t.Fatal("迁移中 Esc 与点遮罩都不应关闭")
	}
	close(gate)
	r.wait()
	r.frames(3)
	if !r.hasText(locales.MigrationDone()) {
		t.Errorf("放行后应进入完成页：%q", r.session.Texts())
	}
}

// 完成页：各类数据数量与「开始使用」；点它关闭并通知宿主刷新。
func TestUIMigrationDoneShowsStatsAndFinishes(t *testing.T) {
	r := newMigrationUI(t)
	r.clickButton(locales.MigrationStart())
	if got := r.migrate.Runs(); !slices.Equal(got, []string{""}) { // 空串 = 用探测到的默认路径
		t.Errorf("没另选路径时应用探测到的默认旧库：%v", got)
	}
	if !r.hasText(locales.MigrationDone()) || !r.hasWrapped(locales.MigrationDoneStats(42, 3, 7, 5, 2)) {
		t.Fatalf("应显示完成与统计：%q", r.session.Texts())
	}
	if got := r.called(); !slices.Equal(got, []string{"migrated"}) {
		t.Errorf("迁移成功应通知宿主刷新：%v", got)
	}
	r.clickButton(locales.MigrationFinish())
	if r.migrationOpen() {
		t.Fatal("开始使用应关闭对话框")
	}
	if got := r.called(); !slices.Equal(got, []string{"migrated", "closed"}) {
		t.Errorf("关闭应通知宿主：%v", got)
	}
	if prefTrue(r.prefs, settings.PrefMigrationDismissed) {
		t.Error("迁移完成不是「暂不迁移」，不应写入已拒绝记录")
	}
}

// 失败页：原因原文；「重试」再迁移一次，成功后进入完成页；「关闭」收起。
func TestUIMigrationFailedShowsReasonAndRetries(t *testing.T) {
	r := newMigrationUI(t)
	r.migrate.FailRun(errors.New("schema version 3 is not supported"))
	r.clickButton(locales.MigrationStart())
	if !r.hasText(locales.MigrationFailed()) || !r.hasWrapped("schema version 3 is not supported") {
		t.Fatalf("应显示失败与原因：%q", r.session.Texts())
	}
	if !r.hasButton(locales.ImportsRetry()) || !r.hasButton(locales.CommonClose()) {
		t.Fatalf("应有重试与关闭：%q", r.session.Texts())
	}
	if got := r.called(); len(got) != 0 {
		t.Errorf("失败不应通知宿主刷新：%v", got)
	}
	r.migrate.FailRun(nil)
	r.clickButton(locales.ImportsRetry())
	if !r.hasText(locales.MigrationDone()) || len(r.migrate.Runs()) != 2 {
		t.Errorf("重试应再迁移一次并成功：runs=%d %q", len(r.migrate.Runs()), r.session.Texts())
	}
}

func TestUIMigrationFailedCloseDismissesWithoutRemembering(t *testing.T) {
	r := newMigrationUI(t)
	r.migrate.FailRun(errors.New("disk is full"))
	r.clickButton(locales.MigrationStart())
	r.clickButton(locales.CommonClose())
	if r.migrationOpen() {
		t.Fatal("应关闭")
	}
	if prefTrue(r.prefs, settings.PrefMigrationDismissed) {
		t.Error("失败后关闭不应记为「暂不迁移」：下次启动应能再次提示")
	}
}
