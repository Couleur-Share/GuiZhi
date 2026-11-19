package library

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件补行菜单的几处交互：右键只开菜单不选中行、菜单开着时再右键别的行就地换菜单、右键开的菜单不占着旧的触发按钮、
// 删掉正筛着的合集。变异测试（2026-11-19）发现这几处原先都没有断言。
//
// 其中三个变异体最终判为等价（脚本里写明了理由），这里的测试保护的是可见行为，不依赖它们被杀掉：
//   - N05（行不调 canvas.Consume）：Viva v0.6.4 里只有主键的释放才会产生点击（internal/interaction/pointer.go 的 Click），
//     次键释放本来就不会选中行；松开之后指针按下状态变了，运行时也一定会重绘，Consume 不改变任何可见行为，
//     调它是遵守 Viva「控件接手了这个指针事件」的约定。
//   - N02（右键重开时保留旧的触发按钮）：菜单开着时的右键按下会先被背景层当作一次外部点击（PassSecondary）关掉菜单并清空触发按钮，
//     松开才由行重开，所以重开时触发按钮早已是零值。

// 右键只是开菜单：不能同时把行选中（渲染层的 contextmenu 事件与 click 是两回事）。
func TestRightClickOnlyOpensTheMenuAndDoesNotSelectTheRow(t *testing.T) {
	r := newRig(t)
	r.rightClick(gfx.RoleButton, "go")
	if !r.has(gfx.RoleMenu, "") {
		t.Fatal("右键标签行应打开菜单")
	}
	if got := r.view().Filters.TagID; got != "" {
		t.Errorf("右键不应选中标签，筛选里的标签 = %q", got)
	}
	r.keys("Escape")
	r.rightClick(gfx.RoleButton, "工作")
	if got := r.view().Filters.CollectionID; got != "" {
		t.Errorf("右键不应选中合集，筛选里的合集 = %q", got)
	}
	if r.mustNode(gfx.RoleButton, "工作").Selected {
		t.Error("右键之后行不应呈选中状态")
	}
}

// 菜单开着时在另一行右键：菜单就地换成那一行的，只有一个菜单。
func TestRightClickWhileAMenuIsOpenMovesTheMenuToTheNewRow(t *testing.T) {
	r := newRig(t)
	r.rightClick(gfx.RoleButton, "go")
	if !r.has(gfx.RoleMenuItem, locales.LibraryTagColor()) {
		t.Fatal("右键标签行应打开标签菜单")
	}
	r.rightClick(gfx.RoleButton, "工作")
	if !r.has(gfx.RoleMenuItem, deleteCollectionLabel()) {
		t.Error("在合集行再右键，菜单应换成合集菜单")
	}
	if r.has(gfx.RoleMenuItem, locales.LibraryTagColor()) {
		t.Error("标签菜单不应还留着")
	}
	menus := 0
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleMenu {
			menus++
		}
	}
	if menus != 1 {
		t.Errorf("画面上有 %d 个菜单，期望 1 个", menus)
	}
}

// 右键开出的菜单没有触发按钮：旧的「更多」按钮不再享有「点它只收起菜单」的待遇，
// 点它走普通的外部点击——菜单收起，同一次点击照常落到按钮上，开出这一行自己的菜单（渲染层的 menuTriggerRef 被置空）。
// （这条行为现在由背景层的「右键按下先关菜单」保证，popMenu 里清空触发按钮是冗余的显式写法，见文件头的 N02。）
func TestRightClickMenuDoesNotKeepTheOldMoreButtonAsItsTrigger(t *testing.T) {
	r := newRig(t)
	more := r.mustNode(gfx.RoleButton, locales.LibraryMoreActions()) // 「工作」行的
	mx, my := centerOf(more)
	r.clickAt(mx, my)
	if !r.has(gfx.RoleMenuItem, deleteCollectionLabel()) {
		t.Fatal("点「更多」应打开合集菜单")
	}
	r.rightClick(gfx.RoleButton, "go") // 菜单换成标签的，触发按钮应随之清掉
	if !r.has(gfx.RoleMenuItem, locales.LibraryTagColor()) {
		t.Fatal("右键标签行应换成标签菜单")
	}
	r.clickAt(mx, my) // 再点「工作」行的「更多」
	if !r.has(gfx.RoleMenuItem, deleteCollectionLabel()) {
		t.Error("触发按钮已换，点旧的「更多」应开出它自己的菜单，而不是只收起")
	}
	if r.has(gfx.RoleMenuItem, locales.LibraryTagColor()) {
		t.Error("标签菜单应已收起")
	}
}

// 删掉正筛着的合集：范围回到「全部」；分面里残留的合集 ID 与渲染层一致（setScope 不清分面，台账登记）。
// 没筛着它的时候范围不动。
func TestDeletingTheSelectedCollectionResetsScopeButKeepsTheStaleFacet(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, "收藏 2")
	r.click(gfx.RoleButton, "工作")
	r.rightClick(gfx.RoleButton, "工作")
	r.click(gfx.RoleMenuItem, deleteCollectionLabel())
	f := r.view().Filters
	if f.Scope != services.LibraryScopeAll {
		t.Errorf("删除正筛着的合集后范围应回到「全部」，实际 %q", f.Scope)
	}
	if f.CollectionID != "c1" {
		t.Errorf("渲染层不清分面，残留的合集 ID 应保持 c1，实际 %q", f.CollectionID)
	}
	if !r.hasText(locales.LibraryCollectionDeleted("工作")) {
		t.Error("应弹出撤销提示")
	}
}

func TestDeletingAnotherCollectionKeepsTheCurrentScope(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, "收藏 2")
	r.click(gfx.RoleButton, "工作")
	r.rightClick(gfx.RoleButton, "阅读")
	r.click(gfx.RoleMenuItem, deleteCollectionLabel())
	f := r.view().Filters
	if f.Scope != services.LibraryScopeFavorites || f.CollectionID != "c1" {
		t.Errorf("删的不是正筛着的合集，筛选不应变：%+v", f)
	}
}
