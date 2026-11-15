package main

// 原生界面启动路径（-native，长期分支 feat/native-ui）。
//
// 与 React 版共用 main() 里到处理器登记为止的全部初始化（数据库、Library、IPC 处理器、后台服务），
// 之后不再创建 WebView 与 Bridge 绑定，而是开一个只有 MyGo 原生控件的窗口：
// 界面经 native/services 的窄接口取数，适配器把这些接口接到同一批处理器上（见 native_adapters.go）。

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	nativeapp "guizhi-mygo-shell/native/app"
	"guizhi-mygo-shell/native/kit"
	_ "guizhi-mygo-shell/native/locales" // 注册原生界面的文案目录（MyGo 据此决定应用支持的语言）
)

// nativeFlags 是原生界面的命令行开关；必须在 flag.Parse 之前声明。
// 外观开关（theme/hue/font/motion/wallpaper*）只供开发与截图：真实运行的外观来自设置（settings-go 完成后）。
type nativeFlags struct {
	on        *bool
	module    *string
	theme     *string
	hue       *string
	font      *string
	motion    *string
	wallpaper *string
	wpOpacity *float64
	wpBlur    *float64
	width     *int
	height    *int
	demo      *bool
}

func declareNativeFlags() nativeFlags {
	return nativeFlags{
		on:        flag.Bool("native", false, "原生界面（feat/native-ui 开发中）：用 MyGo 原生控件，不加载 WebView 与渲染进程"),
		module:    flag.String("native-module", "imports", "原生界面的起始页：library|inbox|ask|wiki|imports|settings"),
		theme:     flag.String("native-theme", "", "原生界面的明暗：system|light|dark；缺省时自动化实例用 light（截图可复现），否则跟随系统"),
		hue:       flag.String("native-hue", "", "原生界面的主题色：预设 ID（royal-blue、blue、purple、green、orange、teal）或「色相,饱和度」；缺省为默认预设"),
		font:      flag.String("native-font", "", "原生界面的字号档：small|medium|large；缺省为默认档"),
		motion:    flag.String("native-motion", "", "原生界面的动效偏好：system|off|reduced|standard；缺省为跟随系统"),
		wallpaper: flag.String("native-wallpaper", "", "原生界面的壁纸：图片文件路径，或 demo（内置演示壁纸）；缺省为无壁纸"),
		wpOpacity: flag.Float64("native-wallpaper-opacity", 1, "壁纸不透明度 0–1"),
		wpBlur:    flag.Float64("native-wallpaper-blur", 0, "壁纸模糊 0–50（同 CSS px）"),
		width:     flag.Int("native-width", 1280, "原生窗口宽度（DIP）"),
		height:    flag.Int("native-height", 800, "原生窗口高度（DIP）"),
		demo:      flag.Bool("native-demo", false, "往库里写入一份演示数据（仅限 -shot/-bench 的自动化实例，写隔离库）"),
	}
}

// enabled 报告是否以原生界面启动。
func (f nativeFlags) enabled() bool { return f.on != nil && *f.on }

// themeSpec 把外观开关装配成 kit.ThemeSpec；值写错时返回带可选项的错误，而不是静默落回默认。
func (f nativeFlags) themeSpec(automation bool) (kit.ThemeSpec, error) {
	spec := kit.DefaultThemeSpec()
	modeArg := *f.theme
	if modeArg == "" {
		modeArg = string(kit.ModeSystem)
		if automation {
			modeArg = string(kit.ModeLight)
		}
	}
	var err error
	if spec.Mode, err = kit.ParseMode(modeArg); err != nil {
		return spec, err
	}
	if *f.hue != "" {
		if spec.Hue, spec.Saturation, err = kit.ParseHue(*f.hue); err != nil {
			return spec, err
		}
	}
	if *f.font != "" {
		if spec.FontSize, err = kit.ParseFontSize(*f.font); err != nil {
			return spec, err
		}
	}
	if *f.motion != "" {
		if spec.Motion, err = kit.ParseMotion(*f.motion); err != nil {
			return spec, err
		}
	}
	switch wp := *f.wallpaper; wp {
	case "":
	case "demo":
		spec.Wallpaper = kit.DemoWallpaper(*f.width, *f.height, *f.wpOpacity, *f.wpBlur)
	default:
		if spec.Wallpaper, err = kit.LoadWallpaper(wp, *f.wpOpacity, *f.wpBlur); err != nil {
			return spec, err
		}
	}
	return spec, nil
}

// nativeHost 把窗口与平台能力接给外壳（native/app.Host）。
type nativeHost struct{ win *mygo.Window }

func (h nativeHost) Invalidate()      { h.win.Invalidate() }
func (h nativeHost) SystemDark() bool { return mygo.Theme.IsDark() }

// ApplyAppearance 让系统绘制的窗口按钮跟随应用外观；system 表示跟随系统。
func (h nativeHost) ApplyAppearance(mode kit.Mode) { mygo.Theme.SetSource(mygo.ThemeSource(mode)) }

// nativeBoot 是 runNative 需要的运行期依赖。
type nativeBoot struct {
	flags      nativeFlags
	lib        *Library
	bridge     *Bridge
	automation bool          // 截图/基准实例：窗口放屏幕外、不抢焦点、不碰用户状态
	bench      bool          // 基准实例：首屏有内容后在标准输出打印 READY，之后一直运行直到被终止
	shot       string        // 非空：启动后截图到该文件并退出
	wait       time.Duration // 截图前等待
}

var nativeRoutes = map[string]nativeapp.Route{
	"library": nativeapp.RouteLibrary, "inbox": nativeapp.RouteInbox, "ask": nativeapp.RouteAsk,
	"wiki": nativeapp.RouteWiki, "imports": nativeapp.RouteImports, "settings": nativeapp.RouteSettings,
}

// runNative 打开原生窗口并阻塞到退出。
func runNative(nb nativeBoot) error {
	route, ok := nativeRoutes[*nb.flags.module]
	if !ok {
		return fmt.Errorf("未知的 -native-module %q", *nb.flags.module)
	}
	if *nb.flags.demo {
		// 演示数据会写库：只允许自动化实例（它的库由调用方保证是隔离副本）
		if !nb.automation {
			return errors.New("-native-demo 只能与 -shot 或 -bench 同用：它会往库里写演示数据")
		}
		if err := seedNativeDemo(context.Background(), nb.lib); err != nil {
			return fmt.Errorf("写入演示数据失败：%w", err)
		}
	}

	spec, err := nb.flags.themeSpec(nb.automation)
	if err != nil {
		return err
	}

	hub := newPushHub()
	nb.lib.setPush(hub.Dispatch) // 单监听推送 → 订阅者
	app := nativeapp.New(nativeapp.Options{
		Services: newNativeServices(nb.bridge, hub),
		Theme:    spec,
		Route:    route,
	})

	mygo.App.WhenReady(func() {
		// 窗口创建前先定下外观来源，系统按钮第一帧就是对的颜色，不先闪一下系统外观
		mygo.Theme.SetSource(mygo.ThemeSource(spec.Mode))
		opts := mygo.WindowOptions{
			Title: "归知 GuiZhi", Width: *nb.flags.width, Height: *nb.flags.height,
			MinWidth: minWindowWidth, MinHeight: minWindowHeight,
			Content: ui.View(app.View),
			// 隐藏系统标题栏但保留系统窗口控制按钮（贴靠布局、阴影、圆角、缩放边都保留）；
			// 标题栏高度与顶栏一致，按钮正好占满顶栏右端。
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 48,
		}
		if nb.automation {
			opts.X, opts.Y = benchX(true), benchX(true) // 屏幕外 + 不进任务栏，不打扰用户桌面
			opts.SkipTaskbar = true
		}
		win := mygo.NewWindow(opts)
		app.AttachHost(nativeHost{win})
		mygo.Theme.OnUpdated(win.Invalidate) // 系统明暗、高对比度、文字缩放变了：重新装配外观
		app.Start()
		if nb.automation {
			win.ShowInactive() // 不抢焦点；不能用 Hidden：隐藏窗口的合成器停摆，截不到新帧
		} else {
			win.Show()
		}
		if nb.bench {
			go announceReady(app.Ready)
		}
		if nb.shot != "" {
			go captureAndQuit(win, nb.shot, nb.wait)
		}
	})
	err = mygo.App.Run()
	app.Close()
	return err
}

// announceReady 在起始页面首次加载有结果后打印 READY（与 React 版的就绪判据同义：首屏已有真实内容）。
// 数据到达会触发重绘，多等一小段让那一帧画完，再让测量脚本开始计时。
func announceReady(ready func() bool) {
	for !ready() {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	fmt.Println("READY")
}

// captureAndQuit 等待 wait 后把窗口截图写入 file，然后退出应用。
func captureAndQuit(win *mygo.Window, file string, wait time.Duration) {
	defer mygo.App.Quit()
	time.Sleep(wait)
	png, err := win.CapturePage()
	if err == nil {
		err = os.WriteFile(file, png, 0o644)
	}
	if err != nil {
		fmt.Println("[FAIL] 截图：", err)
		log.Printf("[native] 截图失败：%v", err)
		return
	}
	fmt.Printf("[ OK ] 截图 %s（%d 字节）\n", file, len(png))
}
