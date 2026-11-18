package kit

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 键盘能走到哪些控件（台账 B-030）：Viva 的命中区在声明时按裁剪区收窄（Canvas.HitWithOptions），
// 滚出可视区的控件根本没有命中区，不在 Tab 环里；焦点所在的控件被滚走时，路由也会清掉焦点。
// 渲染层（浏览器）里 Tab 能走到页面上所有控件，并把它滚进视野。
// 这两条测试把 Viva 目前的行为钉住：Viva 日后改了，它们会失败，提醒台账、
// 以及 settings.appearance.pick-focus 场景里「先滚再 Tab」的绕法一起更新。

const reachButtons = 8

// openReach 是一个放不下全部按钮的滚动区：200×110，每个按钮 32px 上下，只看得见前三枚左右。
func openReach(t *testing.T) *harness {
	t.Helper()
	return openPage(t, still(), 200, 110, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			l.ScrollArea(c.Key("area"), [4]float32{}, func() {
				for i := range reachButtons {
					ui.Button(c.Key(fmt.Sprintf("b%d", i)), reachName(i))
				}
			}).FillWidth().Grow(1).MinHeight(0)
		})
	})
}

func reachName(i int) string { return fmt.Sprintf("按钮 %d", i) }

// focusedName 是可访问树里当前聚焦的控件名，没有聚焦则为空。
func (h *harness) focusedName() string {
	for _, n := range h.session.Access() {
		if n.Focused {
			return n.Label
		}
	}
	return ""
}

func TestTabDoesNotReachControlsScrolledOut(t *testing.T) {
	h := openReach(t)
	visible := map[string]bool{}
	for _, n := range h.session.Access() {
		if n.Role == gfx.RoleButton {
			visible[n.Label] = true
		}
	}
	if len(visible) == 0 || len(visible) >= reachButtons {
		t.Fatalf("测试前提不成立：应当只看得见一部分按钮，实际 %d / %d", len(visible), reachButtons)
	}
	reached := map[string]bool{}
	for range 3 * reachButtons {
		h.keys("tab")
		if name := h.focusedName(); name != "" {
			reached[name] = true
		}
	}
	if reached[reachName(reachButtons-1)] {
		t.Fatalf("Tab 走到了滚出可视区的最后一枚按钮：Viva 的行为变了，台账 B-030 与场景绕法要一起更新")
	}
	for name := range reached {
		if !visible[name] {
			t.Errorf("Tab 走到了不在可视区的 %q", name)
		}
	}
	if len(reached) != len(visible) {
		t.Errorf("Tab 环应当恰好是看得见的 %d 枚按钮，实际走到 %d 枚：%v", len(visible), len(reached), reached)
	}
}

func TestScrollingTheFocusedControlAwayDropsFocus(t *testing.T) {
	h := openReach(t)
	h.keys("tab")
	if got := h.focusedName(); got != reachName(0) {
		t.Fatalf("第一次 Tab 应当聚焦 %q，实际 %q", reachName(0), got)
	}
	// 滚到底，第一枚按钮出了视口（落点在页面正中；harness.wheel 的落点是另一份测试页面的正中，不在这页里）。
	if err := h.session.Scroll(gfx.ScrollEvent{X: 100, Y: 55, DeltaY: 1000, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	h.frames(4)
	if h.has(gfx.RoleButton, reachName(0)) {
		t.Fatalf("测试前提不成立：滚到底之后第一枚按钮应当在视口之外")
	}
	if got := h.focusedName(); got != "" {
		t.Fatalf("焦点所在的控件被滚走后焦点应当被清掉（Viva 现状，台账 B-030），实际仍在 %q", got)
	}
}
