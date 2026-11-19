package library

import (
	"encoding/json"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 列表视图（渲染层 components/library/ItemTableView）：占满内容区的表格。
// 复选框列吸附在左（渲染层只在配置里声明，并没有真正吸附）、操作列吸附在右，表头吸顶，列宽可拖、列可隐藏，底部分页。
// 分页在服务端做，Entries 就是当前页。点标题或行打开详情浮层——表格模式下没有常驻详情栏（详情浮层随 M2-4 迁入，台账 B-043）。

// 表格容器的内边距（px-4 py-3）与顶上一行按钮到表格的间距（mb-2），吸附位置的换算要用到。
const (
	tableAreaPadX = 4 // 单位：Tailwind 间距
	tableAreaPadY = 3
)

// tableState 是列表视图自己的界面状态（Store 之外、只属于界面的东西）。
type tableState struct {
	loaded bool
	cols   []tableColumn

	// 列设置菜单：menu 是弹层的状态（锚点在展开那一刻取），menuTrigger 与 menuRect 是触发钮（点它只收起、Esc 时还焦点）。
	menu        kit.PopoverState
	menuTrigger gfx.ID
	menuRect    gfx.Rect

	// resizers 是各列拖动把手的状态（拖动期间指针被捕获）。
	resizers map[columnID]*colResizer

	// topRowH 是表格上方「本页 / 全部符合筛选 / 列设置」那一行上一帧的高度：吸附表头要用它换算表格顶在内容里的位置。
	topRowH float32
	// tableH 与 tableW 是表格上一帧布局后的高与宽：表头吸附不能越出表格底；actionsW 是操作列的实际宽度（表格更宽时被放大）。
	tableH, tableW, actionsW float32
	// checkHover 是复选框正被悬停的那一行的 ID：复选框是套件控件，没法登记进行的悬停分组，行的悬停靠它补上（晚一帧，与分组同口径）。
	checkHover string
	// firstRow 是本页第一行的 ID：它的上线是表头的下边框，由表头画。
	firstRow string
	// titleWrapped 记下哪些行的标题折成了多行：悬停的下划线只画单行文字（kit.Underline），折行的不画。
	titleWrapped map[string]bool
	// reveal 是「选中行滚进视口」的状态（tablereveal.go）。
	reveal tableReveal
}

// colResizer 是一列表头拖动把手的状态（渲染层 ItemTableHeaderCell 的 isDragging 与拖动起点）。
type colResizer struct {
	dragging           bool
	startX, startWidth float32
}

// resizer 取一列把手的状态（按需创建）。
func (t *tableState) resizer(id columnID) *colResizer {
	if t.resizers == nil {
		t.resizers = map[columnID]*colResizer{}
	}
	if t.resizers[id] == nil {
		t.resizers[id] = &colResizer{}
	}
	return t.resizers[id]
}

// loadColumns 读取列偏好（只读一次）。
func (s *Screen) loadColumns() {
	t := &s.table
	if t.loaded {
		return
	}
	t.loaded = true
	var raw json.RawMessage
	if v, ok := s.prefValue(settings.PrefLibraryColumns); ok {
		raw = v
	}
	t.cols = mergeStoredColumns(raw)
}

// saveColumns 保存列偏好；保存失败只记日志（偏好丢了下次启动回到默认，不影响当前使用）。
func (s *Screen) saveColumns(value any) {
	if s.Prefs == nil {
		return
	}
	if err := s.Prefs.Set(settings.PrefLibraryColumns, value); err != nil {
		s.Guard.Background("library.columns", err)
	}
}

// toggleColumn 切换一列的显示（渲染层 toggleColumn）。
func (s *Screen) toggleColumn(id columnID) {
	cols := s.table.cols
	for i := range cols {
		if cols[i].ID == id {
			cols[i].Visible = !cols[i].Visible
		}
	}
	s.saveColumns(columnsPayload(cols))
}

// resetColumns 恢复默认列：渲染层删掉 localStorage 里的键，这里 Prefs 没有删除，写入 null（读回来同样当作没有偏好）。
func (s *Screen) resetColumns() {
	s.table.cols = defaultColumns()
	s.saveColumns(nil)
}

// setColumnWidth 把一列的宽度设为 width（夹在上下限里）；commit 为真时一并保存（拖动结束、按键之后）。
func (s *Screen) setColumnWidth(id columnID, width float32, commit bool) {
	cols := s.table.cols
	for i := range cols {
		if cols[i].ID == id {
			cols[i].Width = clampColumnWidth(cols[i], float64(width))
		}
	}
	if commit {
		s.saveColumns(columnsPayload(cols))
	}
}

// tableWorkspace 是列表视图的工作区：app-wallpaper-section flex h-full min-h-0 flex-1 flex-col overflow-hidden，
// 里面是工具条、可滚动的表格区与分页条。多选时工具条换成批量条，随批量操作迁入（M2-3）。
func (s *Screen) tableWorkspace(c ui.C, v View) {
	l := s.Look
	t := l.T
	s.loadColumns()
	ui.Column(c.Key("table-workspace")).FillWidth().Grow(1).MinHeight(0).Clip().
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg}) }).
		Children(func() {
			s.listToolbar(c, v)
			s.tableArea(c, v)
			s.pager(c, v.List, true)
		})
}

// tableArea 是表格区（data-testid="item-table"）：min-h-0 flex-1 overflow-auto px-4 py-3，
// 里面是右对齐的按钮行，再按「读取中、读取失败、空、表格」四选一。
func (s *Screen) tableArea(c ui.C, v View) {
	l := s.Look
	list := v.List
	empty := len(list.Entries) == 0
	cols := visibleColumns(s.table.cols)
	l.ScrollArea2D(c.Key("table-scroll"), func(view kit.Scroll2D) {
		s.revealSelected(view, list.SelectedID)
		content := ui.Column(c.Key("table-content")).FillWidth().Shrink(0).
			PadSides(l.Space(tableAreaPadY), l.Space(tableAreaPadX), l.Space(tableAreaPadY), l.Space(tableAreaPadX)).
			Draw(func(d *ui.DrawContext) { s.noteOrigin(d) })
		table := !(list.Loading && empty) && !(list.LoadError != "" && empty) && !empty
		if table {
			// 表格的最小宽度是各列宽度之和（含面板边框），再加两侧内边距：窄了就横向滚动。
			content = content.MinWidth(columnsMinWidth(cols) + 2*l.Space(tableAreaPadX))
		}
		content.Children(func() {
			s.tableTopRow(c, view)
			switch {
			case list.Loading && empty:
				// flex h-40 items-center justify-center：一枚 sm 的 muted 转圈。
				ui.Row(c.Key("loading")).FillWidth().Height(l.Rem(10)).Items(ui.Center).Justify(ui.Center).Children(func() {
					l.Spinner(c, l.Rem(1), l.T.MutedForeground)
				})
			case list.LoadError != "" && empty:
				// app-wallpaper-surface rounded-xl border border-border：里面是读取失败的提示与重试。
				s.tableCard(c.Key("error"), 0, func() {
					if l.LoadErrorState(c, list.LoadError) {
						s.Store.Spawn(func() { s.Store.FetchList(s.Ctx) })
					}
				})
			case empty:
				message := locales.LibraryListEmpty()
				if v.Filters.Scope == services.LibraryScopeTrash {
					message = locales.LibraryTrashEmpty()
				}
				// flex h-40 items-center justify-center rounded-xl border border-border px-6 text-center text-sm text-muted-foreground
				s.tableCard(c.Key("empty"), l.Rem(10), func() {
					ui.Row(c.Key("empty-text")).FillWidth().FillHeight().Items(ui.Center).Justify(ui.Center).
						PadSides(0, l.Space(6), 0, l.Space(6)).Children(func() {
						l.Text(c, message, kit.TextSM, kit.WeightNormal, l.T.MutedForeground, kit.Centered(), kit.Wrapped())
					})
				})
			default:
				s.tablePanel(c, v, cols, view)
			}
		})
	})
}

// tableCard 是空态与失败态的外框：rounded-xl border border-border，底色是面板底色；height 为 0 表示随内容。
func (s *Screen) tableCard(c ui.C, height float32, body func()) {
	l := s.Look
	t := l.T
	box := ui.Column(c).FillWidth().Shrink(0).Pad(1)
	if height > 0 {
		box = box.Height(height)
	}
	box.Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.UiSurfaceBg, Border: t.UiSurfaceBorder, BorderW: 1, Radius: l.Rem(0.75), Shadow: t.UiSurfaceShadow})
	}).Children(body)
}

// tableTopRow 是表格上方的一行：mb-2 flex items-center justify-end gap-2。
// 它是块级元素，宽度是容器的内容宽度（视口宽减两侧内边距），不随表格的横向溢出变宽：横向滚动时它留在原处。
func (s *Screen) tableTopRow(c ui.C, view kit.Scroll2D) {
	l := s.Look
	row := ui.Row(c.Key("top")).Shrink(0).Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).MarginBottom(l.Space(2))
	if view.ViewportW > 0 {
		row = row.Width(max(view.ViewportW-2*l.Space(tableAreaPadX), 0))
	} else {
		row = row.FillWidth()
	}
	row.Draw(func(d *ui.DrawContext) { s.table.topRowH = d.Rect.Height }).Children(func() {
		style := l.UIGhostButton(kit.UIButtonSM)
		if l.Button(c.Key("page"), kit.ButtonSpec{Label: locales.LibrarySelectPage()}, style).Clicked() {
			s.Store.TogglePageSelection()
		}
		if l.Button(c.Key("all"), kit.ButtonSpec{Label: locales.LibrarySelectAllMatching()}, style).Clicked() {
			s.selectAllMatching()
		}
		s.columnConfigButton(c)
	})
}

// stickyGeometry 换算吸附元素的位置（渲染层的 position: sticky）。
//
// sticky 的约束矩形是滚动容器的内容盒（扣掉 px-4 py-3 的内边距）：吸附表头的 top 是 -12px（-top-3），
// 正好抵消上内边距，吸在容器上缘；吸附列的 right 是 0，吸在内容盒右缘，也就是视口右缘往里 16px。
// 都不能越出自己的包含块（表格）：表格滚出去了，吸附元素跟着走。坐标都在滚动内容里（原点是内边距盒的左上角）。
type stickyGeometry struct {
	// headerTop 是表头相对表格顶的下移量（0 表示在原位）。
	headerTop float32
	// right 是吸附列的右缘比表格右缘往里缩多少（0 表示在原位）。
	right float32
	// overflow 是表格右缘越出面板内缘的量（面板内宽比表格窄 2px 边框，超出的被 overflow-clip 裁掉）。
	overflow float32
}

func (s *Screen) stickyAt(view kit.Scroll2D, minW float32) stickyGeometry {
	l := s.Look
	padX, padY := l.Space(tableAreaPadX), l.Space(tableAreaPadY)
	// 表格顶在内容里的位置：上内边距 + 按钮行 + mb-2 + 面板的 1px 边框。
	tableTop := padY + s.table.topRowH + l.Space(2) + 1
	g := stickyGeometry{headerTop: max(0, view.Offset-tableTop)}
	// 表头不能越出表格：表格滚出去了，吸附的表头跟着走（它的包含块是表格）。
	if h := s.table.tableH; h > 0 {
		g.headerTop = min(g.headerTop, max(0, h-s.headHeight()))
	}
	if view.ViewportW > 0 {
		// 表格宽度：面板宽度（容器内容宽）减去 2px 边框，但不小于各列宽度之和（超出的部分被面板裁掉）。
		tableW := max(minW, view.ViewportW-2*padX-2)
		tableRight := padX + 1 + tableW
		g.right = max(0, tableRight-(view.OffsetX+view.ViewportW-padX))
		g.overflow = min(max(0, minW-(max(minW, view.ViewportW-2*padX)-2)), 2)
	}
	return g
}
