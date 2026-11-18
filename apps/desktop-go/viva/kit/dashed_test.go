package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 118×32、圆角 12 的边框盒（浅色、16px 档下的「发现新版本」按钮）：渲染层的实测虚线周期是 278/56 ≈ 4.9643。
// 解析弧长是 279.4，按它算出的周期是 4.982，对不上；Skia 的折线测量是 278.9，截断成 278 才对得上。
func TestDashContourMeasuresLikeSkia(t *testing.T) {
	c := newDashContour(118, 32, 12, 0)
	if c.total >= 279 || c.total < 278 {
		t.Fatalf("折线测量的轮廓长度应在 [278, 279) 内（解析值 279.4 会截成 279），得到 %.3f", c.total)
	}
	dash, gap, ok := dashPeriod(float32(int(c.total)), 1)
	if !ok {
		t.Fatal("放得下虚线，不应退化成实线")
	}
	if dash != 3 {
		t.Fatalf("线宽 1 时一段应是 3 倍线宽，得到 %v", dash)
	}
	period := float64(dash + gap)
	if math.Abs(period-278.0/56) > 1e-4 {
		t.Fatalf("周期应为 278/56 ≈ 4.9643，得到 %.4f", period)
	}
}

// 一段圆角在 Skia 里是四条弦：t = 0、¼、½、¾、1（半径 12 时每次二分后弦高仍超过 0.5，直到四分之一弧才够平）。
func TestArcKnotsSplitIntoFourChords(t *testing.T) {
	knots := arcKnots(12)
	if len(knots) != 5 {
		t.Fatalf("半径 12 的圆角应压成 4 条弦（5 个折点），得到 %d 个：%v", len(knots), knots)
	}
	for i, want := range []float64{0, 0.25, 0.5, 0.75, 1} {
		if math.Abs(knots[i].t-want) > 1e-9 {
			t.Fatalf("折点 %d 的参数应为 %v，得到 %v", i, want, knots[i].t)
		}
	}
	// 弦长和比解析弧长（6π ≈ 18.85）略短。
	if got := knots[4].dist; got >= 12*math.Pi/2 || got < 18.5 {
		t.Fatalf("四条弦之和应略短于解析弧长 18.85，得到 %.3f", got)
	}
}

// 小圆角不用细分：半径 1 的圆角中点离弦中点不到 0.5，一条弦就够。
func TestArcKnotsSmallRadiusIsOneChord(t *testing.T) {
	if got := len(arcKnots(1)); got != 2 {
		t.Fatalf("半径 1 的圆角应是一条弦，得到 %d 个折点", got)
	}
}

// 闭合轮廓：先跳过第一段，走完一圈再补；最后一段若被轮廓末端截断，就与补上的第一段连成一条。
func TestDashSpansJoinLastAndFirst(t *testing.T) {
	total := 278.91
	gap := float64(float32(110.0 / 56))
	spans := dashSpans(total, 3, gap)
	if len(spans) != 56 {
		t.Fatalf("278 长、周期 4.9643 的闭合轮廓应有 56 条虚线，得到 %d", len(spans))
	}
	first, last := spans[0], spans[len(spans)-1]
	if math.Abs(first[0]-(3+gap)) > 1e-6 {
		t.Fatalf("循环里的第一条是原来的第二段（起点 %.4f），得到 %.4f", 3+gap, first[0])
	}
	if math.Abs(last[0]-278) > 1e-3 {
		t.Fatalf("尾段应从 278 开始，得到 %.4f", last[0])
	}
	if math.Abs(last[1]-(total+3)) > 1e-9 {
		t.Fatalf("尾段应越过起点接上第一段（止点 %.4f），得到 %.4f", total+3, last[1])
	}
}

// 轮廓末端正好落在间隔里时，第一段单独补在最后，不与尾段相连。
func TestDashSpansFirstStaysSeparateAfterGap(t *testing.T) {
	spans := dashSpans(9.5, 3, 2) // 段 [0,3] 跳过，[5,8] 在循环里，8..9.5 是间隔，最后补 [0,3]
	if len(spans) != 2 {
		t.Fatalf("应有 2 条虚线，得到 %v", spans)
	}
	if spans[0] != [2]float64{5, 8} || spans[1] != [2]float64{0, 3} {
		t.Fatalf("虚线位置不对：%v", spans)
	}
}

// 轮廓上的点必须真在边框盒的外沿上：直边贴着盒边，圆角离圆心恰好一个半径；法线单位长度、指向盒外。
func TestDashContourPointsStayOnOuterEdge(t *testing.T) {
	const w, h, r = 118.0, 32.0, 12.0
	for first := 0; first < 8; first++ {
		c := newDashContour(w, h, r, first)
		for d := 0.0; d < c.total; d += 0.37 {
			px, py, nx, ny := c.at(d)
			if math.Abs(math.Hypot(nx, ny)-1) > 1e-9 {
				t.Fatalf("起点 %d，d=%.2f：法线不是单位向量 (%.3f, %.3f)", first, d, nx, ny)
			}
			// 到圆角矩形边界的距离：向内收 r 的矩形里最近点到 (px,py) 的距离应等于 r。
			cx := math.Min(math.Max(px, r), w-r)
			cy := math.Min(math.Max(py, r), h-r)
			if dist := math.Hypot(px-cx, py-cy); math.Abs(dist-r) > 1e-6 {
				t.Fatalf("起点 %d，d=%.2f：点 (%.3f, %.3f) 不在外沿上（离内缩矩形 %.4f，应为 %v）", first, d, px, py, dist, r)
			}
			// 沿法线向外走一点应当出盒，向内走一点应当入盒。
			ox, oy := px+nx*0.01, py+ny*0.01
			ix, iy := px-nx*0.01, py-ny*0.01
			outside := math.Hypot(ox-math.Min(math.Max(ox, r), w-r), oy-math.Min(math.Max(oy, r), h-r)) > r
			inside := math.Hypot(ix-math.Min(math.Max(ix, r), w-r), iy-math.Min(math.Max(iy, r), h-r)) < r
			if !outside || !inside {
				t.Fatalf("起点 %d，d=%.2f：法线没有指向盒外（外 %v 内 %v）", first, d, outside, inside)
			}
		}
	}
}

// 起点决定虚线从哪里开始：第 0 段是顶边左端，第 6 段是左边下端（旧模型的起点）。
func TestDashContourStartPoints(t *testing.T) {
	top := newDashContour(100, 40, 10, 0)
	if x, y, _, _ := top.at(0); math.Abs(x-10) > 1e-9 || math.Abs(y) > 1e-9 {
		t.Fatalf("第 0 段应从顶边左端 (10,0) 出发，得到 (%.3f, %.3f)", x, y)
	}
	left := newDashContour(100, 40, 10, 6)
	if x, y, _, _ := left.at(0); math.Abs(x) > 1e-9 || math.Abs(y-30) > 1e-9 {
		t.Fatalf("第 6 段应从左边下端 (0,30) 出发，得到 (%.3f, %.3f)", x, y)
	}
	if math.Abs(top.total-left.total) > 1e-9 {
		t.Fatalf("换起点不应改变总长：%v 对 %v", top.total, left.total)
	}
}

// 药丸形（圆角取到一半）与无圆角都要能建轮廓：直边长度为零的段不能让取点出错。
func TestDashContourDegenerateShapes(t *testing.T) {
	for _, tc := range []struct{ w, h, r float64 }{{40, 20, 10}, {40, 20, 99}, {40, 20, 0}} {
		c := newDashContour(tc.w, tc.h, tc.r, 0)
		for d := 0.0; d < c.total; d += 0.5 {
			px, py, _, _ := c.at(d)
			if math.IsNaN(px) || math.IsNaN(py) || px < -1e-6 || py < -1e-6 || px > tc.w+1e-6 || py > tc.h+1e-6 {
				t.Fatalf("%+v：d=%.1f 取到越界或非数值的点 (%v, %v)", tc, d, px, py)
			}
		}
	}
}

// inDash 报告测量距离 d 是否落在某条虚线上（越过起点的那条连接线按模走一圈）。
func inDash(spans [][2]float64, total, d float64) bool {
	for _, s := range spans {
		if (d >= s[0] && d < s[1]) || (s[1] > total && d+total >= s[0] && d+total < s[1]) {
			return true
		}
	}
	return false
}

// 整条直边上每个像素的覆盖率必须与模型一致：这是渲染层逐像素比对（台账 B-006）确认过的节奏——
// 一段 3 像素、周期 278/56，起点在顶边左端，走一圈后第一段与尾段相连。
// 水平方向的覆盖率 Viva 算得精确，所以顶边与底边的容差很小；竖直方向 Viva 只有 4 条子扫描线，左右两边的容差放宽到 1/8。
// 圆角里 Viva 的路径栅格化对弯曲边偏重（最多多算 0.34 的覆盖率，台账 A-021），不在这里比。
func TestDashedBorderPhaseOnStraightEdges(t *testing.T) {
	const w, h, r, ox, oy = 118, 32, 12, 6, 6
	hn := openPage(t, still(), 130, 44, func(l *Look, c ui.C) {
		ui.Column(c).PadLeft(ox).PadTop(oy).Background(gfx.RGBA(255, 255, 255, 1)).Children(func() {
			ui.Row(c).Size(w, h).Draw(func(d *ui.DrawContext) {
				l.PaintDashedBorder(d, tokens.RGBA(0, 0, 0, 1), 1, r)
			})
		})
	})
	img, err := hn.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	contour := newDashContour(w, h, r, 0)
	dash, gap, ok := dashPeriod(float32(int(contour.total)), 1)
	if !ok {
		t.Fatal("放得下虚线")
	}
	spans := dashSpans(contour.total, float64(dash), float64(gap))
	// 每条边：像素坐标 → 测量距离（第 0、2、4、6 段分别是顶边、右边、底边、左边）。
	edges := []struct {
		name   string
		pixels func(i int) (x, y int)
		count  int
		dist   func(u float64) float64 // u 是像素内沿前进方向的连续坐标（从该边的起点像素边界起算）
		tol    float64
	}{
		{"顶边", func(i int) (int, int) { return r + i, 0 }, w - 2*r, func(u float64) float64 { return contour.segs[0].start + u }, 0.03},
		{"右边", func(i int) (int, int) { return w - 1, r + i }, h - 2*r, func(u float64) float64 { return contour.segs[2].start + u }, 0.13},
		{"底边", func(i int) (int, int) { return w - r - 1 - i, h - 1 }, w - 2*r, func(u float64) float64 { return contour.segs[4].start + u }, 0.03},
		{"左边", func(i int) (int, int) { return 0, h - r - 1 - i }, h - 2*r, func(u float64) float64 { return contour.segs[6].start + u }, 0.13},
	}
	for _, e := range edges {
		for i := 0; i < e.count; i++ {
			px, py := e.pixels(i)
			// 该像素沿前进方向占 [i, i+1]：按 256 个子样本数落在虚线上的比例。
			const n = 256
			hit := 0
			for k := 0; k < n; k++ {
				if inDash(spans, contour.total, e.dist(float64(i)+(float64(k)+0.5)/n)) {
					hit++
				}
			}
			want := float64(hit) / n
			rr, gg, bb, _ := img.At(ox+px, oy+py).RGBA()
			got := 1 - float64(rr+gg+bb)/3/65535
			if math.Abs(got-want) > e.tol {
				t.Errorf("%s 第 %d 个像素 (%d,%d)：覆盖率 %.2f，模型 %.2f（容差 %.2f）", e.name, i, px, py, got, want, e.tol)
			}
		}
	}
}
