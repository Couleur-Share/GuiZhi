package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// hoverPost 是滚轮钩子请求的一次指针移动（窗口里它变成发给自己的一条 WM_MOUSEMOVE）。
type hoverPost struct{ x, y float32 }

// postHover 是测试里的 post：只排队，不立刻处理——窗口里的 PostMessage 也是在当前消息处理完之后才被取出来的。
func (h *harness) postHover(x, y float32) { h.hoverPosts = append(h.hoverPosts, hoverPost{x, y}) }

// pumpHover 把排队的指针移动按先后送进会话，等价于窗口消息循环在滚轮处理结束之后取出 WM_MOUSEMOVE 并处理。
func (h *harness) pumpHover() {
	h.t.Helper()
	posts := h.hoverPosts
	h.hoverPosts = nil
	for _, p := range posts {
		if err := h.session.Move(p.x, p.y); err != nil {
			h.t.Fatalf("处理补发的指针移动 (%g,%g) 失败：%v", p.x, p.y, err)
		}
	}
}

// 滚轮之后悬停与渲染层一致：指针底下换了元素，悬停目标就是换上来的那个，且与「真的在原位动一下鼠标」的结果相同。
// 渲染层的对应读数：scripts/parity/tools/probe-wheel-hover.mjs（滚动后 1–8 ms 内悬停行与指针下的行重新一致）。
func TestWheelHoverFollowsTheContentUnderThePointer(t *testing.T) {
	h, p := openTipScrollPage(t)
	h.moveTo(100, 100)
	before := p.hovered
	if before.IsZero() {
		t.Fatal("指针在宿主上，Viva 没有报告悬停目标")
	}
	h.scrollAt(100, 100, 70) // 宿主 2 滚走了，指针底下换成宿主 4
	got := p.hovered
	if got == before {
		t.Fatal("滚轮之后悬停仍指着滚走的宿主：窗口钩子没有让悬停跟上")
	}
	h.moveTo(100, 100) // 真的动一下鼠标
	if got != p.hovered {
		t.Fatalf("补发的指针移动与真实的指针移动结果不同：悬停 %v，真实移动之后 %v", got, p.hovered)
	}
}

// 每一格滚轮都请求一次，位置就是滚轮事件的位置（不取别处、不取整）；没有滚动区接走的滚轮不请求。
func TestWheelHookPostsOncePerWheelAtTheEventPosition(t *testing.T) {
	h, _ := openTipScrollPage(t)
	h.rawWheel = true // 只看请求，不替窗口处理
	for _, at := range []hoverPost{{100, 100}, {37.5, 412.25}, {600, 300}} {
		h.hoverPosts = nil
		if err := h.session.Scroll(gfx.ScrollEvent{X: at.x, Y: at.y, DeltaY: 30, Precise: true}); err != nil {
			t.Fatalf("滚动失败：%v", err)
		}
		if len(h.hoverPosts) != 1 || h.hoverPosts[0] != at {
			t.Fatalf("在 (%g,%g) 滚一格：请求 %v，期望恰好一次、位置相同", at.x, at.y, h.hoverPosts)
		}
	}
	// 连滚三格：三次请求，按先后。
	h.hoverPosts = nil
	for i := 0; i < 3; i++ {
		if err := h.session.Scroll(gfx.ScrollEvent{X: 100 + float32(i), Y: 100, DeltaY: 30, Precise: true}); err != nil {
			t.Fatalf("滚动失败：%v", err)
		}
	}
	if len(h.hoverPosts) != 3 || h.hoverPosts[0].x != 100 || h.hoverPosts[2].x != 102 {
		t.Fatalf("连滚三格应请求三次、按先后，实际 %v", h.hoverPosts)
	}
}

// 内容放得下、没有任何滚动区接走滚轮：没有内容换位，不请求（渲染层同样没有任何补发，见探针的「已到底」两例）。
func TestWheelHookPostsNothingWhenNoAreaTakesTheWheel(t *testing.T) {
	h := openPage(t, still(), 400, 300, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Children(func() {
			hc := c.Key("only")
			ui.Row(hc).FillWidth().Height(40).HitWith(ui.ID(hc), "唯一的行", gfx.HitOptions{Role: gfx.RoleButton})
		})
	})
	h.rawWheel = true
	if err := h.session.Scroll(gfx.ScrollEvent{X: 100, Y: 20, DeltaY: 120, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	if len(h.hoverPosts) != 0 {
		t.Fatalf("没有滚动区接走滚轮，不该请求指针移动，实际 %v", h.hoverPosts)
	}
}

// 滚到头再往同一个方向滚：滚动区不再接走滚轮，不请求；反方向滚才又有（渲染层的探针里「已到底再滚」「回到顶再向上滚」都没有任何补发）。
func TestWheelHookPostsNothingPastTheEndOfTheRange(t *testing.T) {
	h, _ := openTipScrollPage(t)
	h.rawWheel = true
	scroll := func(dy float32) int {
		t.Helper()
		h.hoverPosts = nil
		if err := h.session.Scroll(gfx.ScrollEvent{X: 100, Y: 100, DeltaY: dy, Precise: true}); err != nil {
			t.Fatalf("滚动失败：%v", err)
		}
		h.frames(2)
		return len(h.hoverPosts)
	}
	if n := scroll(5000); n != 1 {
		t.Fatalf("一路滚到底：请求 %d 次，期望 1", n)
	}
	if n := scroll(120); n != 0 {
		t.Fatalf("已经到底再向下滚：请求 %d 次，期望 0", n)
	}
	if n := scroll(-5000); n != 1 {
		t.Fatalf("反方向一路滚回顶：请求 %d 次，期望 1", n)
	}
	if n := scroll(-120); n != 0 {
		t.Fatalf("回到顶再向上滚：请求 %d 次，期望 0", n)
	}
}

// 包装本身：先交给原处理、结果原样返回，再按结果决定是否请求；请求发生在原处理之后。
func TestRefreshHoverAfterWheelWrapsTheHandler(t *testing.T) {
	type call struct {
		what string
		x, y float32
	}
	cases := []struct {
		name   string
		result gfx.EventResult
		posts  bool
	}{
		{"没有接走", gfx.EventResult{}, false},
		{"接走了（Handled）", gfx.EventResult{Handled: true}, true},
		{"要重绘（Repaint）", gfx.EventResult{Repaint: true}, true},
		{"两者都有", gfx.EventResult{Handled: true, Repaint: true, ClipboardText: "x"}, true},
	}
	for _, c := range cases {
		var log []call
		next := func(e gfx.ScrollEvent) gfx.EventResult {
			log = append(log, call{"next", e.X, e.Y})
			return c.result
		}
		wrapped := RefreshHoverAfterWheel(next, func(x, y float32) { log = append(log, call{"post", x, y}) })
		got := wrapped(gfx.ScrollEvent{X: 12.5, Y: 34.25, DeltaY: 7})
		if got != c.result {
			t.Errorf("%s：结果被改动：%+v，期望 %+v", c.name, got, c.result)
		}
		want := []call{{"next", 12.5, 34.25}}
		if c.posts {
			want = append(want, call{"post", 12.5, 34.25})
		}
		if len(log) != len(want) {
			t.Errorf("%s：调用 %v，期望 %v", c.name, log, want)
			continue
		}
		for i := range want {
			if log[i] != want[i] {
				t.Errorf("%s：第 %d 次调用是 %v，期望 %v", c.name, i+1, log[i], want[i])
			}
		}
	}
}

// 没有原处理时按「什么也没接走」算，不请求也不崩；没有 post 时原样返回原处理。
func TestRefreshHoverAfterWheelEdges(t *testing.T) {
	posted := 0
	wrapped := RefreshHoverAfterWheel(nil, func(x, y float32) { posted++ })
	if got := wrapped(gfx.ScrollEvent{DeltaY: 1}); got != (gfx.EventResult{}) || posted != 0 {
		t.Fatalf("没有原处理：结果 %+v、请求 %d 次，期望空结果、不请求", got, posted)
	}
	calls := 0
	next := func(gfx.ScrollEvent) gfx.EventResult { calls++; return gfx.EventResult{Handled: true} }
	same := RefreshHoverAfterWheel(next, nil)
	if got := same(gfx.ScrollEvent{DeltaY: 1}); !got.Handled || calls != 1 {
		t.Fatalf("没有 post：应原样走原处理，结果 %+v、调用 %d 次", got, calls)
	}
	if RefreshHoverAfterWheel(nil, nil) != nil {
		t.Fatal("两者都没有时应返回空处理（窗口配置里就是没有 OnScroll）")
	}
}
