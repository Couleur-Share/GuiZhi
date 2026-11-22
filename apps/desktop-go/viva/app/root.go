package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/reader"
	importsscreen "guizhi-mygo-shell/viva/screens/imports"
	inboxscreen "guizhi-mygo-shell/viva/screens/inbox"
	libraryscreen "guizhi-mygo-shell/viva/screens/library"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// Root 是主窗口的根：把服务、外观与各页面装配起来，交给 Viva 的页面声明。
//
// 它不碰窗口 API（窗口由 Run 打开），所以能用无头会话测试。外观每帧按设置求值，设置变了整套令牌随之重装配；
// 页面主题与背景色以指针交给 ui.Page，原地更新即可，不必重建页面。
type Root struct {
	opts Options

	store    *settingsStore
	win      *shell.Window
	settings *settingsscreen.Shell

	// 采集模块：队列状态在窗口生命周期内常驻（rail 角标在别的模块里也要实时），界面只在进入该模块时才画。
	// Services.Imports 为空（装配不全的自动化实例）时两者都是 nil，rail 与工作区按「尚未迁移」处理。
	imports      *importsscreen.Screen
	importsStore *importsscreen.Store
	started      sync.Once
	ctx          context.Context
	cancel       context.CancelFunc
	stopImports  func()

	// 知识库模块：目前只有二级侧栏（工作区随 M2-2 迁入）。Services.Library 为空时是 nil，侧栏按「尚未迁移」处理。
	library *libraryscreen.Screen

	// 处理中心模块：待处理数在窗口生命周期内常驻（rail 角标在别的模块里也要实时），界面只在进入该模块时才画。
	// Services.Inbox 为空时两者都是 nil，rail 与工作区按「尚未迁移」处理。
	inbox      *inboxscreen.Screen
	inboxStore *inboxscreen.Store
	inboxWatch inboxWatch // 知识库计数变化的监听（root_inbox.go）

	// notifier 是窗口的提示通道，guard 是变更类操作的失败通道（错误提示 + 日志）：所有模块共用这一对。
	notifier *kit.Notifier
	guard    kit.Guard

	look     *kit.Look
	lookSpec kit.Spec
	canvas   *gfx.Canvas // 窗口的画布（第一次绘制时记下）：窗口级键盘读此刻的焦点
	theme    gfx.Theme   // ui.Page 读它的当前值
	bg       gfx.Color

	systemDark atomic.Bool // 系统当前是否深色（由系统外观监听更新）
	ready      sync.Once

	hoverWarned atomic.Bool // 悬停刷新找不到窗口的日志只记一次（OnScroll）

	shellState // 外壳接线：跨线程任务、关闭对话框、网页岛（root_shell.go）
}

// NewRoot 装配根。invalidate 在设置被别处改动后调用，请求重绘（可为空）。
func NewRoot(opts Options, invalidate func()) *Root {
	r := &Root{opts: opts}
	r.invalidate = invalidate
	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	r.store = newSettingsStore(opts.Services.Settings, invalidate)
	r.notifier = kit.NewNotifier(invalidate)
	r.guard = kit.Guard{Notify: r.notifier, Log: opts.Services.Log}
	r.settings = &settingsscreen.Shell{
		Store: r.store, Guard: r.guard, Wake: invalidate,
		Solver: opts.Services.Solver, Shortcuts: opts.Services.Shortcuts, Updater: opts.Services.Updater,
		Data: opts.Services.Data, Backup: opts.Services.Backup, ConfigTransfer: opts.Services.ConfigTransfer,
		AppVersion: opts.AppVersion, OpenExternal: opts.Services.OpenExternal,
	}
	r.win = &shell.Window{
		Settings:        r.settings,
		Notifier:        r.notifier,
		SearchShortcut:  opts.SearchShortcut,
		CaptureShortcut: opts.CaptureShortcut,
		WindowActive:    ownProcessForeground,
		OnModule: func(m shell.Module) {
			r.win.Module, r.win.Page = m, shell.PageHome
		},
		OnSettings:    func() { r.win.Page = shell.PageSettings },
		OnCloseDialog: r.onCloseDialog,
	}
	if opts.Module != "" {
		r.win.Module = opts.Module
	}
	r.win.Page = opts.Page
	r.applyPanePrefs()
	r.systemDark.Store(opts.SystemDark)
	r.win.Modules = map[shell.Module]shell.ModuleView{}
	r.mountImports(invalidate)
	r.mountLibrary(invalidate)
	r.mountInbox(invalidate)
	r.syncLook(nil)
	return r
}

// mountImports 装配采集模块（渲染层 components/imports）：Store 持有队列状态，Screen 是界面。
func (r *Root) mountImports(invalidate func()) {
	svc := r.opts.Services.Imports
	if svc == nil {
		return
	}
	r.importsStore = importsscreen.NewStore(svc, r.guard, invalidate)
	r.imports = &importsscreen.Screen{
		Store: r.importsStore, Ctx: r.ctx, Guard: r.guard,
		Hooks: importsscreen.Hooks{
			CaptureShortcut: func() string { return r.opts.CaptureShortcut },
			// 行内与详情里的网页验证重试按钮写「自动验证并采集」还是「验证并自动采集」，取决于设置里 FlareSolverr 是否开启；
			// 每一帧现读（设置在别处改了，下一帧就跟上）。
			WebSolverEnabled: func() bool { return r.store.Values().App.FlareSolverr.Enabled },
			AppInfo:          func() (string, string) { return r.opts.AppVersion, r.opts.Platform },
			OpenSettingsSection: func(section string) {
				r.settings.Active = settingsscreen.SectionID(section)
				r.win.Page = shell.PageSettings
				if invalidate != nil {
					invalidate()
				}
			},
		},
	}
	r.win.Modules[shell.ModuleImports] = shell.ModuleView{Panel: r.imports.Panel, Workspace: r.imports.Workspace}
}

// mountLibrary 装配知识库模块（渲染层 components/library）：Store 持有筛选与计数，Screen 是二级侧栏。
// 工作区迁入之前，这个模块的右侧仍是「尚未迁移」的占位。
func (r *Root) mountLibrary(invalidate func()) {
	svc := r.opts.Services.Library
	if svc == nil {
		return
	}
	store := libraryscreen.NewStore(svc, r.guard, invalidate)
	// 自动保存开关每次现读：设置在别处改了，下一次编辑就按新值排（或不排）防抖保存。
	store.AutoSave = func() bool { return r.store.Values().UI.AutoSave }
	// AI 功能未配置时跳设置页的 AI 分区：回调来自后台 goroutine，窗口状态只能在界面线程上改，所以经 runOnUI 排队。
	store.OnOpenSettingsAI = r.openSettingsAI
	r.library = &libraryscreen.Screen{
		Store: store, Ctx: r.ctx, Guard: r.guard, Prefs: r.opts.Services.Prefs, Now: r.opts.Services.Now,
		OpenExternal: r.opts.Services.OpenExternal, Imports: r.opts.Services.Imports,
		// 编辑器默认预览每次现读：设置在别处改了，下一次打开条目就按新值走。
		EditorPreview: func() bool { return r.store.Values().UI.EditorMarkdownPreview },
	}
	if r.opts.Web != nil {
		// 阅读区的网页岛：宿主随窗口常驻，岛本身在有内容要显示时才懒开、不显示就关（不进空闲内存）；登记到 r.islands，收托盘前统一销毁。
		r.library.SetReader(reader.New(reader.Options{
			Opener: reader.IslandOpener{Web: r.opts.Web, Group: &r.islands},
			Media:  r.opts.ReaderMedia,
			Diag:   r.readerDiag,
		}))
	}
	view := r.win.Modules[shell.ModuleLibrary]
	view.Panel = r.library.Panel
	view.Workspace = r.library.Workspace
	view.HidePanel = r.library.FocusReading
	r.win.Modules[shell.ModuleLibrary] = view
	r.win.OnSearch = r.library.SearchTyped
	if r.importsStore != nil {
		// 采集任务完成：条目与计数都变了（渲染层 import.store 的 handleChanged）。
		r.importsStore.OnCompleted = func(task services.ImportTask) { store.OnImportCompleted(r.ctx, task) }
	}
}

// readerDiag 把阅读岛的诊断（打不开、握手失败、页面事件不合规）写进日志；宿主保证错误文字里没有页面给的内容与链接地址。
func (r *Root) readerDiag(err error) {
	if log := r.opts.Services.Log; log != nil {
		log.AppError("native", "reader", err.Error())
	}
}

// startBackground 在第一帧启动常驻的后台工作：采集队列的推送订阅与首次读取（rail 角标要在任何模块里都准）。
func (r *Root) startBackground() {
	r.startInbox()
	if r.importsStore == nil {
		return
	}
	r.stopImports = r.importsStore.Subscribe(r.ctx)
	r.importsStore.Spawn(func() { r.importsStore.Fetch(r.ctx) })
}

// currentSpec 是此刻应当使用的外观：设置里的外观，自动化实例再叠加命令行覆盖。
func (r *Root) currentSpec() kit.Spec {
	spec := SpecFromSettings(r.store.Values().UI, r.systemDark.Load(), r.osReduceMotion())
	if r.opts.Appearance != nil {
		spec = r.opts.Appearance(spec)
	}
	return spec
}

// osReduceMotion 是系统是否要求减少动态效果。
func (r *Root) osReduceMotion() bool {
	if r.opts.OSReduceMotion != nil {
		return r.opts.OSReduceMotion()
	}
	return vapp.ReduceMotion()
}

// syncLook 在外观输入变化时重新装配 Look，并让页面读到新主题；c 非空时顺带取窗口缩放。
func (r *Root) syncLook(c ui.C) {
	if spec := r.currentSpec(); r.look == nil || spec != r.lookSpec {
		scale := float32(0)
		if r.look != nil {
			scale = r.look.Scale
		}
		r.look, r.lookSpec = kit.NewLook(spec), spec
		r.look.Scale = scale
		r.look.HoverRefresh = r.refreshHoverHere
		r.look.KeyHook = r.listKey
		r.look.ShortcutHook = r.listShortcut
		r.theme = r.look.Theme
		r.bg = r.look.Col(r.look.T.Background)
		r.win.Look, r.settings.Look = r.look, r.look
		if r.imports != nil {
			r.imports.Look = r.look
		}
		if r.library != nil {
			r.library.Look = r.look
		}
		if r.inbox != nil {
			r.inbox.Look = r.look
		}
	}
	if c != nil {
		canvas := c.Canvas()
		r.canvas = canvas
		r.look.Scale = float32(canvas.PixelScale())
		// 动效关闭档：让 Viva 的过渡与入退场直接到终态（它自己的「减少动态效果」开关就是这个语义）；
		// 其余档位播放动画，时长由 kit 按档位给出。
		canvas.SetReduceMotion(r.lookSpec.Motion == kit.MotionLevelOff)
	}
}

// Look 返回当前外观（窗口线程）。
func (r *Root) Look() *kit.Look { return r.look }

// ThemeRef 与 BackgroundRef 是交给 ui.PageConfig 的指针：Root 原地更新它们指向的值。
func (r *Root) ThemeRef() *gfx.Theme        { return &r.theme }
func (r *Root) BackgroundRef() *gfx.Color   { return &r.bg }
func (r *Root) Services() services.Services { return r.opts.Services }

// Notifier 是窗口的提示通道；Guard 是变更类操作的失败通道。任意 goroutine 可用。
func (r *Root) Notifier() *kit.Notifier { return r.notifier }
func (r *Root) Guard() kit.Guard        { return r.guard }

// OnKey 是窗口按键的前置钩子：收起悬停提示（B-017）与 Tab 可达（滚动区里滚入视野，B-030），再加上知识库列表的窗口级键盘
// （方向键、Home、End、空格，B-045，经 Look.KeyHook），见 kit.Look.OnKey。窗口线程调用；Look 还没装配好（第一次绘制之前）时不插手。
func (r *Root) OnKey(e gfx.KeyEvent) gfx.EventResult {
	if r.look == nil {
		return gfx.EventResult{}
	}
	return r.look.OnKey(e)
}

// listKey 是 Look.KeyHook：知识库列表的窗口级键盘。还没有画布（第一次绘制之前）或没有装配知识库时不插手。
func (r *Root) listKey(e gfx.KeyEvent) gfx.EventResult {
	if r.library == nil || r.canvas == nil {
		return gfx.EventResult{}
	}
	return r.library.ListKey(e, r.canvas)
}

// OnShortcut 是窗口快捷键（带修饰键的可打印字符）的前置钩子，见 kit.Look.OnShortcut。窗口线程调用；Look 还没装配好时不插手。
func (r *Root) OnShortcut(e gfx.KeyEvent) gfx.EventResult {
	if r.look == nil {
		return gfx.EventResult{}
	}
	return r.look.OnShortcut(e)
}

// listShortcut 是 Look.ShortcutHook：知识库详情的窗口级快捷键。没有装配知识库时不插手。
func (r *Root) listShortcut(e gfx.KeyEvent) gfx.EventResult {
	if r.library == nil {
		return gfx.EventResult{}
	}
	return r.library.ListShortcut(e)
}

// errHoverRefreshNoWindow 是补发悬停刷新时找不到本进程 Viva 窗口的诊断信息：只写日志，不给用户看
// （所以走 errors.New，符合「诊断信息用中文、界面文案进 i18n」的约定，见 nocjk_test.go）。
var errHoverRefreshNoWindow = errors.New("没有找到 Viva 主窗口，滚动（滚轮、键盘）或列表换内容之后悬停不会自动刷新（台账 B-031、B-037）")

// refreshHoverHere 是 kit.Look.HoverRefresh：请求窗口在指针此刻的位置补一次指针移动（台账 B-037）。
// 通道是 Options.HoverRefreshHere，空时用系统实现；找不到窗口时与 OnScroll 一样只记一次日志。
func (r *Root) refreshHoverHere() {
	if post := r.opts.HoverRefreshHere; post != nil {
		post()
		return
	}
	if postHoverRefreshHere() || !r.hoverWarned.CompareAndSwap(false, true) {
		return
	}
	if log := r.opts.Services.Log; log != nil {
		log.AppError("native", "hover-refresh", errHoverRefreshNoWindow.Error())
	}
}

// OnScroll 包住页面的滚轮处理：滚轮滚动了内容之后，请求窗口在原位补一次指针移动，让悬停跟上（台账 B-031，见 kit.RefreshHoverAfterWheel）。
// 窗口线程调用。补发的通道是 Options.HoverRefresh，空时用系统实现；系统实现找不到窗口（Viva 升级后窗口类名变了之类）
// 时悬停会停在滚走的元素上，所以记一条日志（只记一次），不让它悄悄失效。
func (r *Root) OnScroll(next func(gfx.ScrollEvent) gfx.EventResult) func(gfx.ScrollEvent) gfx.EventResult {
	post := r.opts.HoverRefresh
	if post == nil {
		post = func(x, y float32) {
			if postHoverRefresh(x, y) || !r.hoverWarned.CompareAndSwap(false, true) {
				return
			}
			if log := r.opts.Services.Log; log != nil {
				log.AppError("native", "hover-refresh", errHoverRefreshNoWindow.Error())
			}
		}
	}
	return kit.RefreshHoverAfterWheel(next, post)
}

// View 声明整个窗口内容。
func (r *Root) View(c ui.C) {
	r.syncLook(c)
	r.look.BeginPaint()
	r.DrainShell()
	r.started.Do(r.startBackground)
	if r.importsStore != nil {
		r.win.ActiveImports = r.importsStore.View().ActiveCount
	}
	r.syncInbox()
	r.win.View(c)
	if r.library != nil {
		r.library.EndFrame()
	}
	if r.inbox != nil {
		r.inbox.EndFrame()
	}
	if r.opts.OnReady != nil {
		r.ready.Do(r.opts.OnReady)
	}
}

// Window 返回窗口页面（顶栏与外壳的状态在这里；测试与装配用）。
func (r *Root) Window() *shell.Window { return r.win }

// WatchSystemAppearance 在系统外观变化时更新缓存并请求重绘；返回停止函数。
func (r *Root) WatchSystemAppearance(invalidate func()) (stop func()) {
	r.systemDark.Store(vapp.SystemAppearance() == vapp.AppearanceDark)
	return vapp.WatchSystemAppearance(func() {
		r.systemDark.Store(vapp.SystemAppearance() == vapp.AppearanceDark)
		if invalidate != nil {
			invalidate()
		}
	})
}

// Close 释放订阅、取消后台请求。采集队列里还在撤销窗口内的移除先提交，关窗口不会把「已删除」的行带回来。
func (r *Root) Close() {
	r.closeIslands()
	if r.importsStore != nil {
		r.importsStore.FlushRemovals()
	}
	if r.library != nil {
		r.library.Store.FlushRemovals()
		r.library.Store.Close() // 取消转写与排版的进度订阅
	}
	r.closeInbox()
	if r.stopImports != nil {
		r.stopImports()
	}
	r.cancel()
	r.store.Close()
}
