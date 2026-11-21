package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// AI 摘要卡片（渲染层 AiSummaryCard）：没有摘要时是一枚淡的「生成 AI 摘要」动作钮，有摘要或正在生成时是占满一行的卡片，
// 头部是展开收起钮与重新生成钮，展开后用 Markdown 排版展示摘要。生成、写回与失败提示在 Store（store_ai.go）。

// summaryUI 是摘要卡片的界面状态，随工具侧栏的挂载而生（syncTools 整体清空）。
type summaryUI struct {
	// expanded 是摘要展开着（渲染层 isExpanded，默认折叠：摘要可能很长，不抢占正文首屏）。
	expanded bool
	// seenExpandSeq 是已经处理过的「请展开」序号：Store 的 SummaryExpandSeq 比它大，说明又有一次写回成功，卡片据此展开
	// （渲染层在写回成功后 setIsExpanded(true)）。侧栏收起期间序号照常累加，重新打开时补上，结果与渲染层（面板只是隐藏、状态保留）相同。
	seenExpandSeq int

	// 摘要的 Markdown 解析缓存：同一份文字每帧不重复解析。
	parsedFor string
	parsed    []kit.MdBlock
}

// blocksOf 返回摘要的 Markdown 块（按原文缓存）。
func (u *summaryUI) blocksOf(summary string) []kit.MdBlock {
	if u.parsed == nil || u.parsedFor != summary {
		u.parsedFor, u.parsed = summary, kit.ParseMarkdown(summary)
	}
	return u.parsed
}

// summaryCard 声明摘要区块。item 必须是详情里的条目；Store 的工具状态还没挂到这一条时按「没有在途」算。
func (s *Screen) summaryCard(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	t := l.T
	u := &s.detail.tools.summary
	generating, seq := false, 0
	if tv := v.Detail.Tools; tv.ItemID == item.ID {
		generating, seq = tv.SummaryGenerating, tv.SummaryExpandSeq
	}
	if seq != u.seenExpandSeq {
		u.seenExpandSeq, u.expanded = seq, true
	}
	generate := func() { s.Store.GenerateSummary(s.Ctx) }

	if item.Summary == "" && !generating {
		box := l.Button(c, kit.ButtonSpec{Label: locales.LibraryAiSummaryGenerate(), Icon: icons.Sparkles, IconSize: l.Rem(0.875)}, s.actionChipStyle())
		if box.Clicked() {
			generate()
		}
		return
	}

	// w-full 在换行的 flex 行里是「基准宽 100%」：卡片独占一行，不与前面的 chip 并排。rounded-xl border border-primary/15 bg-primary/[0.04] px-4 py-3。
	ui.Column(c).WidthPercent(100).PadSides(l.Space(3)+1, l.Space(4)+1, l.Space(3)+1, l.Space(4)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.04), Border: t.Primary.WithAlpha(0.15), BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			s.summaryHeader(c, u, generating, generate)
			switch {
			case item.Summary != "" && u.expanded:
				// mt-1.5：与第一块 Markdown 的上外边距折叠（标题等自带上外边距的取较大者）。
				l.Prose(c.Key("body"), u.blocksOf(item.Summary), l.Space(1.5))
			case generating:
				l.Text(c.Key("generating"), locales.LibraryAiSummaryGenerating(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1.5))
			}
		})
}

// summaryHeader 是卡片头：flex items-center gap-1.5。左边一枚占满的展开钮（星标、标题、箭头），右边是生成中的转圈或重新生成钮。
func (s *Screen) summaryHeader(c ui.C, u *summaryUI, generating bool, generate func()) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		name := locales.LibraryAiSummaryExpand()
		if u.expanded {
			name = locales.LibraryAiSummaryCollapse()
		}
		toggle := s.inlineButton(c.Key("toggle"), inlineSpec{
			Name: name, Disclosure: true, Open: u.expanded, Fg: t.Foreground, BgHover: t.Primary.WithAlpha(0.06),
			Radius: l.Rem(0.75) - 2, PadX: l.Space(1), PadY: l.Space(0.5), Gap: l.Space(1.5),
		}, func(fg tokens.Color) {
			chevron := icons.ChevronDown
			if u.expanded {
				chevron = icons.ChevronUp
			}
			l.Icon(c.Key("star"), icons.Sparkles, l.Rem(0.875), t.Primary)
			l.Text(c.Key("title"), locales.LibraryAiSummary(), kit.TextXS, kit.WeightMedium, t.Primary)
			l.Icon(c.Key("chevron"), chevron, l.Rem(0.875), t.MutedForeground)
		}).Grow(1).Shrink(1).MinWidth(0).MarginLeft(-l.Space(1)) // -ml-1
		if toggle.Clicked() {
			u.expanded = !u.expanded
		}
		if generating {
			l.Spinner(c.Key("spin"), l.Rem(0.875), t.MutedForeground)
			return
		}
		regen := locales.LibraryAiSummaryRegenerate()
		box := s.inlineButton(c.Key("regen"), inlineSpec{
			Name: regen, Hint: regen, Fg: t.MutedForeground.WithAlpha(0.6), FgHover: t.Primary,
		}, func(fg tokens.Color) {
			l.Icon(c.Key("icon"), icons.RotateCcw, l.Rem(0.875), fg)
		})
		if box.Clicked() {
			generate()
		}
	})
}
