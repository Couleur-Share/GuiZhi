package library

import (
	"sync/atomic"

	"guizhi-mygo-shell/viva/reader"
)

// 阅读区的编辑态（渲染层 ReconstructionReader 的 draft / baseline / saving / error 与模块级的 editDrafts，
// 加上 useThemedReadingMode 的 editing）。编辑器本身是岛里的 CodeMirror（viva/reader 的 ViewEditor，台账 C-012），
// 文档由页面持有，每次改动经 edit 事件把全文交回这里；这里只管草稿、基线、保存与失败。

// editDraft 是一份原文的草稿：text 是编辑器里的当前全文，baseline 是开始编辑时那份原文
// （完成时据此判断编辑期间原文有没有被别处改过，渲染层 baseline.current）。
type editDraft struct{ text, baseline string }

// editResult 是后台保存的结果：界面线程在帧首取走（settleEdit）。
type editResult struct {
	key string
	// err 非空表示失败（已是可以直接给用户看的原因）；done 为真表示保存成功、该退出编辑态。
	err  string
	done bool
}

// editState 是编辑态的界面状态。草稿按「条目 ID:来源」存（渲染层模块级的 editDrafts：离开编辑态、切换条目或标签都保留，
// 只有「完成编辑」成功才删；窗口关闭即丢），所以挂在 Screen 上，不随条目重置的 panelState 走。
type editState struct {
	drafts map[string]editDraft
	// key 是正在编辑的那一份（草稿键）；空表示不在编辑态（渲染层 themed.editing，随条目或来源切换复位）。
	key string
	// saving 是「完成编辑」在途；err 是上一次完成失败的原因（渲染层 ReconstructionReader 的 error，红字条）。
	saving bool
	err    string
	// result 由保存的 goroutine 写、界面线程取走。
	result atomic.Pointer[editResult]
}

// draftKey 是草稿的键（渲染层 editKey）。
func draftKey(itemID, sourceKind string) string { return itemID + ":" + sourceKind }

// sourceKindOfTab 把页面上报的标签换成原文的来源：编辑态只出现在正文与讨论总结两个标签上。
func sourceKindOfTab(tab reader.Tab) (string, bool) {
	switch tab {
	case reader.TabBody:
		return sourceBody, true
	case reader.TabSummary:
		return sourceSummary, true
	}
	return "", false
}

// recordDraft 记编辑器里文档的当前全文（渲染层 MarkdownEditor 的 onChange：setDraft 并写进 editDrafts）。
// 没有这份草稿时忽略：「完成编辑」成功删掉草稿之后，迟到的改动不能把它复活。
func (s *Screen) recordDraft(itemID string, tab reader.Tab, text string) {
	kind, ok := sourceKindOfTab(tab)
	if !ok {
		return
	}
	e := &s.detail.edit
	key := draftKey(itemID, kind)
	if d, found := e.drafts[key]; found {
		d.text = text
		e.drafts[key] = d
	}
}
