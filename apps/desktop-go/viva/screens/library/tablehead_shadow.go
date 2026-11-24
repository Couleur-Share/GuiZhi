package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
)

// 吸附表头与二级侧栏右缘外阴影的叠放（台账 B-048）。
//
// 渲染层的 aside 是 z-20 的定位元素，box-shadow 投在右边的工作区上；表格的 thead 是 sticky z-30，层级更高。
// 表格横向滚动、表头的左缘越过侧栏右缘之后，表头不透明的底色把侧栏阴影盖掉，所以表头那一段的右缘外是纯表头色。
// Viva 的阴影在整行的 DrawOver 里补画（shell.paintPanelEdgeShadow），晚于一切子元素，要靠这里报告被盖住的纵向区间，
// 补画时把那一段挖掉（shell.ModuleView.OccludePanelShadow）。
//
// 时序：声明阶段 stickyAt 记下滚动区的位置与视口（noteView），绘制阶段表头的 Draw 里据此换算成画布坐标（noteHead），
// 整行的 DrawOver 在同一帧稍后调 Screen.OccludePanelShadow 读结果（viva@v0.6.4 ui/paint.go：Draw → 子元素 → DrawOver）。
// 每帧开头清零（workspaceBody 调 resetFrame）：不在表格视图、表头没越过侧栏右缘的帧，一律报告「没有」。
type headShadow struct {
	// 滚动区的横向、纵向偏移与视口的宽、高（逻辑像素，声明阶段由 stickyAt 记下）。
	offsetX, offsetY, viewW, viewH float32
	// 被表头盖住的纵向区间（画布绝对坐标）；ok 为假表示没有。
	top, bottom float32
	ok          bool
}

// resetFrame 清掉上一帧的记录。
func (h *headShadow) resetFrame() { *h = headShadow{} }

// noteView 记下本帧滚动区的位置与视口。
func (h *headShadow) noteView(view kit.Scroll2D) {
	h.offsetX, h.offsetY, h.viewW, h.viewH = view.OffsetX, view.Offset, view.ViewportW, view.Viewport
}

// noteHead 在表头的 Draw 里调用：rect 是表头的布局矩形（画布坐标），originX / originY 是滚动内容的原点（noteOrigin 记下的）。
// 滚动区视口的左缘就是侧栏的右缘（工作区紧贴侧栏），所以表头的左缘不超过视口左缘，才整条盖住侧栏阴影；
// 纵向区间取表头与视口的交集（表头吸附在表格底部时可能有一截被视口上沿裁掉，那一截下面是工具条，不在表头之下）。
func (h *headShadow) noteHead(rect gfx.Rect, originX, originY float32) {
	h.ok = false
	if h.viewW <= 0 || h.viewH <= 0 {
		return
	}
	left, top := originX+h.offsetX, originY+h.offsetY
	if rect.X > left+headShadowEdgeEps || rect.X+rect.Width <= left {
		return // 表头的左缘还在侧栏右缘的右边（没有盖满阴影条），或整个表头已经滚到视口左边
	}
	t, b := max(rect.Y, top), min(rect.Y+rect.Height, top+h.viewH)
	if b <= t {
		return
	}
	h.top, h.bottom, h.ok = t, b, true
}

// headShadowEdgeEps 是表头左缘与视口左缘比较时的容差（逻辑像素，布局的浮点误差）。
const headShadowEdgeEps = 0.01

// OccludePanelShadow 报告本帧表头盖住了侧栏右缘外阴影的哪一段纵向区间（画布绝对坐标）：接 shell.ModuleView.OccludePanelShadow。
func (s *Screen) OccludePanelShadow() (top, bottom float32, ok bool) {
	h := &s.table.head
	return h.top, h.bottom, h.ok
}

// noteTableHead 是 tableHead 的 Draw 里调用的入口：把表头矩形连同滚动内容原点交给 headShadow。
func (s *Screen) noteTableHead(d *ui.DrawContext) {
	r := &s.table.reveal
	s.table.head.noteHead(d.Rect, r.originX, r.originY)
}
