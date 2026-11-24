package scenes

import (
	"fmt"

	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// 显示设置「背景图」三个整窗场景（M3-6c，台账 B-020），与 React 采集器（scripts/parity/scenes/react-wallpaper-scenes.mjs、
// lib/react-wallpaper.mjs）一一对应：同一张合成壁纸（viva/parity/testdata/wallpaper-fixture.png）、同样的设置、同样滚到内容底。
//
//   - wallpaper-disabled：已选图、未启用（可见度 0.7、虚化 0）：整窗是普通不透明模式，只有预览舞台展示壁纸模式的缩微外壳；
//   - wallpaper-enabled：已启用（可见度 0.35、虚化 0）：整窗壁纸模式，外观（Look.Glass）换成半透明的玻璃令牌；
//   - wallpaper-blurred：已启用并虚化（可见度 0.55、虚化 14px）。
//
// 整窗壁纸的绘制与 viva/app.Root.paintWallpaper 同一套（Window.Wallpaper + kit.WallpaperLayer）；这里没有外壳 Root，所以由场景自己
// 持缓存并装配。读图从编译进来的夹具字节返回，读取与渲染在后台 goroutine，由 waitCond 逐帧等到位图落地后再采集。

// wallpaperSettings 是一个壁纸场景的背景图设置。
type wallpaperSettings struct {
	enabled bool
	opacity float64
	blur    float64
}

// wallpaperLoad 是读图回调：只认夹具文件名，别的名字如实报错（假错误全英文）。
func wallpaperLoad(fileName string) ([]byte, error) {
	if fileName != parity.WallpaperFixtureName {
		return nil, fmt.Errorf("wallpaper fixture: unknown file %q", fileName)
	}
	return parity.WallpaperFixture(), nil
}

// wallpaperScene 是整窗（顶栏 + rail + 设置页「显示设置」）滚到内容底的壁纸场景。
// 每次渲染各建一份设置、窗口与位图缓存，不跨渲染共享；缓存随渲染结束由 GC 回收（后台任务只持有缓存本身）。
func wallpaperScene(id string, ws wallpaperSettings, waivers ...parity.Waiver) parity.Scene {
	srcW, srcH := parity.WallpaperFixtureSize()
	srcBytes := srcW * srcH * 4 // 解码后的源位图字节数：缓存里多出渲染结果（Retained 超过它）才算位图渲染完成
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		v := appearanceValues(look)
		v.UI.BackgroundImageFileName = parity.WallpaperFixtureName
		v.UI.BackgroundImageEnabled = ws.enabled
		v.UI.BackgroundImageOpacity = ws.opacity
		v.UI.BackgroundImageBlur = ws.blur
		shell := &settingsscreen.Shell{
			Look: look, Store: settingsscreen.NewMemStore(v), Active: settingsscreen.SectionAppearance, LoadBackground: wallpaperLoad,
		}
		window := &shellscreen.Window{
			Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F", Settings: shell,
		}
		cache := kit.NewWallpaperCache(wallpaperLoad, nil)
		window.Wallpaper = func(d *ui.DrawContext) {
			if !look.Glass {
				cache.Bitmap("", 0, 0, 0, 0) // 与 Root.paintWallpaper 相同：壁纸没有启用时清缓存、什么也不画
				return
			}
			look.PaintWallpaperLayer(d, kit.WallpaperLayer{
				Cache: cache, File: parity.WallpaperFixtureName, Opacity: settings.ClampOpacity(ws.opacity), Blur: settings.ClampBlur(ws.blur), Tokens: look.T,
			})
		}
		steps := func(s *headless.Session) error {
			// 先滚到底：预览舞台在内容底部，画出来（在视口里）之后才会读图。
			if err := scrollToEnd(s); err != nil {
				return err
			}
			if err := waitCond(s, "显示设置预览舞台的壁纸渲染完成", func() bool { return shell.PreviewRetained() > srcBytes }); err != nil {
				return err
			}
			if ws.enabled {
				if err := waitCond(s, "整窗壁纸渲染完成", func() bool { return cache.Retained() > srcBytes }); err != nil {
					return err
				}
			}
			return settleData(s)
		}
		return window.View, steps
	}
	return parity.Scene{
		ID: id,
		Spec: func(base kit.Spec) kit.Spec {
			base.Glass = ws.enabled
			return base
		},
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: append(append([]parity.Waiver{}, pageSettingsWaivers...), waivers...),
	}
}

func init() {
	registry["settings.appearance.wallpaper-disabled"] = wallpaperScene("settings.appearance.wallpaper-disabled",
		wallpaperSettings{enabled: false, opacity: 0.7, blur: 0}, scrolledBottomWaivers...)
	registry["settings.appearance.wallpaper-enabled"] = wallpaperScene("settings.appearance.wallpaper-enabled",
		wallpaperSettings{enabled: true, opacity: 0.35, blur: 0}, scrolledBottomWaivers...)
	registry["settings.appearance.wallpaper-blurred"] = wallpaperScene("settings.appearance.wallpaper-blurred",
		wallpaperSettings{enabled: true, opacity: 0.55, blur: 14}, scrolledBottomWaivers...)
}
