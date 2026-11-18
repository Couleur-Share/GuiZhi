package kit

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 窗口化的行列：先测纯算术（上沿、可见区间、空位高度），再在无头会话里与「整表声明」的参照逐步对拍。

const (
	testViewH = 600
	testViewW = 800
	testGap   = 6
)

var testPad = [4]float32{12, 20, 12, 20}

// testRowHeight 让相邻的行高度不同（30、40、50、60 循环），平均值不等于任何一行。
func testRowHeight(i int) float32 { return float32(30 + (i%4)*10) }

func testKey(i int) string { return fmt.Sprintf("r%d", i) }

// ---- 纯算术 ----

func mathSpec(n int, view ScrollView) RowsSpec {
	return RowsSpec{N: n, Gap: 10, View: view, Key: testKey}
}

func TestRowWindowTopsUseSeedThenMeasured(t *testing.T) {
	w := &RowWindow{Seed: 50}
	spec := mathSpec(5, ScrollView{})
	w.align(spec)
	for i := 0; i <= 5; i++ {
		if want := float64(i) * 60; w.top[i] != want {
			t.Fatalf("没量过任何行时 top[%d] = %v，应为 %v（估计 50 + 行距 10）", i, w.top[i], want)
		}
	}
	// 量过两行：80 与 40，平均 60 成了没量过的行的估计。
	w.h[0], w.h[1] = 80, 40
	w.align(spec)
	want := []float64{0, 90, 140, 210, 280, 350}
	for i, v := range want {
		if w.top[i] != v {
			t.Fatalf("量过两行后 top = %v，应为 %v", w.top, want)
		}
	}
}

func TestRowWindowTotalMatchesPlainColumn(t *testing.T) {
	// 整列总高 = 各行高之和 + (行数 − 1) 个行距；top[N] 多算了末行之后的一个行距。
	w := &RowWindow{Seed: 33}
	n := 7
	spec := mathSpec(n, ScrollView{})
	w.align(spec)
	for i := 0; i < n; i++ {
		w.h[i] = testRowHeight(i)
	}
	w.align(spec)
	var sum float64
	for i := 0; i < n; i++ {
		sum += float64(testRowHeight(i))
	}
	if got, want := w.top[n]-10, sum+float64(n-1)*10; got != want {
		t.Fatalf("总高 = %v，应为 %v", got, want)
	}
}

func TestRowWindowVisibleRange(t *testing.T) {
	cases := []struct {
		name        string
		view        ScrollView
		first, last int
	}{
		// 每行 50 + 行距 10 = 60；视口 300、上下内边距 10：内容区坐标 [-10, 310)，相交的行是 0..5，各向外扩 2 行。
		{"顶部", ScrollView{Offset: 0, Viewport: 300, Pad: [4]float32{10, 0, 10, 0}}, 0, 8},
		// 偏移 600：内容区坐标 [590, 910)，第 9 行（540..590 加行距到 600）下沿 600 > 590，相交的行是 9..15。
		{"中部", ScrollView{Offset: 600, Viewport: 300, Pad: [4]float32{10, 0, 10, 0}}, 7, 18},
		// 偏移正好落在第 3 行（240）与第 4 行之间的边界：第 3 行（180..230 加行距到 240）已在视口之外，相交的行从第 4 行起。
		{"边界", ScrollView{Offset: 240, Viewport: 300}, 2, 11},
		// 视口在内容之上（偏移为负）：至少声明第 0 行。
		{"内容之上", ScrollView{Offset: -100, Viewport: 10}, 0, 3},
		// 首帧还没布局：按 Fallback（1080）声明：相交的行是 0..17，向下扩 2 行，向上到 0。
		{"首帧", ScrollView{}, 0, 20},
		// 偏移落在内容之外（列表缩短之后的那一帧）：从末行起。
		{"越界", ScrollView{Offset: 100000, Viewport: 300}, 97, 100},
	}
	for _, c := range cases {
		w := &RowWindow{Seed: 50, Overscan: 2}
		spec := mathSpec(100, c.view)
		w.align(spec)
		first, last := w.visible(spec)
		if first != c.first || last != c.last {
			t.Errorf("%s：区间 [%d, %d)，应为 [%d, %d)", c.name, first, last, c.first, c.last)
		}
	}
	// 一行都没有：什么也不声明。
	w := &RowWindow{Seed: 50}
	spec := mathSpec(0, ScrollView{Viewport: 300})
	w.align(spec)
	if first, last := w.visible(spec); first != 0 || last != 0 {
		t.Errorf("空列表的区间 [%d, %d)，应为 [0, 0)", first, last)
	}
}

func TestRowWindowKeyChangeDropsMeasurement(t *testing.T) {
	w := &RowWindow{Seed: 50}
	spec := mathSpec(3, ScrollView{})
	w.align(spec)
	w.h[0], w.h[1], w.h[2] = 70, 80, 90
	// 同一序号换了键（顶上插了一行）：旧的实测不能算到别的行头上。
	spec.Key = func(i int) string { return fmt.Sprintf("new%d", i) }
	w.align(spec)
	if got := w.Measured(); got != 0 {
		t.Fatalf("所有键都变了，还剩 %d 个实测高度", got)
	}
	// 键没变就留着。
	w.h[1] = 80
	w.align(spec)
	if w.h[1] != 80 || w.Measured() != 1 {
		t.Fatalf("键没变的实测被丢了：%v", w.h)
	}
	// 行数变少再变多，新增的行是没量过的。
	spec.N = 1
	w.align(spec)
	spec.N = 3
	w.align(spec)
	if w.h[1] != 0 || w.h[2] != 0 {
		t.Fatalf("缩短又加长之后，新增的行带着旧的实测：%v", w.h)
	}
}

func TestRowWindowEpochDropsMeasurement(t *testing.T) {
	w := &RowWindow{Seed: 50}
	spec := mathSpec(3, ScrollView{})
	spec.Epoch = RowEpoch{RootPx: 16}
	w.align(spec)
	w.h[0], w.h[1] = 70, 80
	spec.Epoch = RowEpoch{RootPx: 18}
	w.align(spec)
	if w.Measured() != 0 {
		t.Fatalf("字号档变了，实测高度应全部作废，还剩 %d 个", w.Measured())
	}
}

func TestFitKeepsPrefixAndClearsGrowth(t *testing.T) {
	s := fit([]int{1, 2, 3, 4}, 2)
	if !slices.Equal(s, []int{1, 2}) {
		t.Fatalf("缩短后 %v", s)
	}
	s = fit(s, 4) // 容量里还留着 3、4：增长出来的部分必须清零
	if !slices.Equal(s, []int{1, 2, 0, 0}) {
		t.Fatalf("在容量内增长后 %v，新增部分应为零", s)
	}
	s = fit(s, 8)
	if !slices.Equal(s, []int{1, 2, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("超出容量增长后 %v", s)
	}
}

// ---- 无头会话 ----

// windowRig 同一个版面的两种声明：窗口化与整表。
type windowRig struct {
	*harness
	w     *RowWindow
	calls int // 最近一帧 Row 被调用的次数（只在窗口化的页面里累计）
	n     int
	pad   [4]float32
	epoch RowEpoch
	order []int      // 第 i 行显示哪个下标（高度与文字都跟它走）；nil 是原顺序
	view  ScrollView // 最近一帧声明时滚动区给出的几何
	flat  float32    // 非零时所有行同高（让估计恰好等于实测，总高可以逐像素对拍）
}

func (r *windowRig) index(i int) int {
	if r.order != nil {
		return r.order[i]
	}
	return i
}

func (r *windowRig) row(c ui.C, i int) ui.Box {
	j := r.index(i)
	height := testRowHeight(j)
	if r.flat > 0 {
		height = r.flat
	}
	return ui.Column(c.Key(testKey(j))).FillWidth().Height(height).Children(func() {
		ui.Text(c, fmt.Sprintf("row-%d", j))
	})
}

func (r *windowRig) tail(c ui.C) {
	ui.Column(c.Key("tail")).FillWidth().Height(36).Children(func() { ui.Text(c, "TAIL") })
}

func openWindowed(t *testing.T, n int) *windowRig {
	t.Helper()
	r := &windowRig{w: &RowWindow{Seed: 45}, n: n, pad: testPad}
	r.harness = openPage(t, still(), testViewW, testViewH, func(l *Look, c ui.C) {
		r.calls = 0
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollAreaView(c.Key("list"), r.pad, func(view ScrollView) {
				r.view = view
				r.w.Rows(c, RowsSpec{
					Name: "rows", N: r.n, Gap: testGap, View: view, Epoch: r.epoch,
					Key: func(i int) string { return testKey(r.index(i)) },
					Row: func(i int) ui.Box { r.calls++; return r.row(c, i) },
					After: func() {
						r.tail(c)
					},
				})
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	return r
}

func openPlain(t *testing.T, n int) *windowRig {
	t.Helper()
	r := &windowRig{n: n, pad: testPad}
	r.harness = openPage(t, still(), testViewW, testViewH, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollAreaView(c.Key("list"), r.pad, func(view ScrollView) {
				r.view = view
				ui.Column(c.Key("rows")).FillWidth().Gap(testGap).Children(func() {
					for i := 0; i < r.n; i++ {
						r.row(c, i)
					}
					r.tail(c)
				})
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	return r
}

func (h *harness) wheel(dy float32) {
	h.t.Helper()
	if err := h.session.Scroll(gfx.ScrollEvent{X: testViewW / 2, Y: testViewH / 2, DeltaY: dy, Precise: true}); err != nil {
		h.t.Fatalf("滚动失败：%v", err)
	}
	h.frames(4)
}

// seen 是画面里看得见的文字（外框与视口相交）及其上沿；窗口外被裁掉的文字不算。
func (h *harness) seen() map[string]float32 {
	out := map[string]float32{}
	for _, text := range h.session.Snapshot().Texts {
		b := text.Bounds
		if b.Y+b.Height <= 0 || b.Y >= testViewH {
			continue
		}
		out[text.Value] = b.Y
	}
	return out
}

func sameSeen(a, b map[string]float32) (string, bool) {
	for k, ya := range a {
		yb, ok := b[k]
		if !ok {
			return fmt.Sprintf("参照里没有 %q（窗口化 y=%.1f）", k, ya), false
		}
		if math.Abs(float64(ya-yb)) > 0.5 {
			return fmt.Sprintf("%q 的 y：窗口化 %.1f，参照 %.1f", k, ya, yb), false
		}
	}
	for k, yb := range b {
		if _, ok := a[k]; !ok {
			return fmt.Sprintf("窗口化里没有 %q（参照 y=%.1f）", k, yb), false
		}
	}
	return "", true
}

func TestRowWindowDeclaresOnlyTheWindow(t *testing.T) {
	r := openWindowed(t, 2000)
	first, last := r.w.Declared()
	if first != 0 {
		t.Fatalf("顶部的窗口应从第 0 行起，实际 [%d, %d)", first, last)
	}
	// 视口 600、每行约 45 + 6：看得见约 12 行，上下各扩 3 行。
	if r.calls > 25 {
		t.Fatalf("一帧声明了 %d 行：2000 行的列表只该声明窗口里的行", r.calls)
	}
	seen := r.seen()
	if _, ok := seen["row-0"]; !ok {
		t.Fatalf("第 0 行不在画面里：%v", seen)
	}
	if _, ok := seen["row-1500"]; ok {
		t.Fatal("第 1500 行不该出现在顶部的画面里")
	}
	if got := r.w.Measured(); got != r.calls {
		t.Fatalf("已量 %d 行，应与声明的 %d 行一致（声明过的都量到了）", got, r.calls)
	}
}

// restTime 是让会话「静下来」要花的虚拟时间：每一帧都在要下一帧的话，Rest 会一直走到五秒的上限。
func (h *harness) restTime() time.Duration {
	h.t.Helper()
	before := h.session.Now()
	if err := h.session.Rest(); err != nil {
		h.t.Fatalf("Rest 失败：%v", err)
	}
	return h.session.Now() - before
}

func TestRowWindowSettlesWithoutAskingForFrames(t *testing.T) {
	r := openWindowed(t, 300)
	if got := r.restTime(); got > 100*time.Millisecond {
		t.Fatalf("版面稳定之后窗口还在要帧：Rest 走了 %v（上限 5s 说明每帧都在要下一帧）", got)
	}
	r.wheel(900)
	if got := r.restTime(); got > 100*time.Millisecond {
		t.Fatalf("滚动并量完新行之后窗口还在要帧：Rest 走了 %v", got)
	}
}

// 量到新行之后窗口要再出一帧：位置要按实测重算，而真窗口里的帧是事件驱动的，没有谁在轮询，
// 不主动要的话，总高与下方空位要等下一次无关的重绘才会对上实测。
func TestRowWindowMeasuringAsksForAnotherFrame(t *testing.T) {
	r := openWindowed(t, 300)
	r.restTime() // 先让版面稳定
	if err := r.session.Scroll(gfx.ScrollEvent{X: testViewW / 2, Y: testViewH / 2, DeltaY: 900, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	r.frames(1) // 只出一帧：这一帧里第一次声明并量到新进窗口的行
	if got := r.restTime(); got == 0 {
		t.Fatal("量到新行之后窗口没有要再出一帧：位置要等下一次无关的重绘才会按实测对齐")
	}
}
func TestRowWindowMatchesFullListWhileScrolling(t *testing.T) {
	const n = 300
	win, ref := openWindowed(t, n), openPlain(t, n)
	check := func(step string) {
		t.Helper()
		if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
			t.Fatalf("%s：%s", step, msg)
		}
	}
	check("顶部")
	// 逐步往下：每步不超过两行的高度，新行先在窗口外的 Overscan 里量过，进视口时高度已知。
	for i := 0; i < 60; i++ {
		win.wheel(90)
		ref.wheel(90)
		check(fmt.Sprintf("向下第 %d 步", i+1))
	}
	// 再逐步回到顶部：走过的行都量过了。
	for i := 0; i < 60; i++ {
		win.wheel(-90)
		ref.wheel(-90)
		check(fmt.Sprintf("向上第 %d 步", i+1))
	}
}

func TestRowWindowJumpToEndLandsOnTheTail(t *testing.T) {
	const n = 400
	win, ref := openWindowed(t, n), openPlain(t, n)
	win.wheel(1e7)
	ref.wheel(1e7)
	win.frames(4)
	ref.frames(4)
	seen := win.seen()
	if _, ok := seen["TAIL"]; !ok {
		t.Fatalf("跳到末尾之后看不到尾部内容：%v", seen)
	}
	if _, ok := seen[fmt.Sprintf("row-%d", n-1)]; !ok {
		t.Fatalf("跳到末尾之后看不到最后一行：%v", seen)
	}
	// 末端的位置不依赖对上方行的估计：滚动偏移被夹到实际内容的末端，所以与整表一致。
	if msg, ok := sameSeen(seen, ref.seen()); !ok {
		t.Fatalf("跳到末尾的画面与整表不一致：%s", msg)
	}
	// 再整段回到顶部：顶端同样不依赖估计。
	win.wheel(-1e7)
	ref.wheel(-1e7)
	win.frames(4)
	if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
		t.Fatalf("回到顶部的画面与整表不一致：%s", msg)
	}
}

// 列表先短（读取中、空状态）后长：停在顶部，不能被「内容不超出视口时 AtEnd 恒成立」拽到底部。
// 这是导入队列第一次读取之后落在末尾的缺陷：整表参照没有这个问题，只在窗口化的收敛逻辑里出现。
func TestRowWindowGrowingFromShortListStaysAtTop(t *testing.T) {
	win := openWindowed(t, 3) // 3 行远不到一屏：此时 AtEnd 因为没有可滚范围而成立
	win.frames(4)
	if win.view.Content > win.view.Viewport {
		t.Fatalf("前提不成立：3 行应当不超出视口（内容 %.1f，视口 %.1f）", win.view.Content, win.view.Viewport)
	}
	win.n = 400
	win.frames(10)
	if win.view.Offset != 0 {
		t.Fatalf("列表变长之后视口被拽走了：偏移 %.1f，应当仍在顶部", win.view.Offset)
	}
	seen := win.seen()
	if _, ok := seen["row-0"]; !ok {
		t.Fatalf("变长之后顶部看不到第一行：%v", seen)
	}
	// 与整表一致：同一份数据不经过「先短」直接打开。
	ref := openPlain(t, 400)
	if msg, ok := sameSeen(seen, ref.seen()); !ok {
		t.Fatalf("变长之后的画面与整表不一致：%s", msg)
	}
}

func TestRowWindowReorderedRowsAreMeasuredAgain(t *testing.T) {
	const n = 30
	win := openWindowed(t, n)
	// 先把整列走一遍，让每一行都量过。
	for i := 0; i < 40; i++ {
		win.wheel(90)
	}
	if win.w.Measured() < n-1 {
		t.Fatalf("走完整列只量了 %d 行", win.w.Measured())
	}
	// 倒序：同一序号换了键，旧的实测全部作废，只剩重新声明的窗口里的行。
	win.order = make([]int, n)
	for i := range win.order {
		win.order[i] = n - 1 - i
	}
	win.frames(4)
	first, last := win.w.Declared()
	if got, max := win.w.Measured(), last-first; got > max {
		t.Fatalf("倒序之后还有 %d 个实测高度，窗口只有 %d 行：旧的实测被算到了别的行头上", got, max)
	}
	// 窗口里的行高度按新顺序重新量过：与整表参照对拍。
	ref := openPlain(t, n)
	ref.order = win.order
	ref.frames(4)
	for i := 0; i < 40; i++ {
		ref.wheel(90)
	}
	for i := 0; i < 40; i++ {
		win.wheel(-90)
		ref.wheel(-90)
	}
	if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
		t.Fatalf("倒序后回到顶部的画面与整表不一致：%s", msg)
	}
}

func TestRowWindowWidthChangeDropsMeasurement(t *testing.T) {
	win := openWindowed(t, 200)
	win.wheel(300)
	before := win.w.Measured()
	if before < 15 {
		t.Fatalf("滚动之后只量了 %d 行", before)
	}
	win.pad[1] += 60 // 内容区变窄：折行会变，旧的实测不再可信
	win.frames(5)
	first, last := win.w.Declared()
	if got, max := win.w.Measured(), last-first; got > max {
		t.Fatalf("宽度变了之后还有 %d 个实测高度，窗口只有 %d 行", got, max)
	}
}

func TestRowWindowAfterFollowsLastRowWithGap(t *testing.T) {
	win := openWindowed(t, 5) // 5 行都在窗口里
	ref := openPlain(t, 5)
	if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
		t.Fatalf("整列都在窗口里时与整表不一致：%s", msg)
	}
	if _, ok := win.seen()["TAIL"]; !ok {
		t.Fatal("5 行之后看不到尾部内容")
	}
}

// openFlat 打开行高统一为 45 的窗口化页面与整表参照：估计恰好等于实测，总高必须逐像素一致。
func openFlat(t *testing.T, n int) (win, ref *windowRig) {
	t.Helper()
	win = openWindowed(t, n)
	win.flat = 45
	ref = openPlain(t, n)
	ref.flat = 45
	win.frames(6)
	ref.frames(6)
	return win, ref
}

func TestRowWindowContentHeightMatchesPlainColumn(t *testing.T) {
	win, ref := openFlat(t, 500)
	check := func(step string) {
		t.Helper()
		if math.Abs(float64(win.view.Content-ref.view.Content)) > 0.5 {
			t.Fatalf("%s：窗口化的总高 %.1f，整表 %.1f（空位与行距算错了）", step, win.view.Content, ref.view.Content)
		}
		if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
			t.Fatalf("%s：%s", step, msg)
		}
	}
	check("顶部")
	win.wheel(7000)
	ref.wheel(7000)
	check("中部")
	win.wheel(1e7)
	ref.wheel(1e7)
	win.frames(4)
	check("末尾")
}
