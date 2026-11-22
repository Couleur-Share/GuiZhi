package ask

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// sourceList 区分被选中的来源出自哪一份清单：回答里引用的 sources，还是本次读取的全部证据 evidenceSources。
type sourceList uint8

const (
	listSources sourceList = iota
	listEvidence
)

// sourceChoice 是被选中的来源（渲染层 sourceChoice：清单加下标，而不是对象引用，消息更新后仍指到同一条）。
type sourceChoice struct {
	list  sourceList
	index int
}

// cardState 是一轮消息卡的界面状态（渲染层 AskMessageCard / AgentSteps 里的 useState）。
type cardState struct {
	stepsOpen    bool          // 检索过程是否展开
	evidenceOpen bool          // 「本次读取的全部证据」是否展开
	choice       *sourceChoice // 展开了证据摘录的来源
	// 点了来源之后 chip 高亮到 highlightUntil（画布时钟）；0 表示没有高亮。
	highlighted    int
	highlightUntil time.Duration
	copiedUntil    time.Duration // 「已复制」回显到这个时刻

	mdSrc string // 正文 Markdown 的解析缓存：流式回答每个分片都重画，不必每帧重解析
	md    []kit.MdBlock
	mdOK  bool
}

// blocks 返回正文的 Markdown 块；源文字没变时复用上一次的解析结果。
func (st *cardState) blocks(src string) []kit.MdBlock {
	if !st.mdOK || st.mdSrc != src {
		st.md, st.mdSrc, st.mdOK = kit.ParseMarkdown(src), src, true
	}
	return st.md
}

// selected 返回被选中的来源；下标已经越界（消息被改写、重答）时没有。
func (st *cardState) selected(m services.AskMessage) *services.AskSource {
	if st.choice == nil {
		return nil
	}
	list := m.Sources
	if st.choice.list == listEvidence {
		list = m.EvidenceSources
	}
	if st.choice.index < 0 || st.choice.index >= len(list) {
		return nil
	}
	return &list[st.choice.index]
}

// tone 是渲染层行内用到的 Tailwind 色阶（不是主题令牌，浅色与深色各取一档）。
func (s *Screen) tone(light, dark string) tokens.Color {
	if s.Look.Dark {
		return tokens.Tailwind(dark)
	}
	return tokens.Tailwind(light)
}

// messageCard 是一轮问答（渲染层 AskMessageCard）：用户的问题气泡，下面是助手回答卡。
//
// group/turn space-y-3：动作行在整轮内悬停或键盘聚焦时才显现，整轮的命中区只用来感知悬停（不进可访问树），
// 轮内每个可点的命中区都登记进同一分组，指针落在它们上面时整轮仍算悬停。
func (s *Screen) messageCard(c ui.C, v View, m services.AskMessage) {
	l := s.Look
	st := s.card(m.ID)
	c = c.Key("turn-" + m.ID)
	group := "turn:" + m.ID
	id := ui.ID(c)
	s.turns.Add(group, id)

	ordinals := make([]int, len(m.Sources))
	for i, src := range m.Sources {
		ordinals[i] = src.Ordinal
	}
	labels := BuildCitationLabels(m.Answer, ordinals)

	ui.Column(c).FillWidth().Gap(l.Space(3)).HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true}).Children(func() {
		s.userBubble(c, m)
		s.assistantCard(c, v, st, m, ordinals, labels, group)
	})
}

// userBubble 是用户的问题：flex justify-end，里面 max-w-[85%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-primary px-4 py-2.5
// text-sm text-primary-foreground。
func (s *Screen) userBubble(c ui.C, m services.AskMessage) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("question-row")).FillWidth().Justify(ui.End).Children(func() {
		ui.Column(c.Key("question")).MaxWidthPercent(85).
			PadSides(l.Space(2.5), l.Space(4), l.Space(2.5), l.Space(4)).
			Draw(func(d *ui.DrawContext) {
				s.paintCornered(d, kit.Surface{Bg: t.Primary, Radius: l.Rem(1)}, cornerBottomRight, l.Rem(0.75)-2)
			}).
			Children(func() {
				l.Text(c.Key("text"), m.Question, kit.TextSM, kit.WeightNormal, t.PrimaryForeground, kit.Wrapped())
			})
	})
}

// assistantCard 是助手的回答卡：rounded-2xl rounded-bl-md border border-border/70 bg-background/60 px-4 py-3。
// 从上到下：检索过程、警告、失败原因、正文与截断提示、引用来源、全部证据、证据摘录、模型与动作行。
func (s *Screen) assistantCard(c ui.C, v View, st *cardState, m services.AskMessage, ordinals []int, labels *CitationLabels, group string) {
	l := s.Look
	t := l.T
	running := m.Status == services.AskStatusRunning
	pad := func(n float32) float32 { return l.Space(n) + 1 } // 内边距加 1px 边框
	ui.Column(c.Key("answer")).FillWidth().PadSides(pad(3), pad(4), pad(3), pad(4)).
		Draw(func(d *ui.DrawContext) {
			s.paintCornered(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(1)},
				cornerBottomLeft, l.Rem(0.75)-2)
		}).
		Children(func() {
			s.agentSteps(c, st, m.Steps, running, group)
			for i, warning := range m.Warnings {
				// mb-2 text-xs text-muted-foreground，role="status"
				ui.Column(c.Key(fmt.Sprintf("warning-%d", i))).FillWidth().MarginBottom(l.Space(2)).
					Draw(func(d *ui.DrawContext) { markStatus(d) }).
					Children(func() {
						l.Text(c.Key("text"), warning, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
					})
			}
			if m.Status == services.AskStatusError {
				s.messageError(c, m, group)
			}
			switch {
			case m.Answer != "":
				body := RenumberCitations(m.Answer, ordinals, labels)
				// 正文里的 [n] 标记按展示编号改写成纯文字：Viva 的 Markdown 排版里链接不可点，做成蓝字链接是假的可点提示；
				// 点击来源 chip 才是「这句话依据哪份资料」的入口（台账 B-061 之外的新增差异，见 M3-4 收口说明）。
				l.ProseFollowed(c.Key("prose"), st.blocks(body), 0, s.answerFollow(st, m, running))
				if m.Truncated {
					s.truncatedNote(c)
				}
			case running && len(m.Steps) == 0:
				l.Spinner(c.Key("wait"), l.Rem(1), t.MutedForeground)
			}
			if m.Status == services.AskStatusDone {
				s.sourceList(c, st, m, labels, group)
			}
			s.evidenceAll(c, st, m, group)
			if src := st.selected(m); src != nil {
				s.evidenceExcerpt(c, st, m, *src, group)
			}
			if !running {
				s.cardFooter(c, v, st, m, ordinals, labels, group)
			}
		})
}

// answerFollow 是回答正文后面那个兄弟的上外边距：正文末块的下外边距与它折叠（CSS 外边距折叠），取较大者。
// 顺序与 assistantCard 里的声明顺序一致：截断提示 mt-2，引用来源 mt-3，全部证据 my-2，证据摘录（不带外边距），底部一行 mt-1。
func (s *Screen) answerFollow(st *cardState, m services.AskMessage, running bool) float32 {
	l := s.Look
	switch {
	case m.Truncated:
		return l.Space(2)
	case m.Status == services.AskStatusDone && len(m.Sources) > 0:
		return l.Space(3)
	case len(m.EvidenceSources) > 0:
		return l.Space(2)
	case st.selected(m) != nil:
		return l.Space(3)
	case !running:
		return l.Space(1)
	}
	return 0
}

// agentSteps 是检索过程。运行中只显示当前这一步（带转圈与已走的步数）——把已完成的步骤全堆着，每多一步卡片就长一截，
// 正文在眼皮底下往下跳；完成后折叠成「检索过程（N 步）」，点开列出全部步骤。
func (s *Screen) agentSteps(c ui.C, st *cardState, steps []string, running bool, group string) {
	if len(steps) == 0 {
		return
	}
	l := s.Look
	t := l.T
	if running {
		// mb-2 flex items-center gap-1.5 text-xs text-muted-foreground
		ui.Row(c.Key("steps")).FillWidth().MarginBottom(l.Space(2)).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
			l.Spinner(c.Key("spin"), l.Rem(0.75), t.MutedForeground)
			l.Text(c.Key("current"), steps[len(steps)-1], kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
			if len(steps) > 1 {
				l.Text(c.Key("count"), fmt.Sprint(len(steps)), kit.TextXS, kit.WeightNormal, t.MutedForeground.WithAlpha(0.5)).Shrink(0)
			}
		})
		return
	}
	ui.Column(c.Key("steps")).FillWidth().MarginBottom(l.Space(2)).Children(func() {
		open := st.stepsOpen
		icon := icons.ChevronRight
		if open {
			icon = icons.ChevronDown
		}
		// text-xs text-muted-foreground/70 hover:text-muted-foreground；箭头 h-3 w-3，折叠时朝右（渲染层是 -rotate-90 的向下箭头）。
		// 按钮是 flex（块级）但 <button> 的宽度仍是内容宽：放进一行里让它贴着内容，而不是撑满整行。
		var clicked bool
		ui.Row(c.Key("toggle-row")).FillWidth().Children(func() {
			clicked = s.textButton(c.Key("toggle"), locales.AskStepsLabel(len(steps)), textBtn{
				Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.MutedForeground.WithAlpha(0.7), FgHover: t.MutedForeground,
				Icon: icon, IconSize: l.Rem(0.75), Expanded: &open, Groups: &s.turns, Group: group,
			})
		})
		if clicked {
			st.stepsOpen = !st.stepsOpen
		}
		if !open {
			return
		}
		// mt-1 space-y-0.5 border-l border-border/60 pl-3
		ui.Column(c.Key("list")).FillWidth().MarginTop(l.Space(1)).Gap(l.Space(0.5)).PadLeft(l.Space(3) + 1).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeLeft, 1, t.Border.WithAlpha(0.6)) }).
			Children(func() {
				for i, step := range steps {
					l.Text(c.Key(fmt.Sprintf("step-%d", i)), step, kit.TextXS, kit.WeightNormal, t.MutedForeground.WithAlpha(0.7), kit.Wrapped())
				}
			})
	})
}

// messageError 是失败原因：没配模型（带「前往设置」）、知识库里没有相关资料、其余（含「已停止」）带原因。
func (s *Screen) messageError(c ui.C, m services.AskMessage, group string) {
	l := s.Look
	t := l.T
	switch ClassOf(m) {
	case ClassNotConfigured:
		// flex flex-wrap items-center gap-2 text-sm text-muted-foreground
		ui.Flow(c.Key("error")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
			l.Icon(c.Key("icon"), icons.TriangleAlert, l.Rem(1), tokens.Tailwind("amber-500"))
			l.Text(c.Key("text"), locales.AskNotConfigured(), kit.TextSM, kit.WeightNormal, t.MutedForeground)
			// inline-flex items-center gap-1 rounded-md bg-primary/10 px-2 py-0.5 text-xs text-primary hover:bg-primary/20
			b := l.Button(c.Key("settings"), kit.ButtonSpec{
				Label: locales.AskGoToSettings(), Icon: icons.Settings, IconSize: l.Rem(0.75), Groups: &s.turns, Group: group,
			}, kit.ButtonStyle{
				PadX: l.Space(2), PadY: l.Space(0.5), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightNormal,
				Fg: t.Primary, Bg: t.Primary.WithAlpha(0.1), BgHover: t.Primary.WithAlpha(0.2), Transition: kit.DurationDefault,
			})
			if b.Clicked() && s.Store.OnOpenSettingsAI != nil {
				s.Store.OnOpenSettingsAI()
			}
		})
	case ClassNoSource:
		l.Text(c.Key("error"), locales.AskNoSource(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	default:
		// flex items-start gap-1.5 text-sm text-destructive/90；图标 mt-0.5 h-4 w-4 shrink-0
		ui.Row(c.Key("error")).FillWidth().Items(ui.Start).Gap(l.Space(1.5)).Children(func() {
			ui.Column(c.Key("icon-wrap")).Shrink(0).MarginTop(l.Space(0.5)).Children(func() {
				l.Icon(c.Key("icon"), icons.TriangleAlert, l.Rem(1), t.Destructive.WithAlpha(0.9))
			})
			l.Text(c.Key("text"), m.Error, kit.TextSM, kit.WeightNormal, t.Destructive.WithAlpha(0.9), kit.Wrapped()).Shrink(1).MinWidth(0)
		})
	}
}

// truncatedNote 是回答被截断的提示：mt-2 flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-500。
func (s *Screen) truncatedNote(c ui.C) {
	l := s.Look
	ink := s.tone("amber-600", "amber-500")
	ui.Row(c.Key("truncated")).FillWidth().MarginTop(l.Space(2)).Items(ui.Start).Gap(l.Space(1.5)).Children(func() {
		ui.Column(c.Key("icon-wrap")).Shrink(0).MarginTop(l.Space(0.5)).Children(func() {
			l.Icon(c.Key("icon"), icons.TriangleAlert, l.Rem(0.875), ink)
		})
		l.Text(c.Key("text"), locales.AskAnswerTruncated(), kit.TextXS, kit.WeightNormal, ink, kit.Wrapped()).Shrink(1).MinWidth(0)
	})
}

// focusSource 选中一条来源并让它的 chip 高亮 2 秒（渲染层 focusCitation：setSource + setHighlighted + 2 s 定时）。
func (st *cardState) focusSource(list sourceList, index, ordinal int, now time.Duration) {
	st.choice = &sourceChoice{list: list, index: index}
	st.highlighted, st.highlightUntil = ordinal, now+highlightFor
}

// sourceList 是回答下面的「引用来源」：mt-3 border-t border-border/60 pt-2，一排 chip（按展示编号从小到大）。
// chip 点击展开该来源回答时的证据摘录，并让它高亮 2 秒。
func (s *Screen) sourceList(c ui.C, st *cardState, m services.AskMessage, labels *CitationLabels, group string) {
	if len(m.Sources) == 0 {
		return
	}
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	now := canvas.Now()
	order := make([]int, len(m.Sources))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return labels.LabelOf(m.Sources[a].Ordinal) - labels.LabelOf(m.Sources[b].Ordinal)
	})
	ui.Column(c.Key("sources")).FillWidth().MarginTop(l.Space(3)).PadTop(l.Space(2) + 1).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			// mb-1.5 text-xs font-medium text-muted-foreground
			l.Text(c.Key("title"), locales.AskSources(), kit.TextXS, kit.WeightMedium, t.MutedForeground).MarginBottom(l.Space(1.5))
			ui.Flow(c.Key("chips")).FillWidth().Gap(l.Space(1.5)).Children(func() {
				for _, i := range order {
					src := m.Sources[i]
					lit := st.highlighted == src.Ordinal && st.highlightUntil > now
					if lit {
						canvas.RequestFrameIn(st.highlightUntil - now)
					}
					if s.sourceChip(c, src, labels.LabelOf(src.Ordinal), lit, group) {
						st.focusSource(listSources, i, src.Ordinal, now)
					}
				}
			})
		})
}

// sourceChip 是一枚来源 chip：inline-flex max-w-64 items-center gap-1 rounded-lg border px-2 py-1 text-xs text-foreground transition-colors；
// 高亮 border-primary bg-primary/10，平时 border-border/70 bg-background/60，悬停 border-primary/40 bg-primary/5。
// 里面是「[编号] 图标 标题（· 待复核）」。返回这一帧是否被点了。
func (s *Screen) sourceChip(c ui.C, src services.AskSource, label int, lit bool, group string) bool {
	l := s.Look
	t := l.T
	c = c.Key(fmt.Sprintf("chip-%d", src.Ordinal))
	id := ui.ID(c)
	s.turns.Add(group, id)
	title := src.Title
	if src.Evidence != nil && src.Evidence.ReviewStatus == "needs_review" {
		title += " · " + locales.AskSourceNeedsReview()
	}
	badge := fmt.Sprintf("[%d]", label)
	box := ui.Row(c).MaxWidth(l.Rem(16)).Items(ui.Center).Gap(l.Space(1)).
		PadSides(l.Space(1)+1, l.Space(2)+1, l.Space(1)+1, l.Space(2)+1).
		HitWith(id, badge+" "+title, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	pl := l.HoverProgress(c, id, 1, lit, kit.DurationDefault)
	bg := kit.MixToken(kit.MixToken(t.Background.WithAlpha(0.6), t.Primary.WithAlpha(0.05), ph), t.Primary.WithAlpha(0.1), pl)
	border := kit.MixToken(kit.MixToken(t.Border.WithAlpha(0.7), t.Primary.WithAlpha(0.4), ph), t.Primary, pl)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.75))
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
	}).Children(func() {
		// shrink-0 font-mono text-[10px] text-primary：编号（等宽字体没有现成的 kit 文字入口，用界面字体）。
		s.monoBadge(c, label)
		icon := icons.BookOpen
		if src.Kind == "wiki" {
			icon = icons.Network
		}
		ui.Column(c.Key("icon-wrap")).Shrink(0).Children(func() {
			l.Icon(c.Key("icon"), icon, l.Rem(0.75), t.MutedForeground)
		})
		l.Text(c.Key("title"), title, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Shrink(1).MinWidth(0)
	})
	return box.Clicked()
}

// monoBadge 是来源 chip 里的编号：shrink-0 font-mono text-[10px] text-primary。JSX 的 `[{n}]` 在 DOM 里是「[」「n」「]」
// 三个相邻的文本节点，可访问树与文字行都按三段记（对照的 L1 文字序列按这个口径），所以拆成三段画；每段按 Consolas 的字距
// 留宽（kit.MonoAdvance，台账 A-006），行盒高 1rem（随字号档缩放）：行高继承自按钮的 text-xs。
func (s *Screen) monoBadge(c ui.C, label int) {
	l := s.Look
	ui.Row(c.Key("badge")).Shrink(0).Items(ui.Center).Children(func() {
		for i, run := range []string{"[", strconv.Itoa(label), "]"} {
			key := strconv.Itoa(i)
			ui.Row(c.Key("badge-" + key)).Width(kit.MonoAdvance(run, 10)).Height(l.Rem(1)).Items(ui.Center).Children(func() {
				l.MonoText(c.Key("badge-text-"+key), run, 10, l.Rem(1), l.T.Primary)
			})
		}
	})
}

// evidenceAll 是「本次读取的全部证据（N）」折叠区（渲染层 <details>）：my-2 text-xs text-muted-foreground，
// 展开后是一排 rounded border border-border px-2 py-1 的按钮，点击展开对应来源的证据摘录。
func (s *Screen) evidenceAll(c ui.C, st *cardState, m services.AskMessage, group string) {
	if len(m.EvidenceSources) == 0 {
		return
	}
	l := s.Look
	t := l.T
	// 下外边距与下一个兄弟的上外边距折叠（证据摘录 mt-3、底部一行 mt-1，取较大者）；ui 的列不折叠，所以减去下一个兄弟自己保留的那份。
	next := float32(0)
	switch {
	case st.selected(m) != nil:
		next = l.Space(3)
	case m.Status != services.AskStatusRunning:
		next = l.Space(1)
	}
	ui.Column(c.Key("evidence-all")).FillWidth().MarginTop(l.Space(2)).MarginBottom(max(l.Space(2), next) - next).Children(func() {
		open := st.evidenceOpen
		// <details><summary> 的标记是 Chromium 的 disclosure 三角，不是 SVG 图标。
		if s.disclosureSummary(c.Key("summary"), locales.AskEvidenceAll(len(m.EvidenceSources)), open, kit.TextXS, t.MutedForeground, group) {
			st.evidenceOpen = !st.evidenceOpen
		}
		if !open {
			return
		}
		// flex flex-wrap gap-2 py-2
		ui.Flow(c.Key("list")).FillWidth().Gap(l.Space(2)).PadSides(l.Space(2), 0, l.Space(2), 0).Children(func() {
			for i, entry := range m.EvidenceSources {
				b := l.Button(c.Key(fmt.Sprintf("entry-%d", i)), kit.ButtonSpec{Label: entry.Title, Groups: &s.turns, Group: group},
					kit.ButtonStyle{
						PadX: l.Space(2), PadY: l.Space(1), Radius: l.Rem(0.25), Text: kit.TextXS, Weight: kit.WeightNormal,
						Fg: t.MutedForeground, Border: t.Border, BorderW: 1,
					})
				if b.Clicked() {
					st.choice = &sourceChoice{list: listEvidence, index: i}
				}
			}
		})
	})
}

// cardFooter 是回答下面的一行：模型名、「简易检索」徽标（走了兜底管线，回答质量与检索深度会差一档，不标出来用户无从解释为什么这次答得浅），
// 推到行尾的复制 / 重新回答 / 删除。mt-1 flex flex-wrap items-center gap-x-2。
func (s *Screen) cardFooter(c ui.C, v View, st *cardState, m services.AskMessage, ordinals []int, labels *CitationLabels, group string) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("footer")).FillWidth().MarginTop(l.Space(1)).Items(ui.Center).Gap(l.Space(2)).LineGap(0).Children(func() {
		if m.Model != "" {
			// mt-2 inline-flex items-center gap-1 text-[11px] text-muted-foreground/60
			ui.Row(c.Key("model")).MarginTop(l.Space(2)).Items(ui.Center).Gap(l.Space(1)).Children(func() {
				l.Icon(c.Key("icon"), icons.Sparkles, l.Rem(0.75), t.MutedForeground.WithAlpha(0.6))
				l.TextPx(c.Key("text"), m.Model, 11, 16.5, kit.WeightNormal, t.MutedForeground.WithAlpha(0.6))
			})
		}
		if m.UsedFallback {
			s.fallbackBadge(c, group)
		}
		ui.Spacer(c.Key("auto")).Grow(1)
		s.cardActions(c, v, st, m, ordinals, labels, group)
	})
}

// fallbackBadge 是「简易检索」徽标：mt-2 rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[11px] text-amber-600 dark:text-amber-400，
// 悬停说明本轮没能走通 Agent 检索协议。
func (s *Screen) fallbackBadge(c ui.C, group string) {
	l := s.Look
	c = c.Key("fallback")
	id := ui.ID(c)
	s.turns.Add(group, id)
	ink := s.tone("amber-600", "amber-400")
	ui.Row(c).MarginTop(l.Space(2)).PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
		HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: locales.AskFallbackHint()}).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: tokens.Tailwind("amber-500").WithAlpha(0.1), Radius: l.Rem(0.75) - 2})
		}).
		Children(func() {
			l.TextPx(c.Key("text"), locales.AskFallbackBadge(), 11, 16.5, kit.WeightNormal, ink)
		})
}

// cardActions 是复制 / 重新回答 / 删除：mt-2 flex items-center gap-0.5，opacity-0 transition-opacity duration-quick，整轮悬停或聚焦时 opacity-100。
// 按钮一直在可访问树里（渲染层只是透明），所以始终声明，只用不透明度表现显隐。
func (s *Screen) cardActions(c ui.C, v View, st *cardState, m services.AskMessage, ordinals []int, labels *CitationLabels, group string) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	now := canvas.Now()
	c = c.Key("actions")
	id := ui.ID(c)
	p := l.HoverProgress(c, id, 0, s.turns.Active(canvas, group), kit.DurationQuick)
	base := kit.ButtonStyle{
		Height: l.Rem(1.5), PadX: l.Space(1.5), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), FontPx: 11, LinePx: 16.5, Weight: kit.WeightNormal,
		Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Accent.WithAlpha(0.6), Transition: kit.DurationDefault,
	}
	copied := st.copiedUntil > now
	if copied {
		canvas.RequestFrameIn(st.copiedUntil - now)
	}
	ui.Row(c).MarginTop(l.Space(2)).Items(ui.Center).Gap(l.Space(0.5)).Opacity(p).Children(func() {
		if m.Answer != "" {
			spec := kit.ButtonSpec{Label: locales.AskCopyAnswer(), Icon: icons.Copy, IconSize: l.Rem(0.75), Groups: &s.turns, Group: group}
			if copied {
				spec.Label, spec.Icon = locales.AskCopied(), icons.Check
			}
			if l.Button(c.Key("copy"), spec, base).Clicked() {
				if s.copyAnswer(RenumberCitations(m.Answer, ordinals, labels)) {
					st.copiedUntil = now + copiedFor
				}
			}
		}
		retry := base
		retry.DisabledOpacity = 0.4
		spec := kit.ButtonSpec{
			Label: locales.AskRetryTurn(), Icon: icons.RotateCcw, IconSize: l.Rem(0.75), Disabled: v.IsRunning, Groups: &s.turns, Group: group,
		}
		if l.Button(c.Key("retry"), spec, retry).Clicked() {
			mid := m.ID
			s.spawn(func() { s.Store.Retry(mid) })
		}
		remove := base
		remove.FgHover, remove.BgHover = t.Destructive, t.Destructive.WithAlpha(0.1)
		spec = kit.ButtonSpec{Label: locales.CommonDelete(), Icon: icons.Trash2, IconSize: l.Rem(0.75), Groups: &s.turns, Group: group}
		if l.Button(c.Key("delete"), spec, remove).Clicked() {
			mid := m.ID
			s.spawn(func() { s.Store.RemoveMessage(mid) })
		}
	})
}

// copyAnswer 把回答（引用编号已与屏幕一致）写入剪贴板；失败弹出带原因的提示并返回 false。
func (s *Screen) copyAnswer(text string) bool {
	if s.Guard.Notify == nil {
		return false
	}
	if err := s.Guard.Notify.CopyText(text); err != nil {
		s.Guard.Fail(locales.ArticleAskCopyFailed(), locales.AskCopyAnswer(), err)
		return false
	}
	return true
}
