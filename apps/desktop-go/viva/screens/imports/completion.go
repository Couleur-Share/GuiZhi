package imports

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 任务完成的成果卡（渲染层 components/imports/ImportCompletionCard.tsx）。
//
// 任务完成不该只剩一枚绿色状态：用户需要立即知道哪些内容已经落到本地，并能从同一个位置继续阅读或发起问答。
// 这里故意只读条目事实，不重新猜测连接器产物；无论网页、视频还是本地文件都共用同一张结果卡。

// actionKind 是详情弹窗里动作按钮的外观档（渲染层 ACTION_BASE 之上的三种配色）。
type actionKind uint8

const (
	actionPrimary actionKind = iota // bg-primary text-primary-foreground hover:bg-primary/90
	actionOutline                   // border border-border text-muted-foreground hover:bg-accent hover:text-foreground
	actionAsk                       // border border-border bg-background/60 text-foreground hover:bg-accent
)

// actionStyle 是动作按钮的外观：inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs transition-colors
// disabled:pointer-events-none disabled:opacity-40，再按档位配色。
func (s *Screen) actionStyle(kind actionKind) kit.ButtonStyle {
	l := s.Look
	t := l.T
	st := kit.ButtonStyle{
		Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextXS, Weight: kit.WeightNormal,
		Transition: kit.DurationDefault, DisabledOpacity: 0.4, Surface: t.UiPanelStrongBg, // 都在导入详情弹窗里
	}
	switch kind {
	case actionPrimary:
		st.Fg, st.Bg, st.BgHover = t.PrimaryForeground, t.Primary, t.Primary.WithAlpha(0.9)
	case actionOutline:
		st.Fg, st.FgHover, st.BgHover = t.MutedForeground, t.Foreground, t.Accent
		st.Border, st.BorderW = t.Border, 1
	case actionAsk:
		st.Fg, st.Bg, st.BgHover = t.Foreground, t.Background.WithAlpha(0.6), t.Accent
		st.Border, st.BorderW = t.Border, 1
	}
	return st
}

// verifyStyle 是验证类重试钮的外观：bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50，没有 transition。
func (s *Screen) verifyStyle() kit.ButtonStyle {
	st := s.actionStyle(actionPrimary)
	st.Transition, st.DisabledOpacity = kit.DurationNone, 0.5
	return st
}

// completionStat 是成果卡里的一项计数：inline-flex items-center gap-1 text-xs text-muted-foreground，图标 h-3.5 w-3.5。
func (s *Screen) completionStat(c ui.C, icon gfx.Icon, label string) {
	l := s.Look
	ui.Row(c).Items(ui.Center).Gap(l.Space(1)).Children(func() {
		l.Icon(c.Key("icon"), icon, l.Rem(0.875), l.T.MutedForeground)
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, l.T.MutedForeground)
	})
}

// completionCard 是成果卡：rounded-xl border border-primary/20 bg-primary/[0.045] px-4 py-3。
// onOpen 与 onAsk 由弹窗接，它们各自负责关闭弹窗。
func (s *Screen) completionCard(c ui.C, facts services.ImportItemFacts, onOpen, onAsk func()) {
	l := s.Look
	t := l.T
	title := facts.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	ui.Column(c).FillWidth().
		PadSides(l.Space(3)+1, l.Space(4)+1, l.Space(3)+1, l.Space(4)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{
				Bg: t.Primary.WithAlpha(0.045), Border: t.Primary.WithAlpha(0.2), BorderW: 1, Radius: l.Rem(0.75),
			})
		}).
		Children(func() {
			l.Text(c.Key("title"), locales.ImportsCompletionTitle(), kit.TextXS, kit.WeightMedium, t.Foreground)
			s.relaxedText(c.Key("hint"), locales.ImportsCompletionHint(), t.MutedForeground).MarginTop(l.Space(1))
			l.Text(c.Key("item"), title, kit.TextXS, kit.WeightMedium, t.Foreground, kit.Ellipsis()).FillWidth().MarginTop(l.Space(2))

			// mt-2 flex flex-wrap gap-x-3 gap-y-1.5
			ui.Flow(c.Key("stats")).FillWidth().MarginTop(l.Space(2)).Gap(l.Space(3)).LineGap(l.Space(1.5)).Children(func() {
				if facts.BodyLength > 0 {
					s.completionStat(c.Key("body"), icons.FileText, locales.ImportsCompletionBody(facts.BodyLength))
				}
				if facts.TranscriptLength > 0 {
					s.completionStat(c.Key("transcript"), icons.Mic, locales.ImportsCompletionTranscript(facts.TranscriptLength))
				}
				if facts.AssetCount > 0 {
					s.completionStat(c.Key("assets"), icons.Image, locales.ImportsCompletionAssets(facts.AssetCount))
				}
				if len(facts.Tags) > 0 {
					s.completionStat(c.Key("tags"), icons.Tags, locales.ImportsCompletionTags(len(facts.Tags)))
				}
			})

			// mt-3 flex flex-wrap gap-2
			ui.Flow(c.Key("actions")).FillWidth().MarginTop(l.Space(3)).Gap(l.Space(2)).LineGap(l.Space(2)).Children(func() {
				open := l.Button(c.Key("open"), kit.ButtonSpec{Label: locales.ImportsOpenItem(), Icon: icons.FileText, IconSize: l.Rem(0.875)},
					s.actionStyle(actionPrimary))
				if open.Clicked() {
					onOpen()
				}
				ask := l.Button(c.Key("ask"), kit.ButtonSpec{Label: locales.ImportsAskAboutItem(), Icon: icons.MessageCircle, IconSize: l.Rem(0.875)},
					s.actionStyle(actionAsk))
				if ask.Clicked() {
					onAsk()
				}
			})
		})
}

// relaxedText 是 text-xs leading-relaxed 的折行文字：行高是字号的 1.625 倍（12px 时 19.5px）。
func (s *Screen) relaxedText(c ui.C, text string, color tokens.Color) ui.Box {
	l := s.Look
	size := l.Rem(0.75)
	return l.TextPx(c, text, size, size*1.625, kit.WeightNormal, color, kit.Wrapped()).FillWidth()
}
