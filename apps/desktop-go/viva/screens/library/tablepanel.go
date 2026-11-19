package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 表格面板（渲染层 ItemTableView 里的 app-wallpaper-panel 外框、thead 与 tbody）。
//
// 面板 w-full overflow-clip rounded-xl border，最小宽度是各列宽度之和；里面的 table 同样最小宽度是各列之和（table-fixed）。
// 面板内宽比表格窄 2px（边框），超出的部分被面板裁掉；窄了就由外层滚动区横向滚动。
// 列宽：各列取配置宽度，表格更宽时按宽度比例分摊多出的部分（table-layout: fixed 的行为）。

// 表头的单元格内边距（px-4 py-2.5）。
const (
	cellPadX = 4
	cellPadY = 2.5
)

// headHeight 是表头占的高度：py-2.5 的上下内边距加 text-xs 的行高，再加下边框（border-collapse 下各占一半）的 0.5。
func (s *Screen) headHeight() float32 {
	l := s.Look
	return 2*l.Space(cellPadY) + l.Rem(1) + 0.5
}

// rowMinHeight 是一行的最小高度：操作列的 py-2 加 28px 的按钮，再加上下边框各一半的 1px。
func (s *Screen) rowMinHeight() float32 {
	l := s.Look
	return 2*l.Space(2) + l.Rem(1.75) + 1
}

// tablePanel 声明面板、表头（吸附）与全部行。
func (s *Screen) tablePanel(c ui.C, v View, cols []tableColumn, view kit.Scroll2D) {
	l := s.Look
	t := l.T
	minW := columnsMinWidth(cols)
	geo := s.stickyAt(view, minW)
	radius := l.Rem(0.75)
	entries := v.List.Entries
	s.table.firstRow = ""
	if len(entries) > 0 {
		s.table.firstRow = entries[0].ID
	}

	// 面板宽度只取决于容器：表格（最小宽度 minW）比面板内宽 2px 时越出的部分被裁掉，不能反过来撑宽面板，
	// 否则滚动内容比渲染层宽 2px，滚到最右时多滚 2px。所以这里的最小宽度显式写 0，不用内容撑出的自动值。
	ui.Column(c.Key("panel")).FillWidth().Shrink(0).MinWidth(0).Pad(1).Clip().
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.UiPanelBg, Shadow: t.UiPanelShadow, Radius: radius})
		}).
		// 边框画在内容之上：表格比面板内宽 2px，越出的部分压在边框那一圈上，渲染层的 overflow-clip 在内边距盒处裁掉。
		DrawOver(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.UiPanelBorder, BorderW: 1, Radius: radius})
		}).
		Children(func() {
			// 右外边距取负的越出量：套件按「内容宽 + 外边距」算父级的宽度，表格的 minW 要扣掉越出面板内缘的那一段，
			// 面板（含两侧各 1px 边框）才刚好是 minW 宽，滚动内容比渲染层不多不少（minW + 两侧内边距）；
			// 表格自己的宽度仍是 max(面板内宽 + 越出量, minW)，容器很宽、没有越出时与渲染层的 w-full 一致。
			ui.Column(c.Key("table")).FillWidth().Shrink(0).MinWidth(minW).MarginRight(-geo.overflow).
				Draw(func(d *ui.DrawContext) { s.noteTable(d) }).
				Children(func() {
					// 表头与行都是在流元素，次序与渲染层的 DOM 一致（可访问树、文字序列与 Tab 都按绘制顺序排，不能把表头画到最后）。
					// 吸附：表头整体下移 geo.headerTop，贴在滚动区上缘；行区域从表头下沿起裁剪，里面的行上移同样的量，
					// 于是行仍落在随滚动应在的位置，被表头盖住的那一段看不见，表格总高不变。没有吸附时两处的量都是 0。
					s.tableHead(c, v, cols, geo)
					ui.Column(c.Key("rows")).FillWidth().Shrink(0).Clip().Children(func() {
						ui.Column(c.Key("rows-body")).FillWidth().Shrink(0).MarginTop(-geo.headerTop).Children(func() {
							for i, e := range entries {
								s.tableRow(c, v, cols, geo, e, i, i == len(entries)-1)
							}
						})
					})
				})
		})
}

// noteTable 记下表格上一帧布局后的高度与宽度：表头吸附的下限（不能越出表格）与吸附列的几何要用。变了就再要一帧。
func (s *Screen) noteTable(d *ui.DrawContext) {
	t := &s.table
	if abs32(d.Rect.Height-t.tableH) > 0.01 || abs32(d.Rect.Width-t.tableW) > 0.01 {
		t.tableH, t.tableW = d.Rect.Height, d.Rect.Width
		d.Canvas.RequestFrame()
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// actionsWidth 是操作列此刻的实际宽度：默认列宽（表格更宽时按比例放大，由上一帧的占位量到）。
func (s *Screen) actionsWidth(cols []tableColumn) float32 {
	if w := s.table.actionsW; w > 0 {
		return w
	}
	for _, col := range cols {
		if col.ID == colActions {
			return col.Width
		}
	}
	return 0
}

// tableHead 是表头：thead sticky -top-3 z-30 bg-card，里面一行 border-b border-border bg-muted/40。
// 吸附时整体下移 geo.headerTop，贴在滚动区上缘（行区域在它下面裁剪，见 tablePanel）。
func (s *Screen) tableHead(c ui.C, v View, cols []tableColumn, geo stickyGeometry) {
	l := s.Look
	t := l.T
	list := v.List
	allSelected := len(list.Entries) > 0
	for _, e := range list.Entries {
		if !list.IsSelected(e.ID) {
			allSelected = false
			break
		}
	}
	inner := l.Rem(0.75) - 1 // 面板的内圆角
	corner := float32(0)
	if geo.headerTop == 0 {
		corner = inner
	}
	ui.Row(c.Key("thead")).FillWidth().Shrink(0).MarginTop(geo.headerTop).Items(ui.Stretch).
		PadSides(0, 0, 0.5, 0).
		Draw(func(d *ui.DrawContext) {
			rect := d.Rect
			rect.Width -= geo.overflow // 越出面板内缘的部分被裁掉，圆角落在面板的内缘上
			rect.Height -= 0.5
			kit.PaintRoundedCorners(d, rect, t.Card, corner, corner, 0, 0)
			kit.PaintRoundedCorners(d, rect, t.Muted.WithAlpha(0.4), corner, corner, 0, 0)
		}).
		DrawOver(func(d *ui.DrawContext) {
			// border-b border-border：折叠边框的线以网格线为中心，一半在表头里、一半在第一行里。
			// 折叠边框属于表格而不属于行：表头吸附（下移）之后，这条线留在原位、被表头盖住或滚出视口，吸附的表头下沿没有线
			// （渲染层像素实测：表头吸附时下沿一行与第一行同色，未吸附时才有这条线）。
			if geo.headerTop > 0 {
				return
			}
			s.fillLine(d, gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + d.Rect.Height - 0.5, Width: d.Rect.Width - geo.overflow, Height: 1}, t.Border)
		}).
		Children(func() {
			for _, col := range cols {
				switch col.ID {
				case colCheckbox:
					s.headCheckbox(c, col, allSelected)
				case colActions:
					s.headActions(c, col, geo)
				default:
					s.headCell(c, col)
				}
			}
		})
}

// fillLine 填一条 1px 的线（吸附到设备像素）。
func (s *Screen) fillLine(d *ui.DrawContext, rect gfx.Rect, color tokens.Color) {
	d.Canvas.Fill(kit.SnapRect(rect, d.Canvas.PixelScale()), d.Fade(s.Look.Col(color)))
}

// headBox 是一格表头的外框：宽度取列宽，表格更宽时按列宽比例分摊多出的部分。
func (s *Screen) headBox(c ui.C, col tableColumn) ui.Box {
	return ui.Row(c.Key("th-" + string(col.ID))).Width(col.Width).Grow(col.Width).Shrink(0).Items(ui.Center)
}

// headCheckbox 是复选框列的表头：th px-4 py-2.5，里面是「选择本页全部」的复选框。
func (s *Screen) headCheckbox(c ui.C, col tableColumn, allSelected bool) {
	l := s.Look
	checked := allSelected
	s.headBox(c, col).PadSides(l.Space(cellPadY), l.Space(cellPadX), l.Space(cellPadY), l.Space(cellPadX)).Children(func() {
		box := l.Checkbox(c.Key("select-page"), &checked, kit.CheckboxSpec{Name: locales.LibrarySelectPageRows()})
		_ = box
		if checked != allSelected {
			s.Store.TogglePageSelection()
		}
	})
}

// headCell 是普通的表头格：th relative p-0 里一个 whitespace-nowrap px-4 py-2.5 text-xs font-medium text-muted-foreground，
// 居中列 text-center、其余 text-left；可调的列在右缘有一个 8px 宽的拖动把手。
func (s *Screen) headCell(c ui.C, col tableColumn) {
	l := s.Look
	box := s.headBox(c, col).PadSides(l.Space(cellPadY), l.Space(cellPadX), l.Space(cellPadY), l.Space(cellPadX))
	if col.Centered {
		box = box.Justify(ui.Center)
	}
	box.Children(func() {
		l.Text(c.Key("th-text-"+string(col.ID)), col.Label(), kit.TextXS, kit.WeightMedium, l.T.MutedForeground)
		if col.Resizable {
			s.columnHandle(c, col)
		}
	})
}

// headActions 是操作列的表头：吸附在右，th bg-card + 一层 bg-muted/40，文字居中。
// 占位留在行里，真正的单元格绝对定位，吸附时贴着视口右缘（距右 geo.right）。
// 类名里的左侧阴影不画：表格是 border-collapse: collapse，折叠边框模型下 Chromium 不给单元格画 box-shadow
// （渲染层像素实测：吸附列左侧一列像素与旁边的单元格完全同色，浅色主题 4 个变体都如此）。
func (s *Screen) headActions(c ui.C, col tableColumn, geo stickyGeometry) {
	l := s.Look
	t := l.T
	s.actionsSpacer(c, col)
	width := s.actionsWidth([]tableColumn{col})
	inner := l.Rem(0.75) - 1
	round := float32(0)
	if geo.topRight() {
		round = inner
	}
	ui.Row(c.Key("th-actions")).Absolute().Top(0).Bottom(0).Right(geo.right).Width(width).Items(ui.Center).Justify(ui.Center).
		PadSides(l.Space(cellPadY), l.Space(cellPadX), l.Space(cellPadY), l.Space(cellPadX)).
		Draw(func(d *ui.DrawContext) {
			rect := stickyRect(d.Rect, geo)
			kit.PaintRoundedCorners(d, rect, t.Card, 0, round, 0, 0)
			kit.PaintRoundedCorners(d, rect, t.Muted.WithAlpha(0.4), 0, round, 0, 0)
		}).
		Children(func() {
			l.Text(c.Key("th-text-actions"), col.Label(), kit.TextXS, kit.WeightMedium, t.MutedForeground)
		})
}

// actionsSpacer 是操作列在行里的占位：宽度同列宽，并把它的实际宽度记下来，吸附的单元格取同样的宽度。
func (s *Screen) actionsSpacer(c ui.C, col tableColumn) {
	ui.Slot(c.Key("spacer-actions")).Width(col.Width).Grow(col.Width).Shrink(0).
		Draw(func(d *ui.DrawContext) {
			if abs32(d.Rect.Width-s.table.actionsW) > 0.01 {
				s.table.actionsW = d.Rect.Width
				d.Canvas.RequestFrame()
			}
		})
}

// stickyRect 是吸附单元格实际可见的矩形：贴到面板内缘时，越出内缘的部分被裁掉。
func stickyRect(rect gfx.Rect, geo stickyGeometry) gfx.Rect {
	if clip := geo.overflow - geo.right; clip > 0 {
		rect.Width -= clip
	}
	return rect
}

// topRight 报告吸附列的右上角是否落在面板的圆角上：表头在原位，且吸附列贴着面板内缘。
func (g stickyGeometry) topRight() bool { return g.headerTop == 0 && g.right <= g.overflow }
