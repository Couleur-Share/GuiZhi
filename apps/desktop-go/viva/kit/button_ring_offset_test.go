package kit

import (
	"image/color"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// offsetRingPage 打开一页：一枚底色透明的按钮，按给定样式 Tab 键盘聚焦，返回截图取点函数、按钮外框与页面底色。
func offsetRingPage(t *testing.T, mutate func(st *ButtonStyle)) (func(x, y int) color.RGBA, gfx.Rect, color.RGBA) {
	t.Helper()
	h := openPage(t, still(), 200, 100, func(l *Look, c ui.C) {
		style := ButtonStyle{Height: 28, PadX: 12, Radius: 4, Text: TextXS, Fg: l.T.Foreground, Ring: tokens.RGBA(255, 0, 0, 1), FocusRing: true}
		mutate(&style)
		ui.Row(c.Key("row")).PadSides(30, 30, 30, 30).Children(func() {
			l.Button(c.Key("b"), ButtonSpec{Label: "目录"}, style)
		})
	})
	h.keys("Tab")
	h.frames(2)
	n, ok := h.node(gfx.RoleButton, "目录")
	if !ok {
		t.Fatal("找不到按钮「目录」")
	}
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	return func(x, y int) color.RGBA { return img.RGBAAt(x, y) }, n.Bounds, img.RGBAAt(2, 2)
}

var (
	ringRed   = color.RGBA{R: 255, A: 255}
	gapGreen  = color.RGBA{G: 200, A: 255}
	ringWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// 台账 B-039：ring-offset-2 的聚焦环——按钮外 2px 是留白色、再外 2px 是环色，再外是页面底色；
// 底色透明的按钮里面仍是页面底色（留白与环都是空心的圈，不透到按钮里面）。四个方向都要有。
func TestButtonFocusRingOffsetLeavesAGapBeforeTheRing(t *testing.T) {
	at, box, page := offsetRingPage(t, func(st *ButtonStyle) {
		st.RingOffset, st.RingOffsetColor = 2, tokens.RGBA(0, 200, 0, 1)
	})
	left, top := int(box.X), int(box.Y)
	cx, cy := int(box.X+box.Width/2), int(box.Y+box.Height/2)
	right, bottom := int(box.X+box.Width), int(box.Y+box.Height)
	type probe struct {
		name string
		x, y int
	}
	for _, c := range []struct {
		want   color.RGBA
		label  string
		probes []probe
	}{
		{gapGreen, "留白（紧贴按钮外侧 1–2 px）", []probe{
			{"左 1", left - 1, cy}, {"左 2", left - 2, cy}, {"右 1", right, cy}, {"右 2", right + 1, cy},
			{"上 1", cx, top - 1}, {"上 2", cx, top - 2}, {"下 1", cx, bottom}, {"下 2", cx, bottom + 1},
		}},
		{ringRed, "环（留白外侧 2 px，即按钮外 3–4 px）", []probe{
			{"左 3", left - 3, cy}, {"左 4", left - 4, cy}, {"右 3", right + 2, cy}, {"右 4", right + 3, cy},
			{"上 3", cx, top - 3}, {"上 4", cx, top - 4}, {"下 3", cx, bottom + 2}, {"下 4", cx, bottom + 3},
		}},
		{page, "环之外是页面底色（按钮外 5 px）", []probe{
			{"左 5", left - 5, cy}, {"右 5", right + 4, cy}, {"上 5", cx, top - 5}, {"下 5", cx, bottom + 4},
		}},
		{page, "透明底色的按钮里面仍是页面底色", []probe{{"内", left + 3, cy}}},
	} {
		for _, p := range c.probes {
			if got := at(p.x, p.y); got != c.want {
				t.Errorf("%s：%s (%d,%d) = %v，期望 %v", c.label, p.name, p.x, p.y, got, c.want)
			}
		}
	}
}

// 留白色零值取页面底色（ring-offset-background）；RingOffset 为 0 时环紧贴按钮、没有留白（与此前一致）。
func TestButtonFocusRingOffsetDefaultsToPageBackgroundAndZeroMeansNoGap(t *testing.T) {
	t.Run("留白色零值取页面底色", func(t *testing.T) {
		at, box, page := offsetRingPage(t, func(st *ButtonStyle) { st.RingOffset = 2 })
		cy, left := int(box.Y+box.Height/2), int(box.X)
		if got := at(left-1, cy); got != page {
			t.Errorf("留白应是页面底色 %v，实际 %v", page, got)
		}
		if got := at(left-3, cy); got != ringRed {
			t.Errorf("留白之外应是环色，实际 %v", got)
		}
	})
	t.Run("RingOffset 为 0：环紧贴按钮", func(t *testing.T) {
		at, box, _ := offsetRingPage(t, func(st *ButtonStyle) {})
		cy, left := int(box.Y+box.Height/2), int(box.X)
		for _, dx := range []int{1, 2} {
			if got := at(left-dx, cy); got != ringRed {
				t.Errorf("按钮左外侧 %d px 应是环色，实际 %v", dx, got)
			}
		}
	})
}

// UIGhostButton 带 ring-offset-2 的留白，留白色是 Tailwind 缺省的 #fff（渲染层 Button.tsx 没有写 ring-offset-background）；
// UISecondaryButton 的盒阴影被 app-wallpaper-surface 的 !important 盖掉，没有环也没有留白。
func TestUIGhostButtonRingOffsetFollowsTheRenderer(t *testing.T) {
	l := NewLook(still())
	for _, size := range []UIButtonSize{UIButtonSM, UIButtonMD, UIButtonLG} {
		g := l.UIGhostButton(size)
		if g.RingOffset != 2 || g.RingOffsetColor != tokens.White {
			t.Errorf("ghost 档 %d：RingOffset=%v RingOffsetColor=%v，期望 2 与白色", size, g.RingOffset, g.RingOffsetColor)
		}
		if s := l.UISecondaryButton(size); s.RingOffset != 0 || s.FocusRing {
			t.Errorf("secondary 档 %d：RingOffset=%v FocusRing=%v，期望没有环也没有留白", size, s.RingOffset, s.FocusRing)
		}
	}
}

// 真实的 ghost 样式聚焦：按钮外 2 px 是白色留白，再外 2 px 是 border 令牌色的环。
func TestUIGhostButtonFocusedShowsWhiteGapThenBorderRing(t *testing.T) {
	var border tokens.Color
	h := openPage(t, still(), 200, 100, func(l *Look, c ui.C) {
		border = l.T.Border
		ui.Row(c.Key("row")).PadSides(30, 30, 30, 30).Children(func() {
			l.Button(c.Key("b"), ButtonSpec{Label: "目录"}, l.UIGhostButton(UIButtonSM))
		})
	})
	h.keys("Tab")
	h.frames(2)
	n, ok := h.node(gfx.RoleButton, "目录")
	if !ok {
		t.Fatal("找不到按钮「目录」")
	}
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	cy, left := int(n.Bounds.Y+n.Bounds.Height/2), int(n.Bounds.X)
	for _, dx := range []int{1, 2} {
		got := img.RGBAAt(left-dx, cy)
		expectPixel(t, "按钮左外侧留白", [4]uint8{got.R, got.G, got.B, got.A}, [3]float64{255, 255, 255}, 0.5)
	}
	for _, dx := range []int{3, 4} {
		got := img.RGBAAt(left-dx, cy)
		expectPixel(t, "按钮左外侧的环", [4]uint8{got.R, got.G, got.B, got.A}, rgbOf(border, tokens.White), 1.5)
	}
}
