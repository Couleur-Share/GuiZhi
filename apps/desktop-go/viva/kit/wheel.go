package kit

import "github.com/vivaui/viva/gfx"

// RequestHoverRefresh 请求窗口在指针此刻的位置补一次指针移动（台账 B-037）。
//
// 渲染层（Chromium）在任何让指针底下换成别的元素的变化之后——滚轮、键盘滚动、Tab 把控件滚进视野、列表换了内容——
// 都会补发 pointerover / pointerenter 让悬停跟上（读数见 scripts/parity/tools/probe-list-keyboard.mjs 的 D1–D3：
// 方向键滚动、翻页换内容、Tab 滚动之后，悬停行都与指针底下的行一致，补发的事件都是 trusted 的）。
// Viva 只在指针事件里重算悬停：这里把「滚动区偏移变了」（settleReach）与「列表换了内容」两路都接到同一个请求上。
// 没有接线（HoverRefresh 为空）时什么也不做。
func (l *Look) RequestHoverRefresh() {
	if l.HoverRefresh != nil {
		l.HoverRefresh()
	}
}

// RefreshHoverAfterWheel 包住窗口的 OnScroll：滚轮把内容滚动之后，请求窗口在原位补一次指针移动，让悬停跟上（台账 B-031）。
//
// 渲染层（Chromium）在滚动之后会立刻重算悬停：指针底下换成了别的元素，就在同一帧里补发 pointerout / pointerleave /
// pointerover / pointerenter 与对应的 mouse 事件（没有 pointermove / mousemove），行与卡片的悬停高亮、手形光标、
// 悬停提示（TooltipLayer 听的正是 pointerover）随之跟上，不需要鼠标动一下。读数见 scripts/parity/tools/probe-wheel-hover.mjs：
// 滚一格、滚三格、小步滚、连滚都在 1–8 ms 内一致；滚不动的滚轮（已到底再滚）什么也不补发。
// Viva v0.6.4 只在指针事件里重算悬停，滚轮不算（app/painter.go 的 scroll 不重新命中测试），悬停会停在滚走的那个元素上。
//
// post 由窗口实现：把一次指针移动异步送回本窗口的消息队列（Windows 上是 PostMessage(WM_MOUSEMOVE)，hover_windows.go），
// 因此补发的移动在这一次滚轮处理（连同它的重绘）结束之后才被处理，和渲染层「滚动之后刷新」的先后一致。
// x、y 是滚轮事件的位置（逻辑像素、窗口坐标），即指针所在处。
//
// 只在滚轮确实被某个滚动区接走（Handled 或 Repaint）时才请求：没有滚动就没有内容换位，也不该像真的指针移动那样重排提示。
// next 为空时当作什么也不接；post 为空时原样返回 next，不做任何事。
func RefreshHoverAfterWheel(next func(gfx.ScrollEvent) gfx.EventResult, post func(x, y float32)) func(gfx.ScrollEvent) gfx.EventResult {
	if post == nil {
		return next
	}
	return func(e gfx.ScrollEvent) gfx.EventResult {
		var result gfx.EventResult
		if next != nil {
			result = next(e)
		}
		if result.Handled || result.Repaint {
			post(e.X, e.Y)
		}
		return result
	}
}
