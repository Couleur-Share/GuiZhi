package library

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 本文件测阅读区编辑态的界面一侧（detailedit.go、detailreadertoolbar.go 的编辑分支、detailreader.go 的红字条）：
// 点「编辑」之后岛里换成编辑器、页面报来的改动记成草稿、「完成编辑」写回条目并落盘、失败时的红字条与错误提示、空白笔记自动进编辑态、
// 换条目或标签与详情重挂时编辑态复位而草稿保留。页面由测试扮演（fakeReaderPage），编辑器本身（CodeMirror）在岛里，
// 不在这里测——真实页面里的行为由 viva/cmd/readersmoke 的编辑场景覆盖（台账 R-15 的范围）。

const detailNoteBody = "# 并发模式\n\n正文内容。"

// lastView 是页面最近收到的 view 消息里的视图（岛里此刻该显示的内容）。
func lastView(t *testing.T, p *fakeReaderPage) map[string]any {
	t.Helper()
	v, ok := p.last(t, "view")["view"].(map[string]any)
	if !ok {
		t.Fatal("view 消息里没有视图对象")
	}
	return v
}

// openedForEdit 选中 id、让岛完成握手。
func openedForEdit(t *testing.T, id string) (*readerRig, *fakeReaderPage) {
	t.Helper()
	rr := newReaderRig(t)
	rr.pick(id)
	return rr, rr.ready()
}

// flat 把画面上的文字行拼成一串：错误提示的详情会按宽度折成几行，断言一句话在不在要拼起来再找。
func flat(lines string) string { return strings.ReplaceAll(lines, "\n", "") }

// editDraftOf 是某条某个来源的草稿。
func (rr *readerRig) editDraftOf(id, kind string) (editDraft, bool) {
	d, ok := rr.screen.detail.edit.drafts[draftKey(id, kind)]
	return d, ok
}

// editing 报告界面此刻是不是在编辑（工具条上是「完成编辑」而不是「编辑」）。
func (rr *readerRig) isEditing() bool {
	return rr.has(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing()) || rr.has(gfx.RoleButton, locales.ThemedReadingReaderSaving())
}

// 点「编辑」：岛里换成编辑器，文档是原文；工具条换成「正在编辑原文」与「完成编辑」，版本切换、目录、编辑都收起来。
func TestEditButtonOpensTheEditorWithTheOriginalAndSwitchesTheToolbar(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	if got := lastView(t, p)["kind"]; got != "original" {
		t.Fatalf("编辑之前岛里是原文：%v", got)
	}
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())

	want, _ := readingSourceText(*rr.view().Detail.Item, sourceBody)
	if v := lastView(t, p); v["kind"] != "editor" || v["sourceKind"] != "body" || v["content"] != want {
		t.Fatalf("岛里应换成正文的编辑器、文档是原文：%+v", v)
	}
	if !rr.hasText(locales.ThemedReadingReaderEditing()) || !rr.has(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing()) {
		t.Error("工具条应是「正在编辑原文」加「完成编辑」")
	}
	for _, gone := range []string{locales.ThemedReadingReaderOriginal(), locales.ThemedReadingReaderAi(), locales.LibraryCatalog(), locales.ThemedReadingReaderEditOriginal()} {
		if rr.has(gfx.RoleButton, gone) {
			t.Errorf("编辑态下工具条里不该有「%s」", gone)
		}
	}
	d, ok := rr.editDraftOf(detailNoteID, sourceBody)
	if !ok || d.text != want || d.baseline != want || d.touched {
		t.Errorf("进入编辑态时草稿与基线都是原文、还没改过：%+v %v", d, ok)
	}
}

// 论坛条目停在讨论总结标签时，编辑的是总结小节，按钮叫「编辑讨论总结」。
func TestEditOnTheDiscussionSummaryEditsTheSummarySource(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailForumID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditSummary())
	want, _ := readingSourceText(*rr.view().Detail.Item, sourceSummary)
	if v := lastView(t, p); v["kind"] != "editor" || v["sourceKind"] != "summary" || v["content"] != want || want == "" {
		t.Fatalf("岛里应是讨论总结的编辑器、文档是总结小节（%q）：%+v", want, v)
	}
	if _, ok := rr.editDraftOf(detailForumID, sourceSummary); !ok {
		t.Error("草稿记在 summary 来源名下")
	}
	if _, ok := rr.editDraftOf(detailForumID, sourceBody); ok {
		t.Error("没有动正文的草稿")
	}
}

// 页面每次改动都把全文报回来，界面记成草稿；宿主不会把它再发回页面（否则光标会跳）。
func TestEditEventsFeedTheDraftWithoutResendingTheDocument(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	original, _ := readingSourceText(*rr.view().Detail.Item, sourceBody)
	before := len(p.messages(t, "view"))

	rr.event(p, map[string]any{"t": "edit", "text": "新的正文"})
	rr.event(p, map[string]any{"t": "edit", "text": "新的正文，又多了一句"})

	d, ok := rr.editDraftOf(detailNoteID, sourceBody)
	if !ok || d.text != "新的正文，又多了一句" || !d.touched || d.baseline != original {
		t.Fatalf("草稿应是最后一次报来的全文、记为改过、基线仍是原文：%+v", d)
	}
	if after := len(p.messages(t, "view")); after != before {
		t.Errorf("页面自己报来的改动不该再发回页面：view 消息 %d → %d", before, after)
	}
	if !rr.isEditing() {
		t.Error("改动之后仍在编辑态")
	}
}

// 草稿跨条目保留：离开再回来，从草稿接着编辑，基线仍是第一次开始编辑时的原文。
func TestDraftSurvivesLeavingAndComingBack(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	original, _ := readingSourceText(*rr.view().Detail.Item, sourceBody)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "改过的正文"})

	rr.pick(detailForumID)
	if rr.isEditing() {
		t.Fatal("换了条目就不在编辑态了")
	}
	rr.pick(detailNoteID)
	if rr.isEditing() {
		t.Fatal("回来也不自动进编辑态（不是空白笔记）")
	}
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	if v := lastView(t, p); v["kind"] != "editor" || v["content"] != "改过的正文" {
		t.Fatalf("再点编辑应从草稿接着改：%+v", v)
	}
	if d, _ := rr.editDraftOf(detailNoteID, sourceBody); d.baseline != original || !d.touched {
		t.Errorf("基线仍是第一次开始编辑时的原文：%+v", d)
	}
}

// 没改过就离开的编辑态不留草稿：下次进来取的是最新的原文，不是旧的。
func TestUntouchedEditStartsOverFromTheLatestOriginal(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.pick(detailForumID)

	rr.svc.EditExternally(detailNoteID, func(it *services.LibraryItem) { it.Content = "# 新版\n\n别处改的" })
	rr.pick(detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	if v := lastView(t, p); v["kind"] != "editor" || v["content"] != "# 新版\n\n别处改的" {
		t.Fatalf("没改过的编辑态下次从最新的原文开始：%+v", v)
	}
	if d, _ := rr.editDraftOf(detailNoteID, sourceBody); d.baseline != "# 新版\n\n别处改的" {
		t.Errorf("基线也取最新的原文：%+v", d)
	}
}

// 换标签（正文 ↔ 讨论总结）也结束编辑态，两个来源的草稿各是各的。
func TestEditingEndsWhenTheTabChangesAndEachSourceKeepsItsOwnDraft(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailForumID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditSummary())
	rr.event(p, map[string]any{"t": "edit", "text": "总结的草稿"})

	rr.click(gfx.RoleButton, locales.LibraryBodySection())
	if rr.isEditing() || !rr.has(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal()) {
		t.Fatal("换到正文标签：不在编辑态，工具条是正文的「编辑原文」")
	}
	rr.click(gfx.RoleButton, locales.LibraryForumSummarySection())
	if rr.isEditing() {
		t.Fatal("换回总结标签也不自动回到编辑态")
	}
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditSummary())
	if v := lastView(t, p); v["kind"] != "editor" || v["sourceKind"] != "summary" || v["content"] != "总结的草稿" {
		t.Fatalf("总结的草稿保留：%+v", v)
	}
	if _, ok := rr.editDraftOf(detailForumID, sourceBody); ok {
		t.Error("正文那份没有进过编辑态，不该有草稿")
	}
}

// 「完成编辑」把草稿写回条目并落盘，岛里回到原文（内容是改后的），草稿删掉。
func TestFinishWritesTheDraftBackAndLeavesTheEditor(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "# 新\n\n改写后的正文。"})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())

	saved, _ := rr.svc.ItemNow(detailNoteID)
	if saved.Content != "# 新\n\n改写后的正文。" {
		t.Fatalf("服务里的正文应是草稿：%q", saved.Content)
	}
	if got := rr.view().Detail.Item.Content; got != saved.Content {
		t.Errorf("界面里的条目随之更新：%q", got)
	}
	drafts := rr.svc.Drafts()
	if len(drafts) != 1 || drafts[0].Patch.Content == nil || *drafts[0].Patch.Content != saved.Content {
		t.Errorf("应落盘一次、补丁是正文：%+v", drafts)
	}
	if rr.isEditing() || !rr.has(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal()) {
		t.Error("完成之后回到阅读态，工具条恢复")
	}
	if _, ok := rr.editDraftOf(detailNoteID, sourceBody); ok {
		t.Error("完成成功要删掉草稿")
	}
	if v := lastView(t, p); v["kind"] != "original" || v["content"] != saved.Content {
		t.Errorf("岛里回到改后的原文：%+v", v)
	}
	if rr.hasText(locales.ThemedReadingReaderEditFailed()) {
		t.Error("成功时没有失败提示")
	}
}

// 讨论总结只改总结小节，正文与讨论原样。
func TestFinishOnTheSummaryReplacesOnlyTheSummarySection(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailForumID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditSummary())
	rr.event(p, map[string]any{"t": "edit", "text": "改写的总结。"})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())

	want, err := notes.ReplaceReadingSource(detailForumContent, shapeOf("forum"), sourceSummary, "改写的总结。")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := rr.svc.ItemNow(detailForumID); got.Content != want {
		t.Fatalf("只替换总结小节：\n%q\n期望\n%q", got.Content, want)
	}
	if rr.isEditing() {
		t.Error("完成之后回到阅读态")
	}
}

// 没改动也要走完整个保存：渲染层照样 updateSelected 再落盘，然后退出编辑态。
func TestFinishWithoutChangesStillLeavesTheEditor(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	if rr.isEditing() {
		t.Fatal("没改动也退出编辑态")
	}
	if got, _ := rr.svc.ItemNow(detailNoteID); got.Content != detailNoteBody {
		t.Errorf("正文不变：%q", got.Content)
	}
}

// 保存期间钮禁用、文字是「正在保存…」，重复点击没有第二次保存；结果回来后才退出。
func TestFinishShowsSavingAndIgnoresClicksUntilTheResultSettles(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "改了"})

	var queued []func()
	rr.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	if len(queued) == 0 {
		t.Fatal("完成编辑应在后台保存")
	}
	node := rr.mustNode(gfx.RoleButton, locales.ThemedReadingReaderSaving())
	if !node.Disabled {
		t.Error("保存期间钮禁用")
	}
	if rr.has(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing()) {
		t.Error("文字换成了「正在保存…」")
	}
	n := len(queued)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderSaving())
	if len(queued) != n {
		t.Error("禁用的钮点了不该再发起一次保存")
	}
	if !rr.isEditing() {
		t.Fatal("结果回来之前仍在编辑态")
	}
	for len(queued) > 0 {
		fn := queued[0]
		queued = queued[1:]
		fn()
	}
	rr.frames(3)
	if rr.isEditing() {
		t.Fatal("结果回来之后退出编辑态")
	}
	if got, _ := rr.svc.ItemNow(detailNoteID); got.Content != "改了" {
		t.Errorf("正文：%q", got.Content)
	}
}

// 保存中的标记按来源各记：总结在保存时，切到正文标签不受影响。
func TestSavingIsTrackedPerSource(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailForumID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditSummary())
	rr.event(p, map[string]any{"t": "edit", "text": "总结改了"})
	var queued []func()
	rr.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())

	rr.click(gfx.RoleButton, locales.LibraryBodySection())
	if !rr.has(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal()) || rr.has(gfx.RoleButton, locales.ThemedReadingReaderSaving()) {
		t.Fatal("正文标签上不显示总结的保存状态")
	}
	rr.store.Spawn = func(fn func()) { fn() }
	for len(queued) > 0 {
		fn := queued[0]
		queued = queued[1:]
		fn()
	}
	rr.frames(3)
	if _, ok := rr.editDraftOf(detailForumID, sourceSummary); ok {
		t.Error("总结保存成功，草稿删掉（即使这时看的是正文标签）")
	}
	if got, _ := rr.svc.ItemNow(detailForumID); !strings.Contains(got.Content, "总结改了") {
		t.Errorf("服务里应有改后的总结：%q", got.Content)
	}
}

// 原文在编辑期间被别处改过（与基线和草稿都不同）：不写、红字条与错误提示都给出原因，仍在编辑态、草稿还在。
func TestFinishRefusesWhenTheOriginalChangedMeanwhile(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "我的改写"})
	rr.setDetail(func(d *DetailView) {
		it := *d.Item
		it.Content = "# 别处\n\n已被改过"
		d.Item = &it
	})
	views := len(p.messages(t, "view"))
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())

	conflict := locales.ThemedReadingReaderEditConflict()
	if !rr.hasText(conflict) {
		t.Fatal("红字条应写明原文已更新")
	}
	if !rr.hasText(locales.ThemedReadingReaderEditFailed()) {
		t.Error("还要弹「保存原文失败」的错误提示")
	}
	if text := flat(rr.toastText()); strings.Count(text, conflict) < 2 {
		t.Errorf("提示的详情里也写明原因（红字条一处、提示一处）：\n%s", text)
	}
	if !rr.isEditing() {
		t.Fatal("失败后仍在编辑态")
	}
	if len(rr.svc.Drafts()) != 0 {
		t.Errorf("冲突时什么也不能写：%+v", rr.svc.Drafts())
	}
	if d, ok := rr.editDraftOf(detailNoteID, sourceBody); !ok || d.text != "我的改写" {
		t.Errorf("草稿保留：%+v", d)
	}
	if v := lastView(t, p); v["kind"] != "editor" {
		t.Errorf("岛里仍是编辑器：%+v", v)
	}
	if n := len(p.messages(t, "view")); n != views {
		t.Errorf("失败后页面里的文档原样不动（不重发视图）：view 消息 %d → %d", views, n)
	}
}

// 再点「完成编辑」会先清掉红字条；与基线相同（原文没被改）就不算冲突。
func TestRetryingClearsTheBannerAndAnOriginalEqualToTheDraftIsNoConflict(t *testing.T) {
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
	if !rr.hasText(locales.ThemedReadingReaderEditConflict()) {
		t.Fatal("先造出红字条")
	}
	// 别处的改动恰好与草稿一致（服务里与界面里都是）：不算冲突，再点就能完成。
	rr.svc.EditExternally(detailNoteID, func(it *services.LibraryItem) { it.Content = "我的改写" })
	rr.setDetail(func(d *DetailView) {
		it := *d.Item
		it.Content = "我的改写"
		d.Item = &it
	})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	if rr.hasText(locales.ThemedReadingReaderEditConflict()) || rr.isEditing() {
		t.Fatal("重试成功：红字条消失、退出编辑态")
	}
}

// 保存时最新的条目没有讨论总结（不是论坛条目）：原因交给错误提示，什么也不写。
// 界面上走不到这里（总结页签只在论坛条目上有），所以直接走保存这一步，核对失败的出口接对了。
func TestSaveEditReportsAMissingSummarySection(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	key := draftKey(detailNoteID, sourceSummary)
	res := rr.screen.saveEdit(detailNoteID, sourceSummary, key, editDraft{text: "总结改了", baseline: ""})
	if res.done || res.key != key || res.err != notes.ErrNoSummary.Error() {
		t.Fatalf("结果应是失败、原因是没有讨论总结：%+v", res)
	}
	rr.frames(2)
	if !rr.hasText(locales.ThemedReadingReaderEditFailed()) {
		t.Error("应弹「保存原文失败」")
	}
	if len(rr.svc.Drafts()) != 0 {
		t.Errorf("什么也不写：%+v", rr.svc.Drafts())
	}
}

// 保存时选中的已经不是这一条（切走了）：不写、弹错误提示；红字条只属于还在编辑的那一份，不会摆到别的条目上。
func TestFinishAfterSwitchingItemsFailsWithAToastAndNoBanner(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "切走之前的改写"})
	sync := rr.store.Spawn
	var queued []func()
	rr.store.Spawn = func(fn func()) { queued = append(queued, fn) }
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	rr.store.Spawn = sync
	rr.pick(detailForumID)
	for _, fn := range queued {
		fn()
	}
	rr.frames(3)

	if !rr.hasText(locales.ThemedReadingReaderEditFailed()) {
		t.Fatal("应弹「保存原文失败」")
	}
	switched := locales.ThemedReadingReaderEditSwitched()
	text := flat(rr.toastText())
	if !strings.Contains(text, switched) {
		t.Errorf("提示的详情应写明文章已切换：\n%s", text)
	}
	if got, _ := rr.svc.ItemNow(detailNoteID); got.Content != detailNoteBody {
		t.Errorf("没写：%q", got.Content)
	}
	if d, ok := rr.editDraftOf(detailNoteID, sourceBody); !ok || d.text != "切走之前的改写" {
		t.Errorf("草稿保留：%+v", d)
	}
	if rr.screen.detail.edit.saveKey != "" {
		t.Error("在途标记随结果清掉")
	}
	if n := strings.Count(text, switched); n != 1 {
		t.Errorf("别的条目上不该有红字条：「%s」出现 %d 次（提示详情里一次）", switched, n)
	}
}

// 落盘没成功时原因由头部的保存状态说，这里静默：仍在编辑态、没有红字条、没有错误提示，改动留在待保存里。
func TestFinishStaysSilentWhenTheSaveItselfFails(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "写不进去"})
	rr.svc.FailDraft("磁盘已满")
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())

	if !rr.isEditing() {
		t.Fatal("落盘失败后仍在编辑态")
	}
	if rr.hasText(locales.ThemedReadingReaderEditFailed()) {
		t.Error("落盘失败由保存状态说，不再弹「保存原文失败」")
	}
	if rr.view().Detail.SaveError == "" {
		t.Error("保存状态里应有失败原因")
	}
	if d, ok := rr.editDraftOf(detailNoteID, sourceBody); !ok || d.text != "写不进去" {
		t.Errorf("草稿保留：%+v", d)
	}
	if rr.screen.detail.edit.saveKey != "" {
		t.Error("在途标记要清掉，才能再点一次")
	}
	rr.svc.FailDraft("")
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	if rr.isEditing() {
		t.Fatal("故障排除后再点一次就能完成")
	}
}

// 空白笔记打开就直接在编辑态（输入第一个字之后不会被切回阅读态）；完成后再打开仍然如此。
func TestBlankNoteOpensInTheEditor(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, shellBlankID)
	if v := lastView(t, p); v["kind"] != "editor" || v["content"] != "" || v["sourceKind"] != "body" {
		t.Fatalf("空白笔记打开就是空文档的编辑器：%+v", v)
	}
	if !rr.isEditing() {
		t.Fatal("工具条是编辑态")
	}
	rr.event(p, map[string]any{"t": "edit", "text": "第一行"})
	if !rr.isEditing() {
		t.Fatal("输入第一个字之后仍在编辑态")
	}
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	if rr.isEditing() {
		t.Fatal("完成之后回到阅读态")
	}
	if got, _ := rr.svc.ItemNow(shellBlankID); got.Content != "第一行" {
		t.Errorf("正文：%q", got.Content)
	}
}

// 空白按 JS trim 判断（含不换行空格与字节序标记），只在切到该条目时判断一次；回收站里的与非笔记不算。
func TestBlankNoteEditingFollowsTheRendererRule(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	if rr.isEditing() {
		t.Fatal("有内容的笔记不自动进编辑态")
	}
	rr.svc.EditExternally(detailNoteID, func(it *services.LibraryItem) { it.Content = " \n\t\u00a0\ufeff " })
	rr.pick(detailForumID)
	rr.pick(detailNoteID)
	if !rr.isEditing() || lastView(t, p)["kind"] != "editor" {
		t.Fatal("只含空白字符（含 U+00A0、U+FEFF）的笔记算空白")
	}
	// 完成（不改）→ 回到阅读态；同一条里不会再次被切回（只在切到该条目时判断）。
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	rr.frames(5)
	if rr.isEditing() {
		t.Fatal("完成之后不再自动进编辑态")
	}
	rr.pick(detailForumID)
	rr.pick(detailNoteID)
	if !rr.isEditing() {
		t.Fatal("再次切到这条（内容仍是空白）又自动进编辑态")
	}
	rr.pick(detailVideoID)
	if rr.isEditing() {
		t.Fatal("视频条目不是笔记，不自动进编辑态")
	}
	rr.pickTrashed(shellTrashForumID)
	rr.svc.EditExternally(shellTrashNoteID, func(it *services.LibraryItem) { it.Content = "" })
	rr.pick(shellTrashNoteID)
	if rr.isEditing() || rr.has(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal()) {
		t.Fatal("回收站里的空白笔记只读，不进编辑态")
	}
}

// 文档超过岛里编辑器能处理的长度时拒绝进入并给出原因，而不是让页面处理不了一直卡着。
func TestEditRefusesADocumentTooLongForTheEditor(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID) // 岛没握手：不会真的把几兆的文档发出去
	big := strings.Repeat("a", reader.MaxEditChars+1)
	rr.setDetail(func(d *DetailView) {
		it := *d.Item
		it.Content = big
		d.Item = &it
	})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	if rr.isEditing() {
		t.Fatal("超长文档不进编辑态")
	}
	if text := rr.toastText(); !strings.Contains(text, fmt.Sprint(reader.MaxEditChars+1)) || !strings.Contains(text, fmt.Sprint(reader.MaxEditChars)) {
		t.Errorf("提示应写明文档长度与上限：\n%s", text)
	}
	if _, ok := rr.editDraftOf(detailNoteID, sourceBody); ok {
		t.Error("没进编辑态就不建草稿")
	}
}

// 标题框里按 Enter 把焦点交给编辑器（渲染层把光标放进 .cm-content）；不在编辑态时没有这回事。
func TestEnterInTheTitleFocusesTheEditorOnlyWhileEditing(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	rr.keys("enter")
	if n := len(p.messages(t, "focus")); n != 0 {
		t.Fatalf("不在编辑态：Enter 不该要求页面聚焦（%d 条 focus 消息）", n)
	}

	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	focuses := p.focuses
	rr.click(gfx.RoleTextField, locales.LibraryTitlePlaceholder())
	rr.keys("enter")
	if n := len(p.messages(t, "focus")); n != 1 {
		t.Fatalf("编辑态：Enter 要求页面把焦点给编辑器一次，实际 %d 条", n)
	}
	if p.focuses <= focuses {
		t.Error("同时要把键盘焦点交给岛的页面")
	}
	if got := rr.titleBox(); got != detailEntryOf(detailNoteID).Title {
		t.Errorf("Enter 不改标题：%q", got)
	}
}

// 详情栏重挂（专注阅读切换）与没有可显示的条目时，编辑态结束、草稿保留。
func TestEditingEndsWhenTheDetailRemountsOrNothingIsShown(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.event(p, map[string]any{"t": "edit", "text": "重挂之前的草稿"})

	rr.screen.SetFocusReading(true)
	rr.frames(3)
	if rr.isEditing() {
		t.Fatal("详情重挂之后不在编辑态")
	}
	if d, ok := rr.editDraftOf(detailNoteID, sourceBody); !ok || d.text != "重挂之前的草稿" {
		t.Fatalf("草稿保留：%+v", d)
	}
	rr.screen.SetFocusReading(false)
	rr.frames(3)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	if v := lastView(t, p); v["kind"] != "editor" || v["content"] != "重挂之前的草稿" {
		t.Fatalf("再点编辑从草稿接着改：%+v", v)
	}

	rr.goTrash() // 换范围：选中清空，详情栏是「请选择」
	if !rr.hasText(locales.LibraryNoSelection()) || rr.isEditing() {
		t.Fatal("没有选中的条目：内容面板不在树里，不在编辑态")
	}
	rr.pick(detailNoteID)
	if rr.isEditing() {
		t.Fatal("重新选中也要重新点「编辑」（不是空白笔记）")
	}
}

// 保存结果属于别的条目或标签时，只处理它自己的草稿与在途标记，不碰当前面板的查找位置；属于当前这份才复位查找。
func TestSettlingOnlyResetsTheFindPositionForTheShownSource(t *testing.T) {
	t.Parallel()
	rr, _ := openedForEdit(t, detailNoteID)
	e := &rr.screen.detail.edit
	e.drafts = map[string]editDraft{"other:body": {text: "x", baseline: "y", touched: true}}
	e.saveKey = "other:body"
	rr.screen.detail.panel.findActive = 2

	e.post(editResult{key: "other:body", done: true})
	rr.frames(2)
	if _, ok := e.drafts["other:body"]; ok || e.saveKey != "" {
		t.Errorf("别的来源保存成功：它的草稿删掉、在途标记清掉：%+v %q", e.drafts, e.saveKey)
	}
	if rr.screen.detail.panel.findActive != 2 {
		t.Error("别的来源的结果不该动当前面板的查找位置")
	}

	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditOriginal())
	rr.screen.detail.panel.findActive = 3
	e.post(editResult{key: draftKey(detailNoteID, sourceBody), done: true})
	rr.frames(2)
	if rr.screen.detail.panel.findActive != 0 || rr.isEditing() {
		t.Error("当前这份保存成功：退出编辑态、查找位置归零")
	}
}

// 编辑态的红字条只在编辑那一份时出现：换了标签它就消失，不会残留到别处。
func TestBannerBelongsToTheSourceBeingEdited(t *testing.T) {
	t.Parallel()
	rr, p := openedForEdit(t, detailForumID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderEditSummary())
	rr.event(p, map[string]any{"t": "edit", "text": "总结改了"})
	rr.setDetail(func(d *DetailView) {
		it := *d.Item
		it.Content = strings.Replace(it.Content, "大家认为应使用 VACUUM INTO。", "别处改过的总结。", 1)
		d.Item = &it
	})
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderFinishEditing())
	conflict := locales.ThemedReadingReaderEditConflict()
	if !rr.hasText(conflict) {
		t.Fatal("先造出红字条")
	}
	rr.click(gfx.RoleButton, locales.LibraryBodySection())
	if rr.hasText(conflict) {
		t.Error("换了标签，红字条随编辑态一起消失")
	}
	if rr.screen.detail.edit.err != "" || rr.screen.detail.edit.errKey != "" {
		t.Errorf("红字条的状态也要清掉：%q %q", rr.screen.detail.edit.err, rr.screen.detail.edit.errKey)
	}
}
