package app

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// 生产接线（bindInput）不能把页面自己的按键路由换掉：下拉打开之后，方向键要能在选项之间移动焦点。
// 回归背景：47d2b000 把 OnKey 的接线收进 bindInput 时写成 config.OnKey = root.OnKey，换掉了 page.Config 装上的页面路由
// （焦点组、菜单、下拉里方向键的移动），当时没有任何测试走生产接线去按方向键；这里补上，守住 kit.KeyFirst 的合并。
func TestWindowKeyHookKeepsDropdownArrowKeysWorking(t *testing.T) {
	f := openRoot(t, nil)
	focused := func() string {
		for _, n := range f.session.Access() {
			if n.Focused {
				return n.Label
			}
		}
		return ""
	}
	opened := false
	for _, n := range f.session.Access() {
		if n.Role == gfx.RoleCombobox {
			if err := f.session.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			opened = true
			break
		}
	}
	if !opened {
		t.Fatal("设置页上没有下拉（夹具失效）")
	}
	f.frames(t, 4)
	first := focused()
	if first == "" {
		t.Fatal("下拉打开后焦点应在第一个选项上（夹具失效）")
	}
	if err := f.session.Keys("down"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if second := focused(); second == first || second == "" {
		t.Fatalf("↓ 应把焦点移到下一个选项，实际仍是「%s」→「%s」", first, second)
	}
	if err := f.session.Keys("up"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if back := focused(); back != first {
		t.Fatalf("↑ 应回到「%s」，实际「%s」", first, back)
	}
}
