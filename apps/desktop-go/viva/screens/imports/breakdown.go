package imports

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 阶段耗时的可视化（渲染层 components/imports/ImportStageBreakdown.tsx）：一条堆叠时间条加一份图例。
//
// 阶段是顺序的、各段之和恰好等于总耗时，所以堆叠条就是这条任务的时间线，段宽即占比。
// 刻意不判「快慢」：什么叫慢取决于模型、网络与内容长度，写死阈值在慢渠道上会天天误报。

const (
	// negligibleMs 是图例里折叠的阶段上限：不足一秒的阶段四舍五入显示成 0:00，单独占一行是纯噪音。
	negligibleMs = 1000
	// stageRootGroup 是整块容器（间隙、「另有 N 个」钮）在悬停分组里的名字；不会与阶段名重名。
	stageRootGroup = "\x00root"
	// stageDimmed 是悬停时其余段的不透明度（opacity-30）。
	stageDimmed = 0.3
	// stageTextPx 是整块的字号（text-[11px]：px 字面量，不随字号档缩放），行高继承正文的 1.5 倍。
	stageTextPx   = 11
	stageTextLine = stageTextPx * 1.5
)

// slowestIndex 是最慢阶段的下标：第一个最大值（渲染层按对象身份比较，并列时取靠前的那个）。没有阶段时是 -1。
func slowestIndex(stats []services.ImportStageStat) int {
	slow := -1
	for i, stat := range stats {
		if slow < 0 || stat.Ms > stats[slow].Ms {
			slow = i
		}
	}
	return slow
}

// stageTone 是时间条与图例圆点的着色：相邻两段交替深浅即可区分，只有最慢的那段换成琥珀色。
// 走 primary 的透明度档位而不是硬编码色值，换主题时跟着走（bg-primary/75、bg-primary/40、bg-amber-500/80）。
func (s *Screen) stageTone(index int, slowest bool) tokens.Color {
	switch {
	case slowest:
		return tokens.Tailwind("amber-500").WithAlpha(0.8)
	case index%2 == 0:
		return s.Look.T.Primary.WithAlpha(0.75)
	default:
		return s.Look.T.Primary.WithAlpha(0.4)
	}
}

// stageText 是整块里的文字：11px，行高 16.5px。
func (s *Screen) stageText(c ui.C, text string, color tokens.Color, opts ...kit.TextOpt) ui.Box {
	return s.Look.TextPx(c, text, stageTextPx, stageTextLine, kit.WeightNormal, color, opts...)
}

// syncStageHover 把指针此刻所在的命中区折成「悬停中的阶段」：渲染层逐格挂 onMouseEnter、由容器统一在 onMouseLeave 清空，
// 单元格之间有间隙，指针划过间隙时保持上一次悬停，离开整块才清空。
func (s *Screen) syncStageHover(canvas *gfx.Canvas) {
	d := &s.detail
	switch group := d.hover.Hovered(canvas); group {
	case "":
		d.hovered = ""
	case stageRootGroup:
		// 在整块的间隙里：保持
	default:
		d.hovered = group
	}
}

// stageBreakdown 画阶段耗时：读数行、堆叠时间条、图例，以及「另有 N 个阶段不足 1 秒」。
func (s *Screen) stageBreakdown(c ui.C, stats []services.ImportStageStat) {
	d := &s.detail
	total, _ := SummarizeStages(stats)
	slow := slowestIndex(stats)
	d.hover.Begin()
	s.syncStageHover(c.Canvas())

	// 全部阶段都不足一秒时（纯文本笔记）照原样列出：此时「另有 N 个」的「另」指不到任何东西，一行不剩更像是渲染坏了。
	var notable []int
	for i, stat := range stats {
		if stat.Ms >= negligibleMs {
			notable = append(notable, i)
		}
	}
	legend := notable
	if d.showAll || len(notable) == 0 {
		legend = make([]int, len(stats))
		for i := range legend {
			legend[i] = i
		}
	}
	negligible := len(stats) - len(legend)

	root := c.Key("root")
	rootID := ui.ID(root)
	d.hover.Add(stageRootGroup, rootID)
	ui.Column(root).FillWidth().
		HitWith(rootID, "", gfx.HitOptions{SkipFocus: true, SkipTab: true}).
		Children(func() {
			if total > 0 {
				s.stageReadout(root.Key("head"), stats, total, slow)
			}
			s.stageLegend(root.Key("legend"), stats, legend, total, slow, negligible)
		})
}

// stageReadout 是时间条上方的读数与时间条本身：常驻一行读数，悬停时就地换成那一段（空着的话悬停要么撑出一行，
// 要么只能走 title 气泡，在条上划过时很钝）。mb-2 包着 mb-1.5 的读数行与时间条。
func (s *Screen) stageReadout(c ui.C, stats []services.ImportStageStat, total int64, slow int) {
	l := s.Look
	t := l.T
	active := -1
	for i, stat := range stats {
		if stat.Stage == s.detail.hovered {
			active = i
			break
		}
	}
	ui.Column(c).FillWidth().MarginBottom(l.Space(2)).Children(func() {
		// flex items-baseline justify-between gap-2；两端文字同字号同行高，基线对齐就是顶对齐。
		ui.Row(c.Key("readout")).FillWidth().Justify(ui.Between).Gap(l.Space(2)).MarginBottom(l.Space(1.5)).Children(func() {
			if active < 0 {
				s.stageText(c.Key("name"), locales.ImportsTotalElapsed(FormatDuration(total)), t.MutedForeground)
				return
			}
			stat := stats[active]
			s.stageText(c.Key("name"), StageLabel(stat.Stage), t.Foreground)
			s.stageText(c.Key("value"), FormatDuration(stat.Ms)+" \u00b7 "+FormatShare(stat.Ms, total), t.MutedForeground)
		})
		s.stageTimeline(c.Key("timeline"), stats, total, slow)
	})
}

// stageTimeline 是堆叠时间条：flex h-2.5 w-full overflow-hidden rounded-full bg-border/60，段宽是占比。
// 亚秒阶段照样进条里（它们的宽度本就看不见），只是不进图例——把它们从条里摘掉会让各段之和不再等于总耗时。
//
// 悬停时把其余段压暗而不是给当前段描边：段高只有 10px，描边挤不下，而「其余变淡」在任意窄的段上都看得出来。
// overflow-hidden 加圆角会把两端的段裁成圆头：Viva 的裁剪只有矩形，所以每一段都按「整条轨道的圆角矩形」画，再用段自己的矩形裁。
func (s *Screen) stageTimeline(c ui.C, stats []services.ImportStageStat, total int64, slow int) {
	l := s.Look
	t := l.T
	d := &s.detail
	var track gfx.Rect // 轨道的外框：父级先画，各段随后画时读它
	ui.Row(c).FillWidth().Height(l.Rem(0.625)).Shrink(0).
		Draw(func(dc *ui.DrawContext) {
			track = kit.SnapRect(dc.Rect, dc.Canvas.PixelScale())
			l.PaintSurface(dc, kit.Surface{Bg: t.Border.WithAlpha(0.6), Radius: 9999})
		}).
		Children(func() {
			for i, stat := range stats {
				cc := c.Key("seg-" + stat.Stage)
				id := ui.ID(cc)
				d.hover.Add(stat.Stage, id)
				dimmed := d.hovered != "" && d.hovered != stat.Stage
				p := l.HoverProgress(cc, id, 0, dimmed, kit.DurationQuick) // transition-opacity duration-quick
				tone := s.stageTone(i, i == slow)
				ui.Row(cc).WidthPercent(float32(stat.Ms)/float32(total)*100).FillHeight().Shrink(0).
					HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true}).
					Draw(func(dc *ui.DrawContext) {
						r := kit.SnapRect(dc.Rect, dc.Canvas.PixelScale())
						if r.Width <= 0 || track.Height <= 0 {
							return
						}
						color := tone.WithAlpha(tone.A * (1 - (1-stageDimmed)*p))
						dc.Canvas.Clip(r)
						dc.Canvas.Rounded(track, track.Height/2, dc.Fade(l.Col(color)))
						dc.Canvas.Unclip()
					})
			}
		})
}

// ceil64 把宽度向上取到 1/64 像素（Chromium 的 LayoutUnit）：文字量出来的宽度恰好放进同样宽的盒子，不被省略号吃掉。
func ceil64(v float32) float32 { return float32(math.Ceil(float64(v)*64) / 64) }

// stageLegend 是图例：一行一个阶段，但整块图例共用同一套列宽（渲染层是同一个 grid），名称列按最宽的那行自适应，
// 数字列逐行对齐。grid-cols-[0.5rem_auto_3rem_2.5rem_minmax(0,1fr)] items-center gap-x-2 gap-y-1。
// 每行用一个整行的命中区感知悬停：行内单元格之间的横向间隙属于这一行，行与行之间的纵向间隙属于整块（保持上一次的悬停）。
func (s *Screen) stageLegend(c ui.C, stats []services.ImportStageStat, legend []int, total int64, slow, negligible int) {
	l := s.Look
	t := l.T
	d := &s.detail
	canvas := c.Canvas()

	nameW := float32(0)
	for _, i := range legend {
		nameW = max(nameW, canvas.TextWidthWeight(StageLabel(stats[i].Stage), stageTextPx, kit.WeightNormal))
	}
	nameW = ceil64(nameW)

	ui.Column(c).FillWidth().Gap(l.Space(1)).Children(func() {
		for _, i := range legend {
			stat := stats[i]
			hovered := d.hovered == stat.Stage
			cc := c.Key("row-" + stat.Stage)
			id := ui.ID(cc)
			d.hover.Add(stat.Stage, id)
			strong := t.MutedForeground
			if hovered {
				strong = t.Foreground
			}
			ui.Row(cc).FillWidth().Items(ui.Center).Gap(l.Space(2)).
				HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true}).
				Children(func() {
					tone := s.stageTone(i, i == slow)
					ui.Row(cc.Key("dot")).Size(l.Rem(0.5), l.Rem(0.5)).Shrink(0).
						Draw(func(dc *ui.DrawContext) {
							l.PaintSurface(dc, kit.Surface{Bg: tone, Radius: l.Rem(0.75) - 4}) // rounded-sm
						})
					s.stageText(cc.Key("name"), StageLabel(stat.Stage), strong, kit.Ellipsis()).Width(nameW).Shrink(0)
					s.stageText(cc.Key("duration"), FormatDuration(stat.Ms), t.Foreground, kit.Right()).Width(l.Rem(3)).Shrink(0)
					s.stageText(cc.Key("share"), FormatShare(stat.Ms, total), strong, kit.Right()).Width(l.Rem(2.5)).Shrink(0)
					s.stageText(cc.Key("detail"), AIDetail(stat), t.MutedForeground, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
				})
		}
		if negligible > 0 {
			s.negligibleButton(c.Key("negligible"), negligible)
		}
	})
}

// negligibleButton 是「另有 N 个阶段不足 1 秒」：col-span-5 pl-4 text-left text-muted-foreground transition-colors hover:text-foreground。
// 点开后图例列出全部阶段。
func (s *Screen) negligibleButton(c ui.C, count int) {
	l := s.Look
	t := l.T
	label := locales.ImportsStageNegligible(count)
	id := ui.ID(c)
	s.detail.hover.Add(stageRootGroup, id)
	box := ui.Row(c).FillWidth().PadSides(0, 0, 0, l.Space(4)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	color := kit.MixToken(t.MutedForeground, t.Foreground, p)
	box = box.Draw(func(dc *ui.DrawContext) {
		if dc.State.FocusVisible {
			l.PaintRing(dc, 2, t.Primary.WithAlpha(0.5), 0) // 全局 button:focus-visible
		}
	})
	box.Children(func() { s.stageText(c.Key("label"), label, color) })
	if box.Clicked() {
		s.detail.showAll = true
	}
}
