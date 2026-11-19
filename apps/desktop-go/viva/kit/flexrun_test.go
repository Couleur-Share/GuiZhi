package kit

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 窗口化的行段（FlexColumn.Run，台账 B-035）：先对拍算术，再在无头会话里与「每一行都声明」的参照逐步对拍——
// 看得见的文字位置、内容总高、滚动偏移都必须分毫不差，覆盖不收缩、部分收缩、全部压到下限三种溢出状态与 1 万行。

const runRowH = 36 // 行的内容高；FlexItem.MinHeight 是 32，所以溢出时能被压到 32

// ---- 纯算术 ----

func TestRunSpanMatchesBruteForce(t *testing.T) {
	// 第一行的上缘与每行占高取不能精确表示的小数，逐个偏移与「逐行判断」对拍：没有差一行。
	for _, c := range []struct {
		top, pitch float64
		n          int
	}{{103.25, 32.5, 200}, {0, 35.9, 50}, {41, 32, 1}, {7.7, 32.04, 7}} {
		for _, overscan := range []int{0, 3} {
			for off := float32(-40); off < float32(c.top+c.pitch*float64(c.n))+200; off += 11.3 {
				lo, hi := off, off+500
				wantFirst, wantLast := -1, 0
				for i := 0; i < c.n; i++ {
					rowTop := c.top + float64(i)*c.pitch
					if wantFirst < 0 && rowTop+c.pitch > float64(lo) {
						wantFirst = i // 第一条下沿在视口上沿之下的行
					}
					if rowTop < float64(hi) {
						wantLast = i + 1 // 最后一条上沿在视口下沿之上的行
					}
				}
				if wantFirst < 0 { // 偏移落在内容之外（列表缩短之后的那一帧）：从末行起
					wantFirst = c.n - 1
				}
				wantLast = max(wantLast, wantFirst+1)
				wantFirst, wantLast = max(wantFirst-overscan, 0), min(wantLast+overscan, c.n)
				if first, last := runSpan(c.n, lo, hi, c.top, c.pitch, overscan); first != wantFirst || last != wantLast {
					t.Fatalf("top=%v pitch=%v n=%d 余量 %d 跨度 [%v, %v)：窗口 [%d, %d)，暴力判断 [%d, %d)", c.top, c.pitch, c.n, overscan, lo, hi, first, last, wantFirst, wantLast)
				}
			}
		}
	}
}

// ---- 无头会话 ----

// runRig 是一列「标题、段 a、标题、段 b、mt-auto 的页脚」的 FlexColumn，同一个版面的两种声明：窗口化（Run）与逐行声明。
type runRig struct {
	*harness
	flex     FlexColumn
	windowed bool
	na, nb   int
	extra    float32 // 非零时第一个标题之前多一个这么高的块（让各段的上缘整体下移）
	calls    int     // 最近一帧 Row 被调用的次数（只在窗口化的页面里累计）
	view     ScrollView
	overscan int       // 窗口化时的余量；0 取默认
	estimate float32   // 窗口化时的行高估计；0 取默认
	item     *FlexItem // 两种声明共用的行的 flex 属性；nil 取 {Shrink, MinHeight 32}
	runItem  *FlexItem // 仅窗口化的声明改用的 flex 属性（测外边距被忽略）；nil 沿用 item
	blind    bool      // 为真时窗口化的行段看到的视口高度是 0（还不知道）
}

func (r *runRig) rowItem() FlexItem {
	if r.item != nil {
		return *r.item
	}
	return FlexItem{Shrink: true, MinHeight: 32}
}

func (r *runRig) row(c ui.C, prefix string, i int) {
	ui.Column(c.Key(fmt.Sprintf("%s%d", prefix, i))).FillWidth().Height(runRowH).Children(func() {
		ui.Text(c, fmt.Sprintf("%s-%d", prefix, i))
	})
}

// heading 对应侧栏的分区标题：上下有外边距，不收缩。
func (r *runRig) heading(c ui.C, key string) {
	r.flex.Item(FlexItem{MarginTop: 16, MarginBottom: 4})
	ui.Column(c.Key(key)).FillWidth().Height(20).Children(func() { ui.Text(c, key) })
}

func (r *runRig) segment(c ui.C, prefix string, n int) {
	item := r.rowItem()
	if !r.windowed {
		for i := 0; i < n; i++ {
			r.flex.Item(item)
			r.row(c, prefix, i)
		}
		return
	}
	if r.runItem != nil {
		item = *r.runItem
	}
	view := r.view
	if r.blind {
		view.Viewport = 0
	}
	r.flex.Run(c, FlexRun{Key: prefix, N: n, Item: item, View: view, Overscan: r.overscan, Estimate: r.estimate, Row: func(i int) {
		r.calls++
		r.row(c, prefix, i)
	}})
}

func openRun(t *testing.T, windowed bool, na, nb int) *runRig {
	t.Helper()
	return openRunWith(t, windowed, na, nb, nil)
}

// openRunWith 在第一帧之前让 setup 调整试验台（余量、行的 flex 属性）。
func openRunWith(t *testing.T, windowed bool, na, nb int, setup func(*runRig)) *runRig {
	t.Helper()
	r := &runRig{windowed: windowed, na: na, nb: nb}
	if setup != nil {
		setup(r)
	}
	r.harness = openPage(t, still(), testViewW, testViewH, func(l *Look, c ui.C) {
		r.calls = 0
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollAreaView(c.Key("list"), testPad, func(view ScrollView) {
				r.view = view
				r.flex.Begin(view.Viewport)
				r.flex.Box(c.Key("body")).Children(func() {
					if r.extra > 0 {
						r.flex.Item(FlexItem{})
						ui.Column(c.Key("extra")).FillWidth().Height(r.extra).Children(func() { ui.Text(c, "EXTRA") })
					}
					r.heading(c, "ha")
					r.segment(c, "a", r.na)
					r.heading(c, "hb")
					r.segment(c, "b", r.nb)
					r.flex.Item(FlexItem{Shrink: true, MinAuto: true, MarginTopAuto: true})
					ui.Column(c.Key("foot")).FillWidth().Height(40).Children(func() { ui.Text(c, "FOOT") })
				})
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	r.frames(3)
	return r
}

// scrollBy 滚一下并出 frames 帧：窗口按上一帧的几何算，几何稳定时一帧就对了，再出一帧让「几何变了」的补帧有机会发生。
func (h *harness) scrollBy(dy float32, frames int) {
	h.t.Helper()
	if err := h.session.Scroll(gfx.ScrollEvent{X: testViewW / 2, Y: testViewH / 2, DeltaY: dy, Precise: true}); err != nil {
		h.t.Fatalf("滚动失败：%v", err)
	}
	h.frames(frames)
}

// runBound 是两段行一帧最多声明的行数：看得见的那一段 = 一屏的行数 + 首尾各半行 + 上下余量，
// 另一段在视口之外，只留一行加余量；再加 2 行取整的富余。
func runBound() int {
	return int(math.Ceil(testViewH/32.0)) + 1 + 2*defaultRowOverscan + 1 + defaultRowOverscan + 2
}

// sameRun 核对窗口化与参照：看得见的文字与位置、内容总高、滚动偏移都一致，登记数与声明数相符。
func sameRun(t *testing.T, step string, win, ref *runRig) {
	t.Helper()
	if win.flex.Mismatch() || ref.flex.Mismatch() {
		t.Fatalf("%s：登记的 flex 项数与实际声明的孩子数不符", step)
	}
	if n := len(ref.seen()); n < 3 { // 防空对空：参照里至少有几行文字，相等才有意义
		t.Fatalf("%s：参照里只看得见 %d 段文字：%v", step, n, ref.seen())
	}
	if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
		t.Fatalf("%s：%s", step, msg)
	}
	if math.Abs(float64(win.view.Content-ref.view.Content)) > 0.02 {
		t.Fatalf("%s：内容总高 窗口化 %.3f，参照 %.3f", step, win.view.Content, ref.view.Content)
	}
	if math.Abs(float64(win.view.Offset-ref.view.Offset)) > 0.02 {
		t.Fatalf("%s：滚动偏移 窗口化 %.3f，参照 %.3f", step, win.view.Offset, ref.view.Offset)
	}
}

func TestRunMatchesFullDeclaration(t *testing.T) {
	cases := []struct {
		name             string
		na, nb           int
		step             float32
		steps            int
		jumps            []float32
		noShrink         bool    // 行不收缩（占位的内容高要原样算进总高，不能靠「都压到下限」凑数）
		pitchLo, pitchHi float32 // 参照里相邻两行的间距应落在这个区间：核实这一档真的是想测的溢出状态
	}{
		// 未溢出：没有收缩，页脚被 mt-auto 推到底；窗口覆盖全部行。
		{name: "未溢出", na: 3, nb: 2, step: 120, steps: 3, pitchLo: 35.99, pitchHi: 36.01},
		// 14 行 + 标题 + 页脚略超出视口：部分收缩（每行压 1.x px，没到下限）。
		{name: "部分收缩", na: 7, nb: 7, step: 100, steps: 4, pitchLo: 32.5, pitchHi: 35.5},
		// 50 行：全部压到下限 32，窗口只覆盖其中一部分。
		{name: "压到下限", na: 30, nb: 20, step: 400, steps: 6, jumps: []float32{1e7, -1e7, 900}, pitchLo: 31.99, pitchHi: 32.01},
		// 700 行：窗口只是很小一部分。
		{name: "七百行", na: 400, nb: 300, step: 1500, steps: 8, jumps: []float32{4000, -2500, 1e7, -1e7, 7000, 1e7}, pitchLo: 31.99, pitchHi: 32.01},
		// 行不收缩：内容远超视口而每行仍是 36，占位的行高与整段总高都靠内容高算出来。
		{name: "不收缩的行", na: 30, nb: 20, noShrink: true, step: 400, steps: 6, jumps: []float32{1e7, -1e7, 900}, pitchLo: 35.99, pitchHi: 36.01},
	}
	for _, c := range cases {
		// 余量 0 取默认（3 行）；余量 1 让窗口紧贴看得见的行，几何上的小误差也会露出来。
		for _, overscan := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/余量%d", c.name, overscan), func(t *testing.T) {
				setup := func(r *runRig) {
					r.overscan = overscan
					if c.noShrink {
						r.item = &FlexItem{MinHeight: 32}
					}
				}
				win := openRunWith(t, true, c.na, c.nb, setup)
				ref := openRunWith(t, false, c.na, c.nb, setup)
				sameRun(t, "顶部", win, ref)
				seen := ref.seen()
				if pitch := seen["a-1"] - seen["a-0"]; pitch < c.pitchLo || pitch > c.pitchHi {
					t.Fatalf("参照里相邻两行的间距是 %.3f，这一档应在 [%v, %v]（没测到想测的溢出状态）", pitch, c.pitchLo, c.pitchHi)
				}
				for i := 0; i < c.steps; i++ {
					win.scrollBy(c.step, 2)
					ref.scrollBy(c.step, 2)
					sameRun(t, fmt.Sprintf("向下第 %d 步", i+1), win, ref)
				}
				for i := 0; i < c.steps; i++ {
					win.scrollBy(-c.step, 2)
					ref.scrollBy(-c.step, 2)
					sameRun(t, fmt.Sprintf("向上第 %d 步", i+1), win, ref)
				}
				for _, dy := range c.jumps { // 一口气跳到各处：下一帧起就对
					win.scrollBy(dy, 2)
					ref.scrollBy(dy, 2)
					sameRun(t, fmt.Sprintf("跳 %v", dy), win, ref)
					if dy > 1e6 { // 跳到底：页脚（mt-auto 的那一块）与最后一行都在
						for _, want := range []string{"FOOT", fmt.Sprintf("b-%d", c.nb-1)} {
							if _, ok := win.seen()[want]; !ok {
								t.Fatalf("跳 %v 之后窗口化的页面里看不见 %q：%v", dy, want, win.seen())
							}
						}
					}
				}
			})
		}
	}
}

// 段里的行不支持外边距：行的 flex 属性里带了外边距也不起作用（占位与已声明的行才始终同规格，窗口算术才成立），
// 画面与「行没有外边距」的整段声明一致。
func TestRunIgnoresMarginsOnTheRowItem(t *testing.T) {
	win := openRunWith(t, true, 30, 20, func(r *runRig) {
		r.runItem = &FlexItem{Shrink: true, MinHeight: 32, MarginTop: 20, MarginBottom: 10, MarginTopAuto: true}
	})
	ref := openRun(t, false, 30, 20)
	sameRun(t, "顶部", win, ref)
	for _, dy := range []float32{300, 1e7, -1e7, 500} {
		win.scrollBy(dy, 2)
		ref.scrollBy(dy, 2)
		sameRun(t, fmt.Sprintf("跳 %v", dy), win, ref)
	}
}

// 窗口化的列必须真的只声明窗口里的行：1 万 + 5000 行，任何时候一帧声明的行数都在一屏加余量之内。
func TestRunDeclaresOnlyTheWindowAtTenThousandRows(t *testing.T) {
	r := openRun(t, true, 10000, 5000)
	bound := runBound()
	check := func(step string) {
		t.Helper()
		if r.calls > bound {
			t.Fatalf("%s：一帧声明了 %d 行，窗口上限 %d", step, r.calls, bound)
		}
		if r.flex.Mismatch() {
			t.Fatalf("%s：登记的 flex 项数与实际声明的孩子数不符", step)
		}
	}
	check("顶部")
	// 顶部的窗口是精确的：看得见 a-0…a-17（视口含内边距共 600 高，第一行上缘 40，每行 32），上下余量各 3 行，
	// 所以 a-20 已经声明（画在视口之外、被裁掉），a-21 还没有；段 b 在视口之外，只留第一行加余量（b-0…b-3）。
	painted := map[string]bool{}
	for _, text := range r.session.Texts() {
		painted[text] = true
	}
	for _, want := range []string{"a-0", "a-17", "a-20", "b-0", "b-3"} {
		if !painted[want] {
			t.Errorf("顶部：%q 应已声明", want)
		}
	}
	for _, not := range []string{"a-21", "b-4"} {
		if painted[not] {
			t.Errorf("顶部：%q 不该声明（超出视口加余量）", not)
		}
	}
	for _, dy := range []float32{5000, 120000, 1e7, -1e7, 60000, 1e7} {
		r.scrollBy(dy, 3)
		check(fmt.Sprintf("跳 %v", dy))
	}
	if _, ok := r.seen()["FOOT"]; !ok {
		t.Fatalf("到底之后看不见页脚：%v", r.seen())
	}
	// 段 b 的最后一行在页脚之上看得见：占位孩子没有把总高算错。
	if _, ok := r.seen()["b-4999"]; !ok {
		t.Fatalf("到底之后看不见最后一行：%v", r.seen())
	}
}

// 数据变了、几何跟着变（第一段缩短、顶上多出一块）：窗口按旧几何声明的那一帧之后，补一帧就与参照一致。
func TestRunRecoversWhenGeometryChanges(t *testing.T) {
	win, ref := openRun(t, true, 400, 300), openRun(t, false, 400, 300)
	for _, r := range []*runRig{win, ref} {
		r.scrollBy(14000, 2) // 滚到第二段里
	}
	sameRun(t, "滚到第二段", win, ref)
	// settle 只靠窗口化的页面自己要的帧收敛：先把之前攒下的待办帧冲掉，出一帧之后它必须已经要了下一帧
	// （几何变了而窗口没盖住看得见的行），再让虚拟时钟走过去，把要的帧补出来，直到不再要帧；此时画面必须与参照一致。
	// （Transitioning 是「自上次推进时钟以来要过帧」，Frame() 不清它，所以补帧要用 Advance。）
	settle := func(step string) {
		t.Helper()
		win.flush()
		win.frames(1)
		if !win.session.Transitioning() {
			t.Fatalf("%s：几何变了而窗口没盖住看得见的行，这一帧之后应再要一帧", step)
		}
		for i := 0; win.session.Transitioning(); i++ {
			if i >= 4 {
				t.Fatalf("%s：补了 %d 帧还在要帧，没有收敛", step, i)
			}
			win.step()
		}
		ref.frames(4)
		sameRun(t, step, win, ref)
	}
	for _, r := range []*runRig{win, ref} {
		r.na, r.extra = 100, 80 // 第二段整体上移 300 × 32 px，再下移 80 + 外边距
	}
	settle("数据变了之后")
	for _, r := range []*runRig{win, ref} {
		r.na, r.extra = 400, 0 // 再变回来
	}
	settle("变回来之后")
	if bound := runBound(); win.calls > bound {
		t.Fatalf("收敛之后一帧声明了 %d 行，窗口上限 %d", win.calls, bound)
	}
}

// 稳态不要帧：窗口化的列在几何稳定时每一帧之后都不再要帧（否则成了空转的重绘）。
func TestRunAsksForNoFramesAtRest(t *testing.T) {
	r := openRun(t, true, 400, 300)
	for _, dy := range []float32{0, 3000, 1e7, -1e7} {
		if dy != 0 {
			r.scrollBy(dy, 3)
		}
		r.flush() // 先把滚动、首帧留下的待办帧冲掉；此后再出帧而不再要帧，才说明稳态没有空转
		r.frames(2)
		if r.session.Transitioning() {
			t.Fatalf("跳 %v 之后几何已稳定，窗口化的列不该再要帧", dy)
		}
	}
}

// flush 让虚拟时钟走过去，把已经要了的帧补出来，直到不再要帧（至多 20 步，防止空转时死循环）。
func (h *harness) flush() {
	h.t.Helper()
	for i := 0; i < 20 && h.session.Transitioning(); i++ {
		h.step()
	}
	if h.session.Transitioning() {
		h.t.Fatalf("冲了 20 步还在要帧")
	}
}

// step 把虚拟时钟推进一帧的间隔，到点的帧在这里补出来。
func (h *harness) step() {
	h.t.Helper()
	if err := h.session.Advance(20 * time.Millisecond); err != nil {
		h.t.Fatalf("推进时钟失败：%v", err)
	}
}

// 没有几何的第一帧（视口高度未知）声明最前面够铺满一屏的行，几何一到就收到窗口大小。
func TestRunFirstFrameDeclaresTopRowsThenSettles(t *testing.T) {
	r := &runRig{windowed: true, na: 5000, nb: 5000}
	first := -1
	r.harness = openPage(t, still(), testViewW, testViewH, func(l *Look, c ui.C) {
		if first < 0 && r.calls > 0 { // 第一帧的声明量：在重置之前取
			first = r.calls
		}
		r.calls = 0
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollAreaView(c.Key("list"), testPad, func(view ScrollView) {
				r.view = view
				r.flex.Begin(view.Viewport)
				r.flex.Box(c.Key("body")).Children(func() {
					r.heading(c, "ha")
					r.segment(c, "a", r.na)
					r.heading(c, "hb")
					r.segment(c, "b", r.nb)
				})
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	r.frames(3)
	if bound := runBound(); r.calls > bound {
		t.Fatalf("稳定之后一帧声明了 %d 行，窗口上限 %d", r.calls, bound)
	}
	// 两段各按回退视口（1080）铺满一屏、再加余量；窗口外那一段本来只该留几行，但没有几何时每一段都不知道自己在哪。
	screen := int(math.Ceil(float64(defaultRowFallback) / 32))
	if first < 2*screen || first > 2*(screen+defaultRowOverscan) {
		t.Fatalf("没有几何的第一帧声明了 %d 行，应在 [%d, %d] 之内（两段各按回退视口铺满一屏）", first, 2*screen, 2*(screen+defaultRowOverscan))
	}
}

// 几何已经有了而视口高度又变成未知（如界面重新挂载的第一帧）：按回退视口铺满一屏，
// 不按「0 高的视口」只声明几行；视口高度一回来就收回窗口大小。
func TestRunDeclaresAScreenWhenTheViewportIsUnknown(t *testing.T) {
	r := openRun(t, true, 5000, 5000)
	r.blind = true
	r.frames(1)
	screen := int(math.Ceil(float64(defaultRowFallback) / 32))
	if r.calls < 2*screen {
		t.Fatalf("视口高度未知的那一帧只声明了 %d 行，两段各应铺满一屏（至少 %d 行）", r.calls, 2*screen)
	}
	r.blind = false
	r.frames(3)
	if bound := runBound(); r.calls > bound {
		t.Fatalf("视口高度回来之后一帧声明了 %d 行，窗口上限 %d", r.calls, bound)
	}
}
