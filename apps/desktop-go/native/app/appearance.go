package app

import (
	"fmt"
	"path/filepath"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// 界面偏好的存储接口两边各自声明（kit 不依赖 services），这里在编译期保证二者兼容。
var _ kit.PrefStore = services.Prefs(nil)

// ThemeSpecFromSettings 把外观设置装配成主题输入。壁纸文件在 imagesDir 下（设置里只存文件名）。
// 读不出壁纸（文件丢了、太大、格式不对）时，返回不带壁纸的外观和说明原因的错误：
// 调用方记日志后照常启动，不能因为一张壁纸让应用起不来。
func ThemeSpecFromSettings(ui settings.UI, imagesDir string) (kit.ThemeSpec, error) {
	spec := kit.ThemeSpec{
		Mode:       kit.Mode(ui.ThemeMode),
		Hue:        ui.ThemeHue,
		Saturation: ui.ThemeSaturation,
		FontSize:   string(ui.FontSize),
		Motion:     kit.MotionPref(ui.MotionPreference),
	}
	if !ui.BackgroundImageEnabled || ui.BackgroundImageFileName == "" {
		return spec, nil
	}
	// 文件名已由 settings 归一化过：不含路径分隔符与 ..，拼接不会越出图片目录
	wallpaper, err := kit.LoadWallpaper(filepath.Join(imagesDir, ui.BackgroundImageFileName), ui.BackgroundImageOpacity, ui.BackgroundImageBlur)
	if err != nil {
		return spec, fmt.Errorf("壁纸 %q 不可用，已忽略：%w", ui.BackgroundImageFileName, err)
	}
	spec.Wallpaper = wallpaper
	return spec, nil
}
