package library

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// 台账 B-048：吸附表头只在左缘越过侧栏右缘（视口左缘）时才算盖住侧栏阴影，纵向区间取表头与视口的交集。
func TestHeadShadowNoteHead(t *testing.T) {
	// 滚动区：内容原点 (80, 40)，已向右滚 100、向下滚 30，视口 800×600 ⇒ 视口左缘 180，上沿 70，下沿 670。
	view := func() headShadow {
		return headShadow{offsetX: 100, offsetY: 30, viewW: 800, viewH: 600}
	}
	const originX, originY = 80, 40
	rect := func(x, y, w, h float32) gfx.Rect { return gfx.Rect{X: x, Y: y, Width: w, Height: h} }
	cases := []struct {
		name        string
		rect        gfx.Rect
		ok          bool
		top, bottom float32
	}{
		{"左缘越过视口左缘：整个表头行", rect(100, 70, 1200, 40), true, 70, 110},
		{"左缘恰在视口左缘上", rect(180, 70, 1200, 40), true, 70, 110},
		{"左缘在视口左缘内侧一点：不算盖住", rect(181, 70, 1200, 40), false, 0, 0},
		{"左缘还在视口里（未横向滚过）：不算盖住", rect(300, 70, 1200, 40), false, 0, 0},
		{"整个表头已滚到视口左边", rect(-1100, 70, 1200, 40), false, 0, 0},
		{"表头上沿被视口上沿裁掉一截（吸附在表格底部）", rect(100, 50, 1200, 40), true, 70, 90},
		{"表头下沿被视口下沿裁掉一截", rect(100, 650, 1200, 40), true, 650, 670},
		{"表头整个在视口上方：没有交集", rect(100, 0, 1200, 40), false, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := view()
			h.noteHead(c.rect, originX, originY)
			if h.ok != c.ok || (c.ok && (h.top != c.top || h.bottom != c.bottom)) {
				t.Fatalf("noteHead(%v) = ok %v [%v, %v]，期望 ok %v [%v, %v]", c.rect, h.ok, h.top, h.bottom, c.ok, c.top, c.bottom)
			}
		})
	}
}

// 没有视口几何（表格还没布局过）与每帧开头的清零：一律报告「没有」。
func TestHeadShadowResetAndNoGeometry(t *testing.T) {
	var h headShadow
	h.noteHead(gfx.Rect{X: 0, Y: 0, Width: 100, Height: 40}, 0, 0)
	if h.ok {
		t.Fatal("没有视口几何时不应报告遮挡")
	}
	h = headShadow{offsetX: 50, offsetY: 0, viewW: 400, viewH: 300}
	h.noteHead(gfx.Rect{X: -10, Y: 0, Width: 500, Height: 40}, 0, 0)
	if !h.ok {
		t.Fatal("左缘越过视口左缘应报告遮挡")
	}
	h.resetFrame()
	s := &Screen{}
	s.table.head = h
	if top, bottom, ok := s.OccludePanelShadow(); ok || top != 0 || bottom != 0 {
		t.Fatalf("清零后应没有遮挡：[%v, %v] ok=%v", top, bottom, ok)
	}
}
