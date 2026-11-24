package library

import (
	"encoding/json"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 视图模式（渲染层 ui.store 的 libraryViewMode）：卡片是窄列表加常驻详情栏，列表是全宽表格。
const (
	ViewCard = "card"
	ViewList = "list"
)

// detailMinWidth 是详情栏的最小宽度：列表再宽也不能把阅读区挤到这个宽度以下（渲染层 DETAIL_MIN_WIDTH）。
const detailMinWidth float32 = 400

// itemListBounds 是列表栏宽度的缺省与上下限（渲染层 ITEM_LIST_PANE_WIDTH_*）。
var itemListBounds = settings.PaneWidths[1]

// listPane 是工作区里列表栏的界面状态（Store 之外、只属于界面的东西）。
type listPane struct {
	// loaded 表示偏好已读过；stored 是已保存的列表栏宽度（未按窗口夹取），view 是视图模式。
	loaded bool
	stored float32
	view   string

	// resizer 是分隔条的拖拽状态；wsWidth 是上一帧工作区容器的宽度（渲染层的 ResizeObserver）：
	// 它决定列表栏能拉多宽，首帧未知时按上限算。
	resizer kit.ColumnResizerState
	wsWidth float32

	// window 窗口化卡片行；cardHeight 是各卡片上一帧布局后的自然高度（取整行高用，见 entryRow）；
	// sort* 是排序菜单与它的锚点（排序按钮上一帧的外框）。
	window     kit.RowWindow
	reveal     cardReveal // 选中行滚进视野（渲染层 ItemList 的 effect，keyboard.go）
	entriesRev int        // 上一帧见到的 EntriesRev：内容换了要补一次悬停刷新（台账 B-037）
	cardHeight map[string]float32
	sortMenu   kit.MenuState
	sortOpen   bool
	sortAt     gfx.Point
	sortRect   gfx.Rect

	// pageSize 是分页条里「每页」下拉的状态。
	pageSize kit.SelectState

	// filter 是组合筛选弹层的状态；filterTrigger 与 filterRect 是它的触发钮（Esc 关闭时还焦点、点它只收起）；
	// toolbarID 是工具条（tabindex=-1 的容器）：入口随筛选取消而卸载时，焦点交给它（toolbarFocus 是交接的进度，见 toolbarHit）。
	filter        kit.PopoverState
	filterTrigger gfx.ID
	filterRect    gfx.Rect
	toolbarID     gfx.ID
	toolbarFocus  toolbarFocus
}

// ensureMounted 是渲染层「挂载」的对应：侧栏或工作区第一次画出来时重新进入模块（读取智能视图与全部数据），
// 两者谁先画都行——侧栏收起时工作区照常挂载。
func (s *Screen) ensureMounted() {
	if !s.mounted {
		s.mounted = true
		s.Enter()
	}
	s.shown = true
}

// loadPane 读取列表栏宽度与视图模式（只读一次）。
func (s *Screen) loadPane() {
	p := &s.pane
	if p.loaded {
		return
	}
	p.loaded = true
	p.stored = float32(itemListBounds.Default)
	if raw, ok := s.prefValue(settings.PrefItemListPaneWidth); ok {
		var width float64
		if json.Unmarshal(raw, &width) == nil {
			p.stored = float32(itemListBounds.Clamp(width))
		}
	}
	p.view = ViewCard
	if raw, ok := s.prefValue(settings.PrefLibraryViewMode); ok {
		var mode string
		if json.Unmarshal(raw, &mode) == nil {
			p.view = settings.NormalizeLibraryViewMode(mode)
		}
	}
}

// prefValue 读一项界面偏好；没有存储或没有这一项时 ok 为假。
func (s *Screen) prefValue(key string) (json.RawMessage, bool) {
	if s.Prefs == nil {
		return nil, false
	}
	return s.Prefs.Get(key)
}

// ViewMode 是此刻的视图模式。
func (s *Screen) ViewMode() string {
	s.loadPane()
	return s.pane.view
}

// setViewMode 切换视图模式并保存；保存失败只记日志（偏好丢了下次启动回到默认，不影响当前使用）。
func (s *Screen) setViewMode(mode string) {
	s.loadPane()
	if s.pane.view == mode {
		return
	}
	s.pane.view = mode
	if s.Prefs != nil {
		if err := s.Prefs.Set(settings.PrefLibraryViewMode, mode); err != nil {
			s.Guard.Background("library.viewMode", err)
		}
	}
}

// saveListWidth 保存列表栏宽度（拖动结束、按键、双击之后）。
func (s *Screen) saveListWidth(width float32) {
	s.pane.stored = width
	if s.Prefs != nil {
		if err := s.Prefs.Set(settings.PrefItemListPaneWidth, float64(width)); err != nil {
			s.Guard.Background("library.listWidth", err)
		}
	}
}

// listWidths 返回此刻列表栏的宽度与它能拉到的上限：上限是工作区宽度减去详情栏的最小宽度，再夹在 [下限, 上限] 里。
func (s *Screen) listWidths() (width, hi float32) {
	p := &s.pane
	lo, top := float32(itemListBounds.Min), float32(itemListBounds.Max)
	hi = top
	if p.wsWidth > 0 {
		hi = max(lo, min(top, kit.QuantizeLU(p.wsWidth)-detailMinWidth)) // 渲染层读的是 ResizeObserver 的 LayoutUnit 宽度
	}
	return min(p.stored, hi), hi
}

// Workspace 是知识库模块的工作区（渲染层 components/library/LibraryWorkspace）：
// 卡片视图是窄列表栏加常驻详情栏，列表视图是全宽表格；专注阅读模式下只有详情栏（detail.go）。
// 图片查看器的整窗浮层（detailviewer.go）跟着工作区：工作区没画出来，查看器随之结束（渲染层里它随组件卸载）。
func (s *Screen) Workspace(c ui.C) {
	s.workspaceBody(c)
	s.viewerLayer(c)
}

func (s *Screen) workspaceBody(c ui.C) {
	s.table.head.resetFrame() // 吸附表头对侧栏阴影的遮挡由本帧表头的 Draw 重新记下（B-048）
	s.ensureMounted()
	s.loadPane()
	s.wsShown = true
	s.cardGroups.Begin()
	s.settleBulk()
	v := s.Store.View()
	s.noteEntriesChanged(v.List)
	if s.detail.focusReading {
		s.focusWorkspace(c, v)
		return
	}
	if s.pane.view == ViewList {
		s.pane.reveal = cardReveal{} // 卡片列表卸载：下次挂载时已有选中行要重新滚进视野
		s.tableWorkspace(c, v)
		return
	}
	s.table.reveal = tableReveal{} // 表格卸载：下次挂载时已有选中行要重新滚进视口
	s.cardWorkspace(c, v)
}

// noteEntriesChanged 在列表内容整体换了（翻页、筛选、重新取数）的那一帧请求补一次悬停刷新：渲染层里换内容之后
// Chromium 会补发 pointerover，指针底下的新行立刻有悬停态；Viva 只在指针事件里重算悬停（台账 B-037）。
func (s *Screen) noteEntriesChanged(list ListView) {
	if s.pane.entriesRev == list.EntriesRev {
		return
	}
	s.pane.entriesRev = list.EntriesRev
	s.Look.RequestHoverRefresh()
}

// cardWorkspace 是卡片视图的布局：flex h-full min-h-0 flex-1 overflow-hidden，左边列表栏，右边详情栏。
func (s *Screen) cardWorkspace(c ui.C, v View) {
	p := &s.pane
	width, hi := s.listWidths()
	if !p.resizer.Dragging() {
		p.resizer.Width = width // 分隔条显示夹取之后的宽度，拖动从这里起步
	}
	ui.Row(c.Key("workspace")).FillWidth().Grow(1).MinHeight(0).Clip().
		Draw(func(d *ui.DrawContext) { p.wsWidth = d.Rect.Width }).
		Children(func() {
			s.listAside(c, v, p.resizer.Width, hi)
			s.detailColumn(c, v) // min-w-0 flex-1 app-wallpaper-section：详情栏
		})
}

// listAside 是列表栏：relative shrink-0 border-r border-border app-wallpaper-panel，右缘一条可拖的分隔条。
func (s *Screen) listAside(c ui.C, v View, width, hi float32) {
	l := s.Look
	t := l.T
	p := &s.pane
	ui.Row(c.Key("list-aside")).Width(width).Shrink(0).FillHeight().PadSides(0, 1, 0, 0).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.UiPanelBg, Shadow: t.UiPanelShadow})
			l.PaintEdge(d, kit.EdgeRight, 1, t.UiPanelBorder)
		}).Children(func() {
		ui.Column(c.Key("list-col")).Grow(1).MinWidth(0).FillHeight().Children(func() { s.itemList(c, v) })
		// absolute inset-y-0 right-0 z-10 flex：里面一条 ColumnResizer。
		ui.Row(c.Key("list-resizer")).Absolute().Top(0).Right(0).Bottom(0).Children(func() {
			l.ColumnResizer(c, &p.resizer, kit.ColumnResizerSpec{
				Name: locales.LibraryResizeList(), Min: float32(itemListBounds.Min), Max: hi, Default: float32(itemListBounds.Default),
				OnCommit: s.saveListWidth,
			})
		})
	})
}
