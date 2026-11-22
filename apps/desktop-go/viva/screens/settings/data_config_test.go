package settings_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 配置迁移块的 L4 测试：导出对话框的校验与结果、导入的预览、密码就地提示、选择项、失败提示与成功后重启。
// 导出密码与导入密码都经假服务的调用记录核对「确实交给了服务」，同时核对没有出现在提示与日志里。

const (
	exportPW = "export-pass-1"
	importPW = "right-import-pw"
)

// waitFor 同 waitUntil，但等得更久（用于带固定延迟的动作，如导入成功后 1.5 秒才重启）。
func waitFor(r *rig, what string, limit time.Duration, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		r.frames(1)
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatalf("等待超时：%s", what)
}

func openExportDialog(r *rig) {
	r.t.Helper()
	r.press(gfx.RoleButton, locales.SettingsConfigExportStart())
	if !r.dialogOpen(locales.SettingsConfigExportTitle()) {
		r.t.Fatal("点「导出到文件」应打开导出对话框")
	}
}

// 导出对话框：带 API Key 时密码至少 6 位且两次一致才能导出；不带 API Key 不要密码。
func TestConfigExportDialogValidation(t *testing.T) {
	e := newDataEnv()
	r := e.open(t)
	confirm := locales.SettingsConfigExportConfirm()
	pw, again := locales.SettingsConfigExportPassword(), locales.SettingsConfigExportPasswordConfirm()
	openExportDialog(r)

	if !r.has(gfx.RoleTextField, pw) || !r.disabled(gfx.RoleButton, confirm) {
		t.Fatal("默认带 API Key：应有密码框，且没填密码时导出钮禁用")
	}
	r.typeInto(pw, "abc")
	if !r.hasText(locales.SettingsConfigExportPasswordTooShort(6)) || !r.disabled(gfx.RoleButton, confirm) {
		t.Error("密码不足 6 位应就地提示并禁用导出钮")
	}
	r.typeInto(pw, exportPW)
	r.typeInto(again, exportPW+"x")
	if !r.hasText(locales.SettingsConfigExportPasswordMismatch()) || !r.disabled(gfx.RoleButton, confirm) {
		t.Error("两次不一致应就地提示并禁用导出钮")
	}
	if n, _ := r.node(gfx.RoleTextField, pw); !n.Secure {
		t.Error("导出密码框应遮蔽内容")
	}
	r.typeInto(again, exportPW)
	if r.disabled(gfx.RoleButton, confirm) {
		t.Error("密码合格且一致后导出钮应可用")
	}

	// 关掉「包含 API Key」：密码框消失，不需要密码也能导出。
	r.clickNode(gfx.RoleSwitch, locales.SettingsConfigExportIncludeKeys())
	if r.has(gfx.RoleTextField, pw) || r.disabled(gfx.RoleButton, confirm) {
		t.Error("不含 API Key 时不应有密码框，且导出钮可用")
	}

	// 取消：对话框关闭，不导出。
	r.clickLast(gfx.RoleButton, locales.CommonCancel())
	if r.dialogOpen(locales.SettingsConfigExportTitle()) || len(e.cfg.Exports()) != 0 {
		t.Error("取消应关闭对话框且不导出")
	}
	// 重新打开：表单复位（带 API Key，密码清空）。
	openExportDialog(r)
	if !r.switchChecked(locales.SettingsConfigExportIncludeKeys()) || !r.disabled(gfx.RoleButton, confirm) {
		t.Error("重新打开应复位：带 API Key，且密码为空时导出钮禁用")
	}
}

// 导出：选项与密码交给服务；成功后提示、在文件管理器里定位文件并关闭对话框；取消保留对话框；失败提示原因、对话框保留可重试。
func TestConfigExportResults(t *testing.T) {
	e := newDataEnv()
	e.cfg.SetExport(services.ConfigExportResult{FilePath: `C:\out\GuiZhi-Config.json`})
	r := e.open(t)
	confirm := locales.SettingsConfigExportConfirm()
	openExportDialog(r)
	r.typeInto(locales.SettingsConfigExportPassword(), exportPW)
	r.typeInto(locales.SettingsConfigExportPasswordConfirm(), exportPW)
	r.clickNode(gfx.RoleSwitch, locales.SettingsConfigExportMcpScope())

	// 失败：提示带原因，对话框保留。
	e.cfg.Fail("Export", errors.New("disk full"))
	r.clickNode(gfx.RoleButton, confirm)
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsConfigExportFailed() || !strings.Contains(errs[0].Detail, "disk full") {
		t.Errorf("导出失败提示不对：%+v", errs[0])
	}
	if !r.dialogOpen(locales.SettingsConfigExportTitle()) {
		t.Fatal("失败后对话框应保留")
	}
	waitUntil(r, "导出钮恢复", func() bool { return !r.disabled(gfx.RoleButton, confirm) })

	// 取消选位置：对话框保留，没有提示。
	e.cfg.Fail("Export", nil)
	e.cfg.SetExport(services.ConfigExportResult{Canceled: true})
	r.clickNode(gfx.RoleButton, confirm)
	waitUntil(r, "取消调用结束", func() bool { return len(e.cfg.Exports()) == 2 && !r.disabled(gfx.RoleButton, confirm) })
	if !r.dialogOpen(locales.SettingsConfigExportTitle()) || len(e.toasts(kit.ToastSuccess)) != 0 {
		t.Error("用户取消选位置：对话框应保留且没有成功提示")
	}

	// 成功：提示、定位文件、关闭对话框。
	e.cfg.SetExport(services.ConfigExportResult{FilePath: `C:\out\GuiZhi-Config.json`})
	r.clickNode(gfx.RoleButton, confirm)
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	waitUntil(r, "对话框关闭", func() bool { return !r.dialogOpen(locales.SettingsConfigExportTitle()) })
	if ok := e.toasts(kit.ToastSuccess); ok[0].Message != locales.SettingsConfigExportDone() {
		t.Errorf("成功提示不对：%q", ok[0].Message)
	}
	waitUntil(r, "定位文件", func() bool { return len(e.data.CallsOf("OpenPath")) == 1 })
	if got := e.data.CallsOf("OpenPath")[0].Args[0]; got != `C:\out\GuiZhi-Config.json` {
		t.Errorf("应在文件管理器里定位导出的文件，得到 %q", got)
	}

	opts := e.cfg.Exports()[2]
	if !opts.IncludeSecrets || opts.Password != exportPW || !opts.IncludeUILayout || !opts.IncludeShortcuts || opts.IncludeMCPScope {
		t.Errorf("选项不对（应带密钥与布局、快捷键，不带 MCP 范围）：%v", opts)
	}
	e.assertNoSecrets(t, exportPW)
}

// encryptedRead 是一份加密的、带 MCP 范围的配置文件的读取结果。
func encryptedRead() services.ConfigRead {
	return services.ConfigRead{
		FilePath: `C:\in\GuiZhi-Config.json`,
		Preview: services.ConfigPreview{
			ExportedAt: "2026-01-02T03:04:05Z", AppVersion: "0.28.1", Encrypted: true,
			ProviderCount: 2, ModelCount: 5, RouteCount: 3, ShortcutCount: 8,
			MCPScope: &services.ConfigMCPScope{Mode: "selected", CollectionCount: 4, AllowUncategorized: true},
		},
	}
}

func pickImportFile(r *rig) {
	r.t.Helper()
	r.press(gfx.RoleButton, locales.SettingsConfigImportStart())
}

// 选文件：取消什么都不发生；读不出来提示原因。
func TestConfigImportReadCanceledAndFailed(t *testing.T) {
	e := newDataEnv()
	r := e.open(t)
	start := locales.SettingsConfigImportStart()

	pickImportFile(r) // 假服务默认「取消」
	waitUntil(r, "读取结束", func() bool { return len(e.cfg.CallsOf("Read")) == 1 && !r.disabled(gfx.RoleButton, start) })
	if r.dialogOpen(locales.SettingsConfigImportTitle()) || len(e.toasts(kit.ToastError)) != 0 {
		t.Error("取消选文件不应打开对话框或弹提示")
	}

	e.cfg.Fail("Read", errors.New("not a config file"))
	pickImportFile(r)
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsConfigImportUnreadable() || !strings.Contains(errs[0].Detail, "not a config file") {
		t.Errorf("读取失败提示不对：%+v", errs[0])
	}
	if r.dialogOpen(locales.SettingsConfigImportTitle()) {
		t.Error("读不出来不应打开导入对话框")
	}
}

// 预览：把文件里有什么摆出来；加密的文件要密码才能确认；文件没带 MCP 范围时明说「保持本机现状」。
func TestConfigImportPreview(t *testing.T) {
	e := newDataEnv()
	e.cfg.SetRead(encryptedRead())
	r := e.open(t)
	confirm := locales.SettingsConfigImportConfirm()
	pickImportFile(r)
	waitUntil(r, "预览对话框", func() bool { return r.dialogOpen(locales.SettingsConfigImportTitle()) })

	for _, want := range []string{
		locales.SettingsConfigImportRowEndpointsValue(2, 5),
		locales.SettingsConfigImportRowCount(3),
		locales.SettingsConfigImportRowCount(8),
		locales.SettingsConfigImportRowMcpWithUncategorized(locales.SettingsConfigImportRowMcpSelected(4)),
		locales.SettingsConfigImportRowKeysEncrypted(),
	} {
		if !r.hasText(want) {
			t.Errorf("预览里应有「%s」", want)
		}
	}
	if !r.disabled(gfx.RoleButton, confirm) {
		t.Error("加密文件没填密码时「替换本机配置」应禁用")
	}
	r.typeInto(locales.SettingsConfigImportPassword(), importPW)
	if r.disabled(gfx.RoleButton, confirm) {
		t.Error("填了密码后「替换本机配置」应可用")
	}
	r.clickLast(gfx.RoleButton, locales.CommonCancel())
	if r.dialogOpen(locales.SettingsConfigImportTitle()) || len(e.cfg.Applies()) != 0 {
		t.Fatal("取消应关闭对话框且不应用")
	}

	// 未加密、没带 MCP 范围的文件：不要密码，MCP 一行明说「保持本机现状」。
	e.cfg.SetRead(services.ConfigRead{FilePath: `C:\in\plain.json`, Preview: services.ConfigPreview{ExportedAt: "garbage", ProviderCount: 1}})
	pickImportFile(r)
	waitUntil(r, "第二份预览", func() bool { return r.hasText(locales.SettingsConfigImportRowKeysAbsent()) })
	if !r.hasText(locales.SettingsConfigImportRowMcpAbsent()) || !r.hasText(locales.SettingsConfigImportUnknownTime()) {
		t.Error("没带 MCP 范围、导出时间读不出来时应分别明说")
	}
	if r.has(gfx.RoleTextField, locales.SettingsConfigImportPassword()) || r.disabled(gfx.RoleButton, confirm) {
		t.Error("未加密的文件不应要密码，且可直接确认")
	}
}

// 应用：密码不对就地提示、对话框保留；其余失败提示原因；成功后提示（警告进详情）、关闭对话框、留一会儿再重启。
func TestConfigImportApplyFlow(t *testing.T) {
	e := newDataEnv()
	e.cfg.SetRead(encryptedRead())
	e.cfg.WantPassword = importPW
	e.cfg.SetApply(services.ConfigApplyResult{Warnings: []string{"布局偏好未能完整还原"}})
	r := e.open(t)
	confirm := locales.SettingsConfigImportConfirm()
	pwName := locales.SettingsConfigImportPassword()
	pickImportFile(r)
	waitUntil(r, "预览对话框", func() bool { return r.dialogOpen(locales.SettingsConfigImportTitle()) })

	// 取消勾选「快捷键」：只应用其余三项。
	r.press(gfx.RoleSwitch, locales.SettingsConfigImportSelectionShortcuts())

	// 密码不对：就地提示，不弹错误提示，对话框保留，可改后重试。
	r.typeInto(pwName, "wrong-import-pw")
	r.clickNode(gfx.RoleButton, confirm)
	waitUntil(r, "就地提示", func() bool { return r.hasText("wrong password") })
	if len(e.toasts(kit.ToastError)) != 0 || !r.dialogOpen(locales.SettingsConfigImportTitle()) {
		t.Fatal("密码不对应就地提示，不弹错误提示，对话框保留")
	}
	waitUntil(r, "确认钮恢复", func() bool { return !r.disabled(gfx.RoleButton, confirm) })

	// 其余失败：提示带原因，对话框保留。
	e.cfg.Fail("Apply", errors.New("config dir locked"))
	r.clickNode(gfx.RoleButton, confirm)
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if errs := e.toasts(kit.ToastError); errs[0].Message != locales.SettingsConfigImportFailed() || !strings.Contains(errs[0].Detail, "config dir locked") {
		t.Errorf("导入失败提示不对：%+v", errs[0])
	}
	if !r.dialogOpen(locales.SettingsConfigImportTitle()) {
		t.Fatal("失败后对话框应保留")
	}
	waitUntil(r, "确认钮恢复", func() bool { return !r.disabled(gfx.RoleButton, confirm) })

	// 成功：提示、关闭对话框；重启前界面保持忙碌。
	e.cfg.Fail("Apply", nil)
	r.typeInto(pwName, importPW)
	r.clickNode(gfx.RoleButton, confirm)
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	ok := e.toasts(kit.ToastSuccess)[0]
	if ok.Message != locales.SettingsConfigImportDone() || !strings.Contains(ok.Detail, "布局偏好未能完整还原") {
		t.Errorf("成功提示应带警告详情：%+v", ok)
	}
	waitUntil(r, "对话框关闭", func() bool { return !r.dialogOpen(locales.SettingsConfigImportTitle()) })
	if !r.disabled(gfx.RoleButton, locales.SettingsConfigImportStart()) {
		t.Error("导入成功后到重启前入口应保持禁用，避免再点一次")
	}
	if len(e.data.CallsOf("Relaunch")) != 0 {
		t.Error("应留一会儿让提示看得见，不能立即重启")
	}
	waitFor(r, "重启", 4*time.Second, func() bool { return len(e.data.CallsOf("Relaunch")) == 1 })

	applies := e.cfg.Applies()
	if len(applies) != 3 {
		t.Fatalf("应调用三次应用，得到 %d", len(applies))
	}
	for _, a := range applies {
		if a.FilePath != `C:\in\GuiZhi-Config.json` {
			t.Errorf("应用的文件不对：%q", a.FilePath)
		}
		if !a.Selection.Settings || !a.Selection.UILayout || a.Selection.Shortcuts || !a.Selection.MCPScope {
			t.Errorf("选择项不对（应取消快捷键，其余保持勾选）：%+v", a.Selection)
		}
	}
	if got := e.cfg.CallsOf("Apply")[2].Args[1]; got != importPW {
		t.Errorf("密码应交给服务：%q", got)
	}
	e.assertNoSecrets(t, importPW, "wrong-import-pw")
}
