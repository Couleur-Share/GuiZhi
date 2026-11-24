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
//   - 两行的基础高度是子项在 min-h-0 下能缩到的最小外框：面板是滚动区，上下 py-5 的 40px 缩不掉；侧栏只剩下边框（有则 1px）；增长上限是各自的 max-content（子项自然高度）；
//   - 可用高度 H 减去两个基础高度后的自由空间平均分给两行，谁先到上限就停在上限，剩下的给另一行；
//   - 两行的上限加起来还不到 H 时，多出来的高度再平均分给两行（align-content: normal 对 auto 行是拉伸）。
//
// 所以侧栏既不是固定高度，也不是自然高度，而是随窗口高度「平分」：800×600 的空配置页里，侧栏与面板的自然高度都大于一半，
// 都按基础高度加自由空间的一半：空配置（侧栏只有右边框，基础 0）(552−0−40)/2 = 256，侧栏 256、面板 296；有供应商（侧栏有 border-b 的 1px，
// 计入基础高度）侧栏 256.5、面板 295.5；两者都与渲染层 800×600 的 facts 一致（基础高度都取 0 时是 276 / 276）；
// 供应商列表较矮的配置里，面板自然高度小于一半，侧栏拿走 H 减面板自然高度。
//
// 用法：容器必须由外层给出高度（Grow 或 FillHeight），子项恰好两个（侧栏、面板），它们自己的 Width / Height / Grow 在这里不生效。
// 面板是滚动区，它自己报的预测高度不含内容：自然高度由调用方每帧经 setDetail 报进来（取上一帧布局量到的内容高度）。
type aiRowsLayout struct {
	detail    float32 // 面板的自然高度（含滚动区内边距）
	asideList float32 // 侧栏里供应商列表的自然高度（含内边距）；侧栏自然高度 = 它加上方的搜索区与下方的添加按钮

	// 两行的基础高度，由调用方每帧报进来：面板行是滚动区的上下内边距；侧栏行是下边框宽度（空配置没有下边框）。
	baseDetail, baseAside float32
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
	top, bottom := gridAutoRows(bounds.Height, ctx.Height(0), r.detail, r.baseAside, r.baseDetail)
	ctx.SetY(0, bounds.Y)
	ctx.SetHeight(0, top)
	ctx.SetY(1, bounds.Y+top)
	ctx.SetHeight(1, bottom)
}

// gridAutoRows 按上面的规则把高度 total 分给两行；a、b 是两行的自然高度（增长上限），baseA、baseB 是两行的基础高度。
func gridAutoRows(total, a, b, baseA, baseB float32) (float32, float32) {
	if total <= 0 {
		return 0, 0
	}
	a, b = max(a, 0), max(b, 0)
	baseA, baseB = min(max(baseA, 0), a), min(max(baseB, 0), b)
	free := total - baseA - baseB
	if free <= 0 { // 连基础高度都放不下：按基础高度比例分
		return total * baseA / max(baseA+baseB, 1), total * baseB / max(baseA+baseB, 1)
	}
	growA, growB := a-baseA, b-baseB // 各自还能长多少
	half := free / 2
	switch {
	case growA+growB <= free:
		extra := (free - growA - growB) / 2
		return baseA + growA + extra, baseB + growB + extra
	case growA <= half:
		return a, total - a
	case growB <= half:
		return total - b, b
	default:
		return baseA + half, baseB + half
	}
}
