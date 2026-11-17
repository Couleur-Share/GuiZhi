package kit

import "github.com/vivaui/viva/ui"

// aspectLayout 是定宽高比的版面（Tailwind 的 aspect-[16/9]）：宽度由外层给（占满可用宽度），高度 = 宽度 ÷ 比例；
// 子元素都铺满这个盒子（相当于 relative 容器里的 h-full w-full）。
type aspectLayout struct{ ratio float32 }

func (a aspectLayout) MeasureWidth(*ui.LayoutContext) float32 { return 0 } // 宽度由外层给（FillWidth）

func (a aspectLayout) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	for i := 0; i < ctx.Count(); i++ {
		ctx.SetX(i, bounds.X)
		ctx.SetWidth(i, bounds.Width)
	}
}

func (a aspectLayout) MeasureHeight(ctx *ui.LayoutContext) float32 {
	if a.ratio <= 0 {
		return 0
	}
	return ctx.ContentWidth() / a.ratio
}

func (a aspectLayout) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	for i := 0; i < ctx.Count(); i++ {
		ctx.SetY(i, bounds.Y)
		ctx.SetHeight(i, bounds.Height)
	}
}

// AspectBox 是宽高比固定的盒子：撑满可用宽度，高度随宽度走，子元素铺满它。ratio 是宽÷高（16/9 取 16.0/9）。
func (l *Look) AspectBox(c ui.C, ratio float32, body func()) ui.Box {
	return ui.CustomLayout(c, aspectLayout{ratio: ratio}).FillWidth().Children(body)
}
