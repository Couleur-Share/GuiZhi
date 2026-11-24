package kit

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// webpSample 是 golang.org/x/image 自带的 1 位无损 WebP 测试图（gopher-doc.1bpp.lossless.webp，442 字节的 base64），只用来证明 WebP 解码器接上了。
const webpSample = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func solidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pixelBGRA 取位图 (x, y) 的预乘 BGRA。
func pixelBGRA(b gfx.Bitmap, x, y int) [4]byte {
	o := y*b.Stride + x*4
	return [4]byte{b.Pix[o], b.Pix[o+1], b.Pix[o+2], b.Pix[o+3]}
}

func byteNear(a, b byte, tol int) bool { return math.Abs(float64(a)-float64(b)) <= float64(tol) }

// 四种格式都能解成预乘 BGRA；半透明像素被预乘；颜色通道顺序是 B、G、R、A。
func TestDecodeWallpaperFormats(t *testing.T) {
	red := color.NRGBA{R: 200, G: 10, B: 30, A: 255}
	cases := map[string][]byte{"png": encodePNG(t, solidNRGBA(8, 6, red))}

	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, solidNRGBA(8, 6, red), &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	cases["jpeg"] = jbuf.Bytes()

	pal := color.Palette{color.NRGBA{R: 200, G: 10, B: 30, A: 255}, color.NRGBA{A: 0}}
	pimg := image.NewPaletted(image.Rect(0, 0, 8, 6), pal)
	var gbuf bytes.Buffer
	if err := gif.Encode(&gbuf, pimg, nil); err != nil {
		t.Fatal(err)
	}
	cases["gif"] = gbuf.Bytes()

	for name, data := range cases {
		bmp, err := DecodeWallpaper(data)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if bmp.Width != 8 || bmp.Height != 6 || bmp.Stride != 32 || len(bmp.Pix) != 8*6*4 {
			t.Fatalf("%s：位图尺寸不对：%dx%d stride %d", name, bmp.Width, bmp.Height, bmp.Stride)
		}
		got := pixelBGRA(bmp, 3, 2)
		tol := 0
		if name == "jpeg" {
			tol = 6 // 有损
		}
		if !byteNear(got[0], 30, tol) || !byteNear(got[1], 10, tol) || !byteNear(got[2], 200, tol) || got[3] != 255 {
			t.Errorf("%s：像素应是 BGRA(30,10,200,255)：%v", name, got)
		}
	}

	webp, err := base64.StdEncoding.DecodeString(webpSample)
	if err != nil {
		t.Fatal(err)
	}
	bmp, err := DecodeWallpaper(webp)
	if err != nil || bmp.Width <= 0 || bmp.Height <= 0 || !validBitmap(bmp) {
		t.Fatalf("webp 应能解码：%v %+v", err, bmp.Width)
	}
}

// 半透明 PNG 解成预乘值：A=128 的纯白变成 128,128,128,128。
func TestDecodeWallpaperPremultipliesAlpha(t *testing.T) {
	bmp, err := DecodeWallpaper(encodePNG(t, solidNRGBA(2, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 128})))
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelBGRA(bmp, 1, 1); got != [4]byte{128, 128, 128, 128} {
		t.Fatalf("应是预乘值：%v", got)
	}
}

// 坏数据、空数据与截断的文件返回错误；像素总数超限的图不解码（只读头）。
func TestDecodeWallpaperRejects(t *testing.T) {
	good := encodePNG(t, solidNRGBA(4, 4, color.NRGBA{A: 255}))
	for name, data := range map[string][]byte{
		"空":    nil,
		"不是图片": []byte("hello, wallpaper"),
		"截断":   good[:len(good)/2],
		"svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`),
	} {
		if _, err := DecodeWallpaper(data); err == nil {
			t.Errorf("%s：应返回错误", name)
		}
	}
	// 只伪造 PNG 头里的宽高（IHDR，并重算校验和）：声明 40000×40000 = 16 亿像素，必须在解码前就被拒绝。
	huge := append([]byte(nil), good...)
	binary.BigEndian.PutUint32(huge[16:], 40000) // 宽
	binary.BigEndian.PutUint32(huge[20:], 40000) // 高
	binary.BigEndian.PutUint32(huge[29:], crc32.ChecksumIEEE(huge[12:29]))
	if _, err := DecodeWallpaper(huge); !errors.Is(err, ErrWallpaperTooLarge) {
		t.Errorf("超大像素数应在解码前被拒绝：%v", err)
	}
}

// 大图限幅：长边超过 1920 的图等比缩到 1920 以内；小图原尺寸；缩小是面积平均，纯色不变色。
func TestWallpaperBitmapLimitsLargeImages(t *testing.T) {
	big := solidNRGBA(4000, 1000, color.NRGBA{R: 10, G: 120, B: 250, A: 255})
	bmp := wallpaperBitmap(big, WallpaperMaxDim)
	if bmp.Width != 1920 || bmp.Height != 480 {
		t.Fatalf("4000×1000 应缩成 1920×480：%dx%d", bmp.Width, bmp.Height)
	}
	if got := pixelBGRA(bmp, 960, 240); got != [4]byte{250, 120, 10, 255} {
		t.Fatalf("纯色缩小后不应变色：%v", got)
	}
	tall := wallpaperBitmap(solidNRGBA(900, 3000, color.NRGBA{A: 255}), WallpaperMaxDim)
	if tall.Width != 576 || tall.Height != 1920 {
		t.Fatalf("900×3000 应缩成 576×1920：%dx%d", tall.Width, tall.Height)
	}
	if small := wallpaperBitmap(solidNRGBA(300, 200, color.NRGBA{A: 255}), WallpaperMaxDim); small.Width != 300 || small.Height != 200 {
		t.Fatalf("小图保持原尺寸：%dx%d", small.Width, small.Height)
	}
	if thin := wallpaperBitmap(solidNRGBA(5000, 1, color.NRGBA{A: 255}), WallpaperMaxDim); thin.Width != 1920 || thin.Height != 1 {
		t.Fatalf("极细的图高度至少 1：%dx%d", thin.Width, thin.Height)
	}
}

// 面积平均：黑白相间的棋盘缩小一半后是均匀的灰（而不是采样出的纯黑或纯白）。
func TestAreaResampleAveragesInsteadOfSampling(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			v := uint8(0)
			if (x+y)%2 == 0 {
				v = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	bmp := wallpaperBitmap(img, 4)
	if bmp.Width != 4 || bmp.Height != 4 {
		t.Fatalf("尺寸：%dx%d", bmp.Width, bmp.Height)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if p := pixelBGRA(bmp, x, y); !byteNear(p[0], 128, 1) || !byteNear(p[1], 128, 1) || !byteNear(p[2], 128, 1) || p[3] != 255 {
				t.Fatalf("(%d,%d) 应是均匀的灰：%v", x, y, p)
			}
		}
	}
	// 非整数倍：权重之和为 1，纯色不变（边界像素不会被多算或漏算）。
	odd := wallpaperBitmap(solidNRGBA(7, 5, color.NRGBA{R: 40, G: 40, B: 40, A: 255}), 3)
	for y := 0; y < odd.Height; y++ {
		for x := 0; x < odd.Width; x++ {
			if p := pixelBGRA(odd, x, y); !byteNear(p[0], 40, 1) || p[3] != 255 {
				t.Fatalf("非整数倍缩小纯色应不变：(%d,%d) %v", x, y, p)
			}
		}
	}
}

// 模糊的标准差换算：CSS blur 的像素是显示像素，除以显示比例得到位图像素。
func TestWallpaperBlurSigma(t *testing.T) {
	for _, c := range []struct{ blur, scale, want float32 }{
		{10, 1, 10}, {10, 0.5, 20}, {10, 2, 5}, {0, 1, 0}, {-3, 1, 0}, {10, 0, 0}, {10, -1, 0},
	} {
		if got := WallpaperBlurSigma(c.blur, c.scale); math.Abs(float64(got-c.want)) > 1e-6 {
			t.Errorf("sigma(%v, %v) = %v，应为 %v", c.blur, c.scale, got, c.want)
		}
	}
	if got := WallpaperBlurSigma(float32(math.NaN()), 1); got != 0 {
		t.Errorf("NaN 应按 0：%v", got)
	}
}

func uniformBitmap(w, h int, bgra [4]byte) gfx.Bitmap {
	b := gfx.Bitmap{Pix: make([]byte, w*h*4), Width: w, Height: h, Stride: w * 4}
	for i := 0; i < len(b.Pix); i += 4 {
		copy(b.Pix[i:], bgra[:])
	}
	return b
}

// 渲染：不模糊且不透明度 1 时原样返回（同一块像素）；不透明度预乘四个通道；越界与非法参数被钳制。
func TestRenderWallpaperOpacityAndIdentity(t *testing.T) {
	src := uniformBitmap(4, 3, [4]byte{200, 100, 50, 255})
	same := RenderWallpaper(src, 1, 0, 1)
	if &same.Pix[0] != &src.Pix[0] {
		t.Fatal("opacity 1、blur 0 应原样返回，不拷贝像素")
	}
	half := RenderWallpaper(src, 0.5, 0, 1)
	if &half.Pix[0] == &src.Pix[0] {
		t.Fatal("淡出必须是新位图，不能改动源图")
	}
	if got := pixelBGRA(half, 2, 1); got != [4]byte{100, 50, 25, 128} {
		t.Fatalf("0.5 不透明度应预乘四个通道：%v", got)
	}
	if pixelBGRA(src, 0, 0) != [4]byte{200, 100, 50, 255} {
		t.Fatal("源图被改动")
	}
	if got := pixelBGRA(RenderWallpaper(src, 0, 0, 1), 0, 0); got != [4]byte{} {
		t.Fatalf("0 不透明度应全透明：%v", got)
	}
	if got := pixelBGRA(RenderWallpaper(src, 7, 0, 1), 0, 0); got != [4]byte{200, 100, 50, 255} {
		t.Fatalf("大于 1 钳制为 1：%v", got)
	}
	if got := pixelBGRA(RenderWallpaper(src, -1, 0, 1), 0, 0); got != [4]byte{} {
		t.Fatalf("小于 0 钳制为 0：%v", got)
	}
	if got := pixelBGRA(RenderWallpaper(src, float32(math.NaN()), float32(math.Inf(1)), 1), 0, 0); got != [4]byte{200, 100, 50, 255} {
		t.Fatalf("NaN 不透明度按 1、Inf 模糊按 0：%v", got)
	}
	if bad := RenderWallpaper(gfx.Bitmap{}, 1, 5, 1); len(bad.Pix) != 0 {
		t.Fatal("无效源图应返回零值位图")
	}
}

// 模糊：CSS 口径——纯色内部不变，贴着边的像素与边界外的透明混合而变淡；σ 很大时先缩小位图，结果小于源图。
func TestRenderWallpaperBlur(t *testing.T) {
	src := uniformBitmap(64, 64, [4]byte{255, 255, 255, 255})
	small := RenderWallpaper(src, 1, 2, 1) // σ=2 ≤ 上限：不缩小
	if small.Width != 64 || small.Height != 64 {
		t.Fatalf("σ ≤ 上限时尺寸不变：%dx%d", small.Width, small.Height)
	}
	if got := pixelBGRA(small, 32, 32); !byteNear(got[3], 255, 1) {
		t.Fatalf("纯色内部不透明：%v", got)
	}
	if edge, mid := pixelBGRA(small, 0, 32), pixelBGRA(small, 32, 32); edge[3] >= mid[3] || edge[3] < 100 {
		t.Fatalf("边缘应向外渐淡（与透明混合）：edge %v mid %v", edge, mid)
	}
	if corner, edge := pixelBGRA(small, 0, 0), pixelBGRA(small, 0, 32); corner[3] >= edge[3] {
		t.Fatalf("角比边更淡：corner %v edge %v", corner, edge)
	}

	big := RenderWallpaper(src, 1, 50, 0.5) // σ=100：缩到 3/100 倍
	if big.Width >= 64 || big.Height >= 64 || big.Width < 2 || big.Height < 2 {
		t.Fatalf("大模糊应先缩小位图：%dx%d", big.Width, big.Height)
	}
	if !validBitmap(big) {
		t.Fatal("位图无效")
	}
	// 模糊后再淡出：两步叠加。
	both := RenderWallpaper(src, 0.5, 2, 1)
	if mid := pixelBGRA(both, 32, 32); !byteNear(mid[3], 128, 1) {
		t.Fatalf("模糊加淡出：%v", mid)
	}
}

// 4K 大图加最大模糊的整条管线耗时有界（CPU 上，无 GPU）：限幅到 1920 后 σ 被折算，渲染远小于 1 秒。
func TestRenderWallpaperLargeBlurIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	img := image.NewYCbCr(image.Rect(0, 0, 3840, 2160), image.YCbCrSubsampleRatio420) // JPEG 的内存布局，4K（8K 的耗时按像素数线性放大，-race 下太慢）
	for i := range img.Y {
		img.Y[i] = 100
	}
	for i := range img.Cb {
		img.Cb[i], img.Cr[i] = 120, 140
	}
	start := time.Now()
	src := wallpaperBitmap(img, WallpaperMaxDim)
	decode := time.Since(start)
	if src.Width != 1920 || src.Height != 1080 {
		t.Fatalf("限幅：%dx%d", src.Width, src.Height)
	}
	start = time.Now()
	out := RenderWallpaper(src, 0.8, 50, 0.6667)
	render := time.Since(start)
	if !validBitmap(out) || out.Width*out.Height > 1920*1080 {
		t.Fatalf("渲染结果：%dx%d", out.Width, out.Height)
	}
	if render > 2*time.Second { // 竞态检测与慢机器下放宽；正常 < 100 ms
		t.Errorf("blur=50 渲染耗时 %v，应有界", render)
	}
	t.Logf("3840×2160 限幅 %v，blur=50 渲染 %v → %dx%d", decode, render, out.Width, out.Height)
}

// 居中铺满裁剪：object-cover object-center；有模糊时再取 1/1.03 的子矩形（scale(1.03)）。
func TestWallpaperSourceRect(t *testing.T) {
	r := func(x, y, w, h float32) gfx.Rect { return gfx.Rect{X: x, Y: y, Width: w, Height: h} }
	approx := func(a, b gfx.Rect) bool {
		return math.Abs(float64(a.X-b.X)) < 1e-3 && math.Abs(float64(a.Y-b.Y)) < 1e-3 &&
			math.Abs(float64(a.Width-b.Width)) < 1e-3 && math.Abs(float64(a.Height-b.Height)) < 1e-3
	}
	// 同比例：整张图。
	if got := WallpaperSourceRect(200, 100, 400, 200, false); !approx(got, r(0, 0, 200, 100)) {
		t.Errorf("同比例应取整张：%v", got)
	}
	// 目标更宽：上下裁掉（高度只取 100×(200/400)... 宽铺满）。
	if got := WallpaperSourceRect(200, 200, 400, 200, false); !approx(got, r(0, 50, 200, 100)) {
		t.Errorf("目标更宽应裁上下：%v", got)
	}
	// 目标更高：左右裁掉。
	if got := WallpaperSourceRect(200, 200, 200, 400, false); !approx(got, r(50, 0, 100, 200)) {
		t.Errorf("目标更高应裁左右：%v", got)
	}
	// 有模糊：再绕中心取 1/1.03。
	w, h := float32(200/1.03), float32(100/1.03)
	if got := WallpaperSourceRect(200, 100, 400, 200, true); !approx(got, r((200-w)/2, (100-h)/2, w, h)) {
		t.Errorf("有模糊应再向内取 1/1.03：%v", got)
	}
	// 退化输入：取整张，不出现 NaN。
	for _, d := range [][2]float32{{0, 100}, {100, 0}, {-1, 5}, {float32(math.NaN()), 5}} {
		if got := WallpaperSourceRect(200, 100, d[0], d[1], true); got != r(0, 0, 200, 100) {
			t.Errorf("退化目标 %v 应取整张：%v", d, got)
		}
	}
	if got := WallpaperSourceRect(0, 0, 10, 10, false); got != r(0, 0, 0, 0) {
		t.Errorf("空位图：%v", got)
	}
}

// PaintWallpaper 把源矩形与目标矩形交给画布：预览走当前层，整窗壁纸走 DrawBackdrop。
func TestPaintWallpaper(t *testing.T) {
	bmp := uniformBitmap(200, 100, [4]byte{1, 2, 3, 255})
	bmp.ID, bmp.Version = 7, 1
	bounds := gfx.Rect{X: 10, Y: 20, Width: 400, Height: 200}
	for _, backdrop := range []bool{false, true} {
		var canvas gfx.Canvas
		PaintWallpaper(&canvas, bmp, bounds, true, backdrop)
		imgs := canvas.Images()
		if len(imgs) != 1 || imgs[0].Bounds != bounds || imgs[0].Backdrop != backdrop {
			t.Fatalf("backdrop=%v：%+v", backdrop, imgs)
		}
		want := WallpaperSourceRect(200, 100, 400, 200, true)
		if got := imgs[0].Source; math.Abs(float64(got.Width-want.Width)) > 1e-3 || math.Abs(float64(got.X-want.X)) > 1e-3 {
			t.Fatalf("源矩形：%v 应为 %v", got, want)
		}
	}
	var canvas gfx.Canvas
	PaintWallpaper(&canvas, gfx.Bitmap{}, bounds, false, false)
	if len(canvas.Images()) != 0 {
		t.Fatal("无效位图什么也不画")
	}
}

// ── 缓存 ──

type wallpaperLoader struct {
	mu    sync.Mutex
	files map[string][]byte
	errs  map[string]error
	calls map[string]int
	gate  chan struct{} // 非 nil 时 load 先等它关闭
}

func (l *wallpaperLoader) load(name string) ([]byte, error) {
	l.mu.Lock()
	if l.calls == nil {
		l.calls = map[string]int{}
	}
	l.calls[name]++
	data, err, gate := l.files[name], l.errs[name], l.gate
	l.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errors.New("no such wallpaper")
	}
	return data, nil
}

func (l *wallpaperLoader) count(name string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls[name]
}

// waitFor 轮询直到条件成立（缓存的读取与渲染都在后台 goroutine）。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

func newTestCache(l *wallpaperLoader) (*WallpaperCache, *atomic.Int32) {
	var wakes atomic.Int32
	return NewWallpaperCache(l.load, func() { wakes.Add(1) }), &wakes
}

// 读取是后台的：第一次调用立刻返回 ok=false 并标记 Loading；读完调用 wake；再取时拿到位图。
func TestWallpaperCacheLoadsInBackground(t *testing.T) {
	gate := make(chan struct{})
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(40, 30, color.NRGBA{R: 9, A: 255}))}, gate: gate}
	c, wakes := newTestCache(l)
	if _, ok := c.Bitmap("a.png", 1, 0, 100, 100); ok || !c.Loading() || c.Failed() {
		t.Fatalf("读取中：ok=false、Loading、非 Failed（Loading=%v Failed=%v）", c.Loading(), c.Failed())
	}
	close(gate)
	waitFor(t, "wake", func() bool { return wakes.Load() > 0 })
	var bmp gfx.Bitmap
	waitFor(t, "位图", func() bool {
		var ok bool
		bmp, ok = c.Bitmap("a.png", 1, 0, 100, 100)
		return ok
	})
	if bmp.Width != 40 || bmp.Height != 30 || bmp.ID == 0 || bmp.Version == 0 || c.Loading() || c.Failed() {
		t.Fatalf("位图：%+v Loading=%v", bmp.Width, c.Loading())
	}
}

// 参数没变时每帧返回同一块像素与同一个标识（零拷贝、零重新模糊）；读取只发生一次。
func TestWallpaperCacheStableWhenParamsUnchanged(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(80, 60, color.NRGBA{G: 90, A: 255}))}}
	c, _ := newTestCache(l)
	get := func() (gfx.Bitmap, bool) { return c.Bitmap("a.png", 0.7, 12, 320, 240) }
	var first gfx.Bitmap
	waitFor(t, "首份渲染", func() bool {
		b, ok := get()
		first = b
		return ok && first.ID != 0
	})
	// 渲染结果一定出来之后，参数不变应稳定命中；先等到「待渲染」排空。
	waitFor(t, "渲染排空", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.working
	})
	first, _ = get()
	for i := 0; i < 200; i++ {
		b, ok := get()
		if !ok || b.ID != first.ID || b.Version != first.Version || &b.Pix[0] != &first.Pix[0] {
			t.Fatalf("第 %d 次：应命中同一份位图 %d，得到 %d", i, first.ID, b.ID)
		}
	}
	if n := l.count("a.png"); n != 1 {
		t.Fatalf("读取只应发生一次：%d", n)
	}
}

// 不模糊且不透明度 1：渲染结果就是源图本身（共享像素），也不占额外内存。
func TestWallpaperCacheIdentityIsShared(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(20, 20, color.NRGBA{A: 255}))}}
	c, _ := newTestCache(l)
	var b gfx.Bitmap
	waitFor(t, "位图", func() bool {
		var ok bool
		b, ok = c.Bitmap("a.png", 1, 0, 50, 50)
		return ok
	})
	c.mu.Lock()
	same := &b.Pix[0] == &c.src.Pix[0] && b.ID == c.src.ID
	c.mu.Unlock()
	if !same {
		t.Fatal("opacity 1、blur 0 应直接用源图")
	}
}

// 参数变了：渲染期间继续返回上一份（stale），完成后换成新参数的位图，标识递增；回到旧参数时命中旧缓存、不再渲染。
func TestWallpaperCacheParamChangeKeepsStaleThenSwaps(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(100, 100, color.NRGBA{R: 255, A: 255}))}}
	c, wakes := newTestCache(l)
	var base gfx.Bitmap
	waitFor(t, "基础位图", func() bool {
		var ok bool
		base, ok = c.Bitmap("a.png", 1, 0, 100, 100)
		return ok
	})
	before := wakes.Load()
	var faded gfx.Bitmap
	waitFor(t, "淡出位图", func() bool {
		b, ok := c.Bitmap("a.png", 0.5, 0, 100, 100)
		if !ok {
			t.Fatal("参数变了也要有位图可画（上一份）")
		}
		faded = b
		return b.ID != base.ID
	})
	if faded.ID <= base.ID {
		t.Fatalf("标识应递增：%d → %d", base.ID, faded.ID)
	}
	if got := pixelBGRA(faded, 10, 10); got[2] != 128 || got[3] != 128 {
		t.Fatalf("0.5 不透明度的红：%v", got)
	}
	if wakes.Load() <= before {
		t.Fatal("渲染完成应 wake")
	}
	// 回到旧参数：立即命中，标识不变。
	back, ok := c.Bitmap("a.png", 1, 0, 100, 100)
	if !ok || back.ID != base.ID {
		t.Fatalf("回到旧参数应命中旧缓存：%d vs %d", back.ID, base.ID)
	}
}

// 滑块连续拖动：同一时刻最多一个渲染在跑，最终落在最后一次的参数上，且标识永不重复。
func TestWallpaperCacheRapidParamChanges(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(200, 120, color.NRGBA{B: 255, A: 255}))}}
	c, _ := newTestCache(l)
	for i := 0; i <= 50; i++ {
		c.Bitmap("a.png", 0.9, float32(i), 300, 180)
	}
	waitFor(t, "最终参数", func() bool {
		c.Bitmap("a.png", 0.9, 50, 300, 180) // 界面每帧都会取；读取完成后的这一帧才排程渲染
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.working && c.phase == wallpaperReady && c.touchLocked(c.want) != nil
	})
	b, ok := c.Bitmap("a.png", 0.9, 50, 300, 180)
	if !ok {
		t.Fatal("最终参数应有位图")
	}
	if b.ID == 0 || !validBitmap(b) {
		t.Fatal("最终位图无效")
	}
	c.mu.Lock()
	seen := map[uint64]bool{}
	for _, r := range c.renders {
		if seen[r.bmp.ID] {
			t.Fatalf("标识重复：%d", r.bmp.ID)
		}
		seen[r.bmp.ID] = true
	}
	c.mu.Unlock()
}

// 失败：读取错误、解码错误都标 Failed 且给出原因；换成好文件后恢复；空文件名清缓存。
func TestWallpaperCacheFailureAndRecovery(t *testing.T) {
	l := &wallpaperLoader{
		files: map[string][]byte{"bad.png": []byte("not an image"), "good.png": encodePNG(t, solidNRGBA(10, 10, color.NRGBA{A: 255}))},
		errs:  map[string]error{"gone.png": errors.New("read failed")},
	}
	c, _ := newTestCache(l)
	for _, name := range []string{"bad.png", "gone.png", "missing.png"} {
		c.Bitmap(name, 1, 0, 10, 10)
		waitFor(t, name+" 失败", c.Failed)
		if _, ok := c.Bitmap(name, 1, 0, 10, 10); ok || c.Err() == nil || c.Loading() {
			t.Fatalf("%s：失败时无位图、有原因、不在读取", name)
		}
	}
	waitFor(t, "good", func() bool { _, ok := c.Bitmap("good.png", 1, 0, 10, 10); return ok })
	if c.Failed() || c.Err() != nil {
		t.Fatal("换成好文件后应清除失败态")
	}
	if _, ok := c.Bitmap("", 1, 0, 10, 10); ok || c.Failed() || c.Loading() {
		t.Fatal("空文件名清缓存")
	}
	c.mu.Lock()
	cleared := len(c.renders) == 0 && c.src.Pix == nil
	c.mu.Unlock()
	if !cleared {
		t.Fatal("空文件名应释放源图与渲染结果")
	}
	var nilLoader = NewWallpaperCache(nil, nil)
	nilLoader.Bitmap("x.png", 1, 0, 1, 1)
	waitFor(t, "无 loader 失败", nilLoader.Failed)
}

// 换文件：过期的读取结果被丢弃，不会把旧文件的位图当成新文件的。
func TestWallpaperCacheDropsStaleLoad(t *testing.T) {
	gate := make(chan struct{})
	l := &wallpaperLoader{files: map[string][]byte{
		"old.png": encodePNG(t, solidNRGBA(10, 10, color.NRGBA{R: 255, A: 255})),
		"new.png": encodePNG(t, solidNRGBA(20, 20, color.NRGBA{B: 255, A: 255})),
	}}
	l.gate = gate
	c, _ := newTestCache(l)
	c.Bitmap("old.png", 1, 0, 10, 10)
	c.Bitmap("new.png", 1, 0, 10, 10) // 此时 old 还卡在读取里
	close(gate)
	var b gfx.Bitmap
	waitFor(t, "new 位图", func() bool {
		var ok bool
		b, ok = c.Bitmap("new.png", 1, 0, 10, 10)
		return ok
	})
	if b.Width != 20 {
		t.Fatalf("应是 new.png：%dx%d", b.Width, b.Height)
	}
	time.Sleep(20 * time.Millisecond) // 给过期结果落地的机会
	if b2, ok := c.Bitmap("new.png", 1, 0, 10, 10); !ok || b2.Width != 20 || b2.ID != b.ID {
		t.Fatal("过期的读取结果不应覆盖当前文件")
	}
}

// Close 之后后台结果作废、wake 不再被调用，Bitmap 恒 ok=false。
func TestWallpaperCacheClose(t *testing.T) {
	gate := make(chan struct{})
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(10, 10, color.NRGBA{A: 255}))}, gate: gate}
	c, wakes := newTestCache(l)
	c.Bitmap("a.png", 1, 0, 10, 10)
	c.Close()
	c.Close()
	close(gate)
	time.Sleep(30 * time.Millisecond)
	if wakes.Load() != 0 {
		t.Fatalf("Close 之后不应再 wake：%d", wakes.Load())
	}
	if _, ok := c.Bitmap("a.png", 1, 0, 10, 10); ok {
		t.Fatal("Close 之后不应再有位图")
	}
}

// 模糊的显示比例分桶：窗口尺寸在同一桶内缓慢变化共用一份渲染；跨桶重新渲染；不模糊时尺寸完全无关。
func TestWallpaperCacheScaleBuckets(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{"a.png": encodePNG(t, solidNRGBA(100, 100, color.NRGBA{A: 255}))}}
	c, _ := newTestCache(l)
	waitFor(t, "源图", func() bool { c.Bitmap("a.png", 1, 5, 100, 100); return !c.Loading() })
	c.mu.Lock()
	k100 := c.keyLocked(1, 5, 100, 100)
	k103 := c.keyLocked(1, 5, 103, 100)
	k200 := c.keyLocked(1, 5, 200, 200)
	n100 := c.keyLocked(1, 0, 100, 100)
	n900 := c.keyLocked(1, 0, 900, 900)
	c.mu.Unlock()
	if k100 != k103 {
		t.Errorf("3%% 的尺寸差应共用一桶：%v %v", k100, k103)
	}
	if k100 == k200 {
		t.Errorf("尺寸翻倍应换桶：%v %v", k100, k200)
	}
	if n100 != n900 {
		t.Errorf("不模糊时尺寸无关：%v %v", n100, n900)
	}
	if k100.blur != 50 || k100.opacity != 100 {
		t.Errorf("分桶：blur 5 → 50（十分之一像素）、opacity 1 → 100：%v", k100)
	}
}

// 缓存字节预算：超过预算按最久未用淘汰，至少保留最新一份，共享源图的结果不计字节。
func TestWallpaperCacheTrimsByBudget(t *testing.T) {
	c := NewWallpaperCache(nil, nil)
	mk := func(n int, shared bool) *wallpaperRender {
		return &wallpaperRender{key: wallpaperKey{opacity: n}, bmp: gfx.Bitmap{Pix: make([]byte, 20<<20)}, shared: shared}
	}
	c.renders = []*wallpaperRender{mk(1, false), mk(2, true), mk(3, false), mk(4, false)}
	c.trimLocked()
	if len(c.renders) != 3 || c.renders[0].key.opacity != 2 {
		t.Fatalf("应淘汰最旧的一份（60 MB > 48 MB 预算）：%d", len(c.renders))
	}
	c.renders = []*wallpaperRender{{key: wallpaperKey{opacity: 9}, bmp: gfx.Bitmap{Pix: make([]byte, 60<<20)}}}
	c.trimLocked()
	if len(c.renders) != 1 {
		t.Fatal("至少保留最新一份")
	}
}

// 并发（-race）：界面线程每帧取位图的同时，别的线程反复改参数与文件名。
func TestWallpaperCacheConcurrent(t *testing.T) {
	l := &wallpaperLoader{files: map[string][]byte{
		"a.png": encodePNG(t, solidNRGBA(120, 80, color.NRGBA{R: 200, A: 255})),
		"b.png": encodePNG(t, solidNRGBA(90, 60, color.NRGBA{G: 200, A: 255})),
	}}
	c, _ := newTestCache(l)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				name := "a.png"
				if (i+g)%7 == 0 {
					name = "b.png"
				}
				if b, ok := c.Bitmap(name, float32(i%11)/10, float32(i%9), float32(100+i%40), 80); ok && !validBitmap(b) {
					t.Errorf("取到无效位图")
					return
				}
				c.Failed()
				c.Loading()
			}
		}()
	}
	wg.Wait()
	c.Close()
}
