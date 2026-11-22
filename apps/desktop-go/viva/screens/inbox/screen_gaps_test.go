package inbox

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件补 M3-1 变异检查（m31）暴露的界面缺口：分组说明的位置、「不再提醒」的忙态只属于被点的那一行、
// 重读期间已有的列表不被转圈替换。

// textBounds 是画面上恰好等于 value 的那行文字的外框。
func (r *uiRig) textBounds(value string) gfx.Rect {
	r.t.Helper()
	for _, got := range r.session.Snapshot().Texts {
		if got.Value == value {
			return got.Bounds
		}
	}
	r.t.Fatalf("画面上没有文字 %q；画面上有 %q", value, r.session.Texts())
	return gfx.Rect{}
}

// rowBounds 是侧栏某个筛选行（按钮）的外框。
func (r *uiRig) rowBounds(label string, count int) gfx.Rect {
	r.t.Helper()
	n, ok := r.node(gfx.RoleButton, filterName(label, count))
	if !ok {
		r.t.Fatalf("侧栏缺少筛选行「%s」", filterName(label, count))
	}
	return n.Bounds
}

// 分组说明夹在各自的相邻两行之间：「整理建议 · 可选」在「采集问题」与「未归合集」之间，
// 「后台任务 · 待执行内容」在「未归合集」与「语义索引」之间（Wiki 编译排在语义索引之后）。
// 只验证说明在画面上出现（TestSidebarShowsSixFiltersWithCountsAndGroupHeadings）挡不住说明被放到别的行前面。
func TestSidebarGroupHeadingsSitBetweenTheirNeighbourRows(t *testing.T) {
	r := newUIRig(t, boardItems())
	importRow := r.rowBounds(locales.InboxFilterImportIssue(), 1)
	unclassifiedRow := r.rowBounds(locales.InboxFilterUnclassified(), 2)
	semantic := r.rowBounds(locales.InboxFilterSemanticPending(), 0)
	wiki := r.rowBounds(locales.InboxFilterWikiPending(), 0)
	optional := r.textBounds(locales.InboxOptionalSection())
	background := r.textBounds(locales.InboxBackgroundSection())

	if !(importRow.Y+importRow.Height <= optional.Y && optional.Y+optional.Height <= unclassifiedRow.Y) {
		t.Errorf("「%s」应在「采集问题」行之下、「未归合集」行之上：采集问题 %+v，说明 %+v，未归合集 %+v",
			locales.InboxOptionalSection(), importRow, optional, unclassifiedRow)
	}
	if !(unclassifiedRow.Y+unclassifiedRow.Height <= background.Y && background.Y+background.Height <= semantic.Y) {
		t.Errorf("「%s」应在「未归合集」行之下、「语义索引」行之上：未归合集 %+v，说明 %+v，语义索引 %+v",
			locales.InboxBackgroundSection(), unclassifiedRow, background, semantic)
	}
	if semantic.Y+semantic.Height > wiki.Y {
		t.Errorf("「语义索引」行应在「Wiki 编译」行之上：%+v %+v", semantic, wiki)
	}
}

// 「不再提醒」处理中只影响被点的那个任务：另一条带警告的采集任务的按钮仍可点，也不显示「处理中」。
func TestRowDismissWarningBusyOnlyAffectsTheClickedTask(t *testing.T) {
	warn := func(task string) services.InboxItem {
		return services.InboxItem{Kind: services.InboxKindImportIssue, ID: "import:" + task, TaskID: task, Title: "任务 " + task,
			Status: services.ImportStatusCompleted, Message: "文字稿缺失", ResultItemID: "item-" + task}
	}
	r := newUIRig(t, []services.InboxItem{warn("t2"), warn("t3")}, goroutines)
	release := r.svc.Hold("AcknowledgeImportWarning")
	if got := r.count(gfx.RoleButton, locales.InboxDismissWarning()); got != 2 {
		t.Fatalf("两条带警告的任务应各有一个「不再提醒」，实际 %d 个", got)
	}

	r.clickNthButton(locales.InboxDismissWarning(), 0)
	r.settle("第一条进入处理中", func() bool { return r.has(gfx.RoleButton, locales.InboxProcessing()) })
	if got := r.count(gfx.RoleButton, locales.InboxProcessing()); got != 1 {
		t.Fatalf("只有被点的那一条显示「处理中」，实际 %d 个", got)
	}
	other, ok := r.node(gfx.RoleButton, locales.InboxDismissWarning())
	if !ok {
		t.Fatalf("另一条任务的「不再提醒」应仍在；画面上有 %q", r.session.Texts())
	}
	if other.Disabled {
		t.Fatal("另一条任务的「不再提醒」不应被禁用：处理中的是别的任务")
	}

	release()
	r.settle("处理结束", func() bool { return !r.has(gfx.RoleButton, locales.InboxProcessing()) })
}

// 已经读到列表之后再刷新（刷新钮、重试、整理成功后的重读）：读取期间已有的卡片继续留在画面上，
// 不被整块转圈替换（转圈只属于第一次读取）。
func TestBodyKeepsTheListWhileRefreshingAnAlreadyLoadedList(t *testing.T) {
	r := newUIRig(t, boardItems(), goroutines)
	r.settle("首次读取完成", func() bool { return r.hasText("复核一") })

	release := r.svc.Hold("List")
	r.click(gfx.RoleButton, locales.InboxRefresh())
	r.settle("再次读取在途", func() bool { return r.store.View().IsLoading })
	if !r.hasText("复核一") {
		t.Fatalf("重读期间已有的卡片应留在画面上；画面上有 %q", r.session.Texts())
	}
	if r.hasText(locales.InboxGroupDone()) {
		t.Fatalf("重读期间不能画成「已处理完」；画面上有 %q", r.session.Texts())
	}

	release()
	r.settle("再次读取完成", func() bool { return !r.store.View().IsLoading })
	if !r.hasText("复核一") {
		t.Fatalf("读取完成后卡片仍在；画面上有 %q", r.session.Texts())
	}
}
