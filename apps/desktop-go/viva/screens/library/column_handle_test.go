package library

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
)

// 表头的拖动把手（渲染层 ItemTableHeaderCell）：按下拖动，宽度 = 起点宽度 + 指针位移（夹在列的上下限里），
// 松开时才保存，按下之后没动过一个字也不写；鼠标按下不把焦点交给它（mousedown 里 preventDefault），
// 要用 Tab 走到把手上，←/→ 才每次 16 px 并立即保存；复选框列与操作列没有把手。
func TestColumnHandlesResizeByDragAndKeyboard(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	r.toTable()
	width := func(id columnID) float32 {
		for _, c := range r.screen.table.cols {
			if c.ID == id {
				return c.Width
			}
		}
		t.Fatalf("没有列 %q", id)
		return 0
	}
	saved := func() string {
		raw, _ := r.prefs.Get(settings.PrefLibraryColumns)
		return string(raw)
	}
	handle := locales.LibraryResizeColumn(locales.LibraryColumnTitle())
	pos := func() (float32, float32) { return centerOf(r.mustNode(gfx.RoleSeparator, handle)) }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// press 在把手上按下并移动 dx 个像素，返回当前的指针位置；release 在那里松开。
	press := func(dx float32) (float32, float32) {
		t.Helper()
		x, y := pos()
		must(r.session.Move(x, y))
		r.frames(2)
		must(r.session.Press(x, y))
		r.frames(2)
		must(r.session.Move(x+dx, y))
		r.frames(2)
		return x + dx, y
	}
	release := func(x, y float32) {
		t.Helper()
		must(r.session.Release(x, y))
		r.frames(3)
	}

	if got := width(colTitle); got != 260 {
		t.Fatalf("夹具：标题列默认宽应是 260，实际 %v", got)
	}
	// 点一下把手（按下、松开，指针没动）：宽度不变，也不把默认列宽写成偏好。
	x, y := pos()
	r.clickAt(x, y)
	if got := width(colTitle); got != 260 || saved() != "" {
		t.Fatalf("点一下把手不该改宽度或写偏好；宽度 %v，设置 %q", got, saved())
	}

	x, y = press(40)
	if got := width(colTitle); got != 300 {
		t.Fatalf("拖动 40 px：标题列应是 300，实际 %v", got)
	}
	if got := saved(); got != "" {
		t.Fatalf("拖动中不该保存，实际 %s", got)
	}
	release(x, y)
	if got := width(colTitle); got != 300 || !strings.Contains(saved(), `{"id":"title","visible":true,"width":300}`) {
		t.Fatalf("松开之后宽度应保持 300 并保存；实际 %v，设置 %s", width(colTitle), saved())
	}

	x, y = press(1000)
	release(x, y)
	if got := width(colTitle); got != 480 || !strings.Contains(saved(), `"id":"title","visible":true,"width":480`) {
		t.Fatalf("拖过上限应夹在 480 并保存；实际 %v，设置 %s", got, saved())
	}
	x, y = press(-2000)
	release(x, y)
	if got := width(colTitle); got != 150 || !strings.Contains(saved(), `"id":"title","visible":true,"width":150`) {
		t.Fatalf("拖过下限应夹在 150 并保存；实际 %v，设置 %s", got, saved())
	}

	// 键盘：焦点交给别处，再用 Tab 走到把手上（把手在 Tab 顺序里，渲染层 tabIndex=0）；→ 加 16 并立刻保存，← 减 16，到下限不再缩。
	// （渲染层在 mousedown 里 preventDefault、鼠标按下不把焦点交给把手，Viva 的按下会取焦点，台账候选「把手按下取焦点」。）
	r.click(gfx.RoleButton, locales.LibraryViewList())
	if r.focusedIs(gfx.RoleSeparator, handle) {
		t.Fatal("夹具：焦点应已离开把手")
	}
	r.keys("Right")
	if got := width(colTitle); got != 150 {
		t.Fatalf("焦点不在把手上，→ 不该改宽度，实际 %v", got)
	}
	for i := 0; i < 120 && !r.focusedIs(gfx.RoleSeparator, handle); i++ {
		r.keys("Tab")
	}
	if !r.focusedIs(gfx.RoleSeparator, handle) {
		t.Fatal("按 Tab 走不到标题列的把手")
	}
	r.keys("Right")
	if got := width(colTitle); got != 166 || !strings.Contains(saved(), `"id":"title","visible":true,"width":166`) {
		t.Fatalf("→ 一次应加 16 到 166 并立即保存；实际 %v，设置 %s", got, saved())
	}
	r.keys("Left")
	if got := width(colTitle); got != 150 || !strings.Contains(saved(), `"id":"title","visible":true,"width":150`) {
		t.Fatalf("← 一次应减 16 回到 150 并立即保存；实际 %v，设置 %s", got, saved())
	}
	r.keys("Left")
	if got := width(colTitle); got != 150 {
		t.Fatalf("到下限 150 之后 ← 不再缩，实际 %v", got)
	}

	for _, name := range []string{locales.CommonSelect(), locales.LibraryColumnActions()} {
		if r.has(gfx.RoleSeparator, locales.LibraryResizeColumn(name)) {
			t.Errorf("「%s」列不可调，不该有拖动把手", name)
		}
	}
}

// 卡片视图里列表栏与详情栏之间的把手：宽度写进设置、列表栏随之变宽；点一下不写偏好；
// 键盘 →、Shift+←、回车各自提交。（把手本身的键盘与指针行为见 kit/columnresizer_test.go，这里测接线。）
func TestListWidthResizerAppliesAndSavesTheWidth(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	name := locales.LibraryResizeList()
	edge := func() float32 { return r.mustNode(gfx.RoleSeparator, name).Bounds.X } // 分隔条贴着列表栏的右缘
	saved := func() string {
		raw, _ := r.prefs.Get(settings.PrefItemListPaneWidth)
		return string(raw)
	}
	x0 := edge()
	if w, _ := r.screen.listWidths(); w != 320 {
		t.Fatalf("夹具：列表栏默认宽应是 320，实际 %v", w)
	}
	x, y := centerOf(r.mustNode(gfx.RoleSeparator, name))
	r.clickAt(x, y)
	if saved() != "" || edge() != x0 {
		t.Fatalf("点一下分隔条不该改宽度、也不该写偏好；设置 %q，右缘 %v → %v", saved(), x0, edge())
	}
	for _, c := range []struct {
		keys  string
		width float32
	}{{"Right", 336}, {"shift+Left", 272}, {"Enter", 320}} {
		r.keys(c.keys)
		if w, _ := r.screen.listWidths(); w != c.width || saved() != strconv.Itoa(int(c.width)) {
			t.Fatalf("按 %s：列表栏应是 %v 并写进设置，实际 %v，设置 %q", c.keys, c.width, w, saved())
		}
		if got, want := edge(), x0+(c.width-320); got != want {
			t.Fatalf("按 %s：分隔条应移到 %v，实际 %v", c.keys, want, got)
		}
	}
}

// 工作区宽度是 float32 量出来的：多一个 ulp（912.00006）不能漏进列表栏的上限——渲染层读的是 LayoutUnit 精度的 ResizeObserver 宽度，
// 漏进去的话可访问树里分隔条的 valuemax 读成 512.0000610351562，与渲染层的 512 对不上（M2-4 英文变体 detail-review 实测，见台账 G-017 的 M2-4 更新）。
// 真实的 1/64 px 差要保留；窄窗口时上限随宽度走，低于下限时夹到下限。
func TestListWidthLimitIgnoresTheFloatNoiseOfTheMeasuredWorkspace(t *testing.T) {
	t.Parallel()
	r := wsRig(t)
	p := &r.screen.pane
	for _, c := range []struct{ ws, hi float32 }{
		{912, 512}, {math.Nextafter32(912, 2000), 512}, {math.Nextafter32(912, 0), 512},
		{912 + 1.0/64, 512 + 1.0/64},
		{700, 300}, {600, 240},
		{2000, 720}, // 宽窗口：夹在上限 720
	} {
		p.wsWidth = c.ws
		if _, hi := r.screen.listWidths(); hi != c.hi {
			t.Errorf("工作区宽 %v：列表栏上限应是 %v，实际 %v", c.ws, c.hi, hi)
		}
	}
}
