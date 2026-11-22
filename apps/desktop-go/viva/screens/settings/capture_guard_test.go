package settings_test

import (
	"context"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// 本地转写引擎的卸载确认按版本给说明：Python 版与 GGUF 版的体积、能力不同，说错了用户会按错误的预期点确认。
func TestCaptureFunasrUninstallConfirmFollowsFlavor(t *testing.T) {
	cases := []struct {
		name   string
		flavor string
		want   string
	}{
		{"python", "", locales.SettingsFunasrUninstallConfirm()},
		{"gguf", services.FunasrFlavorGguf, locales.SettingsFunasrUninstallConfirmGguf()},
	}
	if cases[0].want == cases[1].want {
		t.Fatal("两版卸载说明应不同，否则这条测试没有意义")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCaptureEnv()
			st := installedFunasr()
			st.InstallFlavor = tc.flavor
			e.cap.SetFunasrStatus(st)
			r := e.open(t)
			waitText(r, "v1.2.0")
			r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 2) // 第三行
			r.press(gfx.RoleButton, locales.SettingsFunasrUninstall())
			if !r.dialogOpen(locales.SettingsFunasrUninstall()) {
				t.Fatal("卸载应当先弹确认")
			}
			if got := r.dialogDescription(locales.SettingsFunasrUninstall()); got != tc.want {
				t.Errorf("确认说明 = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// 「移除内置版」只给内置版：系统 PATH 里的、自定义路径的不是归知装的，没有可移除的东西，不能给这个入口。
func TestCaptureRemoveOnlyOffersManagedCopy(t *testing.T) {
	cases := []struct{ name, source, sourceLabel string }{
		{"系统 PATH", services.ToolSourcePath, locales.SettingsCaptureSourcePath()},
		{"自定义路径", services.ToolSourceCustom, locales.SettingsCaptureSourceCustom()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCaptureEnv()
			e.cap.SetToolStatus(services.ToolYtDlp, services.ToolStatus{Installed: true, Source: tc.source, Version: "2026.01.01", Path: `D:\bin\yt-dlp.exe`, InstallSupported: true})
			r := e.open(t)
			waitText(r, tc.sourceLabel) // 探测结果已回来
			r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 0)
			if !r.has(gfx.RoleButton, locales.SettingsCaptureBrowse()) {
				t.Fatal("高级面板应已展开")
			}
			if r.has(gfx.RoleButton, locales.SettingsCaptureRemoveManaged()) {
				t.Errorf("%s 的来源不是内置版，不应给「移除内置版」", tc.name)
			}
		})
	}
}

// 其它入口（比如另一个窗口）发起的登录：状态里是「忙」，那一行只给「取消」，不能再点「登录」叠一个。
func TestCapturePlatformBusyFromElsewhereOffersCancel(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetAccounts([]services.PlatformStatus{
		{Platform: services.PlatformXiaohongshu, Available: true, Busy: true},
		{Platform: services.PlatformDouyin, Available: true},
		{Platform: services.PlatformLinuxdo, Available: true},
	})
	r := e.open(t)
	waitButton(r, locales.CommonCancel())
	if n := len(r.named(gfx.RoleButton, locales.SettingsPlatformLogin())); n != 1 {
		t.Errorf("忙的那一行只给「取消」，另两行中抖音是「登录」：登录按钮 %d 个", n)
	}
	if !r.has(gfx.RoleButton, locales.SettingsPlatformVerify()) {
		t.Error("LINUX DO 不忙，仍是「验证」")
	}
	if len(r.named(gfx.RoleButton, locales.CommonCancel())) != 1 {
		t.Error("只有忙的那一行有「取消」")
	}
}

// closedLoginTools 模拟登录窗口被用户直接关掉：调用没有出错，但账号也没有登录。
type closedLoginTools struct{ *servicestest.Capture }

func (c closedLoginTools) PlatformLogin(_ context.Context, platform string, _ bool) (services.PlatformStatus, error) {
	return services.PlatformStatus{Platform: platform, Available: true}, nil
}

// 没登录成功不能弹「登录成功」：登录窗口被关掉时没有错误，但状态仍是未登录，提示要以结果为准。
func TestCapturePlatformClosedLoginWindowIsNotSuccess(t *testing.T) {
	e := newCaptureEnv()
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		p := &sscreen.Capture{
			Look: look, Store: e.store, Guard: e.guard(), Service: closedLoginTools{e.cap}, Solver: e.solver, Platform: e.platform, Wake: func() {},
		}
		t.Cleanup(p.Close)
		return p.Page
	})
	waitText(r, locales.SettingsPlatformLoggedOut())
	before := e.cap.AccountStatusCalls()
	r.clickNth(gfx.RoleButton, locales.SettingsPlatformLogin(), 0)
	waitUntil(r, "登录结束后重读状态", func() bool { return e.cap.AccountStatusCalls() > before })
	if got := e.toasts(kit.ToastSuccess); len(got) != 0 {
		t.Errorf("没有登录成功不应弹成功提示：%+v", got)
	}
	if got := e.toasts(kit.ToastError); len(got) != 0 {
		t.Errorf("窗口被关掉不是错误：%+v", got)
	}
	if !r.hasText(locales.SettingsPlatformLoggedOut()) {
		t.Error("账号仍是未登录")
	}
}
