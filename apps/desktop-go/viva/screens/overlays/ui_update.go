package overlays

// 更新对话框的界面（渲染层 components/UpdateDialog.tsx）：xl 档模态窗，页眉是标题、当前版本、通道标签与「检查更新」刷新钮；
// 内容随阶段变化——检查中、有新版本（更新说明、下载）、下载进度、已下载（立即安装、打开安装包位置）、已是最新、失败（重试、手动下载）。
// 阶段与服务调用在 Update（update.go）；这里只画并把点击交给它。macOS 的 Homebrew 分支不存在（只有 Windows 外壳）。

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

const (
	// updateMinHeight 是居中型内容的最小高度（min-h-[320px]，px 字面量）。
	updateMinHeight = 320
	// updateNotesMaxHeight 与 updateNotesMaxHeightNarrow 是更新说明的最大高度（max-h-[440px]，窄窗口 sm 以下 360px，px 字面量）。
	updateNotesMaxHeight       = 440
	updateNotesMaxHeightNarrow = 360
	// updateSmWidth 是 Tailwind 的 sm 断点（640px）：宽于它页眉刷新钮才显示文字、按钮并排。
	updateSmWidth = 640
)

// updateUI 是更新对话框自己的界面状态：模态窗与更新说明的解析缓存。
type updateUI struct {
	modal  kit.ModalState
	notes  string
	blocks []kit.MdBlock
}

// updateDialog 声明更新对话框。每一帧都要经过 Modal：关着的那一帧它负责复位。
func (s *Screen) updateDialog(c ui.C) {
	l := s.Look
	v := s.Update.View()
	version := v.CurrentVersion
	if version == "" {
		version = "..."
	}
	line := locales.SettingsVersion() + ": " + version
	if l.Modal(c.Key("update-modal"), &s.update.modal, kit.ModalSpec{
		Open: v.Open, Size: kit.ModalXL, Title: locales.SettingsCheckUpdate(), CloseLabel: locales.CommonClose(),
		SubtitleText: line,
		Subtitle: func(c ui.C) {
			l.Text(c, line, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
		},
		HeaderActions: func(c ui.C) { s.updateHeader(c, v) },
		Body:          func(c ui.C) { s.updateBody(c, v) },
	}) {
		s.Update.Close()
	}
}

// updateHeader 是页眉右侧：通道标签（rounded-full bg-muted px-2.5 py-1 text-xs）与刷新钮（检查中禁用并转圈）。
func (s *Screen) updateHeader(c ui.C, v UpdateView) {
	l := s.Look
	t := l.T
	label := locales.SettingsStableChannel()
	if v.Channel == settings.ChannelPreview {
		label = locales.SettingsPreviewChannel()
	}
	ui.Row(c.Key("header")).Shrink(1).MinWidth(0).Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).Children(func() {
		ui.Row(c.Key("channel")).Shrink(0).PadSides(l.Space(1), l.Space(2.5), l.Space(1), l.Space(2.5)).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: d.Rect.Height / 2})
		}).Children(func() {
			l.Text(c.Key("text"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
		// rounded-xl border border-border px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted/60 hover:text-foreground disabled:opacity-50
		st := kit.ButtonStyle{
			Height: l.Space(3) + l.Rem(1) + 2, PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6), Border: t.Border, BorderW: 1,
			Transition: kit.DurationDefault, Surface: t.UiPanelStrongBg,
		}
		spec := kit.ButtonSpec{Name: locales.SettingsCheckUpdate(), Disabled: v.Refreshing, BusySize: l.Rem(0.875)}
		if c.Bounds().Width >= updateSmWidth {
			spec.Label = locales.SettingsCheckUpdate()
		}
		if v.Refreshing {
			spec.Busy = true
		} else {
			spec.Icon, spec.IconSize = icons.RefreshCw, l.Rem(0.875)
		}
		if l.Button(c.Key("refresh"), spec, st).Clicked() {
			s.Update.Check()
		}
	})
}

// updateBody 是内容区：按阶段画。
func (s *Screen) updateBody(c ui.C, v UpdateView) {
	c = c.Key("update")
	switch v.Phase {
	case UpdateChecking:
		s.updateChecking(c, v)
	case UpdateAvailable:
		s.updateAvailable(c, v)
	case UpdateLatest:
		s.updateLatest(c, v)
	case UpdateDownloading:
		s.updateDownloading(c, v)
	case UpdateDownloaded:
		s.updateDownloaded(c, v)
	case UpdateFailed:
		s.updateFailed(c, v)
	default:
		s.updateIdle(c, v)
	}
}

// centered 是 flex min-h-[320px] flex-col items-center justify-center text-center 的居中块。
func (s *Screen) centered(c ui.C) ui.Box {
	return ui.Column(c).FillWidth().MinHeight(updateMinHeight).Items(ui.Center).Justify(ui.Center)
}

// updateIdle 是还没有状态：当前版本与「检查更新」。
func (s *Screen) updateIdle(c ui.C, v UpdateView) {
	l := s.Look
	version := v.CurrentVersion
	if version == "" {
		version = "..."
	}
	s.centered(c.Key("idle")).Children(func() {
		l.Text(c.Key("version"), locales.SettingsVersion()+": "+version, kit.TextSM, kit.WeightNormal, l.T.MutedForeground, kit.Centered()).MarginBottom(l.Space(4))
		if s.dlgButton(c.Key("check"), dlgPrimary, kit.ButtonSpec{Label: locales.SettingsCheckUpdate(), Icon: icons.RefreshCw}) {
			s.Update.Check()
		}
	})
}

// updateChecking 是检查中：32px 转圈加一句话（用镜像源时说明在用镜像）。
func (s *Screen) updateChecking(c ui.C, v UpdateView) {
	l := s.Look
	t := l.T
	text := locales.SettingsChecking()
	if v.UseMirror {
		text = locales.SettingsUsingMirrorSource()
	}
	s.centered(c.Key("checking")).Children(func() {
		l.Spinner(c.Key("spinner"), l.Rem(2), t.Primary).MarginBottom(l.Space(4))
		l.Text(c.Key("text"), text, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered())
	})
}

// updateLatest 是已是最新：48px 绿色对勾、标题与当前版本。
func (s *Screen) updateLatest(c ui.C, v UpdateView) {
	l := s.Look
	t := l.T
	version := v.CurrentVersion
	s.centered(c.Key("latest")).Children(func() {
		l.Icon(c.Key("icon"), icons.CircleCheck, l.Rem(3), tokens.Tailwind("green-500")).MarginBottom(l.Space(4))
		l.Heading(c.Key("title"), 3, locales.SettingsNoUpdate(), kit.TextLG, kit.WeightSemibold, t.Foreground, kit.Centered()).MarginBottom(l.Space(1))
		l.Text(c.Key("desc"), locales.SettingsNoUpdateDesc(version), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered())
	})
}

// updateCard 是版本卡片：flex items-start gap-3 rounded-xl border border-border/60 bg-muted/20 p-4，左边 44px 的绿色图标块，右边标题与说明行。
func (s *Screen) updateCard(c ui.C, icon gfx.Icon, title string, lines ...string) {
	l := s.Look
	t := l.T
	green := tokens.Tailwind("green-500")
	ui.Row(c).FillWidth().Items(ui.Start).Gap(l.Space(3)).Pad(l.Space(4) + 1).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)})
	}).Children(func() {
		ui.Row(c.Key("badge")).Size(l.Rem(2.75), l.Rem(2.75)).Shrink(0).Items(ui.Center).Justify(ui.Center).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: green.WithAlpha(0.1), Radius: l.Rem(0.75)})
		}).Children(func() {
			l.Icon(c.Key("icon"), icon, l.Rem(1.5), green)
		})
		ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			l.Heading(c.Key("title"), 3, title, kit.TextLG, kit.WeightSemibold, t.Foreground)
			for i, line := range lines {
				if line != "" {
					l.Text(c.Key(string(rune('a'+i))), line, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
				}
			}
		})
	})
}

// updateVersionLines 是新版本卡片的说明行：版本号，有发布日期时再加一行。
func (s *Screen) updateVersionLines(v UpdateView) []string {
	lines := []string{locales.SettingsVersion() + ": " + v.DisplayVersion()}
	if v.ReleaseDate != "" {
		lines = append(lines, locales.NativeUpdateReleasedOn(v.ReleaseDate))
	}
	return lines
}

// updateAvailable 是有新版本：卡片、更新说明、「下载更新」与「稍后安装」。
func (s *Screen) updateAvailable(c ui.C, v UpdateView) {
	c = c.Key("available")
	ui.Column(c).FillWidth().Gap(s.Look.Space(4)).Children(func() {
		s.updateCard(c.Key("card"), icons.Download, locales.SettingsUpdateAvailable(), s.updateVersionLines(v)...)
		if v.ReleaseNotes != "" {
			s.updateNotes(c.Key("notes"), v.ReleaseNotes)
		}
		ui.Row(c.Key("actions")).FillWidth().Justify(ui.End).Gap(s.Look.Space(2)).Children(func() {
			if s.dlgButton(c.Key("download"), dlgPrimary, kit.ButtonSpec{Label: locales.SettingsDownloadUpdate(), Icon: icons.Download}) {
				s.Update.Download()
			}
			if s.dlgButton(c.Key("later"), dlgMuted, kit.ButtonSpec{Label: locales.SettingsInstallLater()}) {
				s.Update.Close()
			}
		})
	})
}

// updateNotes 是更新说明：rounded-xl border border-border/60 bg-muted/30，页头 border-b（text-xs font-medium uppercase），
// 内容是 Markdown（只放行 http、https 链接，经系统浏览器打开），超过上限时滚动。
func (s *Screen) updateNotes(c ui.C, notes string) {
	l := s.Look
	t := l.T
	u := &s.update
	if u.notes != notes || u.blocks == nil {
		u.notes, u.blocks = notes, kit.ParseMarkdown(notes)
	}
	maxHeight := float32(updateNotesMaxHeight)
	if c.Bounds().Width < updateSmWidth {
		maxHeight = updateNotesMaxHeightNarrow
	}
	ui.Column(c).FillWidth().Clip().Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)})
	}).Pad(1).Children(func() {
		ui.Row(c.Key("head")).FillWidth().PadSides(l.Space(3), l.Space(4), l.Space(3)+1, l.Space(4)).Draw(func(d *ui.DrawContext) {
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
		}).Children(func() {
			// uppercase tracking-wide：Viva 的文字没有字距（台账 A-013），大写照做。
			l.Text(c.Key("title"), strings.ToUpper(locales.SettingsReleaseNotes()), kit.TextXS, kit.WeightMedium, t.MutedForeground)
		})
		opts := kit.ProseOpts{}
		if s.OpenExternal != nil {
			opts.OnLink = s.openLink
		}
		pad := l.Space(4)
		l.ScrollArea(c.Key("body"), [4]float32{l.Space(3), pad, l.Space(3), pad}, func() {
			l.ProseWith(c.Key("prose"), u.blocks, 0, 0, opts)
		}).FillWidth().MinHeight(0).Shrink(1).MaxHeight(maxHeight)
	})
}

// openLink 打开更新说明里的链接：只放行 http、https（渲染层 resolveSafeMarkdownHref），其余不响应。
func (s *Screen) openLink(href string) {
	lower := strings.ToLower(strings.TrimSpace(href))
	if s.OpenExternal == nil || (!strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://")) {
		return
	}
	if err := s.OpenExternal(href); err != nil {
		s.Guard.Report(locales.LibraryMediaMetaOpenSource(), err)
	}
}

// updateDownloading 是下载中：标题与百分比、进度条（h-2 bg-muted rounded-full，填充 bg-primary）与进度说明。
func (s *Screen) updateDownloading(c ui.C, v UpdateView) {
	l := s.Look
	t := l.T
	percent := v.Percent
	pct := formatPercent(percent)
	c = c.Key("downloading")
	ui.Column(c).FillWidth().MinHeight(updateMinHeight).Items(ui.Center).Justify(ui.Center).PadSides(l.Space(4), 0, l.Space(4), 0).Children(func() {
		ui.Column(c.Key("bar")).FillWidth().MaxWidth(l.Rem(28)).MarginBottom(l.Space(4)).Children(func() {
			ui.Row(c.Key("labels")).FillWidth().Justify(ui.Between).MarginBottom(l.Space(2)).Children(func() {
				l.Text(c.Key("label"), locales.SettingsDownloading(), kit.TextSM, kit.WeightNormal, t.Foreground)
				l.Text(c.Key("percent"), pct+"%", kit.TextSM, kit.WeightNormal, t.Foreground)
			})
			ui.Row(c.Key("track")).FillWidth().Height(l.Space(2)).Draw(func(d *ui.DrawContext) {
				r := d.Rect
				d.Canvas.Rounded(r, r.Height/2, d.Fade(l.Col(t.Muted)))
				if w := r.Width * float32(percent) / 100; w > 0 {
					d.Canvas.Rounded(gfx.Rect{X: r.X, Y: r.Y, Width: w, Height: r.Height}, r.Height/2, d.Fade(l.Col(t.Primary)))
				}
			})
		})
		l.Text(c.Key("hint"), locales.SettingsDownloadProgress(pct), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered())
	})
}

// updateDownloaded 是下载完成：卡片、重启提示、「立即安装」「稍后安装」与「打开安装包位置」。
func (s *Screen) updateDownloaded(c ui.C, v UpdateView) {
	l := s.Look
	t := l.T
	c = c.Key("downloaded")
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		s.updateCard(c.Key("card"), icons.CircleCheck, locales.SettingsDownloadComplete(), s.updateVersionLines(v)...)
		l.Text(c.Key("hint"), locales.SettingsInstallRestartHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		ui.Column(c.Key("actions")).FillWidth().Gap(l.Space(2)).Children(func() {
			ui.Row(c.Key("row")).FillWidth().Justify(ui.End).Gap(l.Space(2)).Children(func() {
				if s.dlgButton(c.Key("install"), dlgPrimary, kit.ButtonSpec{Label: locales.SettingsInstallNow(), Busy: v.Installing, Disabled: v.Installing, BusySize: l.Rem(1)}) {
					s.Update.Install()
				}
				if s.dlgButton(c.Key("later"), dlgMuted, kit.ButtonSpec{Label: locales.SettingsInstallLater()}) {
					s.Update.Close()
				}
			})
			if s.dlgButton(c.Key("folder"), dlgSecondary, kit.ButtonSpec{Label: locales.SettingsOpenDownloadFolder(), Icon: icons.FolderOpen, Full: true, Center: true}) {
				s.Update.OpenDownloaded()
			}
		})
	})
}

// updateFailed 是失败：红色叉号、错误标题与原因（SHA512 校验不通过时换成说明并给「打开下载目录」）、重试，
// 底部是手动下载的引导块（mt-auto）。
func (s *Screen) updateFailed(c ui.C, v UpdateView) {
	l := s.Look
	t := l.T
	red := tokens.Tailwind("red-500")
	c = c.Key("failed")
	text := v.Error
	if text == "" {
		text = locales.CommonUnknownError()
	}
	sha := v.SHA512Failed()
	if sha {
		text = locales.ErrorSha512Desc()
	}
	ui.Column(c).FillWidth().MinHeight(updateMinHeight).Items(ui.Center).Children(func() {
		ui.Row(c.Key("badge")).Size(l.Rem(3), l.Rem(3)).Shrink(0).Items(ui.Center).Justify(ui.Center).MarginBottom(l.Space(3)).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: red.WithAlpha(0.1), Radius: d.Rect.Height / 2})
		}).Children(func() {
			l.Icon(c.Key("icon"), icons.X, l.Rem(1.5), red)
		})
		l.Heading(c.Key("title"), 3, locales.CommonError(), kit.TextLG, kit.WeightSemibold, red, kit.Centered()).MarginBottom(l.Space(1))
		l.ScrollArea(c.Key("reason"), [4]float32{0, l.Space(2), 0, l.Space(2)}, func() {
			l.Text(c.Key("text"), text, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered())
		}).FillWidth().MinHeight(0).Shrink(1).MaxHeight(l.Rem(6)).MarginBottom(l.Space(4))
		if sha {
			ui.Column(c.Key("sha")).FillWidth().MarginBottom(l.Space(4)).Children(func() {
				if s.dlgButton(c.Key("folder"), dlgSuccess, kit.ButtonSpec{Label: locales.SettingsOpenDownloadFolder(), Icon: icons.FolderOpen, Full: true, Center: true}) {
					s.Update.OpenDownloaded()
				}
			})
		}
		if s.dlgButton(c.Key("retry"), dlgSecondary, kit.ButtonSpec{Label: locales.CommonRetry(), Icon: icons.RefreshCw, Disabled: v.Refreshing, Full: true, Center: true}) {
			s.Update.Check()
		}
		ui.Column(c.Key("spacer")).Grow(1).MinHeight(l.Space(4))
		s.updateManual(c.Key("manual"))
	})
}

// updateManual 是手动下载引导块：p-4 rounded-xl bg-muted/30 border border-border/50，一句说明加整宽的「手动下载」。
func (s *Screen) updateManual(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().Pad(l.Space(4) + 1).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border.WithAlpha(0.5), BorderW: 1, Radius: l.Rem(0.75)})
	}).Children(func() {
		l.Text(c.Key("hint"), locales.SettingsManualDownloadHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginBottom(l.Space(3))
		if s.dlgButton(c.Key("open"), dlgSecondary, kit.ButtonSpec{Label: locales.SettingsManualDownload(), Icon: icons.ExternalLink, Full: true, Center: true}) {
			s.Update.OpenReleases()
		}
	})
}
