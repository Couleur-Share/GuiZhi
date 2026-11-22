package settings

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// Tailwind 的 orange-500 / orange-600（「提交问题」按钮）。
var (
	orange500 = tokens.RGBA(249, 115, 22, 1)
	orange600 = tokens.RGBA(234, 88, 12, 1)
	white     = tokens.RGBA(255, 255, 255, 1)
)

// Page 声明整页：身份区、产品概览、检查更新、开源与联系、页脚，再加「加入预览通道」确认对话框（浮层）。
func (a *About) Page(c ui.C) {
	l := a.Look
	a.mb.drain()
	a.subscribe()
	l.SettingsContent(c, locales.SettingsAbout(), func() {
		l.SettingSections(c, func() {
			a.identity(c)
			a.overview(c)
			a.updates(c)
			ui.Column(c.Key("support")).FillWidth().Gap(l.Rem(1.5)).Children(func() {
				a.openSource(c)
				a.contact(c)
				a.otherWorks(c)
				a.developer(c)
			})
			a.footer(c)
		})
	})
	a.previewDialog(c)
}

// identity 是应用身份与版本状态：图标 64px rounded-2xl，名称 text-lg font-semibold，版本加通道，支持政策说明。
// 窄于 sm 竖排居中，不窄于 sm 横排（图标在左、文字左对齐）。
func (a *About) identity(c ui.C) {
	l := a.Look
	t := l.T
	wide := l.ViewportWidth(c) >= aboutBreakpoint
	pad := l.Space(2) // py-2
	body := func() {
		l.AppIcon(c, l.Rem(4), l.Rem(1))
		textCol := ui.Column(c.Key("text")).Shrink(1).MinWidth(0)
		if !wide {
			textCol = textCol.Items(ui.Center)
		}
		textCol.Children(func() {
			l.Heading(c, 2, "GuiZhi", kit.TextLG, kit.WeightSemibold, t.Foreground)
			version := a.Version
			if version == "" {
				version = "..."
			}
			line := locales.SettingsCurrentVersion(version) + " · " + a.channelLabel()
			opts := []kit.TextOpt{kit.Wrapped()}
			if !wide {
				opts = append(opts, kit.Centered())
			}
			l.Text(c.Key("version"), line, kit.TextSM, kit.WeightNormal, t.MutedForeground, opts...).MarginTop(l.Space(1))
			// text-xs leading-5：行高 1.25rem，不是 text-xs 自带的 1rem。
			l.TextPx(c.Key("policy"), locales.SettingsSupportPolicyDesc(), l.Rem(0.75), l.Rem(1.25), kit.WeightNormal, t.MutedForeground, opts...).
				MarginTop(l.Space(2)).MaxWidth(l.Rem(36))
		})
	}
	if wide {
		ui.Row(c.Key("identity")).FillWidth().Justify(ui.Center).Items(ui.Center).Gap(l.Space(3)).PadSides(pad, 0, pad, 0).Children(body)
		return
	}
	ui.Column(c.Key("identity")).FillWidth().Items(ui.Center).Gap(l.Space(3)).PadSides(pad, 0, pad, 0).Children(body)
}

// overviewCell 是产品概览的一格：图标、标题、说明。
type overviewCell struct {
	icon        gfx.Icon
	title, desc func() string
}

var overviewCells = []overviewCell{
	{icons.Database, locales.SettingsProductOverviewLocalTitle, locales.SettingsProductOverviewLocalDesc},
	{icons.ScanSearch, locales.SettingsProductOverviewCaptureTitle, locales.SettingsProductOverviewCaptureDesc},
	{icons.Sparkles, locales.SettingsProductOverviewAiTitle, locales.SettingsProductOverviewAiDesc},
}

// overview 是产品概览分区：三格（不窄于 sm 并排、格间竖线，窄时竖排、格间横线，线色 border/70），
// 桌面端再加「设置引导」一行。
func (a *About) overview(c ui.C) {
	l := a.Look
	wide := l.ViewportWidth(c) >= aboutBreakpoint
	l.SettingSectionBody(c.Key("overview"), locales.SettingsProductOverview(), func() {
		// 只创建一个容器：宽窄各建一个会让同层出现两个同键兄弟而 panic。
		var grid ui.Box
		if wide {
			grid = ui.Row(c.Key("grid")).FillWidth()
		} else {
			grid = ui.Column(c.Key("grid")).FillWidth()
		}
		grid.Children(func() {
			for i, cell := range overviewCells {
				a.overviewCell(c.Key(string(rune('a'+i))), i, wide, cell)
			}
		})
		if a.OpenSetupGuide != nil {
			l.SettingItem(c.Key("setup"), kit.SettingRow{
				Label: locales.SettingsSetupGuideTitle(), Description: locales.SetupOpenFromAboutDesc(),
				Control: func() {
					if a.primaryButton(c, locales.SetupOpenFromAbout(), icons.ListChecks, false) {
						a.OpenSetupGuide()
					}
				},
			}, true)
		}
	})
}

// overviewCell 画一格：px-4 py-4，图标 h-5 w-5 text-primary mb-2，h4 text-sm font-medium，p mt-1 text-xs leading-5。
func (a *About) overviewCell(c ui.C, i int, wide bool, cell overviewCell) {
	l := a.Look
	t := l.T
	pad := [4]float32{l.Space(4), l.Space(4), l.Space(4), l.Space(4)}
	edge := kit.EdgeTop
	if wide {
		edge = kit.EdgeLeft
	}
	if i > 0 { // divide-x / divide-y：第二格起有 1px 分隔线，占在格内
		if wide {
			pad[3]++
		} else {
			pad[0]++
		}
	}
	col := ui.Column(c).PadSides(pad[0], pad[1], pad[2], pad[3]).MinWidth(0)
	if wide {
		col = col.Grow(1).Basis(0)
	} else {
		col = col.FillWidth()
	}
	col.DrawOver(func(d *ui.DrawContext) {
		if i > 0 {
			l.PaintEdge(d, edge, 1, t.Border.WithAlpha(0.7))
		}
	}).Children(func() {
		l.Icon(c, cell.icon, l.Rem(1.25), t.Primary).MarginBottom(l.Space(2))
		l.Heading(c, 4, cell.title(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
		l.TextPx(c.Key("desc"), cell.desc(), l.Rem(0.75), l.Rem(1.25), kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
	})
}

// updates 是检查更新分区：自动检查、加入预览通道、检查更新三行。
func (a *About) updates(c ui.C) {
	l := a.Look
	l.SettingSection(c.Key("updates"), locales.SettingsCheckUpdate(), []kit.SettingRow{
		{
			Label: locales.SettingsAutoCheckUpdate(), Description: locales.SettingsAutoCheckUpdateDesc(),
			Control: a.uiSwitch(c, locales.SettingsAutoCheckUpdate(),
				func(u settings.UI) bool { return u.AutoCheckUpdate }, func(u *settings.UI, on bool) { u.AutoCheckUpdate = on }),
		},
		{
			Label: locales.SettingsJoinPreviewChannel(), Description: locales.SettingsJoinPreviewChannelDesc(),
			Control: a.previewSwitch(c),
		},
		{Label: locales.SettingsCheckUpdate(), Description: a.updateDescription(), Control: func() { a.updateControls(c) }},
	})
}

// uiSwitch 是绑定到界面设置布尔字段的开关：点击后写入；写入失败时设置不变，开关保持原状并提示。
func (a *About) uiSwitch(c ui.C, label string, get func(settings.UI) bool, set func(*settings.UI, bool)) func() {
	return func() {
		on := get(a.Store.Values().UI)
		if a.Look.Switch(c, &on, label).Clicked() {
			next := on
			save(a.Guard, a.Store, func(v *settings.Values) { set(&v.UI, next) })
		}
	}
}

// previewSwitch 是「加入预览通道」开关：关掉直接回到稳定通道（并记为用户显式选择）；打开要先过确认对话框。
func (a *About) previewSwitch(c ui.C) func() {
	return func() {
		on := a.Store.Values().UI.UpdateChannel == settings.ChannelPreview
		if !a.Look.Switch(c, &on, locales.SettingsJoinPreviewChannel()).Clicked() {
			return
		}
		if on {
			a.previewAsk = true // 打开：开关保持关，确认后才写入
			return
		}
		a.previewAsk = false
		a.setChannel(settings.ChannelStable)
	}
}

// setChannel 写入更新通道（渲染层 setUpdateChannel：同时标记为用户显式选择，之后不再按版本号推断）。
func (a *About) setChannel(ch settings.UpdateChannel) bool {
	return save(a.Guard, a.Store, func(v *settings.Values) {
		v.UI.UpdateChannel, v.UI.UpdateChannelExplicitlySet = ch, true
	})
}

// previewDialog 是「加入预览通道」确认框：警告、风险、备份、同意四段，确认后才切到预览通道。
func (a *About) previewDialog(c ui.C) {
	l := a.Look
	t := l.T
	switch l.ConfirmDialog(c.Key("preview"), &a.previewConfirm, kit.ConfirmSpec{
		Open:        a.previewAsk,
		Title:       locales.SettingsPreviewChannelConfirmTitle(),
		Description: locales.SettingsPreviewChannelConfirmSubtitle() + " " + locales.SettingsPreviewChannelWarning(),
		ConfirmText: locales.SettingsPreviewChannelConfirmEnable(),
		CancelText:  locales.SettingsPreviewChannelConfirmCancel(),
		Body: func(c ui.C) {
			l.Text(c, locales.SettingsPreviewChannelConfirmSubtitle(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			warn := ui.Column(c.Key("warning")).FillWidth().PadSides(l.Space(3)+1, l.Space(4)+1, l.Space(3)+1, l.Space(4)+1)
			amber := tokens.RGBA(245, 158, 11, 1) // amber-500
			warn.Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: amber.WithAlpha(0.1), Border: amber.WithAlpha(0.3), BorderW: 1, Radius: l.Rem(0.75)})
			}).Children(func() {
				l.Text(c, locales.SettingsPreviewChannelWarning(), kit.TextSM, kit.WeightNormal, tokens.RGBA(146, 64, 14, 1), kit.Wrapped())
			})
			for i, p := range []string{
				locales.SettingsPreviewChannelConfirmRisk(), locales.SettingsPreviewChannelConfirmBackup(), locales.SettingsPreviewChannelConfirmConsent(),
			} {
				l.Text(c.Key(string(rune('a'+i))), p, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			}
		},
	}) {
	case kit.ConfirmAccepted:
		a.previewAsk = false
		a.setChannel(settings.ChannelPreview)
	case kit.ConfirmDismissed:
		a.previewAsk = false
	}
}

// updateControls 是「检查更新」一行右侧的按钮，随流程阶段变化：空闲与已是最新是「检查更新」，检查中禁用转圈，
// 有新版本是「查看发布页」加「下载更新」，下载中禁用，下载完成是「立即安装」。shrink-0 whitespace-nowrap：说明很长时按钮不被压窄。
func (a *About) updateControls(c ui.C) {
	l := a.Look
	ui.Row(c.Key("update-controls")).Shrink(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
		switch a.phase {
		case phaseChecking:
			a.primaryButtonSpec(c, kit.ButtonSpec{Label: locales.SettingsChecking(), Disabled: true, Busy: true})
		case phaseAvailable:
			if a.outlineButton(c.Key("releases"), locales.SettingsOpenReleasesPage()) {
				a.openReleases()
			}
			if a.primaryButton(c.Key("download"), locales.SettingsDownloadUpdate(), gfx.Icon{}, false) {
				a.download()
			}
		case phaseDownloading:
			a.primaryButtonSpec(c, kit.ButtonSpec{Label: locales.SettingsDownloading(), Disabled: true, Busy: true})
		case phaseDownloaded:
			if a.primaryButton(c.Key("install"), locales.SettingsInstallNow(), gfx.Icon{}, false) {
				a.install()
			}
		default:
			if a.primaryButton(c.Key("check"), locales.SettingsCheckUpdate(), gfx.Icon{}, false) {
				a.check()
			}
		}
	})
}

// primaryStyle 是主色按钮：h-8 px-4 rounded-lg bg-primary text-primary-foreground text-sm hover:bg-primary/90。
func (a *About) primaryStyle() kit.ButtonStyle {
	l := a.Look
	t := l.T
	return kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Rem(1), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
		Surface: t.UiPanelBg,
	}
}

// primaryButton 画一枚带可选图标的主色按钮，返回是否被点击。
func (a *About) primaryButton(c ui.C, label string, icon gfx.Icon, disabled bool) bool {
	return a.Look.Button(c, kit.ButtonSpec{Label: label, Icon: icon, Disabled: disabled}, a.primaryStyle()).Clicked()
}

func (a *About) primaryButtonSpec(c ui.C, spec kit.ButtonSpec) {
	a.Look.Button(c, spec, a.primaryStyle())
}

// outlineButton 是带边框的次要按钮（h-8 px-3 text-sm border-border，悬停 bg-accent），返回是否被点击。
func (a *About) outlineButton(c ui.C, label string) bool {
	l := a.Look
	t := l.T
	st := kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent, Transition: kit.DurationDefault,
	}
	return l.Button(c, kit.ButtonSpec{Label: label}, st).Clicked()
}

// linkSpec 描述一枚链接按钮（渲染层的 <a> 按钮：打开外部页面）。
type linkSpec struct {
	name, label, url  string
	leading, trailing gfx.Icon
	iconSize          float32
	height, padX, gap float32
	text              kit.TextStyle
	weight            float32
	fg, bg, bgHover   tokens.Color
	border            tokens.Color
	borderW           float32
}

// linkButton 画一枚链接按钮并在点击时用系统浏览器打开 url。
func (a *About) linkButton(c ui.C, s linkSpec) {
	l := a.Look
	t := l.T
	pad := s.padX + s.borderW
	name := s.name
	if name == "" {
		name = s.label
	}
	box := ui.Row(c).Height(s.height).Shrink(0).Items(ui.Center).Justify(ui.Center).Gap(s.gap).PadSides(0, pad, 0, pad).
		HitWith(ui.ID(c), name, gfx.HitOptions{Role: gfx.RoleLink, Cursor: gfx.PointerCursorHand})
	bg := s.bg
	if box.State().Hovered {
		bg = s.bgHover
	}
	radius := l.Rem(0.75)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: s.border, BorderW: s.borderW, Radius: radius})
	}).Children(func() {
		if s.leading.ViewBox != 0 {
			l.Icon(c.Key("lead"), s.leading, s.iconSize, s.fg)
		}
		l.Text(c.Key("label"), s.label, s.text, s.weight, s.fg)
		if s.trailing.ViewBox != 0 {
			l.Icon(c.Key("trail"), s.trailing, s.iconSize, s.fg)
		}
	})
	if box.Clicked() {
		a.open(s.label, s.url)
	}
}

// outlineLink 是带边框、文字后跟外链图标的链接按钮（用户指南、更新日志、访问网站）。
func (a *About) outlineLink(c ui.C, name, label, url string, small bool) {
	l := a.Look
	t := l.T
	s := linkSpec{
		name: name, label: label, url: url, trailing: icons.ExternalLink, iconSize: l.Rem(1),
		height: l.Rem(2), padX: l.Space(3), gap: l.Space(1.5), text: kit.TextSM, weight: kit.WeightNormal,
		fg: t.Foreground, bgHover: t.Accent, border: t.Border, borderW: 1,
	}
	if small { // 访问网站：text-xs font-medium，图标 h-3.5 w-3.5
		s.text, s.weight, s.iconSize = kit.TextXS, kit.WeightMedium, l.Space(3.5)
	}
	a.linkButton(c, s)
}

// openSource 是开源与支持分区：用户指南、更新日志、项目仓库、提交问题。
func (a *About) openSource(c ui.C) {
	l := a.Look
	l.SettingSection(c.Key("open-source"), locales.SettingsOpenSource(), []kit.SettingRow{
		{
			Label: locales.SettingsUserGuide(), Description: locales.SettingsUserGuideDesc(),
			Control: func() { a.outlineLink(c, "", locales.SettingsOpenUserGuide(), userGuideURL, false) },
		},
		{
			Label: locales.SettingsChangelog(), Description: locales.SettingsChangelogDesc(),
			Control: func() { a.outlineLink(c, "", locales.SettingsOpenChangelog(), changelogURL, false) },
		},
		{
			Label: locales.SettingsProjectRepository(), Description: locales.SettingsProjectRepositoryDesc(),
			Control: func() { a.repoLink(c) },
		},
		{
			Label: locales.SettingsReportIssue(), Description: locales.SettingsReportIssueDesc(),
			Control: func() {
				a.linkButton(c, linkSpec{
					label: locales.SettingsSubmitIssue(), url: issueURL, leading: icons.MessageSquare, iconSize: l.Rem(1),
					height: l.Rem(2), padX: l.Rem(1), gap: l.Space(1.5), text: kit.TextSM, weight: kit.WeightNormal,
					fg: white, bg: orange500, bgHover: orange600,
				})
			},
		},
	})
}

// repoLink 是项目仓库的文字链接：inline-flex gap-2 text-sm font-medium text-primary，悬停下划线。
func (a *About) repoLink(c ui.C) {
	l := a.Look
	t := l.T
	box := ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(2)).
		HitWith(ui.ID(c), repoDisplay, gfx.HitOptions{Role: gfx.RoleLink, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	box.Children(func() {
		l.Icon(c.Key("icon"), icons.Github, l.Rem(1), t.Primary)
		l.Text(c.Key("text"), repoDisplay, kit.TextSM, kit.WeightMedium, t.Primary, kit.Underline(hovered))
	})
	if box.Clicked() {
		a.open(locales.SettingsProjectRepository(), repoURL)
	}
}

// contact 是联系作者分区：px-4 py-3 space-y-3，两行各是 p-2 rounded-lg 的整行按钮。
func (a *About) contact(c ui.C) {
	l := a.Look
	t := l.T
	l.SettingSectionBody(c.Key("contact"), locales.SettingsContactAuthor(), func() {
		ui.Column(c).FillWidth().PadSides(l.Space(3), l.Space(4), l.Space(3), l.Space(4)).Gap(l.Space(3)).Children(func() {
			if a.contactRow(c.Key("github"), authorHandle, authorHandle, "GitHub", icons.Github, t.Foreground, t.Foreground.WithAlpha(0.1), icons.ExternalLink) {
				a.open(authorHandle, authorURL)
			}
			if a.contactRow(c.Key("email"), authorEmail, authorEmail, "Email", icons.Mail, t.Primary, t.Primary.WithAlpha(0.1), icons.ClipboardCopy) {
				a.copyEmail()
			}
		})
	})
}

// contactRow 画一行联系方式：32px 圆形头像底加图标，两行文字（主文字 text-sm font-medium，说明 text-xs muted），
// 右侧一枚只在悬停时出现的图标（group-hover:opacity-100）。悬停底色 bg-muted/50，返回是否被点击。
func (a *About) contactRow(c ui.C, name, title, subtitle string, icon gfx.Icon, ink, avatarBg tokens.Color, hoverIcon gfx.Icon) bool {
	l := a.Look
	t := l.T
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).Pad(l.Space(2)).
		HitWith(ui.ID(c), name, gfx.HitOptions{Role: gfx.RoleLink, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	radius := l.Rem(0.75)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		if hovered {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.5), Radius: radius})
		}
	}).Children(func() {
		size := l.Rem(2)
		ui.Row(c.Key("avatar")).Size(size, size).Shrink(0).Center().Radius(size / 2).Background(l.Col(avatarBg)).Children(func() {
			l.Icon(c.Key("icon"), icon, l.Rem(1), ink)
		})
		ui.Column(c.Key("text")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(func() {
			l.Text(c, title, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Ellipsis())
			l.Text(c.Key("sub"), subtitle, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
		fade := float32(0)
		if hovered {
			fade = 1
		}
		l.Icon(c.Key("hover"), hoverIcon, l.Rem(1), t.MutedForeground.WithAlpha(fade)).Shrink(0)
	})
	return box.Clicked()
}

// otherWorks 是其他作品分区：不窄于 sm 横排（说明在左、按钮在右垂直居中），窄时竖排。
func (a *About) otherWorks(c ui.C) {
	l := a.Look
	t := l.T
	wide := l.ViewportWidth(c) >= aboutBreakpoint
	l.SettingSectionBody(c.Key("other-works"), locales.SettingsOtherWorks(), func() {
		var row ui.Box
		if wide {
			row = ui.Row(c).FillWidth().Gap(l.Space(4)).Items(ui.Center)
		} else {
			row = ui.Column(c).FillWidth().Gap(l.Space(4))
		}
		row.Pad(l.Space(4)).Children(func() {
			ui.Row(c.Key("info")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Items(ui.Start).Gap(l.Space(3)).Children(func() {
				size := l.Rem(2.5)
				ui.Row(c.Key("icon")).Size(size, size).Shrink(0).Center().Radius(l.Rem(0.75)).Background(l.Col(t.Primary.WithAlpha(0.1))).Children(func() {
					l.Icon(c, icons.Sparkles, l.Rem(1.25), t.Primary)
				})
				ui.Column(c.Key("text")).Shrink(1).MinWidth(0).Children(func() {
					l.Heading(c, 4, locales.SettingsSoftMemoName(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
					l.TextPx(c.Key("desc"), locales.SettingsSoftMemoDescription(), l.Rem(0.75), l.Rem(1.25), kit.WeightNormal, t.MutedForeground, kit.Wrapped()).
						MarginTop(l.Space(1))
				})
			})
			a.outlineLink(c.Key("visit"), locales.SettingsVisitSoftMemo(), locales.SettingsVisitWebsite(), softMemoURL, true)
		})
	})
}

// developer 是开发者分区：调试模式开关。
func (a *About) developer(c ui.C) {
	a.Look.SettingSection(c.Key("developer"), locales.SettingsDeveloper(), []kit.SettingRow{{
		Label: locales.SettingsDebugMode(), Description: locales.SettingsDebugModeDesc(),
		Control: a.uiSwitch(c, locales.SettingsDebugMode(),
			func(u settings.UI) bool { return u.DebugMode }, func(u *settings.UI, on bool) { u.DebugMode = on }),
	}})
}

// footer 是页脚许可证（px-4 py-4 text-sm muted 居中）。
func (a *About) footer(c ui.C) {
	l := a.Look
	ui.Column(c.Key("footer")).FillWidth().Items(ui.Center).Pad(l.Space(4)).Children(func() {
		l.Text(c, "AGPL-3.0 License \u00a9 2026 GuiZhi", kit.TextSM, kit.WeightNormal, l.T.MutedForeground)
	})
}
