package settings

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// aside 是左侧栏（`app-wallpaper-panel` 14rem）：「模型路由」入口、供应商搜索、供应商列表与底部的「添加供应商」。
// 窄窗口时叠在内容上方，高度与面板按 grid 的 auto 行规则分配（见 aiRowsLayout），列表自己滚动。
func (a *AI) aside(c ui.C, groups []services.AIEndpointGroup, wide bool) {
	l := a.Look
	t := l.T
	col := ui.Column(c).Shrink(0)
	// 边框：渲染层两个分支的 aside 类名不同——有供应商时是 `border-b lg:border-b-0 lg:border-r`（窄窗口下边框、宽窗口右边框），
	// 空配置时只有 `border-r`（窄窗口也是右边框，没有下边框）。右边框让内容盒窄 1px（800×600 实测空配置侧栏内部 495 宽），
	// 下边框的 1px 又是窄窗口 grid 行的基础高度（空配置侧栏 256 / 面板 296，有供应商 256.5 / 295.5，见 aiRowsLayout）。
	right := wide || len(groups) == 0
	a.rows.baseAside = 0
	if !right {
		a.rows.baseAside = 1
	}
	if wide {
		col = col.Width(l.Rem(14)).FillHeight().PadRight(1)
	} else if right {
		col = col.FillWidth().PadRight(1) // 窄窗口的高度由 aiRowsLayout 分配
	} else {
		col = col.FillWidth().PadBottom(1)
	}
	col.Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.UiPanelBg, Shadow: t.UiPanelShadow})
		if right {
			l.PaintEdge(d, kit.EdgeRight, 1, t.UiPanelBorder)
		} else {
			l.PaintEdge(d, kit.EdgeBottom, 1, t.UiPanelBorder)
		}
	}).Children(func() {
		a.asideTop(c.Key("top"))
		if len(groups) == 0 {
			// 没有供应商：说明文字在剩余高度里居中（flex min-h-0 flex-1 items-center justify-center p-6 text-center）；窄窗口里侧栏被压到 grid 分给它的高度时，
			// 这一块按 flex-shrink 让出高度，但缩不到内边距以下（min-h-0 只去掉内容的最小高度，p-6 的上下 48px 还在）：
			// 800×600 的实测是列表停在 48px，说明文字溢出内容盒仍画出来，「添加供应商」被挤出侧栏下沿 34px（渲染层 facts：侧栏 256、添加块 225～290）。
			ui.Row(c.Key("list")).FillWidth().Grow(1).Shrink(1).MinHeight(2 * l.Space(6)).Items(ui.Center).Justify(ui.Center).Pad(l.Space(6)).Children(func() {
				l.Text(c.Key("empty"), locales.SettingsAiWorkbenchNoModels(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered())
			})
		} else {
			pad := l.Space(2)
			list := l.ScrollAreaView(c.Key("list"), [4]float32{pad, pad, pad, pad}, func(v kit.ScrollView) {
				// 窄窗口：列表自然高度（flex-1 min-h-0 overflow-y-auto 的 max-content 贡献）是侧栏自然高度的一部分，
				// 滚动区自己的预测高度不含内容，取上一帧布局量到的内容高度；变了就补一帧。
				if !wide && a.rows.setAsideList(v.Content+2*pad) {
					c.Canvas().RequestFrame()
				}
				a.providerList(c.Key("rows"), groups)
			}).FillWidth().Grow(1).MinHeight(2 * pad) // min-h-0 的滚动区仍缩不到 p-2 的上下内边距以下
			if !wide {
				list.Height(a.rows.asideList).Shrink(1)
			}
		}
		ui.Column(c.Key("add")).FillWidth().Pad(l.Space(3)).
			PadSides(l.Space(3)+1, l.Space(3), l.Space(3), l.Space(3)).
			DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
			Children(func() {
				st := outlineBtn(l, shape(l, 2.5, 3, 2, kit.TextSM, kit.WeightMedium), t.Muted)
				st.Bg = t.Background
				name := locales.SettingsAddProvider()
				if l.Button(c.Key("btn"), kit.ButtonSpec{Label: name, Icon: icons.Plus, IconSize: l.Rem(1), Full: true}, st).Clicked() {
					a.openAddEndpoint()
				}
			})
	})
}

// asideTop 是侧栏上半：两个分组标题、模型路由入口与搜索框（border-b p-3）。
func (a *AI) asideTop(c ui.C) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Pad(l.Space(3)).PadSides(l.Space(3), l.Space(3), l.Space(3)+1, l.Space(3)).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			sectionDivider(l, c.Key("d1"), locales.SettingsAiWorkbenchSubmenuModelConfig(), 0, l.Space(3))
			a.routingEntry(c.Key("routing"))
			sectionDivider(l, c.Key("d2"), locales.SettingsProviderName(), l.Space(3), 0)
			// 分组标题的 my-3 与搜索框的 mt-3（有供应商时）是相邻块的外边距，折叠成 12px；没有供应商时只有 my-3，也是 12px。
			a.searchBox(c.Key("search")).MarginTop(l.Space(3))
		})
}

// routingEntry 是「模型路由」按钮：h-10 w-full gap-3 px-3 text-sm；选中时 primary 底。
func (a *AI) routingEntry(c ui.C) {
	l := a.Look
	t := l.T
	label := locales.SettingsAiWorkbenchModelRouting()
	active := a.panel == panelRouting
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Height(l.Rem(2.5)).Items(ui.Center).Gap(l.Space(3)).PadSides(0, l.Space(3), 0, l.Space(3)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand}) // 渲染层是普通按钮，选中只靠底色，没有 aria-pressed
	l.TabStop(box)
	fg := t.Foreground
	surface := kit.Surface{Radius: l.Rem(0.75)}
	switch {
	case active:
		fg, surface.Bg = t.PrimaryForeground, t.Primary
	case box.State().Hovered:
		surface.Bg = t.Background
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		l.Icon(c.Key("icon"), icons.Route, l.Rem(1), fg)
		l.Text(c.Key("label"), label, kit.TextSM, kit.WeightNormal, fg)
	})
	if box.Clicked() {
		a.panel = panelRouting
	}
}

// searchBox 是供应商搜索框：h-10 w-full rounded-lg border bg-background pl-9 pr-3 text-sm，左侧放大镜。
func (a *AI) searchBox(c ui.C) ui.Box {
	l := a.Look
	t := l.T
	ph := locales.SettingsSearchProvidersAndModels()
	return ui.Stack(c).FillWidth().Children(func() {
		aiField(l, c.Key("field"), &a.search, aiFieldSpec{
			Name: ph, Placeholder: ph, Height: l.Rem(2.5), Radius: l.Rem(0.75), Bg: t.Background, Border: t.Border, BorderW: 1,
			Left: l.Space(9), Right: l.Space(3), PlaceholderInk: tokens.Tailwind("gray-400"), // 渲染层没写占位符颜色类：用 preflight 的 gray-400
		})
		ui.Row(c.Key("icon")).Absolute().Left(l.Space(3)).Top(0).Bottom(0).Items(ui.Center).Children(func() {
			l.Icon(c, icons.Search, l.Rem(1), t.MutedForeground)
		})
	})
}

// providerList 是供应商行；搜索按显示名、主机、旗下模型名过滤。没有任何供应商时由 aside 给空态说明。
func (a *AI) providerList(c ui.C, groups []services.AIEndpointGroup) {
	l := a.Look
	ui.Column(c).FillWidth().Gap(l.Space(1)).Children(func() {
		for _, g := range a.filterGroups(groups) {
			a.providerRow(c.Key(g.Key), g)
		}
	})
}

// filterGroups 按搜索词过滤端点分组。
func (a *AI) filterGroups(groups []services.AIEndpointGroup) []services.AIEndpointGroup {
	q := strings.ToLower(strings.TrimSpace(a.search.Text()))
	if q == "" {
		return groups
	}
	var out []services.AIEndpointGroup
	for _, g := range groups {
		hit := strings.Contains(strings.ToLower(g.DisplayName(providerLabel)), q) ||
			strings.Contains(strings.ToLower(services.EndpointHost(g.APIURL, "")), q)
		for _, m := range g.Models {
			hit = hit || strings.Contains(strings.ToLower(m.Name+" "+m.Model), q)
		}
		if hit {
			out = append(out, g)
		}
	}
	return out
}

// protocolLabel 是协议的显示名。
func protocolLabel(protocol string) string {
	switch protocol {
	case services.AIProtocolGemini:
		return locales.SettingsProtocolGeminiCompatible()
	case services.AIProtocolAnthropic:
		return locales.SettingsProtocolAnthropicCompatible()
	}
	return locales.SettingsProtocolOpenAICompatible()
}

// providerRow 是一行供应商：h-12，品牌徽标、名称与主机、状态点和模型数。选中行带背景与 1px 边框。
func (a *AI) providerRow(c ui.C, g services.AIEndpointGroup) {
	l := a.Look
	t := l.T
	status := a.endpointStatus(g)
	selected := a.panel == panelProvider && g.Key == a.selected
	name := g.DisplayName(providerLabel)
	host := services.EndpointHost(g.APIURL, protocolLabel(g.APIProtocol))
	detail := host
	if g.ProviderConfigID == services.AIFunasrProviderID {
		detail = locales.SettingsAiWorkbenchBuiltin() + " · " + host
	}
	if !g.Enabled {
		detail = locales.SettingsAiWorkbenchDisabled() + " · " + detail
	}
	id := ui.ID(c)
	category := services.EndpointCategory(g.Provider, g.Models)
	box := ui.Row(c).FillWidth().Height(l.Rem(3)).Items(ui.Center).Gap(l.Space(2)).PadSides(0, l.Space(2)+1, 0, l.Space(2)+1).
		HitWith(id, category+" "+name+" "+detail+" "+itoa(len(g.Models)), gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand}) // 名称由内容拼成：品牌图标 alt（分类名）+ 名称 + 主机 + 模型数
	l.TabStop(box)
	surface := kit.Surface{Radius: l.Rem(0.5), BorderW: 1}
	switch {
	case selected:
		surface.Bg, surface.Border, surface.Shadows = t.Background, t.Border, kit.ShadowSM
	case box.State().Hovered:
		surface.Bg = t.Background.WithAlpha(0.7)
	}
	if !g.Enabled {
		box = box.Opacity(0.6)
	}
	dot := toneColor(status.tone)
	if !g.Enabled {
		dot = t.MutedForeground.WithAlpha(0.4)
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		brandBadge(l, c.Key("brand"), category, l.Rem(1.75), 17, t.Muted, mdRadius(l)) // h-7 w-7 rounded-md bg-muted，图标 17px
		ui.Column(c.Key("text")).Grow(1).MinWidth(0).Children(func() {
			l.Text(c.Key("name"), name, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Ellipsis()).MinWidth(0)
			l.TextPx(c.Key("detail"), detail, 11, 16.5, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).MinWidth(0)
		})
		ui.Row(c.Key("meta")).Shrink(0).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
			statusDot(l, c.Key("dot"), l.Rem(0.375), dot, 0, 0)
			countPill(l, c.Key("count"), len(g.Models))
		})
	})
	if box.Clicked() {
		a.selected, a.panel = g.Key, panelProvider
	}
}

// countPill 是模型数徽标：min-w-5 rounded-full bg-muted px-1.5 py-0.5 text-center text-[10px] font-medium。
func countPill(l *kit.Look, c ui.C, n int) {
	t := l.T
	ui.Row(c).MinWidth(l.Rem(1.25)).Items(ui.Center).Justify(ui.Center).PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: d.Rect.Height / 2}) }).
		Children(func() { l.TextPx(c.Key("n"), itoa(n), 10, 15, kit.WeightMedium, t.MutedForeground) })
}
