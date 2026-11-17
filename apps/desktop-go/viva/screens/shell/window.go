// Package shell 是主窗口的页面外壳：左侧 rail、顶栏与当前页（渲染层 App.tsx、SidebarRail.tsx、TopBar.tsx）。
//
// 模块工作区（知识库、处理中心、问答、Wiki、采集）与设置页是它的子页面；这里只管它们的容器、
// 导航状态与全窗口共用的几块（rail、顶栏）。
//
// Package shell is the main window's frame: rail, top bar and the current page.
package shell

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
)

// Page 是窗口当前显示的页（渲染层 currentPage）。
type Page uint8

const (
	// PageHome 是模块工作区；PageSettings 是设置页（rail 与顶栏照常显示）。
	PageHome Page = iota
	PageSettings
)

// smBreakpoint 是 Tailwind sm 断点：窗口宽度达到它，顶栏的快捷键提示与按钮文字才显示。
const smBreakpoint = 640

// compactLayoutMaxWidth 是紧凑布局的上界（ui.store.ts COMPACT_LAYOUT_MAX_WIDTH）：窗口宽度低于它，二级侧栏默认收起，
// 展开时浮在内容上方。渲染层的媒体查询是 (max-width: 1179.98px)。
const compactLayoutMaxWidth = 1180

// Window 是主窗口的页面与它的导航状态。
//
// 它不拥有业务数据：计数、更新信息等由外壳（viva/app）按需填入，用户操作经回调交还外壳。
type Window struct {
	Look *kit.Look

	// Settings 是设置页；Page 为 PageSettings 时显示。
	Settings *settingsscreen.Shell

	// Modules 是各模块的二级侧栏面板与工作区。没有登记的模块在工作区显示「尚未迁移」的占位。
	Modules map[Module]ModuleView

	// PanelResizer 是二级侧栏宽度的拖拽状态：Width 由装配代码用已保存的宽度初始化。
	PanelResizer kit.ColumnResizerState

	// Notifier 是窗口的提示通道（渲染层的 Toast）；nil 时不画提示。提示宿主是覆盖层，在整个页面声明完之后才声明。
	Notifier *kit.Notifier

	// tips 是悬停提示层（渲染层的 TooltipLayer）：声明了 Hint 的命中区悬停或聚焦 320 ms 后出气泡，在最上层。
	tips kit.TooltipState

	Page   Page
	Module Module

	// InboxTotal 是处理中心的待处理数，ActiveImports 是采集的进行中数；大于 0 时 rail 上显示角标。
	InboxTotal, ActiveImports int

	// SidebarCollapsed 是用户保存的二级侧栏偏好（宽屏下的收起/展开状态）；紧凑布局下不看它，见 sidebarCollapsed。
	SidebarCollapsed bool

	// CompactSidebarOpen 是紧凑布局下二级侧栏是否以浮层展开（渲染层 compactSidebarOpen）。
	CompactSidebarOpen bool

	// UpdateVersion 非空表示有可用更新（顶栏显示更新按钮）；Update 为真但版本号为空时按钮只显示固定文案。
	Update        bool
	UpdateVersion string

	// SearchShortcut 是顶栏快捷键提示（读主进程实际生效的组合键，例如 "Alt+Shift+F"）。
	SearchShortcut string

	// CaptureShortcut 是「新建条目」快捷键的显示文案（创建方式菜单里「快速采集」右侧的提示，例如 "Alt+Shift+N"）；空则不显示。
	CaptureShortcut string

	// OwnControls 为真时顶栏自己画最小化/最大化/关闭按钮。真窗口里这三个按钮由 Viva 画在窗口右上角，
	// 顶栏只留出位置；对照工具与无头测试没有真窗口，需要自己画才能与渲染层逐项比较（台账 A-005）。
	OwnControls bool

	// Maximized 是窗口是否最大化（决定「最大化」按钮的名字是「最大化」还是「还原」）。
	Maximized bool

	// 回调：全部可空，空则忽略。
	OnModule        func(Module)
	OnSettings      func()
	OnBack          func()
	OnToggleSidebar func()
	// OnPanelWidth 在二级侧栏宽度定下来（拖动结束、按键、双击）之后调用，用来保存。
	OnPanelWidth  func(width float32)
	OnUpdate      func()
	OnNewCapture  func()
	OnOpenPalette func()
	OnMinimize    func()
	OnMaximize    func()
	OnClose       func()
	// OnNewBlankNote 与 OnImportFiles 是创建方式菜单的「空白笔记」与「采集文件」（新建空白条目；选文件入采集队列）。
	OnNewBlankNote func()
	OnImportFiles  func()

	// CloseDialogOpen 为真时显示「关闭应用」对话框（渲染层 CloseDialog：设置为「每次询问」时点关闭钮，主进程推 window:showCloseDialog）。
	// OnCloseDialog 收用户的选择（最小化、退出、只关对话框），由外壳转给生命周期策略并把 CloseDialogOpen 置假。
	CloseDialogOpen bool
	OnCloseDialog   func(kit.CloseDialogResult)

	// search 是顶栏搜索框（知识库模块下可输入）。
	search  ui.Field // 知识库模块的搜索框
	trigger ui.Field // 其余模块的只读入口（始终为空，点击与按键打开命令面板）

	// createMenuOpen 是「新建」旁边的创建方式菜单是否展开；createMenu 是菜单自己的状态，
	// createMenuAt 是打开那一刻算出的菜单左上角（渲染层只在点击时取一次锚点，之后布局变了菜单也不跟随），
	// caretRect 是箭头按钮上一帧的外框（点它只收起菜单；打开时据此算锚点）。
	createMenuOpen bool
	createMenu     kit.MenuState
	createMenuAt   gfx.Point
	caretRect      gfx.Rect

	closeDialog kit.CloseDialogState
}

// module 返回当前模块；没设置时是知识库（渲染层 appModule 的默认值）。
func (w *Window) module() Module {
	if w.Module == "" {
		return ModuleLibrary
	}
	return w.Module
}

// compact 报告窗口是否处于紧凑布局（渲染层 useCompactLayout）。
func (w *Window) compact(c ui.C) bool {
	return w.Look.ViewportWidth(c) <= compactLayoutMaxWidth-0.02
}

// sidebarCollapsed 是二级侧栏此刻是否收起（渲染层 use-sidebar-collapse）：紧凑布局下默认收起、展开即浮层；
// 其余沿用用户保存的偏好。
func (w *Window) sidebarCollapsed(c ui.C) bool {
	if w.compact(c) {
		return !w.CompactSidebarOpen
	}
	return w.SidebarCollapsed
}

// toggleSidebar 是顶栏收起/展开按钮的动作：紧凑布局切换浮层，否则切换并保存偏好（由外壳经 OnToggleSidebar 落盘）。
func (w *Window) toggleSidebar(c ui.C) {
	if w.compact(c) {
		w.CompactSidebarOpen = !w.CompactSidebarOpen
		return
	}
	w.SidebarCollapsed = !w.SidebarCollapsed
	if w.OnToggleSidebar != nil {
		w.OnToggleSidebar()
	}
}

// View 声明整个窗口：rail 在左，右边是顶栏加当前页。
func (w *Window) View(c ui.C) {
	l := w.Look
	ui.Row(c).Fill().Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: l.T.Background})
	}).Children(func() {
		w.rail(c)
		ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() {
			w.topBar(c)
			w.body(c)
		})
	})
	w.newMenu(c)
	w.closeDialogView(c)
	l.Toasts(c, w.Notifier)
	l.Tooltips(c, &w.tips)
}

// closeDialogView 声明「关闭应用」对话框；用户有了选择就交给 OnCloseDialog（由它决定是否关上，渲染层的 onClose 同理）。
func (w *Window) closeDialogView(c ui.C) {
	res := w.Look.CloseDialog(c.Key("close-dialog"), &w.closeDialog, kit.CloseDialogSpec{
		Open:  w.CloseDialogOpen,
		Title: locales.CloseDialogTitle(), Message: locales.CloseDialogMessage(),
		MinimizeText: locales.CloseDialogMinimizeToTray(), ExitText: locales.CloseDialogExitApp(),
		RememberText: locales.CloseDialogRememberChoice(), CloseLabel: locales.CommonClose(),
	})
	if res.Choice != kit.CloseDialogNone {
		w.CloseDialogOpen = false
		if w.OnCloseDialog != nil {
			w.OnCloseDialog(res)
		}
	}
}

// content 是顶栏下面、侧栏右边的区域：设置页，或当前模块的工作区。
func (w *Window) content(c ui.C) {
	l := w.Look
	if w.Page == PageHome {
		if view, ok := w.Modules[w.module()]; ok && view.Workspace != nil {
			view.Workspace(c)
			return
		}
	}
	if w.Page == PageSettings {
		s := w.Settings
		if s == nil {
			return
		}
		s.OnBack = func() {
			w.Page = PageHome
			if w.OnBack != nil {
				w.OnBack()
			}
		}
		s.Page(c)
		return
	}
	// 模块工作区还没迁移：如实说明，而不是一个看起来能用的空页。
	title := moduleLabel(w.module())
	ui.Column(c).Fill().Items(ui.Center).Justify(ui.Center).Gap(l.Space(2)).Children(func() {
		l.Text(c, locales.NativeShellNotMigrated(title), kit.TextSM, kit.WeightMedium, l.T.Foreground)
		l.Text(c, locales.NativeShellNotMigratedHint(), kit.TextXS, kit.WeightNormal, l.T.MutedForeground)
	})
}

// moduleLabel 返回模块在 rail 上的名字。
func moduleLabel(m Module) string {
	for _, g := range railGroups {
		for _, it := range g {
			if it.module == m {
				return it.label()
			}
		}
	}
	return string(m)
}
