package ask

import (
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// focusInput 点一下输入框让它取得焦点（与真实使用同一条路径：焦点来自点击，不是直接改状态）。
func (r *uiRig) focusInput() {
	r.t.Helper()
	r.click(gfx.RoleTextField, locales.AskPlaceholder())
	if !r.screen.draftFocus {
		r.t.Fatal("点击后输入框应取得焦点")
	}
}

func TestUITypingAndEnterSendsTheQuestion(t *testing.T) {
	r := newUIRig(t)
	r.focusInput()
	r.typeText("hello world")
	if got := r.screen.draft.Text(); got != "hello world" {
		t.Fatalf("输入应进入草稿，得到 %q", got)
	}
	r.keys("enter")
	calls := r.svc.AskCalls()
	if len(calls) != 1 || calls[0].Question != "hello world" {
		t.Fatalf("Enter 应发送问题：%+v", calls)
	}
	if got := r.screen.draft.Text(); got != "" {
		t.Fatalf("发送后输入框应清空，得到 %q", got)
	}
	if !r.contains("echo:hello world") {
		t.Fatalf("应显示回答：%q", r.session.Texts())
	}
}

func TestUIShiftEnterInsertsANewlineInsteadOfSending(t *testing.T) {
	r := newUIRig(t)
	r.focusInput()
	r.typeText("first")
	r.keysWithShift("enter")
	r.typeText("second")
	if got := r.screen.draft.Text(); got != "first\nsecond" {
		t.Fatalf("Shift+Enter 应换行，得到 %q", got)
	}
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatalf("Shift+Enter 不该发送，实际发出 %d 次", n)
	}
	if r.screen.inputRows != 2 {
		t.Fatalf("输入框应长到两行，得到 %d", r.screen.inputRows)
	}
}

func TestUIEnterOnEmptyInputDoesNothing(t *testing.T) {
	r := newUIRig(t)
	r.focusInput()
	r.keys("enter")
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatal("空输入按 Enter 不该发送")
	}
	if got := r.screen.draft.Text(); got != "" {
		t.Fatalf("空输入按 Enter 也不该插入换行，得到 %q", got)
	}
	r.screen.setDraft("  \n ")
	r.frames(2)
	r.keys("enter")
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatal("只有空白的输入不该发送")
	}
}

func TestUISendButtonIsDisabledForBlankInput(t *testing.T) {
	r := newUIRig(t)
	r.clickButton(locales.AskSend())
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatal("空输入时发送钮应禁用")
	}
	r.screen.setDraft("   ")
	r.frames(2)
	r.clickButton(locales.AskSend())
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatal("空白输入时发送钮应禁用")
	}
}

func TestUIEnterWhileAnsweringKeepsTheDraft(t *testing.T) {
	gate := make(chan struct{})
	r := newUIRig(t, goroutines, func(r *uiRig) {
		r.svc.QueueAsk(servicestest.AskScript{Gate: gate, Answer: services.AskAnswer{Text: "done"}})
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	r.settle("首次加载结束", func() bool { return r.store.View().HasLoaded })
	r.ask("first")
	r.settle("进入回答中", func() bool { return r.store.View().IsRunning })

	r.focusInput()
	r.typeText("second")
	r.keys("enter")
	if n := len(r.svc.AskCalls()); n != 1 {
		t.Fatalf("回答中按 Enter 不该再发一轮，实际 %d 次", n)
	}
	if got := r.screen.draft.Text(); got != "second" {
		t.Fatalf("回答中按 Enter 不该吞掉也不该换行，得到 %q", got)
	}
	close(gate)
	r.settle("回答结束", func() bool { return !r.store.View().IsRunning })
}

func TestUIInputGrowsWithWrappedLinesAndCaps(t *testing.T) {
	r := newUIRig(t)
	r.screen.setDraft(strings.Repeat("a\n", 12) + "z")
	r.frames(3)
	if r.screen.inputRows != 13 {
		t.Fatalf("应数到 13 行，得到 %d", r.screen.inputRows)
	}
	// 一长行也要折：宽度量到之后行数大于 1。
	r.screen.setDraft(strings.Repeat("很长的一行文字", 60))
	r.frames(3)
	r.frames(3)
	if r.screen.inputRows < 2 {
		t.Fatalf("长行应折成多行，得到 %d", r.screen.inputRows)
	}
}

func TestUISetDraftPrefillsFocusesAndDoesNotSend(t *testing.T) {
	r := newUIRig(t, withSessions("s1"), withActive("s1"))
	r.screen.SetDraft("请围绕这条内容提问")
	r.frames(3)
	if got := r.screen.draft.Text(); got != "请围绕这条内容提问" {
		t.Fatalf("草稿应预填，得到 %q", got)
	}
	if !r.screen.draftFocus {
		t.Fatal("预填后输入框应取得焦点")
	}
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatal("预填不能自动发送")
	}
	if r.screen.hasPending {
		t.Fatal("草稿应只被取走一次")
	}
}

// longSession 造一个有 n 轮的会话，每轮回答都很长，让消息列表超出视口。
func longSession(r *uiRig, n int) {
	msgs := make([]services.AskMessage, 0, n)
	for i := 0; i < n; i++ {
		id := strconv.Itoa(i)
		msgs = append(msgs, done("m"+id, "question "+id, strings.Repeat("line of the answer\n\n", 6)))
	}
	putSession(r.t, r.svc, "long", msgs...)
}

func TestUIMessageListStartsAtTheEndAndOffersJumpingBack(t *testing.T) {
	r := newUIRig(t, longSession30, withActive("long"))
	r.frames(3)
	if r.screen.awayFromEnd || r.hasButton(locales.AskScrollToLatest()) {
		t.Fatal("打开会话时应落在最新一轮，不该出现「回到最新」")
	}
	r.scrollBy(800, 300, -100000)
	if !r.screen.awayFromEnd || !r.hasButton(locales.AskScrollToLatest()) {
		t.Fatalf("滚到顶之后应出现「回到最新」：away=%v", r.screen.awayFromEnd)
	}
	r.clickButton(locales.AskScrollToLatest())
	if r.screen.awayFromEnd || r.hasButton(locales.AskScrollToLatest()) {
		t.Fatal("点「回到最新」后应回到底部")
	}
}

func TestUISendingJumpsToTheLatestTurn(t *testing.T) {
	r := newUIRig(t, longSession30, withActive("long"))
	r.scrollBy(800, 300, -100000)
	if !r.screen.awayFromEnd {
		t.Fatal("前提：已滚离底部")
	}
	r.ask("another one")
	r.frames(3)
	if r.screen.awayFromEnd {
		t.Fatal("发出新问题后应滚到最新")
	}
}

func TestUISwitchingSessionScrollsToTheEnd(t *testing.T) {
	r := newUIRig(t, longSession30, withSessions("s1"), withActive("s1"))
	r.clickRow("question 0") // 长会话的标题取自第一问
	r.frames(3)
	if got := r.store.View().ActiveSessionID; got != "long" {
		t.Fatalf("应已切到长会话，得到 %q", got)
	}
	if r.screen.awayFromEnd || r.hasButton(locales.AskScrollToLatest()) {
		t.Fatal("切到另一个会话时应落在它的最新一轮")
	}
}

func longSession30(r *uiRig) { longSession(r, 30) }

func TestWithoutInsertedNewline(t *testing.T) {
	cases := []struct {
		prev, cur, want string
		ok              bool
	}{
		{"", "\n", "", true},
		{"ab", "ab\n", "ab", true},
		{"ab", "abx\n", "abx", true}, // 同一帧里先敲字再按 Enter
		{"ab", "a\nb", "ab", true},   // 在中间按 Enter：发送整段
		{"a\n", "a\n\n", "a\n", true},
		{"ab", "abc", "", false},
		{"ab", "ab\n\n", "", false}, // 一次多出两个换行不是 Enter
		{"ab", "xab\n\n", "", false},
		{"ab\n", "ab", "", false},
	}
	for _, c := range cases {
		got, ok := withoutInsertedNewline(c.prev, c.cur)
		if ok != c.ok || got != c.want {
			t.Errorf("withoutInsertedNewline(%q, %q) = (%q, %v)，期望 (%q, %v)", c.prev, c.cur, got, ok, c.want, c.ok)
		}
	}
}

func TestUIProgrammaticTrailingNewlineIsNotTakenAsEnter(t *testing.T) {
	r := newUIRig(t)
	r.focusInput()
	r.screen.SetDraft("a topic\n")
	r.frames(3)
	if n := len(r.svc.AskCalls()); n != 0 {
		t.Fatal("外部预填的草稿以换行结尾也不能被当成 Enter 发送")
	}
	if got := r.screen.draft.Text(); got != "a topic\n" {
		t.Fatalf("预填内容应原样保留，得到 %q", got)
	}
}
