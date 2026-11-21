package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// 内容面板（渲染层 components/library/ContentPanel）：标签页与工具栏、评审提示、查找条，下面是阅读区。
//
// 阅读区（Markdown 预览、楼层讨论、网页快照、图库、主题阅读、编辑器）由岛里的页面渲染（detailreader.go），文字稿是原生面板
// （detailtranscript.go，台账 B-067）；工具栏上的媒体动作（总结、刷新讨论、AI 排版、转写、区分说话人）接到 Store（detailtoolbar.go，M2-6d）。

// panelState 是内容面板的界面状态（渲染层 ContentPanel 的 useState；随条目切换重置，对应渲染层的 viewItemId 判断）。
type panelState struct {
	itemID       string
	tab          ReadingTab
	isPreview    bool
	snapshotView bool

	findOpen bool
	// canFind 是这一帧工具栏摆不摆查找入口（窗口键盘钩子的 Ctrl+F 据此判断），findFocused 是查找框此刻有没有键盘焦点（钩子的 Enter / Esc 据此判断），panelW 是上一帧面板宽。
	canFind, findFocused bool
	panelW               float32
	// panelRect 是上一帧面板的位置与大小（窗口坐标）：阅读岛盖住同层的东西，查找条抬到更高的层时要按它定位（detailreader.go）。
	panelRect gfx.Rect
	// live 是这一帧阅读区有没有岛在场（readerLive）：工具侧栏盖在阅读区上，岛在场时也要抬到更高的一层（detailtools.go）。
	live       bool
	find       ui.Field
	findQuery  string
	findActive int
	findCount  int
	// focusFind 为真时下一帧把焦点交给查找框（渲染层打开查找条后 requestAnimationFrame focus）。
	focusFind bool

	catalogOpen bool

	// tocOpen 是主题阅读「目录」浮层的开合，tocKey 是它当前属于哪份（条目 + 来源）；restoreTop 是这条刚打开时要恢复的阅读位置，
	// 只对打开时的那个标签（restoreTab），换标签即作废（渲染层 changeTab 之后 restoredRef 恒为真，只在切换条目时恢复）；
	// viewCache 是阅读区内容的切分缓存（detailreader.go）。
	tocOpen    bool
	tocKey     string
	restoreTop *float64
	restoreTab ReadingTab
	viewCache  readerViewCache

	// persisted 是已经写进阅读记忆的「条目 ID + 标签」，repliesPersisted 是讨论标签已写的搜索词与目录开关
	// （渲染层两个 useEffect 的依赖：变了才写）。
	persisted        string
	repliesPersisted string

	// tx 是文字稿面板的界面状态（滚动记忆、查找滚动；detailtranscript.go），longFormat 是长稿排版确认框的焦点状态（detailtoolbar.go）。
	tx         transcriptState
	longFormat kit.ConfirmState
}

// activeTab 是此刻实际显示的标签：记忆里的标签这条没有时回正文（渲染层 activeTab）。
func (p *panelState) activeTab(caps ItemCaps) ReadingTab {
	if p.tabAvailable(p.tab, caps) {
		return p.tab
	}
	return TabBody
}

// tabAvailable 报告标签 tab 在这条上有没有（渲染层 availableTabs）。
func (p *panelState) tabAvailable(tab ReadingTab, caps ItemCaps) bool {
	switch tab {
	case TabBody:
		return true
	case TabTranscript:
		return caps.ShowTranscriptTab
	case TabImages:
		return caps.ShowImagesTab
	case TabRecognized:
		return caps.ShowRecognizedTab
	case TabSummary:
		return caps.ShowForumSummaryTab
	case TabReplies:
		return caps.ShowRepliesTab
	}
	return false
}

// findable 是能用查找条的标签（渲染层 FINDABLE_TABS）。
func findable(tab ReadingTab) bool {
	switch tab {
	case TabSummary, TabBody, TabTranscript, TabReplies:
		return true
	}
	return false
}

// themedAvailable 是渲染层 useThemedReadingMode 的 available：不在网页快照里、且停在正文或讨论总结标签。
// 主题阅读本身（AI 阅读版式、读者工具条里的原文 / AI 阅读 / 目录 / 编辑）随 M2-5；这里只认它「可用」这件事：
// 它决定工具栏摆不摆读者工具条的挂载点、查找入口，以及要不要摆「显示原文 / Markdown 渲染」（台账 B-059）。
func (p *panelState) themedAvailable(active ReadingTab) bool {
	return !p.snapshotView && (active == TabBody || active == TabSummary)
}

// showFindBar 报告工具栏上摆不摆查找入口（渲染层 showFindBar）：预览态（回收站里的条目只读、不算）或主题阅读可用、可查找的标签，
// 文字稿要有内容、讨论与总结要有对应小节。主题阅读与公众号快照里的查找本身在 M2-5 随阅读区一起做。
func (p *panelState) showFindBar(active ReadingTab, caps ItemCaps) bool {
	wechatPreview := caps.IsWechat && p.snapshotView // 渲染层 isWechatPreview：快照里也能查找
	if !((p.isPreview && !caps.Trashed) || p.themedAvailable(active) || wechatPreview) || !findable(active) {
		return false
	}
	switch active {
	case TabTranscript:
		return caps.Transcript != ""
	case TabReplies:
		return caps.ShowRepliesTab
	case TabSummary:
		return caps.ShowForumSummaryTab
	}
	return true
}

// syncPanel 在条目切换时把面板状态换成这条的（渲染层 `viewItemId !== item.id` 那一段）：先按偏好决定要不要预览，
// 再用阅读记忆恢复标签、讨论搜索词与目录开关；没有记忆时论坛条目有讨论总结就先看总结。
func (s *Screen) syncPanel(item services.LibraryItem, caps ItemCaps) {
	p := &s.detail.panel
	if p.itemID == item.ID {
		return
	}
	s.flushTranscriptScroll(true) // 离开上一条：文字稿里还没写进记忆的位置先写掉（渲染层 effect 的清理函数 flush）
	if p.tx.stop != nil {
		p.tx.stop()
	}
	*p = panelState{itemID: item.ID}
	p.isPreview = s.editorPreview() && jsTrim(item.Content) != ""

	replyCount := 0
	if caps.ShowRepliesTab {
		replyCount = len(notes.ParseForumReplies(item.Content))
	}
	mem, remembered := LoadReadingMemory(s.Prefs, item.ID, s.nowTime())
	if remembered && p.restorable(mem.Tab, caps, item) {
		p.tab = mem.Tab
		if mem.Tab == TabReplies && mem.RepliesQuery != nil {
			p.findQuery = *mem.RepliesQuery
			p.find.SetText(p.findQuery)
		}
		p.catalogOpen = defaultCatalogOpen(replyCount)
		if mem.CatalogOpen != nil {
			p.catalogOpen = *mem.CatalogOpen
		}
	} else {
		p.tab = TabBody
		if item.ItemType == services.LibraryTypeForum && caps.ShowForumSummaryTab {
			p.tab = TabSummary
		}
		p.catalogOpen = defaultCatalogOpen(replyCount)
	}
	// 阅读位置：对实际显示的标签恢复（渲染层 useLayoutEffect 用 memory.scrollTopByTab[activeTab]，记忆里的标签这条不能用时也照样取回正文的位置）。
	active := p.activeTab(caps)
	if remembered {
		if top, ok := mem.ScrollTopByTab[active]; ok {
			p.restoreTop, p.restoreTab = &top, active
		}
	}
	// 空白笔记没有可阅读的内容：打开时直接进编辑态（渲染层 startBlankNoteEditing，只在切到该条目时判断一次，输入第一个字后不会被切回阅读态）。
	if item.ItemType == services.LibraryTypeNote && !caps.Trashed && jsTrim(item.Content) == "" {
		s.startEdit(item, active)
	}
}

// restorable 报告记忆里的标签这条还能不能用（渲染层那一长串条件：转写标签要看转写入口，图片的两个标签要看是图片条目）。
func (p *panelState) restorable(tab ReadingTab, caps ItemCaps, item services.LibraryItem) bool {
	switch tab {
	case TabBody:
		return true
	case TabSummary:
		return caps.ShowForumSummaryTab
	case TabReplies:
		return caps.ShowRepliesTab
	case TabTranscript:
		return caps.ShowTranscriptTab
	case TabImages, TabRecognized:
		return item.ItemType == services.LibraryTypeImage
	}
	return false
}

// persistPanel 把标签与讨论状态写进阅读记忆（渲染层的两个 useEffect：标签变了写标签；讨论标签下搜索词或目录开关变了写它们）。
func (s *Screen) persistPanel(item services.LibraryItem, active ReadingTab) {
	p := &s.detail.panel
	if key := item.ID + "\x00" + string(active); key != p.persisted {
		p.persisted = key
		s.patchReading(item.ID, ReadingPatch{Tab: active})
	}
	if active == TabReplies {
		open := p.catalogOpen
		query := p.findQuery
		key := item.ID + "\x00" + query + "\x00" + boolKey(open)
		if key != p.repliesPersisted {
			p.repliesPersisted = key
			s.patchReading(item.ID, ReadingPatch{RepliesQuery: &query, CatalogOpen: &open})
		}
	}
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// patchReading 写阅读记忆；失败只记日志（位置丢了不影响阅读）。
func (s *Screen) patchReading(itemID string, patch ReadingPatch) {
	if err := PatchReadingMemory(s.Prefs, itemID, patch, s.nowTime()); err != nil {
		s.Guard.Background("library.readingMemory", err)
	}
}

// changeTab 切换标签（渲染层 changeTab）：先把当前标签的滚动位置记进记忆，再换标签、查找位置归零，并且此后不再恢复阅读位置
// （restoredRef 置真）。当前标签的位置由页面持续上报、宿主在换内容时补报一次，所以这里不必另存。
func (s *Screen) changeTab(item services.LibraryItem, caps ItemCaps, next ReadingTab) {
	p := &s.detail.panel
	p.tab = next
	p.findActive = 0
	p.restoreTop = nil
	_ = item
	_ = caps
}
