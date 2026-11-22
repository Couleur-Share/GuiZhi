package ask

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// stickToBottomPx 是距底部多少像素以内算「跟在最新消息上」（渲染层 STICK_TO_BOTTOM_THRESHOLD_PX）：
// 超出这个距离才显示「回到最新」。
const stickToBottomPx float32 = 80

// Workspace 是主区域（渲染层 AskWorkspace）：页头、消息列表（或加载、读取失败、空态）与底部输入框。
//
// flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section。
// 读取失败整页换成错误态；当前会话是「围绕本文」的会话时换成本文对话的占位（由 globalArticle 渲染）。
func (s *Screen) Workspace(c ui.C) {
	l := s.Look
	t := l.T
	s.enter()
	s.turns.Begin()
	s.adoptDraft()
	v := s.Store.View()
	set := s.Setup.View()
	s.followSession(v)
	s.pruneCards(v.Messages)

	ui.Column(c.Key("workspace")).FillWidth().Grow(1).MinHeight(0).Clip().
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
		Children(func() {
			switch {
			case v.LoadError != "":
				if l.LoadErrorState(c.Key("load-error"), v.LoadError) {
					s.spawn(func() { s.Store.RetryLoad() })
				}
			case v.ArticleSession != nil && v.ArticleSession.Target != nil:
				s.globalArticle(c, *v.ArticleSession)
			default:
				s.header(c, v)
				s.messageArea(c, v, set)
				s.composer(c, v)
			}
		})
}

// adoptDraft 取走外部带来的草稿（导入结果里的「就此提问」）：只预填、不自动发送，并让输入框聚焦
// （渲染层 consumeAskDraft + requestAnimationFrame(focus)）。
func (s *Screen) adoptDraft() {
	if !s.hasPending {
		return
	}
	s.setDraft(s.pending)
	s.pending, s.hasPending = "", false
	s.focusDraft = true
}

// followSession 在换了会话、或首次加载完成的那一帧让滚动区回到底部（新会话从最新一轮看起）。
func (s *Screen) followSession(v View) {
	key := v.ActiveSessionID
	if !v.HasLoaded {
		key = "\x00loading"
	}
	if key != s.viewKey {
		s.viewKey = key
		s.toEnd = true
	}
}

// header 是页头：flex h-12 shrink-0 items-center gap-2 border-b border-border px-5，标题与「N 轮问答」。
func (s *Screen) header(c ui.C, v View) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("header")).FillWidth().Shrink(0).Height(l.Rem(3)).Items(ui.Center).Gap(l.Space(2)).
		PadSides(0, l.Space(5), 1, l.Space(5)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			l.Heading(c.Key("title"), 2, locales.AskTitle(), kit.TextSM, kit.WeightSemibold, t.Foreground).Shrink(0)
			if len(v.Messages) > 0 {
				l.Text(c.Key("count"), locales.AskTurnCount(len(v.Messages)), kit.TextXS, kit.WeightNormal,
					t.MutedForeground.WithAlpha(0.7), kit.Ellipsis()).Shrink(1).MinWidth(0)
			}
		})
}

// messageArea 是页头与输入框之间的区域：relative min-h-0 flex-1，里面一个 h-full overflow-y-auto px-5 py-4 的滚动区，
// 滚离底部超过 80px 时底部居中浮出「回到最新」。
func (s *Screen) messageArea(c ui.C, v View, set SetupView) {
	l := s.Look
	ui.Column(c.Key("area")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		l.ScrollAreaView(c.Key("scroll"), [4]float32{l.Space(4), l.Space(5), l.Space(4), l.Space(5)}, func(view kit.ScrollView) {
			s.messageBody(c, v, set, view)
		}).FillWidth().Grow(1).MinHeight(0).Follow(true)
		if s.awayFromEnd && len(v.Messages) > 0 {
			s.latestButton(c)
		}
	})
}

// messageBody 是滚动区里的内容：保存失败的提示、加载中、空态或消息列表。
func (s *Screen) messageBody(c ui.C, v View, set SetupView, view kit.ScrollView) {
	l := s.Look
	// 只有用户本来就跟在底部时才自动滚动：流式回答每来一个分片就变一次，想往上翻看前一轮的内容时不能被拽回去。
	// 滚动区自己的 Follow 正是这个语义（用户滚离末端就脱钩，回到末端又跟上）；是否显示「回到最新」另按 80px 的距离算。
	s.awayFromEnd = view.State.Max()-view.State.Offset > stickToBottomPx
	if s.toEnd {
		view.State.ScrollToEnd()
		s.toEnd = false
	}
	if v.SaveError != "" {
		s.saveErrorNote(c, v.SaveError)
	}
	switch {
	case !v.HasLoaded:
		// 上次会话读出来之前不画空态：有历史记录的用户否则会先看到一整屏引导，再被消息列表整块换掉。
		s.loadingState(c, view)
	case len(v.Messages) == 0:
		s.emptyState(c, set, view)
	default:
		// mx-auto max-w-3xl space-y-6
		ui.Column(c.Key("messages")).FillWidth().MaxWidth(l.Rem(48)).Self(ui.Center).Gap(l.Space(6)).Children(func() {
			for _, m := range v.Messages {
				s.messageCard(c, v, m)
			}
		})
	}
}

// loadingState 是首次读取的指示：delayed-fade-in flex h-full items-center justify-center，一枚 sm 的 muted 转圈，
// 读取很快时不闪一下。
func (s *Screen) loadingState(c ui.C, view kit.ScrollView) {
	l := s.Look
	p := s.loading.Opacity(l, c.Canvas(), true)
	ui.Row(c.Key("loading")).FillWidth().Height(max(view.Viewport, l.Rem(8))).Justify(ui.Center).Items(ui.Center).
		Opacity(p).Children(func() {
		l.Spinner(c, l.Rem(1), l.T.MutedForeground)
	})
}

// saveErrorNote 是保存失败的提示：p-3 text-sm text-destructive，role="alert"，带「重试保存」。
// 对话还在内存里，只是没落盘——不能让用户以为已经存好了。
func (s *Screen) saveErrorNote(c ui.C, message string) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("save-error")).FillWidth().Items(ui.Center).Gap(l.Space(1)).LineGap(0).Pad(l.Space(3)).
		Draw(func(d *ui.DrawContext) { markAlert(d) }).
		Children(func() {
			l.Text(c.Key("text"), locales.AskAnswerNotSaved(message), kit.TextSM, kit.WeightNormal, t.Destructive, kit.Wrapped()).Shrink(1).MinWidth(0)
			if s.textButton(c.Key("retry"), locales.ArticleAskRetrySave(), textBtn{Style: kit.TextSM, Weight: kit.WeightNormal, Fg: t.Destructive}) {
				s.spawn(func() { s.Store.Persist() })
			}
		})
}

// latestButton 是「回到最新」：absolute bottom-3 left-1/2 -translate-x-1/2，h-8 w-8 rounded-full border border-border bg-popover
// text-muted-foreground shadow-sm hover:text-foreground，向下箭头。点击滚到底并重新跟随。
func (s *Screen) latestButton(c ui.C) {
	l := s.Look
	t := l.T
	label := locales.AskScrollToLatest()
	size := l.Rem(2)
	ui.Row(c.Key("latest-row")).Absolute().Left(0).Right(0).Bottom(l.Space(3)).Justify(ui.Center).Children(func() {
		b := l.Button(c.Key("latest"), kit.ButtonSpec{Name: label, Hint: label, Icon: icons.ArrowDown, Center: true}, kit.ButtonStyle{
			Height: size, Width: size, Radius: 9999, Fg: t.MutedForeground, FgHover: t.Foreground, Bg: t.Popover,
			Border: t.Border, BorderW: 1, Shadows: kit.ShadowSM, Transition: kit.DurationDefault,
		})
		if b.Clicked() {
			s.toEnd = true
		}
	})
}
