package library

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 SidebarLibraryPanel、LibrarySidebarRow、LibraryScopeTabs、SmartViewsSection 与 CollectionIconPicker 的行为：
// 分组与计数、行的选中与菜单、新建与重命名弹窗、撤销窗口、智能视图的保存与删除。像素对照在 viva/parity 的场景里。

func deleteCollectionLabel() string {
	return locales.LibraryDeleteCollection() + " " + locales.LibraryDeleteCollectionDesc()
}

// ---- 分组与计数 ----

func TestPanelListsScopesAndGroups(t *testing.T) {
	r := newRig(t)
	for _, name := range []string{"未归合集 3", "全部 12", "收藏 2", "归档 1", "工作", "阅读", "哔哩哔哩", "YouTube", "go", "rust", "回收站"} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("侧栏里没有「%s」", name)
		}
	}
	if r.has(gfx.RoleButton, "闲置") {
		t.Error("没有条目的标签默认不列出")
	}
	if !r.mustNode(gfx.RoleButton, "全部 12").Selected {
		t.Error("初始范围是「全部」，页签应当选中")
	}
	if !r.hasText("99+") || r.hasText("120") {
		t.Error("超过 99 的计数写成 99+")
	}
	// 回收站的计数照常显示；计数为 0 的行不画数字（没有「0」）
	if r.hasText("0") {
		t.Error("计数为 0 时不画数字")
	}
}

func TestEmptyGroupsShowHints(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.Seed(services.LibraryCounts{All: 0}, nil, nil) })
	r.frames(2)
	for _, text := range []string{locales.LibraryNoCollections(), locales.LibraryNoPlatforms(), locales.LibraryNoTags()} {
		if !r.hasText(text) {
			t.Errorf("没有内容时应给出说明「%s」", text)
		}
	}
}

func TestScopeTabsSwitchScopeAndKeepFacets(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, "收藏 2")
	if got := r.view().Filters.Scope; got != services.LibraryScopeFavorites {
		t.Fatalf("点「收藏」后范围是 %q", got)
	}
	if !r.mustNode(gfx.RoleButton, "收藏 2").Selected || r.mustNode(gfx.RoleButton, "全部 12").Selected {
		t.Error("选中状态应当跟到「收藏」页签")
	}
	// 范围与分面正交：叠加了分面照样能切范围，分面保留
	r.click(gfx.RoleButton, "工作")
	r.click(gfx.RoleButton, "归档 1")
	if f := r.view().Filters; f.Scope != services.LibraryScopeArchived || f.CollectionID != "c1" {
		t.Errorf("筛选 = %+v，期望范围归档、合集 c1", f)
	}
	// 回收站只有范围是回收站且没有叠加分面时才算选中
	r.click(gfx.RoleButton, "回收站")
	if r.mustNode(gfx.RoleButton, "回收站").Selected {
		t.Error("叠着合集分面时回收站这一行不应选中")
	}
	r.click(gfx.RoleButton, "工作") // 再点一次取消合集
	if !r.mustNode(gfx.RoleButton, "回收站").Selected {
		t.Error("没有分面的回收站范围应当选中")
	}
}

func TestFacetRowsToggleSelection(t *testing.T) {
	r := newRig(t)
	cases := []struct {
		name string
		get  func(Filters) string
		want string
	}{
		{"工作", func(f Filters) string { return f.CollectionID }, "c1"},
		{"哔哩哔哩", func(f Filters) string { return f.Platform }, "bilibili"},
		{"go", func(f Filters) string { return f.TagID }, "t1"},
	}
	for _, c := range cases {
		r.click(gfx.RoleButton, c.name)
		if got := c.get(r.view().Filters); got != c.want {
			t.Errorf("点「%s」后分面 = %q，期望 %q", c.name, got, c.want)
		}
		if !r.mustNode(gfx.RoleButton, c.name).Selected {
			t.Errorf("「%s」应当选中", c.name)
		}
	}
	// 三个分面叠加：计数查询带上全部
	if q := r.svc.CountQueries(); q[len(q)-1].CollectionID != "c1" || q[len(q)-1].TagID != "t1" || q[len(q)-1].Platform != "bilibili" {
		t.Errorf("最近一次计数查询 = %+v，应带三个分面", q[len(q)-1])
	}
	for _, c := range cases {
		r.click(gfx.RoleButton, c.name)
		if got := c.get(r.view().Filters); got != "" {
			t.Errorf("再点「%s」应取消选择，分面 = %q", c.name, got)
		}
	}
}

func TestUnusedTagsAreHiddenUntilToggled(t *testing.T) {
	r := newRig(t)
	show := locales.LibraryShowUnusedTags(1)
	r.click(gfx.RoleButton, show)
	if !r.has(gfx.RoleButton, "闲置") || !r.has(gfx.RoleButton, locales.LibraryHideUnusedTags()) {
		t.Fatal("打开开关后应列出未使用的标签，按钮改成「隐藏」")
	}
	r.click(gfx.RoleButton, locales.LibraryHideUnusedTags())
	if r.has(gfx.RoleButton, "闲置") {
		t.Error("关掉开关后未使用的标签应隐藏")
	}
	// 正筛着的标签即使没有条目也要列出，免得用户所在的位置被藏掉
	r.click(gfx.RoleButton, show)
	r.click(gfx.RoleButton, "闲置")
	r.click(gfx.RoleButton, locales.LibraryHideUnusedTags())
	if !r.has(gfx.RoleButton, "闲置") {
		t.Error("选中的标签应一直列出")
	}
	if r.has(gfx.RoleButton, show) || r.has(gfx.RoleButton, locales.LibraryHideUnusedTags()) {
		t.Error("没有可露出的未使用标签、开关也关着时，不摆这个按钮")
	}
}

// ---- 菜单 ----

func TestMoreButtonOpensCollectionMenuAndSecondClickClosesIt(t *testing.T) {
	r := newRig(t)
	btn := r.mustNode(gfx.RoleButton, locales.LibraryMoreActions())
	x, y := centerOf(btn)
	r.clickAt(x, y)
	for _, item := range []string{locales.LibraryRename(), deleteCollectionLabel()} {
		if !r.has(gfx.RoleMenuItem, item) {
			t.Errorf("合集菜单里没有「%s」", item)
		}
	}
	menu := r.mustNode(gfx.RoleMenu, "")
	if menu.Bounds.X != x || menu.Bounds.Y != y {
		t.Errorf("鼠标点「更多」：菜单应开在光标处 (%.0f,%.0f)，实际 (%.0f,%.0f)", x, y, menu.Bounds.X, menu.Bounds.Y)
	}
	// 菜单开在光标处，会盖住按钮的右下角：再点按钮没被盖住的左上角
	r.clickAt(btn.Bounds.X+3, btn.Bounds.Y+3)
	if r.has(gfx.RoleMenu, "") {
		t.Error("再点同一枚「更多」按钮应收起菜单")
	}
}

func TestRightClickOpensRowMenuAtPointer(t *testing.T) {
	r := newRig(t)
	n := r.mustNode(gfx.RoleButton, "go")
	r.rightClick(gfx.RoleButton, "go")
	if !r.has(gfx.RoleMenuItem, locales.LibraryRename()) || !r.has(gfx.RoleMenuItem, locales.LibraryTagColor()) {
		t.Fatal("右键标签行应打开标签菜单（重命名、颜色、删除）")
	}
	cx, cy := centerOf(n)
	menu := r.mustNode(gfx.RoleMenu, "")
	if menu.Bounds.X != cx || menu.Bounds.Y != cy {
		t.Errorf("右键：菜单应开在指针处 (%.0f,%.0f)，实际 (%.0f,%.0f)", cx, cy, menu.Bounds.X, menu.Bounds.Y)
	}
	// 在另一行再右键：始终重开（不会因为菜单开着而收起）
	r.keys("Escape")
	r.rightClick(gfx.RoleButton, "工作")
	if !r.has(gfx.RoleMenuItem, deleteCollectionLabel()) {
		t.Error("右键合集行应打开合集菜单")
	}
	// 平台行与回收站没有行菜单：右键什么都不开
	r.keys("Escape")
	r.rightClick(gfx.RoleButton, "哔哩哔哩")
	if r.has(gfx.RoleMenu, "") {
		t.Error("平台行没有菜单")
	}
}

func TestKeyboardOpenedMenuAnchorsAtButtonBottomLeft(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 20; i++ {
		r.keys("Tab")
		focused := false
		for _, n := range r.session.Access() {
			if n.Focused && n.Role == gfx.RoleButton && n.Label == locales.LibraryMoreActions() {
				focused = true
			}
		}
		if focused {
			break
		}
	}
	var btn gfx.Rect
	for _, n := range r.session.Access() {
		if n.Focused {
			btn = n.Bounds
		}
	}
	r.keys("Enter")
	menu, ok := r.node(gfx.RoleMenu, "")
	if !ok {
		t.Fatal("键盘按 Enter 应打开菜单")
	}
	if menu.Bounds.X != btn.X || menu.Bounds.Y != btn.Y+btn.Height {
		t.Errorf("键盘打开：菜单应锚在按钮左下角 (%.0f,%.0f)，实际 (%.0f,%.0f)", btn.X, btn.Y+btn.Height, menu.Bounds.X, menu.Bounds.Y)
	}
}

func TestTagColorSubmenuSetsColor(t *testing.T) {
	r := newRig(t)
	r.rightClick(gfx.RoleButton, "rust")
	r.hover(gfx.RoleMenuItem, locales.LibraryTagColor())
	r.click(gfx.RoleMenuItem, locales.LibraryColorPurple())
	if got := r.mutating(); len(got) != 1 || got[0] != "UpdateTag:t2" {
		t.Fatalf("服务收到 %q，期望一次 UpdateTag:t2", got)
	}
	for _, tag := range r.svc.TagsNow() {
		if tag.ID == "t2" && tag.ColorKey != "purple" {
			t.Errorf("t2 的颜色 = %q，期望 purple", tag.ColorKey)
		}
	}
	if r.has(gfx.RoleMenu, "") {
		t.Error("选了颜色后菜单应关闭")
	}
}

// ---- 新建与重命名弹窗 ----

func openRenameCollection(r *rig) {
	r.click(gfx.RoleButton, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.LibraryRename())
}

func TestRenameCollectionFlow(t *testing.T) {
	r := newRig(t)
	openRenameCollection(r)
	if !r.has(gfx.RoleHeading, locales.LibraryRename()) {
		t.Fatal("应打开「重命名」弹窗")
	}
	field := r.mustNode(gfx.RoleTextField, locales.LibraryNamePlaceholder())
	if field.Value != "工作" || !field.Focused {
		t.Errorf("名称输入框 Value=%q Focused=%v，期望带入旧名称且已聚焦", field.Value, field.Focused)
	}
	if !r.mustNode(gfx.RoleButton, locales.LibraryCollectionIconDefault()).Selected {
		t.Error("没有图标的合集应选中「默认图标」")
	}
	r.keys("cmd+a")
	r.typeText("项目")
	r.click(gfx.RoleButton, "📖")
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if got := r.mutating(); len(got) != 1 || got[0] != "UpdateCollection:c1" {
		t.Fatalf("服务收到 %q，期望一次 UpdateCollection:c1", got)
	}
	if c := r.svc.CollectionsNow()[0]; c.Name != "项目" || c.Icon != "📖" {
		t.Errorf("合集 = %+v，期望名称「项目」图标 📖", c)
	}
	if r.has(gfx.RoleHeading, locales.LibraryRename()) {
		t.Error("成功后弹窗应关闭")
	}
	if !r.has(gfx.RoleButton, "项目") || r.has(gfx.RoleButton, "工作") {
		t.Error("侧栏应显示新名称")
	}
}

func TestCreateCollectionSelectsTheNewCollection(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, locales.LibraryNewCollection())
	if !r.has(gfx.RoleHeading, locales.LibraryNewCollection()) {
		t.Fatal("应打开「新建合集」弹窗")
	}
	if !r.mustNode(gfx.RoleButton, locales.CommonConfirm()).Disabled {
		t.Error("名称为空时「确认」应禁用")
	}
	r.typeText("读书")
	r.click(gfx.RoleButton, "🎓")
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if got := r.mutating(); len(got) != 1 || got[0] != "CreateCollection:读书" {
		t.Fatalf("服务收到 %q，期望一次 CreateCollection:读书", got)
	}
	if got := r.svc.CollectionsNow(); got[len(got)-1].Icon != "🎓" {
		t.Errorf("新合集的图标 = %q，期望 🎓", got[len(got)-1].Icon)
	}
	if got := r.view().Filters.CollectionID; got != "col-new-1" {
		t.Errorf("新建后应选中新合集，当前 = %q", got)
	}
	if r.has(gfx.RoleHeading, locales.LibraryNewCollection()) {
		t.Error("成功后弹窗应关闭")
	}
}

func TestEnterSubmitsAndBlankNameDoesNothing(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, locales.LibraryNewCollection())
	r.keys("Enter")
	if len(r.mutating()) != 0 {
		t.Fatal("名称为空时回车不应提交")
	}
	r.typeText("  读书  ")
	r.keys("Enter")
	if got := r.mutating(); len(got) != 1 || got[0] != "CreateCollection:读书" {
		t.Fatalf("服务收到 %q，期望一次 CreateCollection:读书（名称去首尾空白）", got)
	}
}

func TestRenameTagUsesTheNarrowDialogWithoutIcons(t *testing.T) {
	r := newRig(t)
	r.rightClick(gfx.RoleButton, "go")
	r.click(gfx.RoleMenuItem, locales.LibraryRename())
	dialog := r.mustNode(gfx.RoleGroup, locales.LibraryRename())
	if dialog.Bounds.Width >= 500 {
		t.Errorf("重命名标签用窄弹窗，宽度 = %.0f", dialog.Bounds.Width)
	}
	if r.has(gfx.RoleButton, locales.LibraryCollectionIconDefault()) {
		t.Error("标签没有图标选择器")
	}
	r.keys("cmd+a")
	r.typeText("golang")
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if got := r.mutating(); len(got) != 1 || got[0] != "UpdateTag:t1" {
		t.Fatalf("服务收到 %q，期望一次 UpdateTag:t1", got)
	}
	if !r.has(gfx.RoleButton, "golang") {
		t.Error("侧栏应显示新名称")
	}
}

func TestEditFailureKeepsDialogOpenAndExplainsWhy(t *testing.T) {
	r := newRig(t)
	r.svc.FailOn("UpdateCollection", errors.New("磁盘已满"))
	openRenameCollection(r)
	r.keys("cmd+a")
	r.typeText("项目")
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if !r.has(gfx.RoleHeading, locales.LibraryRename()) {
		t.Error("失败后弹窗应保持打开，让用户改了重试")
	}
	if !r.session.HasText("磁盘已满") {
		t.Errorf("应弹出带原因的错误提示；画面文字 %q", r.session.Texts())
	}
	// 重试：取消注入后再点确认就成功
	r.svc.FailOn("UpdateCollection", nil)
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if r.has(gfx.RoleHeading, locales.LibraryRename()) {
		t.Error("重试成功后弹窗应关闭")
	}
}

// 提交在途时再按回车或再点确认不会发出第二个请求（台账 C-005）。
func TestSubmitIsIgnoredWhileARequestIsInFlight(t *testing.T) {
	r := newRig(t)
	var queued []func()
	r.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	r.click(gfx.RoleButton, locales.LibraryNewCollection())
	r.typeText("读书")
	r.keys("Enter")
	r.keys("Enter")
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if len(queued) != 1 {
		t.Fatalf("在途期间排了 %d 个后台请求，期望 1", len(queued))
	}
	queued[0]()
	r.frames(3)
	if r.has(gfx.RoleHeading, locales.LibraryNewCollection()) {
		t.Error("请求完成后弹窗应关闭")
	}
	if got := r.mutating(); len(got) != 1 {
		t.Errorf("服务收到 %q，期望只有一次创建", got)
	}
}

func TestIconPickerShowsCustomIconAndKeepsItSelectable(t *testing.T) {
	r := newRig(t, func(r *rig) {
		counts, cols, tags := libraryFixture()
		cols[0].Icon = "🦄" // 目录之外：旧库迁移来的
		r.svc.Seed(counts, cols, tags)
	})
	openRenameCollection(r)
	current := "🦄 " + locales.LibraryCollectionIconCurrent()
	if !r.mustNode(gfx.RoleButton, current).Selected {
		t.Fatal("目录外的当前图标应单独摆一格并选中")
	}
	r.click(gfx.RoleButton, "📖")
	if r.mustNode(gfx.RoleButton, current).Selected || !r.mustNode(gfx.RoleButton, "📖").Selected {
		t.Fatal("点目录里的图标后选中应移过去")
	}
	r.click(gfx.RoleButton, current)
	if !r.mustNode(gfx.RoleButton, current).Selected {
		t.Error("点了别处之后仍能选回当前图标")
	}
	r.click(gfx.RoleButton, locales.LibraryCollectionIconDefault())
	r.click(gfx.RoleButton, locales.CommonConfirm())
	if c := r.svc.CollectionsNow()[0]; c.Icon != "" {
		t.Errorf("选「默认图标」应清掉图标，实际 %q", c.Icon)
	}
}

func TestIconPickerScrollsTheSelectedIconIntoView(t *testing.T) {
	r := newRig(t, func(r *rig) {
		counts, cols, tags := libraryFixture()
		cols[0].Icon = "🧭" // 第七组
		r.svc.Seed(counts, cols, tags)
	})
	openRenameCollection(r)
	n, ok := r.node(gfx.RoleButton, "🧭")
	if !ok {
		t.Fatal("已选的图标应已滚进视野")
	}
	grid := r.mustNode(gfx.RoleGroup, locales.LibraryRename())
	if n.Bounds.Y < grid.Bounds.Y || n.Bounds.Y+n.Bounds.Height > grid.Bounds.Y+grid.Bounds.Height {
		t.Errorf("已选图标 %+v 不在弹窗 %+v 之内", n.Bounds, grid.Bounds)
	}
}

// ---- 删除与撤销 ----

func TestDeleteCollectionHidesTheRowAndUndoRestoresIt(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, deleteCollectionLabel())
	if r.has(gfx.RoleButton, "工作") {
		t.Error("删除后行应立即隐藏")
	}
	if len(r.mutating()) != 0 {
		t.Errorf("撤销窗口内不应请求后端，实际 %q", r.mutating())
	}
	if !r.hasText(locales.LibraryCollectionDeleted("工作")) {
		t.Errorf("应弹出撤销提示；画面文字 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CommonUndo())
	if !r.has(gfx.RoleButton, "工作") {
		t.Error("撤销后行应回来")
	}
	r.clock.Advance(DeletionUndoWindow + time.Second)
	r.frames(2)
	if len(r.mutating()) != 0 {
		t.Errorf("撤销之后窗口到点也不应删除，实际 %q", r.mutating())
	}
}

func TestDeleteCollectionCommitsAfterTheUndoWindow(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, deleteCollectionLabel())
	r.clock.Advance(DeletionUndoWindow)
	r.frames(2)
	if got := r.mutating(); len(got) != 1 || got[0] != "DeleteCollection:c1" {
		t.Fatalf("服务收到 %q，期望窗口到点后一次 DeleteCollection:c1", got)
	}
}

// 删掉正筛着的合集或标签：范围回到「全部」；分面里残留的 ID 与渲染层一致（setScope 不清分面，台账登记）。
func TestDeletingTheSelectedTagResetsScopeButKeepsTheStaleFacet(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, "收藏 2")
	r.click(gfx.RoleButton, "go")
	r.rightClick(gfx.RoleButton, "go")
	r.click(gfx.RoleMenuItem, locales.LibraryDeleteTag())
	f := r.view().Filters
	if f.Scope != services.LibraryScopeAll {
		t.Errorf("删除正筛着的标签后范围应回到「全部」，实际 %q", f.Scope)
	}
	if f.TagID != "t1" {
		t.Errorf("渲染层不清分面，残留的标签 ID 应保持 t1，实际 %q", f.TagID)
	}
	if !r.hasText(locales.LibraryTagDeleted("go")) {
		t.Error("应弹出撤销提示")
	}
}

// ---- 智能视图 ----

func TestSmartViewsSaveApplyAndRemove(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, "工作")
	r.click(gfx.RoleButton, locales.LibrarySmartViewSaveCurrent())
	if !r.has(gfx.RoleHeading, locales.LibrarySmartViewSaveTitle()) {
		t.Fatal("应打开「保存智能视图」弹窗")
	}
	if !r.mustNode(gfx.RoleButton, locales.CommonSave()).Disabled {
		t.Error("名称为空时「保存」应禁用")
	}
	r.typeText("工作笔记")
	r.click(gfx.RoleButton, locales.CommonSave())
	saved := ReadSavedViews(r.prefs)
	if len(saved) != 1 || saved[0].Name != "工作笔记" || saved[0].Filters.CollectionID != "c1" || saved[0].ID != "sv-1" {
		t.Fatalf("存储里的智能视图 = %+v", saved)
	}
	if r.has(gfx.RoleHeading, locales.LibrarySmartViewSaveTitle()) {
		t.Error("保存后弹窗应关闭")
	}
	if !r.mustNode(gfx.RoleButton, "工作笔记").Selected {
		t.Error("当前筛选正好是这个视图，它应选中")
	}
	// 换一个筛选，再点视图：范围与分面一次回来
	r.click(gfx.RoleButton, "工作")
	r.click(gfx.RoleButton, "收藏 2")
	if r.mustNode(gfx.RoleButton, "工作笔记").Selected {
		t.Error("筛选变了，视图不应再选中")
	}
	r.click(gfx.RoleButton, "工作笔记")
	if f := r.view().Filters; f.Scope != services.LibraryScopeAll || f.CollectionID != "c1" {
		t.Errorf("应用视图后筛选 = %+v，期望范围全部、合集 c1", f)
	}
	// 行尾的按钮直接删除：没有菜单、没有确认
	r.click(gfx.RoleButton, locales.LibrarySmartViewDelete())
	if r.has(gfx.RoleButton, "工作笔记") || len(ReadSavedViews(r.prefs)) != 0 {
		t.Error("删除后行与存储里都应消失")
	}
}

func TestSmartViewDialogCancelKeepsTheDraftAndBlankNameSavesNothing(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, locales.LibrarySmartViewSaveCurrent())
	r.typeText("草稿")
	r.click(gfx.RoleButton, locales.CommonCancel())
	if r.has(gfx.RoleHeading, locales.LibrarySmartViewSaveTitle()) {
		t.Fatal("取消应关闭弹窗")
	}
	r.click(gfx.RoleButton, locales.LibrarySmartViewSaveCurrent())
	if got := r.mustNode(gfx.RoleTextField, locales.LibrarySmartViewNamePlaceholder()).Value; got != "草稿" {
		t.Errorf("取消不清空输入，再次打开应还是「草稿」，实际 %q", got)
	}
	r.keys("cmd+a")
	r.keys("Backspace")
	r.keys("Enter")
	if len(ReadSavedViews(r.prefs)) != 0 {
		t.Error("名称为空时回车不应保存")
	}
}

func TestSmartViewSaveFailureIsReportedButTheListStillUpdates(t *testing.T) {
	r := newRig(t)
	r.prefs.FailSetWith(errors.New("配置文件只读"))
	r.click(gfx.RoleButton, locales.LibrarySmartViewSaveCurrent())
	r.typeText("临时")
	r.click(gfx.RoleButton, locales.CommonSave())
	if !r.has(gfx.RoleButton, "临时") {
		t.Error("界面上的列表照常更新（与渲染层一致）")
	}
	if !r.session.HasText("配置文件只读") && !strings.Contains(strings.Join(r.log.entries(), "\n"), "配置文件只读") {
		t.Errorf("写入失败应有提示或日志；画面文字 %q", r.session.Texts())
	}
}

// ---- 挂载 ----

// 侧栏这一帧没画（切到别的模块或设置页）就当作卸载：回来时弹窗关闭、未使用标签的开关复位、智能视图重新读取。
func TestReenteringThePanelResetsItsState(t *testing.T) {
	r := newRig(t)
	r.click(gfx.RoleButton, locales.LibraryShowUnusedTags(1))
	r.click(gfx.RoleButton, locales.LibraryNewCollection())
	r.typeText("半途")
	r.hidden = true
	r.frames(3)
	r.hidden = false
	r.frames(3)
	if r.has(gfx.RoleHeading, locales.LibraryNewCollection()) {
		t.Error("重新进入后弹窗应关闭")
	}
	if r.has(gfx.RoleButton, "闲置") {
		t.Error("重新进入后未使用标签的开关应复位")
	}
	r.click(gfx.RoleButton, locales.LibraryNewCollection())
	if got := r.mustNode(gfx.RoleTextField, locales.LibraryNamePlaceholder()).Value; got != "" {
		t.Errorf("新建合集的输入框应是空的，实际 %q", got)
	}
}
