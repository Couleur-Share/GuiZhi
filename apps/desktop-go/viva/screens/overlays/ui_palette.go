package overlays

// 全局命令面板的界面（渲染层 components/app/GlobalCommandPalette.tsx）：顶部对齐的整窗搜索浮层，
// 搜索框（打开即聚焦）加分组结果列表，↑↓ 选择、Enter 执行、点击执行、Esc 与点遮罩关闭。
// 状态与搜索在 Palette（palette.go）；这里只画并把输入交给它。

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

const (
	// paletteListMaxFraction 是结果列表的最大高度占窗口高度的比例（max-h-[60vh]）。
	paletteListMaxFraction = 0.6
	// paletteScrollMargin 是键盘移动选中项时，让它离视口边缘至少多远（渲染层的 DOM 滚动没有这条，Viva 的滚动区不会自己跟随选中项）。
	paletteScrollMargin = 4
)

// paletteUI 是命令面板自己的界面状态：搜索框、开合的上升沿与键盘移动选中项之后的滚动跟随。
type paletteUI struct {
	input  ui.Field
	opened bool // 上一帧面板是否开着：由关到开的那一帧清空搜索框
	// hover 是上一帧指针悬停的行（-1 没有）：只在指针「进入」一行时才把它设为选中项（onMouseEnter），
	// 否则键盘移动选中项之后，停在原处的指针会立刻把它拉回去。
	hover int
	// reveal 为真表示键盘刚移动了选中项：选中行在这一帧画出时，若不在视口里就把滚动区滚到能看见它。
	reveal bool
	// state 与 viewport 是本帧结果列表的滚动状态与视口外框（行的 DrawOver 据此判断选中行是否可见）。
	state    *ui.ScrollState
	viewport gfx.Rect
}

// paletteView 声明命令面板。每一帧都要经过它：关着的那一帧负责复位。
func (s *Screen) paletteView(c ui.C) {
	l := s.Look
	v := s.Palette.View()
	p := &s.palette
	if !v.Open {
		p.opened, p.hover, p.reveal = false, -1, false
		return
	}
	if !p.opened {
		p.input.SetText("") // 每次打开都是空白的搜索框（渲染层 show：setQuery("")）
		p.hover = -1
	}
	p.opened = true
	// 搜索框里的文字是输入的来源：变了就交给 Palette（它自己防抖、取消过期的请求）。
	if text := p.input.Text(); text != v.Query {
		s.Palette.SetQuery(text)
		v = s.Palette.View()
	}
	if l.PaletteLayer(c.Key("palette"), kit.PaletteLayerSpec{
		Open: true, Label: locales.CommandPaletteTitle(),
		Body: func(c ui.C) { s.paletteBody(c, v) },
	}) {
		s.Palette.Close()
	}
}

// paletteBody 是面板内容：搜索框、结果列表与底部提示条。
func (s *Screen) paletteBody(c ui.C, v PaletteView) {
	ui.Column(c.Key("body")).FillWidth().MinHeight(0).Children(func() {
		s.paletteSearchBox(c)
		s.paletteList(c, v)
		s.paletteFooter(c, v)
	})
}

// paletteSearchBox 是搜索框：relative border-b border-border，左侧 16px 搜索图标（left-4），输入框 h-12 pl-11 pr-4 text-sm。
func (s *Screen) paletteSearchBox(c ui.C) {
	l := s.Look
	t := l.T
	c = c.Key("search")
	anim := ui.ID(c.Key("ring"))
	var ring float32 // 聚焦环进度：输入后声明、环在绘制时读（同 SettingsInput）
	ui.Stack(c).FillWidth().Height(l.Rem(3) + 1).Draw(func(d *ui.DrawContext) {
		l.PaintEdge(d, kit.EdgeBottom, 1, t.Border)
		// 渲染层的 input:focus-visible { ring-2 ring-primary/50 }：输入框 h-12 铺满面板，环只剩下沿 2px 可见（面板 overflow-hidden）。
		box := d.Rect
		box.Height = l.Rem(3)
		sub := *d
		sub.Rect = box
		l.PaintInputRingBelow(&sub, ring)
	}).Children(func() {
		box := l.Input(c.Key("input"), &s.palette.input, locales.CommandPalettePlaceholder(), locales.CommandPalettePlaceholder(), kit.InputSpec{
			Size: l.Rem(0.875), Weight: kit.WeightNormal, Color: t.Foreground,
			Placeholder: tokens.Tailwind("gray-400"), // 渲染层的 <input> 没有 placeholder 类：Tailwind 预检的默认占位色 gray-400
			Height:      l.Rem(3), Left: l.Space(11), Right: l.Space(4),
		}).Absolute().Inset(0).AutoFocus()
		ring = l.InputRing(c, anim, box.State().Focused)
		box.OnKey(s.paletteKey)
		l.Icon(c.Key("icon"), icons.Search, l.Rem(1), t.MutedForeground).
			Absolute().Left(l.Space(4)).Top((l.Rem(3) - l.Rem(1)) / 2)
	})
}

// paletteKey 是搜索框的键盘（渲染层 onKeyDown）：↑↓ 移动选中项、Enter 执行、Esc 关闭；其余（打字）留给输入框。
func (s *Screen) paletteKey(e gfx.KeyEvent) gfx.EventResult {
	if e.Release {
		return gfx.EventResult{}
	}
	switch e.Key {
	case gfx.KeyDown:
		s.Palette.Move(1)
		s.palette.reveal = true
	case gfx.KeyUp:
		s.Palette.Move(-1)
		s.palette.reveal = true
	case gfx.KeyEnter:
		if e.Repeat {
			return gfx.EventResult{Handled: true}
		}
		s.Palette.RunActive()
	case gfx.KeyEscape:
		s.Palette.Close()
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

// paletteList 是结果列表：max-h-[60vh] overflow-y-auto p-2。没有结果是居中的一句话（搜索中则说「正在搜索」，不是「没有匹配结果」）。
func (s *Screen) paletteList(c ui.C, v PaletteView) {
	l := s.Look
	t := l.T
	p := &s.palette
	pad := l.Space(2)
	maxHeight := c.Bounds().Height * paletteListMaxFraction
	c = c.Key("list")
	l.ScrollAreaView(c, [4]float32{pad, pad, pad, pad}, func(view kit.ScrollView) {
		p.state = view.State
		if len(v.Items) == 0 {
			text := locales.CommandPaletteNoResults()
			if v.Searching {
				text = locales.NativePaletteSearching()
			}
			ui.Column(c.Key("empty")).FillWidth().PadSides(l.Space(8), l.Space(3), l.Space(8), l.Space(3)).Children(func() {
				l.Text(c.Key("text"), text, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered())
			})
			return
		}
		ui.Column(c.Key("items")).FillWidth().Children(func() {
			for i, it := range v.Items {
				if i == 0 || v.Items[i-1].Group != it.Group {
					s.paletteGroup(c.Key("group-"+it.Key), it.Group)
				}
				s.paletteRow(c.Key("row-"+it.Key), v, i, it)
			}
		})
	}).FillWidth().MinHeight(0).Shrink(1).MaxHeight(maxHeight).Draw(func(d *ui.DrawContext) {
		p.viewport = d.Rect
	})
}

// paletteGroup 是分组标题：px-3 pb-1 pt-2 text-[11px] font-medium text-muted-foreground。
func (s *Screen) paletteGroup(c ui.C, title string) {
	l := s.Look
	ui.Column(c).FillWidth().PadSides(l.Space(2), l.Space(3), l.Space(1), l.Space(3)).Children(func() {
		l.TextPx(c.Key("text"), title, 11, 16.5, kit.WeightMedium, l.T.MutedForeground)
	})
}

// paletteIcon 是结果项的图标：设置 → 齿轮，知识条目与 Wiki → 文档，备份 → 数据库备份，其余动作 → 星光，采集任务 → 命令。
func paletteIcon(it PaletteItem) gfx.Icon {
	switch {
	case it.Kind == PaletteSetting:
		return icons.Settings
	case it.Kind == PaletteKnowledge || it.Kind == PaletteWiki:
		return icons.FileText
	case it.Key == PaletteKeyBackup:
		return icons.DatabaseBackup
	case it.Kind == PaletteAction:
		return icons.Sparkles
	}
	return icons.Command
}

// paletteRow 是一行结果：flex w-full items-center gap-3 rounded-lg px-3 py-2，选中项 bg-accent；
// 图标 16px muted，标题 text-sm 单行截断，副标题 text-xs muted 单行截断。点击执行，指针进入时设为选中项。
func (s *Screen) paletteRow(c ui.C, v PaletteView, index int, it PaletteItem) {
	l := s.Look
	t := l.T
	p := &s.palette
	active := index == v.Active
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).
		HitWith(id, strings.TrimSpace(it.Title+" "+it.Subtitle), gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	switch {
	case hovered && p.hover != index:
		p.hover = index
		if !active {
			s.Palette.SetActive(index)
		}
	case !hovered && p.hover == index:
		p.hover = -1
	}
	radius := l.Rem(0.5) // rounded-lg
	box = box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.T.Primary.WithAlpha(0.5), radius)
		}
		if active {
			l.PaintSurface(d, kit.Surface{Bg: t.Accent, Radius: radius})
		}
	}).DrawOver(func(d *ui.DrawContext) {
		if active {
			s.paletteReveal(d, index)
		}
	})
	box.Children(func() {
		l.Icon(c.Key("icon"), paletteIcon(it), l.Rem(1), t.MutedForeground)
		ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			l.Text(c.Key("title"), it.Title, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Ellipsis())
			if it.Subtitle != "" {
				l.Text(c.Key("subtitle"), it.Subtitle, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis())
			}
		})
	})
	if box.Clicked() {
		s.Palette.Run(index)
	}
}

// paletteReveal 在键盘刚移动了选中项时，把选中行滚进结果列表的视口；第一项回到顶部（让分组标题一并露出）。
// 已经完全可见就结束这次跟随；调整了滚动量则请求下一帧，等布局收敛后再判断一次。
func (s *Screen) paletteReveal(d *ui.DrawContext, index int) {
	p := &s.palette
	if !p.reveal || p.state == nil || p.viewport.Height <= 0 {
		return
	}
	top, bottom := p.viewport.Y+paletteScrollMargin, p.viewport.Y+p.viewport.Height-paletteScrollMargin
	row := d.Rect
	switch {
	case index == 0 && p.state.Offset > 0:
		p.state.ScrollTo(0)
	case row.Y < top:
		p.state.ScrollTo(p.state.Offset - (top - row.Y))
	case row.Y+row.Height > bottom:
		p.state.ScrollTo(p.state.Offset + (row.Y + row.Height - bottom))
	default:
		p.reveal = false
		return
	}
	d.Canvas.RequestFrame()
}

// paletteFooter 是底部状态条：「正在搜索」或部分来源搜索失败的说明；没有可说的就不画（空的状态条会让面板比渲染层多出一截）。
// 渲染层没有这一条，也没有按键提示行；失败来源的说明是 Viva 版的补充（渲染层把任何一个来源失败都吞成「没有结果」，用户无从得知）。
func (s *Screen) paletteFooter(c ui.C, v PaletteView) {
	if len(v.FailedSources) == 0 && !v.Searching {
		return
	}
	l := s.Look
	t := l.T
	c = c.Key("footer")
	ui.Row(c).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(2)).PadSides(l.Space(2)+1, l.Space(4), l.Space(2), l.Space(4)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() {
			if len(v.FailedSources) > 0 {
				l.Text(c.Key("failed"), locales.NativePaletteFailedSources(s.sourceNames(v.FailedSources)), kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
				return
			}
			l.Spinner(c.Key("spinner"), l.Rem(0.75), t.MutedForeground)
			l.Text(c.Key("searching"), locales.NativePaletteSearching(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
}

// sourceNames 把失败来源的标识换成界面名称（知识条目、Wiki、采集任务），用逗号分隔。
func (s *Screen) sourceNames(sources []string) string {
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		switch src {
		case SourceKnowledge:
			names = append(names, locales.CommandPaletteGroupKnowledge())
		case SourceWiki:
			names = append(names, locales.NavWiki())
		case SourceTasks:
			names = append(names, locales.ImportsTitle())
		default:
			names = append(names, src)
		}
	}
	return strings.Join(names, ", ")
}
