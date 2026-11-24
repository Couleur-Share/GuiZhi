package kit

import (
	"errors"
	"image/color"
	"testing"

	"github.com/vivaui/viva/ui"
)

// layerPage 在 200×100 的页里画一层壁纸：整页一行，Draw 里调 PaintWallpaperLayer。file 与 opacity 由调用方每帧改。
type layerPage struct {
	h       *harness
	cache   *WallpaperCache
	file    string
	opacity float64
	blur    float64
}

func openLayerPage(t *testing.T, loader *wallpaperLoader) *layerPage {
	t.Helper()
	p := &layerPage{opacity: 1}
	p.cache, _ = newTestCache(loader)
	t.Cleanup(p.cache.Close)
	p.h = openPage(t, still(), 200, 100, func(l *Look, c ui.C) {
		ui.Row(c).Fill().Draw(func(d *ui.DrawContext) {
			l.PaintWallpaperLayer(d, WallpaperLayer{Cache: p.cache, File: p.file, Opacity: p.opacity, Blur: p.blur, Tokens: l.T})
		})
	})
	return p
}

// pixel 取窗口截图里 (x, y) 的颜色（缩放 1）。
func (p *layerPage) pixel(x, y int) color.RGBA {
	p.h.t.Helper()
	img, err := p.h.session.Image()
	if err != nil {
		p.h.t.Fatal(err)
	}
	return img.RGBAAt(x, y)
}

// 壁纸一层：读到位图后铺满（不透明度进像素）；读不出来画 LocalImage 的损坏回退（muted 30% 的底）；没有文件什么也不画并释放缓存。
func TestPaintWallpaperLayerBitmapFallbackAndRelease(t *testing.T) {
	loader := &wallpaperLoader{
		files: map[string][]byte{"red.png": encodePNG(t, solidNRGBA(64, 36, color.NRGBA{R: 255, A: 255}))},
		errs:  map[string]error{"gone.png": errors.New("open gone.png: file not found")},
	}
	p := openLayerPage(t, loader)
	bg := p.h.look.T.Background
	bgPixel := color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: 255}
	if got := p.pixel(100, 50); !byteNear(got.R, bgPixel.R, 2) || !byteNear(got.G, bgPixel.G, 2) {
		t.Fatalf("没有文件时不画任何东西，应是页面底色 %v，得到 %v", bgPixel, got)
	}
	if p.cache.Retained() != 0 {
		t.Fatalf("没有文件时缓存不应占内存：%d", p.cache.Retained())
	}

	p.file = "red.png"
	waitFor(t, "位图读完并画出", func() bool {
		p.h.frames(1)
		got := p.pixel(100, 50)
		return got.R > 240 && got.G < 20 && got.B < 20
	})
	if p.cache.Retained() == 0 {
		t.Error("读到位图后缓存应占有源图")
	}

	p.opacity = 0.5 // 不透明度预乘进位图：半透明红叠在页面底色上
	waitFor(t, "半透明位图画出", func() bool {
		p.h.frames(1)
		got := p.pixel(100, 50)
		return got.R > 200 && got.G > 90 && got.G < 200
	})

	p.file, p.opacity = "gone.png", 1
	waitFor(t, "读不出来时的回退", func() bool { p.h.frames(1); return p.cache.Failed() })
	p.h.frames(2)
	tk := p.h.look.T
	want := tk.Muted.WithAlpha(0.3).Over(tk.Background)
	if got := p.pixel(4, 4); !byteNear(got.R, want.R, 2) || !byteNear(got.G, want.G, 2) || !byteNear(got.B, want.B, 2) {
		t.Errorf("回退底应是 muted 30%% 叠在页面底色上 %+v，得到 %v", want, got)
	}
	if p.cache.Retained() != 0 {
		t.Errorf("读取失败时缓存不应占内存：%d", p.cache.Retained())
	}

	p.file = "" // 关掉壁纸：缓存清空，页面回到底色
	p.h.frames(2)
	if got := p.pixel(100, 50); !byteNear(got.R, bgPixel.R, 2) || !byteNear(got.G, bgPixel.G, 2) {
		t.Errorf("清空文件名后应回到页面底色 %v，得到 %v", bgPixel, got)
	}
	if p.cache.Failed() || p.cache.Retained() != 0 {
		t.Errorf("清空文件名后缓存应复位：Failed=%v Retained=%d", p.cache.Failed(), p.cache.Retained())
	}
}

// 回退层的颜色与图标按容器不透明度整体淡出（LocalImage 的回退在 opacity 容器里）。
func TestPaintWallpaperLayerFallbackFollowsOpacity(t *testing.T) {
	loader := &wallpaperLoader{errs: map[string]error{"gone.png": errors.New("boom")}}
	p := openLayerPage(t, loader)
	p.file, p.opacity = "gone.png", 0.5
	waitFor(t, "回退", func() bool { p.h.frames(1); return p.cache.Failed() })
	p.h.frames(2)
	tk := p.h.look.T
	want := tk.Muted.WithAlpha(0.15).Over(tk.Background) // 30% × 50%
	if got := p.pixel(4, 4); !byteNear(got.R, want.R, 2) || !byteNear(got.G, want.G, 2) || !byteNear(got.B, want.B, 2) {
		t.Errorf("不透明度 0.5 时回退底应是 muted 15%%：%+v，得到 %v", want, got)
	}
}

// 没有缓存时什么也不画，也不 panic。
func TestPaintWallpaperLayerWithoutCache(t *testing.T) {
	h := openPage(t, still(), 80, 40, func(l *Look, c ui.C) {
		ui.Row(c).Fill().Draw(func(d *ui.DrawContext) { l.PaintWallpaperLayer(d, WallpaperLayer{File: "a.png", Opacity: 1}) })
	})
	h.frames(2)
}

// Retained：没有壁纸为 0；读到源图后是源图字节数；模糊渲染另占一份；清空文件名与 Close 都归零。
func TestWallpaperCacheRetained(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(40, 30, color.NRGBA{G: 200, A: 255}))}}
	c, _ := newTestCache(l)
	if c.Retained() != 0 {
		t.Fatalf("新缓存应为 0：%d", c.Retained())
	}
	waitFor(t, "源图读完", func() bool { _, ok := c.Bitmap("a.png", 1, 0, 40, 30); return ok })
	if got, want := c.Retained(), 40*30*4; got != want {
		t.Fatalf("不模糊且不透明度 1 时只占源图本身：%d，期望 %d", got, want)
	}
	waitFor(t, "模糊渲染完成", func() bool {
		_, ok := c.Bitmap("a.png", 0.5, 8, 40, 30)
		return ok && c.Retained() > 40*30*4
	})
	c.Bitmap("", 0, 0, 0, 0)
	if c.Retained() != 0 {
		t.Errorf("清空文件名后应为 0：%d", c.Retained())
	}
	waitFor(t, "重新读取", func() bool { _, ok := c.Bitmap("a.png", 1, 0, 40, 30); return ok })
	c.Close()
	if c.Retained() != 0 {
		t.Errorf("Close 后应为 0：%d", c.Retained())
	}
}
