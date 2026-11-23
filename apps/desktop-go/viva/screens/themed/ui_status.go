package themed

import (
	"math"
	"strconv"
	"time"
	"unicode"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 任务状态卡（ThemedReadingStatus.tsx）：标题与阶段进度、失败原因（可复制）、停止 / 继续 / 不联网继续、「生成详情」。
// 阅读面板（页面上方的紧凑条与居中大卡）和任务列表（紧凑条）共用同一块；内容判断在 BuildStatus（status.go），这里只画。
//
// 结构照渲染层：
//   - 大卡（首次生成、还没有页面）：圆角卡片，头部（图标块 + 标题说明 + 操作钮）、阶段进度条、折叠的详情（带顶边与淡底）、活动时的底部提示；
//   - 紧凑条（已有页面或在任务列表里）：只有下边线，头部一行，详情折叠在 summary 里。
//
// 失败原因与统计行都在折叠的详情里，不在头部。

// copyState 是「复制详细原因」的结果：渲染层把它写在按钮文字上，失败时多一行提示。
type copyState uint8

const (
	copyIdle copyState = iota
	copyDone
	copyFailed
)

// statusUI 是状态卡的界面状态：每张卡的「生成详情」展开与否、复制结果，按任务标识记。
type statusUI struct {
	open map[string]bool
	copy map[string]copyState
	// stepsW 是展开样式的阶段格容器上一帧的宽度（按容器标识记）：auto-fit 的列数取决于它，布局期读不到自己的宽度，只能用上一帧的。
	stepsW map[gfx.ID]float32
}

// statusActions 是状态卡上按钮的回调。回调为空的按钮不出现（任务列表里没有「不联网继续」）。
type statusActions struct {
	Stop, Resume, Offline func()
	// Busy 为一次操作进行中：按钮禁用。
	Busy bool
}

// statusCard 画一张任务状态卡。key 区分同一帧里的多张卡。
func (s *Screen) statusCard(c ui.C, task services.ThemedTask, o StatusOptions, act statusActions) {
	l := s.Look
	t := l.T
	m := BuildStatus(task, o)
	exp := o.Expanded
	var sec ui.Box
	if exp {
		// w-full max-w-xl overflow-hidden rounded-2xl border border-border/70 bg-card shadow-lg shadow-black/[0.04]
		surface := kit.Surface{
			Bg: t.Card, Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(1),
			Shadows: kit.ShadowLGOf(tokens.RGBA(0, 0, 0, 0.04)),
		}
		sec = ui.Column(c).FillWidth().MaxWidth(l.Rem(36)).Pad(1).Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) })
	} else {
		// border-b border-border/50 px-4 py-2.5
		sec = ui.Column(c).FillWidth().PadSides(l.Space(2.5), l.Space(4), l.Space(2.5)+1, l.Space(4)).Draw(func(d *ui.DrawContext) {
			l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
		})
	}
	sec.Children(func() {
		s.statusHead(c.Key("head"), task, m, exp, act)
		if exp && m.Active {
			ui.Column(c.Key("progress")).FillWidth().PadSides(0, l.Space(6), l.Space(6), l.Space(6)).Children(func() {
				s.statusSteps(c.Key("steps"), m.Steps, true)
			})
		}
		s.statusDetails(c.Key("details"), task.ID, m, exp, act)
		if exp && m.Active && m.BackgroundHint != "" {
			// border-t border-border/40 px-6 py-3 text-xs leading-5 text-muted-foreground
			ui.Column(c.Key("hint")).FillWidth().PadSides(l.Space(3)+1, l.Space(6), l.Space(3), l.Space(6)).Draw(func(d *ui.DrawContext) {
				l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.4))
			}).Children(func() {
				l.Text(c.Key("text"), m.BackgroundHint, kit.TextStyle{Size: kit.TextXS.Size, Line: 1.25}, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			})
		}
	})
}

// statusHead 是头部：状态块（图标与标题说明，可读屏播报）和操作钮，放不下时钮折到下一行。
func (s *Screen) statusHead(c ui.C, task services.ThemedTask, m StatusModel, exp bool, act statusActions) {
	l := s.Look
	flow := ui.Flow(c).FillWidth().Items(ui.Center)
	if exp {
		flow = flow.Gap(l.Space(4)).LineGap(l.Space(3)).PadSides(l.Space(7), l.Space(6), l.Space(6), l.Space(6))
	} else {
		flow = flow.Gap(l.Space(3)).LineGap(l.Space(2))
	}
	flow.Children(func() {
		gap, basis := l.Space(2), float32(0)
		if exp {
			gap, basis = l.Space(3.5), l.Rem(16)
		}
		ui.Row(c.Key("status")).Grow(1).Shrink(1).Basis(basis).MinWidth(0).Items(ui.Center).Gap(gap).
			Draw(func(d *ui.DrawContext) {
				// 状态变化（排队→执行→完成）要被读屏播报；整块没有名称。
				d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
			}).Children(func() {
			s.statusIcon(c.Key("icon"), m, task, exp)
			ui.Column(c.Key("text")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(func() {
				s.statusTitleBlock(c.Key("block"), m, exp)
			})
		})
		s.statusButtons(c.Key("buttons"), m, act)
	})
}

// statusTitleBlock 是图标右边的文字：展开样式活动时有一行小标题，然后是标题、设计阶段的实时进度、说明。容器是 text-xs leading-5。
func (s *Screen) statusTitleBlock(c ui.C, m StatusModel, exp bool) {
	l := s.Look
	t := l.T
	xs20 := kit.TextStyle{Size: kit.TextXS.Size, Line: 1.25}
	if exp && m.Headline != "" {
		// <p className="mb-1 text-xs text-muted-foreground">：text-xs 自带 16px 行高。
		l.Text(c.Key("headline"), m.Headline, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginBottom(l.Space(1))
	}
	if exp {
		// text-base font-semibold leading-6 tracking-tight
		l.Text(c.Key("title"), m.Title, kit.TextBase, kit.WeightSemibold, t.Foreground, kit.Wrapped())
	} else {
		l.Text(c.Key("title"), m.Title, xs20, kit.WeightMedium, t.Foreground.WithAlpha(0.85), kit.Wrapped())
	}
	if m.Detail != "" {
		l.Text(c.Key("detail"), m.Detail, xs20, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
	}
	ctx := l.Text(c.Key("context"), m.Context, xs20, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	if exp {
		ctx.MarginTop(l.Space(1.5))
	}
}

// statusIcon 是标题前的图标：执行中转圈，其余是信息图标；展开样式放在 44px 的圆角淡色块里。
func (s *Screen) statusIcon(c ui.C, m StatusModel, task services.ThemedTask, exp bool) {
	l := s.Look
	t := l.T
	size := l.Rem(0.875)
	if exp {
		size = l.Rem(1.25)
	}
	draw := func(c ui.C) {
		if m.Active {
			l.SpinIcon(c, icons.LoaderCircle, size, t.Primary)
			return
		}
		l.Icon(c, icons.Info, size, t.MutedForeground)
	}
	if !exp {
		ui.Row(c).Shrink(0).Items(ui.Center).Children(func() { draw(c.Key("glyph")) })
		return
	}
	// h-11 w-11 rounded-xl bg-primary/10 text-primary ring-1 ring-inset ring-primary/10
	ui.Row(c).Size(l.Rem(2.75), l.Rem(2.75)).Shrink(0).Items(ui.Center).Justify(ui.Center).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Border: t.Primary.WithAlpha(0.1), BorderW: 1, Radius: l.Rem(0.75)})
	}).Children(func() { draw(c.Key("glyph")) })
}

// pulse 是 animate-pulse 的不透明度：2 秒一周，1 → 0.5 → 1。
func pulse(canvas *gfx.Canvas) float32 {
	const period = 2 * time.Second
	phase := float32(canvas.Now()%period) / float32(period)
	canvas.RequestFrameIn(50 * time.Millisecond)
	return 1 - 0.5*(1-float32(math.Abs(float64(2*phase-1))))
}

// statusSteps 是阶段进度。展开样式是等分的一排（每格上面一条进度条），紧凑样式是折行排列的小标签；
// 已完成的带勾，当前的加粗主色，其余是弱化色。
func (s *Screen) statusSteps(c ui.C, steps []StatusStep, exp bool) {
	l := s.Look
	t := l.T
	xs20 := kit.TextStyle{Size: kit.TextXS.Size, Line: 1.25}
	// 展开样式是 grid-template-columns: repeat(auto-fit, minmax(3.25rem, 1fr))、列间距 6、行间距 16：
	// 一行放得下几格（至多全部）就几列，格子等宽，放不下的折到下一行（最后一行的格子不拉伸）。
	var cols int
	var cell float32
	gap := l.Space(1.5)
	rowGap := l.Space(4)
	boxID := ui.ID(c)
	if exp {
		if w := s.status.stepsW[boxID]; w > 0 && len(steps) > 0 {
			cols = max(1, min(len(steps), int((w+gap)/(l.Rem(3.25)+gap))))
			cell = (w - gap*float32(cols-1)) / float32(cols)
		}
	}
	item := func(st StatusStep) {
		kc := c.Key(st.Stage)
		ink, weight := t.MutedForeground, kit.WeightNormal
		bar := t.Border.WithAlpha(0.7)
		switch {
		case st.Current:
			ink, weight, bar = t.Primary, kit.WeightMedium, t.Primary
		case st.Done:
			ink, bar = t.Foreground.WithAlpha(0.75), t.Primary.WithAlpha(0.4)
		}
		body := func() {
			if exp {
				ui.Row(kc.Key("bar")).FillWidth().Height(l.Rem(0.25)).MarginBottom(l.Space(2.5)).Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Bg: bar, Radius: l.Rem(0.125)})
				})
			}
			ui.Row(kc.Key("label-row")).FillWidth().Items(ui.Center).Justify(ui.Center).Gap(l.Space(1)).Children(func() {
				if st.Done {
					l.Icon(kc.Key("check"), icons.Check, l.Rem(0.75), t.Primary.WithAlpha(0.7))
				}
				if oneWord(st.Label) {
					// 单词没有折行点：min-w-0 的 <li> 里 <span> 的最小宽度是整个词，格子比词窄时词与对勾一起溢出、居中。
					l.Text(kc.Key("label"), st.Label, xs20, weight, ink).Shrink(0)
				} else {
					l.Text(kc.Key("label"), st.Label, xs20, weight, ink, kit.Wrapped()).Shrink(1).MinWidth(0)
				}
			})
		}
		col := ui.Column(kc).MinWidth(0)
		if exp {
			if cell > 0 {
				col = col.Width(cell).Shrink(0)
			} else {
				col = col.Grow(1).Shrink(1).Basis(0)
			}
		}
		if st.Current && exp {
			col = col.Opacity(pulse(c.Canvas()))
		}
		col.Children(body)
	}
	if exp {
		measure := func(d *ui.DrawContext) {
			if w := d.Rect.Width; math.Abs(float64(w-s.status.stepsW[boxID])) > 0.25 {
				if s.status.stepsW == nil {
					s.status.stepsW = map[gfx.ID]float32{}
				}
				s.status.stepsW[boxID] = w
				d.Canvas.RequestFrame() // 宽度变了：按新宽度重排一次
			}
		}
		perRow := max(cols, 1)
		if cols == 0 {
			perRow = max(len(steps), 1) // 还没量过宽度：先排成一行，下一帧按实际宽度折行
		}
		ui.Column(c).FillWidth().Gap(rowGap).Draw(measure).Children(func() {
			for start := 0; start < len(steps); start += perRow {
				ui.Row(c.Key("row-" + strconv.Itoa(start))).FillWidth().Gap(gap).Children(func() {
					for _, st := range steps[start:min(start+perRow, len(steps))] {
						item(st)
					}
				})
			}
		})
		return
	}
	ui.Flow(c).FillWidth().Gap(l.Space(3)).LineGap(l.Space(2)).Children(func() {
		for _, st := range steps {
			item(st)
		}
	})
}

// statusButtons 是停止 / 继续（themeButton）。都是后台调用服务，所以回调里自己 spawn。「不联网继续」在详情里。
func (s *Screen) statusButtons(c ui.C, m StatusModel, act statusActions) {
	l := s.Look
	if m.ShowStop && act.Stop != nil {
		spec := kit.ButtonSpec{Label: locales.ThemedReadingStop(), Disabled: act.Busy}
		if l.Button(c.Key("stop"), spec, s.compactButton()).Clicked() {
			act.Stop()
		}
	}
	if m.ShowResume && act.Resume != nil {
		spec := kit.ButtonSpec{Label: m.ResumeLabel, Disabled: act.Busy}
		if l.Button(c.Key("resume"), spec, s.compactButton()).Clicked() {
			act.Resume()
		}
	}
}

// copyReason 复制失败原因到剪贴板；结果写在按钮文字上（没有提示条）。
func (s *Screen) copyReason(id, text string) {
	if s.status.copy == nil {
		s.status.copy = map[string]copyState{}
	}
	if s.Guard.Notify == nil {
		s.status.copy[id] = copyFailed
		return
	}
	if err := s.Guard.Notify.CopyText(text); err != nil {
		s.status.copy[id] = copyFailed
		return
	}
	s.status.copy[id] = copyDone
}

// statusDetails 是折叠的「生成详情」：summary 一行（图标、文字，展开样式右侧有「阶段 n / m」），展开后是可滚动的内容：
// 紧凑样式先放阶段进度；失败原因与复制钮；各行统计与耗时；用量；活动时的后台提示；查证暂停时的「不联网继续」。
func (s *Screen) statusDetails(c ui.C, id string, m StatusModel, exp bool, act statusActions) {
	l := s.Look
	t := l.T
	if s.status.open == nil {
		s.status.open = map[string]bool{}
	}
	open := s.status.open[id]
	name := locales.ThemedReadingReaderTaskDetails()
	if exp && m.StageCount != "" {
		name += " " + m.StageCount
	}
	var wrap ui.Box
	if exp {
		// group border-t border-border/50 bg-muted/20 px-6 text-xs text-muted-foreground
		last := !(m.Active && m.BackgroundHint != "")
		wrap = ui.Column(c).FillWidth().PadSides(1, l.Space(6), 0, l.Space(6)).Draw(func(d *ui.DrawContext) {
			s.paintDetailsBg(d, last)
			l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.5))
		})
	} else {
		wrap = ui.Column(c).MarginTop(l.Space(1))
	}
	wrap.Children(func() {
		padY := l.Space(1)
		row := ui.Row(c.Key("toggle")).Items(ui.Center).Gap(l.Space(1.5))
		if exp {
			padY = l.Space(3)
			row = row.FillWidth()
		} else {
			row = row.Self(ui.Start) // w-fit
		}
		row = row.PadSides(padY, 0, padY, 0)
		did := ui.ID(c.Key("toggle-id"))
		box, _ := s.hitDisclosure(c, did, row, name, open)
		hovered := box.State().Hovered
		box.Draw(func(d *ui.DrawContext) {
			if d.State.FocusVisible {
				l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.25))
			}
		}).Children(func() {
			ink := kit.MixToken(t.MutedForeground, t.Foreground, boolF(hovered))
			icon := icons.ChevronRight
			if open {
				icon = icons.ChevronDown
			}
			l.Icon(c.Key("chevron"), icon, l.Rem(0.875), ink)
			l.Text(c.Key("label"), locales.ThemedReadingReaderTaskDetails(), kit.TextXS, kit.WeightNormal, ink)
			if exp && m.StageCount != "" {
				ui.Row(c.Key("count-box")).Grow(1).Justify(ui.End).PadSides(0, 0, 0, l.Space(3)).Children(func() {
					l.Text(c.Key("count"), m.StageCount, kit.TextXS, kit.WeightNormal, ink)
				})
			}
		})
		if box.Clicked() {
			s.status.open[id] = !open
		}
		if !open {
			return
		}
		l.ScrollAreaFlush(c.Key("body"), [4]float32{l.Space(2), 0, l.Space(4), 0}, func() {
			ui.Column(c.Key("lines")).FillWidth().Gap(l.Space(3)).Children(func() {
				s.statusDetailBody(c.Key("content"), id, m, exp, act)
			})
		}).FillWidth().MaxHeight(l.Rem(16))
	})
}

// paintDetailsBg 画详情的淡底（bg-muted/20）。卡片是 overflow-hidden 的圆角，详情在最底下时两个下角要跟着卡片的圆角收：
// 用一块比详情高出很多的圆角矩形（下角半径 = 卡片圆角 − 边框），再裁到详情自己的范围。
func (s *Screen) paintDetailsBg(d *ui.DrawContext, last bool) {
	l := s.Look
	fill := l.T.Muted.WithAlpha(0.2)
	if !last {
		l.PaintSurface(d, kit.Surface{Bg: fill})
		return
	}
	ext := *d
	ext.Rect = gfx.Rect{X: d.Rect.X, Y: d.Rect.Y - 4*l.Rem(1), Width: d.Rect.Width, Height: d.Rect.Height + 4*l.Rem(1)}
	l.PaintSurface(&ext, kit.Surface{Bg: fill, Radius: l.Rem(1) - 1, Clip: d.Rect})
}

// statusDetailBody 是详情展开后的各块，块间 space-y-3。
func (s *Screen) statusDetailBody(c ui.C, id string, m StatusModel, exp bool, act statusActions) {
	l := s.Look
	t := l.T
	if !exp && len(m.Steps) > 0 {
		s.statusSteps(c.Key("steps"), m.Steps, false)
	}
	if m.Error != "" {
		s.errorBlock(c.Key("error"), id, m.Error)
	}
	for i, line := range m.Lines {
		s.relaxedLine(c.Key("line"+strconv.Itoa(i)), line)
	}
	if len(m.Usage) > 0 {
		ui.Flow(c.Key("usage")).FillWidth().Gap(l.Space(4)).LineGap(l.Space(1)).Children(func() {
			for i, u := range m.Usage {
				l.Text(c.Key("usage"+strconv.Itoa(i)), u, kit.TextXS, kit.WeightNormal, t.MutedForeground)
			}
		})
	}
	if !exp && m.BackgroundHint != "" {
		s.relaxedLine(c.Key("hint"), m.BackgroundHint)
	}
	if m.ShowOffline && act.Offline != nil {
		spec := kit.ButtonSpec{Label: locales.ThemedReadingReaderOffline(), Disabled: act.Busy}
		ui.Row(c.Key("offline-row")).Children(func() {
			if l.Button(c.Key("offline"), spec, s.detailsButton()).Clicked() {
				act.Offline()
			}
		})
	}
}

// relaxedLine 是详情里的一行说明：text-xs，行高是继承来的 16px（<p> 没有 leading 类）。
func (s *Screen) relaxedLine(c ui.C, text string) {
	l := s.Look
	l.Text(c, text, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
}

// errorBlock 是失败原因：保留换行的淡底块，下面是「复制详细原因」钮，复制失败时再多一行提示。
func (s *Screen) errorBlock(c ui.C, id, message string) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
		// select-text whitespace-pre-wrap break-words rounded-lg bg-muted/50 p-3 leading-relaxed text-foreground/75
		ui.Column(c.Key("text")).FillWidth().Pad(l.Space(3)).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.5), Radius: l.Rem(0.5)})
		}).Children(func() {
			l.TextPreWrap(c.Key("message"), message, "", l.Rem(kit.TextXS.Size), l.Rem(kit.TextXS.Size*1.625), kit.WeightNormal,
				t.Foreground.WithAlpha(0.75), kit.PreWrapView{})
		})
		state := s.status.copy[id]
		label := locales.ThemedReadingReaderCopyError()
		if state == copyDone {
			label = locales.ThemedReadingReaderCopied()
		}
		ui.Row(c.Key("copy-row")).Children(func() {
			if l.Button(c.Key("copy"), kit.ButtonSpec{Label: label}, s.detailsButton()).Clicked() {
				s.copyReason(id, message)
			}
		})
		if state == copyFailed {
			// <p role="alert">：继承详情的 text-xs 弱化色。
			l.Text(c.Key("failed"), locales.ThemedReadingReaderCopyFailed(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).
				Draw(func(d *ui.DrawContext) {
					d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
				})
		}
	})
}

func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// detailsButton 是详情区里的按钮：themeButton 不设文字色，继承详情容器的 text-muted-foreground。
func (s *Screen) detailsButton() kit.ButtonStyle {
	style := s.compactButton()
	style.Fg = s.Look.T.MutedForeground
	return style
}

// oneWord 报告 s 是不是一个没有折行点的词（没有空白、没有汉字假名）：这样的文字在浏览器里最小宽度就是它的整个宽度。
func oneWord(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || r >= 0x2E80 {
			return false
		}
	}
	return s != ""
}
