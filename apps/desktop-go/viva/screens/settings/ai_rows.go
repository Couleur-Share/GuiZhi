package settings

import (
	"math"

	"github.com/vivaui/viva/ui"
)

// aiRowsLayout 是窄窗口（不足 lg 断点）里「侧栏在上、面板在下」的两行：渲染层是
//
//	<div class="grid h-full min-h-0 lg:grid-cols-[14rem_minmax(0,1fr)]">
//
// 窄于 lg 时它只有一列、两个隐式的 auto 行，两个子项都带 min-h-0（自动最小高度为 0）。CSS grid 给这两行定高的规则是：
//
//   - 两行的基础高度都是 0，增长上限是各自的 max-content（子项自然高度）；
//   - 可用高度 H 平均分给两行，谁先到上限就停在上限，剩下的给另一行；
//   - 两行的上限加起来还不到 H 时，多出来的高度再平均分给两行（align-content: normal 对 auto 行是拉伸）。
//
// 所以侧栏既不是固定高度，也不是自然高度，而是随窗口高度「平分」：800×600 的空配置页里，侧栏与面板的自然高度都大于一半，
// 各拿 H/2；供应商列表较矮的配置里，面板自然高度小于一半，侧栏拿走 H 减面板自然高度。
//
// 用法：容器必须由外层给出高度（Grow 或 FillHeight），子项恰好两个（侧栏、面板），它们自己的 Width / Height / Grow 在这里不生效。
// 面板是滚动区，它自己报的预测高度不含内容：自然高度由调用方每帧经 setDetail 报进来（取上一帧布局量到的内容高度）。
type aiRowsLayout struct {
	detail    float32 // 面板的自然高度（含滚动区内边距）
	asideList float32 // 侧栏里供应商列表的自然高度（含内边距）；侧栏自然高度 = 它加上方的搜索区与下方的添加按钮
}

// setAsideList 记下侧栏列表的自然高度；变化超过半个像素返回 true。
func (r *aiRowsLayout) setAsideList(h float32) bool {
	if math.Abs(float64(h-r.asideList)) < 0.5 {
		return false
	}
	r.asideList = h
	return true
}

// setDetail 记下面板的自然高度；变化超过半个像素返回 true（调用方据此补一帧）。
func (r *aiRowsLayout) setDetail(h float32) bool {
	if math.Abs(float64(h-r.detail)) < 0.5 {
		return false
	}
	r.detail = h
	return true
}

func (*aiRowsLayout) MeasureWidth(*ui.LayoutContext) float32 { return 0 } // 宽度由外层给（FillWidth）

func (*aiRowsLayout) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	for i := 0; i < ctx.Count(); i++ {
		ctx.SetX(i, bounds.X)
		ctx.SetWidth(i, bounds.Width)
	}
}

func (*aiRowsLayout) MeasureHeight(*ui.LayoutContext) float32 { return 0 } // 高度由外层给

func (r *aiRowsLayout) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	if ctx.Count() != 2 {
		return
	}
	top, bottom := gridAutoRows(bounds.Height, ctx.Height(0), r.detail)
	ctx.SetY(0, bounds.Y)
	ctx.SetHeight(0, top)
	ctx.SetY(1, bounds.Y+top)
	ctx.SetHeight(1, bottom)
}

// gridAutoRows 按上面的规则把高度 total 分给两行；a、b 是两行的自然高度（增长上限）。
func gridAutoRows(total, a, b float32) (float32, float32) {
	if total <= 0 {
		return 0, 0
	}
	a, b = max(a, 0), max(b, 0)
	half := total / 2
	switch {
	case a+b <= total:
		extra := (total - a - b) / 2
		return a + extra, b + extra
	case a <= half:
		return a, total - a
	case b <= half:
		return total - b, b
	default:
		return half, total - half
	}
}
