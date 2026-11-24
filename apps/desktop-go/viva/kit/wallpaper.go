package kit

// 背景图（壁纸）位图管线：解码、限幅、模糊、不透明度与居中铺满裁剪，加一个按参数缓存的异步渲染器。
//
// 对照渲染层 components/ui/BackgroundImageBackdrop.tsx：
//
//	<div class="absolute inset-0 overflow-hidden" style="opacity; filter: blur(Npx); transform: blur>0 ? scale(1.03)">
//	  <img class="h-full w-full object-cover object-center">
//
// 所以：图片按 object-cover 居中铺满容器；filter: blur(N) 的 N 是 CSS 像素里的高斯标准差（作用在容器大小的元素上，边缘与透明背景混合，
// 越往边缘越淡）；有模糊时整体再放大 1.03 倍，把边缘的衰减推到容器外；opacity 作用于整个元素。
// 管线的各步都是纯函数，便于单测；WallpaperCache 把它们串成「后台读取并解码 → 后台按参数渲染 → 界面线程每帧只取现成位图」，
// 预览舞台与整窗壁纸各持一个实例（模糊的像素标准差取决于显示尺寸，所以不共用）。

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 注册 GIF 解码器
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器
	"math"
	"sync"
	"sync/atomic"

	"github.com/vivaui/viva/gfx"
	_ "golang.org/x/image/webp" // 注册 WebP 解码器
)

const (
	// WallpaperMaxDim 是壁纸源图的长边上限（像素）：更大的图解码后先等比缩小，防止 8K 大图加大模糊在 CPU 上耗时数百毫秒、占数十兆内存。
	WallpaperMaxDim = 1920
	// wallpaperMaxPixels 是允许解码的源图像素总数（约 6700 万，覆盖 8192×8192）：先读文件头判断，超过的图不解码，避免解压炸弹。
	wallpaperMaxPixels = 64 << 20
	// wallpaperBlurCap 是送进 gfx.GaussianBlurBitmap 的标准差上限（位图像素）：该函数的耗时与 宽×高×σ 成正比，
	// 更大的模糊先把位图缩小再用 σ = 上限去模糊（模糊后的内容是低频的，缩小再双线性放大几乎看不出差别）。
	wallpaperBlurCap = 3
	// WallpaperBlurScale 是有模糊时的整体放大倍数（transform: scale(1.03)）。
	WallpaperBlurScale = 1.03
	// wallpaperMaxBlur 是模糊值上限（与渲染层 clampBackgroundImageBlur 的 0–50 一致）。
	wallpaperMaxBlur = 50
	// wallpaperScaleStep 是显示比例分桶的公比：模糊的像素标准差 = 模糊值 ÷ 显示比例，比例相差 10% 以内共用一份渲染（σ 误差 ±5%）。
	wallpaperScaleStep = 1.1
	// wallpaperRenderBudget 是缓存里已渲染位图的总字节预算（至少保留最新的一份）。
	wallpaperRenderBudget = 48 << 20
)

// ErrWallpaperTooLarge 表示源图像素太多，不解码。
var ErrWallpaperTooLarge = errors.New("wallpaper image has too many pixels")

// wallpaperBitmapID 是壁纸位图的缓存标识来源：原生渲染器据此识别未变的内容，不必每帧重新上传。
// 高位固定一个壁纸专用的标记，与应用图标等其它位图的标识不冲突；每份新位图取下一个值，永不复用。
var wallpaperBitmapID atomic.Uint64

const wallpaperIDBase uint64 = 0x475a_5750_0000_0000

func nextWallpaperID() uint64 { return wallpaperIDBase + wallpaperBitmapID.Add(1) }

// ── 解码与限幅 ──

// DecodeWallpaper 把 PNG / JPEG / GIF（首帧）/ WebP 的字节解成预乘 BGRA 位图，长边超过 WallpaperMaxDim 的先等比缩小。
// 返回的位图没有缓存标识（ID 为 0），由持有者分配。
func DecodeWallpaper(data []byte) (gfx.Bitmap, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return gfx.Bitmap{}, fmt.Errorf("read wallpaper header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return gfx.Bitmap{}, errors.New("wallpaper image has no pixels")
	}
	if int64(cfg.Width)*int64(cfg.Height) > wallpaperMaxPixels {
		return gfx.Bitmap{}, ErrWallpaperTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return gfx.Bitmap{}, fmt.Errorf("decode wallpaper: %w", err)
	}
	return wallpaperBitmap(img, WallpaperMaxDim), nil
}

// wallpaperBitmap 把解码出的图转成预乘 BGRA 位图，长边超过 maxDim 时等比缩小（面积平均，见 areaResample）。
func wallpaperBitmap(img image.Image, maxDim int) gfx.Bitmap {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return gfx.Bitmap{}
	}
	dw, dh := w, h
	if long := max(w, h); long > maxDim {
		dw = min(w, max(1, int(math.Round(float64(w)*float64(maxDim)/float64(long)))))
		dh = min(h, max(1, int(math.Round(float64(h)*float64(maxDim)/float64(long)))))
	}
	pix := areaResample(w, h, dw, dh, imageRows(img)) // 预乘 RGBA
	swapRB(pix)                                       // RGBA → BGRA
	return gfx.Bitmap{Pix: pix, Width: dw, Height: dh, Stride: dw * 4}
}

func swapRB(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		pix[i], pix[i+2] = pix[i+2], pix[i]
	}
}

// ── 位图变换（纯函数，输入位图不被改动）──

func validBitmap(b gfx.Bitmap) bool {
	if b.Width <= 0 || b.Height <= 0 || b.Stride < b.Width*4 {
		return false
	}
	return len(b.Pix) >= (b.Height-1)*b.Stride+b.Width*4
}

// scaleBitmap 把预乘位图缩小到 dw×dh（不大于源）。
func scaleBitmap(src gfx.Bitmap, dw, dh int) gfx.Bitmap {
	return gfx.Bitmap{Pix: areaResample(src.Width, src.Height, dw, dh, bitmapRows(src)), Width: dw, Height: dh, Stride: dw * 4}
}

// padTransparent 在四周各补 pad 像素的全透明边：CSS 的 blur 滤镜在元素边界外是透明的，边缘因此向外渐淡。
func padTransparent(src gfx.Bitmap, pad int) gfx.Bitmap {
	w, h := src.Width+2*pad, src.Height+2*pad
	out := gfx.Bitmap{Pix: make([]byte, w*h*4), Width: w, Height: h, Stride: w * 4}
	for y := 0; y < src.Height; y++ {
		copy(out.Pix[(y+pad)*out.Stride+pad*4:], src.Pix[y*src.Stride:y*src.Stride+src.Width*4])
	}
	return out
}

// cropBitmap 取 (x, y) 起 w×h 的一块，拷成紧凑的新位图。
func cropBitmap(src gfx.Bitmap, x, y, w, h int) gfx.Bitmap {
	out := gfx.Bitmap{Pix: make([]byte, w*h*4), Width: w, Height: h, Stride: w * 4}
	for row := 0; row < h; row++ {
		from := (y+row)*src.Stride + x*4
		copy(out.Pix[row*out.Stride:], src.Pix[from:from+w*4])
	}
	return out
}

// WallpaperBlurSigma 是 CSS blur(blur px) 换算到位图像素的高斯标准差：blur 是显示像素，scale 是显示像素 ÷ 位图像素（object-cover 的缩放比）。
func WallpaperBlurSigma(blur, scale float32) float32 {
	if !(blur > 0) || !(scale > 0) || math.IsInf(float64(scale), 0) {
		return 0
	}
	return blur / scale
}

// blurWallpaper 做 CSS 口径的高斯模糊：σ 大于上限时先缩小位图（σ 同比缩小），边缘与透明混合（向外渐淡），结果与「缩小后的位图」同尺寸。
func blurWallpaper(src gfx.Bitmap, sigma float32) gfx.Bitmap {
	work, eff := src, sigma
	if sigma > wallpaperBlurCap {
		f := float64(wallpaperBlurCap) / float64(sigma)
		dw := min(src.Width, max(2, int(math.Round(float64(src.Width)*f))))
		dh := min(src.Height, max(2, int(math.Round(float64(src.Height)*f))))
		work = scaleBitmap(src, dw, dh)
		eff = sigma * float32(dw) / float32(src.Width)
	}
	pad := int(math.Ceil(float64(eff) * 3))
	blurred := gfx.GaussianBlurBitmap(padTransparent(work, pad), eff)
	if !validBitmap(blurred) {
		return work
	}
	return cropBitmap(blurred, pad, pad, work.Width, work.Height)
}

// fadeBitmap 把不透明度预乘进四个通道（预乘位图整体乘 opacity 就是整体淡出）；返回新位图，输入不改动。
func fadeBitmap(src gfx.Bitmap, opacity float32) gfx.Bitmap {
	a := uint32(math.Round(float64(opacity) * 255))
	var lut [256]byte
	for v := range lut {
		lut[v] = byte((uint32(v)*a + 127) / 255)
	}
	out := gfx.Bitmap{Pix: make([]byte, src.Width*src.Height*4), Width: src.Width, Height: src.Height, Stride: src.Width * 4}
	for y := 0; y < src.Height; y++ {
		in := src.Pix[y*src.Stride : y*src.Stride+src.Width*4]
		dst := out.Pix[y*out.Stride : y*out.Stride+src.Width*4]
		for i, v := range in {
			dst[i] = lut[v]
		}
	}
	return out
}

func clampF(v, lo, hi, fallback float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return fallback
	}
	return min(max(v, lo), hi)
}

// RenderWallpaper 按参数渲染壁纸位图：先高斯模糊（blur 是显示像素，scale 是显示像素 ÷ 位图像素），再预乘不透明度。
// opacity 钳制在 [0, 1]（非有限值按 1），blur 钳制在 [0, 50]（非有限值按 0）。blur 为 0 且 opacity 为 1 时原样返回 src（同一块像素，不拷贝）。
// 源位图无效返回零值位图。返回的位图没有缓存标识（除非原样返回 src）。
func RenderWallpaper(src gfx.Bitmap, opacity, blur, scale float32) gfx.Bitmap {
	if !validBitmap(src) {
		return gfx.Bitmap{}
	}
	opacity = clampF(opacity, 0, 1, 1)
	blur = clampF(blur, 0, wallpaperMaxBlur, 0)
	out := src
	if blur > 0 {
		if !(scale > 0) {
			scale = 1
		}
		out = blurWallpaper(out, WallpaperBlurSigma(blur, scale))
	}
	if opacity < 1 {
		out = fadeBitmap(out, opacity)
	}
	return out
}

// WallpaperSourceRect 是位图里应当铺到 dstW×dstH 目标上的那一块（位图像素）：复刻 object-cover object-center，
// blurred 为真（模糊大于 0）时再复刻 transform: scale(1.03)——绕中心放大等价于在源图里再向内取 1/1.03 的子矩形，
// 把模糊边缘的衰减推到目标之外。目标或位图没有面积时取整张位图。
func WallpaperSourceRect(bmpW, bmpH int, dstW, dstH float32, blurred bool) gfx.Rect {
	full := gfx.Rect{Width: float32(bmpW), Height: float32(bmpH)}
	if bmpW <= 0 || bmpH <= 0 || !(dstW > 0) || !(dstH > 0) {
		return full
	}
	bw, bh := float64(bmpW), float64(bmpH)
	cover := math.Max(float64(dstW)/bw, float64(dstH)/bh) // 目标像素 ÷ 位图像素
	w, h := float64(dstW)/cover, float64(dstH)/cover
	if blurred {
		w, h = w/WallpaperBlurScale, h/WallpaperBlurScale
	}
	return gfx.Rect{X: float32((bw - w) / 2), Y: float32((bh - h) / 2), Width: float32(w), Height: float32(h)}
}

// PaintWallpaper 把壁纸位图按 WallpaperSourceRect 铺进 bounds；backdrop 为真时画在整帧所有内容之下（半透明表面能透出它，整窗壁纸用），
// 否则画在当前层（预览舞台用）。位图无效时什么也不画。
func PaintWallpaper(canvas *gfx.Canvas, bmp gfx.Bitmap, bounds gfx.Rect, blurred, backdrop bool) {
	if !validBitmap(bmp) {
		return
	}
	src := WallpaperSourceRect(bmp.Width, bmp.Height, bounds.Width, bounds.Height, blurred)
	if backdrop {
		canvas.DrawBackdrop(bmp, src, bounds)
		return
	}
	canvas.DrawBitmapRegion(bmp, src, bounds)
}

// ── 异步渲染缓存 ──

type wallpaperPhase uint8

const (
	wallpaperIdle wallpaperPhase = iota
	wallpaperLoading
	wallpaperReady
	wallpaperFailed
)

// wallpaperKey 是一份渲染结果的参数（已分桶）：不透明度按百分之一，模糊按十分之一像素，显示比例按 wallpaperScaleStep 的对数分桶。
// 不模糊时比例不影响结果，固定为 0。
type wallpaperKey struct{ opacity, blur, scale int }

func (k wallpaperKey) params() (opacity, blur, scale float32) {
	return float32(k.opacity) / 100, float32(k.blur) / 10, float32(math.Pow(wallpaperScaleStep, float64(k.scale)))
}

type wallpaperRender struct {
	key wallpaperKey
	bmp gfx.Bitmap
	// shared 为真表示 bmp 就是源图本身（不模糊且不透明度 1），不另占内存。
	shared bool
}

// WallpaperCache 是一张壁纸的异步渲染缓存，供显示设置的预览舞台与整窗壁纸各持一份。
//
//   - 文件名变了：后台经 load 读取并解码（限幅后的源图只留一份），完成后调用 wake 请求重绘；读不出来或解不开标记 Failed，界面画损坏的回退态。
//   - 参数（不透明度、模糊、显示比例）变了：后台重新渲染，渲染期间继续返回上一份位图，完成后 wake；参数没变时每帧只在锁里查一次，
//     不拷贝像素、不重新模糊，位图标识固定，原生渲染器不必重新上传。
//   - 已渲染的位图按总字节预算保留最近用到的几份，来回拖动滑块回到旧值时立即命中。
//
// 全部方法并发安全；Bitmap 每帧在界面线程调用，load 与渲染都在后台 goroutine。
type WallpaperCache struct {
	load func(fileName string) ([]byte, error)
	wake func()

	mu      sync.Mutex
	closed  bool
	name    string
	gen     uint64 // 文件名变化或关闭时递增：过期的后台结果据此丢弃
	phase   wallpaperPhase
	err     error
	src     gfx.Bitmap
	renders []*wallpaperRender // 最近用到的在最后
	want    wallpaperKey
	working bool
}

// NewWallpaperCache 创建缓存。load 在后台 goroutine 里调用（可以阻塞）；wake 可为 nil。
func NewWallpaperCache(load func(fileName string) ([]byte, error), wake func()) *WallpaperCache {
	return &WallpaperCache{load: load, wake: wake}
}

func (c *WallpaperCache) notify() {
	if c.wake != nil {
		c.wake()
	}
}

// Bitmap 返回 fileName 在给定参数下的壁纸位图，dstW×dstH 是它将被铺满的目标大小（显示像素，用来换算模糊的像素标准差）。
// ok 为假表示还没有可画的位图（正在读取、读取失败或文件名为空）。参数刚变、新渲染还没好时返回上一份位图。
// 文件名为空会清掉缓存（关闭背景图时释放内存）。
func (c *WallpaperCache) Bitmap(fileName string, opacity, blur, dstW, dstH float32) (bmp gfx.Bitmap, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return gfx.Bitmap{}, false
	}
	if fileName != c.name {
		c.resetLocked(fileName)
	}
	if c.name == "" {
		return gfx.Bitmap{}, false
	}
	key := c.keyLocked(opacity, blur, dstW, dstH)
	c.want = key
	if c.phase != wallpaperReady {
		return gfx.Bitmap{}, false
	}
	if r := c.touchLocked(key); r != nil {
		return r.bmp, true
	}
	c.scheduleLocked()
	if n := len(c.renders); n > 0 {
		return c.renders[n-1].bmp, true // 新参数的渲染还在路上：先用上一份
	}
	return gfx.Bitmap{}, false
}

// Failed 报告当前文件是否读取或解码失败（界面按 LocalImage 的损坏回退态绘制）。
func (c *WallpaperCache) Failed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase == wallpaperFailed
}

// Loading 报告当前文件是否还在读取与解码。
func (c *WallpaperCache) Loading() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase == wallpaperLoading
}

// Err 是失败原因（没有失败时为 nil）；只用于日志与详情。
func (c *WallpaperCache) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Retained 是缓存此刻占住的位图字节数（源图加各份已渲染结果；与源图共用像素的渲染结果不重复计）。
// 没有壁纸（文件名为空、已关闭、读取失败）时为 0；用于确认「关闭背景图就释放内存」，也方便诊断。
func (c *WallpaperCache) Retained() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := len(c.src.Pix)
	for _, r := range c.renders {
		if !r.shared {
			total += len(r.bmp.Pix)
		}
	}
	return total
}

// Close 丢弃全部缓存并让后台结果作废，之后 Bitmap 恒返回 ok 为假；可重复调用。
func (c *WallpaperCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.resetLocked("")
}

func (c *WallpaperCache) resetLocked(name string) {
	c.gen++
	c.name, c.err, c.src, c.renders = name, nil, gfx.Bitmap{}, nil
	c.want, c.working = wallpaperKey{}, false
	c.phase = wallpaperIdle
	if name == "" {
		return
	}
	c.phase = wallpaperLoading
	go c.loadSource(c.gen, name)
}

func (c *WallpaperCache) loadSource(gen uint64, name string) {
	var (
		bmp gfx.Bitmap
		err error
	)
	if c.load == nil {
		err = errors.New("no wallpaper loader is configured")
	} else if data, lerr := c.load(name); lerr != nil {
		err = lerr
	} else {
		bmp, err = DecodeWallpaper(data)
	}
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return
	}
	if err != nil {
		c.phase, c.err = wallpaperFailed, err
	} else {
		bmp.ID, bmp.Version = nextWallpaperID(), 1
		c.src, c.phase = bmp, wallpaperReady // 渲染在下一帧 Bitmap 里按那时的参数与显示比例排程（下面的 wake 会触发这一帧）
	}
	c.mu.Unlock()
	c.notify()
}

// keyLocked 把参数分桶；显示比例 = 目标把源图按 object-cover 铺满所需的倍数（显示像素 ÷ 位图像素）。
func (c *WallpaperCache) keyLocked(opacity, blur, dstW, dstH float32) wallpaperKey {
	key := wallpaperKey{
		opacity: int(math.Round(float64(clampF(opacity, 0, 1, 1)) * 100)),
		blur:    int(math.Round(float64(clampF(blur, 0, wallpaperMaxBlur, 0)) * 10)),
	}
	if key.blur > 0 && validBitmap(c.src) {
		s := math.Max(float64(dstW)/float64(c.src.Width), float64(dstH)/float64(c.src.Height))
		if s > 0 && !math.IsInf(s, 0) && !math.IsNaN(s) {
			key.scale = int(math.Round(math.Log(s) / math.Log(wallpaperScaleStep)))
		}
	}
	return key
}

// touchLocked 找到参数相同的渲染结果并移到最近用到的位置。
func (c *WallpaperCache) touchLocked(key wallpaperKey) *wallpaperRender {
	for i, r := range c.renders {
		if r.key == key {
			copy(c.renders[i:], c.renders[i+1:])
			c.renders[len(c.renders)-1] = r
			return r
		}
	}
	return nil
}

func (c *WallpaperCache) scheduleLocked() {
	if c.working {
		return
	}
	c.working = true
	go c.renderLoop(c.gen)
}

// renderLoop 渲染最近一次要的参数；渲染期间参数又变了就接着渲染新的，同一时刻最多一个渲染在跑。
func (c *WallpaperCache) renderLoop(gen uint64) {
	for {
		c.mu.Lock()
		if c.gen != gen {
			c.mu.Unlock() // 已被重置：working 标记属于新一代，不动
			return
		}
		key, src := c.want, c.src
		if c.touchLocked(key) != nil {
			c.working = false
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		opacity, blur, scale := key.params()
		bmp := RenderWallpaper(src, opacity, blur, scale)
		shared := len(bmp.Pix) > 0 && len(src.Pix) > 0 && &bmp.Pix[0] == &src.Pix[0]
		if shared {
			bmp = src
		} else {
			bmp.ID, bmp.Version = nextWallpaperID(), 1
		}

		c.mu.Lock()
		if c.gen != gen {
			c.mu.Unlock()
			return
		}
		c.renders = append(c.renders, &wallpaperRender{key: key, bmp: bmp, shared: shared})
		c.trimLocked()
		again := c.want != key
		if !again {
			c.working = false
		}
		c.mu.Unlock()
		c.notify()
		if !again {
			return
		}
	}
}

// trimLocked 按字节预算丢掉最久没用的渲染结果（至少保留最新一份）。
func (c *WallpaperCache) trimLocked() {
	total := 0
	for _, r := range c.renders {
		if !r.shared {
			total += len(r.bmp.Pix)
		}
	}
	for total > wallpaperRenderBudget && len(c.renders) > 1 {
		if r := c.renders[0]; !r.shared {
			total -= len(r.bmp.Pix)
		}
		c.renders = c.renders[1:]
	}
}
