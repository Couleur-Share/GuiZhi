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

// nearColor 判断两个颜色逐通道相差不超过 tol（半透明混色有取整误差）。
func nearColor(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}

// 半透明底色（悬停过渡中的 ghost 钮、带透明度的底色）同样要空心环：里面是底色混在页面底色上，不能混在环色上。
func TestButtonFocusRingIsHollowOnASemiTransparentBackground(t *testing.T) {
	_, box, at := buttonRingPage(t, tokens.RGBA(0, 128, 0, 0.6))
	page := at(2, 2)
	mix := func(bg, under uint8) uint8 { return uint8(0.6*float64(bg) + 0.4*float64(under) + 0.5) }
	want := color.RGBA{R: mix(0, page.R), G: mix(128, page.G), B: mix(0, page.B), A: 255}
	if got := at(int(box.X)+3, int(box.Y+box.Height/2)); !nearColor(got, want, 2) {
		t.Fatalf("半透明底色的按钮里面应是底色混在页面底色上 %v，实际 %v（环被画成了实心、环色透了上来）", want, got)
	}
}

// 三种按钮的可访问状态与按下态外观：切换钮（aria-pressed）报「按下」；展开收起钮（aria-expanded）报「展开」，收起时也标明可展开；
// 普通按钮两样都没有。On 时（切换钮与展开钮）按钮底色照 BgOn 画，Off 与普通按钮不画。
func TestButtonStatesAndOnAppearanceByKind(t *testing.T) {
	green := color.RGBA{G: 128, A: 255}
	for _, c := range []struct {
		name                                 string
		spec                                 ButtonSpec
		selected, expanded, expandable, onBg bool
	}{
		{"切换钮按下", ButtonSpec{Label: "钮", Toggle: true, On: true}, true, false, false, true},
		{"切换钮弹起", ButtonSpec{Label: "钮", Toggle: true}, false, false, false, false},
		{"展开钮展开", ButtonSpec{Label: "钮", Disclosure: true, On: true}, false, true, true, true},
		{"展开钮收起", ButtonSpec{Label: "钮", Disclosure: true}, false, false, true, false},
		{"普通按钮", ButtonSpec{Label: "钮"}, false, false, false, false},
		{"普通按钮带 On 也不画按下态", ButtonSpec{Label: "钮", On: true}, false, false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := openPage(t, still(), 200, 100, func(l *Look, cc ui.C) {
				style := ButtonStyle{Height: 28, PadX: 12, Radius: 4, Text: TextXS, Fg: l.T.Foreground, BgOn: tokens.RGBA(0, 128, 0, 1)}
				ui.Row(cc.Key("row")).PadSides(30, 30, 30, 30).Children(func() {
					l.Button(cc.Key("b"), c.spec, style)
				})
			})
			n, ok := h.node(gfx.RoleButton, "钮")
			if !ok {
				t.Fatal("找不到按钮「钮」")
			}
			if n.Selected != c.selected || n.Expanded != c.expanded || n.Expandable != c.expandable {
				t.Fatalf("可访问状态应为 selected=%v expanded=%v expandable=%v，实际 %v %v %v", c.selected, c.expanded, c.expandable, n.Selected, n.Expanded, n.Expandable)
			}
			img, err := h.session.Image()
			if err != nil {
				t.Fatal(err)
			}
			got := img.RGBAAt(int(n.Bounds.X)+3, int(n.Bounds.Y+n.Bounds.Height/2))
			if (got == green) != c.onBg {
				t.Fatalf("按钮里面的颜色 %v：画了 BgOn（绿）应为 %v", got, c.onBg)
			}
		})
	}
}
