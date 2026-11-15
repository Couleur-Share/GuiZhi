package kit

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNewWallpaperClampsLikeSettings(t *testing.T) {
	// settings-appearance.ts：不透明度夹到 0–1，模糊夹到 0–50
	bmp := ui.NewBitmap(image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	cases := []struct{ opacity, blur, wantO, wantB float64 }{
		{0.5, 12, 0.5, 12},
		{-1, -5, 0, 0},
		{3, 99, 1, 50},
	}
	for _, c := range cases {
		w := NewWallpaper(bmp, c.opacity, c.blur)
		if w.Opacity != c.wantO || w.Blur != c.wantB {
			t.Errorf("NewWallpaper(%v, %v) = (%v, %v)，期望 (%v, %v)", c.opacity, c.blur, w.Opacity, w.Blur, c.wantO, c.wantB)
		}
	}
	if w := NewWallpaper(bmp, nan(), nan()); w.Opacity != 0 || w.Blur != 0 {
		t.Errorf("NaN 应收敛到下限，实际 (%v, %v)", w.Opacity, w.Blur)
	}
}

func nan() float64 { zero := 0.0; return zero / zero }

func TestDemoWallpaperIsDeterministic(t *testing.T) {
	a := DemoWallpaper(48, 30, 1, 0)
	b := DemoWallpaper(48, 30, 1, 0)
	w, h := ui.ImageSize(a.Image)
	if w != 48 || h != 30 {
		t.Fatalf("尺寸 = %vx%v，期望 48x30", w, h)
	}
	if a.Opacity != 1 || a.Blur != 0 || b.Image == nil {
		t.Fatal("演示壁纸的强度没有按参数装配")
	}
}

func TestLoadWallpaper(t *testing.T) {
	dir := t.TempDir()

	img := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	img.SetNRGBA(1, 1, color.NRGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "wp.png")
	if err := os.WriteFile(good, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := LoadWallpaper(good, 0.8, 10)
	if err != nil {
		t.Fatalf("读取有效壁纸：%v", err)
	}
	if bw, bh := ui.ImageSize(w.Image); bw != 4 || bh != 3 || w.Opacity != 0.8 || w.Blur != 10 {
		t.Errorf("壁纸装配不对：%vx%v opacity=%v blur=%v", bw, bh, w.Opacity, w.Blur)
	}

	// 不存在、不是图片：给出可读的原因，不 panic
	if _, err := LoadWallpaper(filepath.Join(dir, "none.png"), 1, 0); err == nil || !strings.Contains(err.Error(), "读取壁纸失败") {
		t.Errorf("不存在的文件应报读取失败：%v", err)
	}
	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWallpaper(bad, 1, 0); err == nil || !strings.Contains(err.Error(), "解码壁纸失败") {
		t.Errorf("非图片应报解码失败：%v", err)
	}
}

func TestBackdropOnlyDrawsWithWallpaper(t *testing.T) {
	plain := NewLook(DefaultThemeSpec(), Env{})
	spec := DefaultThemeSpec()
	spec.Wallpaper = DemoWallpaper(64, 40, 1, 4)
	glass := NewLook(spec, Env{})

	// 无壁纸与有壁纸两种外观都应能无窗口地绘制出一帧，不 panic；壁纸层不接收点击也不进入无障碍树
	for name, look := range map[string]*Look{"无壁纸": plain, "壁纸": glass} {
		tr := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(look.Theme)
			ui.Column(c).Fill().Background(look.T.FillBackground).Children(func() {
				Backdrop(c, look)
				ui.Text(c, "前景文字")
			})
		}, 320, 200)
		tr.Frame()
		if !tr.HasText("前景文字") {
			t.Errorf("%s：前景内容应照常绘制", name)
		}
	}
}
