package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// wallpaperPNG 是 64×36 的纯色 PNG（16:9，与预览舞台同比例）。
func wallpaperPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 36))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// setBackground 改设置里的背景图字段（经假设置服务，Root 的 store 订阅到后重绘）。
func (f *rootFixture) setBackground(t *testing.T, name string, enabled bool) {
	t.Helper()
	if _, err := f.fake.Update(func(v *settings.Values) {
		v.UI.BackgroundImageFileName, v.UI.BackgroundImageEnabled = name, enabled
	}); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
}

// wholeWindowImages 是当前帧里铺满整个窗口的位图数。
func (f *rootFixture) wholeWindowImages(t *testing.T) (all, whole int, backdrop bool) {
	t.Helper()
	for _, img := range f.root.canvas.Images() {
		all++
		if img.Bounds.X == 0 && img.Bounds.Y == 0 && img.Bounds.Width == 1280 && img.Bounds.Height == 800 {
			whole++
		}
		backdrop = backdrop || img.Backdrop
	}
	return all, whole, backdrop
}

// SpecFromSettings.Glass：选了图并启用才进玻璃；只选图不启用、启用但没图都不进。
func TestSpecFromSettingsGlass(t *testing.T) {
	cases := []struct {
		file    string
		enabled bool
		want    bool
	}{
		{"", false, false},
		{"", true, false},
		{"wall.png", false, false},
		{"wall.png", true, true},
	}
	for _, c := range cases {
		ui := settings.Default().UI
		ui.BackgroundImageFileName, ui.BackgroundImageEnabled = c.file, c.enabled
		if got := SpecFromSettings(ui, false, false).Glass; got != c.want {
			t.Errorf("file=%q enabled=%v：Glass=%v，期望 %v", c.file, c.enabled, got, c.want)
		}
	}
}

// 整窗壁纸：启用后进玻璃，铺满窗口的位图画出（走当前层，不是 DrawBackdrop）；关闭后退出玻璃并释放缓存。
func TestRootWholeWindowWallpaperFollowsEnabledFlag(t *testing.T) {
	bg := &servicestest.BackgroundImagesFake{}
	bg.Put("wall.png", wallpaperPNG(t, color.NRGBA{B: 255, A: 255}))
	f := openRoot(t, func(o *Options) { o.Services.BackgroundImages = bg })
	if f.root.lookSpec.Glass {
		t.Fatal("默认没有背景图，不应进玻璃")
	}
	if all, _, _ := f.wholeWindowImages(t); all != 0 || f.root.wallpaper.Retained() != 0 {
		t.Fatalf("没有背景图时不应有位图：images=%d retained=%d", all, f.root.wallpaper.Retained())
	}

	f.setBackground(t, "wall.png", true)
	if !f.root.lookSpec.Glass {
		t.Fatal("选图并启用后应进玻璃")
	}
	waitUntil(t, "整窗壁纸位图画出", func() bool {
		f.frames(t, 1)
		_, whole, _ := f.wholeWindowImages(t)
		return whole == 1
	})
	if _, _, backdrop := f.wholeWindowImages(t); backdrop {
		t.Error("整窗壁纸应画在当前层，不能走 DrawBackdrop")
	}
	if bg.Reads() != 1 || f.root.wallpaper.Retained() == 0 {
		t.Errorf("应读一次图并持有位图：reads=%d retained=%d", bg.Reads(), f.root.wallpaper.Retained())
	}

	f.setBackground(t, "wall.png", false) // 保留文件、关闭启用：退出玻璃并释放
	if f.root.lookSpec.Glass {
		t.Error("关闭启用后应退出玻璃")
	}
	if all, _, _ := f.wholeWindowImages(t); all != 0 || f.root.wallpaper.Retained() != 0 {
		t.Errorf("关闭后位图应释放：images=%d retained=%d", all, f.root.wallpaper.Retained())
	}

	f.setBackground(t, "wall.png", true) // 再启用：重新读取并画出
	waitUntil(t, "再次启用后位图画出", func() bool {
		f.frames(t, 1)
		_, whole, _ := f.wholeWindowImages(t)
		return whole == 1
	})
	if bg.Reads() != 2 {
		t.Errorf("再次启用应重新读图：reads=%d", bg.Reads())
	}
}

// 读图失败：整窗壁纸照 LocalImage 画损坏的回退（不悄悄什么都不显示），不会有位图。
func TestRootWallpaperReadFailureDrawsFallback(t *testing.T) {
	bg := &servicestest.BackgroundImagesFake{}
	bg.FailRead(errors.New("读取背景图失败"))
	f := openRoot(t, func(o *Options) { o.Services.BackgroundImages = bg })
	f.setBackground(t, "wall.png", true)
	waitUntil(t, "读取失败", func() bool { f.frames(t, 1); return f.root.wallpaper.Failed() })
	f.frames(t, 2)
	if all, _, _ := f.wholeWindowImages(t); all != 0 {
		t.Errorf("读取失败时不应有位图：%d", all)
	}
	if !f.root.lookSpec.Glass {
		t.Error("读取失败不改变玻璃状态（设置仍是启用）")
	}
}

// 没有装配 BackgroundImages：选图如实不可用，读图缺省；壁纸启用时按失败画回退，不 panic。
func TestRootWithoutBackgroundImagesService(t *testing.T) {
	f := openRoot(t, nil)
	if f.root.settings.PickBackground != nil || f.root.settings.LoadBackground != nil {
		t.Fatal("没有装配服务时不应接选图与读图")
	}
	f.setBackground(t, "wall.png", true)
	waitUntil(t, "无服务时按失败处理", func() bool { f.frames(t, 1); return f.root.wallpaper.Failed() })
}

// 选图与读图接到服务：成功给出文件名，取消给空串，失败原样返回错误；读图读到的是服务里的字节。
func TestRootBackgroundPickAndLoadWiring(t *testing.T) {
	bg := &servicestest.BackgroundImagesFake{}
	data := wallpaperPNG(t, color.NRGBA{R: 255, A: 255})
	bg.Put("picked.png", data)
	bg.PickFunc = func(context.Context) (string, error) { return "picked.png", nil }
	f := openRoot(t, func(o *Options) { o.Services.BackgroundImages = bg })

	name, err := f.root.settings.PickBackground()
	if err != nil || name != "picked.png" || bg.Picks() != 1 {
		t.Fatalf("选图成功：name=%q err=%v picks=%d", name, err, bg.Picks())
	}
	got, err := f.root.settings.LoadBackground(name)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("读图应返回服务里的字节：len=%d err=%v", len(got), err)
	}

	bg.PickFunc = func(context.Context) (string, error) { return "", nil }
	if name, err := f.root.settings.PickBackground(); name != "" || err != nil {
		t.Errorf("取消选图应是空文件名且无错误：%q %v", name, err)
	}
	boom := errors.New("复制背景图失败")
	bg.FailPick(boom)
	if _, err := f.root.settings.PickBackground(); !errors.Is(err, boom) {
		t.Errorf("选图失败应原样返回错误：%v", err)
	}
}

// 关窗口释放整窗壁纸与设置页预览持有的位图。
func TestRootCloseReleasesWallpaper(t *testing.T) {
	bg := &servicestest.BackgroundImagesFake{}
	bg.Put("wall.png", wallpaperPNG(t, color.NRGBA{G: 255, A: 255}))
	f := openRoot(t, func(o *Options) { o.Services.BackgroundImages = bg })
	f.setBackground(t, "wall.png", true)
	waitUntil(t, "壁纸读完", func() bool { f.frames(t, 1); return f.root.wallpaper.Retained() > 0 })
	f.root.closeWallpaper()
	if f.root.wallpaper.Retained() != 0 {
		t.Errorf("关闭后应释放：%d", f.root.wallpaper.Retained())
	}
}

// 预览位图随分区与页面走：在显示设置分区里持有，切到别的分区、回到首页就释放，回来再重新读取。
func TestRootAppearancePreviewReleasedOnLeave(t *testing.T) {
	bg := &servicestest.BackgroundImagesFake{}
	bg.Put("wall.png", wallpaperPNG(t, color.NRGBA{R: 255, A: 255}))
	f := openRoot(t, func(o *Options) { o.Services.BackgroundImages = bg })
	f.root.settings.Active = sscreen.SectionAppearance
	f.setBackground(t, "wall.png", true)
	enter := func(what string) {
		t.Helper()
		f.root.settings.Active = sscreen.SectionAppearance
		f.frames(t, 3)
		if err := f.session.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: 4000, Precise: true}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, what, func() bool { f.frames(t, 1); return f.root.settings.PreviewRetained() > 0 })
	}
	enter("进入显示设置后预览读完")
	readsInSection := bg.Reads()

	f.root.settings.Active = sscreen.SectionGeneral
	f.frames(t, 3)
	if got := f.root.settings.PreviewRetained(); got != 0 {
		t.Fatalf("切到别的分区应释放预览：%d", got)
	}
	enter("回到显示设置后重新读取")
	if bg.Reads() <= readsInSection {
		t.Errorf("回来应重新读图：reads %d → %d", readsInSection, bg.Reads())
	}

	f.root.Window().Page = shell.PageHome // 离开设置页：窗口释放预览
	f.frames(t, 3)
	if got := f.root.settings.PreviewRetained(); got != 0 {
		t.Errorf("离开设置页应释放预览：%d", got)
	}
}

// 关窗口时设置页预览持有的位图一并释放：窗口仍停在设置页的显示设置分区，不靠下一帧的离开逻辑。
func TestRootCloseReleasesAppearancePreview(t *testing.T) {
	bg := &servicestest.BackgroundImagesFake{}
	bg.Put("wall.png", wallpaperPNG(t, color.NRGBA{R: 255, A: 255}))
	f := openRoot(t, func(o *Options) { o.Services.BackgroundImages = bg })
	f.root.settings.Active = sscreen.SectionAppearance
	f.setBackground(t, "wall.png", true)
	f.frames(t, 3)
	if err := f.session.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: 4000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "预览读完", func() bool { f.frames(t, 1); return f.root.settings.PreviewRetained() > 0 })
	f.root.closeWallpaper()
	if got := f.root.settings.PreviewRetained(); got != 0 {
		t.Errorf("关窗口应释放设置页预览：%d", got)
	}
}
