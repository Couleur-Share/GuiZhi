package kit

import (
	"image/color"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// buttonRingPage 打开一页：一枚按钮，Tab 键盘聚焦它（focus-visible），返回截图与按钮的外框。
// bg 是按钮底色（零值即透明的 ghost 钮）；环固定画不透明红色，好和底色、页面底色分清。
func buttonRingPage(t *testing.T, bg tokens.Color) (*harness, gfx.Rect, func(x, y int) color.RGBA) {
	t.Helper()
	ring := tokens.RGBA(255, 0, 0, 1)
	h := openPage(t, still(), 200, 100, func(l *Look, c ui.C) {
		style := ButtonStyle{Height: 28, PadX: 12, Radius: 4, Text: TextXS, Fg: l.T.Foreground, Bg: bg, Ring: ring}
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
	at := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
	return h, n.Bounds, at
}

// 键盘聚焦环是 box-shadow 的环：只出现在边框盒外侧。底色透明的 ghost 钮（读者工具条的「目录」）里面不能透出环色
// （环若画成实心圆盘、靠随后的底色盖住，透明底色就盖不住：整枚按钮被涂成环的颜色）。
func TestButtonFocusRingIsHollowOnATransparentBackground(t *testing.T) {
	_, box, at := buttonRingPage(t, tokens.Color{})
	page := at(2, 2)
	cy := int(box.Y + box.Height/2)
	if got := at(int(box.X)-1, cy); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("按钮左外侧 1 px 应是环色（红），实际 %v", got)
	}
	// 按钮盒内、文字左边的内边距里：应是页面底色，不是环色。
	if got := at(int(box.X)+3, cy); got != page {
		t.Fatalf("透明底色的按钮里面应是页面底色 %v，实际 %v（环被画成了实心）", page, got)
	}
}

// 不透明底色的按钮保持原样：实心环在底色之下，被盒子盖住；盒内是底色，盒外 1 px 是环色。
func TestButtonFocusRingStaysUnderAnOpaqueBackground(t *testing.T) {
	_, box, at := buttonRingPage(t, tokens.RGBA(0, 128, 0, 1))
	cy := int(box.Y + box.Height/2)
	if got := at(int(box.X)-1, cy); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("按钮左外侧 1 px 应是环色（红），实际 %v", got)
	}
	if got := at(int(box.X)+3, cy); got != (color.RGBA{G: 128, A: 255}) {
		t.Fatalf("不透明底色的按钮里面应是底色（绿），实际 %v", got)
	}
}

// Disclosure 是展开收起钮（aria-expanded）：可访问状态是「展开」不是「按下」，按下态的外观照 BgOn 画。
func TestButtonDisclosureExposesExpandedNotPressed(t *testing.T) {
	open := false
	on := tokens.RGBA(0, 128, 0, 1)
	h := openPage(t, still(), 200, 100, func(l *Look, c ui.C) {
		style := ButtonStyle{Height: 28, PadX: 12, Radius: 4, Text: TextXS, Fg: l.T.Foreground, BgOn: on}
		ui.Row(c.Key("row")).PadSides(30, 30, 30, 30).Children(func() {
			if l.Button(c.Key("b"), ButtonSpec{Label: "目录", Disclosure: true, On: open}, style).Clicked() {
				open = !open
			}
		})
	})
	state := func() (expanded, selected bool, box gfx.Rect) {
		n, ok := h.node(gfx.RoleButton, "目录")
		if !ok {
			t.Fatal("找不到按钮「目录」")
		}
		return n.Expanded, n.Selected, n.Bounds
	}
	if expanded, selected, _ := state(); expanded || selected {
		t.Fatalf("收起时不该有展开或按下状态，expanded=%v selected=%v", expanded, selected)
	}
	h.click("目录")
	h.frames(2)
	expanded, selected, box := state()
	if !open || !expanded || selected {
		t.Fatalf("点一下应展开：open=%v expanded=%v selected=%v（可访问状态要取 expanded，不取 pressed）", open, expanded, selected)
	}
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	// 按钮盒内、文字左边的内边距里：展开时是 BgOn（绿）。
	if got := img.RGBAAt(int(box.X)+3, int(box.Y+box.Height/2)); got != (color.RGBA{G: 128, A: 255}) {
		t.Fatalf("展开时按钮底色应是 BgOn（绿），实际 %v", got)
	}
	h.click("目录")
	h.frames(2)
	if expanded, selected, _ := state(); open || expanded || selected {
		t.Fatalf("再点一下应收起：open=%v expanded=%v selected=%v", open, expanded, selected)
	}
}
