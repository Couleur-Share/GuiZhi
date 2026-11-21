package scenes

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 文章工具侧栏（M2-6b、M2-6c）的整窗场景：从头部「更多操作」菜单打开侧栏，看文章信息与标签、AI 交接稿、来源评论、网页的来源版本，
// 以及 M2-6c 的内容处理：AI 摘要（折叠/展开）、图片识别图中文字（未识别/已识别）、单条标签建议。
// 与 React 采集器（scripts/parity/scenes/react-library-detail-scenes.mjs 的 detail-tools-*）一一对应：同样的条目、同样的点击顺序；
// 数据取详情夹具的 tools 段，两侧共用。阅读区同样隐去（React 一侧 HIDE_READER_CSS，Viva 一侧留白），侧栏盖在阅读区右侧。

// toolsMode 是工具侧栏场景放进服务的数据，与 React 采集器 config.detail.tools 同值。
type toolsMode string

const (
	toolsNone          toolsMode = ""               // 不放
	toolsBase          toolsMode = "base"           // 网页的来源版本
	toolsComments      toolsMode = "comments"       // 再加来源评论（评论条目的来源链接换成平台来源）
	toolsCommentsEmpty toolsMode = "comments-empty" // 来源链接换成平台来源，评论读到是空的
	toolsSummary       toolsMode = "summary"        // M2-6c：夹具的摘要条目带一份 Markdown 摘要
	toolsOcr           toolsMode = "ocr"            // M2-6c：图片条目的正文带 local-image 引用、没有「图中文字」小节
	toolsOcrDone       toolsMode = "ocr-done"       // M2-6c：图片条目的正文带 local-image 引用、已有「图中文字」小节
	toolsTagSuggest    toolsMode = "tag-suggest"    // M2-6c：标签建议的候选取夹具（模型回的标签）
)

// content 是条目 id 的正文：OCR 场景里图片条目换成带 local-image 引用的正文（夹具里的图片条目引用的是 asset://，不是识别入口认的协议），其余取详情夹具自带的（own）。
func (m toolsMode) content(fix parity.LibraryDetailFixture, id, own string) string {
	if id != fix.Tools.AI.OcrItem {
		return own
	}
	switch m {
	case toolsOcr:
		return fix.Tools.AI.OcrContent
	case toolsOcrDone:
		return fix.Tools.AI.OcrDoneContent
	}
	return own
}

// summary 是条目 id 的 AI 摘要：摘要场景里夹具的摘要条目带一份摘要，其余取详情夹具自带的（own，全是空）。
func (m toolsMode) summary(fix parity.LibraryDetailFixture, id, own string) string {
	if m == toolsSummary && id == fix.Tools.AI.SummaryItem {
		return fix.Tools.AI.Summary
	}
	return own
}

// commentsHere 报告条目 id 是否要当成平台来源：夹具里没有一条详情带小红书或抖音链接，评论场景要在评论条目上换一个。
func (m toolsMode) commentsHere(fix parity.LibraryDetailFixture, id string) bool {
	return (m == toolsComments || m == toolsCommentsEmpty) && id == fix.Tools.CommentsItem
}

// sourceURI 是条目 id 的来源链接：评论场景里评论条目取夹具给的平台链接，其余取详情夹具自带的（own）。
func (m toolsMode) sourceURI(fix parity.LibraryDetailFixture, id, own string) string {
	if m.commentsHere(fix, id) {
		return fix.Tools.CommentsSourceURI
	}
	return own
}

// seed 把网页的来源版本与（toolsComments 时）来源评论放进假服务。
func (m toolsMode) seed(svc *servicestest.Library, fix parity.LibraryDetailFixture) {
	if m == toolsNone {
		return
	}
	for id, web := range fix.Tools.Web {
		svc.SeedWebVersions(id, web)
	}
	if m == toolsComments {
		svc.SeedSourceComments(fix.Tools.CommentsItem, fix.Tools.Comments)
	}
	if m == toolsTagSuggest {
		svc.SeedTagSuggestions(fix.Tools.AI.TagSuggestions, nil)
	}
}

// detailClickMenuItem 点已展开的菜单里可访问名为 name() 的项（取整，见 snap）。name 在步骤执行时才取值（理由同 detailOpenSummary）。
func detailClickMenuItem(name func() string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		want := name()
		n, ok := s.AccessByRole(gfx.RoleMenuItem, want)
		if !ok {
			return fmt.Errorf("找不到 %s「%s」", gfx.RoleMenuItem, want)
		}
		if err := s.Click(snap(n.Bounds.X+n.Bounds.Width/2), snap(n.Bounds.Y+n.Bounds.Height/2)); err != nil {
			return err
		}
		return s.Rest()
	}
}

// detailWaitGroup 等可访问树里出现名字为 name() 的分组（工具侧栏打开）。
func detailWaitGroup(name func() string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		want := name()
		found, err := waitRolePrefix(s, gfx.RoleGroup, want, 100)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("等不到分组「%s」", want)
		}
		return s.Rest()
	}
}

// toolsOpenVia 从头部「更多操作」菜单点开工具侧栏，等侧栏出现（打开时焦点落到收起钮上）。item 是点的菜单项：
// 「文章信息与工具」，或评论入口（「采集评论」「查看来源评论」，同样点开侧栏并把评论卡片展开）。对应 React 一侧的 openToolsVia。
func toolsOpenVia(item func() string) libraryEnvStep {
	return listChain(
		detailOpenHeaderMenuNamed(gfx.RoleCombobox, locales.LibraryMoreActions),
		detailClickMenuItem(item),
		detailWaitGroup(locales.ArticleReaderTools),
	)
}

// toolsOpen 点开工具侧栏（菜单项「文章信息与工具」）。
func toolsOpen() libraryEnvStep { return toolsOpenVia(locales.ArticleReaderTools) }

// toolsClick 点工具侧栏里的钮（取整，见 snap）：同名的钮还有头部与列表里的，只认落在侧栏范围里的。
// prefix 为真时按名字前缀找（展开钮的名字带数量、版本钮带提示）。对应 React 一侧的 clickInTools。
// caretDark 为真时点完把插入符停在熄灭的相位（点开的浮层把焦点交给文本框，同 detailKeysCaretDark，台账 G-028）。
func toolsClick(role gfx.Role, name func() string, prefix, caretDark bool) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		want := name()
		panel, ok := s.AccessByRole(gfx.RoleGroup, locales.ArticleReaderTools())
		if !ok {
			return fmt.Errorf("找不到工具侧栏「%s」", locales.ArticleReaderTools())
		}
		return clickAccessible(s, role, want, prefix, caretDark, &panel.Bounds, "工具侧栏里")
	}
}

// popoverClick 点标签选择浮层里的钮（取整，见 snap）：浮层是画在窗口层的门户，不在侧栏的范围里，所以不限范围；浮层里的钮名都是唯一的。
// 对应 React 一侧的 generateTagSuggestions。caretDark 的含义同 toolsClick。
func popoverClick(role gfx.Role, name func() string, caretDark bool) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		return clickAccessible(s, role, name(), false, caretDark, nil, "窗口里")
	}
}

// clickAccessible 点可访问树里第一个角色是 role、名字是 want（prefix 为真时按前缀）的节点的中心；within 非空时只认中心落在它里面的。
func clickAccessible(s *headless.Session, role gfx.Role, want string, prefix, caretDark bool, within *gfx.Rect, where string) error {
	since := s.Now()
	for _, n := range s.Access() {
		match := n.Label == want
		if prefix {
			match = strings.HasPrefix(n.Label, want)
		}
		if n.Role != role || !match || (within != nil && !toolsInside(n.Bounds, *within)) {
			continue
		}
		if err := s.Click(snap(n.Bounds.X+n.Bounds.Width/2), snap(n.Bounds.Y+n.Bounds.Height/2)); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		if caretDark {
			return parity.ParkCaretDark(s, since)
		}
		return nil
	}
	return fmt.Errorf("%s找不到 %s「%s」", where, role, want)
}

// toolsInside 报告 inner 的中心是否落在 outer 里。
func toolsInside(inner, outer gfx.Rect) bool {
	cx, cy := inner.X+inner.Width/2, inner.Y+inner.Height/2
	return cx >= outer.X && cx <= outer.X+outer.Width && cy >= outer.Y && cy <= outer.Y+outer.Height
}

// toolsPickFirstVersion 展开原文版本下拉，选第一项（最新的版本）。对应 React 一侧的 pickFirstVersion。
func toolsPickFirstVersion() libraryEnvStep {
	return func(s *headless.Session, look *kit.Look, e *libraryEnv) error {
		if err := toolsClick(gfx.RoleCombobox, locales.WebCaptureVersions, false, false)(s, look, e); err != nil {
			return err
		}
		var pick gfx.Rect
		found := false
		for _, n := range s.Access() {
			if n.Role == gfx.RoleOption && (!found || n.Bounds.Y < pick.Y) {
				pick, found = n.Bounds, true
			}
		}
		if !found {
			return fmt.Errorf("版本下拉里没有选项")
		}
		if err := s.Click(snap(pick.X+pick.Width/2), snap(pick.Y+pick.Height/2)); err != nil {
			return err
		}
		return s.Rest()
	}
}

func init() {
	// 侧栏场景把内容面板的工具栏隐去（React 一侧 config.detail.hideToolbar）：侧栏在渲染层 DOM 里排在内容面板之前、在 Viva 里只能排在之后，
	// 工具栏文字的前后之别会让整段侧栏文字错位比不上（台账 C-013）；withToolbar 留着工具栏，专门钉住这个差别。
	base := libraryOptions{variant: "data", list: true, detail: true, tools: toolsBase, hideToolbar: true}
	withToolbar := base
	withToolbar.hideToolbar = false
	comments := base
	comments.tools = toolsComments
	commentsEmpty := base
	commentsEmpty.tools = toolsCommentsEmpty

	reg := func(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) {
		registry[id] = libraryEnvScene(id, opt, steps, mode, waivers...)
	}
	selectNote := listClickCard(detailNote)
	selectWebpage := listClickCard(detailWebpage)
	// 卡片标题的字距放过项（A-013、A-014）与浮层里的文字（A-009，范围见 overlayInkWaiver），同详情场景。
	cards := semiboldKerningWaivers()
	overlay := append([]parity.Waiver{overlayInkWaiver}, cards...)
	// 标签浮层里的小标题「选择已有标签」「AI 建议标签」带字距（A-013，同批量场景的标签浮层）。
	overlayTag := slices.Concat(overlay, wideTrackingWaivers("选择已有标签"), wideTrackingWaivers("AI 建议标签"))
	// 带 aria-haspopup 的钮在渲染层是 button，在 Viva 里是 combobox（同头部「更多操作」）。
	popupButton := func(name func() string) libraryEnvStep { return toolsClick(gfx.RoleCombobox, name, false, false) }
	prefixButton := func(name func() string) libraryEnvStep { return toolsClick(gfx.RoleButton, name, true, false) }

	// 英文变体的小标题字偶距（A-026 的英文延伸，C-015）：只挂在有英文变体的场景上（M2-6c 起含 detail-tools-summary-expanded 与 detail-tools-ocr-done）（中文界面里 Subject 对不上，不命中）。
	en := enToolsWaivers()
	cardsEn := slices.Concat(cards, en)
	reg("app.window.library.detail-tools-note", base, listChain(selectNote, toolsOpen()), parkAll, cardsEn...)
	reg("app.window.library.detail-tools-note-toolbar", withToolbar, listChain(selectNote, toolsOpen()), parkAll, cards...)
	reg("app.window.library.detail-tools-note-picker", base, listChain(selectNote, toolsOpen(), toolsClick(gfx.RoleButton, locales.LibraryAddTag, false, true)), parkKeepFocus, overlayTag...)
	reg("app.window.library.detail-tools-note-handoff", base, listChain(selectNote, toolsOpen(), popupButton(locales.LibraryAiHandoffMore)), parkKeepFocus, overlay...)
	reg("app.window.library.detail-tools-webpage", base, listChain(selectWebpage, toolsOpen()), parkAll, cards...)
	reg("app.window.library.detail-tools-webpage-open", base, listChain(selectWebpage, toolsOpen(), prefixButton(locales.WebCaptureVersions)), parkAll, cardsEn...)
	reg("app.window.library.detail-tools-webpage-selected", base, listChain(selectWebpage, toolsOpen(), prefixButton(locales.WebCaptureVersions), toolsPickFirstVersion()), parkAll,
		slices.Concat(cards, versionPaneWaivers())...)
	reg("app.window.library.detail-tools-comments", comments, listChain(selectNote, toolsOpen()), parkAll, cards...)
	reg("app.window.library.detail-tools-comments-open", comments, listChain(selectNote, toolsOpen(), prefixButton(locales.LibrarySourceComments)), parkAll, cardsEn...)
	// 评论读到是空的时卡片在展开之前不显示（渲染层 SourceCommentsCard），入口只有头部「更多操作」菜单里的「采集评论」。
	reg("app.window.library.detail-tools-comments-empty", commentsEmpty, listChain(selectNote, toolsOpenVia(locales.LibraryCollectSourceComments)), parkAll, cards...)
	reg("app.window.library.detail-tools-comments-menu", comments, listChain(selectNote, detailOpenHeaderMenu(gfx.RoleCombobox, detailMoreActions)), parkKeepFocus, overlay...)

	// M2-6c：AI 摘要卡片（笔记带摘要，默认折叠、点开展开）、图片条目的图中文字入口（未识别 / 已有「图中文字」小节）、标签浮层里的 AI 建议标签。
	// 图片条目在列表里第 9 张（800×600 与大字号档的首屏之外），用方向键选，同 detail-image。
	summary := base
	summary.tools = toolsSummary
	ocr := base
	ocr.tools = toolsOcr
	ocrDone := base
	ocrDone.tools = toolsOcrDone
	tagSuggest := base
	tagSuggest.tools = toolsTagSuggest
	selectImage := listKeys("down", detailImageIndex+1)
	reg("app.window.library.detail-tools-summary-collapsed", summary, listChain(selectNote, toolsOpen()), parkAll, cards...)
	reg("app.window.library.detail-tools-summary-expanded", summary,
		listChain(selectNote, toolsOpen(), toolsClick(gfx.RoleButton, locales.LibraryAiSummaryExpand, false, false)), parkAll, cardsEn...)
	reg("app.window.library.detail-tools-ocr", ocr, listChain(selectImage, toolsOpen()), parkAll, cards...)
	reg("app.window.library.detail-tools-ocr-done", ocrDone, listChain(selectImage, toolsOpen()), parkAll, cardsEn...)
	// 点开标签浮层（焦点交给文本框，插入符停在暗相位，G-028）再点「生成」：模型请求在场景里同步返回（store.Spawn 同步），候选立刻出现。
	reg("app.window.library.detail-tools-tag-suggest", tagSuggest,
		listChain(selectNote, toolsOpen(), toolsClick(gfx.RoleButton, locales.LibraryAddTag, false, true), popoverClick(gfx.RoleButton, locales.LibraryTagsGenerate, true)),
		parkKeepFocus, overlayTag...)
}
