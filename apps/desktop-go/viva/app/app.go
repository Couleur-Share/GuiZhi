// Package app 是 Viva 原生界面的外壳：负责开窗口并承载页面。
//
// 阶段 0 只有骨架（一个空页面，用来验证 -viva 启动路径、READY 握手与空闲内存基线）；
// 窗口与自绘标题栏、rail、路由、托盘、快捷键、通知、单实例在阶段 2（M0）与阶段 3（M1）补齐。
package app

import (
	"fmt"
	"sync"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

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
}

// Run 打开窗口并阻塞到窗口关闭；返回时进程即将退出。
func Run(opts Options) error {
	if opts.Class == "" {
		return fmt.Errorf("viva/app: Class 不能为空（WebView2 profile 必须用专用名）")
	}
	var ready sync.Once
	theme := ui.LightTheme()
	page := ui.NewPage(ui.PageConfig{
		Theme: &theme,
		View: func(ui.C) {
			if opts.OnReady != nil {
				ready.Do(opts.OnReady)
			}
		},
	})
	return vapp.RunApplication(func(a *vapp.Application) error {
		config := page.Config(vapp.Config{
			Title: opts.Title, Class: opts.Class,
			Width: opts.Width, Height: opts.Height,
			MinWidth: opts.MinWidth, MinHeight: opts.MinHeight,
		})
		var canvas gfx.Canvas
		if _, err := a.OpenWindow(config, &canvas); err != nil {
			return fmt.Errorf("viva/app: 打开窗口失败：%w", err)
		}
		return nil
	})
}
