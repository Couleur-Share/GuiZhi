package library

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测批量结果条里「逐条结果」清单的行窗口（台账 B-056）：渲染层把全部结果整表渲染，Viva 一侧只声明视口附近的行，
// 否则一次「全部符合筛选」的批量（上万条结果）展开之后每帧要几百毫秒。窗口只能改变「声明哪些行」，不能改变看得见的东西：
// 滚得到每一条、过长的失败原因折行之后其余行的位置不乱、折叠与展开照旧。

// resultsRig 装好 total 条夹具、全部符合筛选之后批量收藏（failures 里的条目失败，原因是给定的文字），并展开「逐条结果」。
func resultsRig(t *testing.T, total int, failures map[int]string) *rig {
	t.Helper()
	r := wsRig(t, func(r *rig) { r.svc.SeedEntries(servicestest.GenerateEntries(total)) })
	if !r.store.SelectAllMatching(r.screen.Ctx) {
		t.Fatal("全部符合筛选应成功")
	}
	r.frames(2)
	for i, reason := range failures {
		r.svc.FailBatchItem(r.view().List.Selection[i], reason)
	}
	r.click(gfx.RoleButton, locales.LibraryBulkFavorite())
	r.frames(2)
	label := locales.LibraryWorkflowResults(len(r.view().Batch.Results))
	r.click(gfx.RoleButton, label)
	r.frames(3)
	if !r.mustNode(gfx.RoleButton, label).Expanded {
		t.Fatal("「逐条结果」应已展开")
	}
	return r
}

// resultsScroll 在清单里滚一步（指针落在「逐条结果」那一行下面的清单上），随后出两帧让窗口跟上。
func resultsScroll(t *testing.T, r *rig, dy float32) {
	t.Helper()
	summary := r.mustNode(gfx.RoleButton, locales.LibraryWorkflowResults(len(r.view().Batch.Results)))
	x, y := centerOf(summary)
	if err := r.session.Scroll(gfx.ScrollEvent{X: x, Y: y + 40, DeltaY: dy, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	r.frames(2)
}

// idYs 是画面上这些编号文字各自的纵坐标（没画出来的不在表里）。
func idYs(r *rig, ids []string) map[string]float32 {
	out := map[string]float32{}
	for _, tx := range r.session.Snapshot().Texts {
		if slices.Contains(ids, tx.Value) {
			out[tx.Value] = tx.Bounds.Y
		}
	}
	return out
}

// 一千条结果展开之后只声明视口附近的行：清单只有 9rem 高，画出的行数是个位数加上下余量，与结果总数无关；
// 第一条看得见，最后一条没有画出来。
func TestBatchResultsListDeclaresOnlyTheRowsNearTheViewport(t *testing.T) {
	t.Parallel()
	r := resultsRig(t, 1250, nil)
	results := r.view().Batch.Results
	if len(results) != 1000 {
		t.Fatalf("应有 1000 条结果，实际 %d", len(results))
	}
	declared := 0
	for _, tx := range r.session.Snapshot().Texts {
		if tx.Value == locales.CommonSuccess() {
			declared++
		}
	}
	if declared < 8 || declared > 30 {
		t.Fatalf("清单只有 9rem 高，应只声明视口附近的行（8…30 行），实际画出 %d 行", declared)
	}
	if !r.hasText(results[0].ID) || r.hasText(results[len(results)-1].ID) {
		t.Fatalf("第一条应看得见、最后一条不该画出来；画面文字前 40 项 %q", r.session.Texts()[:min(40, len(r.session.Texts()))])
	}
}

// 窗口里的行滚得到每一条：往下滚到底能看见最后一条（且落在清单的视口里），第一条滚出去了；往回滚到顶又是第一条。
func TestBatchResultsListScrollsFromTheFirstRowToTheLast(t *testing.T) {
	t.Parallel()
	r := resultsRig(t, 1250, nil)
	results := r.view().Batch.Results
	first, last := results[0].ID, results[len(results)-1].ID

	for i := 0; i < 60 && !r.hasText(last); i++ {
		resultsScroll(t, r, 2000)
	}
	if !r.hasText(last) || r.hasText(first) {
		t.Fatalf("滚到底之后应看见最后一条（%s）、第一条（%s）滚出去了", last, first)
	}
	summary := r.mustNode(gfx.RoleButton, locales.LibraryWorkflowResults(len(results)))
	top, height := summary.Bounds.Y+summary.Bounds.Height, r.screen.Look.Rem(9)
	y, ok := idYs(r, []string{last})[last]
	if !ok || y < top-0.5 || y > top+height+0.5 {
		t.Fatalf("最后一条应落在清单的视口里（%.1f…%.1f），实际在 %.1f", top, top+height, y)
	}

	for i := 0; i < 60 && !r.hasText(first); i++ {
		resultsScroll(t, r, -2000)
	}
	if !r.hasText(first) || r.hasText(last) {
		t.Fatal("滚回顶部之后应又看见第一条、最后一条滚出去了")
	}
}

// 过长的失败原因折成多行：这一行比别的行高，它下面的行接在它后面；滚出窗口再滚回来，各行的位置与第一次一模一样
// （量过的行高不会因为行离开窗口而丢掉，也不会被估计值顶替）。
func TestBatchResultsListKeepsAWrappedFailureRowIntact(t *testing.T) {
	t.Parallel()
	reason := strings.Repeat("磁盘已满，无法写入，", 40)
	r := resultsRig(t, 500, map[int]string{3: reason})
	results := r.view().Batch.Results
	ids := []string{results[2].ID, results[3].ID, results[4].ID, results[5].ID}
	line := r.screen.Look.Rem(1) // TextXS 的行高是 1rem

	before := idYs(r, ids)
	if len(before) != len(ids) {
		t.Fatalf("前几行应都画出来：%v", before)
	}
	near := func(got, want float32) bool { return got > want-0.5 && got < want+0.5 }
	if !near(before[ids[1]]-before[ids[0]], line) || !near(before[ids[3]]-before[ids[2]], line) {
		t.Fatalf("单行的行距应是一行（%.1f）：%v", line, before)
	}
	if before[ids[2]]-before[ids[1]] < 2*line {
		t.Fatalf("折成多行的失败行应至少两行高（%.1f），实际 %.1f", 2*line, before[ids[2]]-before[ids[1]])
	}

	for i := 0; i < 8; i++ { // 滚到前几行都滚出窗口，再滚回来
		resultsScroll(t, r, 600)
	}
	if len(idYs(r, ids)) != 0 {
		t.Fatalf("滚远之后前几行不该还声明着：%v", idYs(r, ids))
	}
	for i := 0; i < 8 && len(idYs(r, ids)) != len(ids); i++ {
		resultsScroll(t, r, -600)
	}
	for i := 0; i < 20; i++ { // 回到顶部
		resultsScroll(t, r, -600)
	}
	after := idYs(r, ids)
	for _, id := range ids {
		if !near(after[id], before[id]) {
			t.Fatalf("滚出窗口再滚回来，%s 的位置应不变：之前 %.1f，之后 %.1f", id, before[id], after[id])
		}
	}
}

// measuredOf 是行窗口里量过高度的行数与本帧声明的行数。
func measuredOf(r *rig) (measured, declared int) {
	w := &r.screen.bulk.results
	first, last := w.Declared()
	return w.Measured(), last - first
}

// 字号档变了（根字号 16 → 18）：行高随之变，滚过的那些行（早已不在窗口里）量过的高度作废，只剩本帧声明的行是量过的
// （窗口靠 Epoch.RootPx 重量；否则它们的旧高度会一直留着，窗口外的位置与滚动范围都按旧字号算）。
func TestBatchResultsListMeasuresTheRowsAgainWhenTheFontTierChanges(t *testing.T) {
	t.Parallel()
	r := resultsRig(t, 1250, nil)
	for i := 0; i < 8; i++ { // 一路滚下去，把滚过的行都量一遍
		resultsScroll(t, r, 120)
	}
	if measured, declared := measuredOf(r); measured < 3*declared {
		t.Fatalf("滚过的行应都量过：量过 %d 行，本帧声明 %d 行", measured, declared)
	}
	r.look.RootPx = 18
	r.frames(4)
	if measured, declared := measuredOf(r); measured > declared+4 {
		t.Fatalf("字号档变了之后，只有刚声明的行是新量的：量过 %d 行，本帧声明 %d 行", measured, declared)
	}
}

// 展开着重试失败项：失败的那一条从第 4 位挪到最后，它后面所有行的序号都往前顶了一位——同一个序号上现在是另一条结果，
// 旧结果量过的行高不能留给新的行（行的键变了，窗口作废它们）。
func TestBatchResultsListForgetsTheRowHeightsOfRowsThatShiftedAfterARetry(t *testing.T) {
	t.Parallel()
	r := resultsRig(t, 1250, map[int]string{3: "磁盘已满"})
	for i := 0; i < 8; i++ { // 一路滚下去，把滚过的行都量一遍
		resultsScroll(t, r, 120)
	}
	if measured, declared := measuredOf(r); measured < 3*declared {
		t.Fatalf("滚过的行应都量过：量过 %d 行，本帧声明 %d 行", measured, declared)
	}
	failed := r.view().Batch.Results[3].ID
	r.svc.FailBatchItem(failed, "")
	r.click(gfx.RoleButton, locales.LibraryWorkflowRetryFailed())
	r.frames(4)
	results := r.view().Batch.Results
	if len(results) != 1000 || results[len(results)-1].ID != failed || results[3].ID == failed {
		t.Fatalf("重试之后失败的那条应挪到最后：共 %d 条，第 4 位 %s，最后 %s", len(results), results[3].ID, results[len(results)-1].ID)
	}
	if !r.mustNode(gfx.RoleButton, locales.LibraryWorkflowResults(1000)).Expanded {
		t.Fatal("重试期间「逐条结果」应一直展开着")
	}
	if measured, declared := measuredOf(r); measured > declared+4 {
		t.Fatalf("序号挪动之后旧行高应作废：量过 %d 行，本帧声明 %d 行", measured, declared)
	}
}
