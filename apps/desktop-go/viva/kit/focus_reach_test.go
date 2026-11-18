package kit

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Tab 可达与聚焦跟随滚动（台账 B-030，reach.go）。
//
// Viva 自己的 Tab 环只含声明了命中区的控件，滚出视野的没有；这里测的是 kit 补上的那一层：
// 下一个停靠点在视野之外时 Tab 把它滚进视野并聚焦（Chromium 的 CenterIfNeeded：完全在外居中，露出一部分就近靠边，
// 完全在内不动），Shift+Tab 对称，键盘落在半露停靠点上时跟随滚动，点击不跟随。
//
// 页面：中间一个 110px 高的滚动区，里面 8 枚 32px 高的按钮首尾相接（内容 256px，最大偏移 146）；
// 可选地在区前、区后各放一枚区外按钮（那时页面加高 64px，滚动区仍是 110px）。

const (
	reachStops  = 8
	reachRowH   = 32
	reachViewH  = 110
	reachMaxOff = reachStops*reachRowH - reachViewH
)

// reachRig 是一页测试页面，并记下每次绘制里各按钮的聚焦环是否可见。
type reachRig struct {
	*harness
	areaTop float32 // 滚动区上沿的 y（有区前按钮时为 reachRowH）
	ring    [reachStops]bool
}

func reachName(i int) string { return fmt.Sprintf("按钮 %d", i) }

func reachButton(l *Look, c ui.C, key, label string) ui.Box {
	return l.Button(c.Key(key), ButtonSpec{Label: label}, ButtonStyle{Height: reachRowH, PadX: 8})
}

// openReach 打开测试页面。outside 为真时区前、区后各有一枚区外按钮。
func openReach(t *testing.T, outside bool) *reachRig {
	t.Helper()
	r := &reachRig{}
	height := reachViewH
	if outside {
		height += 2 * reachRowH
		r.areaTop = reachRowH
	}
	r.harness = openPage(t, still(), 200, height, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			if outside {
				reachButton(l, c, "before", "区前")
			}
			l.ScrollArea(c.Key("area"), [4]float32{}, func() {
				for i := range reachStops {
					r.ring[i] = reachButton(l, c, fmt.Sprintf("b%d", i), reachName(i)).State().FocusVisible
				}
			}).FillWidth().Grow(1).MinHeight(0)
			if outside {
				reachButton(l, c, "after", "区后")
			}
		})
	})
	return r
}

// focused 是可访问树里当前聚焦的节点。
func (h *harness) focused() (app.AccessNode, bool) {
	for _, n := range h.session.Access() {
		if n.Focused {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// focusedName 是当前聚焦控件的名称，没有聚焦则为空。
func (h *harness) focusedName() string {
	n, _ := h.focused()
	return n.Label
}

// focusedTop 是当前聚焦控件上沿到滚动区上沿的距离。
func (r *reachRig) focusedTop() float32 {
	n, _ := r.focused()
	return n.Bounds.Y - r.areaTop
}

// tabTo 按 n 次 Tab（shift 为真则 Shift+Tab），逐次返回聚焦的名称与上沿。
func (r *reachRig) tabTo(n int, shift bool) (names []string, tops []float32) {
	r.t.Helper()
	key := "tab"
	if shift {
		key = "shift+tab"
	}
	for range n {
		r.keys(key)
		names = append(names, r.focusedName())
		tops = append(tops, r.focusedTop())
	}
	return names, tops
}

func sameY(a, b float32) bool { return math.Abs(float64(a-b)) <= 0.5 }

// reachDown 是从头到尾逐个 Tab 时，每个停靠点聚焦之后离滚动区上沿的距离（手算）：
//
//	0、1、2 完全看得见，不动；3 露出一半（96..128，视野 0..110）就近靠边，下沿对齐，偏移 18；
//	4 在视野之外（128..160 − 18 = 110..142），居中，偏移 144−55 = 89 → 39；
//	5 在视野里（160..192 − 89 = 71..103），不动；6 露出一半（103..135），下沿对齐，偏移 114 → 78；
//	7 在视野之外（224..256 − 114 = 110..142），居中该是偏移 185，夹在最大偏移 146，→ 224−146 = 78。
var reachDown = [reachStops]float32{0, 32, 64, 78, 39, 71, 78, 78}

func TestTabWalksEveryStopAndScrollsItIntoView(t *testing.T) {
	r := openReach(t, false)
	names, tops := r.tabTo(reachStops, false)
	for i := range reachStops {
		if names[i] != reachName(i) {
			t.Fatalf("第 %d 次 Tab 应当聚焦 %q，实际 %q（逐次：%v）", i+1, reachName(i), names[i], names)
		}
		if !sameY(tops[i], reachDown[i]) {
			t.Errorf("聚焦 %q 之后它离滚动区上沿 %.1f，期望 %.1f（CenterIfNeeded）", reachName(i), tops[i], reachDown[i])
		}
	}
	// 键盘走到的，焦点环要看得见（不然用户不知道落在哪）。
	if !r.ring[reachStops-1] {
		t.Errorf("Tab 滚动之后聚焦环不可见（键盘模态丢了）")
	}
}

func TestShiftTabWalksBackwardAndMirrors(t *testing.T) {
	r := openReach(t, false)
	r.tabTo(reachStops, false)
	// 从底部倒着走。6、5 在视野里不动；4 上半截被切（偏移 → 128），上沿对齐；3 在视野之外（−32..0），居中，
	// 偏移 112−55 = 57 → 39；2 在视野里（7）；1 上半截被切，上沿对齐；0 在视野之外，居中会是负偏移，夹在 0。
	want := []float32{46, 14, 0, 39, 7, 0, 0}
	names, tops := r.tabTo(len(want), true)
	for i, top := range want {
		stop := reachStops - 2 - i
		if names[i] != reachName(stop) {
			t.Fatalf("第 %d 次 Shift+Tab 应当聚焦 %q，实际 %q（逐次：%v）", i+1, reachName(stop), names[i], names)
		}
		if !sameY(tops[i], top) {
			t.Errorf("Shift+Tab 聚焦 %q 之后离滚动区上沿 %.1f，期望 %.1f", reachName(stop), tops[i], top)
		}
	}
}

func TestTabLeavesTheAreaAtTheEndsWithoutScrolling(t *testing.T) {
	r := openReach(t, true)
	// 第一次 Tab 落在区前按钮（它在窗口 Tab 环的最前面）；之后走完区里 8 枚，再一次 Tab 出区。
	r.keys("tab")
	if got := r.focusedName(); got != "区前" {
		t.Fatalf("第一次 Tab 应当聚焦区前按钮，实际 %q", got)
	}
	names, _ := r.tabTo(reachStops, false)
	if names[0] != reachName(0) || names[reachStops-1] != reachName(reachStops-1) {
		t.Fatalf("区里应当依次聚焦首尾两枚，实际 %v", names)
	}
	r.keys("tab")
	if got := r.focusedName(); got != "区后" {
		t.Fatalf("走完区里最后一枚再 Tab 应当出区到「区后」，实际 %q", got)
	}
	// 反方向：Shift+Tab 回到区里最后一枚，一路倒着出区到「区前」。
	r.keys("shift+tab")
	if got := r.focusedName(); got != reachName(reachStops-1) {
		t.Fatalf("从「区后」Shift+Tab 应当回到区里最后一枚，实际 %q", got)
	}
	for range reachStops {
		r.keys("shift+tab")
	}
	if got := r.focusedName(); got != "区前" {
		t.Fatalf("倒着走完区里再 Shift+Tab 应当出区到「区前」，实际 %q", got)
	}
}

func TestPointerFocusDoesNotFollowScroll(t *testing.T) {
	r := openReach(t, false)
	// 按钮 3 露出一半（96..110 在视野里）。点它：聚焦，但不滚动、也不画键盘环。
	r.clickAt(100, 100)
	if got := r.focusedName(); got != reachName(3) {
		t.Fatalf("点击应当聚焦 %q，实际 %q", reachName(3), got)
	}
	if top := r.focusedTop(); !sameY(top, 96) {
		t.Errorf("点击不应当滚动：按钮 3 离上沿 %.1f，期望 96", top)
	}
	if r.ring[3] {
		t.Errorf("点击聚焦不应当画键盘环")
	}
}

func TestTabContinuesFromAFocusScrolledAway(t *testing.T) {
	r := openReach(t, false)
	r.keys("tab") // 聚焦按钮 0
	if err := r.session.Scroll(gfx.ScrollEvent{X: 100, Y: 55, DeltaY: 1000, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	r.frames(4)
	if r.has(gfx.RoleButton, reachName(0)) {
		t.Fatalf("测试前提不成立：滚到底之后按钮 0 应当在视野之外")
	}
	if got := r.focusedName(); got != "" {
		t.Fatalf("焦点所在的控件被滚走后路由器应当把焦点清掉，实际仍在 %q", got)
	}
	// 浏览器里焦点留在原处：Tab 从按钮 0 接着走到按钮 1（现在在视野之外，居中会是负偏移，夹在 0 → 32）。
	r.keys("tab")
	if got := r.focusedName(); got != reachName(1) {
		t.Fatalf("焦点被滚走之后 Tab 应当从它接着走到 %q，实际 %q", reachName(1), got)
	}
	if top := r.focusedTop(); !sameY(top, 32) {
		t.Errorf("按钮 1 离上沿 %.1f，期望 32（滚回顶）", top)
	}
	if !r.ring[1] {
		t.Error("锚点接着走之后按钮 1 的键盘环不可见（续走的 Tab 应当把键盘模态打开）")
	}
}

func TestTabAcrossAGapLongerThanTheViewport(t *testing.T) {
	// 相邻两个停靠点相隔 300px，比 110px 的视野还高：目标居中滚动之后出发点也出了视野，路由器没有出发点可续走，
	// 走「滚动，下一帧请求焦点」的那条路。按键时原来有焦点，键盘环照常可见。
	const gap = 300
	var ring [3]bool
	h := openPage(t, still(), 200, reachViewH, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollArea(c.Key("area"), [4]float32{}, func() {
				for i := range 3 {
					if i > 0 {
						ui.Column(c.Key(fmt.Sprintf("gap%d", i))).FillWidth().Height(gap)
					}
					ring[i] = reachButton(l, c, fmt.Sprintf("b%d", i), reachName(i)).State().FocusVisible
				}
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	r := &reachRig{harness: h}
	// 内容 0..32、332..364、664..696，最大偏移 586。
	// b1 居中：中心 348 − 55 = 293 → 39；b2 居中会是 625，夹在 586 → 664−586 = 78；倒回 b1：同样居中 → 39。
	steps := []struct {
		key  string
		name string
		top  float32
	}{{"tab", reachName(0), 0}, {"tab", reachName(1), 39}, {"tab", reachName(2), 78}, {"shift+tab", reachName(1), 39}}
	for _, s := range steps {
		r.keys(s.key)
		if got := r.focusedName(); got != s.name {
			t.Fatalf("%s 之后应当聚焦 %q，实际 %q", s.key, s.name, got)
		}
		if top := r.focusedTop(); !sameY(top, s.top) {
			t.Errorf("%s 聚焦 %q 之后离上沿 %.1f，期望 %.1f", s.key, s.name, top, s.top)
		}
		idx := int(s.name[len(s.name)-1] - '0')
		if !ring[idx] {
			t.Errorf("%s 聚焦 %q 之后键盘环不可见", s.key, s.name)
		}
	}
}

func TestWindowedAreaIsLeftToTheRouter(t *testing.T) {
	// 窗口化的行列只声明视窗里的行，停靠点登记不完整：这样的区不插手，Tab 的行为同补充之前（B-029）。
	// 20 行、每行一枚 32px 的按钮，视窗 110px 看得见 0..3（3 露出一半）；第 4 行声明了（Overscan）但在视野之外，没有命中区。
	var win RowWindow
	h := openPage(t, still(), 200, reachViewH, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollAreaView(c.Key("area"), [4]float32{}, func(view ScrollView) {
				win.Rows(c, RowsSpec{
					N: 20, View: view, Key: func(i int) string { return reachName(i) },
					Row: func(i int) ui.Box { return reachButton(l, c, fmt.Sprintf("w%d", i), reachName(i)) },
				})
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
	var names []string
	for range 5 {
		h.keys("tab")
		names = append(names, h.focusedName())
	}
	want := []string{reachName(0), reachName(1), reachName(2), reachName(3), reachName(0)}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("窗口化的区里 Tab 应当只走看得见的行，第五次绕回开头：期望 %v，实际 %v", want, names)
		}
	}
	if n, _ := h.focused(); !sameY(n.Bounds.Y, 0) {
		t.Errorf("窗口化的区不应当被滚动：按钮 0 离上沿 %.1f", n.Bounds.Y)
	}
}

func TestRevealFollowsChromiumsCenterIfNeeded(t *testing.T) {
	view := gfx.Rect{X: 0, Y: 100, Width: 200, Height: 100} // 视野 y 100..200
	at := func(offset float32) *reachArea { return &reachArea{view: view, offset: offset, max: 1000} }
	rect := func(y, h float32) gfx.Rect { return gfx.Rect{X: 10, Y: y, Width: 50, Height: h} }
	cases := []struct {
		name   string
		offset float32
		r      gfx.Rect
		scale  float32
		to     float32
		moved  bool
	}{
		{"完全在视野里不动", 40, rect(120, 30), 1, 40, false},
		{"紧贴视野上下沿也算在里面", 40, rect(100, 100), 1, 40, false},
		{"完全在下方：居中", 0, rect(300, 40), 1, 170, true},    // 中心 320 − 视野中心 150
		{"完全在上方：居中", 500, rect(-100, 40), 1, 270, true}, // 中心 −80 − 150 = −230，500 − 230
		{"刚好在下沿之外（相切）算完全在外", 0, rect(200, 20), 1, 60, true},
		{"露出一半（下沿被切）：下沿对齐", 0, rect(180, 40), 1, 20, true},
		{"露出一半（上沿被切）：上沿对齐", 200, rect(80, 40), 1, 180, true},
		{"比视野还高：上沿对齐", 0, rect(150, 400), 1, 50, true},
		{"居中按设备像素取整（100%）", 0, rect(300.4, 40), 1, 170, true},   // 170.4 → 170
		{"居中按设备像素取整（200%）", 0, rect(300.4, 40), 2, 170.5, true}, // 170.4×2 = 340.8 → 341 → 170.5
		{"夹在 0 以下", 10, rect(-300, 40), 1, 0, true},
	}
	for _, c := range cases {
		to, moved := at(c.offset).reveal(c.r, c.scale)
		if to != c.to || moved != c.moved {
			t.Errorf("%s：得到 (%v, %v)，期望 (%v, %v)", c.name, to, moved, c.to, c.moved)
		}
	}
	// 夹在最大偏移之内：滚到底了、目标还要往下，就没有可滚的：不吞掉这次 Tab。
	atEnd := &reachArea{view: view, offset: 146, max: 146}
	if to, moved := atEnd.reveal(rect(300, 40), 1); moved || to != 146 {
		t.Errorf("已经滚到底时得到 (%v, %v)，期望 (146, false)", to, moved)
	}
}

func TestTabPastEndOfAScrolledAreaIsLeftToTheRouter(t *testing.T) {
	// 没有区外按钮时，最后一枚之后 Tab：路由器绕回窗口 Tab 环的开头（按钮 0），那一枚现在在视野之外，
	// 没有命中区，所以环里只有看得见的几枚：不报错、不死循环就行。
	r := openReach(t, false)
	r.tabTo(reachStops, false)
	r.keys("tab")
	if got := r.focusedName(); got == "" || got == reachName(reachStops-1) {
		t.Fatalf("走完最后一枚再 Tab 应当落在环里别的控件上，实际 %q", got)
	}
}
