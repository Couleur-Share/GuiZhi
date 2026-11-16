package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// scrollbarPx 是渲染层 globals.css 里 ::-webkit-scrollbar 的宽度：8px。
const scrollbarPx float32 = 8

// ScrollArea 复刻渲染层的滚动容器（overflow-y: auto 加自定义的 webkit 滚动条）：
//   - 内容溢出时，滚动条槽（8px）占去布局宽度，内容区相应变窄——与 Chromium 的经典滚动条一致；
//   - 槽透明，拇指是 muted-foreground/20 的全圆角条，悬停 /40；
//   - 拇指长度 = 槽长 × 视口 ÷ 内容，位置按偏移比例；最短不小于槽宽。
//
// 是否溢出用上一帧布局的结果判断（ScrollState.Content/Viewport），首帧之后需要再画一帧才稳定：
// 外壳在窗口里每次交互都会重绘；无头测试用 Settle 补帧。
//
// pad 是内容四边的内边距（上、右、下、左），它在滚动条槽之内。
func (l *Look) ScrollArea(c ui.C, pad [4]float32, body func()) ui.Box {
	box := ui.Scroll(c)
	state := box.ScrollState()
	right := pad[1]
	if state.Content > state.Viewport+0.5 {
		right += scrollbarPx
	}
	return box.PadSides(pad[0], right, pad[2], pad[3]).
		DrawOver(func(d *ui.DrawContext) { l.paintScrollThumb(d, box.ScrollState()) }).
		Children(body)
}

// paintScrollThumb 在滚动区右缘画拇指。state.Content 是内容总高（含内边距），Viewport 是视口高（不含内边距）。
func (l *Look) paintScrollThumb(d *ui.DrawContext, state *ui.ScrollState) {
	view := d.Rect.Height
	content := state.Content + (d.Rect.Height - state.Viewport) // 内容 + 上下内边距 = 可滚动总高
	if content <= view+0.5 {
		return
	}
	track := view
	thumb := max(float32(int(track*view/content+0.5)), scrollbarPx)
	maxOffset := content - view
	offset := min(max(state.Offset, 0), maxOffset)
	top := (track - thumb) * offset / maxOffset
	color := l.T.MutedForeground.WithAlpha(0.2)
	bounds := gfx.Rect{X: d.Rect.X + d.Rect.Width - scrollbarPx, Y: d.Rect.Y + top, Width: scrollbarPx, Height: thumb}
	d.Canvas.Rounded(bounds, scrollbarPx/2, d.Fade(l.Col(color)))
}
