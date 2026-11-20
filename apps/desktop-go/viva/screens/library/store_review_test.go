package library

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 library-workflow.store.ts 的 beginReview、navigate（连续整理，台账 B-052）、knowledge.store 的 selectItem
// 返回值（SelectItemWait）与 execute 开头的落盘。条目是 detailHarness 的 a、b、c（默认按更新时间倒序：整理顺序是 a b c）。

func (h *harness) review() ReviewState { return h.store.View().Review }

// 打开连续整理：冻结的是当前筛选与排序、不含分页；选中这一条、详情读到；顺序是冻结的全部结果；忙标志放下。
func TestBeginReviewFreezesTheFilterAndOpensTheItem(t *testing.T) {
	h := detailHarness(t)
	if !h.store.BeginReview(h.ctx, "b") {
		t.Fatal("打开连续整理应成功")
	}
	v := h.store.View()
	if !slices.Equal(v.Review.IDs, []string{"a", "b", "c"}) || v.Review.Index("b") != 1 || v.Review.Index("zzz") != -1 || len(v.Review.Skipped) != 0 {
		t.Fatalf("整理顺序：%+v", v.Review)
	}
	if v.List.SelectedID != "b" || v.Detail.Item == nil || v.Detail.Item.ID != "b" || v.Detail.Loading {
		t.Fatalf("应选中并读到 b：%q %+v", v.List.SelectedID, v.Detail)
	}
	qs := h.svc.Freezes()
	if len(qs) != 1 || qs[0].Scope != services.LibraryScopeAll || qs[0].SortBy != services.LibrarySortUpdatedAt || qs[0].Limit != 0 || qs[0].Cursor != "" {
		t.Fatalf("冻结的查询应是筛选与排序、不含分页：%+v", qs)
	}
	if v.Batch.Busy {
		t.Fatal("做完之后忙标志要放下")
	}
}

// 点的这一条不在冻结的结果里（例如刚被别处移进了回收站）：排到顺序最前面，照样能整理它。
func TestBeginReviewPutsAnItemOutsideTheFrozenResultFirst(t *testing.T) {
	h := detailHarness(t)
	h.svc.EditExternally("c", func(it *services.LibraryItem) { it.DeletedAt = 5 })
	if !h.store.BeginReview(h.ctx, "c") {
		t.Fatal("打开连续整理应成功")
	}
	if got := h.review().IDs; !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("不在冻结结果里的条目应排在最前：%v", got)
	}
}

// 冻结期间筛选变了：冻结的是旧筛选的结果，不能当成新筛选的整理顺序——失败并留下原因，不选中、不记顺序。
func TestBeginReviewRefusesAStaleFreeze(t *testing.T) {
	h := detailHarness(t)
	release := h.svc.HoldFreeze()
	done := make(chan bool)
	go func() { done <- h.store.BeginReview(h.ctx, "b") }()
	waitFor(t, "冻结发出", func() bool { return len(h.svc.Freezes()) == 1 })

	h.store.SetScope(h.ctx, services.LibraryScopeFavorites)
	release()
	if ok := <-done; ok {
		t.Fatal("筛选已变化，晚到的冻结不应被接受")
	}
	if !h.logged(locales.LibraryActionOpenWorkflow(), "筛选已变化，请重新打开") {
		t.Fatalf("失败应留痕：%v", h.log.entries())
	}
	if len(h.review().IDs) != 0 || h.store.View().List.SelectedID == "b" || h.store.View().Batch.Busy {
		t.Fatalf("不应记顺序或选中：%+v %q", h.review(), h.store.View().List.SelectedID)
	}
}

// 上一条的草稿落不了盘：不选中新的，也不记整理顺序；原因是「请先处理当前保存或加载错误」，草稿仍在待保存里。
func TestBeginReviewStopsWhenTheCurrentDraftCannotBeSaved(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	h.svc.FailDraft("磁盘满")
	if h.store.BeginReview(h.ctx, "b") {
		t.Fatal("草稿落不了盘时不应打开")
	}
	if !h.logged(locales.LibraryActionOpenWorkflow(), "请先处理当前保存或加载错误") {
		t.Fatalf("失败应留痕：%v", h.log.entries())
	}
	if h.store.View().List.SelectedID != "a" || len(h.review().IDs) != 0 || !h.detail().Unsaved {
		t.Fatalf("选中与草稿应原样保留：%q %+v", h.store.View().List.SelectedID, h.detail())
	}
}

// 这一条读不出来：同样不记顺序；详情留下读取失败的原因（界面显示重试）。
func TestBeginReviewStopsWhenTheItemCannotBeRead(t *testing.T) {
	h := detailHarness(t)
	h.svc.FailOn("GetItem:b", errors.New("读取失败"))
	if h.store.BeginReview(h.ctx, "b") {
		t.Fatal("读不出来时不应打开")
	}
	if !h.logged(locales.LibraryActionOpenWorkflow(), "请先处理当前保存或加载错误") || h.detail().LoadError != "读取失败" || len(h.review().IDs) != 0 {
		t.Fatalf("读取失败：%+v %v", h.detail(), h.log.entries())
	}
}

// 冻结失败走变更失败通道：返回 false、留日志（带原因），不选中，忙标志放下。
func TestBeginReviewFreezeFailureIsReported(t *testing.T) {
	h := detailHarness(t)
	h.svc.FailOn("Freeze", errors.New("数据库繁忙"))
	if h.store.BeginReview(h.ctx, "b") {
		t.Fatal("冻结失败应返回 false")
	}
	if !h.logged(locales.LibraryActionOpenWorkflow(), "数据库繁忙") || h.store.View().List.SelectedID != "" || h.store.View().Batch.Busy {
		t.Fatalf("失败：%v %+v", h.log.entries(), h.store.View().Batch)
	}
}

// 同一时刻只做一件事：忙着（批量或另一次整理）时打开与切换都直接返回 false，不冻结、不读取、不提示。
func TestReviewIsRefusedWhileBusy(t *testing.T) {
	h := detailHarness(t)
	if !h.store.BeginReview(h.ctx, "a") {
		t.Fatal("先打开")
	}
	freezes, gets := len(h.svc.Freezes()), len(h.svc.Gets())
	if !h.store.tryBusy() {
		t.Fatal("空闲时应抢得到忙标志")
	}
	if h.store.tryBusy() {
		t.Fatal("忙着时不应再抢到")
	}
	if h.store.BeginReview(h.ctx, "b") || h.store.ReviewNavigate(h.ctx, 1) {
		t.Fatal("忙着时都应返回 false")
	}
	if len(h.svc.Freezes()) != freezes || len(h.svc.Gets()) != gets || len(h.log.entries()) != 0 || h.store.View().List.SelectedID != "a" {
		t.Fatalf("忙着时什么也不该做：%d %d %v", len(h.svc.Freezes()), len(h.svc.Gets()), h.log.entries())
	}
	h.store.clearBusy()
	if h.store.View().Batch.Busy {
		t.Fatal("放下之后不应再忙")
	}
}

// 沿冻结的顺序切换；走到头给「已到本次整理范围的边界」、选中不动，之后反向照常走。
func TestReviewNavigateWalksTheFrozenOrderAndStopsAtTheEnds(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	selected := func() string { return h.store.View().List.SelectedID }
	steps := []struct {
		dir  int
		want string
		edge bool
	}{{-1, "a", true}, {1, "b", false}, {1, "c", false}, {1, "c", true}, {-1, "b", false}, {-1, "a", false}, {-1, "a", true}}
	for i, st := range steps {
		h.store.DismissBatch()
		if !h.store.ReviewNavigate(h.ctx, st.dir) {
			t.Fatalf("第 %d 步应成功", i)
		}
		if selected() != st.want || h.detail().Item == nil || h.detail().Item.ID != st.want {
			t.Fatalf("第 %d 步（%d）应停在 %s：%q", i, st.dir, st.want, selected())
		}
		notice := h.store.View().Batch.Notice
		if st.edge != (notice == locales.LibraryWorkflowBoundary()) {
			t.Fatalf("第 %d 步的边界提示：%q", i, notice)
		}
	}
	if h.store.View().Batch.Busy {
		t.Fatal("忙标志要放下")
	}
}

// 被删除的条目跳过并记账（去重）：来回走两遍，已跳过只记一次。
func TestReviewNavigateSkipsDeletedItems(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	h.svc.EditExternally("b", func(it *services.LibraryItem) { it.DeletedAt = 5 })
	if !h.store.ReviewNavigate(h.ctx, 1) || h.store.View().List.SelectedID != "c" {
		t.Fatalf("应跳过 b 停在 c：%q", h.store.View().List.SelectedID)
	}
	if !h.store.ReviewNavigate(h.ctx, -1) || h.store.View().List.SelectedID != "a" {
		t.Fatalf("应跳过 b 回到 a：%q", h.store.View().List.SelectedID)
	}
	if got := h.review().Skipped; !slices.Equal(got, []string{"b"}) {
		t.Fatalf("已跳过应去重：%v", got)
	}
	// 重新打开一轮整理：顺序重新冻结（b 已不在「全部」里），已跳过清零。
	if !h.store.BeginReview(h.ctx, "a") {
		t.Fatal("重新打开应成功")
	}
	if r := h.review(); !slices.Equal(r.IDs, []string{"a", "c"}) || len(r.Skipped) != 0 {
		t.Fatalf("新一轮整理：%+v", r)
	}
}

// 条目已被彻底删除（读不到）也跳过并记账，和已移到回收站的一样。
func TestReviewNavigateSkipsMissingItems(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	if !h.store.MoveToTrash(h.ctx, []string{"b"}) || !h.store.DeleteForever(h.ctx, []string{"b"}, false) {
		t.Fatal("先把 b 彻底删掉")
	}
	if !h.store.ReviewNavigate(h.ctx, 1) || h.store.View().List.SelectedID != "c" || !slices.Equal(h.review().Skipped, []string{"b"}) {
		t.Fatalf("应跳过读不到的 b 停在 c：%q %+v", h.store.View().List.SelectedID, h.review())
	}
}

// 切换时落盘等不到结果（另一次落盘还在途、等待被取消）：报「保存未完成」，不切换。
func TestReviewNavigateReportsAnUnfinishedSave(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	h.edit(patchTitle("甲改"))
	release := h.svc.HoldSave()
	flushed := make(chan bool)
	go func() { flushed <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "落盘发出", func() bool { return len(h.svc.Drafts()) == 1 })
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if h.store.ReviewNavigate(ctx, 1) {
		t.Fatal("落盘没有结果时不应切换")
	}
	if !h.logged(locales.LibraryActionSwitchWorkflowItem(), "保存未完成") || h.store.View().List.SelectedID != "a" {
		t.Fatalf("应留在 a 并说明原因：%q %v", h.store.View().List.SelectedID, h.log.entries())
	}
	release()
	if !<-flushed {
		t.Fatal("原来那次落盘应成功")
	}
}

// 切换之前先把当前条的草稿落盘；落不了就不切，原因带上保存失败的原文。
func TestReviewNavigateSavesTheDraftFirstAndStopsWhenItFails(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	h.edit(patchTitle("甲改"))
	if !h.store.ReviewNavigate(h.ctx, 1) || h.store.View().List.SelectedID != "b" {
		t.Fatal("草稿应先落盘再切到 b")
	}
	if ds := h.svc.Drafts(); len(ds) != 1 || ds[0].ID != "a" || ds[0].Patch.Title == nil || *ds[0].Patch.Title != "甲改" {
		t.Fatalf("草稿应落盘：%+v", ds)
	}

	h.edit(patchTitle("乙改"))
	h.svc.FailDraft("磁盘满")
	if h.store.ReviewNavigate(h.ctx, 1) {
		t.Fatal("落不了盘时不应切换")
	}
	if h.store.View().List.SelectedID != "b" || !h.logged(locales.LibraryActionSwitchWorkflowItem(), "磁盘满") {
		t.Fatalf("应留在 b 并说明原因：%q %v", h.store.View().List.SelectedID, h.log.entries())
	}
}

// 目标条目读不出来：失败并留原因，选中不动。
func TestReviewNavigateReportsAnUnreadableTarget(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	h.svc.FailOn("GetItem:b", errors.New("读取失败"))
	if h.store.ReviewNavigate(h.ctx, 1) {
		t.Fatal("读不出来时不应切换")
	}
	if !h.logged(locales.LibraryActionSwitchWorkflowItem(), "读取失败") || h.store.View().List.SelectedID != "a" {
		t.Fatalf("失败：%v %q", h.log.entries(), h.store.View().List.SelectedID)
	}
}

// 检查读到了、真正选中时被后来的选中取代：报「资料读取失败，请重试」，不当成成功。
func TestReviewNavigateReportsASupersededSwitch(t *testing.T) {
	h := detailHarness(t)
	h.store.BeginReview(h.ctx, "a")
	first := h.svc.HoldGet()
	done := make(chan bool)
	go func() { done <- h.store.ReviewNavigate(h.ctx, 1) }()
	waitFor(t, "检查读取发出", func() bool { return slices.Contains(h.svc.Gets(), "b") })
	second := h.svc.HoldGet() // 第二次读取（选中时的读取）也卡住
	n := len(h.svc.Gets())
	first()
	waitFor(t, "选中时的读取发出", func() bool { return len(h.svc.Gets()) > n })
	h.store.SelectItem("c") // 别处又选了一条：b 的读取作废
	second()
	if ok := <-done; ok {
		t.Fatal("被取代的切换不应算成功")
	}
	if !h.logged(locales.LibraryActionSwitchWorkflowItem(), "资料读取失败，请重试") {
		t.Fatalf("失败应留痕：%v", h.log.entries())
	}
}

// ---- SelectItemWait ----

// 等待版选中：成功、取消选中返回 true；条目缺失、读取失败、上一条草稿落不了盘返回 false 且选中不动。
func TestSelectItemWaitReportsTheOutcome(t *testing.T) {
	h := detailHarness(t)
	if !h.store.SelectItemWait(h.ctx, "a") || h.detail().Item == nil || h.detail().Item.ID != "a" {
		t.Fatalf("成功：%+v", h.detail())
	}
	if h.store.SelectItemWait(h.ctx, "nope") || h.detail().LoadError != "条目不存在或已被删除" {
		t.Fatalf("缺失：%+v", h.detail())
	}
	if !h.store.SelectItemWait(h.ctx, "") || h.store.View().List.SelectedID != "" || h.detail().Item != nil {
		t.Fatalf("取消选中：%+v", h.detail())
	}
	h.open("a")
	h.edit(patchTitle("甲改"))
	h.svc.FailDraft("磁盘满")
	if h.store.SelectItemWait(h.ctx, "b") || h.store.View().List.SelectedID != "a" {
		t.Fatalf("草稿落不了盘：%q", h.store.View().List.SelectedID)
	}
}

// 取消选中之前还有没落盘的草稿：先落盘，落盘成功后清空选中并返回 true（落盘之后才走到的那条路径）。
func TestSelectItemWaitClearsTheSelectionAfterSavingTheDraft(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	if !h.store.SelectItemWait(h.ctx, "") {
		t.Fatal("草稿能落盘时取消选中应成功")
	}
	if ds := h.svc.Drafts(); len(ds) != 1 || ds[0].ID != "a" {
		t.Fatalf("草稿应先落盘：%+v", ds)
	}
	if h.store.View().List.SelectedID != "" || h.detail().Item != nil {
		t.Fatalf("应已取消选中：%q %+v", h.store.View().List.SelectedID, h.detail())
	}
}

// 等落盘的时候又选了别的条目：先发起的选中被取代，返回 false；最终选中的是后来的那条。
func TestSelectItemWaitIsSupersededWhileWaitingForTheDraft(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	h.store.Spawn = func(fn func()) { go fn() } // 后来的选中不阻塞调用方
	release := h.svc.HoldSave()
	waited := make(chan bool)
	go func() { waited <- h.store.SelectItemWait(h.ctx, "b") }()
	waitFor(t, "落盘发出", func() bool { return len(h.svc.Drafts()) == 1 })
	h.store.SelectItem("c")
	release()
	if ok := <-waited; ok {
		t.Fatal("被取代的选中不应算成功")
	}
	waitFor(t, "c 被选中并读到", func() bool {
		d := h.detail()
		return h.store.View().List.SelectedID == "c" && d.Item != nil && d.Item.ID == "c"
	})
}

// ---- 批量开头的落盘 ----

// 批量开头先把详情里的草稿落盘（渲染层 execute）：落盘在批量之前；落不了就整批不做，原因是「请先处理草稿保存错误」。
func TestBatchSavesTheDraftFirstAndRefusesWhenItCannot(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	if !h.store.SetStatus(h.ctx, []string{"b"}, services.LibraryStatusArchived) {
		t.Fatal("草稿能落盘时批量应成功")
	}
	calls := h.svc.Calls()
	indexOf := func(prefix string) int {
		return slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
	}
	if save, batch := indexOf("SaveDraft:"), indexOf("ExecuteBatch:"); save < 0 || batch < 0 || save > batch {
		t.Fatalf("应先落盘再批量：%v", calls)
	}

	h.edit(patchTitle("甲再改"))
	h.svc.FailDraft("磁盘满")
	before := len(h.svc.Batches())
	if h.store.SetStatus(h.ctx, []string{"c"}, services.LibraryStatusArchived) {
		t.Fatal("草稿落不了盘时整批不应执行")
	}
	if len(h.svc.Batches()) != before || !h.logged(locales.LibraryActionBatch(), "请先处理草稿保存错误") || h.store.View().Batch.Busy {
		t.Fatalf("不应发出批量：%v %v", h.svc.Batches(), h.log.entries())
	}
}
