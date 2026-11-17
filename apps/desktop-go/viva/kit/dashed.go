package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 虚线边框（CSS border-style: dashed）
//
// Chromium 的画法（blink 的 StrokeData 与 BoxBorderPainter）：边框环的中线是一条圆角矩形路径，沿它描边，
// 线宽等于边框宽度，平头端点；虚线的节奏由边框宽度定——宽度不到 3 时一段 3 倍线宽、间隔 2 倍线宽，
// 到 3 以上时一段 2 倍、间隔 1 倍——再把间隔微调到整条路径恰好放下整数段（SelectBestDashGap，闭合路径每段后面都跟一个间隔）。
// 路径从左下角圆角的上端点出发，顺时针绕行（Skia 的 addRRect 默认起点）。
// Viva 自带的 PathStyle.Dashed 是 6:4 的固定节奏，与 Chromium 不同，所以这里自己算段，每段画成一个多边形。

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
// 放不下两段时返回 ok=false（Chromium 此时画实线）。
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

// rrectPath 是圆角矩形中线，按路径参数 s（弧长）取点与前进方向。
type rrectPath struct {
	x, y, w, h, r float32
	total         float32
	// 各段（直边、圆角交替）的起点弧长：left, TL, top, TR, right, BR, bottom, BL。
	starts [9]float32
}

func newRRectPath(x, y, w, h, r float32) rrectPath {
	r = min(max(r, 0), min(w, h)/2)
	p := rrectPath{x: x, y: y, w: w, h: h, r: r}
	arc := float32(math.Pi) / 2 * r
	lens := [8]float32{h - 2*r, arc, w - 2*r, arc, h - 2*r, arc, w - 2*r, arc}
	for i, n := range lens {
		p.starts[i+1] = p.starts[i] + n
	}
	p.total = p.starts[8]
	return p
}

// at 返回弧长 s 处的位置与单位前进方向（屏幕坐标，y 向下；顺时针）。
func (p rrectPath) at(s float32) (px, py, tx, ty float32) {
	seg := 7
	for i := 0; i < 8; i++ {
		if s < p.starts[i+1] {
			seg = i
			break
		}
	}
	u := s - p.starts[seg]
	x, y, w, h, r := p.x, p.y, p.w, p.h, p.r
	arcAt := func(cx, cy, theta0 float32) (float32, float32, float32, float32) {
		theta := float64(theta0) + float64(u/r)
		sn, cs := math.Sincos(theta)
		// 顺时针（屏幕）：角度增大，前进方向是 (-sin, cos)。
		return cx + r*float32(cs), cy + r*float32(sn), float32(-sn), float32(cs)
	}
	switch seg {
	case 0: // 左边向上
		return x, y + h - r - u, 0, -1
	case 1: // 左上角
		return arcAt(x+r, y+r, math.Pi)
	case 2: // 上边向右
		return x + r + u, y, 1, 0
	case 3: // 右上角
		return arcAt(x+w-r, y+r, 3*math.Pi/2)
	case 4: // 右边向下
		return x + w, y + r + u, 0, 1
	case 5: // 右下角
		return arcAt(x+w-r, y+h-r, 0)
	case 6: // 下边向左
		return x + w - r - u, y + h, -1, 0
	default: // 左下角
		return arcAt(x+r, y+h-r, math.Pi/2)
	}
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
	half := width / 2
	// 中线：边框盒向内缩半个线宽（坐标相对外框左上角，视框与外框同大）。
	line := newRRectPath(half, half, outer.Width-width, outer.Height-width, radius-half)
	dash, gap, ok := dashPeriod(line.total, width)
	tint := d.Fade(l.Col(color))
	if !ok {
		d.Canvas.RoundedOutline(outer, radius, width, tint)
		return
	}
	builder := d.Canvas.PathBuilder()
	for s := float32(0); s < line.total-0.01; s += dash + gap {
		line.dashContour(builder, s, min(s+dash, line.total), half)
	}
	d.Canvas.DrawPath(builder.Path(outer.Width, outer.Height), outer, gfx.PathStyle{Fill: tint})
}

// dashContour 把弧长 [s0, s1] 这一段画成一个闭合多边形：沿中线两侧各偏出 half。
func (p rrectPath) dashContour(b *gfx.PathBuilder, s0, s1, half float32) {
	// 取样点：两端，加上跨过的各段起点，圆角段内每 1 像素一个。
	samples := []float32{s0}
	for i := 1; i < 8; i++ {
		if p.starts[i] > s0 && p.starts[i] < s1 {
			samples = append(samples, p.starts[i])
		}
	}
	samples = append(samples, s1)
	var pts []float32 // 展开成 s 序列
	for i := 0; i+1 < len(samples); i++ {
		a, c := samples[i], samples[i+1]
		pts = append(pts, a)
		if p.isArc(a, c) {
			for s := a + 1; s < c-0.25; s++ {
				pts = append(pts, s)
			}
		}
	}
	pts = append(pts, s1)
	type vertex struct{ ox, oy, ix, iy float32 }
	verts := make([]vertex, len(pts))
	for i, s := range pts {
		// 圆角矩形在直边与圆角的交接处切线连续，所以段边界上取哪一侧的方向结果相同。
		px, py, tx, ty := p.at(s)
		// 向外的法线是前进方向的左手边：(ty, -tx)。
		verts[i] = vertex{px + ty*half, py - tx*half, px - ty*half, py + tx*half}
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

// isArc 报告弧长区间 [a, c] 是否落在某个圆角段内。
func (p rrectPath) isArc(a, c float32) bool {
	mid := (a + c) / 2
	for i := 1; i < 8; i += 2 {
		if mid >= p.starts[i] && mid <= p.starts[i+1] {
			return true
		}
	}
	return false
}
