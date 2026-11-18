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
		return func(c ui.C, fg tokens.Color) { l.TextPx(c, emoji, l.Rem(1), l.Rem(1), kit.WeightNormal, fg) }
	}
	ui.Column(c.Key("picker")).FillWidth().Gap(l.Space(1.5)).Children(func() {
		l.Text(c.Key("title"), locales.LibraryCollectionIcon(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
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

// iconGrid 是滚动的图标目录：relative max-h-64 overflow-y-auto rounded-xl border border-border/60 bg-muted/20 p-2，
// 八个分组，每组一条小标题加一排 h-9 w-9 的格子（flex flex-wrap gap-1）。
func (s *Screen) iconGrid(c ui.C, glyph func(string) func(c ui.C, fg tokens.Color)) {
	l := s.Look
	t := l.T
	e := &s.edit
	groups := collectionIconGroups()
	pad := l.Space(2) + 1 // p-2 加 1px 边框
	radius := l.Rem(0.75) // rounded-xl
	var cont gfx.Rect
	var state *ui.ScrollState
	l.ScrollAreaView(c.Key("grid"), [4]float32{pad, pad, pad, pad}, func(view kit.ScrollView) {
		state = view.State
		for gi, g := range groups {
			group := ui.Column(c.Key("group-" + g.ID)).FillWidth()
			if gi < len(groups)-1 {
				group = group.MarginBottom(l.Space(2)) // mb-2 last:mb-0
			}
			group.Children(func() {
				l.TextTracked(c.Key("label"), strings.ToUpper(g.Label()), kit.TextXS, kit.WeightSemibold, t.MutedForeground, 0.05)
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
	}).FillWidth().MaxHeight(l.Rem(16)).Draw(func(d *ui.DrawContext) {
		cont = d.Rect
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: radius})
	})
	if e.picker.frames < scrollRetryFrames {
		e.picker.frames++
	}
}

// scrollToSelected 把已选的格子滚到容器正中（渲染层 useLayoutEffect：按容器坐标手算，不用 scrollIntoView，
// 后者会连带滚动弹窗本身的滚动容器）。只在打开后的头几帧里做，内容高度布局之后才收敛。
func (s *Screen) scrollToSelected(d *ui.DrawContext, cont gfx.Rect, state *ui.ScrollState) {
	e := &s.edit
	if e.picker.frames >= scrollRetryFrames || state == nil || cont.Height == 0 {
		return
	}
	// offsetTop：格子上沿到容器内边距盒上沿的距离（含已滚过的部分）；clientHeight：内边距盒的高。
	offsetTop := d.Rect.Y - (cont.Y + 1) + state.Offset
	clientHeight := cont.Height - 2
	target := float32(math.Max(0, float64(offsetTop-(clientHeight-d.Rect.Height)/2)))
	if math.Abs(float64(state.Offset-target)) > 0.5 {
		state.ScrollTo(target)
		d.Canvas.RequestFrame()
	}
}

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
			l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), radius) // 全局 button:focus-visible
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
