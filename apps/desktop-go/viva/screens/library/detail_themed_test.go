package library

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	themedscreen "guizhi-mygo-shell/viva/screens/themed"
)

// 本文件测主题阅读（AI 阅读，台账 B-064）在知识库详情里的接线（detailthemed.go 与读者工具条、阅读区的分支）：
// 谁在什么条件下显示、模式切换去了哪里、切走时任务有没有被误取消。主题阅读自己的界面行为在 viva/screens/themed 的 L4 里测；
// 没接线时「AI 阅读」钮的「尚未迁移」说明由 detail_reader_test.go 的 TestUnmigratedReaderActionsFailLoudlyWithTheirMilestone 守着。
// 页面由岛宿主的假打开器扮演，不拉起 WebView2：真实页面里的渲染不在这里验证（台账 R-15）。

var themedNote = services.ThemedRequest{ItemID: detailNoteID, SourceKind: "body"}

// withThemed 把主题阅读界面接到阅读区：后台动作同步执行，阅读实例标识固定。
func withThemed(svc *servicestest.ThemedFake) func(*rig) {
	return func(r *rig) {
		th := themedscreen.New(svc, kit.Guard{Notify: r.notifier, Log: r.log}, nil)
		th.Look = r.look
		th.SetSpawn(func(fn func()) { fn() })
		th.Reading.NewInstanceID = func() string { return "inst-themed" }
		r.screen.Themed, r.themed = th, th
	}
}

func themedRig(t *testing.T, svc *servicestest.ThemedFake, mutate ...func(*rig)) *readerRig {
	t.Helper()
	return newReaderRig(t, append([]func(*rig){withThemed(svc)}, mutate...)...)
}

func TestThemedToggleSwitchesBetweenOriginalAndAIReading(t *testing.T) {
	t.Parallel()
	rr := themedRig(t, &servicestest.ThemedFake{})
	rr.openCard(detailNoteID)
	if rr.has(gfx.RoleButton, locales.ThemedReadingReaderCreate()) {
		t.Fatal("还没选 AI 阅读时阅读区是原文")
	}
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderAi())
	rr.frames(3)
	if !rr.has(gfx.RoleButton, locales.ThemedReadingReaderCreate()) {
		t.Fatalf("没有页面的条目选 AI 阅读应显示空态引导；画面上有 %q", rr.session.Texts())
	}
	if rr.has(gfx.RoleButton, locales.LibraryCatalog()) {
		t.Fatal("AI 阅读下目录让位给阅读页设置")
	}
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderOriginal())
	rr.frames(3)
	if rr.has(gfx.RoleButton, locales.ThemedReadingReaderCreate()) {
		t.Fatal("切回原文后不该再显示 AI 阅读的空态")
	}
	if !rr.has(gfx.RoleButton, locales.LibraryCatalog()) {
		t.Fatal("切回原文后目录回来")
	}
	if strings.Contains(rr.toastText(), "B-064") {
		t.Fatalf("接线后不该再弹「尚未迁移」：%q", rr.toastText())
	}
}

func TestThemedItemWithPageOpensInAIReadingAndFeedsTheIsland(t *testing.T) {
	t.Parallel()
	svc := &servicestest.ThemedFake{}
	svc.PutPage(themedNote, services.ThemedPage{ID: "page-1"})
	rr := themedRig(t, svc)
	rr.openCard(detailNoteID)
	if !rr.has(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) {
		t.Fatalf("有页面的条目默认进 AI 阅读，工具栏应有「阅读页设置」；画面上有 %q", rr.session.Texts())
	}
	p := rr.ready()
	got := rr.viewOf(p)
	if got["kind"] != "snapshot" || got["document"] != "<html>page page-1 instance inst-themed</html>" || got["instanceId"] != "inst-themed" || got["width"] != "fluid" {
		t.Fatalf("岛里应是外壳渲染的主题页（流式宽度、阅读实例标识）：%v", got)
	}
}

func TestThemedOriginalModeFeedsTheOriginalToTheIsland(t *testing.T) {
	t.Parallel()
	svc := &servicestest.ThemedFake{}
	svc.PutPage(themedNote, services.ThemedPage{ID: "page-1"})
	rr := themedRig(t, svc)
	rr.openCard(detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderOriginal())
	rr.frames(3)
	p := rr.ready()
	if got := rr.viewOf(p); got["kind"] == "snapshot" {
		t.Fatalf("切回原文后岛里不该还是主题页：%v", got)
	}
	if rr.has(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) {
		t.Fatal("原文模式不画「阅读页设置」")
	}
}

func TestThemedTrashedItemShowsPageReadOnly(t *testing.T) {
	t.Parallel()
	svc := &servicestest.ThemedFake{}
	svc.PutPage(services.ThemedRequest{ItemID: shellTrashNoteID, SourceKind: "body"}, services.ThemedPage{ID: "page-t"})
	rr := themedRig(t, svc)
	rr.pickTrashed(shellTrashNoteID)
	rr.frames(3)
	if !rr.hasText(locales.ThemedReadingReaderReadOnly()) {
		t.Fatalf("回收站里的条目应提示只读；画面上有 %q", rr.session.Texts())
	}
	rr.click(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	if n, ok := rr.node(gfx.RoleMenuItem, locales.ThemedReadingReaderDelete()); !ok || !n.Disabled {
		t.Fatalf("回收站里不能删除 AI 阅读页：%+v ok=%v", n, ok)
	}
	if n, ok := rr.node(gfx.RoleMenuItem, locales.ThemedReadingExport()); !ok || n.Disabled {
		t.Fatalf("回收站里仍可导出：%+v ok=%v", n, ok)
	}
}

func TestThemedEditingTheOriginalLeavesAIReading(t *testing.T) {
	t.Parallel()
	svc := &servicestest.ThemedFake{}
	svc.PutPage(themedNote, services.ThemedPage{ID: "page-1"})
	rr := themedRig(t, svc)
	rr.openCard(detailNoteID)
	if !rr.has(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) {
		t.Fatal("前置：应在 AI 阅读")
	}
	rr.click(gfx.RoleButton, rr.editButtonName())
	rr.frames(3)
	if !rr.isEditing() {
		t.Fatal("应进入编辑")
	}
	if rr.has(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) {
		t.Fatal("编辑原文时 AI 阅读退出显示")
	}
}

func TestThemedSwitchingItemOrClosingDetailNeverCancelsTheTask(t *testing.T) {
	t.Parallel()
	svc := &servicestest.ThemedFake{}
	task := svc.PutTask(services.ThemedTask{
		ItemID: themedNote.ItemID, SourceKind: themedNote.SourceKind, Title: "note task",
		State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite,
	})
	rr := themedRig(t, svc)
	rr.openCard(detailNoteID)
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderAi())
	rr.frames(3)
	if !rr.has(gfx.RoleButton, locales.ThemedReadingStop()) || svc.Subscribers() != 1 {
		t.Fatalf("前置：应显示执行中的任务并订阅进度（订阅 %d）；画面上有 %q", svc.Subscribers(), rr.session.Texts())
	}

	rr.pick(detailVideoID) // 切到别的条目：订阅换成新条目的（没有任务），旧任务不受影响
	rr.frames(3)
	if n := svc.Subscribers(); n != 1 {
		t.Fatalf("切到别的条目应只剩新条目的订阅，实际 %d 个", n)
	}
	if rr.has(gfx.RoleButton, locales.ThemedReadingStop()) {
		t.Fatal("别的条目不该显示这个任务")
	}
	rr.store.SelectItem("") // 关闭详情
	rr.frames(3)
	if n := svc.Subscribers(); n != 0 {
		t.Fatalf("关闭详情应退订进度，仍有 %d 个", n)
	}
	if got := svc.Calls().Cancels; len(got) != 0 {
		t.Fatalf("切走与关闭不能隐式取消已付费的生成：%v", got)
	}
	if tk, _ := svc.Task(task.ID); tk.State != services.ThemedTaskRunning {
		t.Fatalf("任务应仍在执行：%+v", tk)
	}

	rr.pick(detailNoteID) // 回来：任务仍在，状态卡重新出现
	rr.click(gfx.RoleButton, locales.ThemedReadingReaderAi())
	rr.frames(3)
	if !rr.has(gfx.RoleButton, locales.ThemedReadingStop()) {
		t.Fatalf("回来后应仍能看到并停止这个任务；画面上有 %q", rr.session.Texts())
	}
}

func TestThemedKeepsTheArticleAskBar(t *testing.T) {
	t.Parallel()
	svc := &servicestest.ThemedFake{}
	svc.PutPage(themedNote, services.ThemedPage{ID: "page-1"})
	rr := themedRig(t, svc, withArticleAsk)
	rr.openCard(detailNoteID)
	if !rr.has(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) {
		t.Fatal("前置：应在 AI 阅读")
	}
	rr.toggleAskButton()
	if _, ok := rr.askPane(); !ok {
		t.Fatalf("AI 阅读下点「提问」应展开提问栏；画面上有 %q", rr.session.Texts())
	}
	if !rr.has(gfx.RoleCombobox, locales.ThemedReadingReaderSettings()) {
		t.Fatal("提问栏展开后阅读区仍是 AI 阅读")
	}
	rr.click(gfx.RoleTextField, locales.ArticleAskQuestion())
	rr.typeText("这篇讲了什么")
	rr.click(gfx.RoleButton, locales.AskSend())
	if calls := rr.askSvc.ArticleCalls(); len(calls) != 1 || calls[0].Question != "这篇讲了什么" {
		t.Fatalf("AI 阅读下提问栏应能发送，服务收到 %+v", calls)
	}
}

func TestShowThemedTaskRemembersTheRequestedSourceAsAIReading(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		source string
		tab    ReadingTab
	}{{"body", TabBody}, {"summary", TabSummary}} {
		rr := themedRig(t, &servicestest.ThemedFake{})
		rr.screen.ShowThemedTask(detailForumID, c.source)
		mem, ok := LoadReadingMemory(rr.prefs, detailForumID, rigNow)
		if !ok || mem.Tab != c.tab || !mem.ThemedBySource[c.source] {
			t.Fatalf("%s 任务应让条目停在 %s 标签并记住 AI 阅读：%+v ok=%v", c.source, c.tab, mem, ok)
		}
	}
}
