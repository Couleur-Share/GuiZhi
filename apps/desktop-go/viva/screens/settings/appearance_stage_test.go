package settings

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// 预览舞台自己的底色（舞台根元素的 bg-background）：窗口没开壁纸时不透明；窗口开了壁纸（Look.Glass）时命中
// `.app-background-mode-image .bg-background`，浅色 22%、深色 30%。令牌本身固定取壁纸模式，不随窗口外观变。
func TestStageBaseOpaqueUnlessWindowGlass(t *testing.T) {
	for _, dark := range []bool{false, true} {
		look := kit.NewLook(kit.DefaultSpec())
		look.Dark = dark
		gt := tokens.For(dark, true, look.Spec.Hue, look.Spec.Saturation)
		a := &Appearance{Look: look}
		if got := a.stageBase(gt); got != gt.Background {
			t.Errorf("dark=%v 窗口没开壁纸时舞台底色应是不透明的背景色 %+v，得到 %+v", dark, gt.Background, got)
		}
		look.Glass = true
		alpha := float32(0.22)
		if dark {
			alpha = 0.3
		}
		want := gt.Background.WithAlpha(alpha)
		if got := a.stageBase(gt); got != want {
			t.Errorf("dark=%v 窗口开了壁纸时舞台底色应是背景色 %.0f%%：%+v，得到 %+v", dark, alpha*100, want, got)
		}
	}
}

// Close 要真的关掉预览缓存（释放位图、让后台结果作废），不只是丢掉引用；可重复调用。
func TestAppearanceCloseClosesPreviewCache(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 9))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	cache := kit.NewWallpaperCache(func(string) ([]byte, error) { return buf.Bytes(), nil }, nil)
	a := &Appearance{Look: kit.NewLook(kit.DefaultSpec()), preview: cache}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := cache.Bitmap("w.png", 1, 0, 16, 9); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("等待预览位图读完超时")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a.PreviewRetained() == 0 {
		t.Fatal("读完后预览缓存应占有位图")
	}
	a.Close()
	if a.preview != nil || a.PreviewRetained() != 0 {
		t.Errorf("Close 后应丢掉预览缓存：preview=%v retained=%d", a.preview, a.PreviewRetained())
	}
	if got := cache.Retained(); got != 0 {
		t.Errorf("Close 应真的关掉缓存、释放位图：%d", got)
	}
	if _, ok := cache.Bitmap("w.png", 1, 0, 16, 9); ok {
		t.Error("缓存关闭后不应再返回位图")
	}
	a.Close() // 可重复调用
}
