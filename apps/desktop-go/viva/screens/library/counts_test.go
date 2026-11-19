package library

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件钉住计数的几处边界：写法（99+）、为 0 时不画、行尾计数与「更多」按钮的淡入淡出。
// 变异测试（2026-11-19）发现这几处原先只有「正常数字」的用例，边界一改测试照样绿。

// 计数超过 99 写成 99+：99 是上限内、100 起折叠（渲染层 count > 99 ? "99+" : count）。
func TestCountLabelFoldsAboveNinetyNine(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{{1, "1"}, {9, "9"}, {10, "10"}, {98, "98"}, {99, "99"}, {100, "99+"}, {101, "99+"}, {12345, "99+"}}
	for _, c := range cases {
		if got := countLabel(c.n); got != c.want {
			t.Errorf("countLabel(%d) = %q，期望 %q", c.n, got, c.want)
		}
	}
}

// 计数为 0 的行与页签都不画数字（没有「0」）：这里让一个合集、一个标签、一个范围页签都是 0。
func TestZeroCountsAreNeverDrawn(t *testing.T) {
	r := newRig(t, func(r *rig) {
		counts, cols, tags := libraryFixture()
		counts.Favorites, counts.Archived = 0, 0
		counts.ByCollection["c1"] = 0
		counts.ByTag["t2"] = 0
		r.svc.Seed(counts, cols, tags)
	})
	r.click(gfx.RoleButton, locales.LibraryShowUnusedTags(2)) // 把没有条目的标签也列出来
	for _, name := range []string{"工作", "rust", "闲置", "收藏", "归档"} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("计数为 0 的「%s」应照常列出，且名称里不带数字", name)
		}
	}
	for _, text := range r.session.Texts() {
		if text == "0" {
			t.Fatal("计数为 0 时不应画出「0」")
		}
	}
}

// 行尾的计数在悬停或焦点落进整行时淡出、让位给「更多」按钮（group-hover/row 与 group-focus-within/row）。
// 键盘聚焦「更多」时指针不在行上：只有按「悬停或焦点在行内」算，计数才会淡出。
func TestRowCountFadesOutWhenTheRowIsHoveredOrFocused(t *testing.T) {
	r := newRig(t)
	visible := func() bool {
		a, ok := r.inkAlpha("5") // 「工作」行的计数
		if !ok {
			t.Fatal("画面上没有「工作」行的计数 5")
		}
		return a >= kit.VisibleInkAlpha
	}
	if !visible() {
		t.Fatal("没有悬停也没有焦点时，行尾应显示计数")
	}
	r.hover(gfx.RoleButton, "工作")
	if visible() {
		t.Error("悬停在行上时计数应淡出")
	}
	// 指针移到别处（「阅读」行）：「工作」行恢复显示计数
	r.hover(gfx.RoleButton, "阅读")
	if !visible() {
		t.Error("指针离开后计数应恢复")
	}
	// 键盘走到「工作」行的「更多」按钮：指针仍在「阅读」行，计数因焦点在行内而淡出
	for i := 0; i < 20 && !r.focusedIs(gfx.RoleButton, locales.LibraryMoreActions()); i++ {
		r.keys("Tab")
	}
	if !r.focusedIs(gfx.RoleButton, locales.LibraryMoreActions()) {
		t.Fatal("按 Tab 没有走到「更多」按钮")
	}
	if visible() {
		t.Error("焦点在行内（键盘聚焦「更多」）时计数应淡出，即使指针不在这一行上")
	}
}

// 没有「更多」按钮的行（平台）计数一直显示，不随悬停淡出：行尾槽里没有按钮要让位。
func TestCountOfARowWithoutMoreButtonNeverFades(t *testing.T) {
	r := newRig(t)
	r.hover(gfx.RoleButton, "哔哩哔哩")
	a, ok := r.inkAlpha("4") // 哔哩哔哩的计数（回收站的 4 在另一处，取第一处即平台行之前的行没有 4）
	if !ok || a < kit.VisibleInkAlpha {
		t.Errorf("平台行悬停时计数应保持可见：found=%v alpha=%v", ok, a)
	}
}

// 页签：计数为 0 的页签不带数字徽标，名称里也没有数字；有计数时两处都有。
func TestScopeTabsShowCountsOnlyWhenPositive(t *testing.T) {
	r := newRig(t, func(r *rig) {
		counts, cols, tags := libraryFixture()
		counts.Favorites = 0
		r.svc.Seed(counts, cols, tags)
	})
	if !r.has(gfx.RoleButton, "收藏") || r.has(gfx.RoleButton, "收藏 0") {
		t.Error("计数为 0 的页签名称不带数字")
	}
	if !r.has(gfx.RoleButton, "归档 1") {
		t.Error("计数为 1 的页签名称带数字")
	}
	if _, ok := r.inkAlpha("0"); ok {
		t.Error("计数为 0 的页签不画徽标")
	}
	if !r.hasText("1") {
		t.Error("计数为 1 的页签应画徽标")
	}
	// 计数从无到有：后端有了收藏之后，换范围触发重新取计数，页签名称带上数字
	counts, cols, tags := libraryFixture()
	r.svc.Seed(counts, cols, tags)
	r.click(gfx.RoleButton, "收藏")
	if !r.has(gfx.RoleButton, "收藏 2") {
		t.Error("计数变成 2 之后页签名称应带上数字")
	}
}

// 正筛着的平台即使没有条目也要列出（免得用户所在的位置被藏掉），取消选择后才消失；
// 没筛着的平台没有条目就不列。
func TestActivePlatformStaysListedWithoutItems(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, "哔哩哔哩")
	// 后端里哔哩哔哩的条目没了：换一个范围触发重新取计数
	counts, cols, tags := libraryFixture()
	counts.ByPlatform = map[string]int{"youtube": 2}
	r.svc.Seed(counts, cols, tags)
	r.click(gfx.RoleButton, "收藏 2")
	if !r.has(gfx.RoleButton, "哔哩哔哩") {
		t.Error("正筛着的平台没有条目也应继续列出")
	}
	if !r.mustNode(gfx.RoleButton, "哔哩哔哩").Selected {
		t.Error("列出的平台应保持选中")
	}
	r.click(gfx.RoleButton, "哔哩哔哩") // 取消选择
	if r.has(gfx.RoleButton, "哔哩哔哩") {
		t.Error("取消选择之后没有条目的平台应消失")
	}
	if !r.has(gfx.RoleButton, "YouTube") {
		t.Error("有条目的平台一直列出")
	}
}
