package library

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 阅读区（渲染层 ContentPanel 里 min-h-0 flex-1 的 data-article-reader）：阅读内容由岛里的页面渲染（viva/reader：Markdown 预览、
// 楼层讨论、图片画廊，复用渲染层自己的阅读组件，见 apps/desktop/src/renderer/reader/）。
//
// 本文件是界面一侧：按渲染层 ContentPanel 各分支的先后决定岛里显示什么（readerViewFor），把界面状态（查找、目录开合、阅读位置、外观）
// 整理成一帧的期望（readerRequest）交给宿主，并接收页面的事件（readerEvents）。界面状态仍然归 panelState，岛只是显示它：
// 页面里的改动（目录开合、查找清除）经事件回到这里，由这里改状态；下一帧宿主把「界面状态」与「页面状态」的差异发过去，
// 所以界面不接受的改动会被拉回。
//
// 没有接宿主（Screen.readerHost 为空：对照与单测）时阅读区留白，版面不变。台账 B-059 的余项见各分支。

// readerViewFor 决定阅读区此刻显示什么：渲染层 ContentPanel 里 data-article-reader 内的分支，先后一致。
//
//	公众号网页快照（isWechatPreview）→ 快照面板（B-066，未做，留白）
//	主题阅读可用（正文 / 讨论总结，不在快照里）→ ReconstructionReader 的「原文」
//	文字稿标签 → 文字稿面板（B-067，未做，留白）
//	配图标签 → 图片画廊
//	论坛条目 → 讨论标签的楼层讨论（正文与总结在主题阅读里）
//	图片条目 → 「图中文字」标签的 Markdown
//
// 其余（回收站与预览态的 Markdown 预览、编辑器）在渲染层里只有主题阅读不可用、又不属于上面各标签时才会走到，
// 而正文与总结之外的标签都已被上面接住，所以不会出现；编辑器本身归 M2-6（B-060）。
func readerViewFor(item services.LibraryItem, caps ItemCaps, snapshotView bool, active ReadingTab) reader.View {
	none := reader.View{Kind: reader.ViewNone}
	switch {
	case caps.IsWechat && snapshotView:
		return none
	case !snapshotView && (active == TabBody || active == TabSummary):
		kind := sourceBody
		if active == TabSummary {
			kind = sourceSummary
		}
		text, ok := readingSourceText(item, kind)
		if !ok {
			return none
		}
		return reader.View{Kind: reader.ViewOriginal, Content: text, SourceKind: kind}
	case active == TabTranscript:
		return none
	case active == TabImages:
		return reader.View{Kind: reader.ViewGallery, Content: item.Content}
	case item.ItemType == services.LibraryTypeForum:
		if active == TabReplies {
			return reader.View{Kind: reader.ViewReplies, Content: notes.SplitForumSections(item.Content).Replies}
		}
	case item.ItemType == services.LibraryTypeImage:
		if active == TabRecognized {
			return reader.View{Kind: reader.ViewMarkdown, Content: notes.SplitImageSections(item.Content).Recognized, Tab: reader.TabRecognized}
		}
	}
	return none
}

// readerViewKey 是 readerViewFor 的输入：同一份输入重算只会得到同样的视图，所以每帧只在输入变了才重新切分正文。
type readerViewKey struct {
	content      string
	itemType     string
	wechat       bool
	snapshotView bool
	active       ReadingTab
}

// readerViewCache 缓存最近一次的视图：正文可以很大，每帧重切一遍（甚至重复比较）不划算。
// 字符串比较在两边是同一块内存时是 O(1)（Go 先比指针），正文没变时这里不会逐字节比。
type readerViewCache struct {
	key  readerViewKey
	view reader.View
	ok   bool
}

func (s *Screen) readerViewOf(item services.LibraryItem, caps ItemCaps, active ReadingTab) reader.View {
	p := &s.detail.panel
	key := readerViewKey{content: item.Content, itemType: item.ItemType, wechat: caps.IsWechat, snapshotView: p.snapshotView, active: active}
	if !p.viewCache.ok || p.viewCache.key != key {
		p.viewCache = readerViewCache{key: key, view: readerViewFor(item, caps, p.snapshotView, active), ok: true}
	}
	return p.viewCache.view
}

// readerTheme 是页面的外观输入：与 <html> 上的主题类、色相、饱和度、根字号、动效档位一一对应（页面用同一份样式表算令牌）。
func readerTheme(l *kit.Look, viewportWidth float32) reader.Theme {
	motion := reader.MotionOff
	switch l.Spec.Motion {
	case kit.MotionLevelStandard:
		motion = reader.MotionStandard
	case kit.MotionLevelReduced:
		motion = reader.MotionReduced
	}
	return reader.Theme{Dark: l.Dark, Hue: l.Spec.Hue, Saturation: l.Spec.Saturation, BasePx: float64(l.RootPx), Motion: motion, ViewportWidth: float64(viewportWidth)}
}

// readerLang 是页面的界面语言（设置里的「语言」）。
func readerLang() reader.Lang {
	if locales.CurrentLang() == locales.En {
		return reader.LangEn
	}
	return reader.LangZh
}

// readerFind 是页面此刻该有的查找状态。楼层讨论的过滤词一直生效（关了查找条也不撤，渲染层 query={findQuery}）；
// 原文与 Markdown 的高亮只在查找条开着时有（渲染层 snapshotFindQuery = isFindOpen ? findQuery : ""）；画廊不查找。
func (p *panelState) readerFind(kind reader.ViewKind) (query string, index int) {
	switch kind {
	case reader.ViewReplies:
		return p.findQuery, p.findActive
	case reader.ViewOriginal, reader.ViewMarkdown:
		if p.findOpen {
			return p.findQuery, p.findActive
		}
	}
	return "", 0
}

// readerRequest 把界面状态整理成这一帧阅读区的期望。
func (s *Screen) readerRequest(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab) reader.Request {
	p := &s.detail.panel
	view := s.readerViewOf(item, caps, active)
	s.detail.viewportW = s.Look.ViewportWidth(c)
	req := reader.Request{
		ItemID: item.ID, Tab: reader.Tab(active), View: view, CatalogOpen: p.catalogOpen,
		Theme: readerTheme(s.Look, s.detail.viewportW), Lang: readerLang(),
	}
	if view.Kind == reader.ViewNone {
		return req
	}
	if view.Kind == reader.ViewOriginal {
		s.syncToc(item.ID, view.SourceKind)
		req.TocOpen = p.tocOpen
	}
	req.FindQuery, req.FindIndex = p.readerFind(view.Kind)
	if p.restoreTop != nil && p.restoreTab == active {
		top := *p.restoreTop
		req.RestoreTop = &top
	}
	return req
}

// readerLive 报告这一帧阅读区会不会有岛（决定悬浮控件要不要抬到更高的层）：接了宿主、且有内容可显示。
func (s *Screen) readerLive(item services.LibraryItem, caps ItemCaps, active ReadingTab) bool {
	return s.readerHost != nil && s.readerViewOf(item, caps, active).Kind != reader.ViewNone
}

// SetReader 接上阅读区的岛宿主（nil 表示没有：阅读区留白，对照与单测）。
func (s *Screen) SetReader(host *reader.Host) {
	s.readerHost = host
	if host != nil {
		host.SetHandler(readerEvents{s})
		host.Viewer().SetHandler(viewerEvents{s})
	}
}

// readerArea 是阅读区的那一格（min-h-0 flex-1）：有岛就把原生视图放在它的矩形上；岛不可用时画「读取失败」与重试。
func (s *Screen) readerArea(c ui.C, item services.LibraryItem, caps ItemCaps, active ReadingTab) {
	col := ui.Column(c.Key("reader")).FillWidth().Grow(1).MinHeight(0)
	host := s.readerHost
	if host == nil {
		return
	}
	req := s.readerRequest(c, item, caps, active)
	failure := host.Failure(req)
	col.Draw(func(d *ui.DrawContext) {
		if failure == nil {
			host.Frame(d.Canvas, d.Rect, req)
		}
	}).Children(func() {
		if failure != nil && s.Look.LoadErrorState(c.Key("reader-error"), failure.Error()) {
			host.Retry()
		}
	})
}

// 页面事件的接收者：全部在界面线程、且都已被宿主校验过（viva/reader）。
type readerEvents struct{ s *Screen }

func (e readerEvents) Matches(count int) { e.s.detail.panel.findCount = count }

// Scroll 记阅读位置（渲染层 useDebouncedScrollSave 的 flush：标签与该标签的位置一起写进阅读记忆）。
// 事件所属的条目与标签由宿主按序号找回，所以换条目前补报的那一次不会记到新条目名下。
func (e readerEvents) Scroll(itemID string, tab reader.Tab, top float64) {
	rt := ReadingTab(tab)
	e.s.patchReading(itemID, ReadingPatch{Tab: rt, ScrollTopByTab: map[ReadingTab]float64{rt: top}})
	// 还在「刚打开、尚未换过标签」的状态时，把待恢复的位置跟到最新：岛被收起（托盘）后重开，要回到用户最后看到的地方，而不是打开时的。
	if p := &e.s.detail.panel; p.restoreTop != nil && p.itemID == itemID && p.restoreTab == rt {
		p.restoreTop = &top
	}
}

// Catalog 是楼层目录的开合（渲染层 onOpenChange={setCatalogOpen}）：状态在面板里，persistPanel 随后写进记忆。
func (e readerEvents) Catalog(open bool) { e.s.detail.panel.catalogOpen = open }

func (e readerEvents) Toc(open bool) { e.s.setTocOpen(open) }

// Link 用系统浏览器打开页面里的链接（宿主已校验为 http/https）。
func (e readerEvents) Link(url string) { e.s.openSource(url) }

// Copy 把页面里复制的文本（代码块的复制钮）写进系统剪贴板，并把成败告诉宿主，宿主再回执给页面（页面的「已复制」勾只在成功后出现，
// 与渲染层 writeText 成功之后才亮一致，台账 B-069）。渲染层的复制失败是静默的；这里仍弹带原因的失败提示并记日志——
// 用户操作失败必须可理解（AGENTS.md），见台账「有意的增强」。
func (e readerEvents) Copy(text string) error {
	s := e.s
	if s.Guard.Notify == nil {
		return errors.New("library: 没有剪贴板服务")
	}
	if err := s.Guard.Notify.CopyText(text); err != nil {
		s.Guard.Report(locales.LibraryCopyCode(), err)
		return err
	}
	return nil
}

// Lightbox 打开图片查看器：另开一个盖满窗口的岛（detailviewer.go）。
func (e readerEvents) Lightbox(images []reader.Image, index int) { e.s.showViewer(images, index) }

// Toast 是页面要给用户看的警告（页面里的组件调用 showToast("…", "warning")）。
func (e readerEvents) Toast(text string) {
	if n := e.s.Guard.Notify; n != nil {
		n.Warning(text)
	}
}

// Key 是页面转交的窗口级快捷键：焦点在岛里时窗口自己收不到按键，页面把渲染层挂在 window 上的那几个转过来。
func (e readerEvents) Key(key string, ctrl, alt, shift bool) {
	s := e.s
	mod := gfx.Modifiers(0)
	if ctrl {
		mod |= gfx.ModifierControl
	}
	if alt {
		mod |= gfx.ModifierAlt
	}
	if shift {
		mod |= gfx.ModifierShift
	}
	switch key {
	case "arrowleft", "arrowright":
		if alt && !ctrl && s.detail.modal.open {
			dir := -1
			if key == "arrowright" {
				dir = 1
			}
			s.Store.Spawn(func() { s.Store.ReviewNavigate(s.Ctx, dir) })
		}
	default:
		s.detailShortcut(gfx.KeyEvent{Text: key, Modifiers: mod})
	}
}

// FindReset 是页面里要求清空查找词（楼层过滤的「清除」）：与查找条上的清除钮同一件事。
func (e readerEvents) FindReset() {
	p := &e.s.detail.panel
	p.find.SetText("")
	p.findQuery, p.findActive = "", 0
}

// Failed 什么也不做：诊断已由宿主的 Diag 记进日志，失败状态由 readerArea 向宿主查（Host.Failure）后画出来，那里有重试。
func (e readerEvents) Failed(error) {}

// 主题阅读「目录」浮层的开合（渲染层 ReconstructionReader 的 catalogOpen）：按「条目 + 来源」存在界面偏好 reading.catalogOpen，
// 键是 original:<条目 ID>:<来源>，值只有 true（缺省收起；渲染层写 "false" 时迁移也不带）。

const maxCatalogFlags = 500

func tocPrefKey(itemID, sourceKind string) string { return "original:" + itemID + ":" + sourceKind }

// loadTocOpen 读一份目录的开合；没有存储或坏数据都是收起。
func loadTocOpen(prefs services.Prefs, itemID, sourceKind string) bool {
	if prefs == nil {
		return false
	}
	raw, ok := prefs.Get(settings.PrefCatalogOpen)
	if !ok {
		return false
	}
	var flags map[string]bool
	if json.Unmarshal(raw, &flags) != nil {
		return false
	}
	return flags[tocPrefKey(itemID, sourceKind)]
}

// storeTocOpen 写一份目录的开合：展开记 true，收起就删键；超过上限时按键名排序留前面的（与迁移同口径）。
func storeTocOpen(prefs services.Prefs, itemID, sourceKind string, open bool) error {
	if prefs == nil {
		return nil
	}
	flags := map[string]bool{}
	if raw, ok := prefs.Get(settings.PrefCatalogOpen); ok {
		if json.Unmarshal(raw, &flags) != nil || flags == nil {
			flags = map[string]bool{}
		}
	}
	key := tocPrefKey(itemID, sourceKind)
	if open {
		flags[key] = true
	} else {
		delete(flags, key)
	}
	if len(flags) > maxCatalogFlags {
		others := make([]string, 0, len(flags))
		for k := range flags {
			if k != key {
				others = append(others, k)
			}
		}
		sort.Strings(others)
		for _, k := range others[maxCatalogFlags-1:] { // 留下按键名排在前面的 maxCatalogFlags-1 项加上刚写的这一项
			delete(flags, k)
		}
	}
	return prefs.Set(settings.PrefCatalogOpen, flags)
}

// syncToc 在条目或来源变了时从偏好读出这一份目录的开合（渲染层 effect 随 editKey 重读 localStorage）。
func (s *Screen) syncToc(itemID, sourceKind string) {
	p := &s.detail.panel
	if key := itemID + "\x00" + sourceKind; p.tocKey != key {
		p.tocKey = key
		p.tocOpen = loadTocOpen(s.Prefs, itemID, sourceKind)
	}
}

// setTocOpen 改目录的开合并写进偏好（渲染层 changeCatalog）；写入失败只记日志，目录照常开合。
func (s *Screen) setTocOpen(open bool) {
	p := &s.detail.panel
	p.tocOpen = open
	itemID, sourceKind, ok := strings.Cut(p.tocKey, "\x00")
	if !ok {
		return
	}
	if err := storeTocOpen(s.Prefs, itemID, sourceKind, open); err != nil {
		s.Guard.Background("library.catalogOpen", err)
	}
}
