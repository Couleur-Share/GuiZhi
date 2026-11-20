package library

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件补测详情界面壳的接线（台账 M2-4 遗留的成批变异检查）：键盘钩子、详情浮层与整理条、头部、通知条、内容面板与工具栏、
// 「更多」菜单与来源动作。detail_ui_test.go 只测了各块的主路径；这里测每一处分支：谁在什么条件下出现、按钮点下去走向哪里、
// 边界上的行为（到头、回收站、没有来源、没有内容）。能从界面走到的状态一律用真实的点击与按键走；
// 只有生产里由后台动作置位、界面只读的状态（读取失败之后的别条冲突、正在标记复核……）才直接写 Store 的视图。
// 数据是 detailRig 的四条加下面的追加条目（回收站里的、没有内容的、没有文字稿的……），追加的都带正文。

// shellExtra 是壳测试在 detailRig 的四条之外追加的一条（列表投影加正文）。
type shellExtra struct {
	entry services.LibraryEntry
	body  servicestest.DetailBody
}

const (
	shellBlankID       = "x-blank"
	shellTrashNoteID   = "x-trash-note"
	shellTrashForumID  = "x-trash-forum"
	shellTrashVideoID  = "x-trash-video"
	shellTrashReviewID = "x-trash-review"
	shellBareVideoID   = "x-bare-video"
	shellArchivedID    = "x-archived"
	shellCollectedID   = "x-collected"
	shellDouyinID      = "x-douyin"
)

// shellEntry 照笔记的投影造一条，再按 edit 调整。
func shellEntry(id, title, kind string, edit ...func(*services.LibraryEntry)) services.LibraryEntry {
	e := detailEntryOf(detailNoteID)
	e.ID, e.Title, e.ItemType, e.IsPinned = id, title, kind, false
	for _, f := range edit {
		f(&e)
	}
	return e
}

func shellExtras() []shellExtra {
	trashed := func(e *services.LibraryEntry) { e.DeletedAt = 5 }
	return []shellExtra{
		{shellEntry(shellBlankID, "空白笔记", services.LibraryTypeNote), servicestest.DetailBody{}},
		{shellEntry(shellTrashNoteID, "回收站里的笔记", services.LibraryTypeNote, trashed),
			servicestest.DetailBody{Content: "# 已删除\n\n正文。", SourceURI: "https://example.com/trash"}},
		{shellEntry(shellTrashForumID, "回收站里的论坛帖", "forum", trashed),
			servicestest.DetailBody{Content: detailForumContent, SourceURI: "https://linux.do/t/456"}},
		{shellEntry(shellTrashVideoID, "回收站里的视频", "video", trashed),
			servicestest.DetailBody{Content: "视频", Transcript: "这是一段文字稿内容。", SourceURI: "https://www.bilibili.com/video/BV1zz"}},
		{shellEntry(shellTrashReviewID, "回收站里待复核的网页", "webpage", trashed, func(e *services.LibraryEntry) {
			e.ReviewStatus, e.ReviewReasons = "needs_review", []string{"正文被截断"}
		}), servicestest.DetailBody{Content: "网页正文。", SourceURI: "https://example.com/review"}},
		{shellEntry(shellBareVideoID, "没有文字稿的视频", "video"),
			servicestest.DetailBody{Content: "视频", SourceURI: "https://www.bilibili.com/video/BV1yy"}},
		{shellEntry(shellArchivedID, "已归档的笔记", services.LibraryTypeNote, func(e *services.LibraryEntry) { e.Status = services.LibraryStatusArchived }),
			servicestest.DetailBody{Content: "正文"}},
		{shellEntry(shellCollectedID, "在合集里的笔记", services.LibraryTypeNote, func(e *services.LibraryEntry) { e.CollectionID = "c1" }),
			servicestest.DetailBody{Content: "正文"}},
		{shellEntry(shellDouyinID, "抖音笔记", services.LibraryTypeNote),
			servicestest.DetailBody{Content: "正文", SourceURI: "https://www.douyin.com/video/7300000000000000000"}},
	}
}

// shellRig 是 detailRig 加上追加条目的夹具：服务里的条目全集换成「四条加追加的」，追加的正文一并播种；还没有选中任何一条。
func shellRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	return detailRig(t, append([]func(*rig){func(r *rig) {
		entries := detailEntries()
		for _, x := range shellExtras() {
			entries = append(entries, x.entry)
			r.svc.SeedDetail(x.entry.ID, x.body)
		}
		r.svc.SeedEntries(entries)
	}}, mutate...)...)
}

// pick 经 Store 选中一条并等详情读到：不依赖卡片在不在列表窗口里。回收站里的条目先用 goTrash 切到回收站范围。
func (r *rig) pick(id string) {
	r.t.Helper()
	r.store.SelectItem(id)
	r.frames(3)
	if d := r.view().Detail; d.Item == nil || d.Item.ID != id {
		r.t.Fatalf("选中 %s 之后详情应读到它：%+v", id, d)
	}
}

// pickTrashed 切到回收站范围再选中一条回收站里的条目。
func (r *rig) pickTrashed(id string) {
	r.t.Helper()
	r.goTrash()
	r.pick(id)
}

// setDetail 直接改 Store 里的详情状态：只用在生产里由后台动作置位、界面只读而且从界面走不到的状态上。
func (r *rig) setDetail(edit func(d *DetailView)) {
	r.t.Helper()
	r.store.mu.Lock()
	edit(&r.store.view.Detail)
	r.store.mu.Unlock()
	r.frames(3)
}

// nodeWithPrefix 取第一个可访问名以 prefix 开头的 role 节点（折叠块的按钮名是整段摘要）。
func (r *rig) nodeWithPrefix(role gfx.Role, prefix string) (app.AccessNode, bool) {
	for _, n := range r.session.Access() {
		if n.Role == role && strings.HasPrefix(n.Label, prefix) {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// detailNode 取详情栏里的节点：侧栏与列表栏里有同名的按钮（「更多操作」「收藏」），所以只认左缘不在标题框之左的那些。
func (r *rig) detailNode(role gfx.Role, name string) (app.AccessNode, bool) {
	left := float32(0)
	if title, ok := r.node(gfx.RoleTextField, locales.LibraryTitlePlaceholder()); ok {
		left = title.Bounds.X
	}
	for _, n := range r.session.Access() {
		if n.Role == role && n.Label == name && n.Bounds.X >= left-1 {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

func (r *rig) hasInDetail(role gfx.Role, name string) bool {
	_, ok := r.detailNode(role, name)
	return ok
}

func (r *rig) clickInDetail(role gfx.Role, name string) {
	r.t.Helper()
	n, ok := r.detailNode(role, name)
	if !ok {
		r.t.Fatalf("详情栏里找不到 %v「%s」", role, name)
	}
	x, y := centerOf(n)
	r.clickAt(x, y)
}

// screenText 是此刻画面上所有文字拼成的一串。
func (r *rig) screenText() string { return strings.Join(r.session.Texts(), "\n") }

// openModal 切到表格视图、点第一行打开详情浮层，返回冻结前的列表。
func (r *rig) openModal() []services.LibraryEntry {
	r.t.Helper()
	r.toTable()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[0])
	r.clickAt(x, y)
	if !r.screen.DetailModalOpen() {
		r.t.Fatal("夹具失效：点行应打开详情浮层")
	}
	return entries
}

// ---- 键盘 ----

// Ctrl+F 只在工具栏摆着查找入口时拦截：回收站条目的讨论页签没有查找入口（预览不算），这时它不拦、也不开查找条。
func TestCtrlFNeedsAFindEntry(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pickTrashed(shellTrashForumID)
	r.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if r.has(gfx.RoleButton, locales.LibraryPanelFindOpen()) {
		t.Fatal("夹具失效：回收站条目的讨论页签不该有查找入口")
	}
	res := r.screen.ListShortcut(gfx.KeyEvent{Modifiers: gfx.ModifierControl, Text: "f"})
	if res.Handled || r.screen.detail.panel.findOpen {
		t.Fatalf("没有查找入口时 Ctrl+F 不该拦截也不该开查找条：%+v findOpen=%v", res, r.screen.detail.panel.findOpen)
	}
}

// Esc 只在焦点在查找框里时关查找条；焦点在别处（点了标题框）时 Esc 不动它。
func TestEscClosesTheFindBarOnlyWhileTheFindFieldHasFocus(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.keys("ctrl+f")
	placeholder := locales.LibraryPanelFindPlaceholder()
	if !r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("夹具失效：Ctrl+F 应打开查找条")
	}
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	r.keys("escape")
	if !r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("焦点不在查找框里时 Esc 不该关查找条")
	}
}

// 菜单开着（焦点在浮层里）时 Esc 只关菜单，不退出专注阅读；菜单关了再按一次才退出。
func TestEscInAFloatingLayerKeepsFocusReading(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.keys("alt+z")
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	if !r.has(gfx.RoleMenuItem, locales.LibraryUnpin()) {
		t.Fatal("夹具失效：点「更多」应打开菜单")
	}
	r.keys("escape")
	if r.has(gfx.RoleMenuItem, locales.LibraryUnpin()) {
		t.Fatal("Esc 应关掉菜单")
	}
	if !r.screen.FocusReading() {
		t.Fatal("焦点在菜单里时 Esc 只该关菜单，不该退出专注阅读")
	}
	r.keys("escape")
	if r.screen.FocusReading() {
		t.Fatal("菜单关了之后再按一次 Esc 才退出专注阅读")
	}
}

// Alt+方向键只在详情浮层开着、焦点不在输入框里时整理：卡片视图里没有浮层，不拦截；输入框里按到钩子上也不拦截
// （真实窗口里输入框自己先吃掉这个键，钩子的这道判断是第二道防线，所以直接问钩子）。
func TestAltArrowsStepOnlyInTheModalAndNotWhileEditing(t *testing.T) {
	t.Parallel()
	altRight := gfx.KeyEvent{Modifiers: gfx.ModifierAlt, Key: gfx.KeyRight}

	card := detailRig(t)
	card.openCard(detailNoteID)
	if res := card.screen.ListKey(altRight, &card.canvas); res.Handled {
		t.Fatalf("卡片视图里没有整理浮层，Alt+→ 不该被拦下：%+v", res)
	}

	r := detailRig(t)
	entries := r.openModal()
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	if res := r.screen.ListKey(altRight, &r.canvas); res.Handled || r.view().List.SelectedID != entries[0].ID {
		t.Fatalf("焦点在标题框里时 Alt+→ 不该整理：%+v 选中 %q", res, r.view().List.SelectedID)
	}
}

// 工作区没挂载（切到别的模块）时 Ctrl+F 与 Alt+Z 都不拦截，也不改专注阅读。
func TestDetailShortcutsAreIgnoredWhileTheWorkspaceIsUnmounted(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.wsHidden = true
	r.frames(3)
	for name, e := range map[string]gfx.KeyEvent{
		"Ctrl+F": {Modifiers: gfx.ModifierControl, Text: "f"},
		"Alt+Z":  {Modifiers: gfx.ModifierAlt, Text: "z"},
	} {
		if res := r.screen.ListShortcut(e); res.Handled {
			t.Fatalf("工作区没挂载时 %s 不该被拦下", name)
		}
	}
	if r.screen.FocusReading() {
		t.Fatal("工作区没挂载时 Alt+Z 不该切换专注阅读")
	}
}

// ---- 详情浮层与整理条 ----

// 点行先冻结当前列表：冻结失败（读取失败）时浮层不开、弹出带原因的提示；对照开关 HideDetail 时行只选中、不开浮层。
func TestModalOpensOnlyAfterTheListIsFrozen(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.svc.FailOn("Freeze", errors.New("冻结失败：磁盘繁忙"))
	r.toTable()
	x, y := r.rowBlank(r.view().List.Entries[0])
	r.clickAt(x, y)
	if r.screen.DetailModalOpen() {
		t.Fatal("冻结失败时不该打开浮层")
	}
	if text := r.toastText(); !strings.Contains(text, "冻结失败：磁盘繁忙") {
		t.Fatalf("冻结失败的原因应进提示，实际画面文字 %q", text)
	}

	hidden := detailRig(t, func(r *rig) { r.screen.HideDetail = true })
	hidden.toTable()
	entries := hidden.view().List.Entries
	x, y = hidden.rowBlank(entries[0])
	hidden.clickAt(x, y)
	if hidden.screen.DetailModalOpen() || hidden.view().List.SelectedID != entries[0].ID {
		t.Fatalf("HideDetail 时点行只该选中：浮层 %v 选中 %q", hidden.screen.DetailModalOpen(), hidden.view().List.SelectedID)
	}
}

// 浮层开着时选中被清掉：浮层随即收起，不留一个空的整理条（渲染层 Boolean(selectedId)）。
func TestModalHidesWhenTheSelectionIsCleared(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openModal()
	if !r.has(gfx.RoleButton, locales.LibraryWorkflowNext()) {
		t.Fatal("夹具失效：浮层里应有整理条")
	}
	r.store.SelectItem("")
	r.frames(3)
	if r.has(gfx.RoleButton, locales.LibraryWorkflowNext()) {
		t.Fatal("没有选中时浮层不该显示（否则会闪出上一条）")
	}
}

// 整理条的三个按钮：第一条上「上一条」禁用、最后一条上「下一条」与「保存并下一条」禁用，方向不能反。
func TestReviewBarButtonsStepAndDisableAtTheEnds(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	entries := r.openModal()
	disabled := func(name string) bool { return r.mustNode(gfx.RoleButton, name).Disabled }
	prev, next, saveNext := locales.LibraryWorkflowPrevious(), locales.LibraryWorkflowNext(), locales.LibraryWorkflowSaveNext()
	if !disabled(prev) || disabled(next) || disabled(saveNext) {
		t.Fatalf("第一条：上一条应禁用、下一条与保存并下一条可点：%v %v %v", disabled(prev), disabled(next), disabled(saveNext))
	}
	r.click(gfx.RoleButton, next)
	if got := r.view().List.SelectedID; got != entries[1].ID {
		t.Fatalf("「下一条」应到第二条 %q，实际 %q", entries[1].ID, got)
	}
	r.click(gfx.RoleButton, prev)
	if got := r.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("「上一条」应回到第一条 %q，实际 %q", entries[0].ID, got)
	}
	for i := 1; i < len(entries); i++ {
		r.click(gfx.RoleButton, next)
	}
	if got := r.view().List.SelectedID; got != entries[len(entries)-1].ID {
		t.Fatalf("走到头应停在最后一条 %q，实际 %q", entries[len(entries)-1].ID, got)
	}
	if disabled(prev) || !disabled(next) || !disabled(saveNext) {
		t.Fatalf("最后一条：上一条可点、下一条与保存并下一条应禁用：%v %v %v", disabled(prev), disabled(next), disabled(saveNext))
	}
}

// 浮层开着而整理顺序为空：整理条整条不存在，也没有「0 / 0」这样的文字。
// 这是防御式守卫——正常流程里 BeginReview 成功才开浮层，顺序必非空，只能直接置浮层状态来走到（变异 M04 因此存活过一轮）。
func TestReviewBarIsAbsentWithoutAReviewOrder(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.toTable() // 浮层只在表格视图里声明
	r.pick(detailNoteID)
	r.screen.detail.modal.open = true
	r.frames(3)
	if !r.screen.DetailModalOpen() || len(r.view().Review.IDs) != 0 {
		t.Fatalf("夹具失效：浮层应开着且整理顺序为空：%+v", r.view().Review)
	}
	if !r.has(gfx.RoleGroup, locales.CommonDialog()) {
		t.Fatal("夹具失效：浮层应已画出（对话框节点在）")
	}
	// 整理条里的文字是分段画的（序号、「 / 」、总数各一段），「0 / 0」不会连着出现，所以认「 / 」这一段与三个按钮。
	if r.has(gfx.RoleButton, locales.LibraryWorkflowPrevious()) || r.has(gfx.RoleButton, locales.LibraryWorkflowNext()) ||
		r.has(gfx.RoleButton, locales.LibraryWorkflowSaveNext()) || strings.Contains(r.screenText(), "\n / \n") {
		t.Fatal("整理顺序为空时不该画整理条")
	}
}

// ---- 详情分派 ----

// 详情读取失败：显示失败原因与重试钮，点重试重新读取。
func TestDetailLoadErrorOffersARetry(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.svc.FailOn("GetItem", errors.New("读取失败：磁盘繁忙"))
	r.store.SelectItem(detailNoteID)
	r.frames(3)
	if !strings.Contains(r.screenText(), "读取失败：磁盘繁忙") || !r.has(gfx.RoleButton, locales.CommonRetry()) {
		t.Fatalf("读取失败应显示原因与重试钮，实际画面文字 %q", r.screenText())
	}
	r.svc.FailOn("GetItem", nil)
	r.click(gfx.RoleButton, locales.CommonRetry())
	if d := r.view().Detail; d.LoadError != "" || d.Item == nil || d.Item.ID != detailNoteID {
		t.Fatalf("点重试应重新读到详情：%+v", d)
	}
}

// 选中项已经换了而详情还是上一条的内容：显示「请选择」，不显示上一条（渲染层 selectedItem?.id !== selectedId）。
func TestDetailShowsNoSelectionWhileTheLoadedItemIsNotTheSelectedOne(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.store.mu.Lock()
	r.store.view.List.SelectedID = detailForumID
	r.store.mu.Unlock()
	r.frames(3)
	if !r.hasText(locales.LibraryNoSelection()) || r.has(gfx.RoleTextField, locales.LibraryTitlePlaceholder()) {
		t.Fatalf("详情不是选中的那条时应显示「%s」，实际画面文字 %q", locales.LibraryNoSelection(), r.screenText())
	}
}

// 专注阅读：内容居中、宽度封顶 56rem（头部左右各 1.5rem 内边距，所以标题框宽 56rem − 3rem）；切换会重挂详情，查找条回到关闭。
func TestFocusReadingCapsTheWidthAndRemountsTheDetail(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.keys("ctrl+f")
	if !r.has(gfx.RoleTextField, locales.LibraryPanelFindPlaceholder()) {
		t.Fatal("夹具失效：Ctrl+F 应打开查找条")
	}
	r.keys("alt+z")
	if r.has(gfx.RoleTextField, locales.LibraryPanelFindPlaceholder()) {
		t.Fatal("切换专注阅读会重挂详情：查找条应回到关闭")
	}
	got := r.mustNode(gfx.RoleTextField, locales.LibraryTitlePlaceholder()).Bounds.Width
	want := r.look.Rem(detailMaxWidthRem) - 2*r.look.Space(6)
	if got < want-1 || got > want+1 {
		t.Fatalf("专注阅读时标题框宽应是 %.1f（56rem 减两侧内边距），实际 %.1f", want, got)
	}
}

// ---- 头部 ----

// 回收站条目的标题只读：敲的字被丢掉、文字退回原值，也不会排队保存。
func TestTrashedTitleIsReadOnly(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pickTrashed(shellTrashNoteID)
	orig := r.titleBox()
	r.editTitle("!")
	if got := r.titleBox(); got != orig {
		t.Fatalf("回收站条目的标题应退回原值 %q，实际 %q", orig, got)
	}
	r.clock.Advance(10 * time.Second)
	r.frames(3)
	if n := len(r.svc.Drafts()); n != 0 {
		t.Fatalf("回收站条目的标题不该保存，实际 %d 次", n)
	}
}

// 新建的空白条目：光标直接落进标题，请求随之消费掉。
func TestNewItemTitleTakesTheFocus(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.setDetail(func(d *DetailView) { d.TitleFocusID = shellBlankID })
	r.pick(shellBlankID)
	if !r.focusedIs(gfx.RoleTextField, locales.LibraryTitlePlaceholder()) {
		t.Fatal("要求聚焦标题的条目，标题框应拿到键盘焦点")
	}
	if got := r.view().Detail.TitleFocusID; got != "" {
		t.Fatalf("聚焦请求应被消费掉，实际 %q", got)
	}
	plain := shellRig(t)
	plain.pick(detailNoteID)
	if plain.focusedIs(gfx.RoleTextField, locales.LibraryTitlePlaceholder()) {
		t.Fatal("没有聚焦请求的条目，标题框不该自己拿焦点")
	}
}

// 排队中的改动：自动保存开着时不算脏（不亮「未保存」也没有保存钮），关着时亮出来并给保存钮。
func TestUnsavedChipFollowsTheAutoSaveSetting(t *testing.T) {
	t.Parallel()
	on := detailRig(t)
	on.openCard(detailNoteID)
	on.editTitle("!")
	if on.hasText(locales.LibraryUnsaved()) || on.has(gfx.RoleButton, locales.LibrarySave()) {
		t.Fatal("自动保存开着时排队中的改动不该亮「未保存」与保存钮")
	}
	off := detailRig(t, func(r *rig) { r.store.AutoSave = func() bool { return false } })
	off.openCard(detailNoteID)
	off.editTitle("!")
	if !off.hasText(locales.LibraryUnsaved()) || !off.has(gfx.RoleButton, locales.LibrarySave()) {
		t.Fatalf("自动保存关着时应亮「未保存」与保存钮，实际画面文字 %q", off.screenText())
	}
}

// 「已保存」只在保存刚结束、没有别的状态要提示时亮：保存中、保存失败、有未落盘的编辑时都不画。
func TestSavedChipOnlyShowsWhenNothingElseNeedsSaying(t *testing.T) {
	t.Parallel()
	r := detailRig(t, func(r *rig) { r.store.AutoSave = func() bool { return false } })
	r.openCard(detailNoteID)
	r.setDetail(func(d *DetailView) { d.JustSaved = true })
	if !r.hasText(locales.LibrarySaved()) {
		t.Fatal("保存刚结束应亮「已保存」")
	}
	for name, edit := range map[string]func(d *DetailView){
		"保存中":     func(d *DetailView) { d.Saving = true },
		"保存失败":    func(d *DetailView) { d.SaveError = "磁盘已满" },
		"有未落盘的编辑": func(d *DetailView) { d.Unsaved = true },
	} {
		r.setDetail(func(d *DetailView) { d.JustSaved, d.Saving, d.SaveError, d.Unsaved = true, false, "", false })
		r.setDetail(edit)
		if r.hasText(locales.LibrarySaved()) {
			t.Fatalf("%s时不该亮「已保存」", name)
		}
	}
}

// 字数是去掉首尾空白后的 UTF-16 码元数（渲染层 content.trim().length）：BOM 算空白、U+0085 不算，辅助平面字符算两个。
func TestWordCountIsTheTrimmedUTF16Length(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   string
		want int
	}{
		{"", 0}, {" \n\t ", 0}, {"abc", 3}, {"  abc \n", 3}, {"\uFEFFabc\uFEFF", 3}, {"a\u0085", 2}, {"中文", 2}, {"😀", 2}, {"a😀b", 4},
	} {
		if got := wordCount(c.in); got != c.want {
			t.Errorf("wordCount(%q) = %d，应为 %d", c.in, got, c.want)
		}
	}
}

// 头部的字数只在有内容时显示；空白笔记没有字数。
func TestWordCountChipNeedsContent(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(shellBlankID)
	if r.hasText(locales.LibraryWordCount(0)) {
		t.Fatal("空白笔记不该显示字数")
	}
	r.pick(detailNoteID)
	if want := locales.LibraryWordCount(wordCount("# 并发模式\n\n正文内容。")); !r.hasText(want) {
		t.Fatalf("有内容的笔记应显示字数 %q，实际画面文字 %q", want, r.screenText())
	}
}

// 合集 chip：回收站条目的不可点；未归合集的项把条目移出合集，选一个合集把它移进去。
func TestCollectionChipMovesTheItemAndIsDisabledInTheTrash(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pickTrashed(shellTrashNoteID)
	if !r.mustNode(gfx.RoleCombobox, locales.LibraryNoCollection()).Disabled {
		t.Fatal("回收站条目的合集 chip 应禁用")
	}

	r2 := shellRig(t)
	r2.pick(detailNoteID)
	if r2.mustNode(gfx.RoleCombobox, locales.LibraryNoCollection()).Disabled {
		t.Fatal("未删除条目的合集 chip 应可点")
	}
	r2.click(gfx.RoleCombobox, locales.LibraryNoCollection())
	r2.click(gfx.RoleMenuItem, "阅读")
	if got, _ := r2.svc.ItemNow(detailNoteID); got.CollectionID != "c2" {
		t.Fatalf("选「阅读」应把条目移进合集 c2，实际 %q", got.CollectionID)
	}

	r3 := shellRig(t)
	r3.pick(shellCollectedID)
	r3.click(gfx.RoleCombobox, "工作")
	r3.click(gfx.RoleMenuItem, locales.LibraryNoCollection())
	if got, _ := r3.svc.ItemNow(shellCollectedID); got.CollectionID != "" {
		t.Fatalf("选「未归合集」应把条目移出合集，实际 %q", got.CollectionID)
	}
}

// 来源只认 http(s) 链接；认不出的协议、没有主机的、相对地址都不成链接；公众号来源单独识别。
func TestSourceViewOnlyTrustsHTTPLinks(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "   ", "javascript:alert(1)", "ftp://example.com/a", "ssh://example.com", "file:///C:/x", "//example.com/a", "example.com/a", "data:text/html,x"} {
		if u := resolveSafeSourceURL(bad); u != nil {
			t.Errorf("resolveSafeSourceURL(%q) 应为空，实际 %v", bad, u)
		}
	}
	for _, good := range []string{"https://example.com/a", "http://example.com", "  https://example.com/a  "} {
		if u := resolveSafeSourceURL(good); u == nil || u.Hostname() != "example.com" {
			t.Errorf("resolveSafeSourceURL(%q) 应认出主机 example.com，实际 %v", good, u)
		}
	}
	web := sourceViewOf(services.LibraryItem{ItemType: services.LibraryTypeNote, SourceURI: "https://example.com/a"})
	if web.label != "example.com" || web.href != "https://example.com/a" || web.wechat {
		t.Errorf("普通网页来源：%+v", web)
	}
	wechat := sourceViewOf(services.LibraryItem{ItemType: services.LibraryTypeNote, SourceURI: "https://mp.weixin.qq.com/s/abc"})
	if !wechat.wechat || wechat.label != locales.LibraryPlatformWechat() {
		t.Errorf("公众号来源应被认出：%+v", wechat)
	}
	if bad := sourceViewOf(services.LibraryItem{ItemType: services.LibraryTypeNote, SourceURI: "javascript:alert(1)"}); bad.label != "" || bad.href != "" {
		t.Errorf("不是 http(s) 的来源不该有来源 chip：%+v", bad)
	}
}

// 来源 chip 旁的刷新钮：只有未删除的条目有；公众号来源走补采，其余把来源链接排进采集队列且强制建副本。
func TestSourceRefreshButtonAndWhereItGoes(t *testing.T) {
	t.Parallel()
	trashed := shellRig(t)
	trashed.pickTrashed(shellTrashNoteID)
	if !trashed.has(gfx.RoleLink, "example.com") || trashed.has(gfx.RoleButton, locales.LibraryRefreshSource()) {
		t.Fatal("回收站条目有来源链接、没有刷新钮")
	}

	imports := &servicestest.Imports{}
	web := shellRig(t, func(r *rig) { r.screen.Imports = imports })
	web.pick(detailNoteID)
	web.click(gfx.RoleButton, locales.LibraryRefreshSource())
	queued := imports.Enqueued()
	if len(queued) != 1 {
		t.Fatalf("刷新应向采集队列入一个任务，实际 %+v", queued)
	}
	in := queued[0]
	if in.Kind != "url" || in.Input != "https://example.com/go" || in.RefreshOfItemID != detailNoteID || !in.ForceDuplicate || in.CollectionID != nil ||
		!strings.Contains(strings.Join(in.TagNames, ","), locales.LibrarySourceRefreshTag()) {
		t.Fatalf("刷新任务的内容不对：%+v", in)
	}
	for _, call := range web.svc.Calls() {
		if call == "SupplementWechat" {
			t.Fatal("普通网页来源不该走公众号补采")
		}
	}

	wechat := shellRig(t, func(r *rig) { r.screen.Imports = imports })
	wechat.pick(detailReviewID)
	wechat.click(gfx.RoleButton, locales.LibraryRefreshSource())
	var supplemented bool
	for _, call := range wechat.svc.Calls() {
		supplemented = supplemented || call == "SupplementWechat"
	}
	if !supplemented || len(imports.Enqueued()) != 1 {
		t.Fatalf("公众号来源应走补采而不是采集队列：调用 %v，队列 %d 个", wechat.svc.Calls(), len(imports.Enqueued()))
	}
}

// 提问入口：空白笔记没有可提问的正文，不摆。
func TestAskButtonNeedsContent(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(shellBlankID)
	if r.has(gfx.RoleButton, locales.ArticleAskTitle()) {
		t.Fatal("空白笔记不该有提问钮")
	}
	r.pick(detailNoteID)
	if !r.has(gfx.RoleButton, locales.ArticleAskTitle()) {
		t.Fatal("有内容的笔记应有提问钮")
	}
}

// 回收站条目的头部只有「恢复」；点它把条目恢复。未删除条目有专注、收藏、更多，没有恢复。
func TestTrashedHeaderOffersOnlyRestore(t *testing.T) {
	t.Parallel()
	live := shellRig(t)
	live.pick(detailNoteID)
	for _, name := range []string{locales.LibraryEnterFocusReading(), locales.LibraryFavorite()} {
		if !live.hasInDetail(gfx.RoleButton, name) {
			t.Fatalf("未删除条目应有「%s」", name)
		}
	}
	if !live.hasInDetail(gfx.RoleCombobox, locales.LibraryMoreActions()) {
		t.Fatalf("未删除条目应有「%s」", locales.LibraryMoreActions())
	}
	if live.hasInDetail(gfx.RoleButton, locales.LibraryRestore()) {
		t.Fatal("未删除条目不该有「恢复」")
	}

	r := shellRig(t)
	r.pickTrashed(shellTrashNoteID)
	for _, name := range []string{locales.LibraryEnterFocusReading(), locales.LibraryFavorite(), locales.ArticleAskTitle()} {
		if r.hasInDetail(gfx.RoleButton, name) {
			t.Fatalf("回收站条目不该有「%s」", name)
		}
	}
	if r.hasInDetail(gfx.RoleCombobox, locales.LibraryMoreActions()) {
		t.Fatalf("回收站条目不该有「%s」", locales.LibraryMoreActions())
	}
	r.clickInDetail(gfx.RoleButton, locales.LibraryRestore())
	if got, _ := r.svc.ItemNow(shellTrashNoteID); got.DeletedAt != 0 {
		t.Fatalf("点「恢复」应把条目移出回收站，实际 DeletedAt=%d", got.DeletedAt)
	}
}

// 关闭钮只在详情浮层里有：卡片视图的详情是常驻的，不摆；浮层里点它关浮层。
func TestCloseDetailButtonOnlyInTheModal(t *testing.T) {
	t.Parallel()
	card := detailRig(t)
	card.openCard(detailNoteID)
	if card.has(gfx.RoleButton, locales.LibraryCloseDetail()) {
		t.Fatal("卡片视图里的详情是常驻的，不该有关闭钮")
	}
	r := detailRig(t)
	r.openModal()
	r.click(gfx.RoleButton, locales.LibraryCloseDetail())
	if r.screen.DetailModalOpen() {
		t.Fatal("点关闭钮应关掉浮层")
	}
}

// ---- 通知条 ----

// 别的条目的保存冲突、来源版本不该画在这一条上（后台保存晚于切换条目回来时会撞上）。
func TestNoticesBelongToTheirOwnItem(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.setDetail(func(d *DetailView) {
		d.Conflict = &DraftConflict{ItemID: detailForumID, Server: services.LibraryItem{}, Fields: []string{services.LibraryDraftFieldTitle}}
		d.Revisions = RevisionsView{ItemID: detailForumID, Rows: []services.LibrarySourceVersion{{ID: "sv", Title: "别的条目的版本", ReviewJSON: "[]"}}}
	})
	if r.hasText(locales.LibraryDraftConflict()) || r.hasText(locales.LibrarySourceRevisions(1)) {
		t.Fatalf("别的条目的冲突与来源版本不该显示在这一条上，实际画面文字 %q", r.screenText())
	}
	r.setDetail(func(d *DetailView) { d.Conflict.ItemID = detailNoteID })
	if !r.hasText(locales.LibraryDraftConflict()) {
		t.Fatal("夹具失效：冲突属于这一条时应显示")
	}
}

// 冲突提示：「比较」折叠块点一下展开、再点一下收起；「保留我的修改」把本地改动写回去、不是采用已保存版本。
func TestConflictNoticeComparesAndKeepsMine(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	orig := detailEntryOf(detailNoteID).Title
	r.svc.EditExternally(detailNoteID, func(item *services.LibraryItem) { item.Title = "外部改过的标题" })
	r.editTitle("!")
	r.keys("ctrl+s")
	if !r.hasText(locales.LibraryDraftConflict()) {
		t.Fatalf("夹具失效：应显示冲突提示，实际画面文字 %q", r.screenText())
	}
	compare := locales.LibraryDraftConflictCompare()
	if r.hasTextWithPrefix(locales.LibraryDraftConflictMine()) || r.mustNode(gfx.RoleButton, compare).Expanded {
		t.Fatal("比较区一开始是收起的")
	}
	r.click(gfx.RoleButton, compare)
	if !r.hasTextWithPrefix(locales.LibraryDraftConflictMine()) || !r.hasTextWithPrefix(locales.LibraryDraftConflictSaved()) || !r.mustNode(gfx.RoleButton, compare).Expanded {
		t.Fatalf("点一下应展开两份内容并报告已展开，实际画面文字 %q", r.screenText())
	}
	r.click(gfx.RoleButton, compare)
	if r.hasTextWithPrefix(locales.LibraryDraftConflictMine()) || r.mustNode(gfx.RoleButton, compare).Expanded {
		t.Fatal("再点一下应收起")
	}
	r.click(gfx.RoleButton, locales.LibraryDraftConflictKeepMine())
	if r.hasText(locales.LibraryDraftConflict()) {
		t.Fatal("选择之后冲突提示应消失")
	}
	if got, _ := r.svc.ItemNow(detailNoteID); got.Title != orig+"!" {
		t.Fatalf("「保留我的修改」应把我的标题写回去，实际 %q", got.Title)
	}
}

// 来源版本读取失败：块里显示原因与重试钮（没有版本也要显示），重试重新读取。
func TestRevisionsErrorShowsAReasonAndRetries(t *testing.T) {
	t.Parallel()
	r := detailRig(t, func(r *rig) { r.svc.FailOn("SourceVersions", errors.New("来源版本读取失败：磁盘繁忙")) })
	r.openCard(detailNoteID)
	head := locales.LibrarySourceRevisions(0)
	if !r.hasText(head) {
		t.Fatalf("只有读取错误时也该显示来源版本块，实际画面文字 %q", r.screenText())
	}
	r.click(gfx.RoleButton, head)
	if !strings.Contains(r.screenText(), "来源版本读取失败：磁盘繁忙") || !r.has(gfx.RoleButton, locales.CommonRetry()) {
		t.Fatalf("展开后应显示失败原因与重试钮，实际画面文字 %q", r.screenText())
	}
	r.svc.FailOn("SourceVersions", nil)
	r.svc.SeedSourceVersions(detailNoteID, []services.LibrarySourceVersion{{ID: "sv1", Title: "新版本", Content: "新版本的正文", ReviewJSON: "[]", CapturedAt: 1_800_000_000_000}})
	r.click(gfx.RoleButton, locales.CommonRetry())
	if !r.hasText(locales.LibrarySourceRevisions(1)) {
		t.Fatalf("重试应重新读取来源版本，实际画面文字 %q", r.screenText())
	}
}

// 来源版本行：点一下展开、再点一下收起；缺口说明与文字稿只在版本带着它们时才画。
func TestRevisionRowsToggleAndShowOnlyWhatTheVersionHas(t *testing.T) {
	t.Parallel()
	r := detailRig(t, func(r *rig) {
		r.svc.SeedSourceVersions(detailNoteID, []services.LibrarySourceVersion{
			{ID: "sv1", Title: "简单版本", Content: "简单版本的正文", ReviewJSON: "[]", CapturedAt: 1_800_000_000_000},
			{ID: "sv2", Title: "完整版本", Content: "完整版本的正文", Transcript: "完整版本的文字稿", ReviewJSON: `["图片未能下载"]`, CapturedAt: 1_800_000_100_000},
		})
	})
	r.openCard(detailNoteID)
	r.click(gfx.RoleButton, locales.LibrarySourceRevisions(2))
	missing := locales.LibrarySourceRevisionMissing("")

	head, ok := r.nodeWithPrefix(gfx.RoleButton, "简单版本 · ")
	if !ok || head.Expanded || r.has(gfx.RoleButton, locales.LibrarySourceRevisionAdopt()) {
		t.Fatalf("版本行一开始是收起的：%+v", head)
	}
	r.clickPrefix(gfx.RoleButton, "简单版本 · ")
	if head, _ = r.nodeWithPrefix(gfx.RoleButton, "简单版本 · "); !head.Expanded || !r.has(gfx.RoleButton, locales.LibrarySourceRevisionAdopt()) {
		t.Fatal("点一下应展开并报告已展开")
	}
	text := r.screenText()
	if r.hasTextWithPrefix(missing) || strings.Contains(text, locales.LibrarySourceRevisionTranscript()) {
		t.Fatalf("没有缺口也没有文字稿的版本不该画缺口行与文字稿标题，实际画面文字 %q", text)
	}
	r.clickPrefix(gfx.RoleButton, "简单版本 · ")
	if r.has(gfx.RoleButton, locales.LibrarySourceRevisionAdopt()) {
		t.Fatal("再点一下应收起")
	}

	r.clickPrefix(gfx.RoleButton, "完整版本 · ")
	text = r.screenText()
	if !r.hasTextWithPrefix(missing) || !strings.Contains(text, "图片未能下载") ||
		!strings.Contains(text, locales.LibrarySourceRevisionTranscript()) || !strings.Contains(text, "完整版本的文字稿") {
		t.Fatalf("带缺口与文字稿的版本应画出它们，实际画面文字 %q", text)
	}
}

// 复核提醒：折叠块报告展开状态；回收站里的条目只读（给原因、不给「标记已复核」）；标记中按钮禁用；换条目折叠状态重置。
func TestReviewNoticeFoldsAndIsReadOnlyInTheTrash(t *testing.T) {
	t.Parallel()
	prefix := locales.ArticleReaderImportIncomplete()
	r := shellRig(t)
	r.pick(detailReviewID)
	head, ok := r.nodeWithPrefix(gfx.RoleButton, prefix)
	if !ok || head.Expanded || r.has(gfx.RoleButton, locales.LibraryMarkReviewed()) {
		t.Fatalf("复核提醒一开始是收起的：%+v", head)
	}
	r.clickPrefix(gfx.RoleButton, prefix)
	if head, _ = r.nodeWithPrefix(gfx.RoleButton, prefix); !head.Expanded || !r.has(gfx.RoleButton, locales.LibraryMarkReviewed()) {
		t.Fatal("点一下应展开并报告已展开")
	}
	if r.mustNode(gfx.RoleButton, locales.LibraryMarkReviewed()).Disabled {
		t.Fatal("没有在标记时「标记已复核」应可点")
	}
	r.setDetail(func(d *DetailView) { d.MarkingReviewedID = detailReviewID })
	if !r.mustNode(gfx.RoleButton, locales.LibraryMarkReviewed()).Disabled {
		t.Fatal("正在标记时「标记已复核」应禁用")
	}
	r.pick(detailNoteID)
	r.pick(detailReviewID)
	if head, _ = r.nodeWithPrefix(gfx.RoleButton, prefix); head.Expanded {
		t.Fatal("换了条目再回来，折叠块应回到收起")
	}

	trashed := shellRig(t)
	trashed.pickTrashed(shellTrashReviewID)
	trashed.clickPrefix(gfx.RoleButton, prefix)
	if !trashed.hasText("正文被截断") || trashed.has(gfx.RoleButton, locales.LibraryMarkReviewed()) {
		t.Fatalf("回收站条目应给复核原因、不给「标记已复核」，实际画面文字 %q", trashed.screenText())
	}
}

// ---- 内容面板与工具栏 ----

// 打开条目时的默认页签：论坛有讨论总结就先看总结，其余看正文。
func TestDefaultTabFollowsTheItemType(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailForumID)
	if got := r.screen.detail.panel.tab; got != TabSummary {
		t.Fatalf("有讨论总结的论坛应先看总结，实际 %q", got)
	}
	r.openCard(detailNoteID)
	if got := r.screen.detail.panel.tab; got != TabBody {
		t.Fatalf("笔记应先看正文，实际 %q", got)
	}
}

// 默认预览需要有内容：空白笔记不进预览（它直接是编辑态），有内容的进。
func TestPreviewByDefaultNeedsContent(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(shellBlankID)
	if r.screen.detail.panel.isPreview {
		t.Fatal("空白笔记不该默认进预览")
	}
	r.pick(detailNoteID)
	if !r.screen.detail.panel.isPreview {
		t.Fatal("有内容的笔记应默认进预览")
	}
}

// 阅读记忆只在页签变了时写：停在同一个页签上逐帧重画不再写偏好。
func TestReadingMemoryIsWrittenOnlyWhenTheTabChanges(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailForumID)
	before := len(r.prefs.SetKeys())
	if before == 0 {
		t.Fatal("夹具失效：打开条目应写一次阅读记忆")
	}
	r.frames(10)
	if after := len(r.prefs.SetKeys()); after != before {
		t.Fatalf("页签没变不该再写阅读记忆：%d → %d 次", before, after)
	}
	r.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if after := len(r.prefs.SetKeys()); after <= before {
		t.Fatalf("换了页签应写阅读记忆：%d → %d 次", before, after)
	}
}

// 楼层目录默认展开的线：讨论不少于 20 条才展开。
func TestCatalogOpensByDefaultFromTwentyReplies(t *testing.T) {
	t.Parallel()
	for n, want := range map[int]bool{0: false, 1: false, 19: false, 20: true, 21: true, 100: true} {
		if got := defaultCatalogOpen(n); got != want {
			t.Errorf("defaultCatalogOpen(%d) = %v，应为 %v", n, got, want)
		}
	}
}

// 查找条的宽：不超过 18rem，面板窄时随面板缩（面板宽减 2rem），面板宽还没量到（首帧）取 18rem。
func TestFindBarWidthShrinksWithThePanel(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	rem := r.look.Rem
	for _, c := range []struct {
		name   string
		panelW float32
		want   float32
	}{
		{"首帧还没量到面板", 0, rem(18)},
		{"面板很宽", rem(60), rem(18)},
		{"面板刚好不挤", rem(20), rem(18)},
		{"面板窄", rem(15), rem(13)},
	} {
		if got := findBarWidth(r.look, c.panelW); got != c.want {
			t.Errorf("%s：宽应为 %.1f，实际 %.1f", c.name, c.want, got)
		}
	}
}

// 工具栏的查找入口：点它开查找条、查找条的关闭钮关它；文字稿为空的页签与回收站条目的讨论页签没有入口，有内容的有。
func TestFindEntryOpensAndClosesTheBar(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	placeholder := locales.LibraryPanelFindPlaceholder()
	r.click(gfx.RoleButton, locales.LibraryPanelFindOpen())
	if !r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("点工具栏的查找钮应打开查找条")
	}
	r.click(gfx.RoleButton, locales.CommonClose())
	if r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("点查找条的关闭钮应关掉查找条")
	}

	bare := shellRig(t)
	bare.pick(shellBareVideoID)
	bare.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	if bare.has(gfx.RoleButton, locales.LibraryPanelFindOpen()) {
		t.Fatal("文字稿为空时没有查找入口")
	}
	bare.pick(detailVideoID)
	bare.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
	if !bare.has(gfx.RoleButton, locales.LibraryPanelFindOpen()) {
		t.Fatal("有文字稿时应有查找入口")
	}

	trashed := shellRig(t)
	trashed.pickTrashed(shellTrashForumID)
	trashed.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if trashed.has(gfx.RoleButton, locales.LibraryPanelFindOpen()) {
		t.Fatal("回收站条目的讨论页签没有查找入口")
	}
	live := shellRig(t)
	live.pick(detailForumID)
	live.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if !live.has(gfx.RoleButton, locales.LibraryPanelFindOpen()) {
		t.Fatal("未删除论坛的讨论页签应有查找入口")
	}
}

// 查找入口没了就关掉查找条：切到「显示原文」之后入口消失，再切回预览时查找条不该自己回来。
func TestFindBarClosesWhenTheEntryGoesAway(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailForumID)
	r.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	placeholder := locales.LibraryForumRepliesFilterPlaceholder()
	r.keys("ctrl+f")
	if !r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("夹具失效：Ctrl+F 应在讨论页签打开筛选条")
	}
	r.click(gfx.RoleButton, locales.LibraryShowSource())
	if r.has(gfx.RoleButton, locales.LibraryPanelFindOpen()) || r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("显示原文时没有查找入口，查找条也不该还开着")
	}
	r.click(gfx.RoleButton, locales.LibraryRenderMarkdown())
	if r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("入口回来时查找条不该自己重新打开")
	}
}

// 查找框里的空白不算查找词：只有空白时不显示「无匹配」，写了字才显示。
func TestFindBarIgnoresWhitespaceOnlyQueries(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.keys("ctrl+f")
	r.typeText("   ")
	if r.hasText(locales.LibraryPanelFindEmptyShort()) {
		t.Fatal("只有空白的查找词不该显示「无匹配」")
	}
	r.typeText("x")
	if !r.hasText(locales.LibraryPanelFindEmptyShort()) {
		t.Fatal("写了查找词、没有命中时应显示「无匹配」")
	}
}

// 工具栏的几个工具各自出现的条件：总结（回收站没有）、刷新讨论（只在讨论页签、回收站没有）、
// 显示原文 / Markdown 渲染（只在主题阅读不可用的页签，回收站没有）。
func TestToolbarToolsAppearOnlyWhereTheyApply(t *testing.T) {
	t.Parallel()
	summary := locales.LibraryMediaSummaryGenerateVideo()
	live := shellRig(t)
	live.pick(detailVideoID)
	if !live.has(gfx.RoleButton, summary) {
		t.Fatal("有文字稿的视频应有总结钮")
	}
	trashed := shellRig(t)
	trashed.pickTrashed(shellTrashVideoID)
	if trashed.has(gfx.RoleButton, summary) {
		t.Fatal("回收站里的视频不该有总结钮")
	}

	refresh, source, render := locales.LibraryForumDiscussionRefresh(), locales.LibraryShowSource(), locales.LibraryRenderMarkdown()
	forum := shellRig(t)
	forum.pick(detailForumID)
	if forum.has(gfx.RoleButton, refresh) || forum.has(gfx.RoleButton, source) {
		t.Fatal("论坛的讨论总结页签不该有刷新讨论与显示原文")
	}
	forum.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if !forum.has(gfx.RoleButton, refresh) || !forum.has(gfx.RoleButton, source) {
		t.Fatal("讨论页签应有刷新讨论与显示原文")
	}
	forum.click(gfx.RoleButton, source)
	if !forum.has(gfx.RoleButton, render) || forum.has(gfx.RoleButton, source) {
		t.Fatal("显示原文之后按钮应换成 Markdown 渲染")
	}
	gone := shellRig(t)
	gone.pickTrashed(shellTrashForumID)
	gone.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
	if gone.has(gfx.RoleButton, refresh) || gone.has(gfx.RoleButton, source) || gone.has(gfx.RoleButton, render) {
		t.Fatal("回收站条目的讨论页签不该有刷新讨论与预览切换")
	}

	note := detailRig(t)
	note.openCard(detailNoteID)
	if note.has(gfx.RoleButton, source) || note.has(gfx.RoleButton, render) {
		t.Fatal("正文页签有主题阅读，不该摆「显示原文 / Markdown 渲染」")
	}
}

// 公众号文章在阅读与网页快照之间切换：点一下进快照、按钮换成「返回阅读」，再点一下回阅读。
func TestWechatSnapshotToggleGoesBackAndForth(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailReviewID)
	view, back := locales.ArticleAskViewSnapshot(), locales.LibraryBackToReading()
	r.click(gfx.RoleButton, view)
	if !r.has(gfx.RoleButton, back) || r.has(gfx.RoleButton, view) {
		t.Fatal("点「查看网页快照」之后按钮应换成「返回阅读」")
	}
	r.click(gfx.RoleButton, back)
	if !r.has(gfx.RoleButton, view) || r.has(gfx.RoleButton, back) {
		t.Fatal("再点一下应回到阅读")
	}
}

// 还没迁入的入口点一下都弹失败提示，详情里写明归属的里程碑与台账编号（台账 B-060）：一个入口一条。
func TestEveryUnmigratedEntryNamesItsMilestone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		reach     func(r *rig) // 走到入口所在的状态
		role      gfx.Role
		label     string
		milestone string
	}{
		{"总结", func(r *rig) { r.openCard(detailVideoID) }, gfx.RoleButton, locales.LibraryMediaSummaryGenerateVideo(), "M2-6"},
		{"刷新讨论", func(r *rig) {
			r.openCard(detailForumID)
			r.click(gfx.RoleButton, locales.LibraryForumRepliesSection())
		}, gfx.RoleButton, locales.LibraryForumDiscussionRefresh(), "M2-6"},
		{"文章工具", func(r *rig) {
			r.openCard(detailNoteID)
			r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
		}, gfx.RoleMenuItem, locales.ArticleReaderTools(), "M2-6"},
		{"采集评论", func(r *rig) {
			r.pick(shellDouyinID)
			r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
		}, gfx.RoleMenuItem, locales.LibraryCollectSourceComments(), "M2-6"},
		{"AI 排版文字稿", func(r *rig) {
			r.openCard(detailVideoID)
			r.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
		}, gfx.RoleButton, locales.LibraryTranscriptFormat(), "M2-6"},
		{"重新转写", func(r *rig) {
			r.openCard(detailVideoID)
			r.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
		}, gfx.RoleButton, locales.LibraryTranscribeRegenerate(), "M2-6"},
		{"区分说话人转写", func(r *rig) {
			r.openCard(detailVideoID)
			r.clickPrefix(gfx.RoleButton, locales.LibraryTranscript())
			r.setDetail(func(d *DetailView) { d.CanDiarize = true })
		}, gfx.RoleButton, locales.LibraryTranscribeDiarize(), "M2-6"},
		{"提问", func(r *rig) { r.openCard(detailNoteID) }, gfx.RoleButton, locales.ArticleAskTitle(), "M3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := shellRig(t)
			c.reach(r)
			r.click(c.role, c.label)
			text := r.toastText()
			if !strings.Contains(text, locales.CommonOperationFailed(c.label)) || !strings.Contains(text, c.milestone) || !strings.Contains(text, "B-060") {
				t.Fatalf("应弹失败提示并写明里程碑 %s 与 B-060，实际画面文字 %q", c.milestone, text)
			}
		})
	}
}

// ---- 「更多」菜单 ----

// 菜单靠按钮右缘对齐（渲染层 rect.right − 160）；置顶与归档的文字随当前状态；采集评论只给抖音与小红书。
func TestMoreMenuAlignsAndFollowsTheItemState(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(detailNoteID)
	more, _ := r.detailNode(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	first := r.mustNode(gfx.RoleMenuItem, locales.ArticleReaderTools())
	// 第一项比菜单左缘内缩 1 像素（量得：偏差 1.0）；容差只给到 3，宽了靠左对齐的菜单被夹回窗口后也混得过去
	// （按钮离窗口右缘只有 24 像素，靠左对齐的菜单夹回后的左缘比靠右对齐的只差 25 像素——变异 U01 因此存活过一轮）。
	want := more.Bounds.X + more.Bounds.Width - moreMenuWidth
	if first.Bounds.X < want-1 || first.Bounds.X > want+3 {
		t.Fatalf("菜单应靠按钮右缘对齐（左缘约 %.1f），第一项的左缘在 %.1f", want, first.Bounds.X)
	}
	if !r.has(gfx.RoleMenuItem, locales.LibraryUnpin()) || r.has(gfx.RoleMenuItem, locales.LibraryPin()) {
		t.Fatal("置顶的条目菜单里应是「取消置顶」")
	}
	if !r.has(gfx.RoleMenuItem, locales.LibraryArchive()) || r.has(gfx.RoleMenuItem, locales.LibraryUnarchive()) {
		t.Fatal("没归档的条目菜单里应是「归档」")
	}
	if r.has(gfx.RoleMenuItem, locales.LibraryCollectSourceComments()) {
		t.Fatal("普通网页来源不该有「采集评论」")
	}
	r.click(gfx.RoleMenuItem, locales.LibraryUnpin())
	if got, _ := r.svc.ItemNow(detailNoteID); got.IsPinned {
		t.Fatal("点「取消置顶」应取消置顶")
	}

	f := shellRig(t)
	f.pick(detailForumID)
	f.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	if !f.has(gfx.RoleMenuItem, locales.LibraryPin()) || f.has(gfx.RoleMenuItem, locales.LibraryUnpin()) {
		t.Fatal("没置顶的条目菜单里应是「置顶」")
	}
	f.click(gfx.RoleMenuItem, locales.LibraryArchive())
	if got, _ := f.svc.ItemNow(detailForumID); got.Status != services.LibraryStatusArchived {
		t.Fatalf("点「归档」应归档，实际状态 %q", got.Status)
	}

	a := shellRig(t)
	a.pick(shellArchivedID)
	a.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	if !a.has(gfx.RoleMenuItem, locales.LibraryUnarchive()) || a.has(gfx.RoleMenuItem, locales.LibraryArchive()) {
		t.Fatal("已归档的条目菜单里应是「取消归档」")
	}
	a.click(gfx.RoleMenuItem, locales.LibraryUnarchive())
	if got, _ := a.svc.ItemNow(shellArchivedID); got.Status != services.LibraryStatusActive {
		t.Fatalf("点「取消归档」应恢复为活跃，实际状态 %q", got.Status)
	}

	d := shellRig(t)
	d.pick(shellDouyinID)
	d.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	if !d.has(gfx.RoleMenuItem, locales.LibraryCollectSourceComments()) {
		t.Fatal("抖音来源应有「采集评论」")
	}
}
