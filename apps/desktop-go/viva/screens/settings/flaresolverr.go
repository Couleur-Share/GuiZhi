package settings

import (
	"context"
	"errors"
	"strconv"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// errSolverUnavailable 是宿主没有接上 FlareSolverr 测试时的失败原因（进「查看详情」与日志，不进界面文案）。
var errSolverUnavailable = errors.New("the FlareSolverr connection check is not available in this build")

// FlareSolverr 是「Cloudflare 后备采集」配置块（渲染层 FlareSolverrSettings）：启用开关、连接方式、端口与 SSH 主机别名、
// 「测试连接」「保存设置」两个按钮。
//
// 与渲染层一样，编辑的是一份草稿：保存才写进设置，测试连接用的是草稿（不必先保存）。
// 设置从别处改了，草稿跟着换成新值。
type FlareSolverr struct {
	Look  *kit.Look
	Store Store
	// Guard 接住保存与测试的失败：错误提示（原因折叠在「查看详情」里）并记日志。
	Guard kit.Guard
	// Solver 做连通性测试；nil 表示宿主没有接上，点测试如实报错。
	Solver services.FlareSolverr
	// Wake 请求界面重绘（后台测试完成时调用，任意线程）；可为 nil。
	Wake func()

	draft  settings.FlareSolverr // 草稿里的开关与连接方式；端口与主机别名以下面两个文本框的文字为准
	seen   settings.FlareSolverr // 草稿上次对齐的设置值
	synced bool

	port, sshHost ui.Field
	connSel       kit.SelectState

	busy    bool   // 测试进行中：开关、选择、输入、按钮都不能动
	message string // 最近一次成功的结果（role=status）；任何编辑都清掉它

	mb     mailbox
	cancel context.CancelFunc
}

// Close 在块被卸载时调用：取消进行中的测试，之后它的结果不再回来改界面（也不再弹错误提示）。
func (f *FlareSolverr) Close() {
	f.mb.close()
	if f.cancel != nil {
		f.cancel()
	}
}

// solverGridBreakpoint 是 Tailwind sm 断点（640px）：不窄于它时「连接方式」与「服务端口」并排（grid sm:grid-cols-2）。
const solverGridBreakpoint = 640

// sync 把草稿对齐到设置里的值（渲染层 useEffect(() => setDraft(saved), [saved])）：首次出现与设置变化时。
func (f *FlareSolverr) sync() {
	saved := f.Store.Values().App.FlareSolverr
	if f.synced && saved == f.seen {
		return
	}
	f.draft, f.seen, f.synced = saved, saved, true
	f.port.SetText(strconv.Itoa(saved.Port))
	f.sshHost.SetText(saved.SSHHost)
}

// config 是此刻要保存或测试的配置：开关与连接方式取草稿，端口与主机别名取文本框里的文字（端口读不出来给 0，由 Go 侧校验拒绝）。
func (f *FlareSolverr) config() settings.FlareSolverr {
	cfg := f.draft
	cfg.Port = parseNumber(f.port.Text())
	cfg.SSHHost = f.sshHost.Text()
	return cfg
}

// Page 声明整块：border border-border rounded-xl p-4，里面 space-y-3。
func (f *FlareSolverr) Page(c ui.C) {
	l := f.Look
	t := l.T
	f.mb.drain()
	f.sync()
	surface := kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)}
	ui.Column(c).FillWidth().MinWidth(0).Pad(l.Space(4) + 1).Gap(l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).
		Children(func() {
			f.header(c)
			l.Text(c, locales.WebCaptureSolverHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			f.fields(c)
			hint := locales.WebCaptureSolverLocalHint()
			if f.draft.Connection == "ssh" {
				hint = locales.WebCaptureSolverRemoteHint()
			}
			l.Text(c.Key("hint2"), hint, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			f.buttons(c)
			if f.message != "" {
				l.Text(c.Key("message"), f.message, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
			}
		})
}

// header 是标题加启用开关（flex items-center justify-between gap-3）。
func (f *FlareSolverr) header(c ui.C) {
	l := f.Look
	ui.Row(c.Key("header")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).Children(func() {
		l.Heading(c, 3, locales.WebCaptureSolverTitle(), kit.TextSM, kit.WeightMedium, l.T.Foreground, kit.Wrapped()).Shrink(1).MinWidth(0)
		on := f.draft.Enabled
		sw := l.SwitchWith(c, &on, kit.SwitchSpec{Label: locales.WebCaptureSolverEnable(), Disabled: f.busy})
		if sw.Clicked() && !f.busy {
			f.draft.Enabled, f.message = on, ""
		}
	})
}

// fields 是连接方式、端口与（SSH 时的）主机别名：grid gap-3，sm 起两列，主机别名占满两列。
func (f *FlareSolverr) fields(c ui.C) {
	l := f.Look
	wide := l.ViewportWidth(c) >= solverGridBreakpoint
	cell := func(key string, label string, body func()) {
		col := ui.Column(c.Key(key)).MinWidth(0).Gap(l.Space(1)) // label space-y-1 min-w-0
		if wide && key != "ssh-host" {
			col = col.Grow(1).Basis(0)
		} else {
			col = col.FillWidth()
		}
		col.Children(func() {
			// 渲染层的标签是 <label> 里的内联 <span class="text-sm">：行盒由外层的 16px/24px 行高撑成 24px，14px 的文字按基线落在其中，
			// 比单独一行 20px 的文字低 3px、行盒高 4px（实测：文字顶低 3px，控件顶低 4px）。
			lc := c.Key(key + "-label")
			ui.Column(lc).FillWidth().Height(l.Rem(1.5)).PadTop(l.Rem(0.1875)).Children(func() {
				l.Text(lc.Key("text"), label, kit.TextSM, kit.WeightNormal, l.T.Foreground)
			})
			body()
		})
	}
	connection := func() {
		cell("connection", locales.WebCaptureSolverConnection(), func() {
			picked, ok := l.Select(c, &f.connSel, kit.SelectSpec{
				Label: locales.WebCaptureSolverConnection(), Value: f.draft.Connection,
				Options: []kit.SelectOption{
					{Value: "local", Label: locales.WebCaptureSolverLocal()},
					{Value: "ssh", Label: locales.WebCaptureSolverSsh()},
				},
				Width: -1, // w-full
			})
			if f.busy {
				f.connSel.Open = false
			} else if ok {
				f.draft.Connection, f.message = picked, ""
			}
		})
	}
	port := func() {
		cell("port", locales.WebCaptureSolverPort(), func() {
			f.input(c.Key("port-input"), &f.port, kit.SettingsInputSpec{Name: locales.WebCaptureSolverPort()})
		})
	}
	host := func() {
		cell("ssh-host", locales.WebCaptureSolverHost(), func() {
			f.input(c.Key("host-input"), &f.sshHost, kit.SettingsInputSpec{Name: locales.WebCaptureSolverHost(), Placeholder: "my-server"})
		})
	}
	if wide {
		ui.Row(c.Key("grid")).FillWidth().Gap(l.Space(3)).Children(func() {
			connection()
			port()
		})
	} else {
		connection()
		port()
	}
	if f.draft.Connection == "ssh" {
		host()
	}
}

// input 声明一个文本框：编辑清掉上一条结果；测试进行中文字不能改（渲染层 disabled={busy}），编辑后退回原文。
func (f *FlareSolverr) input(c ui.C, fld *ui.Field, spec kit.SettingsInputSpec) {
	before := fld.Text()
	box := f.Look.SettingsInput(c, fld, spec)
	if !box.Changed() {
		return
	}
	if f.busy {
		fld.SetText(before)
		return
	}
	f.message = ""
}

// buttons 是「测试连接」与「保存设置」（flex gap-2）：rounded-lg px-3 py-2 text-sm，测试带边框，保存是主色实心；测试进行中都禁用。
func (f *FlareSolverr) buttons(c ui.C) {
	l := f.Look
	t := l.T
	base := kit.ButtonStyle{Radius: l.Rem(0.75), PadX: l.Space(3), PadY: l.Space(2), Text: kit.TextSM, Weight: kit.WeightNormal}
	check, save := base, base
	check.Fg, check.Border, check.BorderW = t.Foreground, t.Border, 1
	save.Fg, save.Bg = t.PrimaryForeground, t.Primary
	ui.Row(c.Key("buttons")).FillWidth().Gap(l.Space(2)).Children(func() {
		if l.Button(c.Key("check"), kit.ButtonSpec{Label: locales.WebCaptureSolverCheck(), Disabled: f.busy}, check).Clicked() {
			f.check()
		}
		if l.Button(c.Key("save"), kit.ButtonSpec{Label: locales.WebCaptureSolverSave(), Disabled: f.busy}, save).Clicked() {
			f.save()
		}
	})
}

// save 保存草稿（渲染层 run(true)）：成功显示「设置已保存」；失败设置不变，错误提示带原因，用户可以直接再点一次。
func (f *FlareSolverr) save() {
	if f.busy {
		return
	}
	f.message = ""
	cfg := f.config()
	if f.Guard.Run(locales.WebCaptureSolverAction(), func() error {
		return f.Store.Update(func(v *settings.Values) { v.App.FlareSolverr = cfg })
	}) {
		f.message = locales.WebCaptureSolverSaved()
	}
}

// check 测试连接（渲染层 run(false)）：在后台连接，期间 busy；成功显示「服务连接成功…」，失败弹出带原因的错误提示。
// 测试用的是草稿，不改已保存的设置。
func (f *FlareSolverr) check() {
	if f.busy {
		return
	}
	f.busy, f.message = true, ""
	cfg := f.config()
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	solver, wake, g := f.Solver, f.Wake, f.Guard // goroutine 里只用拷好的局部变量：Network 每帧会改写 f.Guard 等字段
	go func() {
		var err error
		if solver == nil {
			err = errSolverUnavailable
		} else {
			err = solver.Check(ctx, cfg)
		}
		if ctx.Err() != nil {
			return // 块已经被卸载：不再提示，也没有界面可改
		}
		if err != nil {
			g.Report(locales.WebCaptureSolverAction(), err)
		}
		f.mb.post(wake, func() {
			f.busy = false
			if err == nil {
				f.message = locales.WebCaptureSolverConnected()
			}
		})
	}()
}
