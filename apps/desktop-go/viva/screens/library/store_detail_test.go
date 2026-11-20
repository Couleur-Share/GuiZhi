package library

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 stores/knowledge.store.ts 的详情与草稿保存管线：selectItem 的读取与乱序保护、updateSelected 的本地回显与防抖、
// flushPendingSave、persistPatch 的失败与冲突、resolveSaveConflict、切条目与改筛选前的落盘（台账 B-033 (1)(2)）。
// 条目是三条小数据：a（带标签 go）、b、c；断言以「界面看到的」View().Detail 与「后端写进去的」假实现为准。

func detailHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	tags := []services.LibraryTag{{ID: "t1", Name: "go", ColorKey: "blue"}, {ID: "t2", Name: "rust", ColorKey: "red"}}
	h.svc.Seed(services.LibraryCounts{All: 3}, []services.LibraryCollection{{ID: "c1", Name: "工作"}}, tags)
	entry := func(id, title string, updated int64, ts ...services.LibraryTag) services.LibraryEntry {
		if ts == nil {
			ts = []services.LibraryTag{}
		}
		return services.LibraryEntry{
			ID: id, Title: title, ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
			ReviewStatus: "clear", ReviewReasons: []string{}, UpdatedAt: updated, CreatedAt: updated, Tags: ts,
		}
	}
	h.svc.SeedEntries([]services.LibraryEntry{entry("a", "甲", 3000, tags[0]), entry("b", "乙", 2000), entry("c", "丙", 1000)})
	h.svc.SeedDetail("a", servicestest.DetailBody{Content: "甲的正文"})
	h.svc.SeedDetail("b", servicestest.DetailBody{Content: "乙的正文"})
	h.svc.SeedDetail("c", servicestest.DetailBody{Content: "丙的正文"})
	h.store.Load(h.ctx)
	return h
}

func (h *harness) detail() DetailView { return h.store.View().Detail }

// open 选中一条并确认详情读到了。
func (h *harness) open(id string) {
	h.t.Helper()
	h.store.SelectItem(id)
	if d := h.detail(); d.Item == nil || d.Item.ID != id || d.Loading {
		h.t.Fatalf("选中 %s 之后详情应已读到：%+v", id, d)
	}
}

func (h *harness) edit(patch services.LibraryDraftFields) {
	h.store.UpdateSelected(h.ctx, patch)
}

func patchTitle(s string) services.LibraryDraftFields {
	return services.LibraryDraftFields{Title: services.StringPtr(s)}
}
func patchContent(s string) services.LibraryDraftFields {
	return services.LibraryDraftFields{Content: services.StringPtr(s)}
}
func patchTags(names ...string) services.LibraryDraftFields {
	if names == nil {
		names = []string{}
	}
	return services.LibraryDraftFields{TagNames: names}
}

func tagNamesOf(item *services.LibraryItem) []string { return item.TagNames() }

func (h *harness) loggedUnder(action string) []string {
	var out []string
	for _, e := range h.log.entries() {
		if strings.HasPrefix(e, action+"|") {
			out = append(out, e)
		}
	}
	return out
}

// ---- 选中与读取 ----

// 选中一条：列表高亮与详情同时落定，详情是完整条目（含正文）。
func TestSelectItemLoadsTheDetail(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	d := h.detail()
	if h.store.View().List.SelectedID != "a" || d.Item.Content != "甲的正文" || d.Item.Title != "甲" || d.LoadError != "" {
		t.Fatalf("详情：%+v", d)
	}
	if !slices.Equal(h.svc.Gets(), []string{"a"}) {
		t.Fatalf("读取的条目：%v", h.svc.Gets())
	}
}

// 读取期间是加载态：选中立刻生效、详情为空、Loading 为真，读到之后才换成条目。
func TestSelectItemShowsLoadingUntilTheDetailArrives(t *testing.T) {
	h := detailHarness(t)
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.SelectItem("a")
	d := h.detail()
	if h.store.View().List.SelectedID != "a" || !d.Loading || d.Item != nil {
		t.Fatalf("读取之前：%+v", d)
	}
	q.Run()
	if d = h.detail(); d.Loading || d.Item == nil || d.Item.ID != "a" {
		t.Fatalf("读取之后：%+v", d)
	}
}

// 选中之后又选了别的：旧条目的读取不论先到后到，都不能盖掉新条目（渲染层 detailRequestSeq）。
func TestAStaleDetailNeverOverwritesTheNewerSelection(t *testing.T) {
	for _, newestFirst := range []bool{false, true} {
		h := detailHarness(t)
		q := &spawnQueue{}
		h.store.Spawn = q.Spawn
		h.store.SelectItem("a")
		h.store.SelectItem("b")
		if len(q.queued) != 2 {
			t.Fatalf("应有两个待执行的读取：%d", len(q.queued))
		}
		order := []int{0, 1}
		if newestFirst {
			order = []int{1, 0}
		}
		for _, i := range order {
			q.queued[i]()
		}
		d := h.detail()
		if h.store.View().List.SelectedID != "b" || d.Item == nil || d.Item.ID != "b" || d.Loading {
			t.Fatalf("先后顺序 %v：详情应是 b：%+v", order, d)
		}
	}
}

// 条目已经不在了：显示「条目不存在或已被删除」，留一条日志，不弹提示。
func TestSelectItemReportsAMissingItem(t *testing.T) {
	h := detailHarness(t)
	h.store.SelectItem("nope")
	d := h.detail()
	if d.Loading || d.Item != nil || d.LoadError != "条目不存在或已被删除" {
		t.Fatalf("缺失条目：%+v", d)
	}
	if len(h.loggedUnder("library.detail")) != 1 {
		t.Fatalf("日志：%v", h.log.entries())
	}
}

// 读取失败：原因去掉首尾空白原样显示；原因是空的显示「未知错误」；换条重选或再选一次就恢复。
func TestSelectItemReportsAReadFailureAndRetries(t *testing.T) {
	h := detailHarness(t)
	h.svc.FailOn("GetItem", errors.New("  磁盘错误\n"))
	h.store.SelectItem("a")
	if d := h.detail(); d.LoadError != "磁盘错误" || d.Loading || d.Item != nil {
		t.Fatalf("失败：%+v", d)
	}
	h.svc.FailOn("GetItem", errors.New(" \n"))
	h.store.SelectItem("a")
	if d := h.detail(); d.LoadError != locales.CommonUnknownError() {
		t.Fatalf("空原因：%q", d.LoadError)
	}
	h.svc.FailOn("GetItem", nil)
	h.store.SelectItem("a") // 重试按钮：再选一次
	if d := h.detail(); d.LoadError != "" || d.Item == nil || d.Item.ID != "a" {
		t.Fatalf("重试之后：%+v", d)
	}
}

// 取消选中：详情清空，不再读取。
func TestSelectingNothingClearsTheDetail(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	before := len(h.svc.Gets())
	h.store.SelectItem("")
	d := h.detail()
	if h.store.View().List.SelectedID != "" || d.Item != nil || d.Loading || len(h.svc.Gets()) != before {
		t.Fatalf("取消选中：%+v，读取 %d → %d", d, before, len(h.svc.Gets()))
	}
}

// 详情快照拿到之后不会再变：编辑换的是新对象。
func TestDetailSnapshotsAreImmutable(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	before := h.detail().Item
	h.edit(patchTitle("新标题"))
	h.edit(patchTags("go", "rust"))
	if before.Title != "甲" || len(before.Tags) != 1 || h.detail().Item == before {
		t.Fatalf("旧快照被改了：%+v", before)
	}
}

// ---- 编辑与防抖保存 ----

// 编辑立即回显；800 ms 之后才落盘，落盘带上编辑起点的基线；成功后状态位复原、列表行跟上。
func TestEditingEchoesAtOnceAndSavesAfterTheDebounce(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("新标题"))
	d := h.detail()
	if d.Item.Title != "新标题" || !d.Unsaved || h.clock.Pending() != 1 || len(h.svc.Drafts()) != 0 {
		t.Fatalf("编辑之后：%+v，定时器 %d，草稿 %d", d, h.clock.Pending(), len(h.svc.Drafts()))
	}
	h.clock.Advance(799 * time.Millisecond) // 渲染层 AUTO_SAVE_DEBOUNCE_MS = 800
	if len(h.svc.Drafts()) != 0 {
		t.Fatal("防抖没到就落盘了")
	}
	h.clock.Advance(time.Millisecond)
	drafts := h.svc.Drafts()
	if len(drafts) != 1 {
		t.Fatalf("到点应落盘一次：%d", len(drafts))
	}
	dr := drafts[0]
	if dr.ID != "a" || dr.RequestID == "" || dr.Base.Title == nil || *dr.Base.Title != "甲" ||
		dr.Patch.Title == nil || *dr.Patch.Title != "新标题" || dr.Patch.Content != nil || dr.Patch.TagNames != nil {
		t.Fatalf("草稿：%+v", dr)
	}
	d = h.detail()
	if d.Unsaved || d.Saving || d.SaveError != "" || d.Item.Title != "新标题" {
		t.Fatalf("落盘之后：%+v", d)
	}
	if got, _ := h.svc.ItemNow("a"); got.Title != "新标题" {
		t.Fatalf("后端：%+v", got)
	}
	if row := h.store.View().List.Entries; row[slices.IndexFunc(row, func(e services.LibraryEntry) bool { return e.ID == "a" })].Title != "新标题" {
		t.Fatal("列表行没有跟上新标题")
	}
}

// 每次编辑都重新计时：连续输入不会在中途落盘。
func TestEachEditRestartsTheDebounce(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchContent("一"))
	h.clock.Advance(500 * time.Millisecond)
	h.edit(patchContent("一二"))
	h.clock.Advance(500 * time.Millisecond)
	if len(h.svc.Drafts()) != 0 {
		t.Fatal("第二次编辑应让计时重来")
	}
	h.clock.Advance(300 * time.Millisecond)
	drafts := h.svc.Drafts()
	if len(drafts) != 1 || drafts[0].Patch.Content == nil || *drafts[0].Patch.Content != "一二" {
		t.Fatalf("只应落盘一次、内容是最后一次：%+v", drafts)
	}
}

// 自动保存关着：不排定时器、改动留着，保存按钮（FlushPendingSave）才落盘。
func TestAutoSaveOffKeepsEditsPendingUntilFlushed(t *testing.T) {
	h := detailHarness(t)
	h.store.AutoSave = func() bool { return false }
	h.open("a")
	h.edit(patchContent("改动"))
	if h.clock.Pending() != 0 || !h.detail().Unsaved {
		t.Fatalf("定时器 %d，状态 %+v", h.clock.Pending(), h.detail())
	}
	h.clock.Advance(time.Minute)
	if len(h.svc.Drafts()) != 0 {
		t.Fatal("自动保存关着，不该落盘")
	}
	if !h.store.FlushPendingSave(h.ctx) || len(h.svc.Drafts()) != 1 || h.detail().Unsaved {
		t.Fatalf("手动落盘：草稿 %d，状态 %+v", len(h.svc.Drafts()), h.detail())
	}
}

// 没有待保存的东西：立即成功，不发请求。
func TestFlushWithNothingPendingSucceeds(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if !h.store.FlushPendingSave(h.ctx) || len(h.svc.Drafts()) != 0 {
		t.Fatal("没有待保存时应直接成功")
	}
}

// 没有选中的详情、或详情还在读取：编辑被忽略，不排定时器。
func TestEditsWithoutALoadedDetailAreIgnored(t *testing.T) {
	h := detailHarness(t)
	h.edit(patchTitle("没处可写"))
	if h.clock.Pending() != 0 || h.detail().Unsaved {
		t.Fatalf("没有选中：%+v", h.detail())
	}
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.SelectItem("a") // 读取尚未完成
	h.edit(patchTitle("读取中"))
	if h.clock.Pending() != 0 || h.detail().Unsaved {
		t.Fatalf("读取中：%+v", h.detail())
	}
}

// 标签编辑即时回显：已有的沿用原对象，新名字是临时占位；落盘之后换成服务端的标签，侧栏的标签与计数也重读。
func TestTagEditsEchoOptimisticallyThenAdoptTheServerTags(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	tagsBefore, countsBefore := countCalls(h.svc.Calls(), "Tags"), len(h.svc.CountQueries())
	h.edit(patchTags("GO", "Rust")) // GO 与已有的 go 按小写算同一个，沿用原对象；Rust 是新名字
	got := h.detail().Item.Tags
	if len(got) != 2 || got[0].ID != "t1" || got[0].Name != "go" || got[1].ID != "pending:rust" || got[1].Name != "Rust" || got[1].ColorKey != "gray" {
		t.Fatalf("回显的标签：%+v", got)
	}
	h.clock.Advance(800 * time.Millisecond)
	names := tagNamesOf(h.detail().Item)
	if len(names) != 2 || strings.HasPrefix(h.detail().Item.Tags[1].ID, "pending:") {
		t.Fatalf("落盘之后应是服务端的标签：%+v", h.detail().Item.Tags)
	}
	if countCalls(h.svc.Calls(), "Tags") != tagsBefore+1 || len(h.svc.CountQueries()) <= countsBefore {
		t.Fatalf("改了标签要重读标签与计数：Tags %d → %d，计数 %d → %d",
			tagsBefore, countCalls(h.svc.Calls(), "Tags"), countsBefore, len(h.svc.CountQueries()))
	}
}

// 按小写认名字，已有标签的名字本身带大写时也一样：「go」「GO」都沿用已有的「Go」（保留它的 ID、颜色与大小写），不生成占位。
func TestReconcileOptimisticTagsIgnoresCaseOnBothSides(t *testing.T) {
	existing := []services.LibraryTag{{ID: "t9", Name: "Go", ColorKey: "green"}}
	for i, tag := range reconcileOptimisticTags(existing, []string{"go", "GO"}) {
		if tag.ID != "t9" || tag.Name != "Go" || tag.ColorKey != "green" {
			t.Fatalf("第 %d 个应沿用已有的 Go：%+v", i, tag)
		}
	}
}

// 防抖窗口里连加两个标签，第一个不能被第二个的补丁覆盖。
func TestTwoQuickTagEditsKeepBoth(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTags("go", "rust"))
	h.edit(patchTags(append(tagNamesOf(h.detail().Item), "zig")...))
	h.clock.Advance(800 * time.Millisecond)
	dr := h.svc.Drafts()
	if len(dr) != 1 || !slices.Equal(dr[0].Patch.TagNames, []string{"go", "rust", "zig"}) {
		t.Fatalf("草稿：%+v", dr)
	}
	if got, _ := h.svc.ItemNow("a"); len(got.Tags) != 3 {
		t.Fatalf("后端的标签：%+v", got.Tags)
	}
}

// 保存在途时又输入：新输入留在待保存里，这一轮结束后接着落盘；两次的基线各是各的，没有误报冲突。
func TestInputDuringAnInFlightSaveIsSavedNext(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.Spawn = func(fn func()) { go fn() }
	h.edit(patchTitle("第一次的标题"))
	release := h.svc.HoldSave()
	done := make(chan bool, 1)
	go func() { done <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "第一次保存在途", func() bool { return h.detail().Saving })

	h.edit(patchContent("第二次的正文"))
	if d := h.detail(); d.Item.Title != "第一次的标题" || d.Item.Content != "第二次的正文" {
		t.Fatalf("在途期间详情要同时显示两次输入：%+v", d.Item)
	}
	release()
	if !<-done {
		t.Fatalf("两次都应成功：%+v", h.detail())
	}
	dr := h.svc.Drafts()
	if len(dr) != 2 || dr[1].Patch.Title != nil || dr[1].Patch.Content == nil || *dr[1].Patch.Content != "第二次的正文" {
		t.Fatalf("草稿：%+v", dr)
	}
	if dr[1].Base.Content == nil || *dr[1].Base.Content != "甲的正文" {
		t.Fatalf("第二次的正文基线应是编辑起点：%+v", dr[1].Base)
	}
	got, _ := h.svc.ItemNow("a")
	if got.Title != "第一次的标题" || got.Content != "第二次的正文" {
		t.Fatalf("后端：%+v", got)
	}
}

// 第一次保存在途时别处改了另一个字段：第二次保存必须按「编辑起点」的基线比较、报冲突；
// 若把基线换成服务端刚返回的版本，别处的改动就被静默覆盖了。两个方向各验一次（标题、正文各自的基线保留）。
func TestABaseFromBeforeAnInFlightSaveStillDetectsExternalEdits(t *testing.T) {
	cases := []struct {
		name     string
		first    services.LibraryDraftFields // 第一次落盘的字段
		later    services.LibraryDraftFields // 在途期间又改的字段
		external func(*services.LibraryItem) // 别处同时改了「又改的那个字段」
		field    string
	}{
		{"在途的是标题、之后改正文", patchTitle("第一次的标题"), patchContent("第二次的正文"),
			func(it *services.LibraryItem) { it.Content = "别处的正文" }, services.LibraryDraftFieldContent},
		{"在途的是正文、之后改标题", patchContent("第一次的正文"), patchTitle("第二次的标题"),
			func(it *services.LibraryItem) { it.Title = "别处的标题" }, services.LibraryDraftFieldTitle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := detailHarness(t)
			h.open("a")
			h.store.Spawn = func(fn func()) { go fn() }
			h.edit(c.first)
			release := h.svc.HoldSave()
			done := make(chan bool, 1)
			go func() { done <- h.store.FlushPendingSave(h.ctx) }()
			waitFor(t, "第一次保存在途", func() bool { return h.detail().Saving })
			h.edit(c.later)
			h.svc.EditExternally("a", c.external)
			release()
			if <-done {
				t.Fatal("别处改了同一个字段，第二次应报冲突而不是成功")
			}
			conflict := h.detail().Conflict
			if conflict == nil || conflict.ItemID != "a" || !slices.Equal(conflict.Fields, []string{c.field}) {
				t.Fatalf("冲突：%+v", conflict)
			}
		})
	}
}

// 第一次保存在途时改了标签、而别处同时加了标签：第一次成功之后，待保存的标签要先按旧基线并进服务端的标签，
// 否则第二次保存会把别处新加的标签当成我删掉的。
func TestTagEditsDuringASaveKeepTagsAddedElsewhere(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.Spawn = func(fn func()) { go fn() }
	h.edit(patchTitle("第一次的标题"))
	release := h.svc.HoldSave()
	done := make(chan bool, 1)
	go func() { done <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "第一次保存在途", func() bool { return h.detail().Saving })
	h.edit(patchTags("go", "rust"))
	h.svc.EditExternally("a", func(it *services.LibraryItem) {
		it.Tags = append(it.Tags, services.LibraryTag{ID: "t3", Name: "zig", ColorKey: "gray"})
	})
	release()
	if !<-done {
		t.Fatalf("两次都应成功：%+v", h.detail())
	}
	got, _ := h.svc.ItemNow("a")
	names := got.TagNames()
	slices.Sort(names)
	if !slices.Equal(names, []string{"go", "rust", "zig"}) {
		t.Fatalf("别处新加的 zig 不能丢：%v", names)
	}
}

// 服务端写回时，详情里在途的与待保存的编辑都叠在上面，同一个字段以最新输入的为准。
func TestAServerWriteBackKeepsTheNewestInputOverAnInFlightOne(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.Spawn = func(fn func()) { go fn() }
	h.edit(patchTitle("在途的标题"))
	release := h.svc.HoldSave()
	done := make(chan bool, 1)
	go func() { done <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "保存在途", func() bool { return h.detail().Saving })
	h.edit(patchTitle("更新的标题"))
	if !h.store.ToggleFavorite(h.ctx, "a") {
		t.Fatal("收藏失败")
	}
	if got := h.detail().Item.Title; got != "更新的标题" {
		t.Fatalf("写回之后标题应是最新输入的：%q", got)
	}
	release()
	if !<-done {
		t.Fatalf("两次都应成功：%+v", h.detail())
	}
	if got, _ := h.svc.ItemNow("a"); got.Title != "更新的标题" {
		t.Fatalf("后端：%+v", got)
	}
}

// 保存失败：改动退回待保存、原因显示出来、写日志；详情仍显示我的改动；修好之后再存就成功，原因清空。
func TestSaveFailureKeepsTheEditsAndReportsTheReason(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("存不进去"))
	h.svc.FailDraft("磁盘已满")
	if h.store.FlushPendingSave(h.ctx) {
		t.Fatal("保存失败应返回 false")
	}
	d := h.detail()
	if d.SaveError != "磁盘已满" || !d.Unsaved || d.Saving || d.Item.Title != "存不进去" {
		t.Fatalf("失败之后：%+v", d)
	}
	if got := h.loggedUnder("library.saveDraft"); len(got) != 1 || !strings.Contains(got[0], "磁盘已满") {
		t.Fatalf("日志：%v", h.log.entries())
	}
	h.svc.FailDraft("")
	if !h.store.FlushPendingSave(h.ctx) {
		t.Fatalf("修好之后应成功：%+v", h.detail())
	}
	if d = h.detail(); d.SaveError != "" || d.Unsaved {
		t.Fatalf("成功之后：%+v", d)
	}
	if got, _ := h.svc.ItemNow("a"); got.Title != "存不进去" {
		t.Fatalf("后端：%+v", got)
	}
}

// 保存调用本身失败（不是业务失败）同样留下原因与改动。
func TestSaveCallFailureIsReportedToo(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchContent("改动"))
	h.svc.FailOn("SaveDraft", errors.New("连接已断开"))
	if h.store.FlushPendingSave(h.ctx) || h.detail().SaveError != "连接已断开" || !h.detail().Unsaved {
		t.Fatalf("状态：%+v", h.detail())
	}
}

// 失败退回的补丁与失败期间新输入的字段合并，新输入的盖旧的；重试一次带上全部。
func TestAFailedSaveMergesWithNewerInput(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.Spawn = func(fn func()) { go fn() }
	h.edit(patchTitle("标题"))
	h.edit(patchContent("旧正文"))
	h.svc.FailDraft("暂时不行")
	release := h.svc.HoldSave()
	done := make(chan bool, 1)
	go func() { done <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "保存在途", func() bool { return h.detail().Saving })
	h.edit(patchContent("新正文"))
	release()
	if <-done {
		t.Fatal("应当失败")
	}
	h.svc.FailDraft("")
	if !h.store.FlushPendingSave(h.ctx) {
		t.Fatalf("重试应成功：%+v", h.detail())
	}
	dr := h.svc.Drafts()
	last := dr[len(dr)-1]
	if last.Patch.Title == nil || *last.Patch.Title != "标题" || last.Patch.Content == nil || *last.Patch.Content != "新正文" {
		t.Fatalf("重试的补丁应含标题与最新正文：%+v", last.Patch)
	}
}

// ---- 切条目、改筛选之前先落盘 ----

// 选中另一条之前，先把当前这条的编辑落盘；落盘之后才换选中。
func TestSelectingAnotherItemSavesTheEditsFirst(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("切走之前"))
	h.store.SelectItem("b")
	if got, _ := h.svc.ItemNow("a"); got.Title != "切走之前" {
		t.Fatalf("切走之前应已落盘：%+v", got)
	}
	if d := h.detail(); h.store.View().List.SelectedID != "b" || d.Item == nil || d.Item.ID != "b" || d.Unsaved {
		t.Fatalf("切换之后：%+v", d)
	}
	if h.clock.Pending() != 0 {
		t.Fatal("落盘之后不该还有防抖定时器")
	}
}

// 落盘失败：选中不动，改动还在，失败原因显示在详情头部。
func TestSelectionStaysPutWhenTheSaveFails(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("存不进去"))
	h.svc.FailDraft("磁盘已满")
	h.store.SelectItem("b")
	d := h.detail()
	if h.store.View().List.SelectedID != "a" || d.Item.ID != "a" || d.Item.Title != "存不进去" || d.SaveError != "磁盘已满" || !d.Unsaved {
		t.Fatalf("选中应留在 a：%+v", d)
	}
}

// 有一次落盘在途时选中另一条：必须等它结束，不能当场换（否则「在途」里的那次失败就没人拦了）。
func TestSelectionWaitsForAnInFlightSave(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.Spawn = func(fn func()) { go fn() }
	h.edit(patchTitle("在途"))
	release := h.svc.HoldSave()
	done := make(chan bool, 1)
	go func() { done <- h.store.FlushPendingSave(h.ctx) }()
	waitFor(t, "保存在途", func() bool { return h.detail().Saving })
	h.store.SelectItem("b")
	time.Sleep(40 * time.Millisecond) // 给错误实现留出换掉选中的时间；正确实现要一直等到放行
	if h.store.View().List.SelectedID != "a" {
		t.Fatal("保存在途时选中不能当场换")
	}
	release()
	<-done
	waitFor(t, "保存结束后选中换到 b", func() bool {
		d := h.detail()
		return h.store.View().List.SelectedID == "b" && d.Item != nil && d.Item.ID == "b"
	})
}

// 落盘等待期间又选了别的：以最后一次选中为准，中间那次作废。
func TestTheLastSelectionWinsWhileFlushing(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("改动"))
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.SelectItem("b")
	h.store.SelectItem("c")
	if h.store.View().List.SelectedID != "a" {
		t.Fatal("有待保存时选中应等落盘之后")
	}
	q.Run()
	d := h.detail()
	if h.store.View().List.SelectedID != "c" || d.Item == nil || d.Item.ID != "c" {
		t.Fatalf("应落在 c：%+v", d)
	}
	if slices.Contains(h.svc.Gets(), "b") {
		t.Fatalf("作废的那次选中不该读详情：%v", h.svc.Gets())
	}
}

// 键盘移动选中走同一条管线：先落盘、再移动。
func TestMovingTheSelectionSavesFirst(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("移动之前"))
	h.store.MoveSelection(MoveNext)
	if got, _ := h.svc.ItemNow("a"); got.Title != "移动之前" {
		t.Fatalf("移动之前应已落盘：%+v", got)
	}
	if h.store.View().List.SelectedID != "b" {
		t.Fatalf("选中：%s", h.store.View().List.SelectedID)
	}
}

// B-033 (1)：改筛选之前先落盘；落盘之后筛选生效、选中与详情清空。
func TestChangingFiltersSavesFirst(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("换筛选之前"))
	h.store.SelectCollection(h.ctx, "c1")
	if got, _ := h.svc.ItemNow("a"); got.Title != "换筛选之前" {
		t.Fatalf("换筛选之前应已落盘：%+v", got)
	}
	v := h.store.View()
	if v.Filters.CollectionID != "c1" || v.List.SelectedID != "" || v.Detail.Item != nil {
		t.Fatalf("筛选生效、详情清空：%+v", v)
	}
}

// B-033 (1)：落盘失败时筛选不变、选中与改动都在。
func TestFiltersStayPutWhenTheSaveFails(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("存不进去"))
	h.svc.FailDraft("磁盘已满")
	h.store.SelectCollection(h.ctx, "c1")
	v := h.store.View()
	if v.Filters.CollectionID != "" || v.List.SelectedID != "a" || v.Detail.Item == nil || v.Detail.SaveError != "磁盘已满" {
		t.Fatalf("筛选应不变：%+v", v)
	}
}

// B-033 (2)：改筛选让在途的详情读取作废，晚到的结果不能把已清空的详情又填回来。
func TestChangingFiltersCancelsAnInFlightDetailRead(t *testing.T) {
	h := detailHarness(t)
	q := &spawnQueue{}
	h.store.Spawn = q.Spawn
	h.store.SelectItem("a")
	reads := slices.Clone(q.queued)
	q.queued = nil
	h.store.SelectCollection(h.ctx, "c1")
	for _, fn := range reads {
		fn()
	}
	d := h.detail()
	if h.store.View().List.SelectedID != "" || d.Item != nil || d.Loading {
		t.Fatalf("详情不该被晚到的读取填回来：%+v", d)
	}
}

// 搜索结果里没有当前选中的条目：详情清空，晚到的读取不能复活它。
func TestSearchingAwayFromTheSelectedItemClearsTheDetail(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.SetSearch(h.ctx, "乙")
	v := h.store.View()
	if v.List.SelectedID != "" || v.Detail.Item != nil || v.Detail.Loading {
		t.Fatalf("搜索不含 a，详情应清空：%+v", v.Detail)
	}
}

// ---- 冲突 ----

// 别处改了标题：保存报冲突、什么也不写，详情仍显示我的改动，冲突里带着服务端的版本与冲突的字段。
func TestAConflictIsReportedAndNothingIsWritten(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("我的标题"))
	h.svc.EditExternally("a", func(it *services.LibraryItem) { it.Title = "别处的标题" })
	if h.store.FlushPendingSave(h.ctx) {
		t.Fatal("冲突时应返回 false")
	}
	d := h.detail()
	if d.Conflict == nil || d.Conflict.ItemID != "a" || d.Conflict.Server.Title != "别处的标题" ||
		!slices.Equal(d.Conflict.Fields, []string{services.LibraryDraftFieldTitle}) {
		t.Fatalf("冲突：%+v", d.Conflict)
	}
	if d.SaveError != "该内容已在其他操作中修改，请选择要保留的版本" || d.Item.Title != "我的标题" || !d.Unsaved {
		t.Fatalf("状态：%+v", d)
	}
	if got, _ := h.svc.ItemNow("a"); got.Title != "别处的标题" {
		t.Fatalf("冲突时不能写：%+v", got)
	}
}

// 保留我的修改：以服务端版本为新基线重写，成功后冲突与失败原因清空。
func TestKeepMineRewritesOnTopOfTheServerVersion(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("我的标题"))
	h.svc.EditExternally("a", func(it *services.LibraryItem) { it.Title = "别处的标题" })
	h.store.FlushPendingSave(h.ctx)
	if !h.store.ResolveSaveConflict(h.ctx, KeepMine) {
		t.Fatalf("保留我的修改应成功：%+v", h.detail())
	}
	d := h.detail()
	if d.Conflict != nil || d.SaveError != "" || d.Unsaved || d.Item.Title != "我的标题" {
		t.Fatalf("之后：%+v", d)
	}
	if got, _ := h.svc.ItemNow("a"); got.Title != "我的标题" {
		t.Fatalf("后端：%+v", got)
	}
}

// 采用已保存的版本：冲突字段上我的修改丢掉，详情换成服务端版本，不再发请求。
func TestUseSavedDropsTheConflictingEdit(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("我的标题"))
	h.svc.EditExternally("a", func(it *services.LibraryItem) { it.Title = "别处的标题" })
	h.store.FlushPendingSave(h.ctx)
	if !h.store.ResolveSaveConflict(h.ctx, UseSaved) {
		t.Fatalf("采用已保存的版本应成功：%+v", h.detail())
	}
	d := h.detail()
	if d.Conflict != nil || d.SaveError != "" || d.Unsaved || d.Item.Title != "别处的标题" {
		t.Fatalf("之后：%+v", d)
	}
	if len(h.svc.Drafts()) != 1 {
		t.Fatalf("丢掉之后没有东西可存，不该再发请求：%d", len(h.svc.Drafts()))
	}
}

// 采用已保存的版本只丢冲突的字段：没冲突的标签改动照样保存。
func TestUseSavedKeepsTheNonConflictingFields(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("我的标题"))
	h.edit(patchTags("go", "rust"))
	h.svc.EditExternally("a", func(it *services.LibraryItem) { it.Title = "别处的标题" })
	h.store.FlushPendingSave(h.ctx)
	if !h.store.ResolveSaveConflict(h.ctx, UseSaved) {
		t.Fatalf("应成功：%+v", h.detail())
	}
	dr := h.svc.Drafts()
	if len(dr) != 2 || dr[1].Patch.Title != nil || !slices.Equal(dr[1].Patch.TagNames, []string{"go", "rust"}) {
		t.Fatalf("第二次只应带标签：%+v", dr)
	}
	got, _ := h.svc.ItemNow("a")
	if got.Title != "别处的标题" || !slices.Equal(got.TagNames(), []string{"go", "rust"}) {
		t.Fatalf("后端：%+v", got)
	}
}

// 冲突时标签先按旧基线重放增删：别处新增的标签保留，我删掉的不会回来。
func TestConflictReplaysTagEditsAgainstTheOldBase(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("我的标题"))
	h.edit(patchTags("rust")) // 删 go、加 rust
	h.svc.EditExternally("a", func(it *services.LibraryItem) {
		it.Title = "别处的标题"
		it.Tags = append(it.Tags, services.LibraryTag{ID: "t3", Name: "zig", ColorKey: "gray"})
	})
	h.store.FlushPendingSave(h.ctx)
	if !h.store.ResolveSaveConflict(h.ctx, KeepMine) {
		t.Fatalf("应成功：%+v", h.detail())
	}
	got, _ := h.svc.ItemNow("a")
	names := got.TagNames()
	slices.Sort(names)
	if !slices.Equal(names, []string{"rust", "zig"}) {
		t.Fatalf("标签应是别处新增的 zig 加我加的 rust：%v", names)
	}
}

// 没有冲突时处理冲突什么也不做。
func TestResolvingWithoutAConflictDoesNothing(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if h.store.ResolveSaveConflict(h.ctx, KeepMine) || len(h.svc.Drafts()) != 0 {
		t.Fatal("没有冲突时应返回 false 且不发请求")
	}
}

// ---- 服务端写回与即时落盘的操作 ----

// 收藏翻转的结果并进详情：详情里还没落盘的标题编辑仍叠在上面。
func TestAFlagToggleMergesIntoTheDetailKeepingPendingEdits(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("没落盘的标题"))
	if !h.store.ToggleFavorite(h.ctx, "a") {
		t.Fatal("收藏失败")
	}
	d := h.detail()
	if !d.Item.IsFavorite || d.Item.Title != "没落盘的标题" {
		t.Fatalf("详情：%+v", d.Item)
	}
	if got := h.store.View().List.Entries; !got[slices.IndexFunc(got, func(e services.LibraryEntry) bool { return e.ID == "a" })].IsFavorite {
		t.Fatal("列表行也应收藏了")
	}
}

// 选中的条目以详情里的收藏值为准翻转（渲染层 toggleFlag 的 known）：详情与列表行不一致时，按详情取反。
func TestToggleUsesTheDetailValueForTheSelectedItem(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.store.mu.Lock()
	forced := *h.store.view.Detail.Item
	forced.IsFavorite = true
	h.store.view.Detail.Item = &forced
	h.store.mu.Unlock()
	h.store.ToggleFavorite(h.ctx, "a")
	calls := h.svc.UpdateCalls()
	if len(calls) != 1 || calls[0].Patch.IsFavorite == nil || *calls[0].Patch.IsFavorite {
		t.Fatalf("详情里已收藏，应发取消收藏：%+v", calls)
	}
}

// 复核状态等条目级字段经 ApplyServerEntry 并进详情，正文保持原样。
func TestApplyServerEntryUpdatesTheEntryLevelFieldsOfTheDetail(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchContent("没落盘的正文"))
	e := h.store.View().List.Entries[slices.IndexFunc(h.store.View().List.Entries, func(e services.LibraryEntry) bool { return e.ID == "a" })]
	e.ReviewStatus, e.ReviewReasons, e.IsPinned, e.UpdatedAt = services.LibraryReviewNeedsReview, []string{"缺少正文"}, true, 9000
	h.store.ApplyServerEntry(e)
	d := h.detail().Item
	if d.ReviewStatus != services.LibraryReviewNeedsReview || !slices.Equal(d.ReviewReasons, []string{"缺少正文"}) || !d.IsPinned || d.UpdatedAt != 9000 {
		t.Fatalf("条目级字段应并进详情：%+v", d)
	}
	if d.Content != "没落盘的正文" {
		t.Fatalf("没落盘的正文仍叠在上面：%q", d.Content)
	}
}

// 选中条目改标签走草稿：立即落盘；别的条目走直接更新，不经草稿。
func TestSetItemTagsRoutesTheSelectedItemThroughTheDraft(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if !h.store.SetItemTags(h.ctx, "a", []string{"go", "rust"}) {
		t.Fatalf("选中条目改标签应成功：%+v", h.detail())
	}
	dr := h.svc.Drafts()
	if len(dr) != 1 || !slices.Equal(dr[0].Patch.TagNames, []string{"go", "rust"}) || len(h.svc.UpdateCalls()) != 0 {
		t.Fatalf("选中条目：草稿 %+v，直接更新 %d", dr, len(h.svc.UpdateCalls()))
	}
	if !h.store.SetItemTags(h.ctx, "b", []string{"go"}) {
		t.Fatal("别的条目改标签应成功")
	}
	if len(h.svc.Drafts()) != 1 || len(h.svc.UpdateCalls()) != 1 {
		t.Fatalf("别的条目走直接更新：草稿 %d，更新 %d", len(h.svc.Drafts()), len(h.svc.UpdateCalls()))
	}
}

// 选中条目改标签失败：原因显示在详情头部，不弹提示（渲染层这条路径不经 runGuardedMutation）。
func TestSetItemTagsFailureOnTheSelectedItemStaysInTheHeader(t *testing.T) {
	h := detailHarness(t)
	var toasts atomic.Int32
	h.store.guard.Notify = kit.NewNotifier(func() { toasts.Add(1) })
	h.open("a")
	h.svc.FailDraft("磁盘已满")
	if h.store.SetItemTags(h.ctx, "a", []string{"rust"}) {
		t.Fatal("应当失败")
	}
	if d := h.detail(); d.SaveError != "磁盘已满" || !d.Unsaved {
		t.Fatalf("状态：%+v", d)
	}
	if toasts.Load() != 0 {
		t.Fatalf("不该弹提示：%d", toasts.Load())
	}
}

// ---- 待保存桶 ----

// patchSet 的遍历顺序是首次写入的顺序：改已有的键不换位置，删掉再写排到最后（渲染层 Map 的语义）。
func TestPatchSetKeepsInsertionOrderLikeAMap(t *testing.T) {
	var p patchSet
	p.put("a", patchTitle("1"))
	p.put("b", patchTitle("2"))
	p.put("a", patchTitle("3"))
	if !slices.Equal(p.ids(), []string{"a", "b"}) {
		t.Fatalf("改已有的键不该换位置：%v", p.ids())
	}
	p.remove("a")
	p.put("a", patchTitle("4"))
	if !slices.Equal(p.ids(), []string{"b", "a"}) || p.len() != 2 {
		t.Fatalf("删掉再写应排到最后：%v", p.ids())
	}
	p.remove("zzz")
	if p.len() != 2 {
		t.Fatal("删不存在的键不该有影响")
	}
}

// 多个条目都有待保存时按首次写入的顺序逐个落盘；一桶失败就停，后面的留着。
func TestPendingBucketsAreSavedInOrderAndStopAtTheFirstFailure(t *testing.T) {
	h := detailHarness(t)
	h.store.mu.Lock()
	h.store.detail.bases = map[string]services.LibraryDraftFields{"a": patchTitle("甲"), "b": patchTitle("乙"), "c": patchTitle("丙")} // 基线是编辑起点的标题，否则按「没有基线」算冲突
	h.store.detail.pending.put("c", patchTitle("丙改"))
	h.store.detail.pending.put("a", patchTitle("甲改"))
	h.store.detail.pending.put("b", patchTitle("乙改"))
	h.store.mu.Unlock()
	if !h.store.FlushPendingSave(h.ctx) {
		t.Fatalf("应全部成功：%+v", h.detail())
	}
	var order []string
	for _, d := range h.svc.Drafts() {
		order = append(order, d.ID)
	}
	if !slices.Equal(order, []string{"c", "a", "b"}) {
		t.Fatalf("落盘顺序：%v", order)
	}

	h2 := detailHarness(t)
	h2.store.mu.Lock()
	h2.store.detail.bases = map[string]services.LibraryDraftFields{"a": patchTitle("甲"), "b": patchTitle("乙")}
	h2.store.detail.pending.put("a", patchTitle("甲改"))
	h2.store.detail.pending.put("b", patchTitle("乙改"))
	h2.store.mu.Unlock()
	h2.svc.FailDraft("坏了")
	if h2.store.FlushPendingSave(h2.ctx) {
		t.Fatal("应当失败")
	}
	if n := len(h2.svc.Drafts()); n != 1 {
		t.Fatalf("第一桶失败就该停：发了 %d 次", n)
	}
	if ids := h2.store.detail.pending.ids(); !slices.Equal(ids, []string{"b", "a"}) {
		t.Fatalf("失败的那桶退回队尾，后面的留着：%v", ids)
	}
}
