package shell

import (
	"github.com/vivaui/viva/app"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// ControlsStyle 是真窗口里 Viva 自己画的最小化/最大化/关闭三枚按钮的外观（app.Config.WindowControls 每次绘制都会读它，
// 所以主题与语言变了按钮跟着变）：颜色按渲染层顶栏的写法——图标 text-foreground/70，悬停 bg-muted，关闭悬停 red-500 配白色图标；
// 名字用当前界面语言。三枚按钮的宽度与字形 Viva 不开放配置，差异登记在台账 A-005。
func ControlsStyle(l *kit.Look) app.WindowControlsStyle {
	t := l.T
	white := tokens.RGBA(255, 255, 255, 1)
	return app.WindowControlsStyle{
		Foreground: l.Col(t.Foreground.WithAlpha(0.7)),
		Hover:      l.Col(t.Muted),
		Pressed:    l.Col(t.Muted),
		Close:      l.Col(red500),
		// 关闭按钮悬停时的图标是白色。
		CloseForeground: l.Col(white),
		Minimize:        locales.CommonMinimize(),
		Maximize:        locales.CommonMaximize(),
		Restore:         locales.CommonRestore(),
		CloseName:       locales.CommonClose(),
	}
}
