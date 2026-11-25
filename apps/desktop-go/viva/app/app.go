// Package app 是 Viva 原生界面的外壳：负责开窗口并承载页面。
//
// Root 把服务、设置与外观装配成页面（root.go）；Run 打开自绘标题栏的主窗口并阻塞到退出。
// 托盘、全局快捷键、单实例与通知在阶段 2 的外壳接线里补齐（见 docs/agents/viva-ui-development.md §0）。
package app

import (
	"context"
	"fmt"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/island"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/reader"
	"guizhi-mygo-shell/viva/screens/shell"
)

// InterfaceFont 是界面字体族：渲染层的 font-family 里 system-ui 在中文 Windows 上解析成它。
const InterfaceFont = "Microsoft YaHei UI"

// Options 是外壳的装配参数。
type Options struct {
	// Class 命名 WebView2 的 profile 目录（os.UserCacheDir()\<Class>\WebView2）。
	// 必须是专用名，绝不能用 "GuiZhi"：那会指向用户真实安装的 WebView2 配置。
	Class string
	// Title 是窗口标题。
	Title string
	// Width、Height 是窗口内容区的初始大小（逻辑像素）。
	Width, Height int
	// MinWidth、MinHeight 是窗口内容区的下限。
	MinWidth, MinHeight int
	// OnReady 在第一帧声明完成后调用一次（窗口线程）。可为空。
	OnReady func()

	// Services 是界面依赖的 Go 能力（设置、偏好、导入队列、日志……）；缺省的字段界面按默认值处理。
	Services services.Services
	// Module、Page 是起始页：空模块取知识库，Page 为设置页时直接打开设置。
	Module shell.Module
	Page   shell.Page
	// SearchShortcut 是顶栏快捷键提示（读主进程实际生效的组合键）。
	SearchShortcut string
	// CaptureShortcut 是「新建条目」快捷键的显示文案（采集空队列引导里的提示用）。
	CaptureShortcut string
	// AppVersion 与 Platform 是应用版本与运行平台（process.platform 的写法，如 win32）：任务详情的「复制诊断信息」写进环境行。
	// 空串表示取不到，诊断文本就少这一行。
	AppVersion, Platform string
	// Context 是应用生命周期：取消后各模块的后台请求随之停止。空表示 context.Background()。
	Context context.Context
	// SystemDark 是启动时系统是否深色；之后由系统外观监听更新。
	SystemDark bool
	// Appearance 非空时，在设置里的外观之上再叠加一次（自动化实例用命令行开关覆盖外观）。
	Appearance func(kit.Spec) kit.Spec
	// OSReduceMotion 报告系统是否要求减少动态效果，与设置里的动效偏好合并成实际动效档位；缺省读系统（Windows 的「显示动画」开关）。
	// 无头测试注入固定值，结果才不随运行机器的系统设置变化。
	OSReduceMotion func() bool
	// Web 是窗口的 WebHost：网页岛（知识库的阅读区）经它开页面。Run 里为空时自己建一个；无头测试不给，阅读区留白。
	Web *vapp.WebHost
	// ReaderMedia 是阅读岛取本地媒体（用户数据目录里的图片）的实现；nil 表示取不到，文章里的本地图片显示不出来。
	ReaderMedia reader.Media
	// Shell 是外壳接线（关闭策略、托盘、界面命令；见 shell.go）：nil 表示没有（截图、对照与无头测试：关窗口就是关闭）。
	Shell Shell
	// AutoOverlays 为真时启动会自动弹出对话框的后台流程：自动检查更新、旧版数据库探测与迁移确认、首次使用设置清单。
	// 只有用户真正在用的外壳实例才开；自动化、截图与对照实例用的是真实服务，但不能被这些对话框盖住画面（root_overlays.go）。
	AutoOverlays bool
	// HoverRefresh 在滚轮滚动了内容之后被调用，请求窗口在 (x, y)（逻辑像素、窗口坐标）补一次指针移动，让悬停跟上（台账 B-031）。
	// 空表示用系统实现（Windows 上向主窗口 PostMessage(WM_MOUSEMOVE)，hover_windows.go）；无头测试注入记录器，不碰系统消息。
	HoverRefresh func(x, y float32)
	// HoverRefreshHere 在键盘滚动、Tab 滚动、列表换内容之后被调用，请求窗口在指针此刻的位置补一次指针移动（台账 B-037）。
	// 空表示用系统实现（Windows 上取光标位置、指针在本窗口上时 PostMessage(WM_MOUSEMOVE)）；无头测试注入记录器。
	HoverRefreshHere func()

	// 窗口位置与大小记忆（台账 B-027）：以下都可为空，空值时行为与此前完全相同（无头测试、截图、基准实例不给）。
	// Origin 是内容区左上角打开的屏幕位置（逻辑像素）；nil 交给系统摆放。
	Origin *gfx.Point
	// OnFrameChange 在内容区移动或改变大小时被调用（窗口线程；最大化、最小化、全屏期间 Windows 不报）。
	OnFrameChange func(gfx.Rect)
	// OnMaximize、OnFullscreen 报告最大化与全屏状态的切换（含系统发起的）。
	OnMaximize   func(maximized bool)
	OnFullscreen func(fullscreen bool)
}

// Run 打开窗口并阻塞到窗口关闭；返回时进程即将退出。
func Run(opts Options) error {
	if opts.Class == "" {
		return fmt.Errorf("viva/app: Class 不能为空（WebView2 profile 必须用专用名）")
	}
	vapp.SetInterfaceFontFamily(InterfaceFont)
	// 岛的临时 profile 目录清理（R5）：登记窗口 Class，清理失败写日志（动作名是诊断用的英文标识，不是界面文案）。
	island.SetProfileClass(opts.Class)
	if log := opts.Services.Log; log != nil {
		island.SetFailureHook(func(err error) { log.AppError("native", "island", err.Error()) })
	}
	if opts.Web == nil {
		opts.Web = &vapp.WebHost{}
	}
	var inv vapp.Invalidator
	root := NewRoot(opts, inv.Invalidate)
	defer root.Close()
	stopAppearance := root.WatchSystemAppearance(inv.Invalidate)
	defer stopAppearance()

	page := ui.NewPage(ui.PageConfig{
		View: root.View, Theme: root.ThemeRef(), Background: root.BackgroundRef(), Invalidator: &inv,
	})
	return vapp.RunApplication(func(a *vapp.Application) error {
		look := root.Look()
		config := page.Config(vapp.Config{
			Title: opts.Title, Class: opts.Class, Web: opts.Web,
			Width: opts.Width, Height: opts.Height,
			MinWidth: opts.MinWidth, MinHeight: opts.MinHeight,
			// 自绘标题栏：内容铺满整窗，顶栏就是标题栏；窗口按钮由 Viva 画在右上角（台账 A-005）。
			// TitlebarHeight 只在创建时生效，之后改字号档不会改变按钮条高度（台账 A-020）。
			WindowMode:     vapp.WindowCustomTitlebar,
			TitlebarHeight: look.Rem(3),
			WindowControls: func() vapp.WindowControlsStyle { return shell.ControlsStyle(root.Look()) },
			ClearColor:     clearColor(look.Col(look.T.Background)),
		})
		bindInput(&config, root)
		config.Origin, config.OnFrameChange = opts.Origin, opts.OnFrameChange
		config.OnMaximize, config.OnFullscreen = opts.OnMaximize, opts.OnFullscreen
		// 外壳任务在 OnWake 里排空：窗口隐藏（收在托盘）时 Viva 仍会在重绘之前调用它，托盘的创建与销毁不会卡在隐藏的窗口里。
		prevWake := config.OnWake
		config.OnWake = func() {
			if prevWake != nil {
				prevWake()
			}
			root.DrainShell()
		}
		// 关闭请求交给外壳决定（没有外壳时不拦）。
		config.OnCloseRequest = closeGate(opts.Shell)
		var canvas gfx.Canvas
		win, err := a.OpenWindow(config, &canvas)
		if err != nil {
			return fmt.Errorf("viva/app: 打开窗口失败：%w", err)
		}
		if opts.Shell != nil {
			// 事件循环线程：外壳在这里建托盘（AddStatusItem 只能在这条线程上调用）。
			host := &Host{app: a, win: win, root: root}
			root.attachHost(host)
			opts.Shell.Attach(host)
		}
		return nil
	})
}

// bindInput 把 Root 的窗口级输入钩子接到窗口配置上（page.Config 之后调用）。Run 与无头测试共用这一处接线，
// 测试走的就是生产的接线：
//   - OnKey：Tab 可达（下一个停靠点在滚动区视野之外时先滚进视野，台账 B-030，kit/reach.go）、悬停提示的收起（B-017）与
//     知识库列表的窗口级键盘（B-045）排在页面自己的按键路由之前（kit.KeyFirst）——不能直接换掉 config.OnKey，那是页面的路由，
//     换掉它菜单、下拉里的方向键就不动了；
//   - OnShortcut：带修饰键的可打印字符（知识库详情的 Ctrl+S、Ctrl+F、Alt+Z）在 Viva 里不走 OnKey，单独走这一条，同样排在页面的前面；
//   - OnScroll：滚轮滚动了内容之后补一次指针移动，让悬停跟上（台账 B-031，kit/wheel.go）。
func bindInput(config *vapp.Config, root *Root) {
	config.OnKey = kit.KeyFirst(root.OnKey, config.OnKey)
	config.OnShortcut = kit.KeyFirst(root.OnShortcut, config.OnShortcut)
	config.OnScroll = root.OnScroll(config.OnScroll)
	if root.capture != nil {
		// 系统文件拖到快速采集的草稿框上：只有装配了采集才注册为拖放目标，否则系统保持「拒绝拖放」。
		config.OnFileDrop = root.capture.FileDrop
	}
}

// clearColor 把 gfx 颜色换成窗口配置用的颜色类型。
func clearColor(c gfx.Color) vapp.Color {
	return vapp.Color{Red: float64(c.Red), Green: float64(c.Green), Blue: float64(c.Blue), Alpha: float64(c.Alpha)}
}
