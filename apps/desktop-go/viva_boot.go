package main

// Viva 原生界面启动路径（-viva，长期分支 feat/viva-ui）。
//
// 与 React 版、-native 共用 main() 里到处理器登记为止的全部初始化（数据库、Library、IPC 处理器、后台服务），
// 之后不建 WebView、不绑定 Bridge、不起 MyGo 窗口，而是开 viva/app 的 Viva 窗口。
// 界面经 native/services 的窄接口取数（适配器见 native_adapters.go），空闲时进程里没有任何 WebView；
// 阅读视图、Wiki 图谱、mermaid、媒体按需开「岛」（viva/island，W1）。
//
// 切换到产品默认之前，-viva 只允许与 -bench 同用，且必须显式给 -db 与 -userdata：
// 它还不是完整的产品界面，不应在真实数据目录上运行（见 docs/agents/viva-ui-development.md §7）。

import (
	"errors"
	"flag"
	"fmt"
	"sync/atomic"

	vivaapp "guizhi-mygo-shell/viva/app"
)

// vivaWindowClass 是 Viva 窗口的类名。Viva 把 WebView2 的 profile 放在 os.UserCacheDir()\<Class>\WebView2，
// 所以必须是专用名：用 "GuiZhi" 会指向用户真实安装的 WebView2 配置。切换为产品默认时再决定正式名称。
const vivaWindowClass = "guizhi-viva"

// vivaFlags 是 -viva 的命令行开关；必须在 flag.Parse 之前声明。
type vivaFlags struct {
	on     *bool
	width  *int
	height *int
}

func declareVivaFlags() vivaFlags {
	return vivaFlags{
		on:     flag.Bool("viva", false, "Viva 原生界面（feat/viva-ui 开发中）：不加载 WebView 与渲染进程；必须与 -bench、-db、-userdata 同用"),
		width:  flag.Int("viva-width", 1280, "Viva 窗口宽度（逻辑像素）"),
		height: flag.Int("viva-height", 800, "Viva 窗口高度（逻辑像素）"),
	}
}

// enabled 报告是否以 Viva 原生界面启动。
func (f vivaFlags) enabled() bool { return f.on != nil && *f.on }

// checkLaunch 检查 -viva 的启动条件。它必须在解析数据目录、初始化数据库之前调用：
// 不带 -db 时 main() 会按正式安装的规则推断出真实的数据目录并对它做初始化（租约、迁移、升级前备份），
// 所以条件不满足时要在碰任何数据之前就退出，而不是等到 runViva 里才拒绝。
func checkVivaLaunch(nativeOn, bench, dbGiven, userDataGiven bool) error {
	switch {
	case nativeOn:
		return errors.New("-native 与 -viva 不能同时使用")
	case !bench || !dbGiven || !userDataGiven:
		return errors.New("-viva 目前只能与 -bench、-db、-userdata 同用：它还不是完整的产品界面，不应在真实数据目录上运行")
	}
	return nil
}

// checkLaunch 用命令行的实际给定情况调用 checkVivaLaunch。
func (f vivaFlags) checkLaunch(nativeOn, bench bool) error {
	return checkVivaLaunch(nativeOn, bench, flagGiven("db"), flagGiven("userdata"))
}

// flagGiven 报告命令行是否显式给了某个开关。
func flagGiven(name string) bool {
	given := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

// vivaBoot 是 runViva 需要的运行期依赖。
type vivaBoot struct {
	flags      vivaFlags
	lib        *Library
	bridge     *Bridge
	automation bool // 截图/基准实例：不占单实例锁、不建托盘、不抢系统快捷键
	bench      bool // 基准实例：首屏声明后在标准输出打印 READY，之后一直运行直到被终止
}

// runViva 打开 Viva 窗口并阻塞到退出。
func runViva(vb vivaBoot) error {
	if err := vb.flags.checkLaunch(false, vb.bench); err != nil { // main() 已检查过一次；这里再守一次，防止别的调用方绕过
		return err
	}
	var ready atomic.Bool
	if vb.bench {
		go announceReady(ready.Load)
	}
	err := vivaapp.Run(vivaapp.Options{
		Class: vivaWindowClass, Title: "归知 GuiZhi",
		Width: *vb.flags.width, Height: *vb.flags.height,
		MinWidth: minWindowWidth, MinHeight: minWindowHeight,
		OnReady: func() { ready.Store(true) },
	})
	if err != nil {
		return fmt.Errorf("Viva 窗口退出：%w", err)
	}
	return nil
}
