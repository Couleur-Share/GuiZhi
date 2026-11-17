package imports

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// loadingState 是列表第一次读取期间的指示：delayed-fade-in flex h-32 items-center justify-center，里面一枚 sm 的 muted 转圈。
// 本地读取通常几十毫秒就回来，指示器先隐藏 250 ms 再淡入，免得一闪而过。
func (s *Screen) loadingState(c ui.C) {
	l := s.Look
	alpha := s.loading.Opacity(l, c.Canvas(), true)
	ui.Row(c.Key("loading")).FillWidth().Height(l.Rem(8)).Justify(ui.Center).Items(ui.Center).Opacity(alpha).Children(func() {
		l.Spinner(c, l.Rem(1), l.T.MutedForeground)
	})
}

// filteredEmpty 是筛选或搜索之后没有结果：与「一条任务都没有」是两回事，出口也不同。
// flex h-48 flex-col items-center justify-center gap-3 text-center。
func (s *Screen) filteredEmpty(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("filtered-empty")).FillWidth().Height(l.Rem(12)).Justify(ui.Center).Items(ui.Center).Gap(l.Space(3)).Children(func() {
		l.Icon(c.Key("icon"), icons.SearchX, l.Rem(2), t.MutedForeground.WithAlpha(0.4))
		l.Text(c.Key("text"), locales.ImportsFilteredEmpty(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered())
		// rounded-lg border border-border px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-muted/60
		b := l.Button(c.Key("reset"), kit.ButtonSpec{Label: locales.ImportsResetFilter()}, kit.ButtonStyle{
			PadX: l.Space(3), PadY: l.Space(1.5), Radius: l.Rem(0.75), Text: kit.TextXS, Weight: kit.WeightNormal,
			Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
		})
		if b.Clicked() {
			s.resetFilters()
		}
	})
}

// resetFilters 是「查看全部任务」：来源、状态与搜索词都回到初始（渲染层依次 setOrigin、setFilter、setQuery）。
func (s *Screen) resetFilters() {
	s.Store.SetOrigin(s.Ctx, OriginAll)
	s.Store.SetFilter(s.Ctx, FilterAll)
	s.Store.SetQuery("")
	s.scheduleFetch()
}

// sourceHint 是引导页里的一枚来源提示：inline-flex items-center gap-1.5 rounded-full border border-border/70 bg-background/40
// px-2.5 py-1 text-xs text-muted-foreground。
func (s *Screen) sourceHint(c ui.C, key string, icon gfx.Icon, label string) {
	l := s.Look
	t := l.T
	pad := func(n float32) float32 { return l.Space(n) + 1 }
	ui.Row(c.Key(key)).Shrink(0).Items(ui.Center).Gap(l.Space(1.5)).PadSides(pad(1), pad(2.5), pad(1), pad(2.5)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.4), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: 9999})
		}).
		Children(func() {
			l.Icon(c.Key("icon"), icon, l.Rem(0.875), t.MutedForeground)
			l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
}

// emptyState 是空队列引导（渲染层 ImportsEmptyState）。导入页是新用户的第一站，这里得说清楚归知能采什么，
// 而不只是一句「还没有导入任务」。
// flex flex-col items-center justify-center gap-5 px-6 py-14 text-center。
func (s *Screen) emptyState(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("empty")).FillWidth().Items(ui.Center).Justify(ui.Center).Gap(l.Space(5)).
		PadSides(l.Space(14), l.Space(6), l.Space(14), l.Space(6)).Children(func() {
		// h-14 w-14 rounded-2xl bg-muted/60，里面 h-7 w-7 的收件箱图标 text-muted-foreground/60
		tile := l.Rem(3.5)
		ui.Row(c.Key("tile")).Size(tile, tile).Shrink(0).Justify(ui.Center).Items(ui.Center).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.6), Radius: l.Rem(1)})
			}).
			Children(func() { l.Icon(c, icons.Inbox, l.Rem(1.75), t.MutedForeground.WithAlpha(0.6)) })

		// space-y-1.5：标题 text-sm font-medium，说明 text-xs 折行居中
		ui.Column(c.Key("copy")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
			l.Text(c.Key("title"), locales.ImportsEmpty(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Centered())
			l.Text(c.Key("desc"), locales.ImportsEmptyDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped()).FillWidth()
		})

		// flex max-w-md flex-wrap items-center justify-center gap-1.5
		ui.Flow(c.Key("hints")).FillWidth().MaxWidth(l.Rem(28)).Items(ui.Center).Justify(ui.Center).
			Gap(l.Space(1.5)).LineGap(l.Space(1.5)).Children(func() {
			s.sourceHint(c, "web", icons.Globe, locales.ImportsSourceWeb())
			s.sourceHint(c, "video", icons.Video, locales.ImportsSourceVideo())
			s.sourceHint(c, "forum", icons.MessagesSquare, locales.ImportsSourceForum())
			s.sourceHint(c, "local", icons.Image, locales.ImportsSourceLocal())
		})

		ui.Row(c.Key("actions")).Items(ui.Center).Gap(l.Space(2)).Children(func() {
			// inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground
			// transition-colors hover:bg-primary/90
			capture := l.Button(c.Key("capture"), kit.ButtonSpec{Label: locales.CaptureTitle(), Icon: icons.Plus}, kit.ButtonStyle{
				Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
				Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault,
			})
			if capture.Clicked() && s.Hooks.QuickCapture != nil {
				s.Hooks.QuickCapture()
			}
			if l.Button(c.Key("files"), kit.ButtonSpec{Label: locales.HeaderNewImportFiles(), Icon: icons.FolderOpen}, s.headerButtonStyle(false)).Clicked() {
				s.importFiles()
			}
		})

		shortcut := ""
		if s.Hooks.CaptureShortcut != nil {
			shortcut = s.Hooks.CaptureShortcut()
		}
		l.Text(c.Key("hint"), locales.ImportsEmptyHint(shortcut), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered())
	})
}
