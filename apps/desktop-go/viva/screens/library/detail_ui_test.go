package library

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测条目详情的界面接线（detail*.go，台账 B-043 / B-045 / B-052 / B-055 / B-057 的详情部分）：
// 谁在什么条件下显示、按键与点击触发了什么。Store 的保存、冲突、复核等规则在 store_detail*_test.go 里逐条测过，这里不重复，
// 只测界面把它们接对了没有。数据是四条不同类型的条目（笔记、论坛、视频、待复核网页）。

const (
	detailNoteID   = "n1"
	detailForumID  = "f1"
	detailVideoID  = "v1"
	detailReviewID = "r1"
)

func detailEntries() []services.LibraryEntry {
	const now = int64(1_800_000_000_000)
	entry := func(id, title, kind string) services.LibraryEntry {
		return services.LibraryEntry{
			ID: id, Title: title, ItemType: kind, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, CreatedAt: now - 86_400_000, UpdatedAt: now - 3_600_000, Tags: []services.LibraryTag{},
		}
	}
	note := entry(detailNoteID, "学习笔记：Go 并发模式", services.LibraryTypeNote)
	note.IsPinned = true
	forum := entry(detailForumID, "技术论坛：SQLite WAL 模式的备份坑", "forum")
	video := entry(detailVideoID, "Transformer 架构详解", "video")
	review := entry(detailReviewID, "待复核的网页", "webpage")
	review.ReviewStatus = "needs_review"
	review.ReviewReasons = []string{"正文被截断", "图片未能下载"}
	return []services.LibraryEntry{note, forum, video, review}
}

const detailForumContent = "## 讨论总结\n\n大家认为应使用 VACUUM INTO。\n\n## 正文\n\n如何备份 WAL 模式的库？\n\n## 讨论\n\n**1 楼 · 张三**\n\n用 VACUUM INTO。\n\n**2 楼 · 李四**\n\n先 checkpoint。\n"

// detailRig 是侧栏加工作区（卡片列表）加上上面四条条目的无头夹具；还没有选中任何一条。
func detailRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	return newRig(t, append([]func(*rig){func(r *rig) {
		r.workspace = true
		r.svc.SeedEntries(detailEntries())
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "# 并发模式\n\n正文内容。", SourceURI: "https://example.com/go"})
		r.svc.SeedDetail(detailForumID, servicestest.DetailBody{Content: detailForumContent, SourceURI: "https://linux.do/t/123"})
		r.svc.SeedDetail(detailVideoID, servicestest.DetailBody{Content: "视频", Transcript: "这是一段文字稿内容。", SourceURI: "https://www.bilibili.com/video/BV1xx"})
		r.svc.SeedDetail(detailReviewID, servicestest.DetailBody{Content: "网页正文。", SourceURI: "https://mp.weixin.qq.com/s/abc"})
	}}, mutate...)...)
}

// entryOf 取夹具里一条条目的列表投影。
func detailEntryOf(id string) services.LibraryEntry {
	for _, e := range detailEntries() {
		if e.ID == id {
			return e
		}
	}
	panic("没有这条条目：" + id)
}

// openCard 点卡片选中一条，等详情读到（后台动作同步执行，所以点完就读到了）。
func (r *rig) openCard(id string) {
	r.t.Helper()
	card, ok := r.cardOf(detailEntryOf(id))
	if !ok {
		r.t.Fatalf("找不到 %s 的卡片", id)
	}
	x, y := centerOf(card)
	r.clickAt(x, y)
	r.frames(3)
	if d := r.view().Detail; d.Item == nil || d.Item.ID != id {
		r.t.Fatalf("点卡片之后详情应是 %s：%+v", id, d)
	}
}

// titleBox 是详情头部的标题输入框。
func (r *rig) titleBox() (value string) {
	r.t.Helper()
	return r.mustNode(gfx.RoleTextField, locales.LibraryTitlePlaceholder()).Value
}

// editTitle 点进标题框、把光标放到末尾、再敲一段字。
func (r *rig) editTitle(extra string) {
	r.t.Helper()
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	r.keys("end")
	r.typeText(extra)
}

// clickPrefix 点第一个可访问名以 prefix 开头的 role 节点（名字里带时间之类不好写死的后缀时用）。
func (r *rig) clickPrefix(role gfx.Role, prefix string) {
	r.t.Helper()
	for _, n := range r.session.Access() {
		if n.Role == role && strings.HasPrefix(n.Label, prefix) {
			x, y := centerOf(n)
			r.clickAt(x, y)
			return
		}
	}
	r.t.Fatalf("找不到名字以 %q 开头的 %v", prefix, role)
}

// hasPrefix 判断有没有可访问名以 prefix 开头的 role 节点：折叠块的按钮名是整段摘要（标题加说明），不是标题本身。
func (r *rig) hasPrefix(role gfx.Role, prefix string) bool {
	r.t.Helper()
	for _, n := range r.session.Access() {
		if n.Role == role && strings.HasPrefix(n.Label, prefix) {
			return true
		}
	}
	return false
}

// toastText 展开提示的「查看详情」，返回此刻画面上所有文字拼成的串（详情原文可能折成几行，所以拼起来再找）。
// 提示一出现就被窗口帧领走，排队的那份已经空了，所以只能从画面上读。
func (r *rig) toastText() string {
	r.t.Helper()
	if r.has(gfx.RoleButton, locales.CommonViewDetail()) {
		r.click(gfx.RoleButton, locales.CommonViewDetail())
	}
	return strings.Join(r.session.Texts(), "\n")
}

// 选中卡片：详情头部显示标题、类型相关的页签与角标；没有选中时是「请选择」。
func TestDetailShowsTheSelectedItemInTheCardView(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	if !r.hasText(locales.LibraryNoSelection()) {
		t.Fatalf("没有选中时应显示「%s」", locales.LibraryNoSelection())
	}
	r.openCard(detailNoteID)
	if got := r.titleBox(); got != detailEntryOf(detailNoteID).Title {
		t.Fatalf("标题框应显示条目标题，实际 %q", got)
	}
	if !r.hasText(locales.LibraryPinnedBadge()) {
		t.Fatalf("置顶的条目应显示「%s」角标", locales.LibraryPinnedBadge())
	}
	if r.hasText(locales.LibraryNoSelection()) {
		t.Fatal("选中之后不应再显示「请选择」")
	}
	if !r.has(gfx.RoleButton, locales.LibraryBodySection()) {
		t.Fatalf("应有「%s」页签", locales.LibraryBodySection())
	}
	for _, absent := range []string{locales.LibraryForumSummarySection(), locales.LibraryForumRepliesSection(), locales.LibraryTranscript()} {
		if r.has(gfx.RoleButton, absent) {
			t.Fatalf("笔记不应有「%s」页签", absent)
		}
	}
}

// 页签随条目类型：论坛有「讨论总结 / 正文 / 讨论」，视频有「文字稿」并带字数后缀。
func TestDetailTabsFollowTheItemType(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailForumID)
	for _, name := range []string{locales.LibraryForumSummarySection(), locales.LibraryBodySection(), locales.LibraryForumRepliesSection()} {
		if !r.has(gfx.RoleButton, name) {
			t.Fatalf("论坛应有「%s」页签", name)
		}
	}
	r.openCard(detailVideoID)
	suffix := locales.LibraryTranscriptLength(len([]rune("这是一段文字稿内容。")))
	if !r.has(gfx.RoleButton, locales.LibraryTranscript()+" "+suffix) { // 可访问名是「标签 后缀」
		t.Fatalf("视频应有「%s」页签（带字数后缀 %s）", locales.LibraryTranscript(), suffix)
	}
	if !r.hasText(suffix) {
		t.Fatalf("文字稿页签的字数后缀应画在页签里")
	}
	if r.has(gfx.RoleButton, locales.LibraryForumSummarySection()) {
		t.Fatal("换成视频之后不应再有论坛的页签")
	}
}

// 在标题框里敲字：先回显，防抖 800 ms 之后保存，补丁里只有标题、基线是原标题。
func TestEditingTheTitleSavesAfterTheDebounce(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	orig := detailEntryOf(detailNoteID).Title
	r.editTitle("（改）")
	if got := r.titleBox(); got != orig+"（改）" {
		t.Fatalf("敲字应立刻回显，实际 %q", got)
	}
	if n := len(r.svc.Drafts()); n != 0 {
		t.Fatalf("防抖期内不应保存，实际 %d 次", n)
	}
	r.clock.Advance(800 * time.Millisecond)
	r.frames(3)
	drafts := r.svc.Drafts()
	if len(drafts) != 1 {
		t.Fatalf("防抖结束后应保存一次，实际 %d 次", len(drafts))
	}
	if d := drafts[0]; d.ID != detailNoteID || d.Patch.Title == nil || *d.Patch.Title != orig+"（改）" || d.Base.Title == nil || *d.Base.Title != orig || d.Patch.Content != nil {
		t.Fatalf("补丁应只有标题、基线是原标题，实际 %+v", d)
	}
}

// Ctrl+S 立即保存（自动保存关着时这是唯一的落盘办法）；没有挂载详情时不拦截这个键。
func TestCtrlSSavesAtOnceAndOnlyWhileTheDetailIsMounted(t *testing.T) {
	t.Parallel()
	r := detailRig(t, func(r *rig) { r.store.AutoSave = func() bool { return false } })
	r.openCard(detailNoteID)
	r.editTitle("!")
	r.clock.Advance(10 * time.Second)
	r.frames(3)
	if n := len(r.svc.Drafts()); n != 0 {
		t.Fatalf("自动保存关着时不该自己保存，实际 %d 次", n)
	}
	r.keys("ctrl+s")
	if n := len(r.svc.Drafts()); n != 1 {
		t.Fatalf("Ctrl+S 应立即保存，实际 %d 次", n)
	}

	// 表格视图里详情浮层没打开时详情没有挂载：Ctrl+S 不拦截（交给别处）。
	r2 := detailRig(t)
	r2.toTable()
	res := r2.screen.ListShortcut(gfx.KeyEvent{Modifiers: gfx.ModifierControl, Text: "s"})
	if res.Handled {
		t.Fatal("详情没挂载时 Ctrl+S 不该被拦下")
	}
}

// Alt+Z 切换专注阅读：列表栏与侧栏让位、详情居中；Esc 退出；Ctrl+Alt+Z 不算。
func TestAltZTogglesFocusReadingAndEscLeavesIt(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	card := detailEntryOf(detailNoteID).Title
	if !r.has(gfx.RoleButton, card) {
		t.Fatal("夹具失效：卡片列表里应有这条的卡片")
	}
	r.keys("alt+z")
	if !r.screen.FocusReading() {
		t.Fatal("Alt+Z 应进入专注阅读")
	}
	if r.has(gfx.RoleButton, card) {
		t.Fatal("专注阅读时列表栏不挂载")
	}
	if !r.has(gfx.RoleButton, locales.LibraryExitFocusReading()) {
		t.Fatal("专注阅读时应有退出入口")
	}
	r.keys("escape")
	if r.screen.FocusReading() {
		t.Fatal("Esc 应退出专注阅读")
	}
	if !r.has(gfx.RoleButton, card) {
		t.Fatal("退出之后列表栏应回来")
	}
	// 按住 Ctrl 时不算（无头会话里没人处理的 Ctrl+Alt 组合会报错，所以直接问快捷键钩子）。
	if res := r.screen.ListShortcut(gfx.KeyEvent{Modifiers: gfx.ModifierControl | gfx.ModifierAlt, Text: "z"}); res.Handled || r.screen.FocusReading() {
		t.Fatal("按住 Ctrl 时 Alt+Z 不该切换")
	}
}

// 头部的专注钮与退出钮也切换专注阅读。
func TestFocusButtonsToggleFocusReading(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.click(gfx.RoleButton, locales.LibraryEnterFocusReading())
	if !r.screen.FocusReading() {
		t.Fatal("点专注钮应进入专注阅读")
	}
	r.click(gfx.RoleButton, locales.LibraryExitFocusReading())
	if r.screen.FocusReading() {
		t.Fatal("点退出钮应退出")
	}
}

// Ctrl+F 打开查找条（工具栏有查找入口时）；焦点在查找框里按 Esc 只关查找条、不退出专注阅读。
func TestFindBarOpensWithCtrlFAndEscClosesOnlyTheBar(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.keys("alt+z")
	r.keys("ctrl+f")
	placeholder := locales.LibraryPanelFindPlaceholder()
	if !r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("Ctrl+F 应打开查找条")
	}
	r.keys("escape")
	if r.has(gfx.RoleTextField, placeholder) {
		t.Fatal("焦点在查找框里按 Esc 应关掉查找条")
	}
	if !r.screen.FocusReading() {
		t.Fatal("这次 Esc 只该关查找条（stopPropagation），不该退出专注阅读")
	}
	r.keys("escape")
	if r.screen.FocusReading() {
		t.Fatal("再按一次 Esc 才退出专注阅读")
	}
}

// 卸载工作区（切到别的模块）之前保存未落盘的编辑（渲染层 LibraryWorkspace 卸载 effect 的 flushPendingSave）。
func TestUnmountingTheWorkspaceFlushesPendingEdits(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.editTitle("!")
	if n := len(r.svc.Drafts()); n != 0 {
		t.Fatalf("还在防抖期内，不该已经保存，实际 %d 次", n)
	}
	r.wsHidden = true
	r.frames(3)
	if n := len(r.svc.Drafts()); n != 1 {
		t.Fatalf("工作区卸载应把待保存的编辑落盘，实际 %d 次", n)
	}
}

// 表格视图：点行打开详情浮层，Esc 关；打开时 BeginReview 冻结了当前列表，Alt+→ / Alt+← 在冻结的列表里前后移动。
func TestTableRowOpensTheDetailModalAndAltArrowsStepThroughTheFrozenList(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.toTable()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[0])
	r.clickAt(x, y)
	if !r.screen.DetailModalOpen() {
		t.Fatal("点行应打开详情浮层")
	}
	if got := r.view().Review.IDs; len(got) != len(entries) {
		t.Fatalf("打开浮层应冻结当前列表（%d 条），实际 %v", len(entries), got)
	}
	if got := r.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("浮层里应是被点的这一条，实际 %q", got)
	}
	r.keys("alt+right")
	if got := r.view().List.SelectedID; got != entries[1].ID {
		t.Fatalf("Alt+→ 应到下一条 %q，实际 %q", entries[1].ID, got)
	}
	r.keys("alt+left")
	if got := r.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("Alt+← 应回到上一条 %q，实际 %q", entries[0].ID, got)
	}
	r.keys("alt+left")
	if got := r.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("到头之后 Alt+← 不该再动，实际 %q", got)
	}
	r.keys("escape")
	if r.screen.DetailModalOpen() {
		t.Fatal("Esc 应关闭详情浮层")
	}
}

// 整理导航条的计数：「第几条 / 共几条」与跳过说明各是一段文字（JSX 里相邻的文本节点，对照按节点逐段比），
// 拼起来读作「3 / 4 · 已跳过 1 条已删除资料」；跳过的条目不进「第几条」的走法，但仍占总数。
func TestReviewBarShowsPositionTotalAndSkipped(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.toTable()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[0])
	r.clickAt(x, y)
	if !r.screen.DetailModalOpen() {
		t.Fatal("点行应打开详情浮层")
	}
	screenText := func() string {
		r.frames(2)
		return strings.Join(r.session.Texts(), "")
	}
	total := len(entries)
	if got, want := screenText(), fmt.Sprintf("1 / %d", total); !strings.Contains(got, want) {
		t.Fatalf("刚打开时计数应是 %q，画面上的文字：%q", want, got)
	}
	if strings.Contains(screenText(), locales.LibraryWorkflowSkipped(1)) {
		t.Fatal("还没有跳过任何条目，不该出现跳过说明")
	}
	r.svc.EditExternally(entries[1].ID, func(it *services.LibraryItem) { it.DeletedAt = 5 })
	r.keys("alt+right")
	if got := r.view().List.SelectedID; got != entries[2].ID {
		t.Fatalf("Alt+→ 应跳过被删除的第二条停在第三条 %q，实际 %q", entries[2].ID, got)
	}
	want := fmt.Sprintf("3 / %d · %s", total, locales.LibraryWorkflowSkipped(1))
	if got := screenText(); !strings.Contains(got, want) {
		t.Fatalf("跳过一条之后计数应是 %q，画面上的文字：%q", want, got)
	}
}

// Alt+方向键在输入框里不动（光标移动属于输入框）。
func TestAltArrowsDoNothingWhileTypingInTheModal(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.toTable()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[0])
	r.clickAt(x, y)
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	r.keys("alt+right")
	if got := r.view().List.SelectedID; got != entries[0].ID {
		t.Fatalf("焦点在标题框里时 Alt+→ 不该翻页，实际 %q", got)
	}
}

// Enter 打开选中的行：只在表格视图；卡片视图里没有这个行为。
func TestEnterOpensTheSelectedRowOnlyInTheTableView(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.keys("down")
	r.keys("enter")
	if r.screen.DetailModalOpen() {
		t.Fatal("卡片视图里 Enter 不开浮层")
	}
	r.toTable()
	r.keys("down")
	if r.view().List.SelectedID == "" {
		t.Fatal("夹具失效：↓ 应选中一行")
	}
	r.keys("enter")
	if !r.screen.DetailModalOpen() {
		t.Fatal("表格视图里 Enter 应打开选中的行")
	}
}

// 关闭浮层前先保存编辑；保存失败时浮层不关、弹出带原因的提示。
func TestClosingTheModalSavesFirstAndStaysOpenWhenTheSaveFails(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.toTable()
	entries := r.view().List.Entries
	x, y := r.rowBlank(entries[0])
	r.clickAt(x, y)
	r.editTitle("!")
	r.keys("escape")
	if r.screen.DetailModalOpen() {
		t.Fatal("保存成功之后 Esc 应关掉浮层")
	}
	if n := len(r.svc.Drafts()); n != 1 {
		t.Fatalf("关闭前应先保存，实际 %d 次", n)
	}

	r2 := detailRig(t)
	r2.toTable()
	x, y = r2.rowBlank(r2.view().List.Entries[0])
	r2.clickAt(x, y)
	r2.svc.FailDraft("磁盘已满")
	r2.editTitle("!")
	r2.keys("escape")
	if !r2.screen.DetailModalOpen() {
		t.Fatal("保存失败时浮层应保持打开，不能丢掉编辑")
	}
	// 失败不弹窗：头部的状态写「保存失败」，原因在 Store 的 SaveError 里（渲染层的 saveError 状态，悬停提示读它）。
	if !r2.hasText(locales.LibrarySaveFailed()) || !strings.Contains(r2.view().Detail.SaveError, "磁盘已满") {
		t.Fatalf("保存失败应在头部显示失败状态并留下原因，实际 SaveError=%q", r2.view().Detail.SaveError)
	}
}

// 来源链接：点击经 OpenExternal 交给系统浏览器；没有接线时弹带原因的失败提示。
func TestSourceLinkOpensInTheSystemBrowser(t *testing.T) {
	t.Parallel()
	var opened []string
	r := detailRig(t, func(r *rig) {
		r.screen.OpenExternal = func(u string) error { opened = append(opened, u); return nil }
	})
	r.openCard(detailNoteID)
	r.click(gfx.RoleLink, "example.com")
	if len(opened) != 1 || opened[0] != "https://example.com/go" {
		t.Fatalf("应打开来源链接，实际 %v", opened)
	}

	r2 := detailRig(t)
	r2.openCard(detailNoteID)
	r2.click(gfx.RoleLink, "example.com")
	if text := r2.toastText(); !strings.Contains(text, errNoOpener.Error()) {
		t.Fatalf("没有接线打开链接时应弹带原因的失败提示，实际画面文字 %q", text)
	}

	r3 := detailRig(t, func(r *rig) {
		r.screen.OpenExternal = func(string) error { return errors.New("被安全软件拦截") }
	})
	r3.openCard(detailNoteID)
	r3.click(gfx.RoleLink, "example.com")
	if text := r3.toastText(); !strings.Contains(text, "被安全软件拦截") {
		t.Fatalf("打开失败的原因应进提示详情，实际画面文字 %q", text)
	}
}

// 没接问答界面的壳（对照与单测里不装配 ArticleAsk）里，「提问」钮点一下弹失败提示而不是什么也不发生；
// 接了问答界面时它开合右侧的提问栏（detail_ask_ui_test.go）。
func TestAskButtonWithoutAnAskScreenAnswersWithAFailureToast(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.click(gfx.RoleButton, locales.ArticleAskTitle())
	text := r.toastText()
	if !strings.Contains(text, locales.CommonOperationFailed(locales.ArticleAskTitle())) {
		t.Fatalf("点「提问」应弹失败提示，实际画面文字 %q", text)
	}
	if !strings.Contains(text, "M3") || !strings.Contains(text, "B-061") {
		t.Fatalf("提示详情应写明里程碑 M3 与台账 B-061，实际画面文字 %q", text)
	}
	if n := len(r.log.entries()); n != 0 {
		t.Fatalf("没迁移不是故障，不该写应用错误日志，实际 %d 条", n)
	}
}

// 保存冲突：外部改过之后再保存，显示冲突提示；「使用已保存版本」放弃本地编辑并采用服务端内容。
func TestConflictNoticeResolvesToTheSavedVersion(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.svc.EditExternally(detailNoteID, func(item *services.LibraryItem) { item.Title = "外部改过的标题" })
	r.editTitle("!")
	r.keys("ctrl+s")
	if !r.hasText(locales.LibraryDraftConflict()) {
		t.Fatalf("应显示冲突提示，实际画面文字 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.LibraryDraftConflictUseSaved())
	if r.hasText(locales.LibraryDraftConflict()) {
		t.Fatal("选择之后冲突提示应消失")
	}
	if got := r.titleBox(); got != "外部改过的标题" {
		t.Fatalf("应采用已保存的版本，实际 %q", got)
	}
}

// 待复核：提醒块显示，点「标记已复核」之后向服务发一次更新、提醒消失。
func TestReviewNoticeMarksTheItemReviewed(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailReviewID)
	// 折叠块的按钮名是整段摘要（标题、「已保留可读内容」、「查看缺口与复核」，与渲染层 <summary> 的文字一致），按开头找。
	if !r.hasPrefix(gfx.RoleButton, locales.ArticleReaderImportIncomplete()) {
		t.Fatalf("待复核的条目应显示复核提醒，实际画面文字 %q", r.session.Texts())
	}
	r.clickPrefix(gfx.RoleButton, locales.ArticleReaderImportIncomplete()) // 先展开：「标记已复核」在展开的正文里
	r.click(gfx.RoleButton, locales.LibraryMarkReviewed())
	if n := len(r.svc.UpdateCalls()); n != 1 {
		t.Fatalf("标记已复核应向服务发一次更新，实际 %d 次", n)
	}
	if r.hasPrefix(gfx.RoleButton, locales.ArticleReaderImportIncomplete()) {
		t.Fatal("标记之后复核提醒应消失")
	}
}

// 更新的来源版本：折叠块显示版本数，展开后「采用」把该版本的正文写进草稿。
func TestRevisionsNoticeAdoptsASourceVersion(t *testing.T) {
	t.Parallel()
	r := detailRig(t, func(r *rig) {
		r.svc.SeedSourceVersions(detailNoteID, []services.LibrarySourceVersion{
			{ID: "sv1", Title: "新版本", Content: "新版本的正文", ReviewJSON: "[]", CapturedAt: 1_800_000_000_000},
		})
	})
	r.openCard(detailNoteID)
	label := locales.LibrarySourceRevisions(1)
	if !r.hasText(label) {
		t.Fatalf("应显示「%s」，实际画面文字 %q", label, r.session.Texts())
	}
	r.click(gfx.RoleButton, label)
	r.clickPrefix(gfx.RoleButton, "新版本 · ") // 再展开这一个版本（标题后面是采集时间）
	r.click(gfx.RoleButton, locales.LibrarySourceRevisionAdopt())
	r.clock.Advance(time.Second)
	r.frames(3)
	var adopted bool
	for _, d := range r.svc.Drafts() {
		if d.Patch.Content != nil && *d.Patch.Content == "新版本的正文" {
			adopted = true
		}
	}
	if !adopted {
		t.Fatalf("采用应把该版本的正文写进草稿，实际 %+v", r.svc.Drafts())
	}
}
