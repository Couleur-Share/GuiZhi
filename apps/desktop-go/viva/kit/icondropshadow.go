package kit

import (
	"math"
	"sync"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 图标的 CSS drop-shadow（台账 B-021）
//
// 渲染层给选中色板里的对勾加了 Tailwind 的 `drop-shadow`：
//
//	filter: drop-shadow(0 1px 2px rgb(0 0 0 / .1)) drop-shadow(0 1px 1px rgb(0 0 0 / .06))
//
// CSS 的 drop-shadow() 取图像的 alpha，按「标准差 = 模糊半径的一半」做高斯模糊，平移，染成阴影色，垫在图像下面；
// 多个滤镜依次作用，后一个的输入是「图标加上前一个的阴影」，所以第二层的阴影来自一块稍大一点的轮廓。
// Viva 没有对已绘内容做滤镜的原语，这里用图标的覆盖度（iconraster.go）自己算：一层一层模糊、平移、乘不透明度，
// 再按「最下层的阴影在先、图标在最后」的次序以一像素一矩形的方式画出（源叠加满足结合律，与滤镜先合成再贴到底色上等价）。
// 只复现 DPR 1（逻辑像素即设备像素）；其它缩放按设备像素取整后照算，没有对照过。

// DropShadow 是 CSS filter: drop-shadow(0 DY Blur color) 的一层，阴影色固定为黑。
type DropShadow struct {
	DY    float32 // 竖直偏移（逻辑像素）
	Blur  float32 // 模糊半径（逻辑像素）；高斯标准差取它的一半
	Alpha float32 // 阴影色的不透明度
}

// TailwindDropShadow 是 Tailwind 的 `drop-shadow`（默认档）。
var TailwindDropShadow = []DropShadow{
	{DY: 1, Blur: 2, Alpha: 0.1},
	{DY: 1, Blur: 1, Alpha: 0.06},
}

// IconDropShadow 与 Icon 相同，只是带 CSS drop-shadow。阴影的不透明度与图标一样受祖先的淡入淡出影响。
func (l *Look) IconDropShadow(c ui.C, ic gfx.Icon, size float32, color tokens.Color, shadows []DropShadow) ui.Box {
	col := l.Col(color)
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		mark := l.markIcon(d)
		PaintIconDropShadow(d.Canvas, ic, d.Rect, d.Fade(col), d.Fade(gfx.RGBA(0, 0, 0, 1)), shadows)
		l.reportIcon(d, mark)
	})
}

// PaintIconDropShadow 画一枚图标和它的 drop-shadow；shadowTint 是阴影色（黑色，已带上淡入淡出的不透明度）。
// 不能走精确栅格化的图标（多色、填充）没有覆盖度可用，只画图标本身。
func PaintIconDropShadow(canvas *gfx.Canvas, ic gfx.Icon, bounds gfx.Rect, tint, shadowTint gfx.Color, shadows []DropShadow) {
	if tint.Alpha <= 0 {
		return
	}
	if !rasterizable(ic) || len(shadows) == 0 {
		PaintIcon(canvas, ic, bounds, tint)
		return
	}
	origin, sideDev, n, ok := iconPlacement(canvas, bounds)
	if !ok {
		return
	}
	scale := float64(canvas.PixelScale())
	cov := iconCoverageSide(ic, sideDev, n, 0, 0)
	layers, m, pad := cachedDropShadow(ic, sideDev, n, scale, cov, shadows)
	// 阴影的掩码比图标大一圈（pad 个设备像素），原点相应左上移。
	shadowOrigin := gfx.Point{X: origin.X - float32(pad)/float32(scale), Y: origin.Y - float32(pad)/float32(scale)}
	for k := len(layers) - 1; k >= 0; k-- {
		paintCoverage(canvas, layers[k], m, shadowOrigin, float32(scale), shadowTint)
	}
	paintCoverage(canvas, cov, n, origin, float32(scale), tint)
}

// dropShadowLayers 按滤镜的次序算出每一层阴影的 alpha（0–255，m×m，行优先；图标的覆盖度放在中央，四周各留 pad 个像素）。
func dropShadowLayers(cov []uint8, n int, scale float64, shadows []DropShadow) (layers [][]uint8, m, pad int) {
	for _, s := range shadows {
		pad += int(math.Ceil(float64(s.DY)*scale)) + gaussRadius(float64(s.Blur)/2*scale)
	}
	m = n + 2*pad
	src := make([]float64, m*m)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			src[(y+pad)*m+x+pad] = float64(cov[y*n+x]) / 255
		}
	}
	for _, s := range shadows {
		blurred := gaussBlur(src, m, float64(s.Blur)/2*scale)
		dy := int(math.Round(float64(s.DY) * scale))
		shadow := make([]float64, m*m)
		layer := make([]uint8, m*m)
		for y := 0; y < m; y++ {
			if y-dy < 0 || y-dy >= m {
				continue
			}
			for x := 0; x < m; x++ {
				a := blurred[(y-dy)*m+x] * float64(s.Alpha)
				shadow[y*m+x] = a
				layer[y*m+x] = uint8(math.Round(255 * math.Min(1, a)))
			}
		}
		layers = append(layers, layer)
		// 下一层的输入：图标加上这一层的阴影（源叠加的 alpha）。
		for i := range src {
			src[i] += shadow[i] * (1 - src[i])
		}
	}
	return layers, m, pad
}

// gaussRadius 是高斯核的半径（3 倍标准差向上取整）；标准差不到 0.05 个像素时不模糊。
func gaussRadius(sigma float64) int {
	if sigma < 0.05 {
		return 0
	}
	return int(math.Ceil(3 * sigma))
}

// gaussBlur 对 m×m 的 alpha 做可分离的高斯模糊，核按标准差 sigma 取样并归一，边界之外按 0 处理。
func gaussBlur(src []float64, m int, sigma float64) []float64 {
	r := gaussRadius(sigma)
	if r == 0 {
		return append([]float64(nil), src...)
	}
	kernel := make([]float64, 2*r+1)
	var sum float64
	for i := -r; i <= r; i++ {
		kernel[i+r] = math.Exp(-float64(i*i) / (2 * sigma * sigma))
		sum += kernel[i+r]
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	tmp := make([]float64, m*m)
	out := make([]float64, m*m)
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			var v float64
			for k := -r; k <= r; k++ {
				if xx := x + k; xx >= 0 && xx < m {
					v += src[y*m+xx] * kernel[k+r]
				}
			}
			tmp[y*m+x] = v
		}
	}
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			var v float64
			for k := -r; k <= r; k++ {
				if yy := y + k; yy >= 0 && yy < m {
					v += tmp[yy*m+x] * kernel[k+r]
				}
			}
			out[y*m+x] = v
		}
	}
	return out
}

// 阴影层缓存：同一枚图标、同一设备尺寸、同一组阴影只算一次。
type dropShadowKey struct {
	first  *gfx.PathCommand
	count  int
	side   int // 边长，单位 1/64 设备像素
	n      int
	scale  int // 缩放，单位 1/64
	shadow [4]DropShadow
	layers int
}

type dropShadowEntry struct {
	layers [][]uint8
	m, pad int
}

// dropShadowCacheMax 是缓存条数上限：带阴影的图标种类 × 尺寸极少，超出时整体清空重来。
const dropShadowCacheMax = 128

// dropShadowMaxLayers 是一枚图标最多带几层阴影（缓存键里的数组长度）。
const dropShadowMaxLayers = 4

var dropShadows = struct {
	sync.Mutex
	m map[dropShadowKey]dropShadowEntry
}{m: map[dropShadowKey]dropShadowEntry{}}

func cachedDropShadow(ic gfx.Icon, side float64, n int, scale float64, cov []uint8, shadows []DropShadow) ([][]uint8, int, int) {
	if len(shadows) > dropShadowMaxLayers {
		shadows = shadows[:dropShadowMaxLayers]
	}
	key := dropShadowKey{first: &ic.Commands[0], count: len(ic.Commands), side: int(math.Round(side * 64)), n: n,
		scale: int(math.Round(scale * 64)), layers: len(shadows)}
	copy(key.shadow[:], shadows)
	dropShadows.Lock()
	e, ok := dropShadows.m[key]
	dropShadows.Unlock()
	if ok {
		return e.layers, e.m, e.pad
	}
	layers, m, pad := dropShadowLayers(cov, n, scale, shadows)
	dropShadows.Lock()
	if len(dropShadows.m) >= dropShadowCacheMax {
		dropShadows.m = map[dropShadowKey]dropShadowEntry{}
	}
	dropShadows.m[key] = dropShadowEntry{layers, m, pad}
	dropShadows.Unlock()
	return layers, m, pad
}
