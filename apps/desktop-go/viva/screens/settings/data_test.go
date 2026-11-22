package settings_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// 「数据与备份」分区的 L4 测试：用 servicestest 的假服务驱动真实的页面，经无头会话点真实的控件。
// 三块（备份、完整备份仓库、配置迁移）共用这里的环境与点击辅助；仓库与配置迁移的用例在各自的文件里。

// dataEnv 是「数据与备份」页的测试环境：三个假服务加设置存储与提示、日志通道。
type dataEnv struct {
	*sysEnv
	store  *sscreen.MemStore
	data   *servicestest.Data
	backup *servicestest.Backup
	cfg    *servicestest.ConfigTransfer
}

func newDataEnv() *dataEnv {
	return &dataEnv{
		sysEnv: newEnv(), store: sscreen.NewMemStore(nsettings.Default()),
		data: &servicestest.Data{}, backup: &servicestest.Backup{}, cfg: &servicestest.ConfigTransfer{},
	}
}

// open 打开「数据与备份」页（页面自带服务；关闭时收尾后台工作）。
func (e *dataEnv) open(t *testing.T) *rig {
	t.Helper()
	return openPage(t, func(look *kit.Look) func(c ui.C) {
		d := &sscreen.Data{
			Look: look, Store: e.store, Guard: e.guard(), Runtime: e.data, Backup: e.backup, Transfer: e.cfg, Wake: func() {},
		}
		t.Cleanup(d.Close)
		return d.Page
	})
}

// secrets 是用例里出现过的口令：提示与日志里一个都不能有。
func (e *dataEnv) assertNoSecrets(t *testing.T, secrets ...string) {
	t.Helper()
	all := fmt.Sprint(e.notify.Queued(), e.logs.all())
	for _, s := range secrets {
		if strings.Contains(all, s) {
			t.Errorf("提示或日志里出现了口令 %q：%s", s, all)
		}
	}
}

// reveal 把页面滚到控件进入可见区（页面比窗口长）；控件已经可见、或页面已滚到头仍在原处时不动。
func (r *rig) reveal(role gfx.Role, name string) {
	r.t.Helper()
	last := float32(-1)
	for i := 0; i < 40; i++ {
		n, ok := r.node(role, name)
		dy := float32(260)
		if ok {
			y := n.Bounds.Y + n.Bounds.Height/2
			if y > 30 && y < 785 || y == last {
				return
			}
			last = y
			if y <= 30 {
				dy = -260
			}
		}
		if err := r.s.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: dy, Precise: true}); err != nil {
			r.t.Fatalf("滚动失败：%v", err)
		}
		r.frames(2)
	}
	r.t.Fatalf("滚不到 %v「%s」", role, name)
}

// press 滚到控件并点击它。
func (r *rig) press(role gfx.Role, name string) {
	r.t.Helper()
	r.reveal(role, name)
	r.clickNode(role, name)
}

// pressNth 点击同名控件里的第 n 个（从 0 起，按画面顺序）。
func (r *rig) pressNth(role gfx.Role, name string, n int) {
	r.t.Helper()
	r.reveal(role, name)
	r.clickNth(role, name, n)
}

func (r *rig) disabled(role gfx.Role, name string) bool {
	r.t.Helper()
	r.reveal(role, name)
	n, ok := r.node(role, name)
	if !ok {
		r.t.Fatalf("找不到 %v「%s」", role, name)
	}
	return n.Disabled
}

// dialogOpen 报告标题为 title 的对话框是否打开（标题与页面上的行标签可能同名，所以按对话框的角色找，不按文字找）。
func (r *rig) dialogOpen(title string) bool { return r.has(gfx.RoleGroup, title) }

// dialogDescription 取对话框的可访问描述（确认框的说明文字在这里，不作为独立文本出现）。
func (r *rig) dialogDescription(title string) string {
	r.t.Helper()
	n := r.last(gfx.RoleGroup, title)
	return n.Description
}

func legacyBackup(name string, kind string, size int64) services.BackupInfo {
	return services.BackupInfo{FileName: name, Kind: kind, SizeBytes: size, CreatedAt: 1767225600000, Format: "legacy-db", Validation: "valid"}
}

// ── 备份计划 ──

// 自动备份开关与间隔、保留份数写入设置；写入失败时界面退回原值并提示带原因的错误。
func TestDataPlanPersists(t *testing.T) {
	e := newDataEnv()
	r := e.open(t)
	auto := locales.SettingsBackupAuto()

	if !e.store.Values().App.BackupAutoEnabled {
		t.Fatal("定时自动备份默认应开启")
	}
	r.clickNode(gfx.RoleSwitch, auto)
	if e.store.Values().App.BackupAutoEnabled || r.switchChecked(auto) {
		t.Fatal("关闭「自动备份」后应写入设置")
	}
	r.pick(locales.SettingsBackupInterval(), locales.SettingsBackupInterval72h())
	r.pick(locales.SettingsBackupKeep(), "20")
	if app := e.store.Values().App; app.BackupIntervalHours != 72 || app.BackupKeepCount != 20 {
		t.Errorf("间隔与保留份数应为 72 与 20，得到 %d、%d", app.BackupIntervalHours, app.BackupKeepCount)
	}

	e.store.FailNext = errors.New("disk full")
	r.clickNode(gfx.RoleSwitch, auto)
	if e.store.Values().App.BackupAutoEnabled || r.switchChecked(auto) {
		t.Error("写入失败时开关应保持原状")
	}
	if errs := e.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "disk full") {
		t.Errorf("应弹一条带原因的错误提示，得到 %+v", errs)
	}
}

// ── 本地快照 ──

// 列表显示来源徽标、文件名与大小；「打开备份目录」「打开日志目录」交给系统文件管理器，目录取自运行时路径。
func TestDataListAndFolderButtons(t *testing.T) {
	e := newDataEnv()
	e.backup.SetLegacy([]services.BackupInfo{
		legacyBackup("knowledge-manual-1.db", services.BackupKindManual, 3*1024*1024),
		legacyBackup("knowledge-auto-2.db", services.BackupKindAuto, 900),
	})
	r := e.open(t)
	waitUntil(r, "快照列表出现", func() bool { return r.hasText("knowledge-manual-1.db") })
	if !r.hasText("3.0 MB") || !r.hasText("900 B") {
		t.Error("列表里应有文件大小（不足 1 KB 按字节显示）")
	}
	if !r.hasText(locales.SettingsBackupKindManual()) || !r.hasText(locales.SettingsBackupKindAuto()) {
		t.Error("列表里应有手动与自动两种来源徽标")
	}

	waitUntil(r, "打开目录按钮可用", func() bool {
		return !r.disabled(gfx.RoleButton, locales.SettingsOpenBackupsDir())
	})
	r.press(gfx.RoleButton, locales.SettingsOpenBackupsDir())
	r.press(gfx.RoleButton, locales.SettingsOpenLogs())
	waitUntil(r, "两次打开目录", func() bool { return len(e.data.CallsOf("OpenPath")) == 2 })
	calls := e.data.CallsOf("OpenPath")
	if calls[0].Args[0] != `C:\fake\userdata\backups` || calls[1].Args[0] != `C:\fake\userdata\logs` {
		t.Errorf("打开的目录不对：%v", calls)
	}
}

// 读取路径失败：两个「打开」按钮保持禁用（用户无从点击），原因只进日志，不弹吓人的错误。
func TestDataFolderButtonsStayDisabledWithoutPaths(t *testing.T) {
	e := newDataEnv()
	e.data.Fail("RuntimePaths", errors.New("no runtime"))
	r := e.open(t)
	waitUntil(r, "路径读取结束", func() bool { return len(e.data.CallsOf("RuntimePaths")) == 1 && len(e.logs.all()) == 1 })
	if !r.disabled(gfx.RoleButton, locales.SettingsOpenBackupsDir()) || !r.disabled(gfx.RoleButton, locales.SettingsOpenLogs()) {
		t.Error("读不到路径时「打开」按钮应禁用")
	}
	if errs := e.toasts(kit.ToastError); len(errs) != 0 {
		t.Errorf("读路径失败不应弹提示：%v", errs)
	}
}

// 没有快照是空态；读取列表失败是读取失败加重试，不能画成「还没有备份」。
func TestDataEmptyAndListLoadError(t *testing.T) {
	e := newDataEnv()
	r := e.open(t)
	waitUntil(r, "空态", func() bool { return r.hasText(locales.SettingsBackupEmpty()) })

	e2 := newDataEnv()
	e2.backup.Fail("List", errors.New("db busy"))
	r2 := e2.open(t)
	waitUntil(r2, "读取失败", func() bool { return r2.hasText("db busy") })
	if r2.hasText(locales.SettingsBackupEmpty()) {
		t.Error("读取失败不能渲染成空态")
	}
	if errs := e2.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "db busy") {
		t.Errorf("读取失败应弹一条带原因的提示，得到 %+v", errs)
	}
	// 重试：原因消除后列表出现。
	e2.backup.Fail("List", nil)
	e2.backup.SetLegacy([]services.BackupInfo{legacyBackup("after-retry.db", services.BackupKindManual, 2048)})
	r2.press(gfx.RoleButton, locales.CommonRetry())
	waitUntil(r2, "重试后出现列表", func() bool { return r2.hasText("after-retry.db") })
}

// 立即备份：成功弹提示并刷新列表；失败提示带原因，按钮恢复可点，再点一次即重试。
func TestDataCreateBackupSuccessAndRetry(t *testing.T) {
	e := newDataEnv()
	r := e.open(t)
	waitUntil(r, "空态", func() bool { return r.hasText(locales.SettingsBackupEmpty()) })

	e.backup.Fail("Create", errors.New("disk full"))
	r.press(gfx.RoleButton, locales.SettingsBackupNow())
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsBackupFailed() || !strings.Contains(errs[0].Detail, "disk full") {
		t.Errorf("失败提示应带标题与原因，得到 %+v", errs[0])
	}
	waitUntil(r, "按钮恢复可点", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsBackupNow()) })

	e.backup.Fail("Create", nil)
	r.press(gfx.RoleButton, locales.SettingsBackupNow())
	waitUntil(r, "新快照出现", func() bool { return r.hasText("knowledge-manual-") })
	if ok := e.toasts(kit.ToastSuccess); len(ok) != 1 || ok[0].Message != locales.SettingsBackupCreated() {
		t.Errorf("成功应弹一条成功提示，得到 %+v", ok)
	}
}

// 备份进行中：转圈且其余按钮禁用，不能并发再点。
func TestDataBusyDisablesOtherActions(t *testing.T) {
	e := newDataEnv()
	e.backup.SetLegacy([]services.BackupInfo{legacyBackup("a.db", services.BackupKindManual, 1024)})
	r := e.open(t)
	waitUntil(r, "列表出现", func() bool { return r.hasText("a.db") })

	release := e.backup.Hold("Create")
	defer release()
	r.press(gfx.RoleButton, locales.SettingsBackupNow())
	waitUntil(r, "进行中", func() bool { return len(e.backup.CallsOf("Create")) == 1 })
	for _, name := range []string{locales.SettingsBackupNow(), locales.SettingsBackupRestore(), locales.SettingsExportStart()} {
		if !r.disabled(gfx.RoleButton, name) {
			t.Errorf("备份进行中「%s」应禁用", name)
		}
	}
	release()
	waitUntil(r, "结束后恢复", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsBackupNow()) })
}

// 恢复：先确认；取消不调用服务；确认后失败提示原因并回到可操作；再确认成功才真正恢复（外壳随后重启）。
func TestDataRestoreConfirmCancelAndFailure(t *testing.T) {
	e := newDataEnv()
	e.backup.SetLegacy([]services.BackupInfo{legacyBackup("knowledge-manual-1.db", services.BackupKindManual, 4096)})
	r := e.open(t)
	waitUntil(r, "列表出现", func() bool { return r.hasText("knowledge-manual-1.db") })

	r.press(gfx.RoleButton, locales.SettingsBackupRestore())
	if !r.hasText(locales.SettingsBackupRestoreConfirmTitle()) {
		t.Fatal("点「恢复」应先弹确认框")
	}
	r.clickLast(gfx.RoleButton, locales.CommonCancel())
	if r.hasText(locales.SettingsBackupRestoreConfirmTitle()) || len(e.backup.CallsOf("Restore")) != 0 {
		t.Fatal("取消后确认框应关闭且不调用恢复")
	}

	e.backup.Fail("Restore", errors.New("db locked"))
	r.press(gfx.RoleButton, locales.SettingsBackupRestore())
	r.clickLast(gfx.RoleButton, locales.CommonConfirm())
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsBackupRestoreFailed() || !strings.Contains(errs[0].Detail, "db locked") {
		t.Errorf("恢复失败提示不对：%+v", errs[0])
	}
	waitUntil(r, "回到可操作", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsBackupRestore()) })

	e.backup.Fail("Restore", nil)
	r.press(gfx.RoleButton, locales.SettingsBackupRestore())
	r.clickLast(gfx.RoleButton, locales.CommonConfirm())
	waitUntil(r, "恢复被调用", func() bool { return len(e.backup.CallsOf("Restore")) == 2 })
	if got := e.backup.CallsOf("Restore")[1].Args[0]; got != "knowledge-manual-1.db" {
		t.Errorf("恢复的快照不对：%q", got)
	}
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	// 外壳随后重启：界面保持忙碌，不能在重启前再次触发。
	if !r.disabled(gfx.RoleButton, locales.SettingsBackupNow()) {
		t.Error("恢复成功后到重启前界面应保持忙碌")
	}
}

// 删除：确认后删除并刷新列表；失败提示原因；取消不动。
func TestDataDeleteConfirmCancelAndFailure(t *testing.T) {
	e := newDataEnv()
	e.backup.SetLegacy([]services.BackupInfo{legacyBackup("old.db", services.BackupKindAuto, 4096)})
	r := e.open(t)
	waitUntil(r, "列表出现", func() bool { return r.hasText("old.db") })

	r.press(gfx.RoleButton, locales.SettingsBackupDelete())
	r.clickLast(gfx.RoleButton, locales.CommonCancel())
	if len(e.backup.CallsOf("Delete")) != 0 {
		t.Fatal("取消删除不应调用服务")
	}

	e.backup.Fail("Delete", errors.New("file in use"))
	r.press(gfx.RoleButton, locales.SettingsBackupDelete())
	r.clickLast(gfx.RoleButton, locales.CommonConfirm())
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsBackupDeleteFailed() || !strings.Contains(errs[0].Detail, "file in use") {
		t.Errorf("删除失败提示不对：%+v", errs[0])
	}
	waitUntil(r, "列表仍在", func() bool { return r.hasText("old.db") && !r.disabled(gfx.RoleButton, locales.SettingsBackupNow()) })

	e.backup.Fail("Delete", nil)
	r.press(gfx.RoleButton, locales.SettingsBackupDelete())
	r.clickLast(gfx.RoleButton, locales.CommonConfirm())
	waitUntil(r, "列表清空", func() bool { return r.hasText(locales.SettingsBackupEmpty()) })
}

// ── Markdown 导出 ──

// 导出：成功提示条数并在文件管理器里打开目录；用户取消什么都不提示；失败提示原因。
func TestDataMarkdownExport(t *testing.T) {
	e := newDataEnv()
	e.backup.SetMarkdown(services.MarkdownExport{Count: 7, Dir: `C:\out\GuiZhi-Export`})
	r := e.open(t)

	r.press(gfx.RoleButton, locales.SettingsExportStart())
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if ok := e.toasts(kit.ToastSuccess); ok[0].Message != locales.SettingsExportDone(7) {
		t.Errorf("成功提示应带条数，得到 %q", ok[0].Message)
	}
	waitUntil(r, "打开导出目录", func() bool { return len(e.data.CallsOf("OpenPath")) == 1 })
	if got := e.data.CallsOf("OpenPath")[0].Args[0]; got != `C:\out\GuiZhi-Export` {
		t.Errorf("应打开导出目录，得到 %q", got)
	}

	e.backup.SetMarkdown(services.MarkdownExport{Canceled: true})
	r.press(gfx.RoleButton, locales.SettingsExportStart())
	waitUntil(r, "第二次调用结束", func() bool {
		return len(e.backup.CallsOf("ExportMarkdown")) == 2 && !r.disabled(gfx.RoleButton, locales.SettingsExportStart())
	})
	if len(e.toasts(kit.ToastSuccess)) != 1 || len(e.toasts(kit.ToastError)) != 0 {
		t.Error("用户取消不应有任何新提示")
	}

	e.backup.Fail("ExportMarkdown", errors.New("no permission"))
	r.press(gfx.RoleButton, locales.SettingsExportStart())
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsExportFailed() || !strings.Contains(errs[0].Detail, "no permission") {
		t.Errorf("导出失败提示不对：%+v", errs[0])
	}
}
