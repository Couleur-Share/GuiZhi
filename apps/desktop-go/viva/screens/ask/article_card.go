package ask

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 围绕本文的一轮问答卡（渲染层 ArticleMessageCard）：问题、选段引用、流式步骤、联网状态、警告、回答、错误、来源 chip、
// 来源摘录与动作行。与全局问答的卡（card.go）是两套版面：这里没有气泡与悬停显隐，动作行一直可见。

// locateIdle、locateFound、locateMissed 是一次「定位到原文」的结果：定位在别的 goroutine 里回报，经原子量回到界面线程。
const (
	locateIdle int32 = iota
	locateFound
	locateMissed
)

// articleCardState 是一轮卡的界面状态（渲染层 ArticleMessageCard 的 useState）。
type articleCardState struct {
	base cardState // 只用其中的 Markdown 解析缓存与复制回显
	open bool      // 是否展开了某条来源的摘录
	// index 是展开的来源在 message.sources 里的下标（按下标而不是对象：消息更新后仍指到同一条）。
	index         int
	locationError bool
	located       atomic.Int32
}

// articleCardOf 取（或建）一轮卡的状态。
func (s *Screen) articleCardOf(id string) *articleCardState {
	if s.art.cards == nil {
		s.art.cards = map[string]*articleCardState{}
	}
	st := s.art.cards[id]
	if st == nil {
		st = &articleCardState{}
		s.art.cards[id] = st
	}
	return st
}

// pruneArticleCards 丢掉已经不在消息列表里的卡片状态（换会话、重答之后）。
func (s *Screen) pruneArticleCards(messages []aiflow.ArticleMessage) {
	if len(s.art.cards) == 0 {
		return
	}
	live := make(map[string]struct{}, len(messages))
	for _, m := range messages {
		live[m.ID] = struct{}{}
	}
	for id := range s.art.cards {
		if _, ok := live[id]; !ok {
			delete(s.art.cards, id)
		}
	}
}

// selected 返回展开着的来源；下标越界（消息被改写）时没有。
func (st *articleCardState) selected(m aiflow.ArticleMessage) *aiflow.ArticleSource {
	if !st.open || st.index < 0 || st.index >= len(m.Sources) {
		return nil
	}
	return &m.Sources[st.index]
}

// articleCard 是一轮问答：article space-y-3 border-b border-border/60 pb-5。running 是整个会话有一轮在回答（重新回答因此禁用）。
func (s *Screen) articleCard(c ui.C, m aiflow.ArticleMessage, running bool) {
	l := s.Look
	t := l.T
	st := s.articleCardOf(m.ID)
	// 定位结果由别的 goroutine 回报，这里取走（只有「没找到」才亮出失败提示，找到了就清掉旧提示）。
	switch st.located.Swap(locateIdle) {
	case locateFound:
		st.locationError = false
	case locateMissed:
		st.locationError = true
	}
	ordinals := make([]int, len(m.Sources))
	for i, src := range m.Sources {
		ordinals[i] = src.Ordinal
	}
	labels := BuildCitationLabels(m.Answer, ordinals)

	ui.Column(c.Key("msg-"+m.ID)).FillWidth().Gap(l.Space(3)).PadSides(0, 0, l.Space(5)+1, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			s.articleQuestion(c, m)
			s.articleStatusLines(c, m)
			if m.Answer != "" {
				// 正文里的 [n] 标记按展示编号改写成纯文字：Viva 的 Markdown 排版里链接不可点（与全局问答同一处差异），
				// 点击来源 chip 才是「这句话依据哪份资料」的入口。
				l.Prose(c.Key("prose"), st.base.blocks(RenumberCitations(m.Answer, ordinals, labels)), 0)
			}
			s.articleOutcome(c, m)
			s.articleSources(c, st, m, labels)
			s.articleExcerpt(c, st, m)
			s.articleActions(c, st, m, ordinals, labels, running)
		})
}

// articleQuestion 是问题与它引用的选段：问题 whitespace-pre-wrap rounded-xl bg-primary/10 p-3 text-sm；
// 选段 max-h-32 overflow-auto border-l-2 border-primary/50 pl-3 text-xs text-muted-foreground。
func (s *Screen) articleQuestion(c ui.C, m aiflow.ArticleMessage) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("question")).FillWidth().Pad(l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: l.Rem(0.75)})
		}).
		Children(func() {
			l.Text(c.Key("text"), m.Question, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
		})
	if m.Context == nil || m.Context.Target.Selection == nil || *m.Context.Target.Selection == "" {
		return
	}
	selection := *m.Context.Target.Selection
	ui.Column(c.Key("quote")).FillWidth().PadLeft(l.Space(3) + 2).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeLeft, 2, t.Primary.WithAlpha(0.5)) }).
		Children(func() {
			l.ScrollArea(c.Key("scroll"), [4]float32{}, func() {
				l.Text(c.Key("text"), selection, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			}).FillWidth().MaxHeight(l.Rem(8))
		})
}

// articleStatusLines 是回答之前的几行小字：流式步骤（animate-pulse）、联网查证未完成、警告。
func (s *Screen) articleStatusLines(c ui.C, m aiflow.ArticleMessage) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	if m.Status == "running" {
		// 渲染层 animate-pulse：2 s 一周，不透明度 1 → 0.5 → 1。
		const period = 2 * time.Second
		phase := float32(canvas.Now()%period) / float32(period)
		pulse := 1 - 0.5*(1-float32(math.Abs(float64(2*phase-1))))
		canvas.RequestFrameIn(50 * time.Millisecond)
		step := ""
		if m.Step != nil {
			step = *m.Step
		}
		ui.Column(c.Key("step")).FillWidth().Opacity(pulse).Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
			if step != "" {
				l.Text(c.Key("text"), step, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			}
		})
	}
	if m.WebStatus == "failed" || m.WebStatus == "partial" {
		ui.Column(c.Key("web-status")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
			l.Text(c.Key("text"), locales.ArticleAskUnverified(), kit.TextXS, kit.WeightMedium, t.Destructive, kit.Wrapped())
		})
	}
	for i, warning := range m.Warnings {
		l.Text(c.Key(fmt.Sprintf("warning-%d", i)), warning, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	}
}

// articleOutcome 是回答之后的失败原因（role="alert"，text-sm text-destructive，保留换行）与截断提示（text-xs text-destructive）。
func (s *Screen) articleOutcome(c ui.C, m aiflow.ArticleMessage) {
	l := s.Look
	t := l.T
	if m.Error != nil && *m.Error != "" {
		message := *m.Error
		ui.Column(c.Key("error")).FillWidth().Draw(func(d *ui.DrawContext) { markAlert(d) }).Children(func() {
			l.Text(c.Key("text"), message, kit.TextSM, kit.WeightNormal, t.Destructive, kit.Wrapped())
		})
	}
	if m.Truncated != nil && *m.Truncated {
		ui.Column(c.Key("truncated")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
			l.Text(c.Key("text"), locales.AskAnswerTruncated(), kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
		})
	}
}

// articleSources 是来源 chip：flex flex-wrap gap-1.5，每枚 max-w-full truncate rounded border border-border px-2 py-1 text-left text-xs，
// 文字是「[展示编号] 标题」，按展示编号从小到大。点击展开该来源的摘录。
func (s *Screen) articleSources(c ui.C, st *articleCardState, m aiflow.ArticleMessage, labels *CitationLabels) {
	if len(m.Sources) == 0 {
		return
	}
	l := s.Look
	t := l.T
	order := make([]int, len(m.Sources))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return labels.LabelOf(m.Sources[a].Ordinal) - labels.LabelOf(m.Sources[b].Ordinal)
	})
	ui.Flow(c.Key("sources")).FillWidth().Gap(l.Space(1.5)).Children(func() {
		for _, i := range order {
			src := m.Sources[i]
			label := fmt.Sprintf("[%d] %s", labels.LabelOf(src.Ordinal), src.Title)
			b := l.Button(c.Key(fmt.Sprintf("chip-%d", src.Ordinal)), kit.ButtonSpec{Label: label}, kit.ButtonStyle{
				PadX: l.Space(2) + 1, PadY: l.Space(1) + 1, Radius: l.Rem(0.25), Text: kit.TextXS, Weight: kit.WeightNormal,
				Fg: t.Foreground, Border: t.Border, BorderW: 1,
			}).MaxWidthPercent(100)
			if b.Clicked() {
				st.open, st.index, st.locationError = true, i, false
				st.located.Store(locateIdle)
			}
		}
	})
}

// articleExcerpt 是展开的来源摘录：有证据快照（或已被清除）用共用的 EvidenceExcerpt 版面，
// 否则是 rounded-lg border border-border bg-muted/30 p-3 text-xs 的「引用摘录」区。定位失败时上方多一行说明。
func (s *Screen) articleExcerpt(c ui.C, st *articleCardState, m aiflow.ArticleMessage) {
	src := st.selected(m)
	if src == nil {
		return
	}
	l := s.Look
	t := l.T
	cleared := src.Cleared != nil && *src.Cleared
	locate := func() { s.locateSource(st, *src) }
	closeIt := func() { st.open = false }
	if src.Evidence != nil && st.locationError {
		ui.Column(c.Key("locate-failed")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
			l.Text(c.Key("text"), locales.ArticleAskLocateFailedEvidence(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		})
	}
	if src.Evidence != nil || cleared {
		s.evidencePanel(c, src.Evidence, cleared, "", locate, closeIt)
		return
	}
	text := src.Text
	if text == "" {
		text = locales.ArticleAskExcerptNotSaved()
	}
	pad := l.Space(3) + 1
	ui.Column(c.Key("excerpt")).FillWidth().Gap(l.Space(2)).Pad(pad).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border, BorderW: 1, Radius: l.Rem(0.5)})
			d.Canvas.Container(d.Rect, gfx.RoleGroup, locales.ArticleAskExcerpt())
		}).
		Children(func() {
			l.Text(c.Key("title"), src.Title, kit.TextXS, kit.WeightMedium, t.Foreground, kit.Wrapped())
			l.ScrollArea(c.Key("text-scroll"), [4]float32{}, func() {
				l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
			}).FillWidth().MaxHeight(l.Rem(12))
			if st.locationError {
				ui.Column(c.Key("locate-failed")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
					l.Text(c.Key("text"), locales.ArticleAskLocateFailed(), kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
				})
			}
			ui.Flow(c.Key("buttons")).FillWidth().LineGap(0).Items(ui.Center).Children(func() {
				label := locales.ArticleAskLocate()
				if src.Kind == "web" {
					label = locales.ArticleAskOpenSource()
				}
				if s.textButton(c.Key("locate"), label, textBtn{Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Primary, Underline: true}) {
					locate()
				}
				ui.Row(c.Key("close-wrap")).MarginLeft(l.Space(3)).Children(func() {
					if s.textButton(c.Key("close"), locales.CommonClose(), textBtn{Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.MutedForeground}) {
						closeIt()
					}
				})
			})
		})
}

// locateSource 是「定位到原文」/「打开网页来源」：网页来源只允许无账号口令的 http(s) 链接，交给系统浏览器；
// 其余来源交给外壳在阅读区里定位，结果稍后回到卡片状态。失败只亮出行内提示（渲染层 setLocationError）。
func (s *Screen) locateSource(st *articleCardState, src aiflow.ArticleSource) {
	if src.Kind == "web" && src.URL != nil && *src.URL != "" {
		st.locationError = !s.openWebSource(*src.URL)
		return
	}
	locate := s.art.onLocate
	if locate == nil {
		st.locationError = true
		return
	}
	st.located.Store(locateIdle)
	locate(src, func(found bool) {
		if found {
			st.located.Store(locateFound)
		} else {
			st.located.Store(locateMissed)
		}
		s.Store.wake()
	})
}

// openWebSource 校验并打开网页来源；链接无效、没有外壳钩子或打开失败返回 false。
func (s *Screen) openWebSource(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" {
		return false
	}
	if s.Hooks.OpenURL == nil {
		return false
	}
	return s.Hooks.OpenURL(u.String()) == nil
}

// articleActions 是最后一行：flex gap-3 text-xs text-muted-foreground，复制（有回答才有）、重新回答（回答中禁用）、
// 推到行尾的模型名（单行截断）。
func (s *Screen) articleActions(c ui.C, st *articleCardState, m aiflow.ArticleMessage, ordinals []int, labels *CitationLabels, running bool) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("actions")).FillWidth().Items(ui.Center).Gap(l.Space(3)).Children(func() {
		base := textBtn{Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.MutedForeground}
		if m.Answer != "" && s.textButton(c.Key("copy"), locales.AskCopyAnswer(), base) {
			if s.copyAnswer(RenumberCitations(m.Answer, ordinals, labels)) {
				s.Guard.Succeed(locales.AskCopied())
			}
		}
		retry := base
		retry.Disabled = running
		if s.textButton(c.Key("retry"), locales.AskRetryTurn(), retry) {
			id := m.ID
			s.spawn(func() { s.Store.Article().Retry(id) })
		}
		if m.Model != nil && *m.Model != "" {
			ui.Spacer(c.Key("auto")).Grow(1)
			l.Text(c.Key("model"), *m.Model, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
		}
	})
}
