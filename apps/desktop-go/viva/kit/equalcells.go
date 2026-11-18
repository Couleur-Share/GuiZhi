package kit

import "github.com/vivaui/viva/ui"

// EqualCells 把孩子摆成渲染层 flex 容器里「每格 flex: 1 1 0%」的一排，位置照 Chromium 的 LayoutUnit 规则取整（layoutunit.go，台账 B-025）。
//
// 渲染层的写法有两种，用 Center 区分：
//
//   - Center 为 true：每格是 `flex-1 flex justify-center min-w-0`，格子里放一个 `flex-shrink-0` 的定宽孩子（显示设置页的主题色带）；
//     孩子保持自己的宽度，在格子里居中，格子比孩子窄时向左右溢出同样多（向零取整）。
//   - Center 为 false：孩子本身就是 `flex-1` 的弹性项，撑满格子。
//
// 交叉轴固定为 `items-center`：孩子在容器内容区里竖向居中（同样向零取整到 1/64）。
//
// 孩子的 Width / Grow / Margin 由本版面接管，调用方不要设（Viva 的 CustomLayout 约定）。版面要放在模型里跨帧保留，不要每帧新建。
// 用法：
//
//	cells := &kit.EqualCells{Center: true} // 放在界面状态里
//	kit.EqualCellsBox(c, cells).Pad(l.Space(2)).Children(func() { ... })
type EqualCells struct {
	// Gap 是格子间距（渲染层的 gap-*），逻辑像素。
	Gap float32
	// Center 见类型说明。
	Center bool

	widths []int32 // 草稿：每格宽（1/64 px）
}

// EqualCellsBox 声明一个占满可用宽度的 EqualCells 容器，返回 Box 以便继续链式设置（Pad 等），最后自己调用 Children。
func EqualCellsBox(c ui.C, cells *EqualCells) ui.Box {
	return ui.CustomLayout(c, cells).FillWidth()
}

// MeasureWidth 是内容想要的宽度：各孩子首选宽度加间距（占满宽度的容器用不到）。
func (e *EqualCells) MeasureWidth(ctx *ui.LayoutContext) float32 {
	n := ctx.Count()
	var sum float32
	for i := 0; i < n; i++ {
		sum += ctx.Width(i)
	}
	if n > 1 {
		sum += e.Gap * float32(n-1)
	}
	return sum
}

// PlaceWidth 按 Chromium 的规则分格、居中。
func (e *EqualCells) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	n := ctx.Count()
	if n == 0 {
		return
	}
	gap := pxToLU(e.Gap)
	e.widths = distributeLU(e.widths, pxToLU(bounds.Width)-gap*int32(n-1), n)
	var x int32
	for i := 0; i < n; i++ {
		cell := e.widths[i]
		if e.Center {
			w := pxToLU(ctx.Width(i))
			ctx.SetWidth(i, luToPx(w))
			ctx.SetX(i, bounds.X+luToPx(x+centerOffsetLU(cell-w)))
		} else {
			ctx.SetWidth(i, luToPx(cell))
			ctx.SetX(i, bounds.X+luToPx(x))
		}
		x += cell + gap
	}
}

// MeasureHeight 是最高的孩子的高度（items-center 的容器高度由最高的项撑起）。
func (e *EqualCells) MeasureHeight(ctx *ui.LayoutContext) float32 {
	var h float32
	for i := 0; i < ctx.Count(); i++ {
		h = max(h, ctx.Height(i))
	}
	return h
}

// PlaceHeight 让每个孩子在内容区里竖向居中。
func (e *EqualCells) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	box := pxToLU(bounds.Height)
	for i := 0; i < ctx.Count(); i++ {
		h := ctx.Height(i)
		ctx.SetHeight(i, h)
		ctx.SetY(i, bounds.Y+luToPx(centerOffsetLU(box-pxToLU(h))))
	}
}
