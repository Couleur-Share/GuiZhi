package app

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
)

// firstHintedHit 取窗口里第一个带提示文字的命中区（最靠上、再靠左）。
func (f *rootFixture) firstHintedHit(t *testing.T) gfx.Hit {
	t.Helper()
	var best gfx.Hit
	found := false
	for _, h := range f.session.Snapshot().Hits {
		if h.Block || h.Hint == "" {
			continue
		}
		if !found || h.Bounds.Y < best.Bounds.Y || (h.Bounds.Y == best.Bounds.Y && h.Bounds.X < best.Bounds.X) {
			best, found = h, true
		}
	}
	if !found {
		t.Fatal("窗口里没有带提示文字的命中区，无法验证悬停提示")
	}
	return best
}

// 窗口按键钩子（Root.OnKey → Look.OnKey）收起悬停提示：键盘 → 窗口钩子 → 提示层传感器的整条链路（台账 B-017）。
// 指针没动，收起之后不再出。
func TestRootKeyPressDismissesHoverTooltip(t *testing.T) {
	f := openRoot(t, nil)
	host := f.firstHintedHit(t)
	cx, cy := host.Bounds.X+host.Bounds.Width/2, host.Bounds.Y+host.Bounds.Height/2
	if err := f.session.Move(cx, cy); err != nil {
		t.Fatalf("移动指针失败：%v", err)
	}
	f.frames(t, 3)
	if err := f.session.Advance(kit.TooltipDelay + 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if _, ok := f.session.AccessByRole(gfx.RoleGroup, host.Hint); !ok {
		t.Fatalf("悬停 320 ms 后没有出气泡「%s」", host.Hint)
	}
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if _, ok := f.session.AccessByRole(gfx.RoleGroup, host.Hint); ok {
		t.Fatal("按键之后气泡仍在：窗口按键钩子没有经过提示层")
	}
	if err := f.session.Advance(2 * kit.TooltipDelay); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if _, ok := f.session.AccessByRole(gfx.RoleGroup, host.Hint); ok {
		t.Fatal("指针没动，按键收起之后不应再出气泡")
	}
}

// 窗口接上了前台探测（渲染层 window blur 的近似）：没接上，失焦收起就整条失效。
func TestRootWiresForegroundProbe(t *testing.T) {
	f := openRoot(t, nil)
	if f.root.win.WindowActive == nil {
		t.Fatal("窗口没有接前台探测：失焦收起悬停提示不会生效")
	}
}

// 窗口失焦（前台探测变为假）收起悬停提示：Root → 窗口 → 提示层的整条链路（台账 B-017）。
// 探测函数换成可控的假的：真探测依赖测试时的前台窗口，不确定。
func TestRootWindowBlurDismissesHoverTooltip(t *testing.T) {
	f := openRoot(t, nil)
	active := true
	f.root.win.WindowActive = func() bool { return active }
	host := f.firstHintedHit(t)
	cx, cy := host.Bounds.X+host.Bounds.Width/2, host.Bounds.Y+host.Bounds.Height/2
	if err := f.session.Move(cx, cy); err != nil {
		t.Fatalf("移动指针失败：%v", err)
	}
	f.frames(t, 3)
	if err := f.session.Advance(kit.TooltipDelay + 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if _, ok := f.session.AccessByRole(gfx.RoleGroup, host.Hint); !ok {
		t.Fatalf("悬停 320 ms 后没有出气泡「%s」", host.Hint)
	}
	active = false
	if err := f.session.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if _, ok := f.session.AccessByRole(gfx.RoleGroup, host.Hint); ok {
		t.Fatal("窗口失焦之后气泡仍在：前台探测没有传到提示层")
	}
}
