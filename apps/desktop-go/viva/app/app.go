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
	"guizhi-mygo-shell/viva/kit"
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
}

// Run 打开窗口并阻塞到窗口关闭；返回时进程即将退出。
func Run(opts Options) error {
	if opts.Class == "" {
		return fmt.Errorf("viva/app: Class 不能为空（WebView2 profile 必须用专用名）")
	}
	vapp.SetInterfaceFontFamily(InterfaceFont)
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
			Title: opts.Title, Class: opts.Class,
			Width: opts.Width, Height: opts.Height,
			MinWidth: opts.MinWidth, MinHeight: opts.MinHeight,
			// 自绘标题栏：内容铺满整窗，顶栏就是标题栏；窗口按钮由 Viva 画在右上角（台账 A-005）。
			// TitlebarHeight 只在创建时生效，之后改字号档不会改变按钮条高度（台账 B-010）。
			WindowMode:     vapp.WindowCustomTitlebar,
			TitlebarHeight: look.Rem(3),
			WindowControls: func() vapp.WindowControlsStyle { return shell.ControlsStyle(root.Look()) },
			ClearColor:     clearColor(look.Col(look.T.Background)),
		})
		var canvas gfx.Canvas
		if _, err := a.OpenWindow(config, &canvas); err != nil {
			return fmt.Errorf("viva/app: 打开窗口失败：%w", err)
		}
		return nil
	})
}

// clearColor 把 gfx 颜色换成窗口配置用的颜色类型。
func clearColor(c gfx.Color) vapp.Color {
	return vapp.Color{Red: float64(c.Red), Green: float64(c.Green), Blue: float64(c.Blue), Alpha: float64(c.Alpha)}
}
