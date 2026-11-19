package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
)

// tableReveal 复刻渲染层 ItemTableView 的一个副作用：选中行变了（包括表格刚挂载时就已有选中行）就
// `scrollIntoView({ block: "nearest" })`，把它滚进视口。两个方向都按「最近」规则（kit.ScrollNearest）：
// 纵向出去多少补多少；行比视口宽（表格横向溢出）时，横向把行首对齐到视口左缘——所以带着选中行切到表格视图时，
// 表格会先横向滚过内边距那一段（默认列下 17px），这是渲染层真实的呈现。
type tableReveal struct {
	// done 是已经滚到过的选中行 ID；与当前选中行不同就要滚。没有选中行时复位，下次选中同一行仍会滚（effect 依赖 selectedId）。
	done string
	// rowID 与 rect 是选中行上一帧画出来的外框（窗口坐标）；originX 与 originY 是滚动内容左上角的窗口坐标。
	// 两者同一帧记下，相减就是行在滚动内容里的位置，与当前偏移无关。
	rowID            string
	rect             gfx.Rect
	originX, originY float32
	// rowTops 是上一帧各行上缘在滚动内容里的位置（按行序），对照场景用它换算要滚多远（RowContentTop）。
	rowTops []float32
}

// noteRowTop 在一行画出来时记下它的上缘；选中行还要记下整个外框。每帧第一行（index 0）开始新的一轮。
func (s *Screen) noteRowTop(d *ui.DrawContext, index int, id string, active bool) {
	r := &s.table.reveal
	if index == 0 {
		r.rowTops = r.rowTops[:0]
	}
	r.rowTops = append(r.rowTops, d.Rect.Y-r.originY)
	if !active || (r.rowID == id && r.rect == d.Rect) {
		return
	}
	r.rowID, r.rect = id, d.Rect
	if r.done != id {
		d.Canvas.RequestFrame() // 外框到手了：下一帧声明时就能算出要滚多远
	}
}

// noteOrigin 记下滚动内容左上角的窗口坐标（内容列自己的外框）。
func (s *Screen) noteOrigin(d *ui.DrawContext) {
	s.table.reveal.originX, s.table.reveal.originY = d.Rect.X, d.Rect.Y
}

// revealSelected 在声明二维滚动区的内容之前调用：选中行还没滚过就按「最近」规则把它滚进视口。
func (s *Screen) revealSelected(view kit.Scroll2D, selected string) {
	r := &s.table.reveal
	if selected == "" {
		r.done = ""
		return
	}
	if r.done == selected || r.rowID != selected || view.State == nil {
		return
	}
	top, left := r.rect.Y-r.originY, r.rect.X-r.originX
	dy := kit.ScrollNearest(top-view.Offset, top+r.rect.Height-view.Offset, 0, view.Viewport)
	dx := kit.ScrollNearest(left-view.OffsetX, left+r.rect.Width-view.OffsetX, 0, view.ViewportW)
	if dy != 0 {
		view.State.ScrollTo(view.Offset + dy)
	}
	if dx != 0 {
		view.State.ScrollToX(view.OffsetX + dx)
	}
	r.done = selected
}

// RowContentTop 是上一帧第 i 行（从 0 起）的上缘在滚动内容里的位置；对照场景用它算要滚到哪里（与渲染层的
// `row.getBoundingClientRect().top - scroller.top + scroller.scrollTop` 同一口径）。表格没有画出这一行时第二个返回值为假。
func (s *Screen) RowContentTop(i int) (float32, bool) {
	tops := s.table.reveal.rowTops
	if i < 0 || i >= len(tops) {
		return 0, false
	}
	return tops[i], true
}
