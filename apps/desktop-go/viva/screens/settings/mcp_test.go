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
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// secretValue 是配置里的环境变量值：客户端配置片段里要原样出现（用户要拿去粘贴），但日志、错误提示与详情里一个字都不能有。
const secretValue = "s3cr3t-env-value-9f2c"

// mcpEnv 是「MCP 接入」页的测试环境：假的 MCP 服务与合集来源。
type mcpEnv struct {
	*sysEnv
	mcp *servicestest.MCPFake
	lib *servicestest.Library
}

func newMCPEnv() *mcpEnv {
	e := &mcpEnv{sysEnv: newEnv(), mcp: &servicestest.MCPFake{}, lib: &servicestest.Library{}}
	e.mcp.SetConfig(services.MCPConfig{
		Available: true, ServerPath: `C:\gz\mcp\server.js`, Command: "node", Args: []string{`C:\gz\mcp\server.js`},
		Env: map[string]string{"GUIZHI_DATA_DIR": `C:\gz\data`, "GUIZHI_API_KEY": secretValue},
	})
	e.lib.Seed(services.LibraryCounts{}, []services.LibraryCollection{
		{ID: "c1", Name: "Reading notes", Icon: "📚"},
		{ID: "c2", Name: "Work"},
	}, nil)
	return e
}

func (e *mcpEnv) open(t *testing.T) *rig {
	t.Helper()
	return openPage(t, func(look *kit.Look) func(c ui.C) {
		m := &sscreen.MCP{Look: look, Guard: e.guard(), Service: e.mcp, Collections: e.lib, Wake: func() {}}
		t.Cleanup(m.Close)
		return m.Page
	})
}

// assertNoSecret 核对提示、详情与日志里没有环境变量值。
func (e *mcpEnv) assertNoSecret(t *testing.T) {
	t.Helper()
	all := fmt.Sprint(e.notify.Queued(), e.logs.all())
	if strings.Contains(all, secretValue) {
		t.Errorf("提示或日志里出现了环境变量值：%s", all)
	}
}

func TestMCPShowsStatusScopeAndClients(t *testing.T) {
	e := newMCPEnv()
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())
	if !r.hasText(`C:\gz\mcp\server.js`) {
		t.Error("应显示 server 路径（要拿去和配置文件核对）")
	}
	if !r.hasText(locales.SettingsMcpScopeDesc()) || !r.has(gfx.RoleTab, "Cursor") || !r.has(gfx.RoleTab, "Codex") {
		t.Error("组件就绪时应有可访问范围与客户端两块")
	}
	// 默认全部合集：模式卡选中，没有合集勾选行。
	if !r.selected(gfx.RoleButton, locales.SettingsMcpScopeAll()) || r.selected(gfx.RoleButton, locales.SettingsMcpScopeSelected()) {
		t.Error("默认应选中「全部合集」")
	}
	if r.has(gfx.RoleButton, locales.SettingsMcpScopeUncategorized()) {
		t.Error("全部合集模式下不列合集")
	}
}

func TestMCPLoadFailureShowsRetry(t *testing.T) {
	e := newMCPEnv()
	e.mcp.FailConfig(errors.New("read mcp config: access denied"))
	r := e.open(t)
	waitText(r, locales.SettingsMcpLoadFailed())
	if r.hasText(locales.SettingsMcpStatusReady()) || r.hasText(locales.SettingsMcpScopeDesc()) {
		t.Error("读取失败不能渲染成正常页或空态")
	}
	if len(e.logs.all()) == 0 {
		t.Error("读取失败应记入日志")
	}
	e.mcp.FailConfig(nil)
	r.clickNode(gfx.RoleButton, locales.SettingsMcpRecheck())
	waitText(r, locales.SettingsMcpStatusReady())
}

func TestMCPMissingComponentHidesScopeAndClients(t *testing.T) {
	e := newMCPEnv()
	e.mcp.SetConfig(services.MCPConfig{Available: false, ServerPath: `C:\gz\mcp\server.js`})
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusMissing())
	if !r.hasText(locales.SettingsMcpMissing()) {
		t.Error("组件缺失应说明原因与处理办法")
	}
	if r.hasText(locales.SettingsMcpScopeDesc()) || r.has(gfx.RoleTab, "Cursor") {
		t.Error("组件缺失时不应显示范围与客户端")
	}
}

func TestMCPScopeAppliesImmediately(t *testing.T) {
	e := newMCPEnv()
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())

	// 切到「指定合集」：立即落盘，列出「未归合集」与各合集。
	r.press(gfx.RoleButton, locales.SettingsMcpScopeSelected())
	waitUntil(r, "范围写入", func() bool { return len(e.mcp.ScopeSets()) == 1 })
	if got := e.mcp.ScopeSets()[0]; got.Mode != services.MCPScopeSelected {
		t.Fatalf("写入的范围 = %+v", got)
	}
	waitButton(r, "Reading notes")
	if !r.has(gfx.RoleButton, "Work") || !r.has(gfx.RoleButton, locales.SettingsMcpScopeUncategorized()) {
		t.Error("应列出「未归合集」与全部合集")
	}

	// 勾选与取消合集都立即落盘。
	r.press(gfx.RoleButton, "Reading notes")
	waitUntil(r, "勾选写入", func() bool { return len(e.mcp.ScopeSets()) == 2 })
	if got := e.mcp.CurrentScope(); !got.HasCollection("c1") {
		t.Errorf("勾选后范围 = %+v", got)
	}
	waitUntil(r, "勾选态可见", func() bool { return r.selected(gfx.RoleButton, "Reading notes") })

	// 取消「未归合集」与「Reading notes」：一个都没勾，提醒 AI 什么都搜不到。
	r.press(gfx.RoleButton, "Reading notes")
	waitUntil(r, "取消勾选写入", func() bool { return len(e.mcp.ScopeSets()) == 3 })
	r.press(gfx.RoleButton, locales.SettingsMcpScopeUncategorized())
	waitUntil(r, "取消未归写入", func() bool { return len(e.mcp.ScopeSets()) == 4 })
	waitText(r, locales.SettingsMcpScopeEmptyWarning())
	if !e.mcp.CurrentScope().IsEmpty() {
		t.Errorf("范围应为空，得到 %+v", e.mcp.CurrentScope())
	}
}

func TestMCPScopeSaveFailureRollsBack(t *testing.T) {
	e := newMCPEnv()
	e.mcp.FailSetScope(errors.New("write mcp scope: disk full"))
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())
	r.press(gfx.RoleButton, locales.SettingsMcpScopeSelected())
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.toasts(kit.ToastError)[0]
	if toast.Message != locales.SettingsMcpScopeSaveFailed() || !strings.Contains(toast.Detail, "disk full") {
		t.Errorf("保存失败应提示并给原因：%+v", toast)
	}
	// 失败后回到原值（全部合集），不能让界面显示一个没生效的范围。
	waitUntil(r, "回滚", func() bool { return r.selected(gfx.RoleButton, locales.SettingsMcpScopeAll()) })
	if r.selected(gfx.RoleButton, locales.SettingsMcpScopeSelected()) {
		t.Error("保存失败后不应停在「指定合集」")
	}
}

func TestMCPClientTabsAndCopy(t *testing.T) {
	e := newMCPEnv()
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())

	// Cursor：位置说明、一键写入、配置片段与易错提醒。
	cfg, _ := e.mcp.Config(nil)
	if !r.hasText(locales.SettingsMcpCursorLocation()) || !r.has(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Cursor")) {
		t.Fatal("默认应显示 Cursor 的位置说明与一键写入")
	}
	r.press(gfx.RoleButton, locales.SettingsMcpCopy())
	if len(e.copied) != 1 || e.copied[0] != services.BuildCursorConfig(cfg) {
		t.Fatalf("复制的应是 Cursor 配置片段，得到 %v", e.copied)
	}
	waitButton(r, locales.SettingsMcpCopied())

	// 切到 Codex：没有一键写入，两段片段（命令行、TOML）与自己的提醒。
	r.clickNode(gfx.RoleTab, "Codex")
	if !r.selected(gfx.RoleTab, "Codex") || r.selected(gfx.RoleTab, "Cursor") {
		t.Error("应选中 Codex 标签")
	}
	if !r.hasText(locales.SettingsMcpCodexLocation()) || r.has(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Codex")) {
		t.Error("Codex 应显示自己的位置说明且没有一键写入")
	}
	if !r.hasText(locales.SettingsMcpSnippetCommand()) || !r.hasText(locales.SettingsMcpSnippetToml()) {
		t.Error("Codex 应有命令行与 TOML 两段片段")
	}
	r.clickNth(gfx.RoleButton, locales.SettingsMcpCopy(), 1)
	if got := e.copied[len(e.copied)-1]; got != services.BuildCodexToml(cfg) {
		t.Errorf("第二段应复制 TOML，得到 %q", got)
	}
	// 切回 Cursor。
	r.clickNode(gfx.RoleTab, "Cursor")
	if !r.hasText(locales.SettingsMcpCursorLocation()) {
		t.Error("切回 Cursor 应显示它的说明")
	}
}

func TestMCPCopyFailureReportsWithoutSecrets(t *testing.T) {
	e := newMCPEnv()
	e.clipErr = errors.New("clipboard is busy")
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())
	r.press(gfx.RoleButton, locales.SettingsMcpCopy())
	waitUntil(r, "复制失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.toasts(kit.ToastError)[0]
	if toast.Message != locales.SettingsMcpCopyFailed() || !strings.Contains(toast.Detail, "clipboard is busy") {
		t.Errorf("复制失败应提示并给原因：%+v", toast)
	}
	e.assertNoSecret(t)
}

func TestMCPInstallWritesAndReportsPath(t *testing.T) {
	e := newMCPEnv()
	e.mcp.SetInstallResult(services.MCPInstallResult{FilePath: `C:\Users\me\.cursor\mcp.json`})
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())

	r.press(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Cursor"))
	waitUntil(r, "写入提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	ok := e.toasts(kit.ToastSuccess)[0]
	if ok.Message != locales.SettingsMcpInstallWritten("Cursor") || ok.Detail != `C:\Users\me\.cursor\mcp.json` {
		t.Errorf("首次写入提示 = %+v", ok)
	}
	if got := e.mcp.Installs(); len(got) != 1 || got[0] != services.MCPClientCursor {
		t.Errorf("写入的客户端 = %v", got)
	}

	// 已有归知配置时是「更新」。
	e.mcp.SetInstallResult(services.MCPInstallResult{FilePath: `C:\Users\me\.cursor\mcp.json`, Replaced: true})
	waitUntil(r, "按钮恢复", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Cursor")) })
	r.press(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Cursor"))
	waitUntil(r, "更新提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 2 })
	if got := e.toasts(kit.ToastSuccess)[1].Message; got != locales.SettingsMcpInstallUpdated("Cursor") {
		t.Errorf("覆盖写入提示 = %q", got)
	}
}

func TestMCPInstallFailureReportsReasonWithoutSecrets(t *testing.T) {
	e := newMCPEnv()
	e.mcp.FailInstall(errors.New(`the existing config is not valid JSON: C:\Users\me\.cursor\mcp.json`))
	r := e.open(t)
	waitText(r, locales.SettingsMcpStatusReady())
	r.press(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Cursor"))
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.toasts(kit.ToastError)[0]
	if toast.Message != locales.SettingsMcpInstallFailed() || !strings.Contains(toast.Detail, "not valid JSON") {
		t.Errorf("写入失败应提示并给原因：%+v", toast)
	}
	if len(e.toasts(kit.ToastSuccess)) != 0 {
		t.Error("写入失败不能报成功")
	}
	e.assertNoSecret(t)
	// 失败后可重试。
	waitUntil(r, "按钮恢复", func() bool { return !r.disabled(gfx.RoleButton, locales.SettingsMcpInstallOneClick("Cursor")) })
}

func TestMCPNoServiceShowsLoadFailure(t *testing.T) {
	e := newMCPEnv()
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		m := &sscreen.MCP{Look: look, Guard: e.guard(), Wake: func() {}}
		t.Cleanup(m.Close)
		return m.Page
	})
	waitText(r, locales.SettingsMcpLoadFailed())
}
