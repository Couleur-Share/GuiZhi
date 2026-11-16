package app

import (
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// SpecFromSettings 把外观设置装配成 kit.Spec。systemDark 是系统当前是否深色，只在明暗为「跟随系统」时用到。
// 壁纸（玻璃）模式随显示设置页一起迁移，目前恒为关。
func SpecFromSettings(ui settings.UI, systemDark bool) kit.Spec {
	return kit.Spec{
		Mode:       tokens.Mode(ui.ThemeMode),
		SystemDark: systemDark,
		Hue:        ui.ThemeHue,
		Saturation: ui.ThemeSaturation,
		FontSize:   string(ui.FontSize),
	}
}
