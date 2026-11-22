package app

import (
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 会话保存之后历史列表要跟上（渲染层 "article-ask-saved" 事件；root_ask.go 里 store.OnSaved 的接线）：
// 开一个新会话并提一个新问题，会话被保存，侧栏的历史列表（HistoryStore，不是 Store 自己的快照）随之读到它。
func TestRootAskSavedSessionRefreshesTheHistoryList(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", func() bool { return f.session.HasText("问题一") })
	history := f.root.ask.History
	f.waitFrames(t, "历史列表读到已有的会话", func() bool { return len(history.View().Entries) == 1 })

	f.clickButton(t, locales.AskNewSession())
	f.waitFrames(t, "新建了空会话", func() bool { return f.root.askStore.View().ActiveSessionID != "s1" })
	go f.root.askStore.Ask("新的问题")
	f.waitFrames(t, "新会话保存后历史列表刷新", func() bool {
		entries := history.View().Entries
		return len(entries) == 2 && hasSessionTitled(entries, "新的问题")
	})
}

func hasSessionTitled(entries []services.AskSessionMeta, title string) bool {
	for _, e := range entries {
		if e.Title == title {
			return true
		}
	}
	return false
}
