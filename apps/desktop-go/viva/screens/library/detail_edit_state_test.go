package library

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件补 detailedit.go 里只能看内部状态、或要直接调用才分得出来的分支（M2-6a 收口的变异检查里存活下来的那几个）：
// 每个用例守一条规则，注释里写明是哪一条。整条界面流程（点「编辑」、改、完成、失败）在 detail_edit_flow_test.go。

// editButtonName 返回此刻工具条上该点的「编辑」钮名字（正文是「编辑原文」，论坛的总结标签是「编辑讨论总结」）。
func (rr *readerRig) editButtonName() string {
	rr.t.Helper()
	for _, name := range []string{locales.ThemedReadingReaderEditOriginal(), locales.ThemedReadingReaderEditSummary()} {
		if rr.has(gfx.RoleButton, name) {
			return name
		}
	}
	rr.t.Fatal("工具条上没有「编辑」钮（夹具失效）")
	return ""
}

// assertiveStatusNodes 是画面上 role=status、aria-live=assertive 的节点个数（红字条是其中一个）。
func (rr *readerRig) assertiveStatusNodes() int {
	n := 0
	for _, node := range rr.session.Access() {
		if node.Role == gfx.RoleStatus && node.Live == gfx.LiveAssertive {
			n++
		}
	}
	return n
}

// 草稿键只在主题阅读可用（停在正文或讨论总结标签、不在网页快照里）时才有；别处进不了编辑态。
func TestEditKeyExistsOnlyWhereThemedReadingIsAvailable(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	s := rr.screen
	if key, kind, ok := s.editKeyFor(detailNoteID, TabBody); !ok || key != draftKey(detailNoteID, sourceBody) || kind != sourceBody {
		t.Fatalf("正文标签应有草稿键：%q %q %v", key, kind, ok)
	}
	if key, kind, ok := s.editKeyFor(detailNoteID, TabSummary); !ok || key != draftKey(detailNoteID, sourceSummary) || kind != sourceSummary {
		t.Fatalf("讨论总结标签的草稿键属于 summary 来源：%q %q %v", key, kind, ok)
	}
	if _, _, ok := s.editKeyFor(detailNoteID, TabImages); ok {
		t.Error("配图标签上主题阅读不可用，不该有草稿键")
	}
	item := *rr.view().Detail.Item
	s.startEdit(item, TabImages)
	if s.detail.edit.key != "" || len(s.detail.edit.drafts) != 0 {
		t.Errorf("主题阅读不可用的标签上进不了编辑态，也不建草稿：key=%q drafts=%d", s.detail.edit.key, len(s.detail.edit.drafts))
	}
	s.detail.panel.snapshotView = true
	if _, _, ok := s.editKeyFor(detailNoteID, TabBody); ok {
		t.Error("网页快照里主题阅读不可用，不该有草稿键")
	}
}

// editing 说的是「此刻显示的这一条这一份原文在不在编辑」：别的条目、另一份来源、主题阅读不可用的标签都不算。
func TestEditingMeansTheShownItemAndSource(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	s := rr.screen
	note := *rr.view().Detail.Item
	if !s.editing(note, TabBody) {
		t.Fatal("正在编辑这一条的正文")
	}
	other := note
	other.ID = detailForumID
	if s.editing(other, TabBody) {
		t.Error("别的条目不算在编辑")
	}
	if s.editing(note, TabSummary) {
		t.Error("讨论总结是另一份来源，不算在编辑")
	}
	if s.editing(note, TabImages) {
		t.Error("主题阅读不可用的标签不算在编辑")
	}
}

// 进编辑态：上次失败留下的红字条清掉、查找位置归零、此后不再恢复阅读位置（渲染层 startEditing）。
func TestStartEditClearsTheBannerTheFindPositionAndTheRestoredPosition(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	s := rr.screen
	e, p := &s.detail.edit, &s.detail.panel
	key := draftKey(detailNoteID, sourceBody)
	e.err, e.errKey = "上一次的失败", key // 离开编辑态时红字条的状态还留着（只是 editError 因 key 不符而不摆）
	top := 250.0
	p.restoreTop, p.restoreTab = &top, TabBody
	p.findActive, p.findCount = 3, 5

	s.startEdit(*rr.view().Detail.Item, TabBody)
	if e.key != key {
		t.Fatalf("应进入这一份的编辑态：%q", e.key)
	}
	if e.err != "" || e.errKey != "" {
		t.Errorf("进编辑态要清掉旧红字条：%q %q", e.err, e.errKey)
	}
	if p.restoreTop != nil {
		t.Error("进编辑态后不再恢复阅读位置")
	}
	if p.findActive != 0 || p.findCount != 0 {
		t.Errorf("进编辑态要把查找位置归零：%d / %d", p.findActive, p.findCount)
	}
}

// leave 结束编辑态：草稿保留，红字条的状态也一并清掉（渲染层随内容面板卸载而复位）。
func TestLeaveClearsTheBannerStateButKeepsTheDrafts(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "留着的草稿"})
	e := &rr.screen.detail.edit
	key := draftKey(detailNoteID, sourceBody)
	e.err, e.errKey = "失败", key
	e.leave()
	if e.key != "" || e.err != "" || e.errKey != "" {
		t.Errorf("编辑态与红字条都应结束：%q %q %q", e.key, e.err, e.errKey)
	}
	if d, ok := e.drafts[key]; !ok || d.text != "留着的草稿" {
		t.Errorf("草稿保留：%+v %v", d, ok)
	}
}

// editError 自己再核一次：红字条只在「正在编辑、且错误属于这一份」时才有。syncEdit 在同一帧更早的位置已把
// 对不上的状态抹掉，所以界面上走不到这两个分支，只有直接调用才看得到（变异 E46：两道核对一起去掉）。
func TestEditErrorShowsOnlyWhileEditingTheSourceItBelongsTo(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	s := rr.screen
	item := *rr.view().Detail.Item
	e := &s.detail.edit
	body, summary := draftKey(detailNoteID, sourceBody), draftKey(detailNoteID, sourceSummary)
	cases := []struct {
		name            string
		editKey, errKey string
		want            string
	}{
		{"正在编辑这一份、错误也属于它", body, body, "失败原因"},
		{"错误属于另一份来源", body, summary, ""},
		{"错误属于这一份、但此刻没在编辑", "", body, ""},
		{"正在编辑另一份", summary, body, ""},
	}
	for _, c := range cases {
		e.key, e.err, e.errKey = c.editKey, "失败原因", c.errKey
		if got := s.editError(item, TabBody); got != c.want {
			t.Errorf("%s：红字条应为 %q，实际 %q", c.name, c.want, got)
		}
	}
}

// 保存在途时对同一份再调「完成编辑」不会发起第二次保存（界面上钮是禁用的，所以这道闸只有直接调用才看得到）。
func TestFinishDoesNotStartASecondSaveWhileTheSameSourceIsSaving(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "改了"})
	var queued []func()
	rr.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	item := *rr.view().Detail.Item
	rr.screen.finishEdit(item, TabBody)
	rr.screen.finishEdit(item, TabBody)
	if len(queued) != 1 {
		t.Fatalf("在途的那份不该再发起一次保存：%d 次", len(queued))
	}
}

// 没有草稿（没进过编辑态）就没有东西可存：什么也不发起，也不记在途标记。
func TestFinishWithoutADraftDoesNothing(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	var queued []func()
	rr.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	rr.screen.finishEdit(*rr.view().Detail.Item, TabBody)
	if len(queued) != 0 || rr.screen.detail.edit.saveKey != "" {
		t.Errorf("没有草稿时不保存：发起 %d 次，在途标记 %q", len(queued), rr.screen.detail.edit.saveKey)
	}
}

// 点「完成编辑」的那一刻，上一次失败的红字条就清掉（渲染层 finish 开头 setError(null)），不等结果回来。
func TestFinishClearsTheOldBannerAtOnce(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "改了"})
	key := draftKey(detailNoteID, sourceBody)
	e := &rr.screen.detail.edit
	e.err, e.errKey = "上一次的失败", key
	var queued []func()
	rr.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	rr.screen.finishEdit(*rr.view().Detail.Item, TabBody)
	if e.err != "" || e.errKey != "" {
		t.Errorf("开始保存就清掉旧红字条：%q %q", e.err, e.errKey)
	}
	if e.saveKey != key {
		t.Errorf("在途标记记下这一份：%q", e.saveKey)
	}
}

// 保存时详情里已经没有选中的条目（切到别的范围、关了详情）：按「文章已切换」失败，不能解引用空指针，也不写任何东西。
func TestSaveEditWithNothingSelectedFailsAsSwitched(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	rr.goTrash()
	if rr.view().Detail.Item != nil {
		t.Fatal("夹具失效：换范围之后选中应清空")
	}
	key := draftKey(detailNoteID, sourceBody)
	res := rr.screen.saveEdit(detailNoteID, sourceBody, key, editDraft{text: "改了", baseline: detailNoteBody})
	if res.done || res.key != key || res.err != locales.ThemedReadingReaderEditSwitched() {
		t.Fatalf("结果应是失败、原因是文章已切换：%+v", res)
	}
	if len(rr.svc.Drafts()) != 0 {
		t.Errorf("什么也不写：%+v", rr.svc.Drafts())
	}
}

// 保存失败要记后台日志（AGENTS.md：后台失败经 logAppError 留痕），原因就是给用户看的那句。
func TestAFailedSaveIsLoggedWithTheReason(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "我的改写"})
	rr.setDetail(func(d *DetailView) {
		it := *d.Item
		it.Content = "# 别处\n\n已被改过"
		d.Item = &it
	})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	want := "library.editSource|" + locales.ThemedReadingReaderEditConflict()
	for _, entry := range rr.log.entries() {
		if entry == want {
			return
		}
	}
	t.Errorf("日志里应有 %q：%q", want, rr.log.entries())
}

// 失败结果只有属于正在编辑的那一份时才摆红字条（直接走 settleSourceEdit：界面级的帧首清理会把摆错的状态擦掉，看不出来）。
func TestAFailureOfAnotherSourceDoesNotSetTheBanner(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	s := rr.screen
	e := &s.detail.edit
	e.post(editResult{key: "other:body", err: "别的条目的失败"})
	s.settleSourceEdit(e.key)
	if e.err != "" || e.errKey != "" {
		t.Fatalf("别的条目的失败不该摆到这一份上：%q %q", e.err, e.errKey)
	}
	e.post(editResult{key: e.key, err: "这一份的失败"})
	s.settleSourceEdit(e.key)
	if e.err != "这一份的失败" || e.errKey != e.key {
		t.Errorf("这一份的失败要记下：%q %q", e.err, e.errKey)
	}
}

// 红字条是 role=alert（渲染层 p[role=alert]）：屏幕阅读器要马上念出来，所以是 status + assertive 的活动区。
func TestTheBannerIsAnAssertiveStatusRegion(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "我的改写"})
	before := rr.assertiveStatusNodes()
	rr.setDetail(func(d *DetailView) {
		it := *d.Item
		it.Content = "# 别处\n\n已被改过"
		d.Item = &it
	})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	if !rr.hasText(locales.ThemedReadingReaderEditConflict()) {
		t.Fatal("先造出红字条")
	}
	if got := rr.assertiveStatusNodes(); got != before+1 {
		t.Errorf("红字条应是一个 assertive 的 status 节点：%d → %d", before, got)
	}
}

// 自动进编辑态只对空白的笔记（渲染层 startBlankNoteEditing 的条件含 itemType === "note"）；空白的视频等不进。
func TestABlankItemThatIsNotANoteDoesNotStartEditing(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	rr.svc.EditExternally(detailVideoID, func(it *services.LibraryItem) { it.Content = "" })
	rr.pick(detailVideoID)
	if rr.isEditing() {
		t.Fatal("空白的视频条目不是笔记，不自动进编辑态")
	}
}

// 没接阅读岛的界面（对照与测试里用的配置）里，标题框按 Enter 没有地方可交焦点，什么也不该发生，更不能崩。
func TestEnterInTheTitleWithoutAnIslandIsHarmless(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(detailNoteID)
	want := r.titleBox()
	r.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	r.keys("enter")
	if got := r.titleBox(); got != want {
		t.Errorf("Enter 不改标题：%q → %q", want, got)
	}
}

// 关闭表格视图的详情浮层时，里面的详情卸载，编辑态随之结束（草稿保留）；再打开不会接着停在编辑态。
func TestClosingTheDetailModalEndsTheEditingAndReopeningDoesNotResumeIt(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	entries := rr.openModal()
	rr.ready()
	rr.click(gfx.RoleButton, rr.editButtonName())
	if !rr.isEditing() {
		t.Fatal("夹具失效：应进入编辑态")
	}
	rr.screen.closeDetail()
	rr.frames(3)
	if rr.screen.DetailModalOpen() {
		t.Fatal("夹具失效：浮层应已关闭")
	}
	if rr.screen.detail.edit.key != "" {
		t.Errorf("浮层里的详情卸载，编辑态随之结束：%q", rr.screen.detail.edit.key)
	}
	x, y := rr.rowBlank(entries[0])
	rr.clickAt(x, y)
	rr.frames(3)
	if !rr.screen.DetailModalOpen() {
		t.Fatal("夹具失效：再点行应重新打开浮层")
	}
	if rr.isEditing() {
		t.Error("重新打开浮层不该接着停在编辑态")
	}
}
