package kit

import "math"

// Chromium 的布局坐标是 1/64 像素的定点数（LayoutUnit）；Viva 的版面是 float32 的精确运算。
// 平分与居中会产生分数位置：精确值恰好是 k + 0.5 的元素，在 Chromium 里因逐步取整略小于它（四舍五入落到 k），
// 在 Viva 里正好是 k + 0.5（落到 k + 1），整个元素差 1 个设备像素（台账 B-025；实例：显示设置页大字号档的七枚色板，
// 第四枚左缘 Chromium 442.484375、精确值 442.5，选中环与圆整体右移 1 像素）。
//
// 本文件是 Chromium flex 容器分配规则的整数版。规则来自 2026-11-18 在渲染层实测（scripts/parity/tools/probe-flex-units.mjs），
// 入库的 1464 例与一次性拟合的 3531 例（容器宽取整数与 1/64 的小数，格数 2–9，间距 0 / 8 / 9 / 13.5，孩子宽 0–101.25）全部一致：
//
//   - 弹性项（flex: 1 1 0%）分剩余宽度：从最后一项往前，逐项取「剩余 ÷ 剩余项数」四舍五入（半数向上）。
//     不是按累计向下取整，也不是从第一项往后分：802 ÷ 7（51328 个单位）得到 [7332, 7333, 7332, 7333, 7332, 7333, 7333]。
//   - 居中（justify-content: center，含每格里居中的孩子）：向零取整的 (剩余 − 孩子宽) ÷ 2。剩余为负时是 −171 而不是 −172。
//   - 两端对齐（space-between）：第 k 个孩子的偏移 = 四舍五入(k × 剩余 ÷ (n − 1))；剩余不为正或只有一个孩子时按起点对齐。
//
// 这几条只是函数，不是布局引擎：哪个容器的 T2 暴露出 1 像素偏差，就在那个容器里用它们摆放（EqualCells 是第一个）。
// 没有量过的形状（弹性项权重不等、min / max 钳制、收缩、多行、竖向）不要套用。

// luPerPx 是每个像素的 LayoutUnit 个数。
const luPerPx = 64

// pxToLU 把逻辑像素换成 1/64 单位。先四舍五入到最近的单位：Viva 的版面是 float32，802 可能是 801.99994，
// 直接向下取整会差一个单位。
func pxToLU(px float32) int32 { return int32(math.Round(float64(px) * luPerPx)) }

// luToPx 把 1/64 单位换回逻辑像素（1/64 的整数倍在 float32 里是精确的，直到 2^18 像素）。
func luToPx(v int32) float32 { return float32(v) / luPerPx }

// QuantizeLU 把逻辑像素量化到 1/64 像素。渲染层经 ResizeObserver 等读到的宽度本来就是 LayoutUnit 的精度，
// Viva 量出来的 float32 宽度可能多一个 ulp（912 量成 912.00006）：拿量到的宽度去算别的上下限之前先量化，
// 噪声才不会漏进可访问树的 valuemax 之类的状态里（M2-4 英文变体 detail-review 实测：列表栏分隔条的上限 512 读成 512.0000610351562，见台账 G-017 的 M2-4 更新）。
func QuantizeLU(px float32) float32 { return luToPx(pxToLU(px)) }

// roundDivLU 是 num ÷ den 四舍五入（半数向上），num ≥ 0、den > 0。
func roundDivLU(num, den int64) int64 { return (2*num + den) / (2 * den) }

// distributeLU 把 free 个单位分给 n 个同权重的弹性项，结果写进 dst（容量够就复用，绘制路径上不分配）并返回。
// free 为负按 0 算：基准宽度为 0 的弹性项不会变成负宽度。
func distributeLU(dst []int32, free int32, n int) []int32 {
	if n <= 0 {
		return dst[:0]
	}
	if cap(dst) < n {
		dst = make([]int32, n)
	} else {
		dst = dst[:n]
	}
	rest := int64(max(free, 0))
	for i := n - 1; i >= 0; i-- {
		w := roundDivLU(rest, int64(i+1))
		dst[i] = int32(w)
		rest -= w
	}
	return dst
}

// centerOffsetLU 是居中的偏移：向零取整的 rest ÷ 2（Go 的整数除法与 C++ 一样向零取整）。
func centerOffsetLU(rest int32) int32 { return rest / 2 }

// betweenOffsetLU 是两端对齐时第 k 个（从 0 数）孩子相对「前 k 个孩子宽度之和」的额外偏移，free 是容器宽减去全部孩子宽。
func betweenOffsetLU(free int32, k, n int) int32 {
	if n < 2 || free <= 0 {
		return 0
	}
	return int32(roundDivLU(int64(free)*int64(k), int64(n-1)))
}
