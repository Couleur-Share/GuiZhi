package parity

import (
	"fmt"
	"image"
	"math"
)

// compareL2 比较度量：已对齐节点的外框，以及已对齐文字的行数、宽度与墨迹外接框。
func compareL2(rep *Report, in Inputs, th Thresholds, nodes []nodePair, texts []textPair) {
	maxDelta := th.Metrics.MaxDeltaPx

	// 外框：位置与尺寸各自的差都不超过阈值。
	for _, p := range nodes {
		if !p.React.HasBox || !p.Viva.HasBox {
			continue
		}
		rep.L2.BoxPairs++
		d := boxDelta(p.React.Box, p.Viva.Box)
		rep.L2.MaxBoxDelta = math.Max(rep.L2.MaxBoxDelta, d)
		if d <= maxDelta+1e-9 {
			rep.L2.BoxWithin++
			continue
		}
		rep.addDelta(d, LayerL2, "box", describe(p.React), "外框差 %.2fpx（上限 %.2f）：React %s，Viva %s", d, maxDelta, fmtBox(p.React.Box), fmtBox(p.Viva.Box))
	}
	if rep.L1.ReactNodes > 0 {
		rep.L2.AlignmentRate = float64(rep.L1.NodesMatched) / float64(rep.L1.ReactNodes)
	} else {
		rep.L2.AlignmentRate = 1
	}
	if rep.L2.AlignmentRate < 1 {
		rep.add(LayerL2, "alignment", rep.Scene, "对齐率 %.1f%%，要求 100%%", rep.L2.AlignmentRate*100)
	}

	// 文字：行数、绘制宽度、墨迹外接框。
	overlayInk, panels := overlayInkRects(in.Viva), overlayPanelRects(in.React)
	for _, p := range texts {
		subject := quote(NormText(p.React.Text))
		lines := 0
		for _, v := range p.Viva {
			lines += len(v.Rects)
		}
		reactRows := rowRects(p.React.Rects)
		if len(reactRows) != lines {
			rep.add(LayerL2, "text-lines", subject, "行数不一致：React %d，Viva %d", len(reactRows), lines)
			continue
		}
		checkWrappedPitch(rep, th, subject, reactRows, p)
		overlay := false
		for _, v := range p.Viva {
			overlay = overlay || v.Overlay
		}
		k := 0
		for _, v := range p.Viva {
			for li, vr := range v.Rects {
				rr := reactRows[k]
				k++
				rep.L2.TextLines++
				painted := vr.W()
				if li < len(v.Painted) {
					painted = v.Painted[li]
				}
				// 末行以外的折行行宽取不到「不含尾随空白」的口径，只比末行与单行文字的宽度。
				if len(reactRows) == 1 || k == len(reactRows) {
					dw := math.Abs(rr.W() - painted)
					rep.L2.MaxWidthDelta = math.Max(rep.L2.MaxWidthDelta, dw)
					if dw <= maxDelta+1e-9 {
						rep.L2.TextWidthOK++
					} else {
						rep.addDelta(dw, LayerL2, "text-width", subject, "绘制宽度差 %.2fpx（上限 %.2f）：React %.2f，Viva %.2f", dw, maxDelta, rr.W(), painted)
					}
				} else {
					rep.L2.TextWidthOK++
				}
				if !in.FactsOnly && in.ReactImg != nil && in.VivaImg != nil {
					if in.Backdrop != nil && !overlay {
						// 有遮罩的场景里，页面层文字都在遮罩背后：被模糊，或（窄窗口里）整个被面板盖住，墨迹处是面板自己的内容，
						// 都不比墨迹（台账 A-007）；面板自己的文字（overlay）照常比。
						rep.L2.InkBlurred++
					} else if !overlay && occludedByOverlayInk(rr, overlayInk, panels) {
						// 页面文字被浮层里的文字、图标或浮层面板盖住一部分：量墨迹的区域里是浮层自己的内容，不是这行文字的墨迹（见 compare_occlusion.go）。
						rep.L2.InkOccluded++
					} else if mostlyOutside(rr, in.ReactImg.Bounds()) {
						// 文字有大半在截图区域之外：区域里只剩字形的几行像素，墨迹总量比被边缘处半个像素的抗锯齿差异左右，
						// 量不出字重、颜色或字号是否对（位置与宽度照常比）。
						rep.L2.InkClipped++
					} else {
						checkInk(rep, in, th, subject, rr, true, overlay, p.React.Clip)
					}
				}
			}
		}
	}
}

// mostlyOutside 报告 rect 是否有一半以上落在截图区域 bounds 之外（被窗口边缘裁掉）。
func mostlyOutside(rect Box, bounds image.Rectangle) bool {
	if rect.W() <= 0 || rect.H() <= 0 {
		return false
	}
	w := math.Min(rect.Right(), float64(bounds.Max.X)) - math.Max(rect.X(), float64(bounds.Min.X))
	h := math.Min(rect.Bottom(), float64(bounds.Max.Y)) - math.Max(rect.Y(), float64(bounds.Min.Y))
	if w <= 0 || h <= 0 {
		return true
	}
	return w*h < 0.5*rect.W()*rect.H()
}

// checkInk 比较同一行文字在两张图里的墨迹：区域取 React 的文字行矩形外扩 1 像素。
//
// 比较「行、列墨迹强度剖面」而不是墨迹外接框：两个引擎的抗锯齿在字形最外一圈强弱不同，外接框会被最浅一圈左右，
// 而基线整体差一个像素时，剖面的最佳对齐位移一定是 ±1。所以取使剖面平方差最小的位移，上下位移必须不超过 InkMaxDeltaY，
// 左右不超过 InkMaxDeltaX；另外墨迹总量（强度之和）的比值要落在 [InkMassMin, InkMassMax]，用来发现字重或颜色不对。
// 返回量到的墨迹比与位移；两侧都没有墨迹或区域为空时返回 nil。
// overlay 为真表示这行文字画在浮层里：墨迹总量比的问题会带上 Overlay 标记，供已登记的浮层文字偏差（A-009）匹配。
// clip 是文字被祖先裁掉一部分时看得见的区域（Text.Clip）：区域只取这一块，且向内取整（压在裁剪线上的半个像素行不算），
// 免得裁剪线之外的页脚、边线进了墨迹剖面；没被裁的文字传 nil。
func checkInk(rep *Report, in Inputs, th Thresholds, subject string, rect Box, fractionalEdgeAllowed, overlay bool, clip []float64) *InkMeasure {
	// 遮罩背后被模糊的文字与图标没法比墨迹（React 是糊的、Viva 是清晰的）：改由 L3 在 Viva 图模糊后比较（backdrop.go，台账 A-007）。
	if !overlay && in.Backdrop.Blurs(rect) {
		rep.L2.InkBlurred++
		return nil
	}
	area := image.Rect(
		int(math.Floor(rect.X()))-1, int(math.Floor(rect.Y()))-1,
		int(math.Ceil(rect.Right()))+1, int(math.Ceil(rect.Bottom()))+1,
	).Intersect(in.ReactImg.Bounds())
	if len(clip) == 4 {
		area = area.Intersect(image.Rect(
			int(math.Ceil(clip[0])), int(math.Ceil(clip[1])),
			int(math.Floor(clip[0]+clip[2])), int(math.Floor(clip[1]+clip[3])),
		))
	}
	if area.Empty() {
		return nil
	}
	rp := inkProfile(in.ReactImg, area)
	vp := inkProfile(in.VivaImg, area)
	rep.L2.InkChecked++
	switch {
	case rp.mass == 0 && vp.mass == 0:
		rep.L2.InkOK++
		return nil
	case rp.mass == 0 || vp.mass == 0:
		rep.add(LayerL2, "ink", subject, "一侧没有墨迹：React 墨迹量 %.0f，Viva 墨迹量 %.0f", rp.mass, vp.mass)
		markInkContext(&rep.Issues[len(rep.Issues)-1], in.React, rect, area, clip)
		return nil
	}
	dy := bestShift(rp.rows, vp.rows)
	dx := bestShift(rp.cols, vp.cols)
	// 左边缘在小数像素上的文字：Viva 整像素落位、渲染层子像素定位，水平最多差半个像素，允许最佳位移到 1。
	allowX := th.Metrics.InkMaxDeltaX
	if fractionalEdgeAllowed && math.Abs(rect.X()-math.Round(rect.X())) > 1e-3 {
		allowX = th.Metrics.InkMaxDeltaXFractional
	}
	ratio := vp.mass / rp.mass
	if dx != 0 || dy != 0 {
		rep.L2.InkShifts = append(rep.L2.InkShifts, InkShift{Subject: subject, DX: dx, DY: dy})
	}
	rep.L2.MaxInkDX = max(rep.L2.MaxInkDX, abs(dx))
	rep.L2.MaxInkDY = max(rep.L2.MaxInkDY, abs(dy))
	rep.L2.MinInkMass = minPositive(rep.L2.MinInkMass, ratio)
	rep.L2.MaxInkMass = math.Max(rep.L2.MaxInkMass, ratio)
	switch {
	case abs(dx) > allowX || abs(dy) > th.Metrics.InkMaxDeltaY:
		rep.addDelta(float64(max(abs(dx), abs(dy))), LayerL2, "ink", subject, "墨迹错位：水平 %+d、垂直 %+d 像素（上限 %d、%d；正值表示 Viva 相对 React 偏右、偏下）",
			dx, dy, allowX, th.Metrics.InkMaxDeltaY)
		rep.Issues[len(rep.Issues)-1].DY = float64(abs(dy))
		markInkContext(&rep.Issues[len(rep.Issues)-1], in.React, rect, area, clip)
	case ratio < th.Metrics.InkMassMin || ratio > th.Metrics.InkMassMax:
		rep.addDelta(math.Abs(ratio-1), LayerL2, "ink-mass", subject, "墨迹总量比 %.3f 超出 [%.2f, %.2f]（Viva / React；字重、颜色或字号可能不对）",
			ratio, th.Metrics.InkMassMin, th.Metrics.InkMassMax)
		rep.Issues[len(rep.Issues)-1].Overlay = overlay
		rep.Issues[len(rep.Issues)-1].Dir = 1
		if ratio < 1 {
			rep.Issues[len(rep.Issues)-1].Dir = -1
		}
		markInkContext(&rep.Issues[len(rep.Issues)-1], in.React, rect, area, clip)
	default:
		rep.L2.InkOK++
	}
	return &InkMeasure{Subject: subject, Ratio: ratio, DX: dx, DY: dy}
}

// InkMeasure 是一处墨迹比较量到的数：Viva 与 React 的墨迹总量比，以及最佳对齐位移（正值表示 Viva 偏右、偏下）。
type InkMeasure struct {
	Subject string  `json:"subject"`
	Ratio   float64 `json:"ratio"`
	DX      int     `json:"dx"`
	DY      int     `json:"dy"`
}

func minPositive(cur, v float64) float64 {
	if cur == 0 || v < cur {
		return v
	}
	return cur
}

// profile 是一块区域里墨迹强度的行剖面、列剖面与总量。
type profile struct {
	rows, cols []float64
	mass       float64
}

// inkProfile 计算区域里每个像素与背景色（区域外圈众数）的最大通道差，按行、按列累加。
func inkProfile(img image.Image, area image.Rectangle) profile {
	bg := ringMode(img, area)
	p := profile{rows: make([]float64, area.Dy()), cols: make([]float64, area.Dx())}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			d := float64(rgbDistance(rgba8(img, x, y), bg))
			p.rows[y-area.Min.Y] += d
			p.cols[x-area.Min.X] += d
			p.mass += d
		}
	}
	return p
}

// bestShift 返回使 b 平移后与 a 的平方差最小的整数位移（b 的第 i 项对 a 的第 i-shift 项）：正值表示 b 相对 a 偏后。
// 搜索范围 ±4；并列时取绝对值小的，再取负的。
func bestShift(a, b []float64) int {
	const span = 4
	best, bestCost := 0, math.Inf(1)
	for s := -span; s <= span; s++ {
		cost := 0.0
		overlap := 0
		for i := range a {
			j := i + s
			var bv float64
			if j >= 0 && j < len(b) {
				bv = b[j]
				overlap++
			}
			d := a[i] - bv
			cost += d * d
		}
		// 位移后两条剖面重叠不足一半：代价量的是「被挤出去多少」而不是对齐程度，这样的位移不参选
		// （被祖先裁成几行薄片的图标，±4 的窗口会把整条剖面推出范围，误报成 4 像素的错位）。
		// 文字行与整枚图标的剖面有十几行，±4 时重叠都在一半以上，不受影响。
		if 2*overlap < len(a) {
			continue
		}
		if cost < bestCost-1e-6 || (math.Abs(cost-bestCost) <= 1e-6 && abs(s) < abs(best)) {
			best, bestCost = s, cost
		}
	}
	return best
}

func ringMode(img image.Image, area image.Rectangle) [3]uint8 {
	counts := map[[3]uint8]int{}
	add := func(x, y int) { counts[rgba8(img, x, y)]++ }
	for x := area.Min.X; x < area.Max.X; x++ {
		add(x, area.Min.Y)
		add(x, area.Max.Y-1)
	}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		add(area.Min.X, y)
		add(area.Max.X-1, y)
	}
	var best [3]uint8
	bestN := -1
	for c, n := range counts {
		if n > bestN || (n == bestN && lessRGB(c, best)) {
			best, bestN = c, n
		}
	}
	return best
}

func lessRGB(a, b [3]uint8) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	if a[1] != b[1] {
		return a[1] < b[1]
	}
	return a[2] < b[2]
}

func rgba8(img image.Image, x, y int) [3]uint8 {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func rgbDistance(a, b [3]uint8) int {
	return max(abs(int(a[0])-int(b[0])), abs(int(a[1])-int(b[1])), abs(int(a[2])-int(b[2])))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// boxDelta 是两个外框的位置与尺寸四个分量里最大的绝对差。
func boxDelta(a, b Box) float64 {
	d := 0.0
	for i := range a {
		d = math.Max(d, math.Abs(a[i]-b[i]))
	}
	return d
}

func fmtBox(b Box) string {
	return fmt.Sprintf("[%.2f %.2f %.2f %.2f]", b[0], b[1], b[2], b[3])
}

// rowRects 把 React 一段文字的片段矩形并成「行」。Range 的矩形是行内盒的片段：预排版文字（white-space: pre-wrap）
// 的行首缩进空白、折行处挂在行尾的空格，都会单独成一块，同一行于是有两三块。行数与行宽要按整行比较：
// y 相差不到 0.5px 的相邻片段属于同一行，并成它们的并集；不同行的片段、没有片段的文字不受影响。
func rowRects(rects []Box) []Box {
	rows := make([]Box, 0, len(rects))
	for _, r := range rects {
		if n := len(rows); n > 0 && math.Abs(rows[n-1].Y()-r.Y()) < 0.5 {
			last := rows[n-1]
			left, top := math.Min(last.X(), r.X()), math.Min(last.Y(), r.Y())
			right, bottom := math.Max(last.Right(), r.Right()), math.Max(last.Bottom(), r.Bottom())
			rows[n-1] = Box{left, top, right - left, bottom - top}
			continue
		}
		rows = append(rows, r)
	}
	return rows
}

// checkWrappedPitch 比较折行文字相邻两行的行距：React 取各行 Range 矩形的 y 之差，Viva 取各行文字盒的 y 之差，
// 两者都等于行高。行距不对说明行高或折行方式不对，单看行数和宽度发现不了。
func checkWrappedPitch(rep *Report, th Thresholds, subject string, reactRows []Box, p textPair) {
	var vivaRects []Box
	for _, v := range p.Viva {
		vivaRects = append(vivaRects, v.Rects...)
	}
	if len(reactRows) < 2 || len(reactRows) != len(vivaRects) {
		return
	}
	for i := 1; i < len(vivaRects); i++ {
		pr := reactRows[i].Y() - reactRows[i-1].Y()
		pv := vivaRects[i].Y() - vivaRects[i-1].Y()
		if d := math.Abs(pr - pv); d > th.Metrics.WrappedTextPitchPx+1e-9 {
			rep.add(LayerL2, "wrapped-pitch", subject, "第 %d 行与上一行的行距差 %.2fpx（上限 %.2f）：React %.2f，Viva %.2f", i+1, d, th.Metrics.WrappedTextPitchPx, pr, pv)
		}
	}
}
