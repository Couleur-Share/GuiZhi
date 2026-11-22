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
	return l.ScrollAreaView(c, pad, func(ScrollView) { body() })
}

// ScrollView 是滚动区在上一帧布局之后的几何：窗口化的行列（RowWindow）据此决定这一帧声明哪些行。
// 首帧布局之前 Viewport 为 0。
type ScrollView struct {
	Offset   float32    // 已经滚过的高度
	Viewport float32    // 视口高（不含内边距）
	Content  float32    // 内容总高（不含内边距）
	Pad      [4]float32 // 内容四边的内边距（上、右、下、左）

	// State 是滚动区的状态：窗口化的行列在估计的内容高度收敛时用它让「停在末端」的视口继续停在末端（ScrollToEnd）。
	// 只在本帧声明期间有效，不要留着。
	State *ui.ScrollState

	reach *reachArea // 区里的 Tab 停靠点登记（reach.go）；窗口化的行列据此声明自己不登记
}

// markWindowed 告诉滚动区：区里的行是窗口化声明的（RowWindow），而且窗口裁掉了一部分行，停靠点只登记了视窗里的行，
// Tab 可达的补充在这个区里不插手（行为同补充之前，台账 B-029 单独评估）。窗口把整列都声明了的不调用它（RowWindow.noteTruncation）。
func (v ScrollView) markWindowed() {
	if v.reach != nil {
		v.reach.windowed = true
	}
}

// AtEnd 报告上一帧布局之后视口是否停在内容末端（内容不超出视口时也成立）。
func (v ScrollView) AtEnd() bool { return v.Offset >= v.Content-v.Viewport-0.5 }

// Span 是视口在「内容区坐标」里的上沿与下沿：内容区的 0 点在上内边距之后，
// 滚动区里能看见的是内边距在内的整块高度（视口高加上、下内边距）。
func (v ScrollView) Span() (lo, hi float32) {
	return v.Offset - v.Pad[0], v.Offset + v.Viewport + v.Pad[2]
}

// ScrollAreaView 是 ScrollArea，另把滚动区上一帧的几何交给 body。
func (l *Look) ScrollAreaView(c ui.C, pad [4]float32, body func(ScrollView)) ui.Box {
	return l.scrollArea(c, pad, false, body)
}

// ScrollAreaFlush 是 ScrollArea，但溢出恰好 0.5px 也算溢出（槽与拇指都出）：Chromium 按 1/64 像素判定溢出，
// 而 ScrollArea 的 0.5px 容差把「恰好 0.5」放成了没有滚动条。处理中心在小字号档下列表溢出恰好 0.5px，渲染层出了滚动条槽（M3-1，`inbox.all` 的 T2）。
// 容差没有统一改小：已通过对照的家族没有逐个核对过，改动留给下一次全量回归（见交付报告）。
func (l *Look) ScrollAreaFlush(c ui.C, pad [4]float32, body func()) ui.Box {
	return l.scrollArea(c, pad, true, func(ScrollView) { body() })
}

// ScrollAreaFlushView 是 ScrollAreaFlush，另把滚动区上一帧的几何交给 body（窗口化的行列据此声明视口附近的行，同 ScrollAreaView）。
func (l *Look) ScrollAreaFlushView(c ui.C, pad [4]float32, body func(ScrollView)) ui.Box {
	return l.scrollArea(c, pad, true, body)
}

// scrollArea 是 ScrollAreaView 的实现：溢出超过 0.5px 算溢出，inclusive 为真时恰好 0.5px 也算。
func (l *Look) scrollArea(c ui.C, pad [4]float32, inclusive bool, body func(ScrollView)) ui.Box {
	box := ui.Scroll(c)
	state := box.ScrollState()
	// 区里声明的可聚焦控件登记到这个区域名下（Tab 可达与聚焦跟随滚动，见 reach.go）；快照在画完时取。
	area := l.reach.enter(state)
	view := ScrollView{Offset: state.Offset, Viewport: state.Viewport, Content: state.Content, Pad: pad, State: state, reach: area}
	right := pad[1]
	if overflows(state.Content, state.Viewport, inclusive) {
		right += scrollbarPx
	}
	out := box.PadSides(pad[0], right, pad[2], pad[3]).
		DrawOver(func(d *ui.DrawContext) {
			l.paintScrollThumb(d, box.ScrollState(), inclusive)
			l.settleReach(d, area)
		}).
		Children(func() { body(view) })
	l.reach.leave()
	return out
}

// overflows 报告内容是否比视口高出了 0.5px 以上（inclusive 为真时恰好高出 0.5px 也算）。
func overflows(content, viewport float32, inclusive bool) bool {
	if inclusive {
		return content >= viewport+0.5
	}
	return content > viewport+0.5
}

// paintScrollThumb 在滚动区右缘画拇指。state.Content 是内容总高（含内边距），Viewport 是视口高（不含内边距）。
func (l *Look) paintScrollThumb(d *ui.DrawContext, state *ui.ScrollState, inclusive bool) {
	view := d.Rect.Height
	content := state.Content + (d.Rect.Height - state.Viewport) // 内容 + 上下内边距 = 可滚动总高
	if !overflows(content, view, inclusive) {
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
