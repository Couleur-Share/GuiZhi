package kit

import (
	"math"
	"sort"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 高斯阴影
//
// CSS box-shadow 的模糊是高斯模糊：标准差 σ = blur/2，结果是「阴影矩形」与高斯核的卷积。Viva 的 Canvas.Shadow 换了个办法：
// 取到阴影圆角矩形的有符号距离，再过 smoothstep（gfx.ShadowStyle 的注释：模糊带宽 Blur，两侧各一份）。阴影矩形比 σ 大得多时
// 两者几乎重合（确认对话框、下拉面板都按它通过了对照），但阴影矩形的短边小于 6σ（= 3×Blur）时距离场会偏深：
// 卷积会把窄条的峰值压下去，而距离场只看最近的一条边。提示卡片（shadow-2xl，σ = 25；卡片 50 高，收缩 12 后只剩 26）
// 正好落在这个区间——浅色下卡片下缘的阴影，Viva 比 Chromium 深 17 级（2026-11-16 在 ui.toast.warning 上读得；开发文档「Viva 阴影模型」一节有数据）。
//
// 这里用网格补上：矩形的卷积是可分离的，透明度 = 高斯横向 × 纵向两个误差函数之差。
// 在网格顶点上按公式取值，每个格子交给 FillQuad 的逐角着色去线性插值。格距 gaussMeshStep 逻辑像素，
// 插值误差上限 h²/8 × max|α″| ≈ 64/8 × 1e-4 = 8e-4（约 0.2 个灰阶）。

// gaussMeshStep 是网格线的间距（逻辑像素）。
const gaussMeshStep = 8

// gaussMinAlpha 低于它的格子不画（约 0.5/255）：整格四个角都这么淡，肉眼与逐像素比较都看不出。
const gaussMinAlpha = 1.0 / 512

// gaussianShadowNeeded 报告一层阴影要不要走高斯网格：阴影矩形的短边不足 6σ（= 3×Blur），距离场近似就会偏深。
func gaussianShadowNeeded(caster gfx.Rect, sh tokens.Shadow) bool {
	if sh.Blur <= 0 {
		return false
	}
	return min(caster.Width, caster.Height)+2*sh.Spread < 3*sh.Blur
}

// paintShadow 画一层阴影：大多数走 Viva 的 Canvas.Shadow，窄小的盒子走高斯网格。
func (l *Look) paintShadow(d *ui.DrawContext, caster gfx.Rect, radius float32, sh tokens.Shadow) {
	if gaussianShadowNeeded(caster, sh) {
		paintGaussianShadow(d, caster, radius, sh)
		return
	}
	style, color := ShadowStyle(sh)
	d.Canvas.Shadow(caster, radius, style, d.Fade(color))
}

// paintGaussianShadow 画一层与 CSS 一致的高斯阴影。caster 是投影的盒子（已吸附到设备像素），radius 是它的圆角。
//
// 与 Canvas.Shadow 一样，投影盒子本身要「挖掉」（半透明的表面不该被自己的阴影压暗）：
// 网格里落在盒子「十字」（去掉四个圆角方块后剩下的部分）里的格子整个不画，网格线对齐十字的边界所以是精确的；
// 四个圆角方块仍然会画阴影，它们在圆弧里面的那部分最多把半透明的底色压暗一两个灰阶，不透明的底色则完全盖住。
// 阴影矩形的圆角（radius + spread）在这里忽略：σ 比它大一个量级，卷积结果与直角矩形的差别不到半个灰阶。
func paintGaussianShadow(d *ui.DrawContext, caster gfx.Rect, radius float32, sh tokens.Shadow) {
	paintGaussianShadowStep(d, caster, radius, sh, gaussMeshStep)
}

// paintGaussianShadowStep 同 paintGaussianShadow，网格线的间距由调用方给（逻辑像素）。
//
// step 不大于 0 表示「逐设备像素」：每个设备像素一格，整格取一个值（格中心处高斯的值），不在格内插值。
// 一格里值变化很快时（模糊半径只有几像素），格边上取值再线性插到像素中心，尾部会偏大一到三成（σ=2 时第四列 0.045 对 0.040），
// 取整后多出 1 个灰阶；整格取中心值，像素中心上的值就是定义给的值。
func paintGaussianShadowStep(d *ui.DrawContext, caster gfx.Rect, radius float32, sh tokens.Shadow, step float32) {
	canvas := d.Canvas
	scale := canvas.PixelScale()
	if scale <= 0 {
		scale = 1
	}
	sigma := sh.Blur / 2
	// 阴影矩形：投影盒子按 spread 向四周扩（负值收缩），再按偏移移动
	ax, bx := caster.X-sh.Spread+sh.X, caster.X+caster.Width+sh.Spread+sh.X
	ay, by := caster.Y-sh.Spread+sh.Y, caster.Y+caster.Height+sh.Spread+sh.Y
	if sigma <= 0 || bx <= ax || by <= ay {
		return
	}
	reach := 3 * sigma // 三倍标准差之外的尾巴不到 0.14%，乘上 0.25 的不透明度远小于一个灰阶
	corner := min(max(radius, 0), min(caster.Width, caster.Height)/2)
	right, bottom := caster.X+caster.Width, caster.Y+caster.Height
	flat := step <= 0
	if flat {
		step = 1 / scale
	}
	xs := gaussLinesStep(ax, bx, reach, scale, step, caster.X, caster.X+corner, right-corner, right)
	ys := gaussLinesStep(ay, by, reach, scale, step, caster.Y, caster.Y+corner, bottom-corner, bottom)
	if len(xs) < 2 || len(ys) < 2 {
		return
	}
	base := d.Fade(gfxColor(sh.Color))
	var tints func(i, j int) [4]gfx.Color
	if flat {
		cx, cy := gaussCells(xs, ax, bx, sigma), gaussCells(ys, ay, by, sigma)
		tints = func(i, j int) [4]gfx.Color {
			c := base
			c.Alpha = base.Alpha * cx[i] * cy[j]
			return [4]gfx.Color{c, c, c, c}
		}
	} else {
		gx, gy := gaussProfile(xs, ax, bx, sigma), gaussProfile(ys, ay, by, sigma)
		tint := func(i, j int) gfx.Color {
			c := base
			c.Alpha = base.Alpha * gx[i] * gy[j]
			return c
		}
		tints = func(i, j int) [4]gfx.Color {
			return [4]gfx.Color{tint(i, j), tint(i+1, j), tint(i, j+1), tint(i+1, j+1)}
		}
	}
	const eps = 0.001
	for j := 0; j+1 < len(ys); j++ {
		y0, y1 := ys[j], ys[j+1]
		for i := 0; i+1 < len(xs); i++ {
			x0, x1 := xs[i], xs[i+1]
			// 十字之内：竖条（左右各收 corner）与横条（上下各收 corner）
			inColumn := x0 >= caster.X+corner-eps && x1 <= right-corner+eps && y0 >= caster.Y-eps && y1 <= bottom+eps
			inRow := x0 >= caster.X-eps && x1 <= right+eps && y0 >= caster.Y+corner-eps && y1 <= bottom-corner+eps
			if inColumn || inRow {
				continue
			}
			t := tints(i, j)
			if max(max(t[0].Alpha, t[1].Alpha), max(t[2].Alpha, t[3].Alpha)) < gaussMinAlpha {
				continue
			}
			canvas.FillQuad(gfx.Quad{
				Points: [4]gfx.Point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x0, Y: y1}, {X: x1, Y: y1}},
				Tint:   t,
			})
		}
	}
}

// gaussLines 是一条轴上的网格线（逻辑像素，吸附到设备像素，升序、不重复），覆盖阴影矩形两边 a、b 各 ±reach 的范围：
// 两条边附近每 gaussMeshStep 一条（透明度在这里变化），中间平坦处不加；extra 是必须对齐的位置（投影盒子「十字」的边界）。
// 网格线落在整设备像素上：相邻格子的外沿羽化正好是 0 或 1，拼缝处不会出现一条淡线。
func gaussLines(a, b, reach, scale float32, extra ...float32) []float32 {
	return gaussLinesStep(a, b, reach, scale, gaussMeshStep, extra...)
}

// gaussLinesStep 同 gaussLines，网格线的间距由调用方给：模糊半径很小时（侧栏的 0 1px 2px，过渡带只有几像素），
// 8 px 一条的网格会把带内的值线性插成一条斜线（右缘外第二列的 1 个灰阶就这样丢了），要逐设备像素取样。
func gaussLinesStep(a, b, reach, scale, step float32, extra ...float32) []float32 {
	lo, hi := snapEdge(a-reach, scale), snapEdge(b+reach, scale)
	lines := make([]float32, 0, 56)
	lines = append(lines, lo, hi)
	for _, edge := range [2]float32{a, b} {
		for p := edge - reach; p < edge+reach; p += step {
			lines = append(lines, snapEdge(p, scale))
		}
		lines = append(lines, snapEdge(edge+reach, scale))
	}
	for _, p := range extra {
		lines = append(lines, snapEdge(p, scale))
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })
	out := lines[:0]
	for _, p := range lines {
		if p < lo || p > hi {
			continue
		}
		if n := len(out); n > 0 && p-out[n-1] < 0.5/scale {
			continue
		}
		out = append(out, p)
	}
	return out
}

// gaussProfile 是沿一条轴的透明度因子：区间 [a, b] 与标准差 σ 的高斯核卷积，在每条网格线上取值。
func gaussProfile(lines []float32, a, b, sigma float32) []float32 {
	out := make([]float32, len(lines))
	for i, p := range lines {
		out[i] = float32(normalCDF(float64((p-a)/sigma)) - normalCDF(float64((p-b)/sigma)))
	}
	return out
}

// gaussCells 是沿一条轴的透明度因子，每格（相邻两条网格线之间）一个值，取格中心处的值。
func gaussCells(lines []float32, a, b, sigma float32) []float32 {
	out := make([]float32, max(len(lines)-1, 0))
	for i := range out {
		mid := (lines[i] + lines[i+1]) / 2
		out[i] = float32(normalCDF(float64((mid-a)/sigma)) - normalCDF(float64((mid-b)/sigma)))
	}
	return out
}

// normalCDF 是标准正态分布的累积分布函数 Φ(z)。
func normalCDF(z float64) float64 { return 0.5 * (1 + math.Erf(z/math.Sqrt2)) }
