package library

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/viva/locales"
)

// 选段提问：发出之后选段与提示语就用完了（渲染层 onSubmitted 清掉 selection 与 prompt），下一问不能再带着旧选段。
// 第一问把选段挂成引用并发出；第二问是用户另外敲的，服务收到的目标里不该还有选段，栏里也不该再显示那段引用。
func TestAskSelectionIsConsumedBySendingTheQuestion(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	r.screen.AskSelection(detailNoteID, "并发模式", nil, true)
	r.frames(4)
	if got := r.screen.detail.ask; got.selection == nil || got.prompt == "" {
		t.Fatalf("夹具失效：选段提问应带着选段与提示语，实际 %+v", got)
	}

	r.click(gfx.RoleButton, locales.AskSend())
	calls := r.askSvc.ArticleCalls()
	if len(calls) != 1 || calls[0].Target.Selection == nil || *calls[0].Target.Selection != "并发模式" {
		t.Fatalf("第一问应带着选段发出，服务收到 %+v", calls)
	}
	r.frames(3)
	if got := r.screen.detail.ask; got.selection != nil || got.prompt != "" {
		t.Fatalf("发出之后应清掉选段与提示语，实际 selection=%v prompt=%q", got.selection, got.prompt)
	}

	r.click(gfx.RoleTextField, locales.ArticleAskQuestion())
	r.typeText("换个问题")
	r.click(gfx.RoleButton, locales.AskSend())
	calls = r.askSvc.ArticleCalls()
	if len(calls) != 2 {
		t.Fatalf("第二问应发出，服务收到 %+v", calls)
	}
	if calls[1].Target.Selection != nil {
		t.Fatalf("第二问不该再带着第一问的选段：%+v", calls[1].Target)
	}
}

// 提问栏只属于打开它的那一条：换到别的条目时就地复位，再换回来栏是收起的（不能「记着」上次开过）。
func TestAskPaneStaysClosedWhenReturningToTheItemItWasOpenedOn(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	r.toggleAskButton()
	if _, ok := r.askPane(); !ok {
		t.Fatal("夹具失效：应已打开提问栏")
	}

	r.pick(detailForumID)
	if _, ok := r.askPane(); ok {
		t.Fatal("换条目应收起提问栏")
	}
	if got := r.screen.detail.ask.openID; got != "" {
		t.Fatalf("换条目后不该还记着上一条的开合，实际 %q", got)
	}

	r.pick(detailNoteID)
	if _, ok := r.askPane(); ok || r.askButton() {
		t.Fatal("换回原来的条目，提问栏应是收起的")
	}
}

// 发送前取目标：正文里还有没落盘的编辑就先落盘再放行（提问读的是库里的正文，不是界面里的草稿）；落不了盘就报「正文尚未保存」，
// 不拿旧正文去提问。
func TestAskResolverFlushesPendingEditsAndRefusesWhenTheyCannotBeSaved(t *testing.T) {
	t.Parallel()
	r := askRig(t)
	r.openCard(detailNoteID)
	target := aiflow.ArticleTarget{ItemID: detailNoteID, View: "body"}
	resolve := r.screen.askResolver(target, false)

	r.editTitle("甲")
	if !r.view().Detail.Unsaved {
		t.Fatal("夹具失效：改了标题应有待保存的改动")
	}
	got, err := resolve()
	if err != nil || got.ItemID != detailNoteID {
		t.Fatalf("改动落得下去时应放行，实际 %+v %v", got, err)
	}
	if r.view().Detail.Unsaved {
		t.Fatal("放行之前应已把待保存的改动落盘")
	}

	r.svc.FailOn("SaveDraft", errors.New("磁盘已满"))
	r.editTitle("乙")
	if _, err := resolve(); !errors.Is(err, errAskUnsaved) {
		t.Fatalf("落盘失败应报「正文尚未保存」，实际 %v", err)
	}
	if !r.view().Detail.Unsaved {
		t.Fatal("落盘失败的改动应还留在待保存里")
	}
}
