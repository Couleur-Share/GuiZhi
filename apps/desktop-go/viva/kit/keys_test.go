package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// KeyFirst：钩子先看到每次按键；钩子没有处理的才交给页面自己的按键路由，两边的结果合并；钩子处理了的页面就看不到了。
func TestKeyFirstRunsTheHookBeforeThePageAndMergesTheResults(t *testing.T) {
	var order []string
	hook := func(e gfx.KeyEvent) gfx.EventResult {
		order = append(order, "hook")
		return gfx.EventResult{Repaint: true, ContinueTraversal: e.Key == gfx.KeyTab}
	}
	page := func(e gfx.KeyEvent) gfx.EventResult {
		order = append(order, "page")
		return gfx.EventResult{Handled: e.Key == gfx.KeyDown}
	}
	key := KeyFirst(hook, page)

	res := key(gfx.KeyEvent{Key: gfx.KeyDown})
	if len(order) != 2 || order[0] != "hook" || order[1] != "page" {
		t.Fatalf("钩子没有处理时应先钩子后页面，实际 %v", order)
	}
	if !res.Handled || !res.Repaint {
		t.Fatalf("两边的结果应合并（页面处理了、钩子要重绘）：%+v", res)
	}

	order = nil
	res = key(gfx.KeyEvent{Key: gfx.KeyTab})
	if !res.ContinueTraversal || res.Handled {
		t.Fatalf("钩子要求继续遍历、页面没处理：%+v", res)
	}
	if len(order) != 2 {
		t.Fatalf("页面没处理的键也要让页面看到，实际 %v", order)
	}
}

func TestKeyFirstSkipsThePageWhenTheHookHandledTheKey(t *testing.T) {
	pageSaw := false
	hook := func(gfx.KeyEvent) gfx.EventResult { return gfx.EventResult{Handled: true} }
	page := func(gfx.KeyEvent) gfx.EventResult { pageSaw = true; return gfx.EventResult{} }
	if res := KeyFirst(hook, page)(gfx.KeyEvent{Key: gfx.KeyDown}); !res.Handled || pageSaw {
		t.Fatalf("钩子处理了的键不该再交给页面：%+v，页面看到 %v", res, pageSaw)
	}
}

func TestKeyFirstWithoutAPageIsTheHook(t *testing.T) {
	hook := func(gfx.KeyEvent) gfx.EventResult { return gfx.EventResult{Repaint: true} }
	if res := KeyFirst(hook, nil)(gfx.KeyEvent{}); !res.Repaint {
		t.Fatalf("没有页面路由时结果就是钩子的：%+v", res)
	}
}

// Look.OnKey 把界面的窗口级键盘（KeyHook）的结果并进自己的结果；没有 KeyHook 时只有 Tab 可达与提示收起。
func TestLookOnKeyMergesTheKeyHook(t *testing.T) {
	l := NewLook(DefaultSpec())
	var saw []gfx.Key
	l.KeyHook = func(e gfx.KeyEvent) gfx.EventResult {
		saw = append(saw, e.Key)
		return gfx.EventResult{Handled: e.Key == gfx.KeyDown, Repaint: true}
	}
	if res := l.OnKey(gfx.KeyEvent{Key: gfx.KeyDown}); !res.Handled || !res.Repaint {
		t.Fatalf("KeyHook 的结果应并进来：%+v", res)
	}
	if res := l.OnKey(gfx.KeyEvent{Key: gfx.KeyEnter}); res.Handled {
		t.Fatalf("KeyHook 没处理的键不该被标成已处理：%+v", res)
	}
	if len(saw) != 2 {
		t.Fatalf("KeyHook 应看到每一次按键，实际 %v", saw)
	}
	l.KeyHook = nil
	if res := l.OnKey(gfx.KeyEvent{Key: gfx.KeyDown}); res.Handled {
		t.Fatalf("没有 KeyHook 时 Look 自己不处理方向键：%+v", res)
	}
}

func TestMergeKeyKeepsTheFirstFocusAndOrsTheFlags(t *testing.T) {
	a := gfx.EventResult{Repaint: true, ClipboardText: "甲"}
	b := gfx.EventResult{Handled: true, Blur: true, ContinueTraversal: true, FocusVisible: true, ClipboardText: "乙"}
	got := MergeKey(a, b)
	if !got.Handled || !got.Repaint || !got.Blur || !got.ContinueTraversal || !got.FocusVisible {
		t.Fatalf("布尔项应取或：%+v", got)
	}
	if got.ClipboardText != "甲" {
		t.Fatalf("文字应取先给出的那个，实际 %q", got.ClipboardText)
	}
	if got := MergeKey(gfx.EventResult{}, gfx.EventResult{ClipboardText: "乙"}); got.ClipboardText != "乙" {
		t.Fatalf("先给出的为空时取后者，实际 %q", got.ClipboardText)
	}

	// 焦点与激活目标同理：两边都给了取先给出的，先给出的为零值（没有目标）取后者，后者为零值不覆盖前者。
	one, two := gfx.Item(1), gfx.Item(2)
	if got := MergeKey(gfx.EventResult{Focus: one, Activate: one}, gfx.EventResult{Focus: two, Activate: two}); got.Focus != one || got.Activate != one {
		t.Fatalf("焦点与激活目标应取先给出的那个：%+v", got)
	}
	if got := MergeKey(gfx.EventResult{}, gfx.EventResult{Focus: two, Activate: two}); got.Focus != two || got.Activate != two {
		t.Fatalf("先给出的没有目标时应取后者：%+v", got)
	}
	if got := MergeKey(gfx.EventResult{Focus: one, Activate: one}, gfx.EventResult{}); got.Focus != one || got.Activate != one {
		t.Fatalf("后者没有目标时不该覆盖前者：%+v", got)
	}
}
