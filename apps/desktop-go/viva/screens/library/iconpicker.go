package library

import (
	"math"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// iconPickerState 是图标选择器的界面状态：打开后把已选的那格滚进视野只做一次。
type iconPickerState struct {
	frames int // 打开后已经尝试过的帧数：滚动区的内容高度要布局之后才知道，头几帧里重试
}

// scrollRetryFrames 是打开后重试「滚到已选」的帧数上限；之后用户的滚动不再被覆盖。
const scrollRetryFrames = 4

// iconPicker 是合集图标选择器（渲染层 CollectionIconPicker）：默认图标与「当前图标」（目录外的）各一格钉在上面，
// 其余按 collection-icons 的分组滚动列出。分组标题不做成筛选页签：「该算学习还是生活」只有作者自己清楚，逼用户先猜对分类
// 才看得见图标，比一片平铺还慢；一条滚动带小标题只是扫视辅助。默认图标不跟着滚：它同时是「清掉图标」的唯一入口。
func (s *Screen) iconPicker(c ui.C) {
	l := s.Look
	t := l.T
	e := &s.edit
	glyph := func(emoji string) func(c ui.C, fg tokens.Color) {
		return func(c ui.C, fg tokens.Color) { s.emojiGlyph(c, emoji, l.Rem(1), 0, fg) } // 格子宽 2.25rem，比字形宽：居中不溢出
	}
	ui.Column(c.Key("picker")).FillWidth().Gap(l.Space(1.5)).Children(func() {
		s.strutLine(c.Key("title-line"), kit.TextXS, kit.WeightNormal, func() {
			l.Text(c.Key("title"), locales.LibraryCollectionIcon(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
		// 这两个不是「另外两个图标」，写上文字才说得清各自是什么：flex items-center gap-1.5 px-2
		ui.Row(c.Key("pills")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).PadSides(0, l.Space(2), 0, l.Space(2)).Children(func() {
			s.iconChip(c.Key("default"), chipSpec{Name: locales.LibraryCollectionIconDefault(), Selected: e.icon == "", Pill: true,
				Content: func(c ui.C, fg tokens.Color) { l.Icon(c, icons.Folder, l.Rem(1), fg) }, Label: locales.LibraryCollectionIconDefault()},
				func() { e.icon = "" })
			if e.custom != "" {
				s.iconChip(c.Key("custom"), chipSpec{Name: e.custom + " " + locales.LibraryCollectionIconCurrent(), Selected: e.icon == e.custom, Pill: true,
					Content: glyph(e.custom), Label: locales.LibraryCollectionIconCurrent()},
					func() { e.icon = e.custom })
			}
		})
		s.iconGrid(c, glyph)
	})
}

// strutLine 是「块容器里直接放一个行内 span」的排法（渲染层：<div><span className="text-xs …">）。
// span 落在匿名行盒里，行盒的高度由容器继承来的正文行高撑起——行高 1.5、根字号 16px 时 24px（1.5rem），
// 不是 span 自己的 16px；文字与行盒的基线对齐，文字上方空出的高度由 kit.StrutTextTop 按渲染层的取整算
// （根字号 16px 时 6px；14px 是 4px、18px 是 6px，不能按 rem 缩放）。style、weight 是那个 span 的字号档与字重。
func (s *Screen) strutLine(c ui.C, style kit.TextStyle, weight float32, children func()) {
	l := s.Look
	top := kit.StrutTextTop(float64(l.Rem(1)), float64(l.Rem(style.Size)), float64(l.Rem(style.Line)), weight)
	ui.Row(c).FillWidth().Height(l.Rem(1.5)).PadSides(float32(top), 0, 0, 0).Children(children)
}

// iconGrid 是滚动的图标目录：relative max-h-64 overflow-y-auto rounded-xl border border-border/60 bg-muted/20 p-2，
// 八个分组，每组一条小标题加一排 h-9 w-9 的格子（flex flex-wrap gap-1）。
func (s *Screen) iconGrid(c ui.C, glyph func(string) func(c ui.C, fg tokens.Color)) {
	l := s.Look
	t := l.T
	e := &s.edit
	groups := collectionIconGroups()
	pad := l.Space(2)     // p-2
	radius := l.Rem(0.75) // rounded-xl
	var cont gfx.Rect
	var state *ui.ScrollState
	// 边框画在外层，滚动区放在边框之内：滚动的内容按内边距盒裁剪、不会盖到那 1px 边框上（渲染层 overflow 就是这样裁的）；
	// max-h-64 是边框盒的高，滚动区的上限相应少 2px。
	ui.Column(c.Key("grid-frame")).FillWidth().MaxHeight(l.Rem(16)).PadSides(1, 1, 1, 1).Draw(func(d *ui.DrawContext) {
		cont = d.Rect
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: radius})
	}).Children(func() {
		l.ScrollAreaView(c.Key("grid"), [4]float32{pad, pad, pad, pad}, func(view kit.ScrollView) {
			state = view.State
			for gi, g := range groups {
				group := ui.Column(c.Key("group-" + g.ID)).FillWidth()
				if gi < len(groups)-1 {
					group = group.MarginBottom(l.Space(2)) // mb-2 last:mb-0
				}
				group.Children(func() {
					s.strutLine(c.Key("label-line"), kit.TextXS, kit.WeightSemibold, func() {
						l.TextTracked(c.Key("label"), strings.ToUpper(g.Label()), kit.TextXS, kit.WeightSemibold, t.MutedForeground, 0.05)
					})
					ui.Flow(c.Key("icons")).FillWidth().Gap(l.Space(1)).LineGap(l.Space(1)).MarginTop(l.Space(1)).Children(func() {
						for _, icon := range g.Icons {
							selected := e.icon == icon
							swatch := s.iconChip(c.Key("swatch-"+icon), chipSpec{Name: icon, Selected: selected, Content: glyph(icon)}, func() { e.icon = icon })
							if selected {
								swatch.DrawOver(func(d *ui.DrawContext) { s.scrollToSelected(d, cont, state) })
							}
						}
					})
				})
			}
		}).FillWidth().MaxHeight(l.Rem(16) - 2)
	})
	if e.picker.frames < scrollRetryFrames {
		e.picker.frames++
	}
}

// scrollToSelected 把已选的格子滚到容器正中（渲染层 useLayoutEffect：按容器坐标手算，不用 scrollIntoView，
// 后者会连带滚动弹窗本身的滚动容器）。只在打开后的头几帧里做，内容高度布局之后才收敛。
//
// 渲染层用的是 DOM 的整数度量：offsetTop、clientHeight、offsetHeight 都是取整之后的整数，
//
//	scrollTop = max(0, offsetTop − (clientHeight − offsetHeight) / 2)
//
// 再落到整像素上（半个像素进位）。格子 2.25rem 在大字号档是 40.5px，按小数算与渲染层差一个像素
// （offsetTop 796.5 取整成 797、offsetHeight 40.5 取整成 41，目标 674.5 进位成 675，而不是 673.75；2026-11-18 量到）。
// cont 是带边框的外框：内边距盒比它四边各小 1px。
func (s *Screen) scrollToSelected(d *ui.DrawContext, cont gfx.Rect, state *ui.ScrollState) {
	e := &s.edit
	if e.picker.frames >= scrollRetryFrames || state == nil || cont.Height == 0 {
		return
	}
	if target, move := scrollCorrection(d.Rect.Y, d.Rect.Height, state.Offset, cont.Y, cont.Height); move {
		state.ScrollTo(target)
		d.Canvas.RequestFrame()
	}
}

// scrollCorrection 算已选格子要滚到的位置，以及当前位置要不要动：cellY、cellHeight 是格子此刻的上沿与高（已含滚动），
// offset 是滚动区此刻的滚动量，contY、contHeight 是带边框的外框。布局用的是取整后的滚动量：格子当前的位置加上它，
// 才是格子在内容里（不含滚动）的位置；已经落在目标的半个像素之内就不再动（免得每帧都请求下一帧）。
func scrollCorrection(cellY, cellHeight, offset, contY, contHeight float32) (target float32, move bool) {
	applied := roundHalfUp(offset)
	target = selectedScrollTarget(cellY+applied, cellHeight, contY, contHeight)
	return target, math.Abs(float64(applied-target)) > 0.5
}

// selectedScrollTarget 算把已选格子滚到容器正中的滚动量，全部按 DOM 的整数度量：
// cellTop、cellHeight 是格子在不含滚动时的上沿与高，contTop、contHeight 是带 1px 边框的外框（内边距盒四边各小 1px）。
// offsetTop 是格子上沿到内边距盒上沿的距离，offsetHeight 是格子按像素对齐后的高，clientHeight 是内边距盒的高。
func selectedScrollTarget(cellTop, cellHeight, contTop, contHeight float32) float32 {
	offsetTop := roundHalfUp(cellTop - (contTop + 1))
	offsetHeight := roundHalfUp(cellTop+cellHeight) - roundHalfUp(cellTop)
	clientHeight := roundHalfUp(contHeight - 2)
	return roundHalfUp(float32(math.Max(0, float64(offsetTop-(clientHeight-offsetHeight)/2))))
}

// roundHalfUp 是 DOM 取整的口径：半个像素向上进位（LayoutUnit::Round）。
func roundHalfUp(v float32) float32 { return float32(math.Floor(float64(v) + 0.5)) }

// chipSpec 描述选择器里的一格：h-9 的图标格（正方形）或带文字的胶囊。
type chipSpec struct {
	Name     string
	Selected bool
	// Pill 为真时是胶囊（inline-flex h-9 gap-1.5 rounded-lg border px-2.5 text-xs），否则是 h-9 w-9 的格子。
	Pill    bool
	Content func(c ui.C, fg tokens.Color)
	Label   string // 胶囊里图标之后的文字
}

// iconChip 声明一格并在被点击时调用 pick：border + rounded-lg + transition-colors；
// 选中 border-primary/50 bg-primary/10 text-primary，否则 border-border text-muted-foreground，悬停 bg-muted/60。
func (s *Screen) iconChip(c ui.C, spec chipSpec, pick func()) ui.Box {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	size := l.Rem(2.25) // h-9
	box := ui.Row(c).Height(size).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, spec.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: spec.Selected, Cursor: gfx.PointerCursorHand})
	if spec.Pill {
		box = box.Gap(l.Space(1.5)).PadSides(0, l.Space(2.5)+1, 0, l.Space(2.5)+1)
	} else {
		box = box.Width(size)
	}
	l.TabStop(box)

	ph := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	pa := l.HoverProgress(c, id, 1, spec.Selected, kit.DurationDefault)
	bg := kit.MixToken(kit.MixToken(tokens.Transparent, t.Muted.WithAlpha(0.6), ph), t.Primary.WithAlpha(0.1), pa)
	border := kit.MixToken(t.Border, t.Primary.WithAlpha(0.5), pa)
	fg := kit.MixToken(t.MutedForeground, t.Primary, pa)
	radius := l.Rem(0.75)

	box = box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius) // 全局 button:focus-visible；格子底色透明或半透明：环要空心
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: radius})
	}).Children(func() {
		spec.Content(c.Key("glyph"), fg)
		if spec.Label != "" {
			l.Text(c.Key("label"), spec.Label, kit.TextXS, kit.WeightNormal, fg)
		}
	})
	if box.Clicked() {
		pick()
	}
	return box
}
