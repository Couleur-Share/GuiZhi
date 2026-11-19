package kit

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
)

// 整表声明一帧、量完之后才窗口化（RowsSpec.MeasureAll，台账 B-047）：渲染层没有窗口化的表格用它，
// 窗口里每一行的位置要与整表声明时逐像素相同，所以不能靠估计；以及它带出来的几件事：
// 行列的位置表（ItemSpan）、就近滚进视野（RevealNearest）、二维滚动区到窗口的几何换算（Scroll2D.RowsView）、
// 窗口没裁行时 Tab 可达照常工作（noteTruncation）。

// ---- 纯算术 ----

func TestRowWindowMeasureAllDeclaresEveryRowUntilAllAreMeasured(t *testing.T) {
	w := &RowWindow{Seed: 50, Overscan: 2}
	// 视口远在内容中段：不量完就整列声明；一行没量时与量了一部分时都一样。
	far := ScrollView{Offset: 3000, Viewport: 300, Pad: [4]float32{10, 0, 10, 0}}
	spec := mathSpec(100, far)
	spec.MeasureAll = true
	w.align(spec)
	if first, last := w.visible(spec); first != 0 || last != 100 {
		t.Fatalf("一行都没量过时区间 [%d, %d)，应整列声明 [0, 100)", first, last)
	}
	for i := 0; i < 99; i++ {
		w.h[i] = 50
	}
	w.align(spec)
	if w.unmeasured != 1 {
		t.Fatalf("量过 99 行，unmeasured 应为 1，实际 %d", w.unmeasured)
	}
	if first, last := w.visible(spec); first != 0 || last != 100 {
		t.Fatalf("还剩一行没量时区间 [%d, %d)，应整列声明", first, last)
	}
	// 量完：窗口按实测位置裁（每行 50 + 行距 10 = 60；偏移 3000 → 内容区坐标 [2990, 3310)，行 49..55，各扩 2 行）。
	w.h[99] = 50
	w.align(spec)
	if w.unmeasured != 0 {
		t.Fatalf("量完之后 unmeasured 应为 0，实际 %d", w.unmeasured)
	}
	if first, last := w.visible(spec); first != 47 || last != 58 {
		t.Fatalf("量完之后区间 [%d, %d)，应为 [47, 58)", first, last)
	}
	// 不开 MeasureAll 的窗口照旧按估计裁，没量过也不整列声明。
	plain := &RowWindow{Seed: 50, Overscan: 2}
	spec.MeasureAll = false
	plain.align(spec)
	if first, last := plain.visible(spec); last-first >= 100 {
		t.Fatalf("没有 MeasureAll 的窗口不该整列声明，区间 [%d, %d)", first, last)
	}
}

func TestRowWindowItemSpanReportsExactnessOnlyWhenEveryRowIsMeasured(t *testing.T) {
	w := &RowWindow{Seed: 50}
	spec := mathSpec(4, ScrollView{})
	w.n, w.gap = spec.N, spec.Gap
	w.align(spec)
	if _, _, exact := w.ItemSpan(1); exact {
		t.Fatal("一行都没量过，位置只是估计，exact 应为假")
	}
	for i, h := range []float32{40, 60, 40, 60} {
		w.h[i] = h
	}
	w.align(spec)
	for _, c := range []struct {
		i           int
		top, bottom float32
	}{{0, 0, 40}, {1, 50, 110}, {2, 120, 160}, {3, 170, 230}} { // 行距 10 不计入下沿
		top, bottom, exact := w.ItemSpan(c.i)
		if top != c.top || bottom != c.bottom || !exact {
			t.Errorf("第 %d 行：[%v, %v] exact=%v，应为 [%v, %v] exact=true", c.i, top, bottom, exact, c.top, c.bottom)
		}
	}
	for _, i := range []int{-1, 4} {
		if top, bottom, exact := w.ItemSpan(i); top != 0 || bottom != 0 || exact {
			t.Errorf("序号 %d 越界，应返回零值，实际 [%v, %v] exact=%v", i, top, bottom, exact)
		}
	}
	// 一行变回没量过：又不精确了。
	w.h[2] = 0
	w.align(spec)
	if _, _, exact := w.ItemSpan(0); exact {
		t.Fatal("有一行没量过，exact 应为假")
	}
}

func TestRowWindowItemSpanInFixedMode(t *testing.T) {
	w := &RowWindow{}
	w.n, w.gap, w.fixed = 10, 6, 40
	top, bottom, exact := w.ItemSpan(3)
	if top != 138 || bottom != 178 || !exact { // 3 × (40 + 6)
		t.Fatalf("定高模式第 3 行 [%v, %v] exact=%v，应为 [138, 178] exact=true", top, bottom, exact)
	}
}

// 「就近」：完全在视口里不动，出去多少补多少；夹在 [0, 最大滚动量]；不像虚拟列表的 scrollToIndex，末行不靠最大滚动量。
func TestScrollIndexTargetNearest(t *testing.T) {
	const size, maxOffset = 100, 500
	cases := []struct {
		name               string
		start, end, offset float32
		last               bool
		want               float32
		move               bool
	}{
		{"完全在视口里不动", 120, 160, 100, false, 100, false},
		{"下沿恰好压在视口下沿也不动（与 auto 不同）", 160, 200, 100, false, 100, false},
		{"下沿出去 1px 就补 1px", 170, 201, 100, false, 101, true},
		{"上沿出去 1px 就补 1px", 99, 139, 100, false, 99, true},
		{"在下方远处：下沿对齐视口下沿", 300, 340, 100, false, 240, true},
		{"在上方远处：上沿对齐视口上沿", 20, 60, 100, false, 20, true},
		{"末行照常下沿对齐（auto 会取最大滚动量 500）", 460, 500, 100, true, 400, true},
		{"比视口还高：对齐上沿，再夹在最大滚动量", 520, 640, 100, false, 500, true},
		{"目标夹在 0", 0, 40, 100, false, 0, true},
	}
	for _, c := range cases {
		got, _, move := scrollIndexTarget(c.start, c.end, c.offset, size, maxOffset, c.last, scrollNearest)
		if got != c.want || move != c.move {
			t.Errorf("%s：滚动量 %v（move %v），应为 %v（move %v）", c.name, got, move, c.want, c.move)
		}
	}
}

func TestScroll2DRowsViewTranslatesTheViewportIntoRowCoordinates(t *testing.T) {
	area := &reachArea{}
	v := Scroll2D{Offset: 50, Viewport: 600, Content: 2000, reach: area}.RowsView(120, 13)
	if v.Pad != [4]float32{120, 0, 13, 0} {
		t.Fatalf("Pad = %v", v.Pad)
	}
	if v.Viewport != 467 || v.Content != 1867 {
		t.Fatalf("视口 %v、内容 %v，应为 467、1867（各扣掉行列上方的 120 与下方的 13）", v.Viewport, v.Content)
	}
	// 视口在行列坐标里：内容坐标 [50, 650) 减去行列的上沿 120。
	if lo, hi := v.Span(); lo != -70 || hi != 530 {
		t.Fatalf("Span = [%v, %v]，应为 [-70, 530]", lo, hi)
	}
	// 最大滚动量（内容 − 视口）不变，所以「停在末端」的判定与原来一致。
	if !(Scroll2D{Offset: 1400, Viewport: 600, Content: 2000}.RowsView(120, 13)).AtEnd() {
		t.Error("偏移 1400 = 内容 2000 − 视口 600，应判为停在末端")
	}
	if (Scroll2D{Offset: 1390, Viewport: 600, Content: 2000}.RowsView(120, 13)).AtEnd() {
		t.Error("偏移 1390 不在末端")
	}
	v.markWindowed()
	if !area.windowed {
		t.Fatal("RowsView 应带着二维滚动区的 Tab 登记，窗口裁行时才能通知它")
	}
}

// 窗口把整列都声明了：登记完整，Tab 可达照常；裁掉任何一端的行才通知滚动区「不插手」。
func TestRowWindowMarksTheAreaWindowedOnlyWhenRowsAreOmitted(t *testing.T) {
	cases := []struct {
		name        string
		n           int
		first, last int
		want        bool
	}{
		{"整列都声明了", 10, 0, 10, false},
		{"空列表", 0, 0, 0, false},
		{"上方裁了行", 10, 1, 10, true},
		{"下方裁了行", 10, 0, 9, true},
		{"两端都裁了", 10, 3, 7, true},
	}
	for _, c := range cases {
		area := &reachArea{}
		w := &RowWindow{}
		w.noteTruncation(RowsSpec{N: c.n, View: ScrollView{reach: area}}, c.first, c.last)
		if area.windowed != c.want {
			t.Errorf("%s：windowed = %v，应为 %v", c.name, area.windowed, c.want)
		}
	}
}

// ---- 无头会话 ----

// 变高的窗口（估计与整列声明两种）也一样：裁了行才把滚动区标成窗口化，整列都声明了就不标。
func TestRowWindowVariableRowsMarkTheAreaOnlyWhileRowsAreOmitted(t *testing.T) {
	for _, measureAll := range []bool{false, true} {
		long := openWindowedWith(t, 300, func(r *windowRig) { r.measureAll = measureAll })
		if first, last := long.w.Declared(); first != 0 || last >= 300 {
			t.Fatalf("measureAll=%v：前提不成立，300 行的列表窗口应裁掉下方的行，实际 [%d, %d)", measureAll, first, last)
		}
		if !anyWindowed(long.look) {
			t.Errorf("measureAll=%v：窗口裁了行，滚动区应被标成窗口化", measureAll)
		}
		short := openWindowedWith(t, 8, func(r *windowRig) { r.measureAll = measureAll })
		if first, last := short.w.Declared(); first != 0 || last != 8 {
			t.Fatalf("measureAll=%v：前提不成立，8 行的列表窗口应声明全部，实际 [%d, %d)", measureAll, first, last)
		}
		if anyWindowed(short.look) {
			t.Errorf("measureAll=%v：窗口把整列都声明了，滚动区不该被标成窗口化", measureAll)
		}
	}
}

// 头几帧：第一帧整列声明（量每一行）；之后任何一帧只要裁了窗口，行高就已经全部量完；稳定之后只声明视口附近的行。
// （滚动条槽出现会让这一列变窄、已量的行高全部作废，那一帧又整列声明一次——所以不断言「第二帧起就是窗口」。）
func TestRowWindowMeasureAllDeclaresEverythingOnceThenWindows(t *testing.T) {
	const n = 300
	r := openWindowedWith(t, n, func(r *windowRig) { r.measureAll = true })
	if len(r.callLog) == 0 || r.callLog[0] != n {
		t.Fatalf("第一帧应整列声明 %d 行，各帧声明的行数 %v", n, r.callLog)
	}
	if r.partial != 0 {
		t.Fatalf("有 %d 帧在行高没量完时就裁了窗口，各帧声明的行数 %v", r.partial, r.callLog)
	}
	if r.calls > 40 {
		t.Fatalf("稳定之后一帧还在声明 %d 行：应只声明视口附近的行（各帧 %v）", r.calls, r.callLog)
	}
	if got := r.w.Measured(); got != n {
		t.Fatalf("整列声明那一帧应量完每一行，实际量到 %d 行", got)
	}
	if first, last := r.w.Declared(); first != 0 || last > 40 {
		t.Fatalf("顶部的窗口应是 [0, 约 25)，实际 [%d, %d)", first, last)
	}
	// 稳定之后不再要帧。
	if got := r.restTime(); got > 100*time.Millisecond {
		t.Fatalf("版面稳定之后窗口还在要帧：Rest 走了 %v（上限 5s 说明每帧都在要下一帧）", got)
	}
}

// 与整表声明逐帧对拍：高度参差不齐的行、没有任何估计收敛的过程，第一次滚动就与整表一致，
// 连一口气跳到几千像素之外也一样（估计模式的窗口要走完几帧才对得上）。总高也相同。
func TestRowWindowMeasureAllMatchesTheFullListAtEveryOffset(t *testing.T) {
	const n = 300
	win := openWindowedWith(t, n, func(r *windowRig) { r.measureAll = true })
	ref := openPlain(t, n)
	check := func(step string) {
		t.Helper()
		if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
			t.Fatalf("%s：%s", step, msg)
		}
		if d := win.view.Content - ref.view.Content; d > 0.5 || d < -0.5 {
			t.Fatalf("%s：内容总高 窗口化 %.1f，整表 %.1f", step, win.view.Content, ref.view.Content)
		}
	}
	check("顶部")
	for _, dy := range []float32{5000, 37, 900, -3000, 12345, -1e7, 1e7, -250} {
		win.wheel(dy)
		ref.wheel(dy)
		check(fmt.Sprintf("滚 %.0f 之后", dy))
	}
}

// 内容换了（同一序号换了键）：旧的实测全部作废，下一帧整列再声明一次、量完再窗口化，画面仍与整表一致。
func TestRowWindowMeasureAllMeasuresAgainWhenTheContentChanges(t *testing.T) {
	const n = 120
	win := openWindowedWith(t, n, func(r *windowRig) { r.measureAll = true })
	win.wheel(900)
	win.order = make([]int, n)
	for i := range win.order {
		win.order[i] = n - 1 - i
	}
	win.callLog, win.calls, win.partial = nil, 0, 0 // calls 里是换内容之前最后一帧的行数，不属于换内容之后
	win.frames(4)
	if len(win.callLog) == 0 || win.callLog[0] != n {
		t.Fatalf("内容换了之后第一帧应整列声明 %d 行，各帧声明的行数 %v", n, win.callLog)
	}
	if win.partial != 0 {
		t.Fatalf("有 %d 帧在行高没量完时就裁了窗口，各帧声明的行数 %v", win.partial, win.callLog)
	}
	if win.calls > 40 {
		t.Fatalf("量完之后还在声明 %d 行：%v", win.calls, win.callLog)
	}
	ref := openPlain(t, n)
	ref.order = win.order
	ref.frames(4)
	ref.wheel(900)
	if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
		t.Fatalf("换内容之后的画面与整表不一致：%s", msg)
	}
}

// 就近滚进视野：行列还没量完（位置只是估计）时请求挂着，量完这一帧才滚——滚早了会按估计偏；
// 下方的行下沿对齐视口下沿，上方的行上沿对齐视口上沿，已经完全在视口里的不动。
func TestRowWindowRevealNearestIsExactAfterMeasuring(t *testing.T) {
	const n = 300
	win := openWindowedWith(t, n, func(r *windowRig) {
		r.measureAll = true
		r.w.RevealNearest(250) // 第一帧之前就请求：几何与行高都还没有
	})
	win.frames(4)
	y, ok := win.seen()["row-250"]
	if !ok {
		t.Fatalf("请求滚到第 250 行之后它不在画面里：%v", win.seen())
	}
	if want := float32(testViewH) - testRowHeight(250); y < want-0.5 || y > want+0.5 {
		t.Fatalf("第 250 行在下方，应下沿对齐视口下沿（上沿 %.1f），实际上沿 %.1f", want, y)
	}
	// 已经完全在视口里：不动。
	before := win.view.Offset
	win.w.RevealNearest(250)
	win.frames(4)
	if win.view.Offset != before {
		t.Fatalf("行已完全在视口里，偏移不该变：%.1f → %.1f", before, win.view.Offset)
	}
	// 在上方：上沿对齐视口上沿（滚动区上沿在窗口坐标 0）。
	win.w.RevealNearest(100)
	win.frames(4)
	y, ok = win.seen()["row-100"]
	if !ok {
		t.Fatalf("请求滚到第 100 行之后它不在画面里：%v", win.seen())
	}
	if y < -0.5 || y > 0.5 {
		t.Fatalf("第 100 行在上方，应上沿对齐视口上沿（0），实际 %.1f", y)
	}
	// 末行：只把末行的下沿露出来（浏览器的 scrollIntoView），不像虚拟列表的 auto 那样靠到最大滚动量——行列后面还有内容。
	win.w.RevealNearest(n - 1)
	win.frames(4)
	y, ok = win.seen()["row-299"]
	if !ok {
		t.Fatalf("请求滚到末行之后它不在画面里：%v", win.seen())
	}
	if want := float32(testViewH) - testRowHeight(n-1); y < want-0.5 || y > want+0.5 {
		t.Fatalf("末行应下沿对齐视口下沿（上沿 %.1f），实际上沿 %.1f", want, y)
	}
	if win.view.AtEnd() {
		t.Fatal("就近对齐不该把视口靠到最大滚动量：末行后面还有内容，只需露出末行")
	}
	// 请求办完就作废：之后滚轮想滚到哪就在哪。
	win.wheel(300)
	if win.view.Offset == 0 || win.w.req != nil {
		t.Fatalf("请求办完之后不该还挂着（req=%v）或把视口拉回去（偏移 %.1f）", win.w.req, win.view.Offset)
	}
}

// 内容换了（行高全部作废）的同一帧发来的请求：视口的几何已经有了，但新内容的行高还没量，位置只是估计——
// 估计比实测大得多（Seed 200 对 30–60）时，按估计去滚会冲到很远的地方；请求要挂到量完再办，落点才是实测的位置。
func TestRowWindowRevealNearestWaitsForTheRemeasure(t *testing.T) {
	const n = 300
	win := openWindowedWith(t, n, func(r *windowRig) {
		r.measureAll = true
		r.w.Seed = 200
	})
	win.order = make([]int, n)
	for i := range win.order {
		win.order[i] = n - 1 - i
	}
	win.w.RevealNearest(200) // 第 200 行现在是 row-99
	win.frames(6)
	y, ok := win.seen()["row-99"]
	if !ok {
		t.Fatalf("请求滚到第 200 行（row-99）之后它不在画面里：%v", win.seen())
	}
	if want := float32(testViewH) - testRowHeight(99); y < want-0.5 || y > want+0.5 {
		t.Fatalf("row-99 应下沿对齐视口下沿（上沿 %.1f），实际上沿 %.1f：请求没等行高量完就滚了", want, y)
	}
}

// 滚到末端之后在下方加了行：浏览器里滚动量不动，不会粘在底部。整列声明的行高是一次量齐的，没有「估计被实测取代」要追；
// 借变高窗口的末端收敛去追，就会把视口拽到新的末端。
func TestRowWindowMeasureAllKeepsTheOffsetWhenRowsAreAppended(t *testing.T) {
	win := openWindowedWith(t, 120, func(r *windowRig) { r.measureAll = true })
	win.frames(4)
	win.wheel(1e7)
	if !win.view.AtEnd() {
		t.Fatalf("试验台没有滚到末端：偏移 %.1f", win.view.Offset)
	}
	before := win.view.Offset
	win.n = 160
	win.frames(8)
	if got := win.view.Offset; math.Abs(float64(got-before)) > 0.5 {
		t.Fatalf("下方加了行之后滚动量从 %.1f 变成 %.1f：整列声明的列表不该追着末端走", before, got)
	}
	if win.view.AtEnd() {
		t.Fatal("下方加了行之后视口不该仍在末端")
	}
}

// 视口高带小数（高 DPI 缩放）时：虚拟列表的各种对齐取整之后算，scrollIntoView（nearest）按小数算。
func TestRevealViewportRoundsOnlyForTheVirtualListAlignments(t *testing.T) {
	view := ScrollView{Viewport: 100.4, Pad: [4]float32{8, 0, 8, 0}}
	for _, c := range []struct {
		align scrollAlign
		want  float32
	}{{scrollAuto, 116}, {scrollStart, 116}, {scrollEnd, 116}, {scrollNearest, 116.4}} {
		if got := revealViewport(view, c.align); math.Abs(float64(got-c.want)) > 0.001 {
			t.Errorf("对齐 %d：视口高 %.3f，应为 %.3f", c.align, got, c.want)
		}
	}
}

// ---- 窗口没裁行时 Tab 可达照常 ----

// reachWindowRig 是滚动区里一列窗口化的按钮：8 枚时内容 256px、视口 110px（与 focus_reach_test 同一页面）。
type reachWindowRig struct {
	*harness
	w *RowWindow
	n int
}

func openReachWindow(t *testing.T, n, overscan int) *reachWindowRig {
	t.Helper()
	r := &reachWindowRig{w: &RowWindow{Seed: reachRowH, Overscan: overscan}, n: n}
	r.harness = openPage(t, still(), 200, reachViewH, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollAreaView(c.Key("area"), [4]float32{}, func(view ScrollView) {
				r.w.Rows(c, RowsSpec{
					N: r.n, View: view, Key: testKey, Fixed: reachRowH,
					Row: func(i int) ui.Box { return reachButton(l, c, fmt.Sprintf("b%d", i), reachName(i)) },
				})
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	return r
}

// anyWindowed 报告有没有滚动区被标成「窗口裁了行」（Tab 补充不插手）。
func anyWindowed(l *Look) bool {
	for _, a := range l.reach.areas {
		if a.windowed {
			return true
		}
	}
	return false
}

// 窗口把 8 枚按钮都声明了：Tab 逐个走遍，每次聚焦的按钮都在视野里（滚动区跟着滚）——与不窗口化的列表一样。
func TestRowWindowKeepsTabReachWhileTheWindowCoversEveryRow(t *testing.T) {
	r := openReachWindow(t, reachStops, 8)
	if first, last := r.w.Declared(); first != 0 || last != reachStops {
		t.Fatalf("前提不成立：窗口应声明全部 %d 行，实际 [%d, %d)", reachStops, first, last)
	}
	if anyWindowed(r.look) {
		t.Fatal("窗口没裁行，滚动区不该被标成窗口化")
	}
	for i := range reachStops {
		r.keys("tab")
		node, ok := r.focused()
		if !ok || node.Label != reachName(i) {
			t.Fatalf("第 %d 次 Tab 应聚焦「%s」，实际 %q", i+1, reachName(i), node.Label)
		}
		if node.Bounds.Y < 0 || node.Bounds.Y+node.Bounds.Height > reachViewH {
			t.Fatalf("第 %d 次 Tab 聚焦的「%s」不在视野里（%.1f..%.1f），滚动区没有跟着滚", i+1, reachName(i), node.Bounds.Y, node.Bounds.Y+node.Bounds.Height)
		}
	}
}

// 窗口裁了行：停靠点登记不完整，滚动区被标成窗口化、Tab 补充不插手（B-029 的局限，与补充之前一致）；
// 窗口又覆盖整列时（行数变少）标记随之撤掉。
func TestRowWindowMarksTheAreaOnlyWhileRowsAreOmitted(t *testing.T) {
	const n = 60
	r := openReachWindow(t, n, 1)
	if first, last := r.w.Declared(); first != 0 || last >= n {
		t.Fatalf("前提不成立：窗口应裁掉下方的行，实际 [%d, %d)", first, last)
	}
	if !anyWindowed(r.look) {
		t.Fatal("窗口裁了行，滚动区应被标成窗口化")
	}
	r.n, r.w.Overscan = reachStops, 8 // 行数变少、窗口放宽到覆盖整列：标记撤掉，Tab 补充恢复
	r.frames(4)
	if first, last := r.w.Declared(); first != 0 || last != reachStops {
		t.Fatalf("前提不成立：窗口应声明全部 %d 行，实际 [%d, %d)", reachStops, first, last)
	}
	if anyWindowed(r.look) {
		t.Fatal("窗口又覆盖整列了，窗口化的标记应当撤掉")
	}
}
