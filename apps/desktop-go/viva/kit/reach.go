package kit

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Tab 可达与聚焦跟随滚动（台账 B-030）。
//
// 浏览器里 Tab 走遍文档里的每个控件：下一个控件在滚动区的视野之外时，焦点落过去，滚动区把它带进视野。
// Chromium 对聚焦元素的对齐是 CenterIfNeeded：完全在视野里不动；与视野不相交就居中；只露出一部分则就近靠边。
// Viva 不是这样：滚出滚动区的控件在声明时就没有命中区（Canvas.HitWithOptions 在命中区与裁剪区不相交时
// 直接返回），不在路由器的 Tab 环里，Tab 会越过它们；焦点所在的控件被滚走，路由器还会把焦点清掉。
//
// 这一层补上两件事，行为取自对渲染层的实测（scripts/parity/tools/probe-tab-reach.mjs；记录的副本在
// viva/app/testdata/tab-reach-react-800x600.json，viva/app/root_tabreach_test.go 逐键对照）：
//
//  1. 下一个停靠点完全在视野之外时，Tab 先按上面的规则把它滚进视野，再让路由器照常走这一步：返回 ContinueTraversal，
//     运行时重绘（带着新的滚动位置）、用新的命中区重建路由，随后同一个 Tab 接着走——落点、键盘环、禁用与 SkipTab
//     全部由路由器自己决定。出发点（原来持焦点的停靠点）在滚动之后仍在视野里时走这条路；焦点已被路由器清掉
//     （被滚走了）而出发点滚动之后又回到视野里时，先在重绘里把焦点还给它（Box.AutoFocus）再续走。
//     出发点滚动之后也出了视野（相邻两个停靠点相隔超过一屏）时路由器没有出发点可走，改为：滚动，
//     在滚动之后的那一帧对目标请求焦点（Box.AutoFocus），并再要一帧让焦点环画出来；那一帧里焦点环
//     是否可见取决于按键时有没有焦点（Viva 的键盘模态跟着 Tab 时的焦点走）。
//     按 Shift+Tab 对称；下一个停靠点已经（至少露出一部分）在视野里时不插手，路由器自己走得到。
//  2. 键盘把焦点交到只露出一部分的停靠点上时，滚动区就近靠边把它滚到完全可见。点击不跟随。
//
// 停靠点由 kit 控件在声明时登记（noteStop / TabStop），按声明顺序排列，与路由器的 Tab 顺序一致：
// 登记在栈顶的滚动区名下，嵌套的滚动区各管各的。
// 窗口里每次绘制开始时调用 BeginPaint，窗口的 OnKey 经 TabKey 过一遍。
//
// 已知的边界（台账 B-030 记录）：
//   - 从滚动区之外 Tab 进入一个已经滚动过的区域，路由器落在第一个看得见的停靠点，浏览器则落在第一个并滚到它；
//   - 窗口化的列表（RowWindow）只声明视窗里的行，那里的行内控件不登记，行为不变（B-029 单独评估）；
//   - 颜色选择器的弹层内部、下拉与菜单的选项在浮层里，不是滚动区的停靠点，也不登记；
//   - 焦点已被清掉、续走的出发点滚动之后也不在视野里（相隔超过一屏）时，落点的键盘环要等下一次按键才出现（无用例）。

// reachSet 是一个窗口里各滚动区的 Tab 登记，Look 持有一份（零值可用），窗口线程独占。
type reachSet struct {
	paint  uint64                         // BeginPaint 的次数：区域「活着」就是声明在最近一次绘制里
	areas  map[*ui.ScrollState]*reachArea // 以滚动状态指针为键；没被最近两次绘制声明的在 BeginPaint 里清掉
	stack  []*reachArea                   // 声明期间的嵌套滚动区，栈顶接收停靠点
	latest *reachArea                     // 最近一次有停靠点持有键盘焦点的区域
}

// reachArea 是一个滚动区的停靠点登记与上一次绘制的快照。
type reachArea struct {
	paint uint64
	state *ui.ScrollState // 只在 paint 那一次绘制之后、下一次绘制之前有效（Viva：没被声明的区域状态会被丢弃）

	boxes []ui.Box // 本次绘制声明的停靠点，声明顺序；盒句柄只在本帧有效，快照之后清空

	snap    []gfx.Rect // 上一次绘制里各停靠点的外框（窗口坐标）
	view    gfx.Rect   // 滚动区的可视矩形（窗口坐标，含内边距）
	offset  float32    // 快照时的滚动偏移
	max     float32    // 最大偏移
	focused int        // 持有键盘焦点的停靠点，没有为 -1
	anchor  int        // 最近一次持有过焦点的停靠点；焦点因滚出视野被路由器清掉后，Tab 从这里接着走
	want    int        // 出发点滚动之后也出了视野时：已经滚动、等下一帧请求焦点的目标停靠点，没有为 -1
	refocus int        // 焦点已被清掉时：滚动之后、续走 Tab 之前要先把焦点还给的出发点，没有为 -1
	trapped bool       // 快照时有模态焦点陷阱：停靠点都在陷阱之外，不插手

	// windowed 为真表示区里有窗口化的行列（RowWindow）：只声明视窗里的行，停靠点登记不完整，这个区不插手。
	windowed bool
}

// BeginPaint 在窗口每次绘制开始时调用（根的 View 开头）：给这次绘制编号，并清掉过期的区域记录。
func (l *Look) BeginPaint() {
	s := &l.reach
	s.paint++
	for key, a := range s.areas {
		if a.paint+1 < s.paint {
			delete(s.areas, key)
			if s.latest == a {
				s.latest = nil
			}
		}
	}
}

// enter 开始声明一个滚动区：取回（或新建）它的登记并压栈。
func (s *reachSet) enter(state *ui.ScrollState) *reachArea {
	if s.areas == nil {
		s.areas = map[*ui.ScrollState]*reachArea{}
	}
	a := s.areas[state]
	if a == nil || a.paint+1 < s.paint {
		a = &reachArea{focused: -1, anchor: -1, want: -1, refocus: -1}
		s.areas[state] = a
	}
	a.state, a.paint, a.windowed = state, s.paint, false
	a.boxes = a.boxes[:0]
	s.stack = append(s.stack, a)
	return a
}

// leave 结束声明一个滚动区。
func (s *reachSet) leave() { s.stack = s.stack[:len(s.stack)-1] }

// noteStop 把一个可聚焦控件登记为栈顶滚动区的 Tab 停靠点；不在滚动区里时什么也不做。
// 只登记 Tab 走得到的控件：禁用的、SkipTab 的不要调。
func (l *Look) noteStop(b ui.Box) {
	s := &l.reach
	if n := len(s.stack); n > 0 {
		a := s.stack[n-1]
		a.boxes = append(a.boxes, b)
	}
}

// TabStop 供界面直接声明的可聚焦控件（行内按钮等）登记为所在滚动区的 Tab 停靠点。
func (l *Look) TabStop(b ui.Box) { l.noteStop(b) }

// visible 报告 r 是否与滚动区的可视矩形相交（与 Viva 声明命中区的条件一致：相交面积为正）。
func (a *reachArea) visible(r gfx.Rect) bool {
	return r.Y < a.view.Y+a.view.Height && r.Y+r.Height > a.view.Y &&
		r.X < a.view.X+a.view.Width && r.X+r.Width > a.view.X
}

// reveal 算出要让 r 进入视野，滚动区该滚到哪里（Chromium 的 CenterIfNeeded）：
// 完全在视野里不动；与视野不相交居中；露出一部分就近靠边（比视野还高时对齐上沿）。
// 结果按设备像素取整（Chromium 的滚动位置落在设备像素上）并夹在 [0, max] 内；位置没有变化时 moved 为 false。
func (a *reachArea) reveal(r gfx.Rect, scale float32) (to float32, moved bool) {
	top, bottom := r.Y, r.Y+r.Height
	viewTop, viewBottom := a.view.Y, a.view.Y+a.view.Height
	var delta float32
	switch {
	case top >= viewTop && bottom <= viewBottom:
		return a.offset, false
	case bottom <= viewTop || top >= viewBottom:
		delta = (top+bottom)/2 - (viewTop+viewBottom)/2
	case r.Height > a.view.Height || top < viewTop:
		delta = top - viewTop
	default:
		delta = bottom - viewBottom
	}
	to = float32(math.Round(float64((a.offset+delta)*scale))) / scale
	to = min(max(to, 0), a.max)
	return to, to != a.offset
}

// settleReach 在滚动区画完（DrawOver）时取快照，并处理两件待办：
// 上一次按键滚动之后等着聚焦的停靠点（已经在视野里了就请求焦点），以及键盘落在半露停靠点上的跟随滚动。
func (l *Look) settleReach(d *ui.DrawContext, a *reachArea) {
	if a.windowed {
		a.snap, a.focused, a.anchor, a.want, a.refocus = a.snap[:0], -1, -1, -1, -1
		clear(a.boxes)
		a.boxes = a.boxes[:0]
		return
	}
	st := a.state
	a.view, a.offset, a.max = d.Rect, st.Offset, st.Max()
	a.trapped = !d.Canvas.TrappedFocusScope().IsZero()
	a.snap = a.snap[:0]
	focused, ring := -1, false
	for i, b := range a.boxes {
		a.snap = append(a.snap, b.Rect())
		if state := b.State(); state.Focused {
			focused, ring = i, state.FocusVisible
		}
	}
	a.focused = focused
	if focused >= 0 {
		l.reach.latest = a
	}

	if r := a.refocus; r >= 0 {
		// 续走 Tab 之前把焦点还给出发点：这次绘制之后运行时立刻重建路由并应用请求，随后同一个 Tab 从它出发。
		a.refocus = -1
		if r < len(a.boxes) && focused != r && a.visible(a.snap[r]) {
			a.boxes[r].AutoFocus()
		}
	}
	switch want := a.want; {
	case want >= 0:
		a.want = -1
		if want < len(a.boxes) && a.visible(a.snap[want]) {
			// 请求在这次绘制之后生效，帧已经发布出去了：再要一帧，焦点环才画得出来。
			a.boxes[want].AutoFocus()
			a.anchor = want
			d.Canvas.RequestFrame()
		}
	case focused >= 0 && focused != a.anchor:
		a.anchor = focused
		if ring {
			if to, moved := a.reveal(a.snap[focused], l.scalePx()); moved {
				st.ScrollTo(to)
				d.Canvas.RequestFrame()
			}
		}
	}
	clear(a.boxes)
	a.boxes = a.boxes[:0]
}

// scalePx 是设备像素与逻辑像素之比，未设置按 1。
func (l *Look) scalePx() float32 {
	if l.Scale > 0 {
		return l.Scale
	}
	return 1
}

// TabKey 是窗口 OnKey 的一环：Tab / Shift+Tab 的下一个停靠点完全在视野之外时，把它滚进视野并安排聚焦。
// 其余按键、其余情形都返回零值，交给路由器照常处理。
func (l *Look) TabKey(e gfx.KeyEvent) gfx.EventResult {
	if e.Key != gfx.KeyTab || e.Release || e.Modifiers&^gfx.ModifierShift != 0 {
		return gfx.EventResult{}
	}
	s := &l.reach
	a, at := s.origin(e.Target)
	if a == nil {
		return gfx.EventResult{}
	}
	next := at + 1
	if e.Modifiers&gfx.ModifierShift != 0 {
		next = at - 1
	}
	if next < 0 || next >= len(a.snap) || a.visible(a.snap[next]) {
		return gfx.EventResult{}
	}
	to, moved := a.reveal(a.snap[next], l.scalePx())
	if !moved {
		return gfx.EventResult{}
	}
	// 滚动之后出发点还在视野里：交给路由器续走这一步（见文件头）。
	from := a.snap[at]
	from.Y -= to - a.offset
	a.state.ScrollTo(to)
	if a.visible(from) {
		if a.focused != at {
			a.refocus = at
		}
		return gfx.EventResult{Repaint: true, ContinueTraversal: true}
	}
	a.want = next
	return gfx.EventResult{Handled: true, Repaint: true}
}

// origin 找出这次 Tab 从哪个区域的哪个停靠点出发：焦点就在某个停靠点上就是它；焦点为空时，
// 若最近持有过焦点的停靠点已被滚出视野（路由器因此清了焦点），从它接着走。其余情形没有出发点。
func (s *reachSet) origin(target gfx.ID) (*reachArea, int) {
	for _, a := range s.areas {
		if a.paint == s.paint && !a.trapped && a.focused >= 0 && a.focused < len(a.snap) {
			return a, a.focused
		}
	}
	if !target.IsZero() {
		return nil, -1
	}
	a := s.latest
	if a == nil || a.paint != s.paint || a.trapped || a.anchor < 0 || a.anchor >= len(a.snap) || a.visible(a.snap[a.anchor]) {
		return nil, -1
	}
	return a, a.anchor
}
