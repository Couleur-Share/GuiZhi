package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Button 通过 ButtonSpec.IDOut 交出自己的命中盒标识，它与 PointerOn、ClickedByKey 用的是同一个：
// 点按钮在光标处开菜单的调用方靠它读到指针松开的位置与「是不是键盘点的」。
// （调用方自己先 ui.ID(c) 取不到：Button 的第一步就是取命中 ID，会消耗 c 上的键，两边取到的不是同一个。）
func TestButtonIDOutIsTheHitIDOfPointerAndKeyboardClicks(t *testing.T) {
	var id, other gfx.ID
	var up gfx.Point
	var sawUp, clicked, byKey bool
	h := openPage(t, still(), 300, 120, func(l *Look, c ui.C) {
		style := ButtonStyle{Height: 28, PadX: 8, Text: TextSM, Fg: l.T.Foreground}
		ui.Row(c.Key("row")).Gap(8).Children(func() {
			box := l.Button(c.Key("a"), ButtonSpec{Label: "甲", IDOut: &id}, style)
			l.Button(c.Key("b"), ButtonSpec{Label: "乙", IDOut: &other}, style)
			if ev, ok := c.Canvas().PointerOn(id); ok && ev.Kind == gfx.PointerUp {
				up, sawUp = gfx.Point{X: ev.X, Y: ev.Y}, true
			}
			if box.Clicked() {
				clicked, byKey = true, c.Canvas().ClickedByKey(id)
			}
		})
	})
	if id.IsZero() || other.IsZero() || id == other {
		t.Fatalf("两枚按钮应各交出一个非零且不同的标识，实际 %v、%v", id, other)
	}
	stable := id
	h.frames(2)
	if id != stable {
		t.Fatal("标识应跨帧稳定")
	}

	// 键盘：Tab 到第一枚按钮，回车点击；这是键盘点击。
	h.keys("Tab")
	h.keys("Enter")
	if !clicked || !byKey {
		t.Fatalf("回车点击应被读成键盘点击，clicked=%v byKey=%v", clicked, byKey)
	}

	// 鼠标：点击读到指针松开的位置，不是键盘点击。
	clicked, byKey, sawUp = false, false, false
	n, ok := h.node(gfx.RoleButton, "甲")
	if !ok {
		t.Fatal("找不到按钮「甲」")
	}
	x, y := n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2
	h.clickAt(x, y)
	if !clicked || byKey {
		t.Fatalf("鼠标点击不是键盘点击，clicked=%v byKey=%v", clicked, byKey)
	}
	if !sawUp || up.X != x || up.Y != y {
		t.Fatalf("应读到指针松开的位置 (%g, %g)，实际 sawUp=%v (%g, %g)", x, y, sawUp, up.X, up.Y)
	}
}
