package kit

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 「滚到第 i 行」（RowWindow.ScrollToIndex，台账 B-045）：先用渲染层的实测数字对拍纯算术，再在无头会话里
// 逐行选中、看滚动位置与实测一致，最后钉住行高还在变化时的重算、首帧没有几何时的延期与办完之后不再插手。
//
// 实测来自 scripts/parity/tools/probe-list-keyboard.mjs（记录的副本：viva/screens/library/testdata/list-keyboard-react-1280x800.json，
// 浮层里的按键 E1、E2 在同目录的 …-E.json）：
// 1280×800、每页 20 条的卡片列表，滚动区 clientHeight 638（下内边距 8），虚拟列表的行高取整之后依次是 reactRowHeights。

var reactRowHeights = []float32{45, 45, 45, 65, 84, 45, 65, 45, 45, 65, 45, 45, 45, 45, 45, 45, 45, 45, 45, 45}

// reactTops 是 reactRowHeights 各行的上沿，末项是总高 999。
func reactTops() []float32 {
	tops := make([]float32, len(reactRowHeights)+1)
	for i, h := range reactRowHeights {
		tops[i+1] = tops[i] + h
	}
	return tops
}

const (
	reactClient float32 = 638  // 滚动区 clientHeight
	reactPadEnd float32 = 8    // pb-2
	reactScroll float32 = 1007 // scrollHeight = 总高 999 + 下内边距
)

// 渲染层读到的滚动量：↓ 从没有选中起连按，选中行依次是 0..19。
var reactDownwards = []float32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 46, 91, 136, 181, 226, 271, 316, 369}

// End 之后 ↑ 连按：选中 19、18、…、3 时的滚动量。
var reactUpwards = []float32{369, 369, 369, 369, 369, 369, 369, 369, 369, 369, 369, 369, 369, 329, 284, 200, 135}

func TestScrollIndexTargetMatchesTheVirtualizerReadings(t *testing.T) {
	tops := reactTops()
	maxOffset := reactScroll - reactClient
	step := func(offset float32, i int) float32 {
		target, _, move := scrollIndexTarget(tops[i], tops[i+1], offset, reactClient, maxOffset, i == len(reactRowHeights)-1, scrollAuto)
		if !move {
			return offset
		}
		return target
	}
	offset := float32(0)
	for i, want := range reactDownwards {
		offset = step(offset, i)
		if offset != want {
			t.Fatalf("↓ 选中第 %d 行：滚动量 %v，渲染层读数 %v", i, offset, want)
		}
	}
	for k, want := range reactUpwards {
		i := 19 - k
		offset = step(offset, i)
		if offset != want {
			t.Fatalf("↑ 选中第 %d 行：滚动量 %v，渲染层读数 %v", i, offset, want)
		}
	}
}

func TestScrollIndexTargetRules(t *testing.T) {
	const size, maxOffset = 100, 500
	cases := []struct {
		name               string
		start, end, offset float32
		last               bool
		align              scrollAlign
		want               float32
		move               bool
	}{
		{"完全在视野里不动", 120, 160, 100, false, scrollAuto, 100, false},
		{"下沿恰好压在视口下沿：判靠下，目标就是现在的位置", 160, 200, 100, false, scrollAuto, 100, true},
		{"下沿只出去一点也要滚到边", 170, 201, 100, false, scrollAuto, 101, true},
		{"上沿恰好压在视口上沿：判靠上，目标就是现在的位置", 100, 140, 100, false, scrollAuto, 100, true},
		{"上沿只出去一点也要滚到边", 99, 139, 100, false, scrollAuto, 99, true},
		{"在下方远处：下沿对齐视口下沿", 300, 340, 100, false, scrollAuto, 240, true},
		{"在上方远处：上沿对齐视口上沿", 20, 60, 100, false, scrollAuto, 20, true},
		{"比视口还高：下沿优先", 150, 400, 100, false, scrollAuto, 300, true},
		{"末行靠下：用最大滚动量，不是下沿对齐", 560, 600, 100, true, scrollAuto, maxOffset, true},
		{"末行已经在最大滚动量上还是 move（目标与现在相同）", 560, 600, 500, true, scrollAuto, maxOffset, true},
		{"末行靠上：照常对齐上沿", 20, 60, 100, true, scrollAuto, 20, true},
		{"目标夹在 0", 0, 40, 100, false, scrollAuto, 0, true},
		{"目标夹在最大滚动量", 480, 700, 100, false, scrollAuto, maxOffset, true},
		{"已解析的对齐沿用：靠下", 120, 160, 100, false, scrollEnd, 60, true},
		{"已解析的对齐沿用：靠上", 120, 160, 100, false, scrollStart, 120, true},
	}
	for _, c := range cases {
		got, _, move := scrollIndexTarget(c.start, c.end, c.offset, size, maxOffset, c.last, c.align)
		if got != c.want || move != c.move {
			t.Errorf("%s：滚动量 %v（move %v），应为 %v（move %v）", c.name, got, move, c.want, c.move)
		}
	}
}

// scrollRig 是 React 读数那一屏：窗口高 638、下内边距 8、各行高度取 reactRowHeights（无行距），行高是声明的，所以量到的与声明的一致。
type scrollRig struct {
	*harness
	w        *RowWindow
	n        int
	heights  func(i int) float32
	fixed    float32
	view     ScrollView
	hover    int // 悬停刷新请求的次数（Look.HoverRefresh）
}

func openScrollRig(t *testing.T, n int, seed float32, heights func(i int) float32, fixed float32) *scrollRig {
	t.Helper()
	r := &scrollRig{w: &RowWindow{Seed: seed, Overscan: 8}, n: n, heights: heights, fixed: fixed}
	r.harness = openPage(t, still(), 400, int(reactClient), func(l *Look, c ui.C) {
		l.ScrollAreaView(c.Key("list"), [4]float32{0, 0, reactPadEnd, 0}, func(view ScrollView) {
			r.view = view
			spec := RowsSpec{
				Name: "rows", N: r.n, View: view, Fixed: r.fixed,
				Row: func(i int) ui.Box {
					return ui.Column(c.Key(fmt.Sprintf("r%d", i))).FillWidth().Height(r.heights(i)).Children(func() {
						ui.Text(c, fmt.Sprintf("row-%d", i))
					})
				},
			}
			if r.fixed == 0 {
				spec.Key = func(i int) string { return fmt.Sprintf("r%d", i) }
			}
			r.w.Rows(c, spec)
		}).FillWidth().Grow(1).MinHeight(0)
	})
	return r
}

func reactHeights(i int) float32 { return reactRowHeights[i] }

// settle 出足够的帧让「行高在变 → 请求重算」的链走完。
func (r *scrollRig) settle() { r.frames(8) }

func TestScrollToIndexFollowsTheVirtualizerReadings(t *testing.T) {
	r := openScrollRig(t, len(reactRowHeights), 64, reactHeights, 0)
	r.settle()
	if got := r.view.Viewport + r.view.Pad[2]; got != reactClient {
		t.Fatalf("夹具与读数不是同一屏：视口 %v，应为 %v", got, reactClient)
	}
	if got := r.view.Content - r.view.Viewport; got != reactScroll-reactClient {
		t.Fatalf("最大滚动量 %v，渲染层读数 %v", got, reactScroll-reactClient)
	}
	for i, want := range reactDownwards {
		r.w.ScrollToIndex(i)
		r.settle()
		if r.view.Offset != want {
			t.Fatalf("↓ 选中第 %d 行：滚动量 %v，渲染层读数 %v", i, r.view.Offset, want)
		}
	}
	for k, want := range reactUpwards {
		i := 19 - k
		r.w.ScrollToIndex(i)
		r.settle()
		if r.view.Offset != want {
			t.Fatalf("↑ 选中第 %d 行：滚动量 %v，渲染层读数 %v", i, r.view.Offset, want)
		}
	}
}

func TestScrollToIndexInFixedMode(t *testing.T) {
	// 定高 45：不用实测，位置按序号算；与变高模式在同样的行高下读数一致。
	flat := func(int) float32 { return 45 }
	r := openScrollRig(t, 20, 0, flat, 45)
	r.settle()
	// 第 k 行下沿 = 45(k+1)：k=14 时 675 → 675−638 = 37；k=15 → 82；……第 18 行 → 217；末行是最大滚动量 900−630 = 270。
	want := []float32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 37, 82, 127, 172, 217, 270}
	for i, w := range want {
		r.w.ScrollToIndex(i)
		r.settle()
		if r.view.Offset != w {
			t.Fatalf("定高：选中第 %d 行，滚动量 %v，应为 %v", i, r.view.Offset, w)
		}
	}
}

func TestScrollToIndexReconcilesWhileRowsAreBeingMeasured(t *testing.T) {
	// 100 行、渲染层的估计高度 64、真实高度 45：第一次算目标时行高是估计，量到之后要重算，最终落在真实位置上。
	const n = 100
	flat := func(int) float32 { return 45 }
	r := openScrollRig(t, n, 64, flat, 0)
	r.settle()
	r.w.ScrollToIndex(60)
	r.frames(24)
	want := float32(61*45) - reactClient // 第 60 行的下沿对齐视口下沿
	if r.view.Offset != want {
		t.Fatalf("估计高度 64 之下跳到第 60 行：最终滚动量 %v，应为 %v（按真实行高 45 重算）", r.view.Offset, want)
	}
	// 末行：最大滚动量也要按真实总高算。
	r.w.ScrollToIndex(n - 1)
	r.frames(24)
	if want := r.view.Content - r.view.Viewport; r.view.Offset != want {
		t.Fatalf("跳到末行：滚动量 %v，应为最大滚动量 %v", r.view.Offset, want)
	}
}

func TestScrollToIndexDoesNotLingerAfterItIsDone(t *testing.T) {
	r := openScrollRig(t, len(reactRowHeights), 64, reactHeights, 0)
	r.settle()
	r.w.ScrollToIndex(14)
	r.settle()
	if r.w.req != nil {
		t.Fatal("行高稳定之后滚动请求还留着：以后任何一次重绘都会把用户滚走的位置拉回去")
	}
	before := r.view.Offset
	// 用户滚轮滚开，再来几帧：不能被拉回去。
	if err := r.session.Scroll(gfx.ScrollEvent{X: 200, Y: 300, DeltaY: -100, Precise: true}); err != nil {
		t.Fatalf("滚轮失败：%v", err)
	}
	r.frames(4)
	if r.view.Offset == before {
		t.Fatalf("滚轮没有滚动（夹具失效）：滚动量仍是 %v", r.view.Offset)
	}
	moved := r.view.Offset
	r.frames(6)
	if r.view.Offset != moved {
		t.Fatalf("办完的请求又动了滚动区：%v → %v", moved, r.view.Offset)
	}
}

func TestScrollToIndexBeforeAnyGeometryIsDeferredNotDropped(t *testing.T) {
	// 请求在第一帧之前（滚动区还没有几何）就发出：留到有几何的那一帧执行，不丢。
	r := &scrollRig{w: &RowWindow{Seed: 64, Overscan: 8}, n: len(reactRowHeights), heights: reactHeights}
	r.w.ScrollToIndex(19)
	r.harness = openPage(t, still(), 400, int(reactClient), func(l *Look, c ui.C) {
		l.ScrollAreaView(c.Key("list"), [4]float32{0, 0, reactPadEnd, 0}, func(view ScrollView) {
			r.view = view
			r.w.Rows(c, RowsSpec{
				Name: "rows", N: r.n, View: view,
				Key: func(i int) string { return fmt.Sprintf("r%d", i) },
				Row: func(i int) ui.Box {
					return ui.Column(c.Key(fmt.Sprintf("r%d", i))).FillWidth().Height(r.heights(i)).Children(func() { ui.Text(c, "x") })
				},
			})
		}).FillWidth().Grow(1).MinHeight(0)
	})
	r.settle()
	if r.view.Offset != 369 {
		t.Fatalf("首帧之前请求的末行：滚动量 %v，应为 369", r.view.Offset)
	}
}

// 滚动之后请求悬停刷新（台账 B-037）：滚动区移动了才请求，已经完全在视野里的行不动、不请求。
func TestScrollToIndexRequestsAHoverRefreshOnlyWhenItMoves(t *testing.T) {
	r := openScrollRig(t, len(reactRowHeights), 64, reactHeights, 0)
	r.look.HoverRefresh = func() { r.hover++ }
	r.settle()
	r.hover = 0
	r.w.ScrollToIndex(2) // 已完全在视野里
	r.settle()
	if r.hover != 0 {
		t.Fatalf("没有移动滚动区却请求了 %d 次悬停刷新", r.hover)
	}
	r.w.ScrollToIndex(15)
	r.settle()
	if r.hover == 0 {
		t.Fatal("滚动区移动了却没有请求悬停刷新")
	}
	if r.view.Offset != 181 { // 渲染层读数：选中第 15 行时滚动量 181
		t.Fatalf("第 15 行滚进视野之后滚动量 %v，渲染层读数 181", r.view.Offset)
	}
}
