package library

import (
	"errors"
	"fmt"
	"sync"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 阅读区的编辑态（渲染层 ReconstructionReader 的 draft / baseline / saving / error 与模块级的 editDrafts，
// 加上 useThemedReadingMode 的 editing）。编辑器本身是岛里的 CodeMirror（viva/reader 的 ViewEditor，台账 C-012），
// 文档由页面持有，每次改动经 edit 事件把全文交回这里；这里只管草稿、基线、保存与失败。

// editDraft 是一份原文的草稿：text 是编辑器里的当前全文，baseline 是开始编辑时那份原文
// （完成时据此判断编辑期间原文有没有被别处改过，渲染层 baseline.current）。
//
// touched 表示编辑器报过至少一次改动：渲染层的 editDrafts 只在 onChange 时才有这一项，没改过就离开的编辑态下次进来是从最新的原文重新开始，
// 而不是恢复一份旧的基线；这里进入编辑态时就建项（页面的改动要有地方记），靠 touched 区分「建了项」与「渲染层里有这一项」。
type editDraft struct {
	text, baseline string
	touched        bool
}

// editResult 是后台「完成编辑」的结果：界面线程在帧首取走（settleSourceEdit）。
// err 非空表示失败（已是可以直接给用户看的原因）；done 为真表示保存成功、该退出编辑态；两者都不是表示落盘没成功但原因已由保存状态报告（静默）。
type editResult struct {
	key  string
	err  string
	done bool
}

// editState 是编辑态的界面状态。草稿按「条目 ID:来源」存（渲染层模块级的 editDrafts：离开编辑态、切换条目或标签都保留，
// 只有「完成编辑」成功才删；窗口关闭即丢），所以挂在 Screen 上，不随条目重置的 panelState 走。
type editState struct {
	drafts map[string]editDraft
	// key 是正在编辑的那一份（草稿键）；空表示不在编辑态（渲染层 themed.editing，随条目或来源切换复位）。
	key string
	// saveKey 是「完成编辑」在途的那一份（渲染层 saving，按来源各自记）；err 是上一次完成失败的原因（红字条），errKey 是它属于哪一份。
	saveKey     string
	err, errKey string

	// results 由保存的 goroutine 追加、界面线程取走。
	mu      sync.Mutex
	results []editResult
}

func (e *editState) post(r editResult) {
	e.mu.Lock()
	e.results = append(e.results, r)
	e.mu.Unlock()
}

func (e *editState) take() []editResult {
	e.mu.Lock()
	out := e.results
	e.results = nil
	e.mu.Unlock()
	return out
}

// leave 结束编辑态（渲染层里 ContentPanel 卸载或来源换了）：草稿保留，红字条随之消失。
func (e *editState) leave() { e.key, e.err, e.errKey = "", "", "" }

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

// sourceKindOfActive 是此刻显示的标签对应的原文来源（渲染层 sourceKind：只有讨论总结标签是 summary，其余都当正文）。
func sourceKindOfActive(active ReadingTab) string {
	if active == TabSummary {
		return sourceSummary
	}
	return sourceBody
}

// editKeyFor 是条目在标签 active 下若进编辑态会用的草稿键与来源；主题阅读不可用（网页快照里、不在正文与总结标签上）时没有。
func (s *Screen) editKeyFor(itemID string, active ReadingTab) (key, kind string, ok bool) {
	if !s.detail.panel.themedAvailable(active) {
		return "", "", false
	}
	kind = sourceKindOfActive(active)
	return draftKey(itemID, kind), kind, true
}

// editing 报告阅读区此刻是不是在编辑这一条这个标签的原文（渲染层 themed.editing）。
func (s *Screen) editing(item services.LibraryItem, active ReadingTab) bool {
	key, _, ok := s.editKeyFor(item.ID, active)
	return ok && key == s.detail.edit.key
}

// editorView 是编辑态下阅读区该显示的视图：岛里的编辑器，文档是草稿的当前全文。
func (s *Screen) editorView(item services.LibraryItem, active ReadingTab) (reader.View, bool) {
	key, kind, ok := s.editKeyFor(item.ID, active)
	e := &s.detail.edit
	if !ok || key != e.key {
		return reader.View{}, false
	}
	d, found := e.drafts[key]
	if !found {
		return reader.View{}, false
	}
	return reader.View{Kind: reader.ViewEditor, Content: d.text, SourceKind: kind}, true
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
		d.text, d.touched = text, true
		e.drafts[key] = d
	}
}

// utf16Exceeds 报告字符串的 UTF-16 码元数（渲染层字符串长度的口径）是否超过 limit；超过时 n 是真实的码元数（给提示用），否则 n 为 0。
// 每个字节至多对应一个码元，所以字节数不超过 limit 时不必逐个数。
func utf16Exceeds(s string, limit int) (n int, exceeds bool) {
	if len(s) <= limit {
		return 0, false
	}
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n, n > limit
}

// startEdit 进编辑态（渲染层读者工具条「编辑」钮的 onClick，以及空白笔记打开时的自动进入）：
// 基线与草稿取这份来源上次没提交的草稿，没有就取此刻的原文；红字条清掉，查找位置归零，此后不再恢复阅读位置。
// 文档超过岛里编辑器能处理的长度时拒绝进入并给出原因（台账 B-070）。
func (s *Screen) startEdit(item services.LibraryItem, active ReadingTab) {
	key, kind, ok := s.editKeyFor(item.ID, active)
	if !ok {
		return
	}
	original, ok := readingSourceText(item, kind)
	if !ok {
		return
	}
	if n, over := utf16Exceeds(original, reader.MaxEditChars); over {
		name := locales.ThemedReadingReaderEditOriginal()
		if kind == sourceSummary {
			name = locales.ThemedReadingReaderEditSummary()
		}
		s.Guard.Report(name, fmt.Errorf("文档有 %d 个字符，超过编辑器能处理的 %d 个字符", n, reader.MaxEditChars))
		return
	}
	e := &s.detail.edit
	if e.drafts == nil {
		e.drafts = map[string]editDraft{}
	}
	if d, found := e.drafts[key]; !found || !d.touched {
		e.drafts[key] = editDraft{text: original, baseline: original}
	}
	e.key = key
	e.err, e.errKey = "", ""
	p := &s.detail.panel
	p.restoreTop = nil
	p.findActive, p.findCount = 0, 0
}

// finishEdit 是「完成编辑」：后台把草稿写回条目并落盘，成功才退出编辑态（渲染层 finish）。
func (s *Screen) finishEdit(item services.LibraryItem, active ReadingTab) {
	key, kind, ok := s.editKeyFor(item.ID, active)
	e := &s.detail.edit
	if !ok || e.saveKey == key {
		return
	}
	d, found := e.drafts[key]
	if !found {
		return
	}
	e.saveKey = key
	e.err, e.errKey = "", ""
	itemID := item.ID
	s.Store.Spawn(func() {
		e.post(s.saveEdit(itemID, kind, key, d))
		s.Store.wake()
	})
}

// saveEdit 把草稿写回条目并落盘（后台）。失败时弹错误提示（原因在详情里）并记日志，同时把原因交回界面线程摆红字条。
// 检查与渲染层一致：先看选中的是不是还是这一条，再看原文在编辑期间有没有被改过（与基线和草稿都不同才算冲突），
// 落盘失败由头部保存状态说明、这里不再重复，落盘「成功」之后仍有未保存的改动要提示重试。
func (s *Screen) saveEdit(itemID, kind, key string, d editDraft) editResult {
	res := editResult{key: key}
	fail := func(err error) editResult {
		res.err = err.Error()
		if n := s.Guard.Notify; n != nil {
			n.Error(locales.ThemedReadingReaderEditFailed(), res.err)
		}
		s.Guard.Background("library.editSource", err)
		return res
	}
	latest := s.Store.View().Detail.Item
	if latest == nil || latest.ID != itemID {
		return fail(errors.New(locales.ThemedReadingReaderEditSwitched()))
	}
	shape := shapeOf(latest.ItemType)
	current, err := notes.ReadingSourceText(latest.Content, shape, kind)
	if err != nil {
		return fail(err)
	}
	if current != d.baseline && current != d.text {
		return fail(errors.New(locales.ThemedReadingReaderEditConflict()))
	}
	content, err := notes.ReplaceReadingSource(latest.Content, shape, kind, d.text)
	if err != nil {
		return fail(err)
	}
	s.Store.UpdateSelected(s.Ctx, services.LibraryDraftFields{Content: &content})
	if !s.Store.FlushPendingSave(s.Ctx) {
		return res
	}
	if s.Store.View().Detail.Unsaved {
		return fail(errors.New(locales.ThemedReadingReaderEditUnsaved()))
	}
	res.done = true
	return res
}

// settleSourceEdit 在帧首取走后台保存的结果：成功删掉草稿、退出编辑态；失败摆红字条（还在编辑那一份时）；
// 在途标记随结果清掉，所以保存期间「完成编辑」钮一直是禁用的。shown 是此刻显示的那一份的草稿键（没有为空）：
// 结果属于别的条目或标签时只处理草稿与在途标记，不碰当前面板的查找与阅读位置。
func (s *Screen) settleSourceEdit(shown string) {
	e := &s.detail.edit
	for _, r := range e.take() {
		if e.saveKey == r.key {
			e.saveKey = ""
		}
		switch {
		case r.done:
			delete(e.drafts, r.key)
			if e.errKey == r.key {
				e.err, e.errKey = "", ""
			}
			if e.key == r.key {
				e.key = ""
			}
			if r.key == shown {
				p := &s.detail.panel
				p.findActive, p.findCount = 0, 0
				p.restoreTop = nil
			}
		case r.err != "":
			if e.key == r.key {
				e.err, e.errKey = r.err, r.key
			}
		}
	}
}

// syncEdit 在帧首把编辑态与此刻显示的条目、标签对齐：先看草稿键还对不对得上——换了条目、换了标签或来源、
// 主题阅读不可用，编辑态就复位（草稿保留），红字条也一并消失——再取走保存结果。
func (s *Screen) syncEdit(item services.LibraryItem, active ReadingTab) {
	e := &s.detail.edit
	shown, _, ok := s.editKeyFor(item.ID, active)
	if !ok {
		shown = ""
	}
	if e.key != "" && e.key != shown {
		e.key = ""
	}
	if e.errKey != "" && e.errKey != shown {
		e.err, e.errKey = "", ""
	}
	s.settleSourceEdit(shown)
}

// editError 是此刻该摆的红字条（只在编辑那一份时有）。
func (s *Screen) editError(item services.LibraryItem, active ReadingTab) string {
	key, _, ok := s.editKeyFor(item.ID, active)
	e := &s.detail.edit
	if !ok || e.err == "" || e.errKey != key || e.key != key {
		return ""
	}
	return e.err
}

// editBanner 是完成编辑失败的红字条（渲染层 p[role=alert].shrink-0.select-text.px-4.py-2.text-sm.text-destructive）。
// 文字不可选中（Viva 没有文字标签的选择入口，台账 A-019）；完整原因另在错误提示的「查看详情」里，可以复制。
func (s *Screen) editBanner(c ui.C, message string) {
	l := s.Look
	ui.Row(c.Key("edit-error")).FillWidth().Shrink(0).PadSides(l.Space(2), l.Space(4), l.Space(2), l.Space(4)).Draw(func(d *ui.DrawContext) {
		d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
	}).Children(func() {
		l.Text(c.Key("edit-error-text"), message, kit.TextSM, kit.WeightNormal, l.T.Destructive, kit.Wrapped()).Shrink(1).MinWidth(0)
	})
}

// focusEditor 把键盘焦点交给阅读区里的编辑器（渲染层标题框 Enter：把光标交给 .cm-content）；
// 不在编辑态、编辑器还没就绪或没接宿主时什么也不做。
func (s *Screen) focusEditor() {
	if s.readerHost != nil {
		s.readerHost.FocusEditor()
	}
}
