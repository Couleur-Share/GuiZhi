package kit

import (
	"math"
	"sort"
	"sync"

	"github.com/vivaui/viva/gfx"
)

// 图标栅格化
//
// 渲染层（Chromium/Skia）按「路径描边后精确面积覆盖」画 lucide 图标：16px 的图标笔画宽 1.333px，
// 每个设备像素的灰度就是笔画盖住该像素的面积比。Viva 的 DrawIcon / DrawPath 有两处和它不同
// （用 gfx.PathStyle 实测，台账 A-003）：
//   - 笔画宽度被「向上取整到半个设备像素」，1.333 变 1.5；
//   - 半透明笔画（foreground/80 这类）用单轮廓三角剖分，内部三角形不做边缘羽化，
//     细笔画的两条边各被推出约 0.5px，线宽变成近 2px、墨迹多 17%–50%。
// 所以这里自己算覆盖度：把图标缩放到设备像素，折线化路径，用扫描线求每个像素被「到折线的距离 ≤ 半线宽」的带盖住的面积比
// （圆头圆角描边就是路径的等距带，正好是 lucide 的笔画样式），再以一像素一矩形的方式经 Canvas.Fill 画出。
// 覆盖度按（图标，设备像素尺寸）缓存，画一帧只是把缓存里的非零像素合并成行程后发矩形。

const (
	// iconScanlines 是每个像素行的扫描线条数：竖直方向覆盖度的量化为 1/32，水平方向精确。
	iconScanlines = 32
	// iconFlatness 是曲线拉平的最大偏差（设备像素）。
	iconFlatness = 0.02
	// iconRasterCacheMax 是缓存条数上限：图标种类 × 尺寸 × 缩放远小于它，超出时整体清空重来。
	iconRasterCacheMax = 2048
)

// rasterizable 报告图标能否走精确栅格化：lucide 的描边图标（圆头圆角、方形视框、没有填充层）。
// 其余（多色图标、填充图标、非圆头）交还 Viva 原生路径。
func rasterizable(ic gfx.Icon) bool {
	return ic.Stroke > 0 && len(ic.Layers) == 0 && len(ic.Commands) > 0 && ic.ViewBox > 0 &&
		(ic.ViewHeight == 0 || ic.ViewHeight == ic.ViewBox) &&
		ic.Cap == gfx.CapRound && ic.Join == gfx.JoinRound
}

// iconSeg 是拉平后的一条线段（设备像素坐标）；两端点相同表示一个孤点（圆头下画成圆）。
type iconSeg struct{ ax, ay, bx, by float64 }

// flattenIcon 把图标的路径命令按 scale（设备像素 / 视框单位）拉平成线段，原点偏移 (ox, oy) 设备像素。
func flattenIcon(ic gfx.Icon, scale, ox, oy float64) []iconSeg {
	var segs []iconSeg
	var cx, cy, sx, sy float64
	started, hasSeg := false, false
	pt := func(p gfx.Point) (float64, float64) { return float64(p.X)*scale + ox, float64(p.Y)*scale + oy }
	line := func(x, y float64) {
		segs = append(segs, iconSeg{cx, cy, x, y})
		cx, cy = x, y
		hasSeg = true
	}
	finish := func() {
		if started && !hasSeg {
			segs = append(segs, iconSeg{sx, sy, sx, sy}) // 只有 MoveTo 的孤点
		}
		started, hasSeg = false, false
	}
	for _, cmd := range ic.Commands {
		switch cmd.Kind {
		case gfx.PathCommandMove:
			finish()
			cx, cy = pt(cmd.Point)
			sx, sy = cx, cy
			started = true
		case gfx.PathCommandLine:
			if started {
				x, y := pt(cmd.Point)
				line(x, y)
			}
		case gfx.PathCommandQuad:
			if started {
				c1x, c1y := pt(cmd.Control1)
				x, y := pt(cmd.Point)
				flattenQuad(cx, cy, c1x, c1y, x, y, 0, func(px, py float64) { line(px, py) })
			}
		case gfx.PathCommandCubic:
			if started {
				c1x, c1y := pt(cmd.Control1)
				c2x, c2y := pt(cmd.Control2)
				x, y := pt(cmd.Point)
				flattenCubic(cx, cy, c1x, c1y, c2x, c2y, x, y, 0, func(px, py float64) { line(px, py) })
			}
		case gfx.PathCommandClose:
			if started {
				if cx != sx || cy != sy {
					line(sx, sy)
				}
				finish()
			}
		}
	}
	finish()
	return segs
}

// distToChord 是点 (px,py) 到直线 (ax,ay)-(bx,by) 的距离；两点重合时取到该点的距离。
func distToChord(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l := math.Hypot(dx, dy)
	if l < 1e-12 {
		return math.Hypot(px-ax, py-ay)
	}
	return math.Abs(dx*(py-ay)-dy*(px-ax)) / l
}

func flattenQuad(x0, y0, cx, cy, x1, y1 float64, depth int, emit func(x, y float64)) {
	// 二次曲线到弦的最大偏差是控制点到弦距离的一半。
	if depth >= 12 || distToChord(cx, cy, x0, y0, x1, y1)/2 <= iconFlatness {
		emit(x1, y1)
		return
	}
	ax, ay := (x0+cx)/2, (y0+cy)/2
	bx, by := (cx+x1)/2, (cy+y1)/2
	mx, my := (ax+bx)/2, (ay+by)/2
	flattenQuad(x0, y0, ax, ay, mx, my, depth+1, emit)
	flattenQuad(mx, my, bx, by, x1, y1, depth+1, emit)
}

func flattenCubic(x0, y0, c1x, c1y, c2x, c2y, x1, y1 float64, depth int, emit func(x, y float64)) {
	// 三次曲线到弦的最大偏差不超过两个控制点到弦距离的最大值的 3/4。
	d := math.Max(distToChord(c1x, c1y, x0, y0, x1, y1), distToChord(c2x, c2y, x0, y0, x1, y1))
	if depth >= 12 || d*0.75 <= iconFlatness {
		emit(x1, y1)
		return
	}
	ax, ay := (x0+c1x)/2, (y0+c1y)/2
	bx, by := (c1x+c2x)/2, (c1y+c2y)/2
	cx, cy := (c2x+x1)/2, (c2y+y1)/2
	dx, dy := (ax+bx)/2, (ay+by)/2
	ex, ey := (bx+cx)/2, (by+cy)/2
	mx, my := (dx+ex)/2, (dy+ey)/2
	flattenCubic(x0, y0, ax, ay, dx, dy, mx, my, depth+1, emit)
	flattenCubic(mx, my, ex, ey, cx, cy, x1, y1, depth+1, emit)
}

// capsuleSpan 求线段 s 的等距带（半宽 h 的圆头胶囊，凸集）与水平线 y=Y 的交区间；没有交点返回 false。
// 胶囊 = 沿线段的矩形条 ∪ 两端的圆盘，各自与水平线的交都是区间，且彼此相连，取并集的两端即可。
func capsuleSpan(s iconSeg, h, y float64) (lo, hi float64, ok bool) {
	lo, hi = math.Inf(1), math.Inf(-1)
	disc := func(cx, cy float64) {
		if dy := y - cy; math.Abs(dy) <= h {
			r := math.Sqrt(h*h - dy*dy)
			lo, hi = math.Min(lo, cx-r), math.Max(hi, cx+r)
		}
	}
	disc(s.ax, s.ay)
	disc(s.bx, s.by)
	dx, dy := s.bx-s.ax, s.by-s.ay
	if l := math.Hypot(dx, dy); l > 1e-12 {
		ux, uy := dx/l, dy/l
		nx, ny := -uy, ux
		w := y - s.ay
		// x 须同时满足：沿线段方向的投影 ∈ [0, l]，垂直方向的偏移 ∈ [-h, h]；两者都是 x 的一次式。
		xlo, xhi := math.Inf(-1), math.Inf(1)
		clip := func(a, b, from, to float64) {
			if math.Abs(a) < 1e-12 {
				if b < from || b > to {
					xlo, xhi = math.Inf(1), math.Inf(-1)
				}
				return
			}
			p, q := (from-b)/a, (to-b)/a
			if p > q {
				p, q = q, p
			}
			xlo, xhi = math.Max(xlo, p), math.Min(xhi, q)
		}
		clip(ux, -s.ax*ux+w*uy, 0, l)
		clip(nx, -s.ax*nx+w*ny, -h, h)
		if xlo <= xhi {
			lo, hi = math.Min(lo, xlo), math.Max(hi, xhi)
		}
	}
	return lo, hi, lo <= hi
}

// rasterizeIcon 返回图标按 size×size 设备像素缩放后的覆盖度（见 rasterizeIconSide，边长取整数）。
func rasterizeIcon(ic gfx.Icon, size int, ox, oy float64) []uint8 {
	return rasterizeIconSide(ic, float64(size), size, ox, oy)
}

// rasterizeIconSide 返回图标在 n×n 设备像素的掩码里的覆盖度（0–255，行优先）：每个像素被「路径的等距带」盖住的面积比。
// side 是图标的边长（设备像素，可以带小数：渲染层的 SVG 内容按精确的 边长/视框 缩放，边长不取整），n 是掩码边长（≥ side）。
// ox、oy 是图标原点相对像素网格的偏移（设备像素，0 表示落在整像素上）。
//
// 做法是扫描线：每个像素行取 iconScanlines 条水平线，每条线上各线段的等距带是一些区间，合并后按像素格切开累加。
// 水平方向是精确的（竖直边没有量化误差），竖直方向的量化是 1/iconScanlines。
func rasterizeIconSide(ic gfx.Icon, side float64, n int, ox, oy float64) []uint8 {
	cov := make([]uint8, n*n)
	scale := side / float64(ic.ViewBox)
	half := float64(ic.Stroke) * scale / 2
	segs := flattenIcon(ic, scale, ox, oy)
	if len(segs) == 0 || half <= 0 {
		return cov
	}
	type span struct{ lo, hi float64 }
	spans := make([]span, 0, len(segs))
	acc := make([]float64, n)
	for y := 0; y < n; y++ {
		for i := range acc {
			acc[i] = 0
		}
		for k := 0; k < iconScanlines; k++ {
			sy := float64(y) + (float64(k)+0.5)/iconScanlines
			spans = spans[:0]
			for _, s := range segs {
				if sy < math.Min(s.ay, s.by)-half || sy > math.Max(s.ay, s.by)+half {
					continue
				}
				if lo, hi, ok := capsuleSpan(s, half, sy); ok {
					spans = append(spans, span{lo, hi})
				}
			}
			if len(spans) == 0 {
				continue
			}
			sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
			cur := spans[0]
			flush := func(sp span) {
				lo, hi := math.Max(sp.lo, 0), math.Min(sp.hi, float64(n))
				for x := int(math.Floor(lo)); x < n && float64(x) < hi; x++ {
					acc[x] += math.Min(hi, float64(x+1)) - math.Max(lo, float64(x))
				}
			}
			for _, sp := range spans[1:] {
				if sp.lo <= cur.hi {
					cur.hi = math.Max(cur.hi, sp.hi)
				} else {
					flush(cur)
					cur = sp
				}
			}
			flush(cur)
		}
		for x := 0; x < n; x++ {
			v := math.Round(255 * acc[x] / iconScanlines)
			cov[y*n+x] = uint8(math.Min(255, math.Max(0, v)))
		}
	}
	return cov
}

// 覆盖度缓存：同一枚图标在同一设备尺寸下只算一次。
type iconRasterKey struct {
	first *gfx.PathCommand // 命令切片首元素的地址，标识一枚（包级只读的）图标
	count int
	side  int  // 边长，单位 1/64 设备像素
	n     int  // 掩码边长
	ox    int8 // 原点偏移，单位 1/16 设备像素
	oy    int8
}

var iconRasters = struct {
	sync.Mutex
	m map[iconRasterKey][]uint8
}{m: map[iconRasterKey][]uint8{}}

// iconCoverage 取（必要时计算并缓存）图标按整数边长缩放的覆盖度。
func iconCoverage(ic gfx.Icon, size int, ox, oy float64) []uint8 {
	return iconCoverageSide(ic, float64(size), size, ox, oy)
}

// iconCoverageSide 取（必要时计算并缓存）图标在 n×n 掩码里、按边长 side（设备像素，可带小数）缩放的覆盖度。
func iconCoverageSide(ic gfx.Icon, side float64, n int, ox, oy float64) []uint8 {
	key := iconRasterKey{first: &ic.Commands[0], count: len(ic.Commands), side: int(math.Round(side * 64)), n: n,
		ox: int8(math.Round(ox * 16)), oy: int8(math.Round(oy * 16))}
	iconRasters.Lock()
	cov, ok := iconRasters.m[key]
	iconRasters.Unlock()
	if ok {
		return cov
	}
	cov = rasterizeIconSide(ic, side, n, float64(key.ox)/16, float64(key.oy)/16)
	iconRasters.Lock()
	if len(iconRasters.m) >= iconRasterCacheMax {
		iconRasters.m = map[iconRasterKey][]uint8{}
	}
	iconRasters.m[key] = cov
	iconRasters.Unlock()
	return cov
}

// paintCoverage 把覆盖度按一像素一矩形画到 origin（逻辑坐标，已落在整设备像素上）；同一行里覆盖度相同的相邻像素合并成一个矩形。
func paintCoverage(canvas *gfx.Canvas, cov []uint8, size int, origin gfx.Point, scale float32, tint gfx.Color) {
	unit := 1 / scale
	for y := 0; y < size; y++ {
		row := cov[y*size : (y+1)*size]
		for x := 0; x < size; {
			c := row[x]
			if c == 0 {
				x++
				continue
			}
			end := x + 1
			for end < size && row[end] == c {
				end++
			}
			color := tint
			color.Alpha = tint.Alpha * float32(c) / 255
			canvas.Fill(gfx.Rect{
				X: origin.X + float32(x)*unit, Y: origin.Y + float32(y)*unit,
				Width: float32(end-x) * unit, Height: unit,
			}, color)
			x = end
		}
	}
}
