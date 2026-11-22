package settings_test

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 导出选项必须如实跟随对话框里的开关：关闭「包含 API Key」就不能把机密写进文件（导出脱敏的第一道关），
// 三个偏好开关也各自对应各自的选项。用「取消选位置」的结果让对话框保留，在同一个对话框里连导两次。
func TestConfigExportOptionsFollowSwitches(t *testing.T) {
	e := newDataEnv()
	e.cfg.SetExport(services.ConfigExportResult{Canceled: true})
	r := e.open(t)
	confirm := locales.SettingsConfigExportConfirm()
	exportOnce := func(want int) {
		r.t.Helper()
		r.clickNode(gfx.RoleButton, confirm)
		waitUntil(r, "导出调用结束", func() bool { return len(e.cfg.Exports()) == want && !r.disabled(gfx.RoleButton, confirm) })
	}
	openExportDialog(r)

	// 第一次：关掉「包含 API Key」，其余默认（布局、快捷键、MCP 范围都带）。
	r.clickNode(gfx.RoleSwitch, locales.SettingsConfigExportIncludeKeys())
	exportOnce(1)
	got := e.cfg.Exports()[0]
	if got.IncludeSecrets {
		t.Errorf("关闭「包含 API Key」后导出选项不应带机密：%+v", got)
	}
	if !got.IncludeUILayout || !got.IncludeShortcuts || !got.IncludeMCPScope {
		t.Errorf("其余三项默认都带：%+v", got)
	}

	// 第二次：重新带 API Key（需要密码），关掉布局与快捷键，保留 MCP 范围。
	r.clickNode(gfx.RoleSwitch, locales.SettingsConfigExportIncludeKeys())
	r.typeInto(locales.SettingsConfigExportPassword(), exportPW)
	r.typeInto(locales.SettingsConfigExportPasswordConfirm(), exportPW)
	r.clickNode(gfx.RoleSwitch, locales.SettingsConfigExportLayout())
	r.clickNode(gfx.RoleSwitch, locales.SettingsConfigExportShortcuts())
	exportOnce(2)
	got = e.cfg.Exports()[1]
	if !got.IncludeSecrets || got.Password != exportPW {
		t.Errorf("带 API Key 时应带机密与密码：%+v", got)
	}
	if got.IncludeUILayout || got.IncludeShortcuts || !got.IncludeMCPScope {
		t.Errorf("应只带 MCP 范围（布局与快捷键已关）：%+v", got)
	}
	e.assertNoSecrets(t, exportPW)
}
