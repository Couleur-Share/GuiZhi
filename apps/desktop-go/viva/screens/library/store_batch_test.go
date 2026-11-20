package library

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 stores/library-workflow.store.ts（全部符合筛选、批量执行、重试、状态条）与 knowledge.store.ts 的条目写操作：
// 每条独立记账、单条成功不弹结果栏、批后选择只留失败项、同一批次重试复用回执、筛选变了就不动选择。

// batchHarness 是装好 100 条夹具并读完第一页的台子：「全部」范围 80 条（20 条归档不算），回收站 5 条。
func batchHarness(t *testing.T) *harness {
	t.Helper()
	h := listHarness(t)
	h.store.Load(h.ctx)
	return h
}

func (h *harness) batch() BatchState { return h.store.View().Batch }

// logged 报告日志里有没有同时含有所有片段的一条。
func (h *harness) logged(parts ...string) bool {
	for _, entry := range h.log.entries() {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(entry, p)
		}
		if all {
			return true
		}
	}
	return false
}

func resultFor(b BatchState, id string) (services.LibraryBatchResult, bool) {
	for _, r := range b.Results {
		if r.ID == id {
			return r, true
		}
	}
	return services.LibraryBatchResult{}, false
}

// ---- 全部符合筛选 ----

// 冻结整个筛选结果：查询是筛选加排序、没有分页；选择是全部命中项，起点取最后一个；状态条给出冻结了多少项。
func TestSelectAllMatchingFreezesTheWholeFilter(t *testing.T) {
	h := batchHarness(t)
	if !h.store.SelectAllMatching(h.ctx) {
		t.Fatal("冻结应成功")
	}
	qs := h.svc.Freezes()
	if len(qs) != 1 || qs[0].Scope != services.LibraryScopeAll || qs[0].SortBy != services.LibrarySortUpdatedAt ||
		qs[0].SortOrder != services.LibraryOrderDesc || qs[0].Limit != 0 || qs[0].Offset != 0 || qs[0].Cursor != "" {
		t.Fatalf("冻结的查询应是筛选与排序、不含分页：%+v", qs)
	}
	v := h.store.View()
	if len(v.List.Selection) != listTotal || v.List.Anchor != v.List.Selection[listTotal-1] {
		t.Fatalf("应选中全部 %d 条且起点是最后一个：%d 条，起点 %q", listTotal, len(v.List.Selection), v.List.Anchor)
	}
	if got, want := v.Batch.Notice, locales.LibraryWorkflowFrozen(listTotal); got != want || !v.Batch.Visible() {
		t.Fatalf("状态条应说冻结了多少项：%q，期望 %q", got, want)
	}
	if len(v.Batch.Results) != 0 || v.Batch.Command != nil {
		t.Fatalf("冻结不产生逐条结果：%+v", v.Batch)
	}
}

// 冻结用的是当前的合集、标签、平台与去空白的搜索词；回收站范围冻结回收站。
func TestSelectAllMatchingCarriesFacetsAndTrimmedSearch(t *testing.T) {
	h := batchHarness(t)
	h.store.SelectCollection(h.ctx, "c1")
	h.store.SelectTag(h.ctx, "t1")
	h.store.SelectPlatform(h.ctx, "bilibili")
	h.store.SetSearch(h.ctx, "  条目  ")
	h.store.SelectAllMatching(h.ctx)
	q := h.svc.Freezes()[0]
	if q.CollectionID != "c1" || q.TagID != "t1" || q.Platform != "bilibili" || q.Search != "条目" {
		t.Fatalf("冻结应带筛选与去空白的搜索词：%+v", q)
	}
}

// 冻结期间筛选变了：冻结的是旧筛选的结果，不能当成新筛选的选择——失败并留下原因，选择保持被筛选变化清空之后的样子。
func TestSelectAllMatchingRefusesAStaleFreeze(t *testing.T) {
	h := batchHarness(t)
	release := h.svc.HoldFreeze()
	done := make(chan bool)
	go func() { done <- h.store.SelectAllMatching(h.ctx) }()
	waitFor(t, "冻结发出", func() bool { return len(h.svc.Freezes()) == 1 })

	h.store.SetScope(h.ctx, services.LibraryScopeFavorites)
	release()
	if ok := <-done; ok {
		t.Fatal("筛选已变化，晚到的冻结不应被接受")
	}
	if sel := h.store.View().List.Selection; len(sel) != 0 {
		t.Fatalf("不应带着旧筛选的选择：%d 条", len(sel))
	}
	if !h.logged(locales.LibraryActionSelectAllMatching(), "筛选已变化，请重新选择") {
		t.Fatalf("失败应留痕：%v", h.log.entries())
	}
}

// 冻结失败走变更失败通道：返回 false、留日志，选择不变。
func TestSelectAllMatchingFailureIsReported(t *testing.T) {
	h := batchHarness(t)
	h.store.ToggleSelection("item-000001")
	h.svc.FailOn("Freeze", errors.New("数据库繁忙"))
	if h.store.SelectAllMatching(h.ctx) {
		t.Fatal("冻结失败应返回 false")
	}
	if !h.logged(locales.LibraryActionSelectAllMatching(), "数据库繁忙") {
		t.Fatalf("失败应留痕：%v", h.log.entries())
	}
	if sel := h.store.View().List.Selection; !slices.Equal(sel, []string{"item-000001"}) {
		t.Fatalf("失败不应改动选择：%v", sel)
	}
}

// ---- 筛选变化的提示 ----

// 带着多选改筛选或搜索词：选择随之清空，状态条给一句话；没有多选或筛选没变就不提示。
func TestFilterChangeWithSelectionLeavesANotice(t *testing.T) {
	h := batchHarness(t)
	want := locales.LibraryWorkflowSelectionCleared()

	h.store.SetScope(h.ctx, services.LibraryScopeFavorites) // 没有多选：不提示
	if got := h.batch().Notice; got != "" {
		t.Fatalf("没有多选时不应提示：%q", got)
	}
	h.store.ToggleSelection("item-000000")
	h.store.SetScope(h.ctx, services.LibraryScopeFavorites) // 筛选没变：不提示，但选择照常清空
	if got := h.batch().Notice; got != "" {
		t.Fatalf("筛选没变时不应提示：%q", got)
	}
	h.store.ToggleSelection("item-000000")
	h.store.SetScope(h.ctx, services.LibraryScopeAll)
	if got := h.batch().Notice; got != want {
		t.Fatalf("改范围应提示：%q", got)
	}

	h.store.DismissBatch()
	h.store.ToggleSelection("item-000001")
	h.store.SetSearch(h.ctx, "条目") // 搜索词变了
	if got := h.batch().Notice; got != want {
		t.Fatalf("改搜索词应提示：%q", got)
	}
	h.store.DismissBatch()
	h.store.ToggleSelection("item-000001")
	h.store.SetSearch(h.ctx, "条目") // 搜索词没变
	if got := h.batch().Notice; got != "" {
		t.Fatalf("搜索词没变不应提示：%q", got)
	}
}

// ---- 批量执行 ----

// 单条移到回收站成功：不弹结果栏（撤销提示由调用方给），选择清空，列表已刷新，回执键非空。
func TestSingleTrashSucceedsQuietly(t *testing.T) {
	h := batchHarness(t)
	if !h.store.MoveToTrash(h.ctx, []string{"item-000001"}) {
		t.Fatal("应成功")
	}
	v := h.store.View()
	if v.Batch.Visible() || v.Batch.Busy {
		t.Fatalf("单条成功不应有状态条：%+v", v.Batch)
	}
	if slices.Contains(entryIDs(v.List), "item-000001") || v.List.Total != listTotal-1 {
		t.Fatalf("列表应已刷新：总数 %d", v.List.Total)
	}
	calls := h.svc.Batches()
	if len(calls) != 1 || calls[0].RunID == "" || calls[0].Command.Kind != services.LibraryBatchTrash || !slices.Equal(calls[0].IDs, []string{"item-000001"}) {
		t.Fatalf("批量调用：%+v", calls)
	}
}

// 单条失败仍要给结果栏：这时没有别的提示。
func TestSingleFailureStillShowsTheStrip(t *testing.T) {
	h := batchHarness(t)
	h.svc.FailBatchItem("item-000001", "磁盘已满")
	if h.store.MoveToTrash(h.ctx, []string{"item-000001"}) {
		t.Fatal("失败应返回 false")
	}
	b := h.batch()
	if b.Notice != "成功 0 项，失败 1 项" || len(b.Results) != 1 || b.Results[0].OK || b.Results[0].Error != "磁盘已满" {
		t.Fatalf("状态条：%+v", b)
	}
}

// 批量：逐条记账；汇总一句话；选择只留失败的条目；成功的已离开列表。
func TestBulkTrashSummarisesAndKeepsFailuresSelected(t *testing.T) {
	h := batchHarness(t)
	ids := []string{"item-000001", "item-000002", "item-000003"}
	h.svc.FailBatchItem("item-000002", "磁盘已满")
	h.store.SetSelection(ids)

	if h.store.MoveToTrash(h.ctx, ids) {
		t.Fatal("有失败时应返回 false")
	}
	v := h.store.View()
	b := v.Batch
	if b.Notice != "成功 2 项，失败 1 项" || b.Busy || len(b.Results) != 3 {
		t.Fatalf("状态条：%+v", b)
	}
	if r, _ := resultFor(b, "item-000002"); r.OK || r.Error != "磁盘已满" {
		t.Fatalf("失败行：%+v", r)
	}
	if r, _ := resultFor(b, "item-000001"); !r.OK {
		t.Fatalf("成功行：%+v", r)
	}
	if b.Command == nil || b.Command.Kind != services.LibraryBatchTrash || !slices.Equal(b.Failed(), []string{"item-000002"}) {
		t.Fatalf("命令与失败项：%+v / %v", b.Command, b.Failed())
	}
	if !slices.Equal(v.List.Selection, []string{"item-000002"}) {
		t.Fatalf("选择应只留失败项：%v", v.List.Selection)
	}
	if got := entryIDs(v.List); slices.Contains(got, "item-000001") || !slices.Contains(got, "item-000002") {
		t.Fatalf("成功的应离开列表、失败的留着：%v", got[:5])
	}
}

// 仅重试失败项：同一个批次（回执键相同）、同一个命令，只带失败的条目；结果行按 ID 原地替换，汇总按整批算。
func TestRetryFailedReusesTheRunAndOnlySendsFailures(t *testing.T) {
	h := batchHarness(t)
	ids := []string{"item-000001", "item-000002", "item-000003"}
	h.svc.FailBatchItem("item-000002", "磁盘已满")
	h.store.SetSelection(ids)
	h.store.MoveToTrash(h.ctx, ids)

	h.svc.FailBatchItem("item-000002", "")
	if !h.store.RetryFailed(h.ctx) {
		t.Fatal("重试应成功")
	}
	calls := h.svc.Batches()
	if len(calls) != 2 || calls[1].RunID != calls[0].RunID || !slices.Equal(calls[1].IDs, []string{"item-000002"}) ||
		calls[1].Command.Kind != services.LibraryBatchTrash {
		t.Fatalf("重试应复用批次、只带失败项：%+v", calls)
	}
	b := h.batch()
	if b.Notice != "成功 3 项，失败 0 项" || len(b.Results) != 3 || len(b.Failed()) != 0 {
		t.Fatalf("重试后整批都成功：%+v", b)
	}
	if sel := h.store.View().List.Selection; len(sel) != 0 {
		t.Fatalf("没有失败项就不该留选择：%v", sel)
	}
}

// 没有上一批（或没有命令）时重试什么也不做。
func TestRetryFailedWithoutABatchDoesNothing(t *testing.T) {
	h := batchHarness(t)
	if h.store.RetryFailed(h.ctx) || len(h.svc.Batches()) != 0 {
		t.Fatal("没有上一批时重试不应发请求")
	}
}

// 每块 50 条：120 条是 50、50、20 三次调用，同一个回执键；结果逐块并入，汇总按总数。
func TestBatchSendsChunksOfFifty(t *testing.T) {
	h := newHarness(t)
	h.svc.SeedEntries(servicestest.GenerateEntries(200))
	h.store.Load(h.ctx)
	var ids []string
	for _, e := range h.svc.EntriesNow() {
		if e.DeletedAt == 0 && len(ids) < 120 {
			ids = append(ids, e.ID)
		}
	}
	if !h.store.SetStatus(h.ctx, ids, services.LibraryStatusArchived) {
		t.Fatal("应成功")
	}
	calls := h.svc.Batches()
	if len(calls) != 3 || len(calls[0].IDs) != 50 || len(calls[1].IDs) != 50 || len(calls[2].IDs) != 20 {
		t.Fatalf("应分三块：%d 次", len(calls))
	}
	if calls[0].RunID == "" || calls[1].RunID != calls[0].RunID || calls[2].RunID != calls[0].RunID {
		t.Fatalf("同一批次的回执键应相同：%q %q %q", calls[0].RunID, calls[1].RunID, calls[2].RunID)
	}
	if calls[0].Command.Kind != services.LibraryBatchStatus || calls[0].Command.Status != services.LibraryStatusArchived {
		t.Fatalf("命令：%+v", calls[0].Command)
	}
	if b := h.batch(); len(b.Results) != 120 || b.Notice != "成功 120 项，失败 0 项" {
		t.Fatalf("汇总：%q，%d 行", b.Notice, len(b.Results))
	}
}

// 重复的 ID 只处理一次，顺序保持。
func TestBatchDeduplicatesIDs(t *testing.T) {
	h := batchHarness(t)
	h.store.SetStatus(h.ctx, []string{"item-000002", "item-000001", "item-000002"}, services.LibraryStatusArchived)
	calls := h.svc.Batches()
	if len(calls) != 1 || !slices.Equal(calls[0].IDs, []string{"item-000002", "item-000001"}) {
		t.Fatalf("应去重并保持先后：%+v", calls)
	}
}

// 同一时刻只跑一批：进行中第二批直接返回 false、不发请求；进行中状态条显示已核对的数目。
func TestABusyBatchRejectsASecondOne(t *testing.T) {
	h := batchHarness(t)
	release := h.svc.HoldBatch()
	done := make(chan bool)
	go func() { done <- h.store.MoveToTrash(h.ctx, []string{"item-000001", "item-000002"}) }()
	waitFor(t, "批量发出", func() bool { return len(h.svc.Batches()) == 1 })

	if !h.batch().Busy {
		t.Fatal("批量进行中应为忙")
	}
	if h.store.MoveToTrash(h.ctx, []string{"item-000003"}) || len(h.svc.Batches()) != 1 {
		t.Fatal("忙时第二批应被拒绝且不发请求")
	}
	release()
	if !<-done {
		t.Fatal("第一批应成功")
	}
	if h.batch().Busy {
		t.Fatal("结束后不应再忙")
	}
}

// 整块请求失败（连接中断等）：块里每一条都记同一个原因，写法同 String(error)；不弹整批失败的提示（渲染层同）。
func TestChunkErrorBecomesPerItemFailures(t *testing.T) {
	h := batchHarness(t)
	h.svc.FailOn("ExecuteBatch", errors.New("连接中断"))
	ids := []string{"item-000001", "item-000002"}
	if h.store.MoveToTrash(h.ctx, ids) {
		t.Fatal("应失败")
	}
	b := h.batch()
	if b.Notice != "成功 0 项，失败 2 项" || len(b.Results) != 2 {
		t.Fatalf("状态条：%+v", b)
	}
	for _, r := range b.Results {
		if r.OK || r.Error != "Error: 连接中断" {
			t.Fatalf("每条都应记同一个原因：%+v", r)
		}
	}
	if len(h.log.entries()) != 0 {
		t.Fatalf("逐条失败不该写成整批失败的日志：%v", h.log.entries())
	}
}

// shortLibrary 在回复里丢掉最后一条结果，模拟后端漏报。
type shortLibrary struct{ *servicestest.Library }

func (s shortLibrary) ExecuteBatch(ctx context.Context, runID string, ids []string, cmd services.LibraryBatchCommand) ([]services.LibraryBatchResult, error) {
	results, err := s.Library.ExecuteBatch(ctx, runID, ids, cmd)
	if len(results) > 0 {
		results = results[:len(results)-1]
	}
	return results, err
}

// 回复里缺了某条：记「未返回此条结果」。
func TestMissingResultRowIsMarked(t *testing.T) {
	h := batchHarness(t)
	h.store.svc = shortLibrary{h.svc}
	h.store.SetStatus(h.ctx, []string{"item-000001", "item-000002"}, services.LibraryStatusArchived)
	r, ok := resultFor(h.batch(), "item-000002")
	if !ok || r.OK || r.Error != locales.LibraryWorkflowNoResult() {
		t.Fatalf("缺的那条应标为未返回：%+v", r)
	}
}

// 批量进行中筛选变了：选择已被筛选变化清掉，批后不能再把旧筛选的失败项塞回去。
func TestBatchDoesNotRestoreSelectionAfterTheFilterChanged(t *testing.T) {
	h := batchHarness(t)
	ids := []string{"item-000001", "item-000002"}
	h.svc.FailBatchItem("item-000002", "磁盘已满")
	h.store.SetSelection(ids)
	release := h.svc.HoldBatch()
	done := make(chan bool)
	go func() { done <- h.store.MoveToTrash(h.ctx, ids) }()
	waitFor(t, "批量发出", func() bool { return len(h.svc.Batches()) == 1 })

	h.store.SetScope(h.ctx, services.LibraryScopeFavorites)
	release()
	<-done
	if sel := h.store.View().List.Selection; len(sel) != 0 {
		t.Fatalf("筛选变了就不动选择：%v", sel)
	}
	if b := h.batch(); len(b.Results) != 2 || b.Failed()[0] != "item-000002" {
		t.Fatalf("状态条仍应记着这一批：%+v", b)
	}
}

// 批后的选中项：成功移到回收站、恢复、删除之后条目已离开当前视图，取消选中；失败或别的命令（归档）保持选中。
func TestSelectedItemAfterABatch(t *testing.T) {
	h := batchHarness(t)
	h.store.SelectItem("item-000001")
	h.store.MoveToTrash(h.ctx, []string{"item-000001"})
	if got := h.store.View().List.SelectedID; got != "" {
		t.Fatalf("成功移到回收站后应取消选中：%q", got)
	}

	h.store.SelectItem("item-000002")
	h.svc.FailBatchItem("item-000002", "磁盘已满")
	h.store.MoveToTrash(h.ctx, []string{"item-000002"})
	if got := h.store.View().List.SelectedID; got != "item-000002" {
		t.Fatalf("失败时应保持选中：%q", got)
	}

	h.svc.FailBatchItem("item-000002", "")
	h.store.SetStatus(h.ctx, []string{"item-000002", "item-000003"}, services.LibraryStatusActive)
	if got := h.store.View().List.SelectedID; got != "item-000002" {
		t.Fatalf("改状态不会让条目离开视图的命令不应取消选中：%q", got)
	}

	h.store.SelectItem("item-000004") // 选中的不在这批里：不动
	h.store.MoveToTrash(h.ctx, []string{"item-000005"})
	if got := h.store.View().List.SelectedID; got != "item-000004" {
		t.Fatalf("不在这批里的选中项不该动：%q", got)
	}
}

// 收起：一句话与逐条结果都清掉，命令保留（渲染层 dismiss 只清 notice 与 results）。
func TestDismissClearsTheStrip(t *testing.T) {
	h := batchHarness(t)
	h.svc.FailBatchItem("item-000001", "x")
	h.store.MoveToTrash(h.ctx, []string{"item-000001", "item-000002"})
	if !h.batch().Visible() {
		t.Fatal("应有状态条")
	}
	h.store.DismissBatch()
	if b := h.batch(); b.Visible() || b.Command == nil {
		t.Fatalf("收起后不可见、命令仍在：%+v", b)
	}
}

// ---- 回收站相关 ----

// 彻底删除只对回收站里的条目有效：其余逐条失败，原因同服务端。
func TestDeleteForeverRefusesItemsOutsideTheTrash(t *testing.T) {
	h := batchHarness(t)
	if h.store.DeleteForever(h.ctx, []string{"item-000001"}, false) {
		t.Fatal("不在回收站的条目不能彻底删除")
	}
	b := h.batch()
	if b.Notice != "成功 0 项，失败 1 项" || b.Results[0].Error != "只能彻底删除回收站中的条目" {
		t.Fatalf("状态条：%+v", b)
	}
}

// 单条彻底删除成功：不弹结果栏；带「清证据」时通知问答侧、且只通知真正删掉的。
func TestDeleteForeverClearsEvidenceOfDeletedOnly(t *testing.T) {
	h := batchHarness(t)
	var cleared []string
	h.store.OnEvidenceCleared = func(ids []string) { cleared = append(cleared, ids...) }
	h.store.SetScope(h.ctx, services.LibraryScopeTrash)
	h.svc.FailBatchItem("item-000039", "文件被占用")
	ok := h.store.DeleteForever(h.ctx, []string{"item-000019", "item-000039"}, true)
	if ok {
		t.Fatal("有失败应返回 false")
	}
	if !slices.Equal(cleared, []string{"item-000019"}) {
		t.Fatalf("只应清掉真正删除了的条目的证据：%v", cleared)
	}
	if !slices.Equal(h.svc.ClearedEvidence(), []string{"item-000019"}) {
		t.Fatalf("服务端记录：%v", h.svc.ClearedEvidence())
	}

	h.store.DismissBatch()
	cleared = nil
	if !h.store.DeleteForever(h.ctx, []string{"item-000059"}, false) {
		t.Fatal("单条删除应成功")
	}
	if h.batch().Visible() || len(cleared) != 0 {
		t.Fatalf("单条成功不弹结果栏；没选清证据就不通知：%+v / %v", h.batch(), cleared)
	}
}

// 清空回收站：先冻结回收站再逐条彻底删除；多条给汇总。
func TestEmptyTrashFreezesTheTrashThenDeletes(t *testing.T) {
	h := batchHarness(t)
	var cleared []string
	h.store.OnEvidenceCleared = func(ids []string) { cleared = append(cleared, ids...) }
	if !h.store.EmptyTrash(h.ctx, true) {
		t.Fatal("应成功")
	}
	if qs := h.svc.Freezes(); len(qs) != 1 || qs[0].Scope != services.LibraryScopeTrash {
		t.Fatalf("应冻结回收站：%+v", qs)
	}
	calls := h.svc.Batches()
	if len(calls) != 1 || calls[0].Command.Kind != services.LibraryBatchDelete || !calls[0].Command.ClearEvidence || len(calls[0].IDs) != 5 {
		t.Fatalf("批量调用：%+v", calls)
	}
	if len(cleared) != 5 {
		t.Fatalf("清证据通知：%v", cleared)
	}
	if b := h.batch(); b.Notice != "成功 5 项，失败 0 项" {
		t.Fatalf("汇总：%q", b.Notice)
	}
	for _, e := range h.svc.EntriesNow() {
		if e.DeletedAt != 0 {
			t.Fatalf("回收站应已清空：%s", e.ID)
		}
	}
}

// 冻结回收站失败：走变更失败通道（动作名是「清空回收站」），不发批量。
func TestEmptyTrashFreezeFailureIsReported(t *testing.T) {
	h := batchHarness(t)
	h.svc.FailOn("Freeze", errors.New("读取失败"))
	if h.store.EmptyTrash(h.ctx, false) || len(h.svc.Batches()) != 0 {
		t.Fatal("冻结失败不应继续")
	}
	if !h.logged(locales.LibraryEmptyTrash(), "读取失败") {
		t.Fatalf("应留痕：%v", h.log.entries())
	}
}

// 恢复：回收站里的条目回到工作区。
func TestRestoreItemsLeavesTheTrash(t *testing.T) {
	h := batchHarness(t)
	h.store.SetScope(h.ctx, services.LibraryScopeTrash)
	if !h.store.RestoreItems(h.ctx, []string{"item-000019"}) {
		t.Fatal("应成功")
	}
	if got := entryIDs(h.store.View().List); slices.Contains(got, "item-000019") {
		t.Fatalf("恢复后应离开回收站：%v", got)
	}
	if calls := h.svc.Batches(); len(calls) != 1 || calls[0].Command.Kind != services.LibraryBatchRestore {
		t.Fatalf("批量调用：%+v", calls)
	}
}

// ---- 批量改字段 ----

// 移到合集：补丁里的合集是指针，空串表示移出合集（线上是 null）；追加标签后重读标签列表，不论这一批成败。
func TestBulkUpdatePatches(t *testing.T) {
	h := batchHarness(t)
	h.store.BulkMoveToCollection(h.ctx, []string{"item-000001"}, "c1")
	h.store.BulkMoveToCollection(h.ctx, []string{"item-000001"}, "")
	calls := h.svc.Batches()
	if len(calls) != 2 || calls[0].Command.Patch == nil || calls[0].Command.Patch.CollectionID == nil || *calls[0].Command.Patch.CollectionID != "c1" {
		t.Fatalf("移到合集：%+v", calls)
	}
	if p := calls[1].Command.Patch; p == nil || p.CollectionID == nil || *p.CollectionID != "" {
		t.Fatalf("移出合集应是指向空串的指针：%+v", calls[1].Command.Patch)
	}
	if got := h.svc.EntriesNow(); got[1].CollectionID != "" {
		t.Fatalf("最终应未归合集：%q", got[1].CollectionID)
	}

	tagCalls := func() int { return countCalls(h.svc.Calls(), "Tags") }
	n := tagCalls()
	h.store.BulkUpdate(h.ctx, []string{"item-000001"}, services.LibraryEntryPatch{AddTagNames: []string{"新标签"}})
	if tagCalls() != n+1 {
		t.Fatalf("追加标签后应重读标签列表：%d → %d", n, tagCalls())
	}
	h.svc.FailBatchItem("item-000002", "x")
	n = tagCalls()
	h.store.BulkUpdate(h.ctx, []string{"item-000002"}, services.LibraryEntryPatch{AddTagNames: []string{"另一个"}})
	if tagCalls() != n+1 {
		t.Fatalf("失败时也应重读标签列表（渲染层同）：%d → %d", n, tagCalls())
	}
	n = tagCalls()
	h.store.BulkUpdate(h.ctx, []string{"item-000001"}, services.LibraryEntryPatch{IsFavorite: services.BoolPtr(true)})
	if tagCalls() != n {
		t.Fatal("没有追加标签时不该重读标签列表")
	}
}

func countCalls(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}

// ---- 单条 ----

// 翻转收藏：本地行先回显，再重取列表与计数；在「收藏」范围里取消收藏，条目随重取离开列表。
func TestToggleFavoriteEchoesThenRefreshes(t *testing.T) {
	h := batchHarness(t)
	rev := h.store.View().List.EntriesRev
	if !h.store.ToggleFavorite(h.ctx, "item-000001") {
		t.Fatal("应成功")
	}
	e, _ := findEntry(h.store.View().List, "item-000001")
	if !e.IsFavorite {
		t.Fatal("行上应已是收藏")
	}
	if h.store.View().List.EntriesRev <= rev {
		t.Fatal("Entries 应整体替换（EntriesRev 增加）")
	}
	calls := h.svc.UpdateCalls()
	if len(calls) != 1 || calls[0].Patch.IsFavorite == nil || !*calls[0].Patch.IsFavorite || calls[0].Patch.IsPinned != nil {
		t.Fatalf("补丁只含收藏：%+v", calls)
	}

	h.store.SetScope(h.ctx, services.LibraryScopeFavorites)
	if !slices.Contains(entryIDs(h.store.View().List), "item-000001") {
		t.Fatal("收藏范围里应有它")
	}
	h.store.ToggleFavorite(h.ctx, "item-000001")
	if slices.Contains(entryIDs(h.store.View().List), "item-000001") {
		t.Fatal("在收藏范围里取消收藏，条目应随重取离开列表")
	}
}

func findEntry(l ListView, id string) (services.LibraryEntry, bool) {
	for _, e := range l.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return services.LibraryEntry{}, false
}

// 置顶：补丁只含置顶；已置顶的翻成取消。
func TestTogglePinnedFlipsTheCurrentValue(t *testing.T) {
	h := batchHarness(t)
	pinned, _ := findEntry(h.store.View().List, "item-000000")
	if !pinned.IsPinned {
		t.Fatal("夹具里 item-000000 应已置顶")
	}
	h.store.TogglePinned(h.ctx, "item-000000")
	calls := h.svc.UpdateCalls()
	if len(calls) != 1 || calls[0].Patch.IsPinned == nil || *calls[0].Patch.IsPinned || calls[0].Patch.IsFavorite != nil {
		t.Fatalf("应发取消置顶：%+v", calls)
	}
}

// 连点两次：翻转值以在途请求为准——第二次发的是第一次的反面，不是同一个值发两遍。
func TestToggleFlagTwiceUsesTheInflightValue(t *testing.T) {
	h := batchHarness(t)
	release := h.svc.HoldUpdate()
	done := make(chan bool)
	go func() { done <- h.store.ToggleFavorite(h.ctx, "item-000001") }()
	waitFor(t, "第一次更新发出", func() bool { return len(h.svc.UpdateCalls()) == 1 })

	if !h.store.ToggleFavorite(h.ctx, "item-000001") {
		t.Fatal("第二次应成功")
	}
	release()
	<-done
	calls := h.svc.UpdateCalls()
	if len(calls) != 2 || !*calls[0].Patch.IsFavorite || *calls[1].Patch.IsFavorite {
		t.Fatalf("两次应发相反的值：%+v", calls)
	}
	h.store.mu.Lock()
	left := len(h.store.pendingFlags)
	h.store.mu.Unlock()
	if left != 0 {
		t.Fatalf("在途记录应已清掉：%d", left)
	}
}

// 翻转失败走变更失败通道（动作名「更新标记」），行保持原样。
func TestToggleFlagFailureIsReported(t *testing.T) {
	h := batchHarness(t)
	h.svc.FailOn("UpdateEntry", errors.New("数据库繁忙"))
	if h.store.ToggleFavorite(h.ctx, "item-000001") {
		t.Fatal("失败应返回 false")
	}
	if !h.logged(locales.LibraryActionSetFlag(), "数据库繁忙") {
		t.Fatalf("应留痕：%v", h.log.entries())
	}
	if e, _ := findEntry(h.store.View().List, "item-000001"); e.IsFavorite {
		t.Fatal("失败后行不该变")
	}
}

// 整体替换标签：新标签名建出新标签，行与侧栏标签列表、计数都跟上；空列表是清空。
func TestSetItemTagsReplacesAndRefreshes(t *testing.T) {
	h := batchHarness(t)
	h.svc.Seed(services.LibraryCounts{All: 80}, nil, []services.LibraryTag{{ID: "t1", Name: "go", ColorKey: "blue"}})
	h.store.FetchTags(h.ctx)
	counts := len(h.svc.CountQueries())

	if !h.store.SetItemTags(h.ctx, "item-000001", []string{"go", "新标签"}) {
		t.Fatal("应成功")
	}
	e, _ := findEntry(h.store.View().List, "item-000001")
	if names := tagNames(e.Tags); !slices.Equal(names, []string{"go", "新标签"}) {
		t.Fatalf("行上的标签：%v", names)
	}
	if got := tagNames(h.store.View().Tags); !slices.Contains(got, "新标签") {
		t.Fatalf("侧栏标签应含新建的：%v", got)
	}
	if len(h.svc.CountQueries()) <= counts {
		t.Fatal("改标签后应重读计数")
	}

	h.store.SetItemTags(h.ctx, "item-000001", nil)
	if e, _ := findEntry(h.store.View().List, "item-000001"); len(e.Tags) != 0 {
		t.Fatalf("空应是清空：%v", tagNames(e.Tags))
	}
	calls := h.svc.UpdateCalls()
	if p := calls[len(calls)-1].Patch; p.TagNames == nil || len(p.TagNames) != 0 {
		t.Fatalf("清空的补丁应是空切片而不是 nil（nil 表示不改）：%#v", p.TagNames)
	}
}

func tagNames(tags []services.LibraryTag) []string {
	out := make([]string, len(tags))
	for i, tag := range tags {
		out[i] = tag.Name
	}
	return out
}

// 改标签失败走变更失败通道（动作名「更新标签」）。
func TestSetItemTagsFailureIsReported(t *testing.T) {
	h := batchHarness(t)
	h.svc.FailOn("UpdateEntry", errors.New("数据库繁忙"))
	if h.store.SetItemTags(h.ctx, "item-000001", []string{"x"}) || !h.logged(locales.LibraryActionSetTags(), "数据库繁忙") {
		t.Fatalf("应失败并留痕：%v", h.log.entries())
	}
}

// 补采公众号快照：按 50 一块；某块失败就停，返回 false 并留痕。
func TestSupplementWechatSendsChunksAndStopsOnFailure(t *testing.T) {
	h := batchHarness(t)
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = fmt.Sprintf("item-%06d", i)
	}
	if !h.store.SupplementWechat(h.ctx, ids) {
		t.Fatal("应成功")
	}
	got := h.svc.Supplemented()
	if len(got) != 3 || len(got[0]) != 50 || len(got[1]) != 50 || len(got[2]) != 20 {
		t.Fatalf("应分三块：%d", len(got))
	}

	h.svc.FailOn("SupplementWechat", errors.New("队列已满"))
	if h.store.SupplementWechat(h.ctx, ids) || !h.logged(locales.LibrarySnapshotSupplementWechatBulk(), "队列已满") {
		t.Fatalf("应失败并留痕：%v", h.log.entries())
	}
	if n := len(h.svc.Supplemented()); n != 3 {
		t.Fatalf("失败的那块不该记入：%d", n)
	}
}

// ---- 交接稿 ----

// 复制：完整版与精简版各取各的；文字原样进剪贴板。
func TestCopyAiHandoffFullAndBrief(t *testing.T) {
	h := batchHarness(t)
	var copied []string
	var mu sync.Mutex
	h.store.guard.Notify.Clipboard = func(text string) error {
		mu.Lock()
		copied = append(copied, text)
		mu.Unlock()
		return nil
	}
	h.svc.SeedHandoff("item-000001",
		services.LibraryHandoff{Title: "标题", Text: "完整正文", CharCount: 4},
		services.LibraryHandoff{Title: "标题", Text: "精简正文", CharCount: 4, OmittedChars: 1200})
	h.store.CopyAiHandoff(h.ctx, "item-000001", true)
	h.store.CopyAiHandoff(h.ctx, "item-000001", false)
	if !slices.Equal(copied, []string{"完整正文", "精简正文"}) {
		t.Fatalf("剪贴板内容：%q", copied)
	}
}

// 条目不存在、内容为空、剪贴板写失败：都不写剪贴板（后者除外）、不 panic，也不写日志（渲染层同：只弹提示）。
func TestCopyAiHandoffFailuresDoNotTouchTheClipboard(t *testing.T) {
	h := batchHarness(t)
	writes := 0
	h.store.guard.Notify.Clipboard = func(string) error { writes++; return nil }
	h.store.CopyAiHandoff(h.ctx, "item-不存在", true)
	h.svc.FailHandoff("item-000001", services.ErrHandoffEmpty)
	h.store.CopyAiHandoff(h.ctx, "item-000001", true)
	h.svc.FailHandoff("item-000001", errors.New("读取失败"))
	h.store.CopyAiHandoff(h.ctx, "item-000001", true)
	if writes != 0 {
		t.Fatalf("失败时不该写剪贴板：%d 次", writes)
	}
	h.svc.FailHandoff("item-000001", nil)
	h.store.guard.Notify.Clipboard = func(string) error { return errors.New("剪贴板被占用") }
	h.store.CopyAiHandoff(h.ctx, "item-000001", true) // 不 panic
	if len(h.log.entries()) != 0 {
		t.Fatalf("交接稿的失败只弹提示、不写日志：%v", h.log.entries())
	}
}

// 另存始终给完整版并带标题；取消对话框不提示、不报错。
func TestSaveAiHandoffExportsTheFullText(t *testing.T) {
	h := batchHarness(t)
	h.svc.SeedHandoff("item-000001",
		services.LibraryHandoff{Title: "标题", Text: "完整正文", CharCount: 4},
		services.LibraryHandoff{Title: "标题", Text: "精简正文", CharCount: 4, OmittedChars: 9})
	h.store.SaveAiHandoff(h.ctx, "item-000001")
	if ex := h.svc.Exports(); len(ex) != 1 || ex[0].Title != "标题" || ex[0].Text != "完整正文" {
		t.Fatalf("另存的请求：%+v", ex)
	}
	h.svc.SetExportResult(services.LibraryExportResult{Canceled: true})
	h.store.SaveAiHandoff(h.ctx, "item-000001")
	h.svc.SetExportResult(services.LibraryExportResult{Error: "磁盘已满"})
	h.store.SaveAiHandoff(h.ctx, "item-000001")
	if len(h.log.entries()) != 0 {
		t.Fatalf("不写日志：%v", h.log.entries())
	}
}

// 千分位：与渲染层 withThousands 同形。
func TestWithThousands(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 8432: "8,432", 123456: "123,456", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := withThousands(in); got != want {
			t.Errorf("withThousands(%d) = %q，期望 %q", in, got, want)
		}
	}
}
