package settings_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 完整备份仓库块的 L4 测试：启用校验、立即备份、预检并恢复、删除、便携备份与改口令。
// 每个用例都核对：口令确实交给了服务，又没有出现在提示与日志里。

const (
	recoveryPW = "recovery-pass-123" // 17 位，达到 12 位的门槛
	oldPW      = "old-recovery-pw-1"
	newPW      = "new-recovery-pw-2"
)

// fill 滚到文本框并输入。
func (r *rig) fill(name, text string) {
	r.t.Helper()
	r.reveal(gfx.RoleTextField, name)
	r.typeInto(name, text)
}

func repoSnap(id string, items int) services.BackupInfo {
	return services.BackupInfo{
		FileName: id, Kind: "manual", SizeBytes: 4 << 20, CreatedAt: 1767225600000, Format: "repository-snapshot", Encrypted: true, Validation: "valid",
		Summary: &services.BackupSummary{ItemCount: items, AssetCount: 5, ConfigDomains: []string{"ai", "shortcuts"}, SchemaVersion: 33, AppVersion: "0.0.0"},
	}
}

// readyEnv 是已启用完整备份、有两份快照的环境。
func readyEnv() *dataEnv {
	e := newDataEnv()
	e.backup.SetStatus(services.RepositoryStatus{Initialized: true, AutomaticAccessAvailable: true})
	e.backup.SetSnapshots([]services.BackupInfo{repoSnap("snap-a", 12), repoSnap("snap-b", 30)})
	return e
}

// 未启用：口令不足 12 位或两次不一致时启用钮禁用并就地提示；合格后启用，口令框消失，进入已启用界面。
func TestRepoSetupValidationAndEnable(t *testing.T) {
	e := newDataEnv()
	r := e.open(t)
	enable := locales.SettingsFullBackupEnable()
	first, again := locales.SettingsFullBackupSetPassword(), locales.SettingsFullBackupConfirmPassword()
	waitUntil(r, "启用表单出现", func() bool { return r.has(gfx.RoleButton, enable) })

	if !r.disabled(gfx.RoleButton, enable) {
		t.Fatal("什么都没填时启用钮应禁用")
	}
	r.fill(first, "short")
	r.fill(again, "short")
	if !r.disabled(gfx.RoleButton, enable) {
		t.Error("口令不足 12 位时启用钮应禁用")
	}
	r.fill(first, recoveryPW)
	r.fill(again, recoveryPW+"x")
	if !r.hasText(locales.SettingsFullBackupPasswordMismatch()) || !r.disabled(gfx.RoleButton, enable) {
		t.Error("两次不一致应就地提示并禁用启用钮")
	}
	if n, _ := r.node(gfx.RoleTextField, first); !n.Secure || strings.Contains(n.Value, recoveryPW) {
		t.Errorf("口令框应遮蔽内容：Secure=%v Value=%q", n.Secure, n.Value)
	}

	// 失败：提示带原因，口令框保留，用户改一下就能重试。
	e.backup.Fail("InitRepository", errors.New("keyring unavailable"))
	r.fill(again, recoveryPW)
	r.press(gfx.RoleButton, enable)
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsFullBackupInitFailed() || !strings.Contains(errs[0].Detail, "keyring unavailable") {
		t.Errorf("启用失败提示不对：%+v", errs[0])
	}
	waitUntil(r, "启用钮恢复", func() bool { return !r.disabled(gfx.RoleButton, enable) })

	e.backup.Fail("InitRepository", nil)
	r.press(gfx.RoleButton, enable)
	waitUntil(r, "进入已启用界面", func() bool { return r.has(gfx.RoleButton, locales.SettingsFullBackupNow()) })
	if calls := e.backup.CallsOf("InitRepository"); len(calls) != 2 || calls[1].Args[0] != recoveryPW {
		t.Errorf("口令应交给服务：%v", calls)
	}
	if ok := e.toasts(kit.ToastSuccess); len(ok) != 1 || ok[0].Message != locales.SettingsFullBackupEnabled() {
		t.Errorf("启用成功应弹成功提示，得到 %+v", ok)
	}
	if !r.hasText(locales.SettingsFullBackupKeyringReady("secure storage")) {
		t.Error("已启用界面应显示密钥环状态")
	}
	e.assertNoSecrets(t, recoveryPW)
}

// 已启用：列表显示摘要；立即备份把恢复口令交给服务，成功弹提示并刷新列表，失败提示原因可重试。
func TestRepoCreateSnapshot(t *testing.T) {
	e := readyEnv()
	r := e.open(t)
	now := locales.SettingsFullBackupNow()
	waitUntil(r, "快照列表出现", func() bool {
		return r.hasText(locales.SettingsFullBackupSnapshotSummary(12, 5, "4.0 MB", 33))
	})
	if !r.hasText(locales.SettingsFullBackupSnapshotSummary(30, 5, "4.0 MB", 33)) {
		t.Error("第二份快照的摘要也应显示")
	}

	r.fill(locales.SettingsFullBackupUnlockPlaceholder(), recoveryPW)
	e.backup.Fail("CreateSnapshot", errors.New("object store full"))
	r.press(gfx.RoleButton, now)
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsFullBackupFailed() || !strings.Contains(errs[0].Detail, "object store full") {
		t.Errorf("备份失败提示不对：%+v", errs[0])
	}
	waitUntil(r, "按钮恢复", func() bool { return !r.disabled(gfx.RoleButton, now) })

	e.backup.Fail("CreateSnapshot", nil)
	r.press(gfx.RoleButton, now)
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if ok := e.toasts(kit.ToastSuccess); ok[0].Message != locales.SettingsFullBackupCreated(5, 2) {
		t.Errorf("成功提示应带对象数：%q", ok[0].Message)
	}
	waitUntil(r, "列表刷新", func() bool { return len(e.backup.CallsOf("ListSnapshots")) >= 2 })
	if calls := e.backup.CallsOf("CreateSnapshot"); len(calls) != 2 || calls[1].Args[0] != recoveryPW {
		t.Errorf("恢复口令应交给服务：%v", calls)
	}
	e.assertNoSecrets(t, recoveryPW)
}

// 预检并恢复：预检失败把缺失与损坏的文件逐行列在详情里，不弹确认框；通过后确认；取消不恢复；失败提示原因。
func TestRepoPreviewAndRestore(t *testing.T) {
	e := readyEnv()
	r := e.open(t)
	restore := locales.SettingsFullBackupRestoreAction()
	waitUntil(r, "快照列表出现", func() bool { return r.has(gfx.RoleButton, restore) })
	r.fill(locales.SettingsFullBackupUnlockPlaceholder(), recoveryPW)

	e.backup.Fail("PreviewRestore", &services.RestorePreviewError{Message: "快照不完整", MissingFiles: []string{"a.obj"}, DamagedFiles: []string{"b.obj"}})
	r.press(gfx.RoleButton, restore)
	waitUntil(r, "预检失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	errs := e.toasts(kit.ToastError)
	if errs[0].Message != "快照不完整" || !strings.Contains(errs[0].Detail, locales.SettingsFullBackupMissingFile("a.obj")) ||
		!strings.Contains(errs[0].Detail, locales.SettingsFullBackupDamagedFile("b.obj")) {
		t.Errorf("预检失败应列出缺失与损坏的文件：%+v", errs[0])
	}
	if r.hasText(locales.SettingsFullBackupRestoreConfirmTitle()) {
		t.Fatal("预检失败不应弹恢复确认框")
	}
	waitUntil(r, "按钮恢复", func() bool { return !r.disabled(gfx.RoleButton, restore) })

	// 预检通过：确认框带摘要；取消不恢复。
	e.backup.Fail("PreviewRestore", nil)
	r.press(gfx.RoleButton, restore)
	waitUntil(r, "确认框", func() bool { return r.hasText(locales.SettingsFullBackupRestoreConfirmTitle()) })
	if got := r.dialogDescription(locales.SettingsFullBackupRestoreConfirmTitle()); got != locales.SettingsFullBackupRestoreConfirmMessage(12, 5, 2) {
		t.Errorf("确认框应写明条目、附件与配置域数量，得到 %q", got)
	}
	r.clickLast(gfx.RoleButton, locales.CommonCancel())
	if r.hasText(locales.SettingsFullBackupRestoreConfirmTitle()) || len(e.backup.CallsOf("RestoreSnapshot")) != 0 {
		t.Fatal("取消后应关闭确认框且不恢复")
	}

	// 确认后失败：提示原因，确认框保留可重试。
	e.backup.Fail("RestoreSnapshot", errors.New("wrong recovery password"))
	r.press(gfx.RoleButton, restore)
	waitUntil(r, "确认框", func() bool { return r.hasText(locales.SettingsFullBackupRestoreConfirmTitle()) })
	r.clickLast(gfx.RoleButton, locales.SettingsFullBackupRestoreAndRestart())
	waitUntil(r, "恢复失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 2 })
	if errs := e.toasts(kit.ToastError); errs[1].Message != locales.SettingsBackupRestoreFailed() || !strings.Contains(errs[1].Detail, "wrong recovery password") {
		t.Errorf("恢复失败提示不对：%+v", errs[1])
	}

	e.backup.Fail("RestoreSnapshot", nil)
	waitUntil(r, "确认钮恢复", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsFullBackupRestoreAndRestart()) })
	r.clickLast(gfx.RoleButton, locales.SettingsFullBackupRestoreAndRestart())
	waitUntil(r, "恢复被调用", func() bool { return len(e.backup.CallsOf("RestoreSnapshot")) == 2 })
	if got := e.backup.CallsOf("RestoreSnapshot")[1].Args; got[0] != "snap-a" || got[1] != recoveryPW {
		t.Errorf("恢复的快照与口令不对：%v", got)
	}
	e.assertNoSecrets(t, recoveryPW)
}

// 删除快照：确认后才删；失败提示原因；成功后列表少一项。
func TestRepoDeleteSnapshot(t *testing.T) {
	e := readyEnv()
	r := e.open(t)
	del := locales.SettingsFullBackupDeleteSnapshot()
	waitUntil(r, "快照列表出现", func() bool { return r.has(gfx.RoleButton, del) })

	r.press(gfx.RoleButton, del)
	r.clickLast(gfx.RoleButton, locales.CommonCancel())
	if len(e.backup.CallsOf("DeleteSnapshot")) != 0 {
		t.Fatal("取消不应删除")
	}

	e.backup.Fail("DeleteSnapshot", errors.New("object locked"))
	r.press(gfx.RoleButton, del)
	r.clickLast(gfx.RoleButton, locales.CommonDelete())
	waitUntil(r, "删除失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsBackupDeleteFailed() || !strings.Contains(errs[0].Detail, "object locked") {
		t.Errorf("删除失败提示不对：%+v", errs[0])
	}

	e.backup.Fail("DeleteSnapshot", nil)
	r.press(gfx.RoleButton, del)
	r.clickLast(gfx.RoleButton, locales.CommonDelete())
	waitUntil(r, "删除成功", func() bool { return len(e.backup.Snapshots()) == 1 })
	if got := e.backup.CallsOf("DeleteSnapshot"); got[len(got)-1].Args[0] != "snap-a" {
		t.Errorf("删除的快照不对：%v", got)
	}
}

// 便携备份：导出把恢复口令交给服务，取消不提示；导入没有口令时只提示不调用服务，有口令才导入，替换仓库时详情里说明。
func TestRepoPortableExportAndImport(t *testing.T) {
	e := readyEnv()
	r := e.open(t)
	export := locales.SettingsFullBackupExportPortable()
	importBtn := locales.SettingsFullBackupImportAction()
	waitUntil(r, "快照列表出现", func() bool { return r.has(gfx.RoleButton, export) })

	e.backup.SetPortableExport(services.PortableExport{Canceled: true})
	r.fill(locales.SettingsFullBackupUnlockPlaceholder(), recoveryPW)
	r.press(gfx.RoleButton, export)
	waitUntil(r, "导出调用结束", func() bool {
		return len(e.backup.CallsOf("ExportPortable")) == 1 && !r.disabled(gfx.RoleButton, export)
	})
	if len(e.toasts(kit.ToastSuccess)) != 0 {
		t.Error("取消导出不应提示")
	}
	e.backup.SetPortableExport(services.PortableExport{FilePath: `C:\out\a.guizhi-backup`})
	r.press(gfx.RoleButton, export)
	waitUntil(r, "导出成功", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if got := e.backup.CallsOf("ExportPortable")[1].Args; got[0] != "snap-a" || got[1] != recoveryPW {
		t.Errorf("导出的快照与口令不对：%v", got)
	}

	// 导入：口令为空时只提示。
	r.fill(locales.SettingsFullBackupUnlockPlaceholder(), "")
	r.press(gfx.RoleButton, importBtn)
	if errs := e.toasts(kit.ToastError); len(errs) != 1 || errs[0].Message != locales.SettingsFullBackupImportNeedPassword() || errs[0].Detail != "" {
		t.Fatalf("没有口令应提示先输入口令，得到 %+v", errs)
	}
	if len(e.backup.CallsOf("ImportPortable")) != 0 {
		t.Fatal("没有口令不应调用导入")
	}

	e.backup.SetPortableImport(services.PortableImport{ReplacedRepository: true})
	r.fill(locales.SettingsFullBackupUnlockPlaceholder(), recoveryPW)
	r.press(gfx.RoleButton, importBtn)
	waitUntil(r, "导入成功", func() bool { return len(e.toasts(kit.ToastSuccess)) == 2 })
	ok := e.toasts(kit.ToastSuccess)[1]
	if ok.Message != locales.SettingsFullBackupImported() || ok.Detail != locales.SettingsFullBackupImportReplacedDetail() {
		t.Errorf("替换仓库的导入应带说明：%+v", ok)
	}

	e.backup.Fail("ImportPortable", errors.New("bad archive"))
	r.press(gfx.RoleButton, importBtn)
	waitUntil(r, "导入失败", func() bool { return len(e.toasts(kit.ToastError)) == 2 })
	if errs := e.toasts(kit.ToastError); errs[1].Message != locales.SettingsFullBackupImportFailed() || !strings.Contains(errs[1].Detail, "bad archive") {
		t.Errorf("导入失败提示不对：%+v", errs[1])
	}
	e.assertNoSecrets(t, recoveryPW)
}

// 修改恢复口令：折叠展开；当前口令不对时提示原因、口令框保留；成功后清空两个口令框（口令框遮蔽内容，只能经交给服务的参数观察）。
func TestRepoChangePassword(t *testing.T) {
	e := readyEnv()
	e.backup.SetPassword(oldPW)
	r := e.open(t)
	cur, next := locales.SettingsFullBackupCurrentPassword(), locales.SettingsFullBackupNewPassword()
	waitUntil(r, "折叠行出现", func() bool { return r.has(gfx.RoleButton, locales.SettingsFullBackupChangePassword()) })
	if r.has(gfx.RoleTextField, cur) {
		t.Fatal("折叠时不应有口令框")
	}
	r.press(gfx.RoleButton, locales.SettingsFullBackupChangePassword())

	r.fill(cur, "not-the-password")
	r.fill(next, newPW)
	r.press(gfx.RoleButton, locales.SettingsFullBackupConfirmChange())
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsFullBackupPasswordChangeFailed() || errs[0].Detail == "" {
		t.Errorf("改口令失败应带原因：%+v", errs[0])
	}
	// 失败时口令框保留：只改当前口令再点一次，新口令仍是刚才输入的那个。
	r.fill(cur, oldPW)
	r.press(gfx.RoleButton, locales.SettingsFullBackupConfirmChange())
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if got := e.backup.CallsOf("ChangePassword"); len(got) != 2 || got[1].Args[0] != oldPW || got[1].Args[1] != newPW {
		t.Errorf("口令应交给服务，失败后新口令应保留：%v", got)
	}
	// 成功后两个口令框都被清空：再点一次交给服务的是两个空串。
	waitUntil(r, "按钮恢复", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsFullBackupConfirmChange()) })
	r.press(gfx.RoleButton, locales.SettingsFullBackupConfirmChange())
	waitUntil(r, "第三次调用", func() bool { return len(e.backup.CallsOf("ChangePassword")) == 3 })
	if got := e.backup.CallsOf("ChangePassword")[2].Args; got[0] != "" || got[1] != "" {
		t.Errorf("成功后口令框应清空，却交给了服务 %q", got)
	}
	e.assertNoSecrets(t, oldPW, newPW, "not-the-password")
}

// 读取仓库状态失败：整块画读取失败与重试（不能一直转圈或画成未启用）；重试成功后进入对应界面。
func TestRepoStatusLoadError(t *testing.T) {
	e := newDataEnv()
	e.backup.Fail("RepositoryStatus", errors.New("repo locked"))
	r := e.open(t)
	waitUntil(r, "读取失败", func() bool { return r.hasText("repo locked") })
	if r.has(gfx.RoleButton, locales.SettingsFullBackupEnable()) {
		t.Error("读取失败不能画成未启用")
	}
	if errs := e.toasts(kit.ToastError); len(errs) != 1 || errs[0].Message != locales.SettingsFullBackupLoadFailed() || !strings.Contains(errs[0].Detail, "repo locked") {
		t.Errorf("读取失败应弹带原因的提示：%+v", errs)
	}
	e.backup.Fail("RepositoryStatus", nil)
	e.backup.SetStatus(services.RepositoryStatus{Initialized: true, AutomaticAccessAvailable: false, Warning: "no keyring"})
	r.press(gfx.RoleButton, locales.CommonRetry())
	waitUntil(r, "进入已启用界面", func() bool { return r.hasText("no keyring") })
	_ = fmt.Sprint
}
