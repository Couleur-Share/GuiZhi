package kit

import (
	"math"

	"github.com/vivaui/viva/ui"
)

// 渲染层 ItemList 的「选中行滚进视野」：虚拟列表的 `virtualizer.scrollToIndex(index, { align: "auto" })`
// （@tanstack/virtual-core 3.14 的 getOffsetForIndex 与 getOffsetForAlignment，台账 B-045）。
// 它与浏览器的 scrollIntoView({ block: "nearest" })（ScrollNearest，表格视图用）不是一回事：
// 露出一部分的行也会被滚到边、末行靠到真实的最大滚动量、判定用「≥ / ≤」。
// 读数（scripts/parity/tools/probe-list-keyboard.mjs，1280×800、每页 20 条）：↓ 连按时选中行的下沿恰好对齐视口下沿
// （滚动量 1、46、91……，逐行 +45），到末行是 369 = 最大滚动量（比对齐下沿多出下内边距的 8px）；
// 从末行连按 ↑，选中行落到视口上沿之外时上沿对齐视口上沿（329、284、200、135，即各行上沿）。

// scrollAlign 是 scrollToIndex 的对齐方式。
type scrollAlign uint8

const (
	scrollAuto  scrollAlign = iota // 由当前位置决定：见 scrollIndexTarget
	scrollStart                    // 行的上沿对齐视口上沿
	scrollEnd                      // 行的下沿对齐视口下沿
)

// scrollIndexTarget 算 scrollToIndex(i, {align}) 该把滚动区滚到哪里。
//
//	start、end   行的上下沿（滚动区坐标：0 点是内容区的起点，上内边距在内）
//	offset、size 当前滚动量与视口高（含上下内边距，渲染层取整之后的 clientHeight）
//	maxOffset    浏览器的最大滚动量（scrollHeight − clientHeight）
//	last         是不是最后一行
//
// 返回目标滚动量、解析后的对齐方式，以及要不要滚（move 为假：auto 判定这一行已经完全在视口里）。
//
//   - auto：行的下沿到了视口下沿或更远（end ≥ offset + size）按「靠下」；否则上沿到了视口上沿或更远（start ≤ offset）按「靠上」；
//     两者都不是就不动。行比视口高时下沿优先。
//   - 靠下：目标是 end − size；末行改用最大滚动量（virtual-core 的注释：浏览器的真实上限里有边框与内边距，估计的总高里没有）。
//   - 靠上：目标是 start。
//   - 目标夹在 [0, maxOffset]。
func scrollIndexTarget(start, end, offset, size, maxOffset float32, last bool, align scrollAlign) (target float32, resolved scrollAlign, move bool) {
	if align == scrollAuto {
		switch {
		case end >= offset+size:
			align = scrollEnd
		case start <= offset:
			align = scrollStart
		default:
			return offset, scrollAuto, false
		}
	}
	if align == scrollEnd && last {
		return maxOffset, scrollEnd, true
	}
	to := start
	if align == scrollEnd {
		to = end - size
	}
	return min(max(to, 0), maxOffset), align, true
}

// scrollReq 是一次待办的「滚到第 index 行」。行高没量到之前位置只是估计，所以请求在行高还在变化的几帧里每帧按最新位置
// 重算一次目标（沿用第一次解析出的对齐方式，与 virtual-core 的 scrollState 同一做法），行高稳定之后作废。
type scrollReq struct {
	index   int
	align   scrollAlign
	applied bool // 已经算过至少一次（第一帧可能还没有滚动区的几何，那时不算）
}

// ScrollToIndex 请求下一次 Rows 把第 i 行按 scrollToIndex(i, { align: "auto" }) 的规则滚进视野。
// 请求在 Rows 里按这一帧最新的行位置执行，调用方不必先有几何；同一时刻只有最后一次请求有效。
func (w *RowWindow) ScrollToIndex(i int) { w.req = &scrollReq{index: i} }

// itemSpan 是第 i 行的上下沿（内容区坐标，不含上内边距，行距不计入下沿）。
func (w *RowWindow) itemSpan(i int) (start, end float32) {
	if w.fixed > 0 {
		top := float32(float64(i) * float64(w.fixed+w.gap))
		return top, top + w.fixed
	}
	return float32(w.top[i]), float32(w.top[i+1]) - w.gap
}

// scrollRequested 在 Rows 声明窗口之前执行待办的滚动请求：滚动之后 spec.View.Offset 同步到新位置，让紧接着的窗口计算
// 用新的位置声明行（不然跳得远时这一帧是空的）。首帧还没有滚动区的几何时请求留到下一帧。
// 滚动之后指针底下可能换了元素，补悬停刷新是滚动区自己的事（settleReach 每帧比较偏移，台账 B-037），这里不重复通知。
func (w *RowWindow) scrollRequested(c ui.C, spec *RowsSpec) {
	if w.applyScroll(&spec.View, spec.N) {
		c.Canvas().RequestFrame()
	}
}

// applyScroll 是 scrollRequested 的算术：返回请求是否因为还没有几何而延期。
func (w *RowWindow) applyScroll(view *ScrollView, n int) (deferred bool) {
	r := w.req
	if r == nil {
		return false
	}
	if r.index < 0 || r.index >= n {
		w.req = nil
		return false
	}
	if view.State == nil || view.Viewport <= 0 {
		return true // 首帧还没有布局：留到下一帧
	}
	start, end := w.itemSpan(r.index)
	start, end = start+view.Pad[0], end+view.Pad[0]
	size := float32(math.Round(float64(view.Viewport + view.Pad[0] + view.Pad[2])))
	maxOffset := max(view.Content-view.Viewport, 0)
	target, align, move := scrollIndexTarget(start, end, view.Offset, size, maxOffset, r.index == n-1, r.align)
	if !move {
		w.req = nil // auto 判定这一行已完全在视野里
		return false
	}
	r.align, r.applied = align, true
	if math.Abs(float64(target-view.Offset)) <= 0.5 {
		return false
	}
	view.State.ScrollTo(target)
	view.Offset = target
	return false
}
