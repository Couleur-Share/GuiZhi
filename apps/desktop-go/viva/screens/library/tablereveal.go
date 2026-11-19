package library

import (
	"slices"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// tableReveal 复刻渲染层 ItemTableView 的一个副作用：选中行变了（包括表格刚挂载时就已有选中行）就
// `scrollIntoView({ block: "nearest" })`，把它滚进视口。两个方向都按「最近」规则（kit.ScrollNearest）：
// 纵向出去多少补多少；行比视口宽（表格横向溢出）时，横向把行首对齐到视口左缘——所以带着选中行切到表格视图时，
// 表格会先横向滚过内边距那一段（默认列下 17px），这是渲染层真实的呈现。
//
// 行列是窗口化的（kit.RowWindow，台账 B-047）：选中行可能没有声明，所以不再读它画出来的外框，纵向交给行列自己的
// 「就近滚进视野」请求（RevealNearest，行高全部量过之后才执行，位置是实测的），横向只需要表格（也是每一行）的左缘与宽度。
type tableReveal struct {
	// done 是已经滚到过的选中行 ID；与当前选中行不同就要滚。没有选中行时复位，下次选中同一行仍会滚（effect 依赖 selectedId）。
	done string
	// originX 与 originY 是滚动内容左上角的窗口坐标，tableX 是表格左缘的窗口坐标，rowsY 是行列（第 0 行）上沿的窗口坐标：
	// 都是同一帧记下的，相减就是它们在滚动内容里的位置，与当前偏移无关。rowsOK 表示 rowsY 已经记过（至少画出过一行）。
	originX, originY float32
	tableX           float32
	rowsY            float32
	rowsOK           bool
}

// noteRowsOrigin 在一行画出来时记下行列上沿的窗口坐标：这一行的上缘减去它在行列里的位置。
// 行高全部量过之后位置表是精确的，任何一条声明的行都给出同一个值，最后画的那条留下它。
// 吸附表头只是把表头下移、行列用负的上外边距拉回，行列的自然位置不受影响，所以这个坐标随滚动内容走。
func (s *Screen) noteRowsOrigin(d *ui.DrawContext, index int) {
	top, _, _ := s.table.window.ItemSpan(index)
	s.table.reveal.rowsY, s.table.reveal.rowsOK = d.Rect.Y-top, true
}

// noteOrigin 记下滚动内容左上角的窗口坐标（内容列自己的外框）。
func (s *Screen) noteOrigin(d *ui.DrawContext) {
	s.table.reveal.originX, s.table.reveal.originY = d.Rect.X, d.Rect.Y
}

// revealSelected 在声明二维滚动区的内容之前调用：选中行还没滚过就按「最近」规则把它滚进视口。
// 要等表格的几何到手（至少画过一帧）：横向立刻滚，纵向向行列挂一个请求，由行列在这一帧声明窗口之前执行。
func (s *Screen) revealSelected(view kit.Scroll2D, list ListView) {
	r := &s.table.reveal
	selected := list.SelectedID
	if selected == "" {
		r.done = ""
		return
	}
	if r.done == selected || view.State == nil || s.table.tableW <= 0 || view.ViewportW <= 0 {
		return
	}
	i := slices.IndexFunc(list.Entries, func(e services.LibraryEntry) bool { return e.ID == selected })
	if i < 0 {
		return
	}
	left := r.tableX - r.originX
	if dx := kit.ScrollNearest(left-view.OffsetX, left+s.table.tableW-view.OffsetX, 0, view.ViewportW); dx != 0 {
		view.State.ScrollToX(view.OffsetX + dx)
	}
	s.table.window.RevealNearest(i)
	r.done = selected
}

// RowContentTop 是上一帧第 i 行（从 0 起）的上缘在滚动内容里的位置；对照场景用它算要滚到哪里（与渲染层的
// `row.getBoundingClientRect().top - scroller.top + scroller.scrollTop` 同一口径）。
// 表格还没量完每一行（位置只是估计）、没画出过行或没有这一行时第二个返回值为假。
func (s *Screen) RowContentTop(i int) (float32, bool) {
	r := &s.table.reveal
	top, _, exact := s.table.window.ItemSpan(i)
	if !r.rowsOK || !exact {
		return 0, false
	}
	return r.rowsY - r.originY + top, true
}
