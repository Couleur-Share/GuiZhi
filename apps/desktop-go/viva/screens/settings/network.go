package settings

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Network 是「网络」页（渲染层 NetworkSettings）：网络代理与应用更新的镜像源。
//
// 渲染层的 FlareSolverr 配置块挂在「采集与转写」页；那一页还没迁移，这里把它放在本页最下面（FlareSolverr 自成一块，
// 迁移采集页时把它挂过去即可，台账登记为有意的临时位置）。
type Network struct {
	Look  *kit.Look
	Store Store
	// Guard 接住写入失败：弹出带原因的错误提示并记日志；零值时失败只会静默回到原值。
	Guard kit.Guard
	// Solver 与 Wake 交给本页最下面的 FlareSolverr 块（见 FlareSolverr 同名字段）。
	Solver services.FlareSolverr
	Wake   func()

	modeSel, protocolSel kit.SelectState
	// 手动代理的五个文本框：受控，每次编辑立即写入（渲染层 setNetworkProxy 逐键写入），文字始终跟设置里的值一致。
	host, port, username, password, bypass ui.Field
	showPassword                           bool

	solver FlareSolverr
}

// Close 在分区被切走时调用：放掉后台状态（测试连接进行中的结果不再回来改界面）。
func (n *Network) Close() { n.solver.Close() }

// Page 声明整页：代理、应用更新两个分区，加 FlareSolverr 块。
func (n *Network) Page(c ui.C) {
	l := n.Look
	l.SettingsContent(c, locales.SettingsNetwork(), func() {
		l.SettingSections(c, func() {
			n.proxy(c)
			n.mirror(c)
			n.solver.Look, n.solver.Store, n.solver.Guard, n.solver.Solver, n.solver.Wake = l, n.Store, n.Guard, n.Solver, n.Wake
			n.solver.Page(c.Key("flaresolverr"))
		})
	})
}

func (n *Network) save(mutate func(*settings.Values)) bool { return save(n.Guard, n.Store, mutate) }

// setProxy 改代理设置的一部分（渲染层 setNetworkProxy(partial)）：归一化由 Store 的实现负责（与渲染层同一份规则），
// 写入失败时设置不变，界面随之回到原值并提示。
func (n *Network) setProxy(change func(*settings.NetworkProxy)) bool {
	return n.save(func(v *settings.Values) { change(&v.App.NetworkProxy) })
}

func (n *Network) proxyNow() settings.NetworkProxy { return n.Store.Values().App.NetworkProxy }

func proxyModeOptions() []kit.SelectOption {
	return []kit.SelectOption{
		{Value: "system", Label: locales.SettingsNetworkProxyModeSystem()},
		{Value: "direct", Label: locales.SettingsNetworkProxyModeDirect()},
		{Value: "manual", Label: locales.SettingsNetworkProxyModeManual()},
	}
}

// proxyProtocolOptions 的标签是协议名本身，两种界面语言下相同（渲染层也是写死的字面量）。
func proxyProtocolOptions() []kit.SelectOption {
	return []kit.SelectOption{
		{Value: "http", Label: "HTTP"},
		{Value: "https", Label: "HTTPS"},
		{Value: "socks5", Label: "SOCKS5"},
	}
}

func (n *Network) proxy(c ui.C) {
	l := n.Look
	rows := []kit.SettingRow{{
		Label: locales.SettingsNetworkProxyMode(), Description: locales.SettingsNetworkProxyModeDesc(),
		Control: func() {
			picked, ok := l.Select(c, &n.modeSel, kit.SelectSpec{
				Label: locales.SettingsNetworkProxyMode(), Value: n.proxyNow().Mode,
				Options: proxyModeOptions(), Width: l.Rem(10), // w-40
			})
			if ok {
				n.setProxy(func(p *settings.NetworkProxy) { p.Mode = picked })
			}
		},
	}}
	if n.proxyNow().Mode == "manual" {
		rows = append(rows,
			kit.SettingRow{
				Label: locales.SettingsNetworkProxyProtocol(), Description: locales.SettingsNetworkProxyProtocolDesc(),
				Control: func() {
					picked, ok := l.Select(c, &n.protocolSel, kit.SelectSpec{
						Label: locales.SettingsNetworkProxyProtocol(), Value: n.proxyNow().Protocol,
						Options: proxyProtocolOptions(), Width: l.Rem(8), // w-32
					})
					if ok {
						n.setProxy(func(p *settings.NetworkProxy) { p.Protocol = picked })
					}
				},
			},
			kit.SettingRow{Label: locales.SettingsNetworkProxyHost(), Description: locales.SettingsNetworkProxyHostDesc(), Control: func() { n.hostPort(c) }},
			kit.SettingRow{Label: locales.SettingsNetworkProxyAuth(), Description: locales.SettingsNetworkProxyAuthDesc(), Control: func() { n.auth(c) }},
			kit.SettingRow{
				Label: locales.SettingsNetworkProxyBypass(), Description: locales.SettingsNetworkProxyBypassDesc(),
				Control: func() {
					n.field(c.Key("bypass"), &n.bypass, kit.SettingsInputSpec{Name: locales.SettingsNetworkProxyBypass()},
						func() string { return n.proxyNow().Bypass },
						func(text string) { n.setProxy(func(p *settings.NetworkProxy) { p.Bypass = text }) })
				},
			},
		)
	}
	l.SettingSection(c, locales.SettingsNetworkProxy(), rows)
}

// maxControlWidth 是 max-w-md（28rem）：代理地址、认证、绕过列表的控件区上限。
func (n *Network) maxControlWidth() float32 { return n.Look.Rem(28) }

// hostPort 是「代理地址」行的控件：主机输入（flex-1）加端口输入（w-24），中间 gap-2。
func (n *Network) hostPort(c ui.C) {
	l := n.Look
	ui.Row(c.Key("host-port")).FillWidth().MaxWidth(n.maxControlWidth()).Items(ui.Center).Gap(l.Space(2)).Children(func() {
		n.field(c.Key("host"), &n.host, kit.SettingsInputSpec{Name: locales.SettingsNetworkProxyHost(), Placeholder: "127.0.0.1", Grow: true},
			func() string { return n.proxyNow().Host },
			func(text string) { n.setProxy(func(p *settings.NetworkProxy) { p.Host = text }) })
		n.field(c.Key("port"), &n.port, kit.SettingsInputSpec{Name: locales.SettingsNetworkProxyPort(), Width: l.Rem(6)}, // w-24
			func() string { return strconv.Itoa(n.proxyNow().Port) },
			func(text string) { n.setProxy(func(p *settings.NetworkProxy) { p.Port = parseNumber(text) }) })
	})
}

// auth 是「代理认证」行的控件：用户名与口令两列等宽（grid-cols-2 gap-2）。口令框带显示/隐藏按钮，默认是圆点。
func (n *Network) auth(c ui.C) {
	l := n.Look
	ui.Row(c.Key("auth")).FillWidth().MaxWidth(n.maxControlWidth()).Items(ui.Center).Gap(l.Space(2)).Children(func() {
		n.field(c.Key("username"), &n.username, kit.SettingsInputSpec{
			Name: locales.SettingsNetworkProxyUsername(), Placeholder: locales.SettingsNetworkProxyUsername(), Grow: true,
		}, func() string { return n.proxyNow().Username },
			func(text string) { n.setProxy(func(p *settings.NetworkProxy) { p.Username = text }) })

		spec := kit.SettingsInputSpec{
			Name: locales.SettingsNetworkProxyPassword(), Placeholder: locales.SettingsNetworkProxyPassword(), Grow: true,
			PlaceholderInk: l.T.MutedForeground.WithAlpha(0.6), // placeholder:text-muted-foreground/60
		}
		box := l.SettingsPasswordInput(c.Key("password"), &n.password, &n.showPassword, spec, locales.CommonShowPassword(), locales.CommonHidePassword())
		n.control(box, &n.password, func() string { return n.proxyNow().Password },
			func(text string) { n.setProxy(func(p *settings.NetworkProxy) { p.Password = text }) })
	})
}

// field 声明一个受控的设置文本框并接上编辑。
func (n *Network) field(c ui.C, f *ui.Field, spec kit.SettingsInputSpec, stored func() string, apply func(text string)) {
	n.control(n.Look.SettingsInput(c, f, spec), f, stored, apply)
}

// control 让文本框受控：编辑交给 apply（写入设置），之后文字回到设置里的值——写入被归一化改写、被拒绝、失败时都如此。
// 设置从别处改了（或首次出现）时文字也跟着变。文字没变就不动它，免得把插入符拽到末尾。
func (n *Network) control(box ui.Box, f *ui.Field, stored func() string, apply func(text string)) {
	controlled(box, f, stored, apply)
}

// controlled 是受控文本框的共用写法：box 是 SettingsInput 返回的输入元素。
func controlled(box ui.Box, f *ui.Field, stored func() string, apply func(text string)) {
	if box.Changed() {
		apply(f.Text())
	}
	if s := stored(); f.Text() != s {
		f.SetText(s)
	}
}

// parseNumber 是渲染层 Number(text) 的对应物：整数照读，读不出来（空串、字母）给 0——Store 的归一化把无效端口换成默认值。
func parseNumber(text string) int {
	v, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0
	}
	return v
}

func (n *Network) mirror(c ui.C) {
	l := n.Look
	l.SettingSection(c, locales.SettingsNetworkMirrorSource(), []kit.SettingRow{{
		Label: locales.SettingsTryMirrorSource(), Description: locales.SettingsMirrorSourceRisk(),
		Control: func() {
			on := n.Store.Values().UI.UseUpdateMirror
			if l.Switch(c, &on, locales.SettingsTryMirrorSource()).Clicked() {
				next := on
				n.save(func(v *settings.Values) { v.UI.UseUpdateMirror = next })
			}
		},
	}})
}
