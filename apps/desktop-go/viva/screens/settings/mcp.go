package settings

import (
	"context"
	"errors"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// errMCPUnavailable 是外壳没有接上 MCP 服务时的失败原因（进「查看详情」与日志，不进界面文案）。
var errMCPUnavailable = errors.New("the MCP service is not available in this build")

// CollectionSource 是范围选择器列出合集的来源；services.Library 满足它。
type CollectionSource interface {
	Collections(ctx context.Context) ([]services.LibraryCollection, error)
}

// MCP 是「MCP 接入」页（渲染层 McpSettings）：服务状态、可访问的合集、接入 AI 客户端三块。
//
// 渲染层与外壳都没有「启停 MCP 服务」的开关，也没有访问令牌：归知的 MCP server 随应用打包、由客户端按需拉起，
// 范围文件由 server 每次调用时重读。这一页只做渲染层有的事——读取接入信息、改可访问范围（立即落盘）、生成并复制客户端配置、一键写入配置文件。
type MCP struct {
	Look  *kit.Look
	Guard kit.Guard
	// Service 是 MCP 服务；nil 表示外壳没接：页面如实显示「读取失败」。
	Service services.MCP
	// Collections 给范围选择器列出合集；nil 时只有「未归合集」一行。
	Collections CollectionSource
	// Wake 请求窗口重绘：后台结果回到界面线程前调用。可为 nil。
	Wake func()

	started    bool
	config     *services.MCPConfig
	scope      services.MCPScope
	collection []services.LibraryCollection
	checking   bool
	loadFailed bool
	w          worker
	clients    mcpClients
}

// Close 在分区被切走时调用：取消读取请求，丢弃还没送达的结果；进行中的写入配置不被打断。
func (m *MCP) Close() {
	m.w.close()
	m.clients.close()
}

// refresh 读取接入信息与范围（渲染层 refresh）；读不出来和「没配置」在界面上长得一样，所以失败要画出来。
func (m *MCP) refresh() {
	m.checking = true
	svc := m.Service
	m.w.load(m.Wake, func(ctx context.Context) func() {
		var (
			cfg   services.MCPConfig
			scope services.MCPScope
			err   = errMCPUnavailable
		)
		if svc != nil {
			if cfg, err = svc.Config(ctx); err == nil {
				scope, err = svc.Scope(ctx)
			}
		}
		if err != nil && ctx.Err() == nil {
			m.Guard.Background(locales.SettingsMcp(), err)
		}
		return func() {
			m.checking = false
			if err != nil {
				m.loadFailed = true
				return
			}
			m.config, m.scope, m.loadFailed = &cfg, scope, false
		}
	})
}

// loadCollections 读取合集列表（范围选择器）；失败只进日志，选择器仍可用「未归合集」。
func (m *MCP) loadCollections() {
	src := m.Collections
	if src == nil {
		return
	}
	m.w.load(m.Wake, func(ctx context.Context) func() {
		list, err := src.Collections(ctx)
		if err != nil {
			if ctx.Err() == nil {
				m.Guard.Background(locales.SettingsMcpScopeTitle(), err)
			}
			return func() {}
		}
		return func() { m.collection = list }
	})
}

// applyScope 立刻落盘（MCP server 每次工具调用都会重读这个文件，留一颗「保存」按钮只会让人以为改了却没生效）：
// 先乐观更新，失败则回到原值并弹出带原因的错误提示。
func (m *MCP) applyScope(next services.MCPScope) {
	previous := m.scope
	m.scope = next
	svc, g := m.Service, m.Guard
	m.w.act(m.Wake, func() func() {
		saved, err := services.MCPScope{}, errMCPUnavailable
		if svc != nil {
			saved, err = svc.SetScope(context.Background(), next)
		}
		if err != nil {
			g.Fail(locales.SettingsMcpScopeSaveFailed(), locales.SettingsMcpScopeTitle(), err)
		}
		return func() {
			if err != nil {
				m.scope = previous
				return
			}
			m.scope = saved
		}
	})
}

// relaxed 是 Tailwind 的 leading-relaxed：保持字号，行高取 1.625 倍字号（text-sm 的 20px 变 22.75px，text-xs 的 16px 变 19.5px）。
func relaxed(st kit.TextStyle) kit.TextStyle {
	return kit.TextStyle{Size: st.Size, Line: st.Size * 1.625}
}

// Page 声明整页。
func (m *MCP) Page(c ui.C) {
	l := m.Look
	m.w.drain()
	if !m.started {
		m.started = true
		m.refresh()
		m.loadCollections()
	}
	l.SettingsContent(c, locales.SettingsMcp(), func() {
		switch {
		case m.loadFailed:
			m.failedView(c)
		case m.config == nil:
			ui.Row(c.Key("loading")).FillWidth().Height(l.Rem(12)).Items(ui.Center).Justify(ui.Center).Children(func() {
				l.Spinner(c.Key("spinner"), l.Rem(2), l.T.MutedForeground)
			})
		default:
			m.readyView(c)
		}
	})
}

func (m *MCP) failedView(c ui.C) {
	l := m.Look
	ui.Column(c.Key("failed")).FillWidth().Gap(l.Space(4)).Children(func() {
		l.Text(c, locales.SettingsMcpLoadFailed(), kit.TextSM, kit.WeightNormal, l.T.Destructive, kit.Wrapped())
		m.recheck(c, kit.TextSM, 2.25, 4)
	})
}

// recheck 是「重新检查」按钮：失败页是 h-9 text-sm 的描边钮，状态卡里是 h-8 text-xs 的小钮；检查中转圈并禁用。
func (m *MCP) recheck(c ui.C, text kit.TextStyle, hRem, iconRem float32) {
	l := m.Look
	t := l.T
	small := text == kit.TextXS
	pad := float32(3)
	if small {
		pad = 2.5
	}
	s := outlineBtn(l, shape(l, hRem, pad, 1.5, text, kit.WeightNormal), t.Muted.WithAlpha(0.6))
	if small {
		s.Fg, s.FgHover = t.MutedForeground, t.Foreground
	}
	ic, size := icons.RotateCw, l.Rem(iconRem/4)
	if btn(l, c.Key("recheck"), kit.ButtonSpec{Label: locales.SettingsMcpRecheck(), Icon: ic, IconSize: size}, s, m.checking) {
		m.refresh()
	}
}

func (m *MCP) readyView(c ui.C) {
	l := m.Look
	t := l.T
	cfg := *m.config
	l.SettingSections(c, func() {
		l.Text(c.Key("desc"), locales.SettingsMcpDesc(), relaxed(kit.TextSM), kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		l.SettingSectionBody(c.Key("status"), locales.SettingsMcpStatusTitle(), func() { m.status(c, cfg) })
		if !cfg.Available {
			return
		}
		l.SettingSectionBody(c.Key("scope"), locales.SettingsMcpScopeTitle(), func() {
			ui.Column(c.Key("scope-body")).FillWidth().PadSides(l.Space(3.5), l.Space(4), l.Space(3.5), l.Space(4)).Children(func() {
				l.Text(c.Key("scope-desc"), locales.SettingsMcpScopeDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginBottom(l.Space(3))
				m.scopePicker(c.Key("picker"))
			})
		})
		l.SettingSectionBody(c.Key("clients"), locales.SettingsMcpClientsTitle(), func() {
			ui.Column(c.Key("clients-body")).FillWidth().PadSides(l.Space(3.5), l.Space(4), l.Space(3.5), l.Space(4)).Children(func() {
				m.clients.Look, m.clients.Guard, m.clients.Service, m.clients.Wake = l, m.Guard, m.Service, m.Wake
				m.clients.Page(c.Key("tabs"), cfg)
			})
		})
	})
}

// status 是「服务状态」卡片内容：图标、组件就绪与否、路径（要拿去和配置文件核对，截断了等于没给，所以折行显示）与重新检查。
func (m *MCP) status(c ui.C, cfg services.MCPConfig) {
	l := m.Look
	t := l.T
	amber := tokens.Tailwind("amber-500")
	amberText := tokens.Tailwind("amber-600")
	if l.Dark {
		amberText = tokens.Tailwind("amber-400")
	}
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2.5)).PadSides(l.Space(3.5), l.Space(4), l.Space(3.5), l.Space(4)).Children(func() {
		if cfg.Available {
			l.Icon(c.Key("ok"), icons.CircleCheck, l.Rem(1), tokens.Tailwind("emerald-500")).Shrink(0)
		} else {
			l.Icon(c.Key("missing"), icons.PlugZap, l.Rem(1), amber).Shrink(0)
		}
		ui.Column(c.Key("text")).Grow(1).MinWidth(0).Children(func() {
			title := locales.SettingsMcpStatusReady()
			if !cfg.Available {
				title = locales.SettingsMcpStatusMissing()
			}
			l.Text(c.Key("title"), title, kit.TextSM, kit.WeightMedium, t.Foreground)
			l.TextBreakAll(c.Key("path"), cfg.ServerPath, relaxed(kit.TextXS), kit.WeightNormal, t.MutedForeground)
			if !cfg.Available {
				l.Text(c.Key("hint"), locales.SettingsMcpMissing(), kit.TextXS, kit.WeightNormal, amberText, kit.Wrapped()).MarginTop(l.Space(1))
			}
		})
		m.recheck(c.Key("action"), kit.TextXS, 2, 3.5)
	})
}
