package library

import (
	"errors"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// chipTextPx 是表格里标签小块、状态小块与「+N」的字号：渲染层是 text-[10px]，px 字面量不随字号档（根字号 14 / 16 / 18px）缩放，
// 所以不能写成 Rem(0.625)——中档（16px）里两者相等，只有小档与大档才看得出来（M2-2 的 T2 抓到）。
const chipTextPx = 10

// 表格行（渲染层 ItemTableRow）：tr group cursor-pointer border-b border-border/50 transition-colors last:border-b-0
// hover:bg-accent/50，选中 bg-primary/10、勾选 bg-primary/5（悬停盖过它们）。
//
// 行高由操作列撑起：py-2 加 28px 的按钮是 44px，再加上下边框（border-collapse 下各占一半）共 45px；
// 标题折成两行时是 61px。行与行之间的线以网格线为中心，画在下一行的顶上（盖住两行的底色）；最后一行没有下边框。

// tableRow 声明一行（index 是它在本页的序号），返回它的最外层盒子：窗口化的行列（kit.RowWindow）在绘制时读它布局后的高度。
func (s *Screen) tableRow(c ui.C, v View, cols []tableColumn, geo stickyGeometry, e services.LibraryEntry, index int, last bool) ui.Box {
	l := s.Look
	t := l.T
	list := v.List
	canvas := c.Canvas()
	rc := c.Key("row-" + e.ID)
	id := ui.ID(rc)
	active := list.SelectedID == e.ID
	checked := list.IsSelected(e.ID)
	s.cardGroups.Add(e.ID, id)
	hovered := s.cardGroups.Hovered(canvas) == e.ID || s.table.checkHover == e.ID

	// 上下边框在 border-collapse 下各占一半（0.5px）；最后一行 last:border-b-0 没有下边框，所以比别的行矮 0.5px
	// （45 → 44.5，两行的 61 → 60.5），面板的下沿因此落在整像素上。
	bottomHalf, minHeight := float32(0.5), s.rowMinHeight()
	if last {
		bottomHalf, minHeight = 0, minHeight-0.5
	}
	row := ui.Row(rc).FillWidth().Shrink(0).Items(ui.Stretch).PadSides(0.5, 0, bottomHalf, 0).MinHeight(minHeight).
		HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Cursor: gfx.PointerCursorHand})
	// 底色：悬停盖过选中与勾选（hover: 变体排在后面）；各状态之间走 transition-colors。
	ph := l.HoverProgress(rc, id, 0, hovered, kit.DurationDefault)
	pa := l.HoverProgress(rc, id, 1, active, kit.DurationDefault)
	pc := l.HoverProgress(rc, id, 2, checked && !active, kit.DurationDefault)
	base := kit.MixToken(kit.MixToken(t.Primary.WithAlpha(0), t.Primary.WithAlpha(0.05), pc), t.Primary.WithAlpha(0.1), pa)
	bg := kit.MixToken(base, t.Accent.WithAlpha(0.5), ph)
	// overlay 是操作列补回的底色：吸附列自带 bg-card，行级的选中与悬停底色在它上面再叠一层（悬停只在没有选中、勾选时才变）。
	overlay := kit.MixToken(kit.MixToken(t.Accent.WithAlpha(0), t.Accent.WithAlpha(0.5), ph), t.Primary.WithAlpha(0.05), pc)
	overlay = kit.MixToken(overlay, t.Primary.WithAlpha(0.1), pa)

	radius := l.Rem(0.75) - 1 // 面板的内圆角：最后一行的底边贴着它
	rowTop := row
	rowTop.Draw(func(d *ui.DrawContext) {
		s.noteRowsOrigin(d, index)
		corner := float32(0)
		if last {
			corner = radius
		}
		rect := d.Rect
		rect.Width -= geo.overflow
		kit.PaintRoundedCorners(d, rect, bg, 0, 0, corner, corner)
	}).DrawOver(func(d *ui.DrawContext) {
		// border-b border-border/50：线以网格线为中心；第一行的上线是表头的下边框。
		if first := s.table.firstRow; first != e.ID {
			s.fillLine(d, gfx.Rect{X: d.Rect.X, Y: d.Rect.Y - 0.5, Width: d.Rect.Width - geo.overflow, Height: 1}, t.Border.WithAlpha(0.5))
		}
	}).Children(func() {
		for _, col := range cols {
			s.tableCell(c, v, geo, col, e, rc, active, checked, overlay, last)
		}
	})
	if row.Clicked() {
		s.rowClicked(list, e, row.ClickModifiers())
	}
	return row
}

// rowClicked 处理行上的一次点击（渲染层 ItemTableView 的 handleRowClick）：Ctrl/Meta 切换勾选，Shift 选一段，
// 否则打开详情浮层。详情浮层随 M2-4 迁入（台账 B-043），在那之前只把这一行设为选中。
func (s *Screen) rowClicked(list ListView, e services.LibraryEntry, mods gfx.Modifiers) {
	switch {
	case mods&(gfx.ModifierControl|gfx.ModifierMeta) != 0:
		s.Store.ToggleSelection(e.ID)
	case mods&gfx.ModifierShift != 0:
		s.Store.RangeSelectTo(e.ID)
	default:
		s.Store.SelectItem(e.ID)
	}
}

// cell 是一格的外框：px-4 py-2.5 align-middle，宽度取列宽，表格更宽时按列宽比例分摊多出的部分；内容垂直居中。
func (s *Screen) cell(c ui.C, col tableColumn) ui.Box {
	l := s.Look
	return ui.Row(c.Key("cell-"+string(col.ID))).Width(col.Width).Grow(col.Width).Shrink(0).Items(ui.Center).
		PadSides(l.Space(cellPadY), l.Space(cellPadX), l.Space(cellPadY), l.Space(cellPadX))
}

// tableCell 按列声明一格。
func (s *Screen) tableCell(c ui.C, v View, geo stickyGeometry, col tableColumn, e services.LibraryEntry, rc ui.C,
	active, checked bool, overlay tokens.Color, last bool) {
	l := s.Look
	t := l.T
	list := v.List
	title := e.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	cc := rc.Key("c-" + string(col.ID))
	switch col.ID {
	case colCheckbox:
		on := checked
		s.cell(cc, col).Children(func() {
			box := l.Checkbox(cc.Key("check"), &on, kit.CheckboxSpec{Name: locales.LibrarySelectRow(title)})
			// inline-flex 的行内盒子落在基线上，比居中高 1px（行内盒基线对齐的结果）。
			box.MarginBottom(2)
			if box.State().Hovered {
				s.table.checkHover = e.ID
			} else if s.table.checkHover == e.ID {
				s.table.checkHover = ""
			}
		})
		if on != checked {
			s.Store.ToggleSelection(e.ID)
		}
	case colTitle:
		s.cell(cc, col).Children(func() { s.titleCell(cc, e, title, list.searchTerm()) })
	case colSnippet:
		s.cell(cc, col).Children(func() {
			if e.Snippet == "" {
				s.dash(cc, kit.TextXS, t.MutedForeground.WithAlpha(0.4))
				return
			}
			// block truncate text-xs：搜索时命中词包进 mark（台账 B-041），单行放不下以省略号收尾。
			text, ok := ui.Box{}, false
			if query := list.searchTerm(); query != "" {
				text, ok = l.TextMarked(cc.Key("snippet"), e.Snippet, query, l.Rem(kit.TextXS.Size), l.Rem(kit.TextXS.Line), kit.WeightNormal,
					t.MutedForeground, kit.MarkOptions{Truncate: true})
			}
			if !ok {
				text = l.Text(cc.Key("snippet"), e.Snippet, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis())
			}
			text.Grow(1).MinWidth(0)
		})
	case colTags:
		s.cell(cc, col).Children(func() { s.tagsCell(cc, e) })
	case colType:
		s.cell(cc, col).Justify(ui.Center).Children(func() {
			l.Text(cc.Key("type"), kit.GetItemTypeMeta(e.ItemType).Label, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
	case colSource:
		s.cell(cc, col).Justify(ui.Center).Children(func() { s.sourceCell(cc, e) })
	case colCollection:
		s.cell(cc, col).Children(func() {
			name := locales.LibraryNoCollection()
			for _, col := range v.Collections {
				if col.ID == e.CollectionID {
					name = col.Name
					break
				}
			}
			l.Text(cc.Key("collection"), name, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Grow(1).MinWidth(0)
		})
	case colStatus:
		s.cell(cc, col).Justify(ui.Center).Children(func() { s.statusChip(cc, e.Status) })
	case colCreatedAt:
		s.cell(cc, col).Children(func() { s.dateText(cc, e, e.CreatedAt) })
	case colUpdatedAt:
		s.cell(cc, col).Children(func() { s.dateText(cc, e, e.UpdatedAt) })
	case colActions:
		s.actionsCell(cc, v, geo, col, e, overlay, list.SelectedID == e.ID, checked, last)
	}
}

// dash 是空值占位的短横线。
func (s *Screen) dash(c ui.C, st kit.TextStyle, color tokens.Color) {
	s.Look.Text(c.Key("dash"), "-", st, kit.WeightNormal, color)
}

// titleCell 是标题格：flex min-w-0 items-center gap-1.5，依次是类型图标、置顶图标、标题按钮、星标。
func (s *Screen) titleCell(c ui.C, e services.LibraryEntry, title, query string) {
	l := s.Look
	t := l.T
	typeMeta := kit.GetItemTypeMeta(e.ItemType)
	ui.Row(c.Key("title-flex")).Grow(1).MinWidth(0).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		// 类型图标外面的 span 带 title，悬停出类型名；它不可聚焦、不进 Tab 序列，悬停时整行仍算悬停。
		tc := c.Key("type")
		typeID := ui.ID(tc)
		s.cardGroups.Add(e.ID, typeID)
		ui.Row(tc).Shrink(0).Items(ui.Center).
			HitWith(typeID, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: typeMeta.Label}).Children(func() {
			l.Icon(tc.Key("glyph"), typeMeta.Icon, l.Rem(0.875), t.MutedForeground.WithAlpha(0.7))
		})
		if e.IsPinned {
			l.Icon(c.Key("pin"), icons.Pin, l.Rem(0.875), t.Primary)
		}
		s.titleButton(c, e, title, query)
		if e.IsFavorite {
			l.IconFilled(c.Key("star"), icons.Star, l.Rem(0.875), tokens.Tailwind("amber-400"))
		}
	})
}

// titleButton 是标题按钮：min-w-0 line-clamp-2 break-words text-left text-sm font-medium text-primary transition-colors
// hover:text-primary/80 hover:underline；点击打开详情浮层（随 M2-4 迁入，台账 B-043），在那之前选中这一行。
func (s *Screen) titleButton(c ui.C, e services.LibraryEntry, title, query string) {
	l := s.Look
	t := l.T
	bc := c.Key("open")
	id := ui.ID(bc)
	s.cardGroups.Add(e.ID, id)
	box := ui.Column(bc).Shrink(1).MinWidth(0).
		HitWith(id, title, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	hovered := box.State().Hovered
	p := l.HoverProgress(bc, id, 0, hovered, kit.DurationDefault)
	color := kit.MixToken(t.Primary, t.Primary.WithAlpha(0.8), p)
	box.Children(func() {
		// 500 档字重：Chromium 不做字偶距、Viva 做，钮宽（= 文字宽）与折行点按渲染层的宽度自己排（台账 A-026 / B-044）；
		// line-clamp-2 至多两行，悬停的下划线每一行各画一条。
		size, line := l.Rem(kit.TextSM.Size), l.Rem(kit.TextSM.Line)
		// 搜索时命中词包进 mark（台账 B-041；表格里的标题连「无标题」也标），悬停下划线每个文字片段各一条。
		if query != "" {
			if _, ok := l.TextMarked(bc.Key("text"), title, query, size, line, kit.WeightMedium, color,
				kit.MarkOptions{MaxLines: 2, Underline: hovered}); ok {
				return
			}
		}
		l.TextNoKern(bc.Key("text"), title, size, line, kit.WeightMedium, color, kit.NoKernOptions{MaxLines: 2, Underline: hovered})
	})
	if box.Clicked() {
		s.Store.SelectItem(e.ID)
	}
}

// tagsCell 是标签格：flex items-center gap-1 overflow-hidden，最多两个小块，多出的写 +N。
func (s *Screen) tagsCell(c ui.C, e services.LibraryEntry) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("tags")).Grow(1).MinWidth(0).Items(ui.Center).Gap(l.Space(1)).Clip().Children(func() {
		if len(e.Tags) == 0 {
			s.dash(c, kit.TextXS, t.MutedForeground.WithAlpha(0.4))
			return
		}
		for i, tag := range e.Tags {
			if i >= 2 {
				break
			}
			s.tagChip(c.Key("tag-"+tag.ID), tag)
		}
		if extra := len(e.Tags) - 2; extra > 0 {
			// shrink-0 text-[10px] text-muted-foreground/60：没有显式行高，继承 table 的 text-sm（1.25rem 行高）。
			l.TextPx(c.Key("more"), "+"+strconv.Itoa(extra), chipTextPx, l.Rem(1.25), kit.WeightNormal, t.MutedForeground.WithAlpha(0.6))
		}
	})
}

// tagChip 是一个标签小块：max-w-[5rem] truncate rounded px-1.5 py-0.5 text-[10px] leading-none。
func (s *Screen) tagChip(c ui.C, tag services.LibraryTag) {
	l := s.Look
	bg, fg := s.tagChipColors(tag.ColorKey)
	size := float32(chipTextPx)
	ui.Row(c).Shrink(1).MinWidth(0).MaxWidth(l.Rem(5)).Items(ui.Center).
		PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.25)})
		}).Children(func() {
		l.TextPx(c.Key("name"), tag.Name, size, size, kit.WeightNormal, fg, kit.Ellipsis()).Shrink(1).MinWidth(0)
	})
}

// tagChipColors 是标签小块的底色与文字色（TAG_COLOR_CLASSES）：彩色是 bg-*-500/15 加 text-*-700（深色 text-*-300），
// 灰色是 bg-muted text-muted-foreground。键不在十个之内时渲染层取到 undefined：没有底色，文字继承前景色。
func (s *Screen) tagChipColors(key string) (bg, fg tokens.Color) {
	l := s.Look
	t := l.T
	for _, tc := range tagColors() {
		if tc.Key != key {
			continue
		}
		if key == "gray" {
			return t.Muted, t.MutedForeground
		}
		shade := "700"
		if l.Dark {
			shade = "300"
		}
		return tokens.Tailwind(key + "-500").WithAlpha(0.15), tokens.Tailwind(key + "-" + shade)
	}
	return tokens.Color{}, t.Foreground
}

// sourceCell 是来源格：有平台时 flex min-w-0 items-center justify-center gap-1.5（14px 平台图标加截断的名称），否则一个淡色短横线。
func (s *Screen) sourceCell(c ui.C, e services.LibraryEntry) {
	l := s.Look
	t := l.T
	if e.Platform == "" {
		s.dash(c, kit.TextXS, t.MutedForeground.WithAlpha(0.5))
		return
	}
	ui.Row(c.Key("source")).MinWidth(0).Items(ui.Center).Justify(ui.Center).Gap(l.Space(1.5)).Children(func() {
		l.PlatformIcon(c.Key("platform"), e.Platform, l.Rem(0.875), t.MutedForeground).Shrink(0)
		l.Text(c.Key("label"), kit.PlatformLabel(e.Platform), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
	})
}

// statusChip 是状态小块：inline-flex items-center rounded border border-border/70 bg-muted/40 px-1.5 py-0.5 text-[10px] leading-none。
func (s *Screen) statusChip(c ui.C, status string) {
	l := s.Look
	t := l.T
	size := float32(chipTextPx)
	label := locales.LibraryStatusUnknown()
	switch status {
	case "active":
		label = locales.LibraryStatusActive()
	case "archived":
		label = locales.LibraryStatusArchived()
	}
	// 渲染层的标签是 td（继承 table 的 text-sm）里的 inline-flex：与单元格的文字支柱按基线对齐，比居中低一截（kit.InlineBoxDrop），
	// 居中的盒按外边距盒居中，所以上外边距取两倍。
	ui.Row(c.Key("status")).Shrink(0).Items(ui.Center).MarginTop(2*s.statusChipDrop(size)).
		PadSides(l.Space(0.5)+1, l.Space(1.5)+1, l.Space(0.5)+1, l.Space(1.5)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.4), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.25)})
		}).Children(func() {
		l.TextPx(c.Key("label"), label, size, size, kit.WeightNormal, t.MutedForeground)
	})
}

// statusChipDrop 是状态标签比单元格居中低多少（逻辑像素，kit.InlineBoxDrop）：支柱是表格的 text-sm（0.875rem / 1.25rem），
// 标签 leading-none（行高 = 字号），上内边距 = py-0.5 + 1px 边框，盒高 = 行高 + 两倍上内边距，基线离顶 = 上内边距 + 文字的基线行。
// 小 / 中 / 大字号档是 1.125 / 2 / 2.75 px（渲染层实测，kit.TestInlineBoxDrop）。
func (s *Screen) statusChipDrop(size float32) float32 {
	l := s.Look
	scale := l.Scale
	if scale <= 0 {
		scale = 1
	}
	pad := l.Space(0.5) + 1
	base := float32(kit.ChromeBaselineRow(kit.WeightNormal, float64(size*scale), float64(size*scale))) / scale
	return kit.InlineBoxDrop(kit.WeightNormal, l.Rem(0.875), l.Rem(1.25), size+2*pad, pad+base, scale)
}

// dateText 是时间格：text-xs text-muted-foreground，里面一个带 title 的 span，悬停出完整的日期时间。
func (s *Screen) dateText(c ui.C, e services.LibraryEntry, ms int64) {
	l := s.Look
	t := l.T
	dc := c.Key("date")
	id := ui.ID(dc)
	s.cardGroups.Add(e.ID, id)
	full := kit.FormatDateTime(ms, s.loc(), s.Locale)
	ui.Row(dc).Items(ui.Center).HitWith(id, "", gfx.HitOptions{SkipFocus: true, SkipTab: true, Hint: full}).Children(func() {
		l.Text(dc.Key("text"), kit.FormatItemTime(ms, s.nowTime(), s.loc(), s.Locale), kit.TextXS, kit.WeightNormal, t.MutedForeground)
	})
}

// errRowActionPending 是行内操作（收藏、恢复、彻底删除、更多）在条目操作迁入之前的失败原因：用户点了就要有交代，不能一声不响。
// 卡片视图里也还没有条目操作（右键菜单、批量栏都随 M2-3 迁入，台账 B-043），所以提示里不能指给用户一个「请先去那边操作」的去处。
var errRowActionPending = errors.New("条目操作尚未迁入原生界面，将随条目操作一起迁入")

// actionsCell 是操作列：sticky right-0 z-20 bg-card p-0；里面先铺一层补回行底色的覆盖，再放按钮。
// 类名里的左侧阴影（shadow-[-4px_0_8px_-4px_…]）不画：折叠边框模型（border-collapse: collapse）下 Chromium 不给单元格画 box-shadow，
// 渲染层像素实测吸附列左侧没有任何阴影（见 headActions）。
// 回收站里是「恢复」「彻底删除」，其余范围是「收藏」「更多操作」。占位留在行里，真正的单元格绝对定位。
func (s *Screen) actionsCell(c ui.C, v View, geo stickyGeometry, col tableColumn, e services.LibraryEntry,
	overlay tokens.Color, active, checked, last bool) {
	l := s.Look
	t := l.T
	s.actionsSpacer(c, col)
	width := s.actionsWidth([]tableColumn{col})
	inner := l.Rem(0.75) - 1
	br := float32(0)
	if last && geo.right <= geo.overflow {
		br = inner
	}
	// 按钮在单元格内容区里垂直居中：border-collapse 下内容区上沿让出 0.5px 的半条上边框、下沿让出半条下边框（最后一行没有下边框），
	// 与行里别的单元格同一口径（tableRow 的 PadSides）。绝对定位的这一格自己补上，否则最后一行的按钮比渲染层高 0.25px，
	// 半像素处落笔取整差一行（M2-2 的 T2：table-page2 小字号档，台账 B-046）。底色仍铺满整行。
	bottomHalf := float32(0.5)
	if last {
		bottomHalf = 0
	}
	ui.Row(c.Key("actions")).Absolute().Top(0).Bottom(0).Right(geo.right).Width(width).Items(ui.Center).Justify(ui.Center).
		PadSides(0.5, 0, bottomHalf, 0).Gap(l.Space(0.5)).
		Draw(func(d *ui.DrawContext) {
			rect := stickyRect(d.Rect, geo)
			kit.PaintRoundedCorners(d, rect, t.Card, 0, 0, br, 0)
			kit.PaintRoundedCorners(d, rect, overlay, 0, 0, br, 0)
		}).Children(func() {
		if v.Filters.Scope == services.LibraryScopeTrash {
			s.rowAction(c, e, rowActionSpec{key: "restore", label: locales.LibraryRestore(), icon: icons.RotateCcw})
			s.rowAction(c, e, rowActionSpec{key: "delete", label: locales.LibraryDeleteForever(), icon: icons.CircleX, destructive: true})
			return
		}
		favorite := locales.LibraryFavorite()
		if e.IsFavorite {
			favorite = locales.LibraryUnfavorite()
		}
		s.rowAction(c, e, rowActionSpec{key: "favorite", label: favorite, icon: icons.Star, active: e.IsFavorite, star: e.IsFavorite, toggle: true})
		s.moreAction(c, e)
	})
}

// rowActionSpec 描述一枚行内操作钮（渲染层 RowAction）。
type rowActionSpec struct {
	key, label  string
	icon        gfx.Icon
	destructive bool
	active      bool // aria-pressed 为真：bg-primary/15 text-primary
	toggle      bool // 带 aria-pressed（收藏钮：真或假都有这个属性）
	star        bool // 图标画成实心的琥珀色星星（fill-amber-400 text-amber-400）
}

// rowAction 是一枚 28px 的图标钮：inline-flex h-7 w-7 rounded-lg transition-colors，
// 危险色悬停 bg-destructive/10 text-destructive，选中 bg-primary/15 text-primary，其余悬停 bg-accent text-foreground。
func (s *Screen) rowAction(c ui.C, e services.LibraryEntry, spec rowActionSpec) {
	l := s.Look
	t := l.T
	ac := c.Key(spec.key)
	id := ui.ID(ac)
	s.cardGroups.Add(e.ID, id)
	radius := l.Rem(0.75)
	box := ui.Row(ac).Size(l.Rem(1.75), l.Rem(1.75)).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, spec.label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Hint: spec.label, Selected: spec.toggle && spec.active})
	l.TabStop(box)
	p := l.HoverProgress(ac, id, 0, box.State().Hovered, kit.DurationDefault)
	var fg, bg tokens.Color
	switch {
	case spec.destructive:
		fg = kit.MixToken(t.MutedForeground, t.Destructive, p)
		bg = kit.MixToken(t.Destructive.WithAlpha(0), t.Destructive.WithAlpha(0.1), p)
	case spec.active:
		fg, bg = t.Primary, t.Primary.WithAlpha(0.15)
	default:
		fg = kit.MixToken(t.MutedForeground, t.Foreground, p)
		bg = kit.MixToken(t.Accent.WithAlpha(0), t.Accent, p)
	}
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		l.PaintSurface(d, kit.Surface{Bg: bg, Radius: radius})
	}).Children(func() {
		if spec.star {
			l.IconFilled(ac.Key("icon"), spec.icon, l.Rem(1), tokens.Tailwind("amber-400"))
			return
		}
		l.Icon(ac.Key("icon"), spec.icon, l.Rem(1), fg)
	})
	if box.Clicked() {
		s.Guard.Report(spec.label, errRowActionPending)
	}
}

// moreAction 是「更多操作」钮：inline-flex h-7 w-7 rounded-lg hover:bg-accent（没有过渡、文字色是前景色）。
func (s *Screen) moreAction(c ui.C, e services.LibraryEntry) {
	l := s.Look
	t := l.T
	mc := c.Key("more")
	id := ui.ID(mc)
	s.cardGroups.Add(e.ID, id)
	radius := l.Rem(0.75)
	label := locales.LibraryMoreActions()
	box := ui.Row(mc).Size(l.Rem(1.75), l.Rem(1.75)).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	hovered := box.State().Hovered
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		if hovered {
			l.PaintSurface(d, kit.Surface{Bg: t.Accent, Radius: radius})
		}
	}).Children(func() {
		l.Icon(mc.Key("icon"), icons.Ellipsis, l.Rem(1), t.Foreground)
	})
	if box.Clicked() {
		s.Guard.Report(label, errRowActionPending)
	}
}
