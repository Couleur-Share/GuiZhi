package app

import (
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// SpecFromSettings 把外观设置装配成 kit.Spec。systemDark 是系统当前是否深色，只在明暗为「跟随系统」时用到；
// osReduceMotion 是系统是否要求减少动态效果，与「动效偏好」合并成实际动效档位。
// 壁纸（玻璃）模式：选了背景图并且开启时为真（渲染层 App.tsx 的 hasWallpaper：backgroundImageFileName 非空且 backgroundImageEnabled），
// 此时整套令牌换成半透明的玻璃色，窗口根部画整窗壁纸（Root.paintWallpaper，台账 B-020）。
func SpecFromSettings(ui settings.UI, systemDark, osReduceMotion bool) kit.Spec {
	return kit.Spec{
		Glass:      ui.BackgroundImageFileName != "" && ui.BackgroundImageEnabled,
		Mode:       tokens.Mode(ui.ThemeMode),
		SystemDark: systemDark,
		Hue:        ui.ThemeHue,
		Saturation: ui.ThemeSaturation,
		FontSize:   string(ui.FontSize),
		Motion:     kit.ResolveMotion(string(ui.MotionPreference), osReduceMotion),
	}
}
