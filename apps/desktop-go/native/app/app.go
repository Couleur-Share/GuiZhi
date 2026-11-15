// Package app 是原生界面的应用外壳：主题装配、顶栏、左侧导航 rail 与模块路由。
// 外壳只认 native/services 的接口与 native/screens 的页面，不依赖 package main，也不碰窗口 API：
// 窗口通过 Host 接口注入，所以整个外壳能用 ui.Tester 无窗口地测试。
//
// Package app is the application shell of the native UI.
package app

import (
	"sync"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
	importsscreen "guizhi-mygo-shell/native/screens/imports"
	"guizhi-mygo-shell/native/services"
)

// Route 标识一个一级页面；取值与渲染层 AppModule 对应（再加设置页）。
type Route string

const (
	RouteLibrary  Route = "library"
	RouteInbox    Route = "inbox"
	RouteAsk      Route = "ask"
	RouteWiki     Route = "wiki"
	RouteImports  Route = "imports"
	RouteSettings Route = "settings"
)

// railGroups 是 rail 的分组与顺序：资产流转 / 智能探索 / 任务队列，组间细线分隔，
// 避免五枚图标等权并列（与渲染层 SidebarRail 一致）。
var railGroups = [][]Route{
	{RouteLibrary, RouteInbox},
	{RouteAsk, RouteWiki},
	{RouteImports},
}

const (
	railWidth  = 80
	topBarH    = 48
	railIconBx = 36
)

// Host 是窗口提供给外壳的能力。
type Host interface {
	// Invalidate 请求重绘，可在任意 goroutine 调用。
	Invalidate()
}

// Options 是 App 的依赖与初始状态。
type Options struct {
	Services services.Services
	Theme    kit.ThemeSpec
	Route    Route // 为空取知识库
}

// App 是应用外壳。
type App struct {
	svc services.Services

	mu   sync.Mutex // 保护 host（窗口在应用创建之后才有）
	host Host

	// 仅 UI 线程读写
	spec    kit.ThemeSpec
	theme   *ui.Theme
	applied *ui.Theme
	route   Route

	startRoute Route // 创建时的页面，创建后不变；Ready 只看它（可在任意 goroutine 读）
	imports    *importsscreen.Screen
}

// New 创建外壳；页面在 Start 时才开始加载数据。
func New(o Options) *App {
	a := &App{svc: o.Services, spec: o.Theme, route: o.Route}
	if a.route == "" {
		a.route = RouteLibrary
	}
	a.startRoute = a.route
	a.theme = kit.Theme(a.spec)
	a.imports = importsscreen.New(importsscreen.Options{
		Imports:    o.Services.Imports,
		Invalidate: a.invalidate,
		Now:        o.Services.Now,
	})
	return a
}

// AttachHost 注入窗口。
func (a *App) AttachHost(h Host) {
	a.mu.Lock()
	a.host = h
	a.mu.Unlock()
}

func (a *App) invalidate() {
	a.mu.Lock()
	h := a.host
	a.mu.Unlock()
	if h != nil {
		h.Invalidate()
	}
}

// Start 启动各页面的数据加载与事件订阅。
func (a *App) Start() { a.imports.Start() }

// Close 释放订阅与待执行的任务。
func (a *App) Close() { a.imports.Close() }

// Ready 报告起始页面的首次加载是否已有结果（成功或失败都算）；尚未迁移的占位页立即就绪。
// 可在任意 goroutine 调用；-bench 的 READY 标记据此判断首屏已出。
func (a *App) Ready() bool {
	switch a.startRoute {
	case RouteImports:
		return a.imports.Ready()
	default:
		return true
	}
}

// Route 返回当前页面（UI 线程）。
func (a *App) Route() Route { return a.route }

// Navigate 切换页面（UI 线程）。
func (a *App) Navigate(r Route) {
	if a.route != r {
		a.route = r
		a.invalidate()
	}
}

// View 绘制整个窗口内容。
func (a *App) View(c *ui.Context) {
	if a.applied != a.theme {
		c.SetTheme(a.theme)
		a.applied = a.theme
	}
	t := c.Theme()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		a.rail(c, t)
		ui.Column(c).Grow(1).Shrink(1).AlignItems(ui.Stretch).Children(func() {
			a.topBar(c, t)
			ui.Box(c).Grow(1).Shrink(1).Children(func() {
				a.content(c, t)
			})
		})
	})
}

// topBar 是顶栏：整条可拖动窗口；右侧给窗口控制按钮让出位置。
// 全局搜索、更新提示与新建入口在 M1 补齐。
func (a *App) topBar(c *ui.Context, t *ui.Theme) {
	bar := c.TitleBar()
	ui.Row(c).Height(topBarH).AlignItems(ui.Center).Padding(0, bar.Right, 0, 16).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {})
}

func (a *App) content(c *ui.Context, t *ui.Theme) {
	switch a.route {
	case RouteImports:
		a.imports.View(c)
	default:
		a.notMigrated(c, t)
	}
}

// notMigrated 是尚未迁移的模块的占位：如实说明，而不是一个看起来能用的空页面。
func (a *App) notMigrated(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, locales.NativeShellNotMigrated(routeLabel(a.route))).FontSize(14).FontWeight(500)
		ui.Text(c, locales.NativeShellNotMigratedHint()).FontSize(12).TextColor(t.TextMuted).MaxWidth(420).Wrap()
	})
}

func routeLabel(r Route) string {
	switch r {
	case RouteLibrary:
		return locales.NavLibrary()
	case RouteInbox:
		return locales.NavInbox()
	case RouteAsk:
		return locales.NavAsk()
	case RouteWiki:
		return locales.NavWiki()
	case RouteImports:
		return locales.NavImports()
	case RouteSettings:
		return locales.HeaderSettings()
	}
	return string(r)
}

func routeIcon(r Route) icons.Name {
	switch r {
	case RouteLibrary:
		return icons.LibraryBig
	case RouteInbox:
		return icons.Inbox
	case RouteAsk:
		return icons.MessagesSquare
	case RouteWiki:
		return icons.Network
	case RouteImports:
		return icons.Download
	}
	return icons.Settings
}

// rail 是左侧导航：模块分组 + 底部设置齿轮。
func (a *App) rail(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(railWidth).Shrink(0).AlignItems(ui.Stretch).Padding(12, 8).
		Background(t.Text.Alpha(0.03).Over(t.Background)).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		for gi, group := range railGroups {
			if gi > 0 {
				ui.Divider(c).Margin(4, 12)
			}
			for _, r := range group {
				a.railItem(c, t, r)
			}
		}
		ui.Box(c).Grow(1)
		a.settingsButton(c, t)
	})
}

func (a *App) railItem(c *ui.Context, t *ui.Theme, r Route) {
	active := a.route == r
	b := ui.ButtonBase(c).Key(string(r))
	bg := ui.Transparent
	switch {
	case active:
		bg = t.Accent.Alpha(0.15)
	case b.Hovered():
		bg = t.Text.Alpha(0.06)
	}
	label := routeLabel(r)
	// 横向内边距取 4（渲染层是 8）：「处理中心」四个汉字刚好 48 DIP，浏览器里恰好放下，MyGo 的文本度量会让它折行
	b.Column().Gap(6).AlignItems(ui.Center).Padding(12, 4).Radius(16).Background(bg).Children(func() {
		box := ui.Column(c).Size(railIconBx, railIconBx).Radius(16).Center()
		if active {
			box.Background(t.Accent.Alpha(0.10))
		}
		box.Children(func() {
			kit.Icon(c, routeIcon(r), 20).TextColor(t.Text)
		})
		text := t.Text.Alpha(0.8)
		if active {
			text = t.Text
		}
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(text)
	})
	if b.Clicked() {
		a.Navigate(r)
	}
}

func (a *App) settingsButton(c *ui.Context, t *ui.Theme) {
	active := a.route == RouteSettings
	b := ui.ButtonBase(c).Key(string(RouteSettings)).Label(routeLabel(RouteSettings))
	bg := ui.Transparent
	switch {
	case active:
		bg = t.Accent.Alpha(0.15)
	case b.Hovered():
		bg = t.Text.Alpha(0.06)
	}
	b.Size(44, 44).AlignSelf(ui.Center).Radius(16).Background(bg).Children(func() {
		col := t.Text.Alpha(0.6)
		if active {
			col = t.Text
		}
		kit.Icon(c, icons.Settings, 20).TextColor(col)
	})
	if b.Clicked() {
		a.Navigate(RouteSettings)
	}
}
