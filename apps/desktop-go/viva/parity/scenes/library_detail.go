package scenes

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 知识库条目详情（M2-4）的整窗场景：详情头部（标题、chip、保存状态、动作区）、通知条（冲突、来源版本、复核）、
// 内容面板的标签栏与查找条、专注阅读、表格视图的详情浮层。阅读区在 React 一侧被隐去（HIDE_READER_CSS），Viva 一侧留白（台账 B-059）。
// 与 React 采集器（scripts/parity/scenes/react-library-detail-scenes.mjs）一一对应：同样的条目、同样的点击位置与顺序；
// 详情字段取自详情夹具（testdata/library-detail-fixture.json），两侧共用。

// 场景要点的卡片标题与条目 ID（取自列表夹具 data 变体）。
const (
	detailNote        = "学习笔记：Go 并发模式" // item-001：笔记，置顶，合集「学习笔记」，标签 AI
	detailNoteID      = "item-001"
	detailWebpage     = "如何构建本地优先的个人知识库"   // item-003：网页，有来源
	detailVideo       = "Transformer 架构详解" // item-004：视频，有文字稿
	detailLongTitle   = "一条标题很长很长"         // item-005：长标题
	detailUntitled    = "无标题"              // item-006：标题为空的片段
	detailImageIndex  = 8                  // item-009「小红书：极简桌面布置灵感」：图片，有图中文字，列表里第 9 张（800×600 与大字号档的首屏之外，用方向键选）
	detailForumIndex  = 11                 // item-012「技术论坛：SQLite WAL 模式的备份坑」：论坛，有总结与逐楼回复，列表里第 12 张（同上）
	detailBlankIndex  = 13                 // item-014「读书清单」：正文为空的笔记，列表里第 14 张（首屏放得下 12 张，用方向键选）
	detailReviewIndex = 16                 // item-017「网页收藏：CSS 容器查询指南」：待复核，列表里第 17 张（首屏之外，用方向键选）
	detailTrashed     = "回收站：误删的草稿"        // item-042：回收站里的笔记

	// 交互步骤要点的按钮名（渲染层 i18n 的中文界面）。
	detailMoreActions = "更多操作"
	detailCollection  = "学习笔记"
)

// libraryDetailFixture 取详情夹具；它是编译进来的常量，解码失败是程序错误。
func libraryDetailFixture() parity.LibraryDetailFixture {
	f, err := parity.LoadLibraryDetailFixture()
	if err != nil {
		panic(err)
	}
	return f
}

// seedLibraryDetail 把详情夹具放进假服务，返回要放进条目全集的条目：夹具里标了复核状态的条目带上状态（列表里不显示复核，
// React 一侧只在详情读取里给）。没有 opt.detail 时什么也不做。
func seedLibraryDetail(svc *servicestest.Library, entries []services.LibraryEntry, opt libraryOptions) []services.LibraryEntry {
	if !opt.detail {
		return entries
	}
	fix := libraryDetailFixture()
	out := slices.Clone(entries)
	for i := range out {
		d, ok := fix.Details[out[i].ID]
		if !ok {
			continue
		}
		svc.SeedDetail(out[i].ID, servicestest.DetailBody{Content: d.Content, Summary: d.Summary, Transcript: d.Transcript, SourceURI: d.SourceURI})
		if d.ReviewStatus != "" {
			out[i].ReviewStatus = d.ReviewStatus
			out[i].ReviewReasons = slices.Clone(d.ReviewReasons)
		}
	}
	if opt.detailVersions {
		for id, versions := range fix.SourceVersions {
			svc.SeedSourceVersions(id, versions)
		}
	}
	if opt.failGet {
		svc.FailOn("GetItem", errors.New(fix.Failures.Get))
	}
	if opt.failSave {
		svc.FailDraft(fix.Failures.Save)
	}
	return out
}

// detailHoldGet 让之后的第一次详情读取卡在服务里，并把后台动作改成异步（卡住的读取不能卡住整个会话）：读取中的画面。
// 对应 React 采集器 config.detail.get = "hold"。
func detailHoldGet(_ *headless.Session, _ *kit.Look, e *libraryEnv) error {
	e.svc.HoldGet()
	e.store.Spawn = func(fn func()) { go fn() }
	return nil
}

// detailHoldSave 让之后的第一次草稿保存卡在服务里，并把后台动作改成异步：保存中的画面。对应 config.detail.save = "hold"。
func detailHoldSave(_ *headless.Session, _ *kit.Look, e *libraryEnv) error {
	e.svc.HoldSave()
	e.store.Spawn = func(fn func()) { go fn() }
	return nil
}

// detailWaitText 等画面上出现以 prefix 开头的一行文字（后台协程改了状态之后才会出现，见 waitTextPrefix）。
func detailWaitText(prefix func() string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		want := prefix()
		found, err := waitTextPrefix(s, want, 400)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("等不到画面上以「%s」开头的文字", want)
		}
		return nil
	}
}

// detailClickTab 点内容面板标签栏里的一枚标签：exact 为真按整名，否则按名字前缀（文字稿标签的名字带字数）。
func detailClickTab(name string, exact bool) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		var err error
		if exact {
			err = parity.ClickRole(s, gfx.RoleButton, name)
		} else {
			err = clickRolePrefix(s, gfx.RoleButton, name)
		}
		if err != nil {
			return err
		}
		return s.Rest()
	}
}

// detailOpenHeaderMenu 点详情头部里会弹出菜单的钮（取整，见 snap）：同名的钮还有侧栏与列表里的，详情头部在最右边，取最靠右的那个。
func detailOpenHeaderMenu(role gfx.Role, name string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		var pick gfx.Rect
		found := false
		for _, n := range s.Access() {
			if n.Role == role && n.Label == name && (!found || n.Bounds.X > pick.X) {
				pick, found = n.Bounds, true
			}
		}
		if !found {
			return fmt.Errorf("找不到 %s「%s」", role, name)
		}
		if err := s.Click(snap(pick.X+pick.Width/2), snap(pick.Y+pick.Height/2)); err != nil {
			return err
		}
		return s.Rest()
	}
}

// detailKeys 按一个组合键，等画面落定（Ctrl+F、Alt+Z：对应 React 一侧 keyboard.press）。
func detailKeys(chord string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		if err := s.Keys(chord); err != nil {
			return err
		}
		return s.Rest()
	}
}

// detailKeysCaretDark 按一个组合键（让焦点落进文本框），等画面落定，再把插入符停在熄灭的相位（parity.ParkCaretDark，台账 G-028）：
// 参照采集器把 caret-color 设成透明，参照图里没有插入符；Viva 的插入符照画，淡色占位符旁一道深色竖线会拉歪墨迹剖面。
// 插入符从按键那一帧开始计时，所以起点取按键之前的虚拟时间。
func detailKeysCaretDark(chord string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		since := s.Now()
		if err := s.Keys(chord); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		return parity.ParkCaretDark(s, since)
	}
}

// detailRetitle 把标题框的内容换成 text（对应 React 一侧的 fill：聚焦、整体替换），再让待存的改动落盘
// （React 一侧等防抖 800 ms 到点；这里的计时器是空的，由步骤直接触发那次保存）。保存在后台，卡住的保存不会卡住会话。
func detailRetitle(text string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, e *libraryEnv) error {
		if err := parity.ClickRole(s, gfx.RoleTextField, locales.LibraryTitlePlaceholder()); err != nil {
			return err
		}
		// 无头会话里 Ctrl+A 只认终端控件；用「跳到行尾、Shift+Home 选到行首」选中整行（单行输入框），再打字替换。
		for _, key := range []string{"end", "shift+home"} {
			if err := s.Keys(key); err != nil {
				return err
			}
		}
		if err := s.Type(text); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		e.store.Spawn(func() { e.store.FlushPendingSave(e.ctx) })
		return s.Rest()
	}
}

// detailEditExternally 模拟另一入口把这条的标题改成别的（让之后的草稿保存撞上冲突）。
// 对应 React 采集器里 config.detail.save = "conflict"（页内的 saveDraft 总是回冲突）。
func detailEditExternally(id, title string) libraryEnvStep {
	return func(_ *headless.Session, _ *kit.Look, e *libraryEnv) error {
		e.svc.EditExternally(id, func(it *services.LibraryItem) { it.Title = title })
		return nil
	}
}

// compositedInkWaiver 登记 A-009 在带 backdrop-filter 的元素上的延伸：专注阅读右下角的悬浮钮是 `backdrop-blur-sm`，
// Chromium 把它放进独立的合成层，文字用灰度抗锯齿；Viva 的 LCD / 灰度是整窗开关，同一帧里只让这一处用灰度做不到，墨迹总量偏大。
// 证据（2026-11-21，浅色皇家蓝中档 1280×800 的 detail-focus）：「退出专注」所在 56×20 像素区域里，渲染层的色差（max−min）
// 最大 26、均值 8.73（只有底色与描边自带的），Viva 最大 153、均值 24.48（LCD 彩边）；墨迹总量比 1.165。
// 只放过这一段文字、「Viva 偏多」、不超过 1.4 倍的墨迹总量比：位置、字重、颜色、字号照常判定。
func compositedInkWaiver(subject string) parity.Waiver {
	return parity.Waiver{
		Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: subject, MinDelta: 0.1, MaxDelta: 0.4, Dir: 1,
		Note: "带 backdrop-filter 的悬浮钮在合成层里用灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（位置、字重、颜色、字号均一致）",
	}
}

// waitRolePrefix 等可访问树里出现角色为 role、名字以 prefix 开头的节点：后台动作（保存冲突、来源版本读取）落地之后，
// 通知条要到下一帧才画出来，紧接着点它会落空（对应 Playwright 的 getByRole(...).click() 自带的等待）。
func waitRolePrefix(s *headless.Session, role gfx.Role, prefix string, attempts int) (bool, error) {
	for range attempts {
		for _, n := range s.Access() {
			if n.Role == role && strings.HasPrefix(n.Label, prefix) {
				return true, nil
			}
		}
		if err := s.Frame(); err != nil {
			return false, err
		}
		if err := s.Rest(); err != nil {
			return false, err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false, nil
}

// detailOpenSummary 点开一个 <details> 的概要（Chromium 的可访问树里是 DisclosureTriangle，对照里按按钮算，台账 G-022）。
// name 在步骤执行时才取值：场景在包初始化时注册，那时界面语言还是默认的中文；英文变体（-lang en）在这之后才切语言，
// 提前取值会拿着中文名去找英文界面里的按钮。
func detailOpenSummary(name func() string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		prefix := name()
		found, err := waitRolePrefix(s, gfx.RoleButton, prefix, 100)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("等不到可访问名以「%s」开头的按钮", prefix)
		}
		if err := clickRolePrefix(s, gfx.RoleButton, prefix); err != nil {
			return err
		}
		return s.Rest()
	}
}

// tableModalBaseSteps 把页面带到「详情浮层打开之前」（遮罩背后的页面，见 parity.BackdropSpec.BaseSteps）：
// 渲染层点行时先选中该行并把它横向滚进视野（表格比容器宽 17 px，行的左缘随之左移 17 px），遮罩背后的页面就停在这个状态；
// 这里同样点一下行、等浮层开了再按 Esc 关掉，得到同样的选中与滚动。
// 不借用 libraryModalBackdrop 的前置步骤：那是侧栏里的对话框用的（紧凑布局下先把侧栏展开），详情浮层是从表格里打开的，侧栏保持收起。
func tableModalBaseSteps(s *headless.Session, look *kit.Look) error {
	if err := detailOpenTableModal(0)(s, look, nil); err != nil {
		return err
	}
	dialog := locales.CommonDialog()
	opened, err := waitRolePrefix(s, gfx.RoleGroup, dialog, 100)
	if err != nil {
		return err
	}
	if !opened {
		return fmt.Errorf("等不到详情浮层「%s」打开", dialog)
	}
	if err := s.Keys("esc"); err != nil {
		return err
	}
	for range 100 {
		open := false
		for _, n := range s.Access() {
			open = open || (n.Role == gfx.RoleGroup && n.Label == dialog)
		}
		if !open {
			return s.Rest()
		}
		if err := s.Frame(); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("详情浮层「%s」按 Esc 后没有关掉", dialog)
}

// detailOpenTableModal 点表格第 row 行的「类型」列（不压在标题按钮或操作按钮上）打开详情浮层：
// React 一侧先点工具条的「列表视图」，这里把视图模式偏好预置成 list（终态相同）。
func detailOpenTableModal(row int) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		x, y, err := tableRowPoint(s, row)
		if err != nil {
			return err
		}
		if err := s.Click(snap(x), snap(y)); err != nil {
			return err
		}
		return s.Rest()
	}
}

func init() {
	detail := libraryOptions{variant: "data", list: true, detail: true}
	versions := detail
	versions.detailVersions = true
	holdGet := detail
	holdGet.holdDetail = true
	failGet := detail
	failGet.failGet = true
	holdSave := detail
	holdSave.holdDetail = true
	failSave := detail
	failSave.failSave = true
	table := libraryOptions{variant: "data", list: true, view: "list", detail: true}

	reg := func(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) {
		registry[id] = libraryEnvScene(id, opt, steps, mode, waivers...)
	}
	cfg := libraryDetailFixture()
	mine := cfg.Conflict.MineTitle
	selectNote := listClickCard(detailNote)
	// 卡片标题的字距放过项（A-013、A-014）、浮层盖在表格上的放过项，与列表、批量场景同一套。
	cards := semiboldKerningWaivers()
	// 详情浮层顶部整理导航条里的「/」与「1」：浮层灰度抗锯齿对一像素细笔画字形的 LCD 彩边（A-009 的延伸，待追认：台账 C-010）。
	tableOverlay := slices.Concat([]parity.Waiver{overlayInkWaiver}, overlayThinGlyphWaivers(), tableBaseWaivers())
	// 浮层里的文字（菜单项）：渲染层灰度抗锯齿，Viva 整窗 LCD（A-009，范围见 overlayInkWaiver）。
	overlay := append([]parity.Waiver{overlayInkWaiver}, cards...)
	// 专注阅读右下角的悬浮钮（backdrop-blur-sm）：它在 Viva 里画在页面层、不带浮层标记，overlayInkWaiver 够不着，单独登记。
	focus := slices.Concat([]parity.Waiver{compositedInkWaiver("「退出专注」")}, enFocusWaivers(), cards)
	// 「49 字」：文字稿页签上的字数是 `text-[10px] opacity-60`（ContentPanel），整段带透明度，渲染层灰度抗锯齿（A-009 的延伸，
	// 依据同 opacityInkWaiver）。页签在视频条目里一直在，所以 detail-video 与 detail-video-transcript 都要登记。
	transcript := slices.Concat([]parity.Waiver{opacityInkWaiver("「49 字」")}, enTranscriptTabWaivers(), cards)
	// 回收站条目头部被禁用的「未归合集」chip：`disabled:opacity-60`，同上（A-009 的延伸）。
	trashed := append([]parity.Waiver{opacityInkWaiver("「未归合集」")}, cards...)
	// 图中文字页签的 12px 描边图标（A-003 的延伸，待追认：台账 C-010）。
	image := slices.Concat(imageTabIconWaivers(), enRecognizedTabWaivers(), cards)
	// 版本记录行的间隔点「·」（G-016 的延伸，待追认：台账 C-010）。
	revisions := slices.Concat(middleDotWaivers(), enRevisionsWaivers(), cards)
	// 英文变体里的字偶距登记（A-014、A-026 的英文延伸，待追认：台账 C-010）：待复核条的标题、冲突条的提示句与钮。
	review := slices.Concat(enImportReviewWaivers(), cards)
	conflict := slices.Concat(enConflictWaivers(), cards)
	// 查找条是 `backdrop-blur-sm`：占位符文字在合成层里用灰度抗锯齿（A-009 的延伸，依据同 compositedInkWaiver，待追认：台账 C-010）。
	find := append([]parity.Waiver{compositedInkWaiver("「在当前页查找…」")}, cards...)

	reg("app.window.library.detail-empty", detail, nil, parkAll, cards...)
	reg("app.window.library.detail-loading", holdGet, listChain(detailHoldGet, listClickCard(detailNote), detailWaitText(locales.LibraryLoadingItem)), parkAll, cards...)
	reg("app.window.library.detail-error", failGet, listClickCard(detailNote), parkAll, cards...)
	reg("app.window.library.detail-note", detail, selectNote, parkAll, cards...)
	reg("app.window.library.detail-note-blank", detail, listKeys("down", detailBlankIndex+1), parkAll, cards...)
	reg("app.window.library.detail-untitled", detail, listClickCard(detailUntitled), parkAll, cards...)
	reg("app.window.library.detail-long-title", detail, listClickCard(detailLongTitle), parkAll, cards...)
	reg("app.window.library.detail-webpage", detail, listClickCard(detailWebpage), parkAll, cards...)
	reg("app.window.library.detail-video", detail, listClickCard(detailVideo), parkAll, transcript...)
	reg("app.window.library.detail-video-transcript", detail, listChain(listClickCard(detailVideo), detailClickTab("文字稿", false)), parkAll, transcript...)
	reg("app.window.library.detail-forum", detail, listKeys("down", detailForumIndex+1), parkAll, cards...)
	reg("app.window.library.detail-forum-replies", detail, listChain(listKeys("down", detailForumIndex+1), detailClickTab("讨论", true)), parkAll, cards...)
	reg("app.window.library.detail-image", detail, listKeys("down", detailImageIndex+1), parkAll, image...)
	reg("app.window.library.detail-image-text", detail, listChain(listKeys("down", detailImageIndex+1), detailClickTab("图中文字", true)), parkAll, image...)
	reg("app.window.library.detail-trashed", detail, listChain(facetLabel(locales.LibraryScopeTrash), listClickCard(detailTrashed)), parkAll, trashed...)
	reg("app.window.library.detail-review", detail, listKeys("down", detailReviewIndex+1), parkAll, review...)
	reg("app.window.library.detail-review-open", detail, listChain(listKeys("down", detailReviewIndex+1), detailOpenSummary(locales.ArticleReaderImportIncomplete)), parkAll, review...)
	reg("app.window.library.detail-revisions", versions, listClickCard(detailWebpage), parkAll, cards...)
	reg("app.window.library.detail-revisions-open", versions, listChain(
		listClickCard(detailWebpage),
		detailOpenSummary(func() string { return prefixOf(locales.LibrarySourceRevisions) }),
		detailOpenSummary(func() string { return detailWebpage + " · " }),
	), parkAll, revisions...)
	reg("app.window.library.detail-conflict", detail, listChain(selectNote, detailEditExternally(detailNoteID, cfg.Conflict.SavedTitle), detailRetitle(mine)), parkAll, conflict...)
	reg("app.window.library.detail-conflict-open", detail, listChain(
		selectNote, detailEditExternally(detailNoteID, cfg.Conflict.SavedTitle), detailRetitle(mine),
		detailOpenSummary(locales.LibraryDraftConflictCompare),
	), parkAll, conflict...)
	reg("app.window.library.detail-find", detail, listChain(selectNote, detailKeysCaretDark("ctrl+f")), parkKeepFocus, find...)
	reg("app.window.library.detail-menu-collection", detail, listChain(selectNote, detailOpenHeaderMenu(gfx.RoleCombobox, detailCollection)), parkKeepFocus, overlay...)
	reg("app.window.library.detail-menu-more", detail, listChain(selectNote, detailOpenHeaderMenu(gfx.RoleCombobox, detailMoreActions)), parkKeepFocus, overlay...)
	reg("app.window.library.detail-focus", detail, listChain(selectNote, detailKeys("alt+z")), parkAll, focus...)
	reg("app.window.library.detail-saving", holdSave, listChain(selectNote, detailHoldSave, detailRetitle(mine), detailWaitText(locales.LibrarySaving)), parkAll, cards...)
	reg("app.window.library.detail-saved", detail, listChain(selectNote, detailRetitle(mine)), parkAll, cards...)
	reg("app.window.library.detail-save-failed", failSave, listChain(selectNote, detailRetitle(mine)), parkAll, cards...)
	reg("app.window.library.table-detail-modal", table, detailOpenTableModal(0), parkAll, tableOverlay...)
	// 全屏 Modal：遮罩背后的页面模糊（A-007，backdrop-blur-md 是 12），渲染层背景留在可访问树里、Viva 的模态把它拿掉（A-015，补回比对）。
	modal := modalBackdrop(locales.CommonDialog())
	modal.PanelNameFn = locales.CommonDialog // 英文变体里面板名是英文的，要等界面语言设定之后才取
	modal.BaseSteps = tableModalBaseSteps
	registry["app.window.library.table-detail-modal"] = withBackdrop(registry["app.window.library.table-detail-modal"], modal)
}
