package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/settings"
)

func TestThemeSpecFromDefaultSettingsIsTheDefaultSpec(t *testing.T) {
	got, err := ThemeSpecFromSettings(settings.DefaultUI(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != kit.DefaultThemeSpec() {
		t.Fatalf("出厂设置应装配出出厂外观：\n得到 %+v\n期望 %+v", got, kit.DefaultThemeSpec())
	}
}

func TestThemeSpecFromSettingsMapsEveryAppearanceField(t *testing.T) {
	ui := settings.DefaultUI()
	ui.ThemeMode, ui.ThemeHue, ui.ThemeSaturation = settings.ThemeDark, 150, 30
	ui.FontSize, ui.MotionPreference = settings.FontLarge, settings.MotionReduced
	got, err := ThemeSpecFromSettings(ui, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := kit.ThemeSpec{Mode: kit.ModeDark, Hue: 150, Saturation: 30, FontSize: "large", Motion: kit.MotionReduced}
	if got != want {
		t.Fatalf("得到 %+v，期望 %+v", got, want)
	}
	// 设置里的取值集合必须都是 kit 认得的：kit.Parse* 与设置的枚举逐项对得上
	for _, m := range []settings.ThemeMode{settings.ThemeLight, settings.ThemeDark, settings.ThemeSystem} {
		if _, err := kit.ParseMode(string(m)); err != nil {
			t.Errorf("明暗 %q：%v", m, err)
		}
	}
	for _, m := range []settings.MotionPreference{settings.MotionSystem, settings.MotionOff, settings.MotionReduced, settings.MotionStandard} {
		if _, err := kit.ParseMotion(string(m)); err != nil {
			t.Errorf("动效 %q：%v", m, err)
		}
	}
	for _, f := range []settings.FontSize{settings.FontSmall, settings.FontMedium, settings.FontLarge} {
		if _, err := kit.ParseFontSize(string(f)); err != nil {
			t.Errorf("字号档 %q：%v", f, err)
		}
	}
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.NRGBA{R: 200, G: 40, B: 40, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestThemeSpecFromSettingsWallpaper(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "wall.png"))
	ui := settings.DefaultUI()
	ui.BackgroundImageFileName, ui.BackgroundImageOpacity, ui.BackgroundImageBlur = "wall.png", 0.4, 12

	// 出厂默认 backgroundImageEnabled=true，只是没有文件名（没选壁纸）
	ui.BackgroundImageFileName = ""
	if got, err := ThemeSpecFromSettings(ui, dir); err != nil || got.Wallpaper != nil {
		t.Fatalf("没选壁纸文件时不该有壁纸：%v %v", got.Wallpaper, err)
	}
	// 关闭开关：有文件名也不读
	ui.BackgroundImageFileName, ui.BackgroundImageEnabled = "wall.png", false
	if got, err := ThemeSpecFromSettings(ui, dir); err != nil || got.Wallpaper != nil {
		t.Fatalf("关闭壁纸时不该有壁纸：%v %v", got.Wallpaper, err)
	}

	ui.BackgroundImageEnabled = true
	got, err := ThemeSpecFromSettings(ui, dir)
	if err != nil || got.Wallpaper == nil {
		t.Fatalf("应读到壁纸：%v", err)
	}
	if got.Wallpaper.Opacity != 0.4 || got.Wallpaper.Blur != 12 {
		t.Errorf("壁纸强度不对：%+v", got.Wallpaper)
	}

	// 文件丢了：外观照常装配，只是没有壁纸，并说明原因（调用方记日志）
	ui.BackgroundImageFileName = "gone.png"
	got, err = ThemeSpecFromSettings(ui, dir)
	if err == nil || !strings.Contains(err.Error(), "gone.png") || got.Wallpaper != nil {
		t.Fatalf("壁纸文件丢了应得到无壁纸的外观和错误：%v %v", got.Wallpaper, err)
	}
	if got.Mode != kit.ModeSystem || got.FontSize != "medium" {
		t.Errorf("壁纸失败不该影响其他外观项：%+v", got)
	}

	// 文件内容不是图片
	if err := os.WriteFile(filepath.Join(dir, "bad.png"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	ui.BackgroundImageFileName = "bad.png"
	if got, err = ThemeSpecFromSettings(ui, dir); err == nil || got.Wallpaper != nil {
		t.Fatalf("坏图片应报错且无壁纸：%v %v", got.Wallpaper, err)
	}
}
