package library

import (
	"encoding/json"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
	"guizhi-mygo-shell/viva/tokens"
)

// 公众号网页快照面板（渲染层 WebSnapshotPane）：内容面板在「网页快照」那一页时的工具条与通知条，加上阅读区里的原文快照。
//
// 渲染层里它是一个组件：自己读快照、持有排版模式（原文 / 标准）、宽度档、补采状态，工具条经门户放进标签页那一行，
// 原文快照放在沙箱 iframe 里，标准排版则渲染 children（MarkdownPreview）。这里按同样的分工：
//   - 数据与异步动作（读取、补采、取消、导出）在 Store（store_snapshot.go）；
//   - 界面状态（排版模式、宽度档、目录开合、「更多」菜单）在 panelState.snap；
//   - 阅读区里的岛显示什么由 readerViewFor 决定（原文快照 → ViewSnapshot，其余 → 标准排版的 ViewOriginal）；
//   - 目录由岛里的页面画（SnapshotToc），原生工具条的「目录」钮只在目录没有常驻侧栏时才摆（页面经 SnapshotState 回报）。
//
// 面板状态随条目重置（panelState 整体换新，对应 ContentPanel 里 key={item.id}）；离开「网页快照」那一页同样重置——
// 渲染层里面板随 isWechatPreview 卸载，再进来是新挂载：重新读取、按默认排版显示。

// snapshotKey 是一次读取的输入：条目、版本与此刻的正文（正文变了，「已编辑」的判断就变了，渲染层 effect 依赖 item.content）。
type snapshotKey struct{ item, version, content string }

// snapshotUI 是网页快照面板的界面状态。
type snapshotUI struct {
	// version 是在看的来源版本（空是当前版本）；simple 为真是标准排版（渲染层 mode === "simple"）。
	version string
	simple  bool
	// width 是宽度档（渲染层 localStorage 的 wechat-reader-width，这里是偏好 reader.wechatWidth），widthSet 表示已经从偏好里读过。
	width    reader.SnapshotWidth
	widthSet bool
	// wideRail、tocOpen 是页面回报的版面状态（SnapshotState）：目录常驻侧栏时原生工具条不摆「目录」钮；tocOpen 是目录浮层的开合。
	wideRail, tocOpen bool

	// key 是已经发起读取的输入，keyed 表示发起过；stale 是发起那一刻 Store 里还留着的旧快照（界面线程手里这一帧的 View 是发起之前的快照，
	// 不能把它当成新读取的结果）；applied 是已按默认规则定过排版的那一份快照。
	key     snapshotKey
	keyed   bool
	stale   *services.LibraryWebSnapshotView
	applied *services.LibraryWebSnapshotView

	// 这一帧可信的读取结果（syncSnapshot 每帧重算）：view 为 nil 表示没有（读取中或失败）；loading 是读取中；err 是读取失败的原因；busy 是补采在途。
	view    *services.LibraryWebSnapshotView
	loading bool
	err     string
	busy    bool

	// noticeW 是读取失败与没有快照两条通知块（文字加行内按钮，snapshotInlineNotice）上一帧的内容宽度。
	noticeW float32

	// 宽度下拉与「更多」菜单；menuRect 是「更多」钮上一帧的外框（菜单的锚点与 Keep）。
	sel      kit.SelectState
	menu     anchoredMenu
	menuRect gfx.Rect
}

// original 报告此刻显示的是不是原文快照（渲染层 original：不是标准排版、且读到了文档）。
func (u *snapshotUI) original() bool { return !u.simple && u.view != nil && u.view.Document != "" }

// doc 是要在岛里显示的原文快照；nil 表示走标准排版。
func (u *snapshotUI) doc() *services.LibraryWebSnapshotView {
	if u.original() {
		return u.view
	}
	return nil
}

// edited 报告正文在采集之后被改过、且看的是当前版本（渲染层 `view?.edited && !versionId`）。
func (u *snapshotUI) edited() bool { return u.view != nil && u.view.Edited && u.version == "" }

// meta 是快照的资源清单与缺失原因；没有保存过快照时为 nil。
func (u *snapshotUI) meta() *services.LibraryWebSnapshotMeta {
	if u.view == nil {
		return nil
	}
	return u.view.Snapshot()
}

// snapshotPane 是 readerViewFor 的输入：「网页快照」那一页开着没有、要显示的原文快照（nil 走标准排版）、版本与宽度。
// 全是可比较的值，读者视图的切分缓存直接拿它当键（detailreader.go）。
type snapshotPane struct {
	view    bool
	doc     *services.LibraryWebSnapshotView
	version string
	width   reader.SnapshotWidth
}

func (p *panelState) snapshotPane() snapshotPane {
	return snapshotPane{view: p.snapshotView, doc: p.snap.doc(), version: p.snap.version, width: p.snap.width}
}

// snapshotReaderView 是原文快照交给岛的视图：整页文档、实例标识、版本（阅读位置按「条目 + 版本」记忆）、宽度档与图片清单（点图时查看器要用）。
func snapshotReaderView(pane snapshotPane) reader.View {
	assets := []string{}
	if meta := pane.doc.Snapshot(); meta != nil {
		for _, a := range meta.Assets {
			assets = append(assets, "local-image://"+a.FileName)
		}
	}
	return reader.View{
		Kind: reader.ViewSnapshot, SnapshotDoc: pane.doc.Document, SnapshotInstanceID: pane.doc.InstanceID,
		SnapshotVersionID: pane.version, SnapshotWidth: pane.width, SnapshotAssets: assets,
	}
}

// ---- 偏好：宽度档 ----

// loadSnapshotWidth 读宽度档偏好（缺省或不认识的值都是「自适应」，渲染层 saved === "original" || "wide" ? saved : "fluid"）。
func loadSnapshotWidth(prefs services.Prefs) reader.SnapshotWidth {
	if prefs == nil {
		return reader.SnapshotWidthFluid
	}
	raw, ok := prefs.Get(settings.PrefWechatWidth)
	if !ok {
		return reader.SnapshotWidthFluid
	}
	var saved string
	if json.Unmarshal(raw, &saved) != nil {
		return reader.SnapshotWidthFluid
	}
	switch reader.SnapshotWidth(saved) {
	case reader.SnapshotWidthOriginal:
		return reader.SnapshotWidthOriginal
	case reader.SnapshotWidthWide:
		return reader.SnapshotWidthWide
	}
	return reader.SnapshotWidthFluid
}

// setSnapshotWidth 改宽度档并写进偏好；写入失败只记日志，宽度照常生效。
func (s *Screen) setSnapshotWidth(width reader.SnapshotWidth) {
	sn := &s.detail.panel.snap
	sn.width, sn.widthSet = width, true
	if s.Prefs == nil {
		return
	}
	if err := s.Prefs.Set(settings.PrefWechatWidth, string(width)); err != nil {
		s.Guard.Background("library.snapshotWidth", err)
	}
}

// ---- 同步 ----

// snapshotPaneOn 报告这一帧阅读区是不是「网页快照」那一页（渲染层 isWechatPreview）。
func (s *Screen) snapshotPaneOn(caps ItemCaps) bool {
	return caps.IsWechat && s.detail.panel.snapshotView
}

// syncSnapshot 在内容面板每帧开头把快照面板的状态对齐：进入「网页快照」页或条目、版本、正文变了就发起读取；
// 读到新的快照按默认规则定排版（正文改过且看当前版本时先看标准排版，渲染层 setMode(edited && !versionId ? "simple" : "original")）；
// 并把这一帧可信的读取结果整理进 panelState.snap，之后的绘制只读它。离开这一页就把面板状态清掉（宽度档保留）。
func (s *Screen) syncSnapshot(v View, item services.LibraryItem, caps ItemCaps) {
	p := &s.detail.panel
	sn := &p.snap
	if !s.snapshotPaneOn(caps) {
		if sn.keyed || sn.version != "" || sn.simple || sn.menu.open || sn.sel.Open || sn.view != nil || sn.applied != nil {
			*sn = snapshotUI{width: sn.width, widthSet: sn.widthSet}
		}
		return
	}
	if !sn.widthSet {
		sn.width, sn.widthSet = loadSnapshotWidth(s.Prefs), true
	}
	want := snapshotKey{item: item.ID, version: sn.version, content: item.Content}
	if !sn.keyed || sn.key != want {
		sn.key, sn.keyed = want, true
		sn.stale = v.Detail.Snapshot.View
		sn.applied = nil
		s.Store.LoadWebSnapshot(s.Ctx, item.ID, sn.version)
	}
	sv := v.Detail.Snapshot
	sn.busy = sv.SupplementBusy
	sn.view, sn.loading, sn.err = nil, true, ""
	trusted := sv.ItemID == item.ID && sv.VersionID == sn.version && !(sv.View != nil && sv.View == sn.stale)
	if !trusted {
		return
	}
	sn.loading = sv.Loading
	if sv.Loading {
		return
	}
	sn.view, sn.err = sv.View, sv.Error
	if sn.view != nil && sn.view != sn.applied {
		sn.applied = sn.view
		sn.simple = sn.view.Edited && sn.version == ""
	}
}

// SnapshotState 在 detailreader.go 的 readerEvents 上：页面回报版面状态，由这里写回面板。
func (s *Screen) setSnapshotLayout(wideRail, tocOpen bool) {
	sn := &s.detail.panel.snap
	sn.wideRail, sn.tocOpen = wideRail, tocOpen
}

// ReportSnapshotLayout 代岛里的页面回报网页快照的版面状态（同 SnapshotState）：对照环境的无头会话没有岛，页面不会自己回报，
// 由场景按页面里 use-snapshot-reader 的同一条公式算出来交给它（viva/parity/scenes/library_snapshot.go）。
func (s *Screen) ReportSnapshotLayout(wideRail, tocOpen bool) { s.setSnapshotLayout(wideRail, tocOpen) }

// showVersionSnapshot 让阅读区切到某个来源版本的网页快照（版本面板里的「查看网页快照」；渲染层是把窗格嵌在版本面板里，
// 原生只有一座岛，所以改成切换阅读区）。工具侧栏收起，让用户看到切换的结果。
func (s *Screen) showVersionSnapshot(versionID string) {
	p := &s.detail.panel
	p.snapshotView = true
	p.snap.version = versionID
	p.snap.simple = false
	p.findActive, p.findCount = 0, 0
	s.closeTools()
}

// setSnapshotSimple 切换排版模式（工具条的按钮与「更多」菜单）；查找位置归零，命中数由新的阅读内容重新回报。
func (s *Screen) setSnapshotSimple(simple bool) {
	p := &s.detail.panel
	p.snap.simple = simple
	p.findActive, p.findCount = 0, 0
}

// ---- 工具条 ----

// snapshotButton 是工具条与通知条里的按钮（渲染层 button：rounded-lg border border-border px-3 py-1.5 text-xs hover:bg-accent disabled:opacity-50）。
func (s *Screen) snapshotButtonStyle() kit.ButtonStyle {
	l := s.Look
	t := l.T
	return kit.ButtonStyle{
		PadX: l.Space(3), PadY: l.Space(1.5), Radius: l.Rem(0.75), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Accent,
		Transition: kit.DurationNone, DisabledOpacity: 0.5, Surface: t.UiPanelBg,
	}
}

func (s *Screen) snapshotButton(c ui.C, label string, disabled bool) bool {
	return s.Look.Button(c, kit.ButtonSpec{Label: label, Disabled: disabled}, s.snapshotButtonStyle()).Clicked()
}

// snapshotToolbar 是标签页那一行里的快照工具条（渲染层 WebSnapshotPane 的 toolbar，经门户放进 snapshotToolbar 容器）：
// flex shrink-0 items-center gap-1.5：模式文字、切换排版的按钮、「目录」钮（目录没有常驻侧栏时）、宽度下拉（原文快照时）、「更多」与补采提示。
func (s *Screen) snapshotToolbar(c ui.C, item services.LibraryItem) {
	l := s.Look
	t := l.T
	sn := &s.detail.panel.snap
	original := sn.original()
	ui.Row(c).Items(ui.Center).Gap(l.Space(1.5)).Shrink(0).Children(func() {
		mode := locales.LibrarySnapshotModeSimple()
		switch {
		case original:
			mode = locales.ArticleAskViewSnapshot()
		case sn.edited():
			mode = locales.LibrarySnapshotModeEdited()
		}
		ui.Row(c.Key("mode")).Items(ui.Center).PadSides(0, l.Space(2), 0, l.Space(2)).Children(func() {
			l.Text(c.Key("mode-text"), mode, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
		switch {
		case !original && sn.view != nil && sn.view.Document != "":
			if s.snapshotButton(c.Key("view-original"), locales.LibrarySnapshotViewOriginal(), false) {
				s.setSnapshotSimple(false)
			}
		case original && sn.edited():
			if s.snapshotButton(c.Key("back-to-edited"), locales.LibrarySnapshotBackToEdited(), false) {
				s.setSnapshotSimple(true)
			}
		}
		if original && !sn.wideRail {
			// 目录钮是 aria-expanded：展开着时可访问状态取「展开」。
			if l.Button(c.Key("toc"), kit.ButtonSpec{Label: locales.LibraryCatalog(), Disclosure: true, On: sn.tocOpen}, s.snapshotButtonStyle()).Clicked() {
				sn.tocOpen = !sn.tocOpen
			}
		}
		if original {
			s.snapshotWidthSelect(c.Key("width"))
		}
		s.snapshotMoreButton(c.Key("more"))
		if sn.busy {
			l.Text(c.Key("busy"), locales.LibrarySnapshotSupplementing(), kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0)
		}
	})
	s.snapshotMenuView(c, item)
}

// snapshotWidthSelect 是宽度下拉（渲染层 Select：w-32，触发钮 h-7 rounded-lg bg-muted px-2 text-xs text-left）。
func (s *Screen) snapshotWidthSelect(c ui.C) {
	l := s.Look
	t := l.T
	sn := &s.detail.panel.snap
	trigger := kit.SelectTriggerStyle{
		Height: l.Rem(1.75), MinWidth: l.Rem(8), PadX: l.Space(2), Gap: l.Space(1), Radius: l.Rem(0.75),
		Bg: t.Muted, BgHover: t.Muted, FontPx: l.Rem(0.75), LinePx: l.Rem(1), TextStart: true, Transition: kit.DurationNone,
	}
	if picked, changed := l.Select(c, &sn.sel, kit.SelectSpec{
		Label: locales.LibrarySnapshotWidth(), Value: string(sn.width), MenuMinWidth: l.Rem(8), Trigger: &trigger,
		Options: []kit.SelectOption{
			{Value: string(reader.SnapshotWidthOriginal), Label: locales.LibrarySnapshotWidthOriginal()},
			{Value: string(reader.SnapshotWidthFluid), Label: locales.LibrarySnapshotWidthFluid()},
			{Value: string(reader.SnapshotWidthWide), Label: locales.LibrarySnapshotWidthWide()},
		},
	}); changed {
		s.setSnapshotWidth(reader.SnapshotWidth(picked))
	}
}

// snapshotMoreButton 是「更多」钮（aria-haspopup="menu"）：点它在钮的左下方 4px 打开菜单。
func (s *Screen) snapshotMoreButton(c ui.C) {
	l := s.Look
	t := l.T
	sn := &s.detail.panel.snap
	label := locales.CommonMore()
	box := s.inlineButton(c, inlineSpec{
		Name: label, Value: label, Popup: true, Open: sn.menu.open,
		Fg: t.Foreground, BgHover: t.Accent, Border: t.Border, BorderW: 1,
		Radius: l.Rem(0.75), Height: l.Rem(1.875), PadX: l.Space(3) + 1, Rect: &sn.menuRect, // px-3 加 1px 边框（inlineSpec 的内边距已含边框）
	}, func(fg tokens.Color) {
		l.Text(c.Key("text"), label, kit.TextXS, kit.WeightNormal, fg)
	})
	if box.Clicked() {
		if sn.menu.open {
			sn.menu.close()
		} else {
			r := sn.menuRect
			sn.menu.show(gfx.Point{X: r.X, Y: r.Y + r.Height + 4})
		}
	}
}

// snapshotMenuItems 是「更多」菜单：切换排版（原文快照时）、补采 / 重试缺失资源（回收站里没有）、导出 HTML（有快照时）、取消补采（在补采时）。
func (s *Screen) snapshotMenuItems(item services.LibraryItem) []kit.MenuItem {
	sn := &s.detail.panel.snap
	var items []kit.MenuItem
	if sn.original() {
		label := locales.LibrarySnapshotSwitchSimple()
		if sn.edited() {
			label = locales.LibrarySnapshotViewEdited()
		}
		items = append(items, kit.MenuItem{Label: label, OnSelect: func() { s.setSnapshotSimple(true) }})
	}
	meta := sn.meta()
	if item.DeletedAt == 0 {
		label := locales.LibrarySnapshotSupplementUpdate()
		if meta != nil && len(meta.Failures) > 0 {
			label = locales.LibrarySnapshotRetryMissing()
		}
		id := item.ID
		items = append(items, kit.MenuItem{Label: label, Disabled: sn.busy, OnSelect: func() { s.Store.SupplementWechatSnapshot(s.Ctx, id) }})
	}
	if meta != nil {
		id, version := item.ID, sn.view.Version.ID
		items = append(items, kit.MenuItem{Label: locales.LibrarySnapshotExportHtml(), OnSelect: func() { s.Store.ExportWebSnapshotHTML(s.Ctx, id, version) }})
	}
	if sn.busy {
		items = append(items, kit.MenuItem{Label: locales.LibrarySnapshotCancelSupplement(), OnSelect: func() { s.Store.CancelWechatSupplement(s.Ctx) }})
	}
	return items
}

// snapshotMenuView 声明「更多」菜单。每一帧都要经过 ContextMenu：关着的那一帧它负责复位。
func (s *Screen) snapshotMenuView(c ui.C, item services.LibraryItem) {
	sn := &s.detail.panel.snap
	var items []kit.MenuItem
	if sn.menu.open {
		items = s.snapshotMenuItems(item)
	}
	keep := sn.menuRect
	if s.Look.ContextMenu(c.Key("menu"), &sn.menu.st, kit.MenuSpec{
		Open: sn.menu.open, X: sn.menu.at.X, Y: sn.menu.at.Y, Items: items, Keep: &keep,
	}) {
		sn.menu.close()
	}
}

// ---- 通知条 ----

// snapshotNotices 是阅读区上方的状态与通知条（渲染层 WebSnapshotPane 里岛 / children 之前的几段，先后一致）：
// 读取失败（带重试）、读取中、没有快照（带补采）、已编辑提示、新来源版本提示、缺失资源与警告。
func (s *Screen) snapshotNotices(c ui.C, item services.LibraryItem) {
	l := s.Look
	t := l.T
	sn := &s.detail.panel.snap
	// 渲染层是一串块级元素；每条的内边距各不相同，所以逐条声明。
	line := func(key string, padY, padX float32, text string, size kit.TextStyle, color tokens.Color) {
		ui.Row(c.Key(key)).FillWidth().Shrink(0).PadSides(padY, padX, padY, padX).Children(func() {
			l.Text(c.Key(key+"-text"), text, size, kit.WeightNormal, color, kit.Wrapped()).Grow(1).Shrink(1).MinWidth(0)
		})
	}
	if sn.err != "" {
		// <div class="p-3 text-sm text-destructive" role="alert">：文字之后紧跟「重试读取」，按钮的文字继承红色。
		if s.snapshotInlineNotice(c, "error", sn.err, t.Destructive, 0, locales.LibrarySnapshotRetryLoad(), false, true) {
			s.Store.LoadWebSnapshot(s.Ctx, item.ID, sn.version)
		}
	}
	if sn.view == nil && sn.err == "" {
		line("loading", l.Space(3), l.Space(3), locales.LibrarySnapshotLoading(), kit.TextSM, t.MutedForeground)
	}
	if sn.view != nil && sn.meta() == nil {
		// <p class="p-3 text-sm text-muted-foreground">：补采钮（ml-2）紧跟文字，按钮的文字继承弱化色；回收站里的条目不摆补采钮。
		if item.DeletedAt != 0 {
			line("missing", l.Space(3), l.Space(3), locales.LibrarySnapshotMissing(), kit.TextSM, t.MutedForeground)
		} else if s.snapshotInlineNotice(c, "missing", locales.LibrarySnapshotMissing(), t.MutedForeground, l.Space(2), locales.LibrarySnapshotSupplement(), sn.busy, false) {
			s.Store.SupplementWechatSnapshot(s.Ctx, item.ID)
		}
	}
	if sn.view != nil && sn.view.Edited {
		hint := locales.LibrarySnapshotEditedHint()
		if sn.original() {
			hint = locales.LibrarySnapshotOriginalHint()
		}
		line("edited", l.Space(1), l.Space(3), hint, kit.TextXS, t.MutedForeground)
	}
	if sn.view != nil && sn.view.Pending {
		line("pending", l.Space(1), l.Space(3), locales.LibrarySnapshotPendingVersion(), kit.TextXS, t.MutedForeground)
	}
	if meta := sn.meta(); meta != nil && meta.Incomplete() {
		amber := tokens.Tailwind("amber-600")
		reasons := append([]string{}, meta.Warnings...)
		for _, f := range meta.Failures {
			reasons = append(reasons, f.Reason)
		}
		ui.Row(c.Key("incomplete")).FillWidth().Shrink(0).PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
		}).Children(func() {
			l.Text(c.Key("incomplete-text"), strings.Join(reasons, locales.NativeLibraryReasonSeparator()), kit.TextXS, kit.WeightNormal, amber, kit.Wrapped()).Grow(1).Shrink(1).MinWidth(0)
		})
	}
}
