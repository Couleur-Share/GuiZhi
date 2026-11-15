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

// Host 是窗口与平台提供给外壳的能力。
type Host interface {
	// Invalidate 请求重绘，可在任意 goroutine 调用。
	Invalidate()
	// SystemDark 报告系统当前是否深色外观（UI 线程）。
	SystemDark() bool
	// ApplyAppearance 让系统绘制的部分（窗口按钮等）跟随应用外观（UI 线程）；
	// 应用外观与系统不同时，系统按钮的字形才不会和背景同色。
	ApplyAppearance(mode kit.Mode)
}

// Options 是 App 的依赖与初始状态。
type Options struct {
	Services services.Services
	// Theme 是初始外观；完整的零值取 kit.DefaultThemeSpec()，其余情况原样使用（所以要改某一项就从默认值起步）。
	Theme kit.ThemeSpec
	Route Route // 为空取知识库
}

// App 是应用外壳。
type App struct {
	svc services.Services

	mu   sync.Mutex // 保护 host（窗口在应用创建之后才有）
	host Host

	// 仅 UI 线程读写
	spec        kit.ThemeSpec
	look        *kit.Look
	lookKey     lookKey
	applied     *kit.Look // 已交给 ui.Context 的外观
	appliedZoom float32
	route       Route

	startRoute Route // 创建时的页面，创建后不变；Ready 只看它（可在任意 goroutine 读）
	imports    *importsscreen.Screen
}

// lookKey 是装配外观的全部输入；任何一项变了才重新装配。
type lookKey struct {
	spec       kit.ThemeSpec
	systemDark bool
	prefs      ui.Preferences
}

// New 创建外壳；页面在 Start 时才开始加载数据。
func New(o Options) *App {
	if o.Theme == (kit.ThemeSpec{}) {
		o.Theme = kit.DefaultThemeSpec()
	}
	a := &App{svc: o.Services, spec: o.Theme, route: o.Route, appliedZoom: 1}
	if a.route == "" {
		a.route = RouteLibrary
	}
	a.startRoute = a.route
	a.imports = importsscreen.New(importsscreen.Options{
		Imports:    o.Services.Imports,
		Invalidate: a.invalidate,
		Now:        o.Services.Now,
	})
	return a
}

// AttachHost 注入窗口，并让系统绘制的部分先跟上应用外观（窗口显示前调用，避免先闪一下系统外观）。
func (a *App) AttachHost(h Host) {
	a.mu.Lock()
	a.host = h
	a.mu.Unlock()
	if h != nil {
		h.ApplyAppearance(a.spec.Mode)
	}
}

// SetAppearance 换一套外观（UI 线程）：设置页改了主题色、明暗、字号档、动效或壁纸后调用。
func (a *App) SetAppearance(spec kit.ThemeSpec) {
	if spec == a.spec {
		return
	}
	modeChanged := spec.Mode != a.spec.Mode
	a.spec = spec
	if h := a.currentHost(); h != nil && modeChanged {
		h.ApplyAppearance(spec.Mode)
	}
	a.invalidate()
}

func (a *App) currentHost() Host {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.host
}

func (a *App) invalidate() {
	if h := a.currentHost(); h != nil {
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

// resolveLook 按当前设置与系统事实装配外观，输入没变就沿用上一次的结果。
func (a *App) resolveLook(c *ui.Context) *kit.Look {
	key := lookKey{spec: a.spec, prefs: c.Preferences()}
	if a.spec.Mode == kit.ModeSystem || a.spec.Mode == "" {
		if h := a.currentHost(); h != nil {
			key.systemDark = h.SystemDark()
		}
	}
	if a.look == nil || key != a.lookKey {
		a.look = kit.NewLook(a.spec, kit.Env{SystemDark: key.systemDark, Prefs: key.prefs})
		a.lookKey = key
	}
	return a.look
}

// View 绘制整个窗口内容。
func (a *App) View(c *ui.Context) {
	l := a.resolveLook(c)
	if a.applied != l {
		c.SetTheme(l.Theme)
		a.applied = l
	}
	if a.appliedZoom != l.Zoom {
		c.SetZoom(l.Zoom) // 字号档位：整窗缩放，字、图标与矢量都保持清晰
		a.appliedZoom = l.Zoom
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(l.T.FillBackground).Children(func() {
		kit.Backdrop(c, l) // 有壁纸时垫在最底层，无壁纸什么都不画
		a.rail(c, l)
		ui.Column(c).Grow(1).Shrink(1).AlignItems(ui.Stretch).Children(func() {
			a.topBar(c, l)
			ui.Box(c).Grow(1).Shrink(1).Children(func() {
				a.content(c, l)
			})
		})
	})
}

// topBar 是顶栏：整条可拖动窗口；右侧给窗口控制按钮让出位置。
// 全局搜索、更新提示与新建入口在 M1 补齐。
func (a *App) topBar(c *ui.Context, l *kit.Look) {
	bar := c.TitleBar()
	ui.Row(c).Height(topBarH).AlignItems(ui.Center).Padding(0, bar.Right, 0, 16).
		BorderWidth(0, 0, 1, 0).BorderColor(l.T.Border).DragWindow().Children(func() {})
}

func (a *App) content(c *ui.Context, l *kit.Look) {
	switch a.route {
	case RouteImports:
		a.imports.View(c, l)
	default:
		a.notMigrated(c, l)
	}
}

// notMigrated 是尚未迁移的模块的占位：如实说明，而不是一个看起来能用的空页面。
func (a *App) notMigrated(c *ui.Context, l *kit.Look) {
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, locales.NativeShellNotMigrated(routeLabel(a.route))).FontSize(14).FontWeight(500).TextColor(l.T.Foreground)
		ui.Text(c, locales.NativeShellNotMigratedHint()).FontSize(12).TextColor(l.T.MutedForeground).MaxWidth(420).Wrap()
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

// rail 是左侧导航：模块分组 + 底部设置齿轮。样式取渲染层 SidebarRail 的写法：
// 底色 sidebar-accent/25，右侧 sidebar-border/60 细线，组间分隔线 sidebar-border/70。
func (a *App) rail(c *ui.Context, l *kit.Look) {
	t := l.T
	ui.Column(c).Width(railWidth).Shrink(0).AlignItems(ui.Stretch).Padding(12, 8).
		Background(t.SidebarAccent.Alpha(0.25)).
		BorderWidth(0, 1, 0, 0).BorderColor(t.SidebarBorder.Alpha(0.6)).Children(func() {
		for gi, group := range railGroups {
			if gi > 0 {
				ui.Box(c).Height(1).Margin(4, 12).Background(t.SidebarBorder.Alpha(0.7))
			}
			for _, r := range group {
				a.railItem(c, l, r)
			}
		}
		ui.Box(c).Grow(1)
		a.settingsButton(c, l)
	})
}

func (a *App) railItem(c *ui.Context, l *kit.Look, r Route) {
	t := l.T
	active := a.route == r
	b := ui.ButtonBase(c).Key(string(r))
	bg := ui.Transparent
	text := t.SidebarForeground.Alpha(0.8)
	switch {
	case active:
		bg, text = t.Primary.Alpha(0.15), t.Foreground
	case b.Hovered():
		bg, text = t.SidebarAccent.Alpha(0.5), t.SidebarForeground
	}
	label := routeLabel(r)
	// 横向内边距取 4（渲染层是 8）：「处理中心」四个汉字刚好 48 DIP，浏览器里恰好放下，MyGo 的文本度量会让它折行
	b.Column().Gap(6).AlignItems(ui.Center).Padding(12, 4).Radius(16).Background(bg).Children(func() {
		box := ui.Column(c).Size(railIconBx, railIconBx).Radius(16).Center()
		if active {
			box.Background(t.Primary.Alpha(0.10))
		}
		box.Children(func() {
			kit.Icon(c, routeIcon(r), 20).TextColor(text)
		})
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(text)
	})
	if b.Clicked() {
		a.Navigate(r)
	}
}

func (a *App) settingsButton(c *ui.Context, l *kit.Look) {
	t := l.T
	active := a.route == RouteSettings
	b := ui.ButtonBase(c).Key(string(RouteSettings)).Label(routeLabel(RouteSettings))
	bg := ui.Transparent
	icon := t.SidebarForeground.Alpha(0.6)
	switch {
	case active:
		bg, icon = t.Primary.Alpha(0.15), t.Foreground
	case b.Hovered():
		bg, icon = t.SidebarAccent.Alpha(0.5), t.SidebarForeground
	}
	b.Size(44, 44).AlignSelf(ui.Center).Radius(16).Background(bg).Children(func() {
		kit.Icon(c, icons.Settings, 20).TextColor(icon)
	})
	if b.Clicked() {
		a.Navigate(RouteSettings)
	}
}
