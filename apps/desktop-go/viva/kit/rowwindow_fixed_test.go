package kit

import (
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/internal/frametime"
)

// 定高模式（RowsSpec.Fixed）：位置按序号直接算。先对拍算术（与变高模式在行同高时给出同一个窗口，
// 再与逐行暴力判断对拍），然后在无头会话里与整表参照逐步对拍，最后验证 Reveal 与「违反定高约定」的检测。

// fixedRowH 与 windowRig 里变高模式的估计值（Seed 45）相同：参照用的整表也取这个高度。
const fixedRowH = 45

func openFixed(t *testing.T, n int) *windowRig {
	t.Helper()
	return openWindowedWith(t, n, func(r *windowRig) { r.fixed, r.flat = fixedRowH, fixedRowH })
}

// jump 滚一下，只出一帧：定高模式不需要等行量过，这一帧声明的窗口就已经对了。
func (h *harness) jump(dy float32) {
	h.t.Helper()
	if err := h.session.Scroll(gfx.ScrollEvent{X: testViewW / 2, Y: testViewH / 2, DeltaY: dy, Precise: true}); err != nil {
		h.t.Fatalf("滚动失败：%v", err)
	}
	h.frames(1)
}

// ---- 纯算术 ----

func TestFixedSpanMatchesVariableWindowWhenRowsAreEqual(t *testing.T) {
	views := []ScrollView{
		{Offset: 0, Viewport: 300, Pad: [4]float32{10, 0, 10, 0}},
		{Offset: 600, Viewport: 300, Pad: [4]float32{10, 0, 10, 0}},
		{Offset: 240, Viewport: 300},
		{Offset: -100, Viewport: 10},
		{}, // 首帧还没布局
		{Offset: 100000, Viewport: 300},
		// 行边界上下各一点：每行 50 + 行距 10，第 k 行的上沿是 60k。
		{Offset: 59.9, Viewport: 100}, {Offset: 60, Viewport: 100}, {Offset: 60.1, Viewport: 100},
		{Offset: 5900, Viewport: 100}, // 100 行的内容 6000 高：停在末端
	}
	for _, v := range views {
		for _, n := range []int{0, 1, 2, 100} {
			spec := RowsSpec{N: n, Gap: 10, Fixed: 50, View: v, Key: testKey}
			variable := &RowWindow{Seed: 50, Overscan: 2}
			variable.align(spec)
			wantFirst, wantLast := variable.visible(spec)
			first, last := (&RowWindow{Overscan: 2}).fixedSpan(spec)
			if first != wantFirst || last != wantLast {
				t.Errorf("N=%d 偏移 %v 视口 %v：定高窗口 [%d, %d)，变高窗口（行同高）[%d, %d)", n, v.Offset, v.Viewport, first, last, wantFirst, wantLast)
			}
		}
	}
}

func TestFixedSpanMatchesBruteForce(t *testing.T) {
	// 行距取不能精确表示的小数，逐个偏移与「逐行判断」对拍：没有差一行。
	for _, c := range []struct{ fixed, gap float32 }{{72.5, 8}, {33.3, 6.1}, {46, 0.4}} {
		const n, overscan = 200, 1
		pitch := float64(c.fixed + c.gap)
		w := &RowWindow{Overscan: overscan}
		for off := float32(-30); off < 20000; off += 13.7 {
			lo, hi := off, off+400
			wantFirst, wantLast := -1, 0
			for i := 0; i < n; i++ {
				top := float64(i) * pitch
				if wantFirst < 0 && top+pitch > float64(lo) {
					wantFirst = i // 第一条下沿（含其后的行距）在视口上沿之下的行
				}
				if top < float64(hi) {
					wantLast = i + 1 // 最后一条上沿在视口下沿之上的行
				}
			}
			if wantFirst < 0 { // 偏移落在内容之外（列表缩短之后的那一帧）：从末行起
				wantFirst = n - 1
			}
			wantLast = max(wantLast, wantFirst+1)
			wantFirst, wantLast = max(wantFirst-overscan, 0), min(wantLast+overscan, n)

			spec := RowsSpec{N: n, Gap: c.gap, Fixed: c.fixed, View: ScrollView{Offset: off, Viewport: 400}}
			if first, last := w.fixedSpan(spec); first != wantFirst || last != wantLast {
				t.Fatalf("行高 %v 行距 %v 偏移 %v：窗口 [%d, %d)，暴力判断 [%d, %d)", c.fixed, c.gap, off, first, last, wantFirst, wantLast)
			}
		}
	}
}

// ---- 无头会话 ----

func TestFixedRowsMatchFullListWhileScrolling(t *testing.T) {
	const n = 300
	win, ref := openFixed(t, n), openPlain(t, n)
	ref.flat = fixedRowH // 整表参照也用同一个行高
	ref.frames(3)
	check := func(step string) {
		t.Helper()
		if msg, ok := sameSeen(win.seen(), ref.seen()); !ok {
			t.Fatalf("%s：%s", step, msg)
		}
	}
	check("顶部")
	for i := 0; i < 60; i++ {
		win.wheel(90)
		ref.wheel(90)
		check(fmt.Sprintf("向下第 %d 步", i+1))
	}
	for i := 0; i < 60; i++ {
		win.wheel(-90)
		ref.wheel(-90)
		check(fmt.Sprintf("向上第 %d 步", i+1))
	}
	// 一口气跳到各处（拖滑块或滚轮一大步）：只出一帧就与整表一致，不像变高模式要等行量过再收敛。
	for _, dy := range []float32{4000, 3000, -2500, 1e7, -1e7, 6000, 1e7} {
		win.jump(dy)
		ref.jump(dy)
		check(fmt.Sprintf("跳 %v", dy))
	}
	// 停在末端：尾部内容的位置也与整表一致（总高按序号算得与整表分毫不差）。
	if _, ok := win.seen()["TAIL"]; !ok {
		t.Fatalf("到底之后看不见尾部：%v", win.seen())
	}
}

func TestFixedRowsDeclareOnlyTheWindowAtTenThousandRows(t *testing.T) {
	const n = 10000
	r := openFixed(t, n)
	if r.calls > 25 {
		t.Fatalf("一帧声明了 %d 行：1 万行的定高列表只该声明窗口里的行", r.calls)
	}
	if r.w.Measured() != 0 {
		t.Fatalf("定高模式不该实测行高，已量 %d 行", r.w.Measured())
	}
	pitch := float32(fixedRowH + testGap)
	// 内容总高 = 全部行高 + 行距（含尾部之前的一个）+ 尾部 36：窗口在顶部、中段、底部时都一样（空位只有一块、两块、一块）。
	wantContent := float32(n)*pitch + 36
	checkContent := func(where string) {
		t.Helper()
		if got := r.view.Content; math.Abs(float64(got-wantContent)) > 1 {
			t.Fatalf("%s：内容总高 %v，应为 %v", where, got, wantContent)
		}
	}
	checkContent("顶部")
	// 滚到正中：偏移恰好是第 5000 行的上沿（加上内边距）。
	r.jump(5000*pitch + r.pad[0])
	if r.calls > 25 {
		t.Fatalf("滚到中段，一帧声明了 %d 行", r.calls)
	}
	r.frames(1) // 内容总高是上一帧布局之后的值：再出一帧，让它反映滚动之后的窗口
	checkContent("中段")
	if want := 5000 * pitch; math.Abs(float64(r.view.Offset-r.pad[0]-want)) > 1 {
		t.Fatalf("偏移 %v，应在第 5000 行的上沿 %v 上下", r.view.Offset, want+r.pad[0])
	}
	if _, ok := r.seen()["row-5000"]; !ok {
		t.Fatalf("第 5000 行不在画面里：%v", r.seen())
	}
	// 到底：总高按序号算，末行与尾部都在画面里。
	r.jump(1e9)
	seen := r.seen()
	if _, ok := seen["row-9999"]; !ok {
		t.Fatalf("到底之后看不见末行：%v", seen)
	}
	if _, ok := seen["TAIL"]; !ok {
		t.Fatalf("到底之后看不见尾部：%v", seen)
	}
	r.frames(1)
	checkContent("底部")
}

// 定高模式不量行高，也就没有「量到新行之后再出一帧」：版面一稳定，窗口就不再要帧。
func TestFixedRowsNeverAskForAnotherFrame(t *testing.T) {
	r := openFixed(t, 300)
	r.restTime()
	r.jump(900)
	if got := r.restTime(); got > 100*time.Millisecond {
		t.Fatalf("定高模式滚动之后窗口还在要帧：Rest 走了 %v", got)
	}
}

func TestFixedRowsDetectMisfit(t *testing.T) {
	ok := openFixed(t, 50)
	ok.frames(2)
	if i, h, bad := ok.w.Misfit(); bad {
		t.Fatalf("行高都是 %d，却报第 %d 行高 %v 不符", fixedRowH, i, h)
	}
	// 各行高度 30、40、50、60 循环，与 Fixed=45 不符：第 0 行（高 30）是第一条。
	bad := openWindowedWith(t, 50, func(r *windowRig) { r.fixed = fixedRowH })
	bad.frames(2)
	if i, h, found := bad.w.Misfit(); !found || i != 0 || h != 30 {
		t.Fatalf("Misfit = (%d, %v, %v)，应报第 0 行高 30", i, h, found)
	}
	// 把行高改成都符合之后，下一次绘制不再报。
	bad.flat = fixedRowH
	bad.frames(2)
	if i, h, found := bad.w.Misfit(); found {
		t.Fatalf("行高修好之后仍报第 %d 行高 %v 不符", i, h)
	}
	// 变高模式没有定高的约定，不报。
	if _, _, found := openWindowed(t, 50).w.Misfit(); found {
		t.Fatal("变高模式不该报不符")
	}
}

func TestFixedRowsRevealScrollsNearest(t *testing.T) {
	const n = 1000
	r := openFixed(t, n)
	pitch := float32(fixedRowH + testGap)
	reveal := func(i int) float32 {
		t.Helper()
		r.w.Reveal(r.view, i)
		r.frames(3)
		return r.view.Offset
	}
	// 在下方：靠下沿。第 100 行的下沿是 100×pitch + 45；视口下沿（含下内边距）= 偏移 + 视口高 + 下内边距。
	bottom := 100*pitch + fixedRowH
	if got, want := reveal(100), bottom-r.view.Viewport-r.pad[2]; math.Abs(float64(got-want)) > 0.5 {
		t.Fatalf("Reveal(100) 之后偏移 %v，应为 %v（靠下沿）", got, want)
	}
	if _, ok := r.seen()["row-100"]; !ok {
		t.Fatalf("第 100 行不在画面里：%v", r.seen())
	}
	// 已经完全在视野里：不动。
	before := r.view.Offset
	if got := reveal(99); got != before {
		t.Fatalf("第 99 行已在视野里，偏移却从 %v 变成 %v", before, got)
	}
	// 在上方：靠上沿（上沿 = 偏移 − 上内边距）。
	top := 40 * pitch
	if got, want := reveal(40), top+r.pad[0]; math.Abs(float64(got-want)) > 0.5 {
		t.Fatalf("Reveal(40) 之后偏移 %v，应为 %v（靠上沿）", got, want)
	}
	// 越界的序号什么也不做。
	before = r.view.Offset
	for _, i := range []int{-1, n, n + 5} {
		if got := reveal(i); got != before {
			t.Fatalf("Reveal(%d) 越界，偏移却从 %v 变成 %v", i, before, got)
		}
	}
}

// 变高模式的 Reveal 按当前估计的位置：滚过去并量到之后，目标行落在视野里。
func TestVariableRowsRevealBringsRowIntoView(t *testing.T) {
	r := openWindowed(t, 300)
	r.w.Reveal(r.view, 12)
	r.frames(6)
	y, ok := r.seen()["row-12"]
	if !ok {
		t.Fatalf("第 12 行不在画面里：%v", r.seen())
	}
	// 在下方的行就近对齐：下沿贴着视野的下沿（行距不算在行里）。
	if bottom := y + testRowHeight(12); math.Abs(float64(bottom-testViewH)) > 1 {
		t.Fatalf("第 12 行的下沿在 %v，应贴着视野下沿 %v", bottom, testViewH)
	}
}

// TestFixedRowsFrameBudget 读出定高窗口的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）：
// 50 行、1000 行与 1 万行的读数应当同量级——每帧只声明与布局窗口里的行，开销与总行数无关。
//
// 无头会话的 Frame() 只含布局与记录（不栅格化）；耗时与机器有关，默认跳过：
// VIVA_FRAME_BUDGET=1 go test ./viva/kit -run TestFixedRowsFrameBudget -v -count=1。读数记进台账 B-029。
func TestFixedRowsFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	for _, n := range []int{50, 1000, 10000} {
		t.Run(fmt.Sprintf("%d_rows", n), func(t *testing.T) {
			heapBefore := frametime.HeapInUse()
			r := openFixed(t, n)
			t.Logf("%d 行 打开页面后 Go 堆增量（含无头会话）：%d KiB", n, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

			idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%d 行 稳态帧（布局+记录）：%s", n, idle)

			// 滚动一步：每步 40 逻辑像素。先预检：向下 20 步（800 px）画面必须变，再原路滚回。
			step := func(dy float32) error {
				if err := r.session.Scroll(gfx.ScrollEvent{X: testViewW / 2, Y: testViewH / 2, DeltaY: dy, Precise: true}); err != nil {
					return err
				}
				return r.session.Frame()
			}
			if n >= 50 {
				before := fmt.Sprint(r.seen())
				for i := 0; i < 20; i++ {
					if err := step(40); err != nil {
						t.Fatalf("预检滚动失败：%v", err)
					}
				}
				if fmt.Sprint(r.seen()) == before {
					t.Fatal("向下滚动 800 px 之后画面没有变化，滚动读数无效")
				}
				for i := 0; i < 20; i++ {
					if err := step(-40); err != nil {
						t.Fatalf("预检回滚失败：%v", err)
					}
				}
			}
			scroll, err := frametime.Measure(200, func(i int) error {
				if (i/20)%2 == 1 {
					return step(-40)
				}
				return step(40)
			})
			if err != nil {
				t.Fatalf("滚动帧失败：%v", err)
			}
			t.Logf("%d 行 滚动一步（滚动+出帧）：%s", n, scroll)

			// 拖滑块到各处：每一步跳过半个内容高度，窗口整个换掉。
			half := float32(n) * (fixedRowH + testGap) / 2
			jump, err := frametime.Measure(60, func(i int) error {
				if i%2 == 1 {
					return step(-half)
				}
				return step(half)
			})
			if err != nil {
				t.Fatalf("跳转帧失败：%v", err)
			}
			t.Logf("%d 行 窗口整个换掉的一帧（滚动+出帧）：%s", n, jump)
		})
	}
}
