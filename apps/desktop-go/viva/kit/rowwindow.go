package kit

import (
	"math"
	"sort"

	"github.com/vivaui/viva/ui"
)

// 默认值：没有任何实测时一行的估计高度、视口上下各多声明的行数、首帧还不知道视口高度时按多高来声明。
const (
	defaultRowSeed     float32 = 80
	defaultRowOverscan         = 3
	defaultRowFallback float32 = 1080
)

// RowEpoch 是影响行高的外观输入：它变了，已经量过的行高全部作废（根字号与界面语言会改变折行与行内间距）。
type RowEpoch struct {
	RootPx  float32 // 根字号（字号档）
	Variant int     // 其他会改变折行的输入，如界面语言
}

// RowsSpec 是一次窗口化声明的输入。
type RowsSpec struct {
	Name  string             // 行列容器的键名；空取 "rows"
	N     int                // 总行数
	Gap   float32            // 行与行之间的间距
	Key   func(i int) string // 第 i 行的稳定键（任务 ID 之类）：同一序号换了键，旧的实测高度作废；定高模式用不到，可为 nil
	Row   func(i int) ui.Box // 声明第 i 行，返回它的最外层盒子；窗口在绘制时读它布局后的高度
	View  ScrollView         // 所在滚动区上一帧布局后的几何（ScrollAreaView 给出）
	Epoch RowEpoch
	After func() // 声明在最后一行之后、同属这一列的内容（如「加载更早的任务」按钮），与行之间同样隔 Gap

	// Fixed 大于 0 是定高模式：每一行都恰好这么高（不含行距），由 Row 保证（Misfit 能查出不符的行）。
	// 位置按序号直接算：不估计、不实测、不随宽度作废，声明与布局只跟看得见的行数有关（与总行数无关，
	// 也不再逐帧遍历全部行的键），所以没有变高模式的两处近似：往上滚进没量过的行时的位移，与滑块拖到中段的换算
	// （台账 B-029 的 (1) 前半与 (2)）。仍然没有的：列表上方增删行时让可见内容不动的滚动锚定（(1) 后半），
	// 与焦点在滚出窗口的行上时的保留（(3)）。
	// 换了行高、或在定高与变高之间切换时，调用方应同时换 Epoch.Variant，让变高模式留着的旧实测作废。
	Fixed float32

	// MeasureAll 用于「渲染层没有窗口化、整表声明」的列表（知识库的表格）：每一行的高度要与整表声明时一模一样，
	// 不能有估计高度带来的位移。只要还有没量过的行（换了页、换了列配置、改了字号档，所有行都作废），这一帧就把整列声明出来，
	// 布局之后一次量完每一行；量完之后位置全是实测，窗口才开始只声明视口附近的行——视口里的画面与整表声明逐像素相同。
	// 代价是每次换内容多一帧整表声明（与不窗口化时同价），换来的是稳态每帧只跟看得见的行数有关。
	// 因为实测先于窗口，这一模式没有变高模式的估计收敛（停在末端的视口跟着落到末端）：不需要，也不该有——
	// 换页后视口留在原偏移，由布局夹取，与浏览器一致。
	MeasureAll bool
}

// RowWindow 把一长列高度不一的行窗口化：每帧只声明视口里的行与上下各 Overscan 行，窗口外的行用两块等高的空位顶着，
// 滚动条与内容总高因此仍按全部行算。声明、布局与绘制的开销随之只跟「看得见的行数」有关，与列表有多长无关
// （渲染层的采集队列没有窗口化，整表声明；一千行时一帧要几十毫秒）。
//
// 行高不预先知道：窗口外的行按「已量行高的平均」估计（还没量过任何行时按 Seed），行一旦声明过，布局后的高度就在绘制回调里记下来，
// 下一帧起位置按实测算。这与 Viva 自带的 ui.List 同一套办法，区别在于 ui.List 把每行当成可选的列表项
// （Option 角色、Tab 停靠、方向键选行、点击选中），那会给可访问树与键盘行为添上渲染层没有的东西，所以这里不用它。
//
// 定高模式（RowsSpec.Fixed）：行高在同一视图模式内固定的列表（知识库的卡片列表）不需要估计也不需要实测，位置按序号直接算，
// 下面的前两处近似（估计高度造成的位移、滑块拖到中段的换算）因此不存在，每帧的开销也与总行数无关；滚动锚定与焦点的局限仍在。
//
// 已知的局限（变高模式，台账 B-029）：滚动锚定。Chromium 在视口上方的内容变高或变矮时会调整滚动位置，让看得见的内容不动；
// 这里没有：往上滚进从没量过的行时，该行的实测高度与估计之差会让可见内容位移一次；列表上方插入新行，可见内容也会随之下移。
//
// 零值可用。一个窗口化的列表一个 RowWindow，只能在界面线程使用。
type RowWindow struct {
	Seed     float32 // 没有任何实测时一行的估计高度（不含行距）；0 取默认
	Overscan int     // 视口外上下各多声明几行；0 取默认
	Fallback float32 // 首帧还不知道视口高度时，按多高的视口来声明；0 取默认

	h     []float32 // 各行最近一次实测的高度，0 表示没量过
	key   []string  // h[i] 是为哪个键量的
	top   []float64 // top[i] 是第 i 行的上沿（含行距）；长度 N+1，top[N] 是整列的总高（含末行之后的一个行距）
	epoch RowEpoch
	width float32 // 上一次绘制时这一列的宽度：变了折行就变了，已量的行高全部作废
	rows  []windowRow
	draw  func(*ui.DrawContext) // measure 的方法值，只做一次（每帧现做一个方法值要分配）

	// converging 为真表示上一次绘制里有行第一次被量到（或宽度变了，所有实测作废重来）：估计的总高变了，不是内容变了。
	// 视口停在末端的话，下一帧让它继续停在末端（见 Rows）。已量行自己变高（内容真的变了）不算。
	// wasAtEnd 是上一帧声明时视口是否停在末端：滚轮或滚动条把视口送到「估计的末端」那一帧，量高的绘制还没发生，
	// 下一帧再看视口已经不在新估计的末端了，所以要记住更早一帧的状态。
	converging, wasAtEnd bool

	first, last int // 本帧声明的行区间 [first, last)

	unmeasured int // 本帧声明时还没有实测高度的行数（align 算出）；为 0 表示每一行的位置都是实测的

	// req 是待办的滚动请求（ScrollToIndex，rowscroll.go）。
	req *scrollReq

	// 最近一次 Rows 的参数，Reveal 据此换算第 i 行的位置。
	n          int
	gap        float32
	fixed      float32 // 定高模式的行高；0 是变高模式
	measureAll bool    // RowsSpec.MeasureAll

	// 定高模式下第一条实测高度与 Fixed 不符的行（-1 没有）与它的高度，见 Misfit。
	misfit  int
	misfitH float32
}

// windowRow 是本帧声明的一行：序号与它的盒子（布局之后读高度）。
type windowRow struct {
	index int
	box   ui.Box
}

// Declared 返回本帧声明的行区间 [first, last)。
func (w *RowWindow) Declared() (first, last int) { return w.first, w.last }

// Measured 返回已有实测高度的行数。
func (w *RowWindow) Measured() int {
	n := 0
	for _, h := range w.h {
		if h > 0 {
			n++
		}
	}
	return n
}

func (w *RowWindow) seed() float32 {
	if w.Seed > 0 {
		return w.Seed
	}
	return defaultRowSeed
}

func (w *RowWindow) overscan() int {
	if w.Overscan > 0 {
		return w.Overscan
	}
	return defaultRowOverscan
}

func (w *RowWindow) fallback() float32 {
	if w.Fallback > 0 {
		return w.Fallback
	}
	return defaultRowFallback
}

// Rows 声明窗口化的行列，返回这一列的容器。
//
// 列里依次是：窗口之前所有行的空位（有的话）、窗口里的行、窗口之后所有行的空位（有的话）、After。
// 列的 Gap 会落在相邻的每两个孩子之间，空位的高度因此各减一个 Gap，整列总高与不窗口化时相同。
func (w *RowWindow) Rows(c ui.C, spec RowsSpec) ui.Box {
	w.n, w.gap, w.fixed, w.measureAll = spec.N, spec.Gap, spec.Fixed, spec.MeasureAll
	if spec.Fixed > 0 {
		return w.fixedRows(c, spec)
	}
	// 拖滚动条到底、或滚轮一口气滚到底：视口停在「估计的末端」，实际总高要等末尾几行量过才知道。
	// 估计收敛的这几帧里让视口跟着落到真正的末端，否则会停在离末尾还差几行的地方
	// （浏览器里的虚拟列表同样有这个现象，这里补上）。
	// 「停在末端」要求内容真的超出视口：内容不超出时 AtEnd 恒成立（没有可滚的范围），
	// 那只是列表还短（读取中、刚从空状态进来），不是用户滚到了底；把它当成末端会让第一次量到行就把列表拽到底部。
	atEnd := spec.View.State != nil && spec.View.Content > spec.View.Viewport+0.5 && spec.View.AtEnd()
	if !spec.MeasureAll && w.converging && (atEnd || w.wasAtEnd) {
		spec.View.State.ScrollToEnd()
	}
	w.converging, w.wasAtEnd = false, atEnd
	w.align(spec)
	w.scrollRequested(c, &spec)
	first, last := w.visible(spec)
	w.first, w.last = first, last
	w.noteTruncation(spec, first, last)
	if w.draw == nil {
		w.draw = w.measure
	}
	name := spec.Name
	if name == "" {
		name = "rows"
	}
	n := spec.N
	return ui.Column(c.Key(name)).FillWidth().Gap(spec.Gap).Draw(w.draw).Children(func() {
		w.rows = w.rows[:0]
		if first > 0 {
			ui.Slot(c).FillWidth().Shrink(0).Height(float32(w.top[first]) - spec.Gap)
		}
		for i := first; i < last; i++ {
			w.rows = append(w.rows, windowRow{index: i, box: spec.Row(i)})
		}
		if last < n {
			ui.Slot(c).FillWidth().Shrink(0).Height(float32(w.top[n]-w.top[last]) - spec.Gap)
		}
		if spec.After != nil {
			spec.After()
		}
	})
}

// noteTruncation 在窗口真的裁掉了行（上方或下方有没声明的行）时，告诉滚动区：这里的停靠点登记不完整，Tab 可达的补充不插手。
// 窗口把整列都声明了（短的列表）时登记是完整的，序号稳定，补充照常工作：与不窗口化的列表行为一致。
func (w *RowWindow) noteTruncation(spec RowsSpec, first, last int) {
	if first > 0 || last < spec.N {
		spec.View.markWindowed()
	}
}

// align 让内部数组与这一帧的行对齐，并重算各行上沿。
func (w *RowWindow) align(spec RowsSpec) {
	n := spec.N
	w.h = fit(w.h, n)
	w.key = fit(w.key, n)
	w.top = fit(w.top, n+1)
	if spec.Epoch != w.epoch {
		w.epoch = spec.Epoch
		clear(w.h)
	}
	var sum float64
	known := 0
	for i := 0; i < n; i++ {
		if k := spec.Key(i); w.key[i] != k {
			w.key[i], w.h[i] = k, 0
		}
		if h := w.h[i]; h > 0 {
			sum += float64(h)
			known++
		}
	}
	w.unmeasured = n - known
	estimate := float64(w.seed())
	if known > 0 {
		estimate = sum / float64(known)
	}
	gap := float64(spec.Gap)
	var y float64
	for i := 0; i < n; i++ {
		w.top[i] = y
		h := float64(w.h[i])
		if h <= 0 {
			h = estimate
		}
		y += h + gap
	}
	w.top[n] = y
}

// visible 是这一帧要声明的行区间 [first, last)：与视口相交的行，再向两侧各扩 Overscan 行。
func (w *RowWindow) visible(spec RowsSpec) (first, last int) {
	n := spec.N
	if n == 0 {
		return 0, 0
	}
	if spec.MeasureAll && w.unmeasured > 0 { // 还有没量过的行：整列声明，布局之后一次量完（见 RowsSpec.MeasureAll）
		return 0, n
	}
	lo, hi := spec.View.Span()
	if spec.View.Viewport <= 0 { // 还没布局过：视口高度未知
		lo, hi = 0, w.fallback()
	}
	top := func(i int) float64 { return w.top[i] }
	// 第一条下沿在视口上沿之下的行；第一条上沿已在视口下沿之外的行。
	f := sort.Search(n, func(i int) bool { return top(i+1) > float64(lo) })
	l := sort.Search(n, func(i int) bool { return top(i) >= float64(hi) })
	if f >= n { // 偏移还没收回到内容里（列表缩短之后的那一帧）：从末行起
		f = n - 1
	}
	ov := w.overscan()
	return max(f-ov, 0), min(max(l, f+1)+ov, n)
}

// measure 在绘制时读本帧各行布局后的高度：窗口外的两行 Overscan 虽然看不见，布局时同样排过，高度同样量得到。
// 有任何变化就再要一帧，下一帧起位置按新的实测算；稳定之后不再要帧。
func (w *RowWindow) measure(d *ui.DrawContext) {
	changed := false
	if width := d.Rect.Width; math.Abs(float64(width-w.width)) > 0.5 {
		w.width = width
		clear(w.h)
		changed, w.converging = true, true
	}
	for _, r := range w.rows {
		if r.index >= len(w.h) {
			continue
		}
		if h := r.box.Rect().Height; h > 0 && math.Abs(float64(h-w.h[r.index])) > 0.01 {
			if w.h[r.index] == 0 {
				w.converging = true // 第一次量到：估计被实测取代
			}
			w.h[r.index] = h
			changed = true
		}
	}
	if changed {
		d.Canvas.RequestFrame()
	} else if r := w.req; r != nil && r.applied {
		w.req = nil // 行高稳定了：位置不会再变，滚动请求办完
	}
}

// fixedRows 是定高模式的 Rows：第 i 行的上沿恰好是 i × (Fixed + Gap)，窗口与两块空位都由算术直接得出，
// 与行数无关（1 万行与 50 行一样只声明看得见的几行），也不依赖任何实测。
func (w *RowWindow) fixedRows(c ui.C, spec RowsSpec) ui.Box {
	w.converging, w.wasAtEnd = false, false // 变高模式留下的标志在定高模式下没有意义
	w.scrollRequested(c, &spec)
	first, last := w.fixedSpan(spec)
	w.first, w.last = first, last
	w.noteTruncation(spec, first, last)
	name := spec.Name
	if name == "" {
		name = "rows"
	}
	n, pitch := spec.N, float64(spec.Fixed+spec.Gap)
	return ui.Column(c.Key(name)).FillWidth().Gap(spec.Gap).Draw(w.checkFixed).Children(func() {
		w.rows = w.rows[:0]
		if first > 0 {
			ui.Slot(c).FillWidth().Shrink(0).Height(float32(float64(first)*pitch) - spec.Gap)
		}
		for i := first; i < last; i++ {
			w.rows = append(w.rows, windowRow{index: i, box: spec.Row(i)})
		}
		if last < n {
			ui.Slot(c).FillWidth().Shrink(0).Height(float32(float64(n-last)*pitch) - spec.Gap)
		}
		if spec.After != nil {
			spec.After()
		}
	})
}

// fixedSpan 是定高模式这一帧要声明的行区间 [first, last)：与视口相交的行，再向两侧各扩 Overscan 行。
func (w *RowWindow) fixedSpan(spec RowsSpec) (first, last int) {
	n := spec.N
	if n == 0 {
		return 0, 0
	}
	lo, hi := spec.View.Span()
	if spec.View.Viewport <= 0 { // 还没布局过：视口高度未知
		lo, hi = 0, w.fallback()
	}
	pitch := float64(spec.Fixed + spec.Gap)
	// 第一条下沿在视口上沿之下的行；第一条上沿已在视口下沿之外的行。偏移落在内容之外（列表缩短之后的那一帧）时从末行起。
	f := min(max(int(math.Floor(float64(lo)/pitch)), 0), n-1)
	l := int(math.Ceil(float64(hi) / pitch))
	ov := w.overscan()
	return max(f-ov, 0), min(max(l, f+1)+ov, n)
}

// checkFixed 在绘制时核对本帧各行的实测高度是否等于 Fixed：不符是调用方违反了定高的约定（版面会错位），
// 记下第一条，测试里用 Misfit 断言；运行时不为它多要一帧。
func (w *RowWindow) checkFixed(*ui.DrawContext) {
	if r := w.req; r != nil && r.applied {
		w.req = nil // 定高：位置是算出来的，一次到位
	}
	w.misfit, w.misfitH = -1, 0
	for _, r := range w.rows {
		if h := r.box.Rect().Height; h > 0 && math.Abs(float64(h-w.fixed)) > 0.01 {
			w.misfit, w.misfitH = r.index, h
			return
		}
	}
}

// Misfit 返回定高模式下最近一次绘制里第一条实测高度与 Fixed 不符的行（序号与实测高度）；没有不符的返回 ok=false。
func (w *RowWindow) Misfit() (index int, height float32, ok bool) {
	return w.misfit, w.misfitH, w.fixed > 0 && w.misfit >= 0
}

// ItemSpan 是第 i 行在行列坐标系里的上下沿（行距不计入下沿；行列的上沿是 0 点），取自最近一次 Rows 的位置表。
// exact 为真表示位置是精确的：定高模式按序号算，变高模式每一行都量过（MeasureAll 整列声明量完之后就是这样）；
// 否则按估计高度算，只能当近似用。序号越界时全返回零值。
func (w *RowWindow) ItemSpan(i int) (top, bottom float32, exact bool) {
	if i < 0 || i >= w.n {
		return 0, 0, false
	}
	if w.fixed > 0 {
		top = float32(float64(i) * float64(w.fixed+w.gap))
		return top, top + w.fixed, true
	}
	if i+1 >= len(w.top) {
		return 0, 0, false
	}
	return float32(w.top[i]), float32(w.top[i+1]) - w.gap, w.unmeasured == 0
}

// Reveal 把第 i 行滚进视野，就近对齐：已经完全在视野里就不动，在上方靠上沿，在下方靠下沿
// （键盘选行时浏览器的 scrollIntoView({block: "nearest"})）。view 取本帧声明时滚动区给出的几何。
// 定高模式下位置是精确的；变高模式按当前估计的位置，行量到之前是近似的。偏移的夹取由布局完成。
func (w *RowWindow) Reveal(view ScrollView, i int) {
	if view.State == nil || i < 0 || i >= w.n {
		return
	}
	var top, bottom float32
	if w.fixed > 0 {
		top = float32(float64(i) * float64(w.fixed+w.gap))
		bottom = top + w.fixed
	} else {
		if i+1 >= len(w.top) {
			return
		}
		top, bottom = float32(w.top[i]), float32(w.top[i+1])-w.gap
	}
	lo, hi := view.Span()
	switch {
	case top < lo:
		view.State.ScrollTo(top + view.Pad[0])
	case bottom > hi:
		view.State.ScrollTo(bottom - view.Viewport - view.Pad[2])
	}
}

// fit 把 s 的长度调到 n，保留前缀；新增的部分清零。
func fit[T any](s []T, n int) []T {
	old := len(s)
	if n <= old {
		return s[:n]
	}
	if n <= cap(s) {
		s = s[:n]
		clear(s[old:])
		return s
	}
	return append(s, make([]T, n-old)...)
}
