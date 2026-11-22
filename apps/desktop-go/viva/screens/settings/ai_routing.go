package settings

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// aiRouteDef 是一条模型路由的界面定义（MODEL_ROUTE_DEFINITIONS）：状态总览卡片与场景默认模型两处共用，保证一一对应。
type aiRouteDef struct {
	route            string
	label, desc, tag func() string
	icon             gfx.Icon
}

var aiRouteDefs = []aiRouteDef{
	{services.AIRouteMainText, locales.SettingsAiWorkbenchRouteMainText, locales.SettingsAiWorkbenchRouteMainTextDesc, locales.SettingsAiWorkbenchBadgeMainText, icons.Brain},
	{services.AIRouteFastText, locales.SettingsAiWorkbenchRouteFastText, locales.SettingsAiWorkbenchRouteFastTextDesc, locales.SettingsAiWorkbenchBadgeFastText, icons.Zap},
	{services.AIRouteVisionText, locales.SettingsAiWorkbenchRouteVisionText, locales.SettingsAiWorkbenchRouteVisionTextDesc, locales.SettingsAiWorkbenchBadgeVisionText, icons.Eye},
	{services.AIRouteEmbedding, locales.SettingsAiWorkbenchRouteEmbedding, locales.SettingsAiWorkbenchRouteEmbeddingDesc, locales.SettingsAiWorkbenchBadgeEmbedding, icons.Database},
	{services.AIRouteAudioText, locales.SettingsAiWorkbenchRouteAudioText, locales.SettingsAiWorkbenchRouteAudioTextDesc, locales.SettingsAiWorkbenchBadgeAudioText, icons.AudioLines},
	{services.AIRouteImageGen, locales.SettingsAiWorkbenchRouteImageGen, locales.SettingsAiWorkbenchRouteImageGenDesc, locales.SettingsAiWorkbenchBadgeImageGen, icons.Image},
}

// routeBadges 是每个模型担任的路由徽标（modelScenarioBadges）：路由当前解析到哪个模型，那个模型行上就挂对应徽标。
func (a *AI) routeBadges() map[string][]string {
	out := map[string][]string{}
	for _, d := range aiRouteDefs {
		if m, ok := a.cfg.ResolveRoute(d.route); ok {
			out[m.ID] = append(out[m.ID], d.tag())
		}
	}
	return out
}

// routing 是「模型路由」面板：顶部操作、状态总览、场景默认模型、用量。
func (a *AI) routing(c ui.C, groups []services.AIEndpointGroup) {
	l := a.Look
	ui.Column(c).FillWidth().Gap(l.Space(6)).Children(func() {
		a.routingActions(c.Key("actions"), groups)
		a.statusOverview(c.Key("overview"))
		a.scenarioDefaults(c.Key("scenarios"))
		a.usageSection(c.Key("usage"))
	})
}

// routingActions 是右对齐的三个主操作：快速配置、测试默认模型、添加模型。没有任何模型时主色给快速配置。
func (a *AI) routingActions(c ui.C, groups []services.AIEndpointGroup) {
	l := a.Look
	t := l.T
	empty := len(a.cfg.Models) == 0
	ui.Row(c).FillWidth().Justify(ui.End).Items(ui.Center).Gap(l.Space(3)).Children(func() {
		base := shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightMedium)
		shadow := kit.ShadowSM

		quick := primaryBtn(l, base)
		if !empty {
			quick = outlineBtn(l, base, t.Accent)
			quick.Border, quick.Bg, quick.Fg = t.Primary.WithAlpha(0.3), t.Primary.WithAlpha(0.05), t.Foreground
		}
		quick.Shadows = shadow
		if btn(l, c.Key("quick"), kit.ButtonSpec{Label: locales.SettingsQuickSetupOpen(), Icon: icons.Sparkles, IconSize: l.Rem(1)}, quick, false) {
			a.openWizard()
		}

		test := outlineBtn(l, base, t.Accent)
		test.Bg, test.FgHover, test.Shadows = t.Background, t.AccentForeground, shadow
		if btn(l, c.Key("test"), kit.ButtonSpec{Label: locales.SettingsAiWorkbenchTestDefault(), Icon: icons.TestTube, IconSize: l.Rem(1), BusySize: l.Rem(1)}, test, a.testingDefault) {
			a.testDefault()
		}

		add := primaryBtn(l, base)
		if empty {
			add = outlineBtn(l, base, t.Accent)
			add.Bg, add.FgHover = t.Background, t.AccentForeground
		}
		add.Shadows = shadow
		if btn(l, c.Key("add"), kit.ButtonSpec{Label: locales.SettingsAddModel()}, add, false) {
			if len(groups) == 0 {
				a.openAddEndpoint()
			} else {
				a.openAddModel(groups[0])
			}
		}
	})
}

// statusOverview 是状态总览：每条路由当前解析到的模型，一张卡（md 两列，xl 三列）。
func (a *AI) statusOverview(c ui.C) {
	l := a.Look
	t := l.T
	cols := 1
	switch vw := l.ViewportWidth(c); {
	case vw >= 1280:
		cols = 3
	case vw >= 768:
		cols = 2
	}
	ui.Column(c).FillWidth().Children(func() {
		l.Heading(c.Key("title"), 3, locales.SettingsAiWorkbenchStatusOverview(), kit.TextSM, kit.WeightMedium, t.MutedForeground).MarginBottom(l.Space(3))
		ui.Column(c.Key("grid")).FillWidth().Gap(l.Space(4)).Children(func() {
			for start := 0; start < len(aiRouteDefs); start += cols {
				end := min(start+cols, len(aiRouteDefs))
				ui.Row(c.Key("row" + itoa(start))).FillWidth().Gap(l.Space(4)).Children(func() {
					for i := start; i < start+cols; i++ {
						if i >= end {
							// 补空位，让最后一行的卡与上面的列等宽。
							ui.Row(c.Key("pad" + itoa(i))).Grow(1).Basis(0)
							continue
						}
						a.statusCard(c.Key(aiRouteDefs[i].route), aiRouteDefs[i])
					}
				})
			}
		})
	})
}

// statusCard 是一张状态卡：右上角状态点，图标加路由名，下面是当前模型名（没有就是「未配置」）。
func (a *AI) statusCard(c ui.C, d aiRouteDef) {
	l := a.Look
	t := l.T
	m, ok := a.cfg.ResolveRoute(d.route)
	value, tone := locales.SettingsAiWorkbenchNotConfigured(), toneWarning
	if ok {
		value, tone = services.ModelDisplayName(m, a.cfg.Providers, providerLabel), toneReady
	}
	ui.Stack(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
		ui.Column(c.Key("card")).FillWidth().Pad(l.Space(3.5) + 1).
			Draw(func(dc *ui.DrawContext) {
				l.PaintSurface(dc, kit.Surface{Bg: t.Card, Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75), Shadows: kit.ShadowSM})
			}).Children(func() {
			ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
				l.Icon(c.Key("icon"), d.icon, l.Rem(1), t.MutedForeground)
				l.Text(c.Key("label"), d.label(), kit.TextXS, kit.WeightMedium, t.MutedForeground)
			})
			l.Text(c.Key("value"), value, kit.TextLG, kit.WeightSemibold, t.Foreground, kit.Ellipsis()).MinWidth(0).MarginTop(l.Space(1.5))
		})
		ui.Row(c.Key("dot")).Absolute().Top(l.Space(3)).Right(l.Space(3)).Children(func() {
			statusDot(l, c, l.Rem(0.375), toneColor(tone), 3, 0.2)
		})
	})
}

// scenarioDefaults 是「模型路由」卡：每个场景一行，右边是候选模型的下拉（没有候选时置灰），第一项「跟随全局默认」。
func (a *AI) scenarioDefaults(c ui.C) {
	l := a.Look
	wide := l.ViewportWidth(c) >= 768
	l.SettingSectionBody(c, locales.SettingsAiWorkbenchModelRouting(), func() {
		for i, d := range aiRouteDefs {
			a.scenarioRow(c.Key(d.route), i, d, wide, i == len(aiRouteDefs)-1)
		}
	})
}

func (a *AI) scenarioRow(c ui.C, idx int, d aiRouteDef, wide, last bool) {
	l := a.Look
	t := l.T
	candidates := a.cfg.RouteCandidates(d.route)
	options := []kit.SelectOption{{Value: "", Label: locales.SettingsAiWorkbenchFollowGlobalDefault()}}
	for _, m := range candidates {
		options = append(options, kit.SelectOption{Value: m.ID, Label: services.ModelDisplayName(m, a.cfg.Providers, providerLabel)})
	}
	value := a.cfg.Routes[d.route]
	label := d.label()
	l.SettingBlock(c, last, func() {
		row := ui.Row(c.Key("row"))
		if !wide {
			row = ui.Column(c.Key("row")).Gap(l.Space(3))
		}
		row.FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).Pad(l.Space(4)).Children(func() {
			ui.Column(c.Key("text")).Shrink(1).MinWidth(0).Gap(l.Space(1)).Children(func() {
				l.Text(c.Key("label"), label, kit.TextSM, kit.WeightMedium, t.Foreground)
				l.Text(c.Key("desc"), d.desc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			})
			width := float32(-1) // w-full
			if wide {
				width = 280 // md:w-[280px]
			}
			if len(candidates) == 0 {
				display := ""
				for _, o := range options {
					if o.Value == value {
						display = o.Label
					}
				}
				a.disabledSelect(c.Key("select"), label, display, width)
				return
			}
			if picked, changed := l.Select(c.Key("select"), &a.routeSel[idx], kit.SelectSpec{Label: label, Value: value, Options: options, Width: width}); changed && picked != value {
				a.setRoute(d.route, picked)
			}
		})
	})
}

// legacyBanner 是只有旧版配置时的迁移提示（`rounded-lg border border-amber-500/30 bg-amber-500/5 p-3`）。
func (a *AI) legacyBanner(c ui.C) {
	l := a.Look
	t := l.T
	amber := tokens.Tailwind("amber-500")
	ui.Row(c).FillWidth().Items(ui.Start).Justify(ui.Between).Gap(l.Space(4)).Pad(l.Space(3) + 1).MarginBottom(l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: amber.WithAlpha(0.05), Border: amber.WithAlpha(0.3), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		ui.Column(c.Key("text")).Shrink(1).MinWidth(0).Children(func() {
			l.Text(c.Key("title"), locales.SettingsAiWorkbenchLegacyBannerTitle(), kit.TextSM, kit.WeightMedium, t.Foreground)
			l.Text(c.Key("desc"), locales.SettingsAiWorkbenchLegacyBannerDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
		})
		st := primaryBtn(l, shape(l, 2, 3, 2, kit.TextXS, kit.WeightMedium))
		st.Radius = mdRadius(l)
		if btn(l, c.Key("import"), kit.ButtonSpec{Label: locales.SettingsAiWorkbenchImportLegacy()}, st, false) {
			a.importLegacy()
		}
	})
}

// disabledSelect 是没有候选模型时置灰的下拉（`disabled:opacity-50`）：外观同 Select 的触发器，可访问树里是禁用的 combobox。
func (a *AI) disabledSelect(c ui.C, label, display string, width float32) {
	l := a.Look
	t := l.T
	box := ui.Row(c).Height(l.Rem(2.5))
	if width < 0 {
		box = box.FillWidth()
	} else {
		box = box.Width(width).Shrink(0)
	}
	box.Items(ui.Center).Gap(l.Rem(0.5)).PadSides(0, l.Rem(0.75), 0, l.Rem(0.75)).Opacity(0.5).
		HitWith(ui.ID(c), label, gfx.HitOptions{Role: gfx.RoleCombobox, Expandable: true, Value: display, Disabled: true, Cursor: gfx.PointerCursorNotAllowed}).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: l.Rem(0.75)}) }).
		Children(func() {
			l.Text(c.Key("value"), display, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Grow(1).MinWidth(0)
			l.Icon(c.Key("chevron"), icons.ChevronDown, l.Rem(1), t.MutedForeground).Shrink(0)
		})
}
