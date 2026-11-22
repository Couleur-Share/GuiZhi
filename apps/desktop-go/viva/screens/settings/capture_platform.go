package settings

import (
	"context"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// platformIDs 是账号行的平台与顺序（渲染层 PlatformAccountRows；NodeSeek 在渲染层有名字但没有行）。
var platformIDs = []string{services.PlatformXiaohongshu, services.PlatformDouyin, services.PlatformLinuxdo}

func platformName(id string) string {
	switch id {
	case services.PlatformXiaohongshu:
		return locales.SettingsPlatformXiaohongshu()
	case services.PlatformDouyin:
		return locales.SettingsPlatformDouyin()
	case services.PlatformLinuxdo:
		return locales.SettingsPlatformLinuxdo()
	}
	return id
}

// platformAccounts 是「平台登录」几行（渲染层 PlatformAccountRows）：小红书、抖音、LINUX DO 的登录状态与登录、重新登录、退出，
// 加「清除全部平台登录数据」。登录会话由外壳内置的登录窗口保管，这里只拿到「已登录与否」，拿不到也不显示任何凭证。
type platformAccounts struct {
	look  *kit.Look
	guard kit.Guard
	svc   services.CaptureTools
	wake  func()

	started  bool
	statuses []services.PlatformStatus
	active   string // 本页刚发起登录或退出的平台：做事期间那一行只给「取消」
	clearing bool   // 清除确认框是否打开
	confirm  kit.ConfirmState
	w        worker
}

func (p *platformAccounts) Close() { p.w.close() }

func (p *platformAccounts) status(id string) *services.PlatformStatus {
	for i := range p.statuses {
		if p.statuses[i].Platform == id {
			return &p.statuses[i]
		}
	}
	return nil
}

// refresh 读取各平台的登录状态；失败弹带原因的错误提示（渲染层 reportOperationError）。
func (p *platformAccounts) refresh() {
	svc, g := p.svc, p.guard
	p.w.load(p.wake, func(ctx context.Context) func() {
		var (
			list []services.PlatformStatus
			err  = errCaptureUnavailable
		)
		if svc != nil {
			list, err = svc.PlatformStatuses(ctx)
		}
		if err != nil && ctx.Err() == nil {
			g.Report(locales.SettingsPlatformCaptureStatus(), err)
		}
		return func() {
			if err == nil {
				p.statuses = list
			}
		}
	})
}

// run 在后台做一次登录或退出：做完回到界面线程清掉「进行中」并重读状态。动作不随分区切走而取消（登录窗口开着就让它走完）。
func (p *platformAccounts) run(id, action string, work func(svc services.CaptureTools) error) {
	p.active = id
	svc, g := p.svc, p.guard
	p.w.act(p.wake, func() func() {
		err := errCaptureUnavailable
		if svc != nil {
			err = work(svc)
		}
		if err != nil {
			g.Report(action, err)
		}
		return func() {
			p.active = ""
			p.refresh()
		}
	})
}

func (p *platformAccounts) login(id string, force bool) {
	g, name := p.guard, platformName(id)
	p.run(id, locales.SettingsPlatformLoginAction(name), func(svc services.CaptureTools) error {
		st, err := svc.PlatformLogin(context.Background(), id, force)
		if err == nil && st.LoggedIn {
			g.Succeed(locales.SettingsPlatformLoginSuccess(name))
		}
		return err
	})
}

func (p *platformAccounts) logout(id string) {
	p.run(id, locales.SettingsPlatformLogoutAction(platformName(id)), func(svc services.CaptureTools) error {
		return svc.PlatformLogout(context.Background(), id)
	})
}

func (p *platformAccounts) cancel(id string) {
	svc, g := p.svc, p.guard
	p.w.act(p.wake, func() func() {
		if svc == nil {
			return func() {}
		}
		if _, err := svc.PlatformCancelLogin(context.Background(), id); err != nil {
			g.Background("cancel platform login", err)
		}
		return func() { p.refresh() }
	})
}

func (p *platformAccounts) clearAll() {
	svc, g := p.svc, p.guard
	p.w.act(p.wake, func() func() {
		err := errCaptureUnavailable
		if svc != nil {
			err = svc.PlatformClearAll(context.Background())
		}
		if err != nil {
			g.Report(locales.SettingsPlatformClearData(), err)
		} else {
			g.Succeed(locales.SettingsPlatformClearSuccess())
		}
		return func() { p.refresh() }
	})
}

// browserText 是「归知内置登录窗口 · Chromium x.y」。
func browserText(version string) string {
	out := locales.NativeSettingsPlatformBrowser()
	if version != "" {
		out += " · Chromium " + version
	}
	return out
}

func (p *platformAccounts) description(id string, st *services.PlatformStatus) string {
	linuxdo := id == services.PlatformLinuxdo
	switch {
	case st == nil || !st.Available:
		return locales.SettingsPlatformBrowserUnavailable()
	case st.LoggedIn && linuxdo:
		return locales.SettingsPlatformLinuxdoVerified(browserText(st.BrowserVersion))
	case st.LoggedIn:
		return locales.SettingsPlatformLoggedIn(browserText(st.BrowserVersion))
	case linuxdo:
		return locales.SettingsPlatformLinuxdoLoggedOut()
	}
	return locales.SettingsPlatformLoggedOut()
}

// Rows 声明账号行（放在「采集」分区卡片里，不自带分区标题）；每帧调用。
func (p *platformAccounts) Rows(c ui.C) {
	l := p.look
	p.w.drain()
	if !p.started {
		p.started = true
		p.refresh()
	}
	for _, id := range platformIDs {
		st := p.status(id)
		l.SettingItem(c.Key(id), kit.SettingRow{
			Label: platformName(id), Description: p.description(id, st),
			Control: func() { p.controls(c.Key(id+"-controls"), id, st) },
		}, false)
	}
	t := l.T
	l.SettingItem(c.Key("clear"), kit.SettingRow{
		Label: locales.SettingsPlatformClearData(), Description: locales.SettingsPlatformClearDataDesc(),
		Control: func() {
			s := outlineBtn(l, shape(l, 2, 3, 1.5, kit.TextXS, kit.WeightMedium), t.Destructive.WithAlpha(0.1))
			s.Fg, s.Border = t.Destructive, t.Destructive.WithAlpha(0.5)
			ic, size := iconOf(l, icons.Trash2, true)
			if btn(l, c.Key("clear-button"), kit.ButtonSpec{Label: locales.CommonClear(), Icon: ic, IconSize: size}, s, false) {
				p.clearing = true
			}
		},
	}, false)
	switch l.ConfirmDialog(c.Key("clear-confirm"), &p.confirm, kit.ConfirmSpec{
		Open: p.clearing, Title: locales.SettingsPlatformClearData(), Message: locales.SettingsPlatformClearConfirm(),
		ConfirmText: locales.CommonClear(), CancelText: locales.CommonCancel(), Destructive: true,
	}) {
	case kit.ConfirmAccepted:
		p.clearing = false
		p.clearAll()
	case kit.ConfirmDismissed:
		p.clearing = false
	}
}

// controls 是一行右侧的按钮：进行中只有「取消」；已登录给「重新登录」与「退出账号」；否则给「登录」（LINUX DO 是「验证」）。
func (p *platformAccounts) controls(c ui.C, id string, st *services.PlatformStatus) {
	l := p.look
	t := l.T
	small := shape(l, 2, 3, 1.5, kit.TextXS, kit.WeightNormal) // h-8 px-3 gap-1.5 text-xs
	outline := func() kit.ButtonStyle { return outlineBtn(l, small, t.Muted.WithAlpha(0.6)) }
	ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
		busy := p.active == id || (st != nil && st.Busy)
		switch {
		case busy:
			ic, size := iconOf(l, icons.X, true)
			spec := kit.ButtonSpec{Label: locales.CommonCancel(), Icon: ic, IconSize: size, Busy: true, BusySize: l.Rem(0.875)}
			// 取消按钮在登录进行中仍要可点：转圈只是进行中的标记，不禁用。
			if l.Button(c.Key("cancel"), spec, outline()).Clicked() {
				p.cancel(id)
			}
		case st != nil && st.LoggedIn:
			ic, size := iconOf(l, icons.LogIn, true)
			if btn(l, c.Key("relogin"), kit.ButtonSpec{Label: locales.SettingsPlatformRelogin(), Icon: ic, IconSize: size}, outline(), false) {
				p.login(id, true)
			}
			ic, size = iconOf(l, icons.LogOut, true)
			if btn(l, c.Key("logout"), kit.ButtonSpec{Label: locales.SettingsPlatformLogout(), Icon: ic, IconSize: size}, outline(), false) {
				p.logout(id)
			}
		default:
			label := locales.SettingsPlatformLogin()
			if id == services.PlatformLinuxdo {
				label = locales.SettingsPlatformVerify()
			}
			style := primaryBtn(l, shape(l, 2, 3, 1.5, kit.TextXS, kit.WeightMedium))
			ic, size := iconOf(l, icons.LogIn, true)
			if btn(l, c.Key("login"), kit.ButtonSpec{Label: label, Icon: ic, IconSize: size, Disabled: st == nil || !st.Available}, style, false) {
				p.login(id, false)
			}
		}
	})
}
