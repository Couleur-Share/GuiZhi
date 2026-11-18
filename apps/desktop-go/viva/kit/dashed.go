package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 虚线边框（CSS border-style: dashed）
//
// 照 Chromium 的画法逐项复现（blink 的 BoxBorderPainter 与 StrokeData，Skia 的 SkDashPathEffect 与 SkContourMeasure；
// 台账 B-006，用渲染层的真实像素反推并核对）：
//  1. 路径是整个边框盒的外沿圆角矩形（不是边框环的中线）：沿它描边，线宽取边框宽的 2.2 倍，再被边框环裁掉外侧，
//     所以虚线实际盖住「外沿向内一个边框宽」那一圈；一段虚线的两端是路径的法线。
//  2. 节奏由边框宽度定：宽度不到 3 时一段 3 倍线宽、间隔 2 倍线宽，到 3 以上时一段 2 倍、间隔 1 倍；
//     再把间隔微调到整条闭合路径恰好放下整数段（StrokeData::SelectBestDashGap，闭合路径每段后面都跟一个间隔）。
//  3. 路径长度不是解析弧长：Skia 的 SkContourMeasure 把圆角压成折线再量（弦高容差 0.5，递归二分曲线参数），所以比解析值略短；
//     Blink 又把这个长度按 int 截断后才交给 SelectBestDashGap。「段数」与「间隔」取决于截断后的整数，
//     这是此前（拿中线、按解析弧长算）虚线对不上渲染层的根源。
//  4. 起点与方向见 dashFirstSegment；闭合轮廓的第一段先跳过，走完一圈再补在最后：若最后一段恰好被轮廓末端截断，
//     两者连成一条更长的虚线（SkDashPath::InternalFilter）。
// Viva 自带的 PathStyle.Dashed 是 6:4 的固定节奏，与 Chromium 不同，所以这里自己算段，每段画成一个多边形。
// 只复现 DPR 1（逻辑像素即设备像素）：高 DPR 下 Chromium 在设备像素里量长度，节奏会不同，见台账。

// dashFirstSegment 是路径起点所在的段：0 = 顶边（左端，向右），顺时针依次是右上角、右边、右下角、底边、左下角、左边、左上角。
// 取值依据：与渲染层的像素逐一比对（台账 B-006）。
const dashFirstSegment = 0

// dashRatios 返回线宽对应的一段与一个间隔的长度（StrokeData::DashLengthRatio / DashGapRatio）。
func dashRatios(width float32) (dash, gap float32) {
	if width >= 3 {
		return width * 2, width
	}
	return width * 3, width * 2
}

// bestDashGap 是 StrokeData::SelectBestDashGap（闭合路径）：在「段数」的两个候选里选使间隔最接近目标值的那个。
func bestDashGap(length, dash, gap float32) float32 {
	minDashes := float32(math.Floor(float64(length / (dash + gap))))
	maxDashes := minDashes + 1
	minGap := (length - minDashes*dash) / minDashes
	maxGap := (length - maxDashes*dash) / maxDashes
	if maxGap <= 0 || abs32(minGap-gap) < abs32(maxGap-gap) {
		return minGap
	}
	return maxGap
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// dashPeriod 返回长为 length 的闭合路径上的一段与间隔的长度（StrokeData::SetupPaintDashPathEffect）。
// length 是已按 int 截断的路径长度。放不下两段时返回 ok=false（Chromium 此时画实线）。
func dashPeriod(length, width float32) (dash, gap float32, ok bool) {
	dash, gap = dashRatios(width)
	if length <= dash*2 {
		return 0, 0, false
	}
	twoDashes := 2*dash + 2*gap // 闭合路径：两段加两个间隔
	if length <= twoDashes {
		k := length / twoDashes
		return dash * k, gap * k, true
	}
	return dash, bestDashGap(length, dash, gap), true
}

// contourKnot 是圆角在 Skia 测量里的一个折点：圆锥曲线参数 t 与从圆角起点起算的折线累计距离。
type contourKnot struct{ t, dist float64 }

// conicPoint 取圆角在局部坐标里的点：圆心在原点，起点 (r,0)，终点 (0,r)，角度增大即屏幕上的顺时针；
// 曲线是权重 √2/2 的圆锥曲线（Skia 的 addRRect 就是这样画圆角的），控制点 (r,0)、(r,r)、(0,r)。
func conicPoint(r, t float64) (x, y float64) {
	const w = math.Sqrt2 / 2
	a := (1 - t) * (1 - t)
	b := 2 * t * (1 - t) * w
	c := t * t
	den := a + b + c
	return r * (a + b) / den, r * (b + c) / den
}

// arcKnots 复现 SkContourMeasure 对一段圆锥曲线的测量（compute_conic_segs）：二分 t 区间，
// 直到曲线中点与弦中点的偏差（x、y 方向取较大者）不超过 0.5，再把每条弦的长度累加。
func arcKnots(r float64) []contourKnot {
	const tolerance = 0.5
	const maxDepth = 20 // Skia 的 tspan_big_enough：t 的跨度小于 2^-20 就不再细分
	knots := []contourKnot{{0, 0}}
	var split func(t0, t1, x0, y0, x1, y1, dist float64, depth int) float64
	split = func(t0, t1, x0, y0, x1, y1, dist float64, depth int) float64 {
		tm := (t0 + t1) / 2
		xm, ym := conicPoint(r, tm)
		if depth < maxDepth && math.Max(math.Abs(xm-(x0+x1)/2), math.Abs(ym-(y0+y1)/2)) > tolerance {
			dist = split(t0, tm, x0, y0, xm, ym, dist, depth+1)
			return split(tm, t1, xm, ym, x1, y1, dist, depth+1)
		}
		dist += math.Hypot(x1-x0, y1-y0)
		knots = append(knots, contourKnot{t1, dist})
		return dist
	}
	x1, y1 := conicPoint(r, 1)
	split(0, 1, r, 0, x1, y1, 0, 0)
	return knots
}

// dashSeg 是闭合轮廓的一段：直边（起点与单位前进方向）或圆角（圆心与起始角）。
type dashSeg struct {
	arc    bool
	start  float64 // 该段起点的累计测量距离
	length float64 // 该段的测量长度
	x, y   float64 // 直边的起点
	dx, dy float64 // 直边的单位前进方向
	cx, cy float64 // 圆角的圆心
	theta  float64 // 圆角的起始角（局部坐标旋转到屏幕坐标）
}

// dashContour 是边框盒外沿的圆角矩形轮廓，按 Skia 的测量方式给出累计距离与位置。
type dashContour struct {
	r     float64
	segs  [8]dashSeg
	knots []contourKnot
	total float64
}

// newDashContour 建立 w×h、圆角 r 的轮廓，起点在第 first 段（0 = 顶边左端，顺时针）。
func newDashContour(w, h, r float64, first int) dashContour {
	r = min(max(r, 0), min(w, h)/2)
	c := dashContour{r: r}
	arcLen := 0.0
	if r > 0 {
		c.knots = arcKnots(r)
		arcLen = c.knots[len(c.knots)-1].dist
	}
	order := [8]dashSeg{
		{x: r, y: 0, dx: 1, dy: 0, length: w - 2*r},                           // 顶边向右
		{arc: true, cx: w - r, cy: r, theta: 3 * math.Pi / 2, length: arcLen}, // 右上角
		{x: w, y: r, dx: 0, dy: 1, length: h - 2*r},                           // 右边向下
		{arc: true, cx: w - r, cy: h - r, theta: 0, length: arcLen},           // 右下角
		{x: w - r, y: h, dx: -1, dy: 0, length: w - 2*r},                      // 底边向左
		{arc: true, cx: r, cy: h - r, theta: math.Pi / 2, length: arcLen},     // 左下角
		{x: 0, y: h - r, dx: 0, dy: -1, length: h - 2*r},                      // 左边向上
		{arc: true, cx: r, cy: r, theta: math.Pi, length: arcLen},             // 左上角
	}
	for i := range c.segs {
		s := order[(first+i)%8]
		s.start = c.total
		c.segs[i] = s
		c.total += s.length
	}
	return c
}

// at 返回测量距离 d（对 total 取模，闭合）处的位置与向外的单位法线（屏幕坐标，y 向下）。
func (c dashContour) at(d float64) (px, py, nx, ny float64) {
	d = math.Mod(d, c.total)
	if d < 0 {
		d += c.total
	}
	idx := len(c.segs) - 1
	for i, s := range c.segs {
		if d < s.start+s.length {
			idx = i
			break
		}
	}
	s := c.segs[idx]
	u := d - s.start
	if !s.arc {
		// 顺时针前进时，向外的法线是前进方向的左手边：(dy, -dx)。
		return s.x + s.dx*u, s.y + s.dy*u, s.dy, -s.dx
	}
	// 圆角：按折点表把距离换成曲线参数 t（弦内线性插值，和 Skia 的 getSegment 一样），再取曲线上的点。
	k := 0
	for k+2 < len(c.knots) && u > c.knots[k+1].dist {
		k++
	}
	k0, k1 := c.knots[k], c.knots[k+1]
	t := k0.t
	if k1.dist > k0.dist {
		t += (k1.t - k0.t) * (u - k0.dist) / (k1.dist - k0.dist)
	}
	lx, ly := conicPoint(c.r, math.Min(math.Max(t, 0), 1))
	sn, cs := math.Sincos(s.theta)
	rx, ry := lx*cs-ly*sn, lx*sn+ly*cs
	return s.cx + rx, s.cy + ry, rx / c.r, ry / c.r
}

// dashSpans 按 SkDashPath::InternalFilter 对闭合轮廓的处理给出每段虚线的 [起, 止] 测量距离：
// 第一段先跳过，走完一圈再补；若最后一次落在「段」上（被轮廓末端截断），就与补上的第一段连成一条，此时止点会超过 total。
func dashSpans(total float64, dash, gap float64) [][2]float64 {
	var spans [][2]float64
	interval := [2]float64{dash, gap}
	dist, index, length := 0.0, 0, dash
	skipFirst, added := true, false
	for dist < total {
		added = false
		if index == 0 && !skipFirst {
			added = true
			spans = append(spans, [2]float64{dist, math.Min(dist+length, total)})
		}
		dist += length
		skipFirst = false
		index = 1 - index
		length = interval[index]
	}
	if added {
		spans[len(spans)-1][1] = total + dash
	} else {
		spans = append(spans, [2]float64{0, dash})
	}
	return spans
}

// PaintDashedBorder 在 d.Rect 的边框环里画 Chromium 风格的虚线（border-dashed）。
// 在 Box.Draw 里调用，画在底色之上；color 是令牌（可带透明度）。
func (l *Look) PaintDashedBorder(d *ui.DrawContext, color tokens.Color, width, radius float32) {
	if width <= 0 || color.A <= 0 {
		return
	}
	scale := d.Canvas.PixelScale()
	outer := SnapRect(d.Rect, scale)
	if outer.Width <= 2*width || outer.Height <= 2*width {
		return
	}
	contour := newDashContour(float64(outer.Width), float64(outer.Height), float64(radius), dashFirstSegment)
	// Blink 把路径长度按 int 截断后才算节奏。
	dash, gap, ok := dashPeriod(float32(int(contour.total)), width)
	tint := d.Fade(l.Col(color))
	if !ok {
		d.Canvas.RoundedOutline(outer, radius, width, tint)
		return
	}
	builder := d.Canvas.PathBuilder()
	for _, span := range dashSpans(contour.total, float64(dash), float64(gap)) {
		contour.dashPolygon(builder, span[0], span[1], float64(width))
	}
	d.Canvas.DrawPath(builder.Path(outer.Width, outer.Height), outer, gfx.PathStyle{Fill: tint})
}

// dashPolygon 把测量距离 [d0, d1] 这一段画成一个闭合多边形：外沿取路径本身，内沿取向内偏出 width（正好是边框环）。
func (c dashContour) dashPolygon(b *gfx.PathBuilder, d0, d1, width float64) {
	// 取样：两端，加上跨过的各段起点与圆角折点；圆角内再按约 1 像素加密，让多边形贴着曲线。
	samples := []float64{d0}
	for lap := 0.0; lap*c.total < d1; lap++ {
		base := lap * c.total
		for _, s := range c.segs {
			if s.length <= 0 {
				continue
			}
			if !s.arc {
				if p := base + s.start; p > d0 && p < d1 {
					samples = append(samples, p)
				}
				continue
			}
			steps := max(1, int(math.Ceil(s.length)))
			for i := 0; i < steps; i++ {
				if p := base + s.start + s.length*float64(i)/float64(steps); p > d0 && p < d1 {
					samples = append(samples, p)
				}
			}
		}
	}
	samples = append(samples, d1)
	type vertex struct{ ox, oy, ix, iy float32 }
	verts := make([]vertex, 0, len(samples))
	for i, d := range samples {
		if i > 0 && d-samples[i-1] < 1e-6 {
			continue
		}
		px, py, nx, ny := c.at(d)
		verts = append(verts, vertex{float32(px), float32(py), float32(px - nx*width), float32(py - ny*width)})
	}
	b.MoveTo(verts[0].ox, verts[0].oy)
	for _, v := range verts[1:] {
		b.LineTo(v.ox, v.oy)
	}
	for i := len(verts) - 1; i >= 0; i-- {
		b.LineTo(verts[i].ix, verts[i].iy)
	}
	b.Close()
}
