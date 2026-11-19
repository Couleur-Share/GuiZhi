package library

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 卡片列表的几何（渲染层 ItemList 的 ItemRow 与它外层的绝对定位行）。
const (
	// cardSeed 是没有实测时一行（含上方 6px 间距）的估计高度（渲染层 ROW_HEIGHT）。
	cardSeed float32 = 64
)

// entryRow 声明列表里的一行：absolute left-0 top-0 w-full px-2 pt-1.5，里面是一张卡片。
// 行高是上方 6px 加卡片自己的高度（标题与搜索时的摘要都整段折行，行数不限）。
//
// 行高要取整：渲染层的虚拟列表量行用 Math.round（@tanstack/react-virtual 的 measureElement），行按取整后的高度逐行累加定位，
// 卡片自己仍是原来的小数高度。单行卡片 6 + 39.25 = 45.25 取 45，相邻卡片之间因此是 5.75px 而不是 6px，
// 往下每张卡片都比不取整时高 0.25px 的累计——不照做，几行之后卡片的位置就差出一个像素。
// 取整用的是上一帧量到的卡片高度（首帧没有记录时按自然高度，随后一帧收敛）。
func (s *Screen) entryRow(c ui.C, v ListView, e services.LibraryEntry, query string) ui.Box {
	l := s.Look
	pad := l.Space(1.5)
	row := ui.Column(c.Key("row-"+e.ID)).FillWidth().PadSides(pad, l.Space(2), 0, l.Space(2))
	if h := s.pane.cardHeight[e.ID]; h > 0 {
		row = row.Height(roundHalfUp(pad + h))
	}
	return row.Children(func() {
		s.entryCard(c, v, e, query)
	})
}

// noteCardHeight 记下卡片布局后的自然高度（供下一帧取整行高）；变了就再要一帧。
func (s *Screen) noteCardHeight(d *ui.DrawContext, id string) {
	p := &s.pane
	if p.cardHeight == nil {
		p.cardHeight = map[string]float32{}
	}
	if h := d.Rect.Height; math.Abs(float64(h-p.cardHeight[id])) > 0.01 {
		p.cardHeight[id] = h
		d.Canvas.RequestFrame()
	}
}

// entryCard 是一张条目卡片（渲染层 ItemRow）：
// flex w-full items-start gap-2 rounded-xl px-3 py-2.5 text-left transition-colors。
//
//	选中  bg-primary text-primary-foreground shadow-sm（实心主色，内部图标改走反色梯度）
//	勾选  bg-primary/20 text-foreground
//	其余  bg-muted/60 text-foreground，悬停 bg-muted
//
// 底色与文字色走 transition-colors（150 ms）；阴影不在过渡清单里，选中就立刻有。
// 点击：Ctrl/Meta 切换勾选，Shift 选一段，否则清掉勾选并打开这一条。
func (s *Screen) entryCard(c ui.C, v ListView, e services.LibraryEntry, query string) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	c = c.Key("card")
	id := ui.ID(c)
	selected := v.SelectedID == e.ID
	checked := v.IsSelected(e.ID)
	typeMeta := kit.GetItemTypeMeta(e.ItemType)
	title := e.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	radius := l.Rem(0.75) // rounded-xl：本应用的 Tailwind 只改了 lg/md/sm，xl 保持默认的 0.75rem

	typeID := ui.ID(c.Key("type"))
	s.cardGroups.Add(e.ID, id)
	s.cardGroups.Add(e.ID, typeID)
	hovered := s.cardGroups.Hovered(canvas) == e.ID

	box := ui.Row(c).FillWidth().Shrink(0).Items(ui.Start).Gap(l.Space(2)).
		PadSides(l.Space(2.5), l.Space(3), l.Space(2.5), l.Space(3)).
		HitWith(id, cardName(title, e, query), gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)

	// 悬停、勾选、选中各一个进度量：状态切换时底色与文字色在 150 ms 里补间。
	ph := l.HoverProgress(c, id, 0, hovered && !selected && !checked, kit.DurationDefault)
	pc := l.HoverProgress(c, id, 1, checked && !selected, kit.DurationDefault)
	pa := l.HoverProgress(c, id, 2, selected, kit.DurationDefault)
	bg := kit.MixToken(kit.MixToken(kit.MixToken(t.Muted.WithAlpha(0.6), t.Muted, ph), t.Primary.WithAlpha(0.2), pc), t.Primary, pa)
	fg := kit.MixToken(t.Foreground, t.PrimaryForeground, pa)
	var shadows []tokens.Shadow
	if selected {
		shadows = kit.ShadowSM
	}

	box.Draw(func(d *ui.DrawContext) {
		s.noteCardHeight(d, e.ID)
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius) // 全局 button:focus-visible
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: radius, Shadows: shadows})
	}).Children(func() {
		if e.IsPinned {
			// mt-0.5 h-3.5 w-3.5 shrink-0；选中时不写颜色类，跟着按钮的文字色走（同样在补间中）。
			pin := t.Primary
			if selected {
				pin = fg
			}
			l.Icon(c.Key("pin"), icons.Pin, l.Rem(0.875), pin).MarginTop(l.Space(0.5))
		}
		// min-w-0 flex-1：标题与摘要。
		ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			// block text-sm font-medium leading-snug line-clamp-2：渲染层的 line-clamp-2 不生效（Tailwind 3.4 里 display 工具类排在
			// line-clamp 之后，`block` 把 `-webkit-box` 盖掉了），长标题整段折行、卡片随之变高，这里照渲染出来的样子不截断。
			l.TextPx(c.Key("title"), title, l.Rem(0.875), l.Rem(0.875)*1.375, kit.WeightMedium, fg, kit.Wrapped())
			if query != "" && e.Snippet != "" {
				snippet := t.MutedForeground
				if selected {
					snippet = t.PrimaryForeground.WithAlpha(0.8)
				}
				// mt-0.5 block text-xs leading-snug line-clamp-2（同样不生效）
				l.TextPx(c.Key("snippet"), e.Snippet, l.Rem(0.75), l.Rem(0.75)*1.375, kit.WeightNormal, snippet, kit.Wrapped()).
					MarginTop(l.Space(0.5))
			}
		})
		// mt-0.5 flex shrink-0 items-center gap-1：星标与类型图标。
		ui.Row(c.Key("badges")).Shrink(0).MarginTop(l.Space(0.5)).Items(ui.Center).Gap(l.Space(1)).Children(func() {
			if e.IsFavorite {
				// h-3.5 w-3.5 fill-amber-400 text-amber-400
				l.IconFilled(c.Key("star"), icons.Star, l.Rem(0.875), tokens.Tailwind("amber-400"))
			}
			tint := t.MutedForeground.WithAlpha(0.7)
			if selected {
				tint = t.PrimaryForeground.WithAlpha(0.8)
			}
			// 类型图标外面的 span 带 title，悬停出类型名；它不可聚焦、不进 Tab 序列。
			ui.Row(c.Key("type")).Items(ui.Center).
				HitWith(typeID, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: typeMeta.Label}).Children(func() {
				l.Icon(c.Key("glyph"), typeMeta.Icon, l.Rem(0.875), tint)
			})
		})
	})

	if box.Clicked() {
		s.cardClicked(v, e, box.ClickModifiers())
	}
}

// cardName 是卡片按钮的可访问名：取自内容——标题与搜索时的摘要。类型图标外层 span 的 title（类型名）不进名字：
// 浏览器的可访问树里它只是悬停提示（对照实测，卡片的名字就是标题）。
func cardName(title string, e services.LibraryEntry, query string) string {
	if query != "" && e.Snippet != "" {
		return title + " " + e.Snippet
	}
	return title
}

// cardClicked 处理一次点击（渲染层 ItemList 的 handleRowClick）。
func (s *Screen) cardClicked(v ListView, e services.LibraryEntry, mods gfx.Modifiers) {
	switch {
	case mods&(gfx.ModifierControl|gfx.ModifierMeta) != 0:
		s.Store.ToggleSelection(e.ID)
	case mods&gfx.ModifierShift != 0:
		s.Store.RangeSelectTo(e.ID)
	default:
		if len(v.Selection) > 0 {
			s.Store.ClearSelection()
		}
		s.Store.SelectItem(e.ID)
	}
}
