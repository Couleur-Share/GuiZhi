package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Scroll2D 是二维滚动区在上一帧布局之后的几何：表格据此算吸附表头与吸附列的位置。
// 偏移量由滚轮在事件到达时直接改（ui.ScrollState.Wheel），所以声明期间读到的就是这一帧要画的偏移，吸附元素不会慢一帧。
type Scroll2D struct {
	Offset, OffsetX     float32 // 已经滚过的高度、宽度
	Viewport, ViewportW float32 // 视口的高、宽（滚动条槽之内）
	Content, ContentW   float32 // 内容总高、总宽（含内容自己的内边距）

	// State 只在本帧声明期间有效，不要留着。
	State *ui.ScrollState
}

// ScrollArea2D 复刻渲染层的 overflow-auto 滚动容器（两个方向都可能溢出，带自定义的 webkit 滚动条）：
//   - 纵向溢出时右侧有一条 8px 宽的槽，横向溢出时底部有一条 8px 高的槽，槽占去布局空间，视口相应缩小——
//     与 Chromium 的经典滚动条一致；两条槽同时出现时右下角是一块 8×8 的空角；
//   - 槽透明，拇指是 muted-foreground/20 的全圆角条，长度 = 槽长 × 视口 ÷ 内容，最短不小于槽宽；
//   - 内边距（px-4 py-3）由内容自己带，随内容一起滚动，不在这里。
//
// 是否溢出用上一帧布局的结果判断（与 ScrollArea 同，首帧之后要再画一帧才稳定）。
// 滚轮：横向滚轮或 Shift+滚轮走横向（Viva 的 Sideways 区自带）。拇指暂不可拖（与 ScrollArea 一致，台账 B-031 的范围之外）。
func (l *Look) ScrollArea2D(c ui.C, body func(Scroll2D)) ui.Box {
	var (
		state    *ui.ScrollState
		vertical bool
		horiz    bool
	)
	outer := ui.Column(c).FillWidth().Grow(1).MinHeight(0)
	return outer.Children(func() {
		ui.Row(c.Key("2d-body")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			// 不写 FillHeight：滚动区的高度没有自己的尺寸时，它的偏好高度只算内边距（内容是它自己的事），
			// 写了就按内容高度往上传，把整条祖先链撑出窗口；行里的容器本来就会撑满行高。
			box := ui.Scroll(c.Key("2d-area")).Sideways().Grow(1).MinWidth(0)
			state = box.ScrollState()
			vertical = state.Content > state.Viewport+0.5
			horiz = state.ContentWidth > state.ViewportWidth+0.5
			view := Scroll2D{
				Offset: state.Offset, OffsetX: state.OffsetX,
				Viewport: state.Viewport, ViewportW: state.ViewportWidth,
				Content: state.Content, ContentW: state.ContentWidth, State: state,
			}
			area := l.reach.enter(state)
			box.DrawOver(func(d *ui.DrawContext) { l.settleReach(d, area) }).Children(func() { body(view) })
			l.reach.leave()
			if vertical {
				ui.Column(c.Key("2d-vbar")).Width(scrollbarPx).FillHeight().Shrink(0).Draw(func(d *ui.DrawContext) {
					l.paintBarV(d, state)
				})
			}
		})
		if horiz {
			ui.Row(c.Key("2d-hbar")).FillWidth().Height(scrollbarPx).Shrink(0).Children(func() {
				ui.Row(c.Key("2d-htrack")).Grow(1).MinWidth(0).FillHeight().Draw(func(d *ui.DrawContext) {
					l.paintBarH(d, state)
				})
				if vertical {
					ui.Slot(c.Key("2d-corner")).Size(scrollbarPx, scrollbarPx).Shrink(0)
				}
			})
		}
	})
}

// paintBarV 在纵向槽里画拇指：槽长是槽自己的高度（已让出了横向槽），拇指按偏移的比例定位。
func (l *Look) paintBarV(d *ui.DrawContext, st *ui.ScrollState) {
	view, content := st.Viewport, st.Content
	if content <= view+0.5 || d.Rect.Height <= 0 {
		return
	}
	track := d.Rect.Height
	thumb := max(float32(int(track*view/content+0.5)), scrollbarPx)
	maxOffset := content - view
	offset := min(max(st.Offset, 0), maxOffset)
	top := (track - thumb) * offset / maxOffset
	bounds := gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + top, Width: scrollbarPx, Height: thumb}
	d.Canvas.Rounded(bounds, scrollbarPx/2, d.Fade(l.Col(l.T.MutedForeground.WithAlpha(0.2))))
}

// paintBarH 在横向槽里画拇指。
func (l *Look) paintBarH(d *ui.DrawContext, st *ui.ScrollState) {
	view, content := st.ViewportWidth, st.ContentWidth
	if content <= view+0.5 || d.Rect.Width <= 0 {
		return
	}
	track := d.Rect.Width
	thumb := max(float32(int(track*view/content+0.5)), scrollbarPx)
	maxOffset := content - view
	offset := min(max(st.OffsetX, 0), maxOffset)
	left := (track - thumb) * offset / maxOffset
	bounds := gfx.Rect{X: d.Rect.X + left, Y: d.Rect.Y, Width: thumb, Height: scrollbarPx}
	d.Canvas.Rounded(bounds, scrollbarPx/2, d.Fade(l.Col(l.T.MutedForeground.WithAlpha(0.2))))
}
